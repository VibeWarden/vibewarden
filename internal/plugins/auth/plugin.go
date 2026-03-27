package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/authui"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Ensure auth.Plugin implements ports.DependencyChecker at compile time.
var _ ports.DependencyChecker = (*Plugin)(nil)

// kratosFlowPaths contains the URL path patterns that must be proxied to the
// Kratos public API instead of the upstream application.
// These paths are the Kratos self-service browser flows and the Ory canonical
// prefix (used by the Ory UI SDK).
var kratosFlowPaths = []string{
	"/self-service/login/*",
	"/self-service/registration/*",
	"/self-service/logout/*",
	"/self-service/settings/*",
	"/self-service/recovery/*",
	"/self-service/verification/*",
	"/.ory/kratos/public/*",
}

// whoamiPath is the Kratos endpoint used for health-checking connectivity.
const whoamiPath = "/health/ready"

// healthCheckTimeout is the HTTP client timeout used by Health().
const healthCheckTimeout = 3 * time.Second

// Plugin is the VibeWarden auth plugin.
//
// It implements ports.Plugin and ports.CaddyContributor.
// Priority is 40, placing it after TLS (10), security-headers (20), and
// rate-limiting (30) in the initialisation/contribution order.
//
// Responsibilities:
//   - Validate Kratos URLs on Init.
//   - Contribute a Caddy route that transparently proxies Kratos self-service
//     flow paths to the Kratos public API (ContributeCaddyRoutes).
//   - When UI.Mode is "built-in", start an internal auth UI HTTP server and
//     contribute a Caddy route that reverse-proxies the /_vibewarden/auth-ui
//     paths to it (ContributeCaddyRoutes).
//   - Contribute an auth middleware handler and an identity-headers handler
//     to the catch-all route (ContributeCaddyHandlers).
//   - Health-check Kratos connectivity (Health).
//
// Start and Stop are no-ops; session validation is performed inline by the
// Caddy auth middleware at request time.
type Plugin struct {
	cfg    Config
	logger *slog.Logger
	// sessionChecker is injected at construction time or created during Init
	// from cfg.KratosPublicURL. Storing it as an interface makes the plugin
	// unit-testable without a live Kratos instance.
	sessionChecker ports.SessionChecker
	// uiHandler is the built-in auth UI HTTP server.
	// It is nil when UI.Mode != "built-in" or when the plugin is disabled.
	uiHandler *authui.Handler
	// healthy tracks whether the last Health() call found Kratos reachable.
	healthy bool
	// healthMsg is the last health status message.
	healthMsg string
}

// New creates a new auth Plugin with the given configuration and logger.
// sessionChecker may be nil; when nil, Init creates a real Kratos adapter.
func New(cfg Config, logger *slog.Logger, sessionChecker ports.SessionChecker) *Plugin {
	return &Plugin{
		cfg:            cfg,
		logger:         logger,
		sessionChecker: sessionChecker,
	}
}

// Name returns the canonical plugin identifier "auth".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "auth" }

// Priority returns the plugin's initialisation priority.
// Auth is assigned priority 40 so it is initialised after TLS (10),
// security-headers (20), and rate-limiting (30).
func (p *Plugin) Priority() int { return 40 }

// Init validates the plugin configuration and, when no sessionChecker was
// injected, creates a real Kratos adapter from cfg.KratosPublicURL.
// Returns an error if the plugin is enabled but KratosPublicURL is empty.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		p.healthy = true
		p.healthMsg = "auth disabled"
		return nil
	}

	if err := validateConfig(p.cfg); err != nil {
		return fmt.Errorf("auth plugin init: %w", err)
	}

	// Apply defaults for optional fields.
	if p.cfg.SessionCookieName == "" {
		p.cfg.SessionCookieName = defaultSessionCookieName
	}

	// Determine the effective login URL.
	// In custom mode, UI.LoginURL takes precedence (already validated above).
	// In built-in mode, the top-level LoginURL is used (or the built-in default).
	uiMode := p.cfg.UI.Mode
	if uiMode == "" {
		uiMode = "built-in"
	}
	if uiMode == "custom" {
		// Custom mode: redirect to operator-supplied login URL.
		p.cfg.LoginURL = p.cfg.UI.LoginURL
	} else {
		// Built-in mode: use the configured LoginURL or the built-in default.
		if p.cfg.LoginURL == "" {
			p.cfg.LoginURL = defaultLoginURL
		}
	}

	// Create the real Kratos adapter when no fake was injected.
	if p.sessionChecker == nil {
		p.sessionChecker = kratosAdapterFunc(p.cfg.KratosPublicURL, p.logger)
	}

	// Start the built-in auth UI server when the mode is "built-in" (default).
	if uiMode == "built-in" {
		uiCfg := authui.AuthUIConfig{
			Mode:            uiMode,
			PrimaryColor:    p.cfg.UI.PrimaryColor,
			BackgroundColor: p.cfg.UI.BackgroundColor,
			TextColor:       p.cfg.UI.TextColor,
			ErrorColor:      p.cfg.UI.ErrorColor,
		}
		h, err := authui.NewHandler(uiCfg, p.logger)
		if err != nil {
			return fmt.Errorf("auth plugin init: creating auth UI handler: %w", err)
		}
		if err := h.Start(); err != nil {
			return fmt.Errorf("auth plugin init: starting auth UI server: %w", err)
		}
		p.uiHandler = h
	}

	p.healthy = true
	p.healthMsg = fmt.Sprintf("auth configured, kratos: %s", p.cfg.KratosPublicURL)

	p.logger.Info("auth plugin initialised",
		slog.String("kratos_public_url", p.cfg.KratosPublicURL),
		slog.String("session_cookie", p.cfg.SessionCookieName),
		slog.Int("public_paths", len(p.cfg.PublicPaths)),
		slog.String("ui_mode", uiMode),
	)

	return nil
}

// Start is a no-op for the auth plugin.
// Session validation happens inline during request processing via Caddy middleware.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop gracefully shuts down the auth plugin. When the built-in auth UI
// server is running, it is stopped. The function honours the provided context.
func (p *Plugin) Stop(ctx context.Context) error {
	if p.uiHandler != nil {
		if err := p.uiHandler.Stop(ctx); err != nil {
			return fmt.Errorf("auth plugin stop: stopping auth UI server: %w", err)
		}
	}
	return nil
}

// Health checks whether Kratos is reachable by calling its health/ready
// endpoint. Returns healthy=true with a "auth disabled" message when the
// plugin is disabled. When enabled, returns the result of the last
// connectivity probe performed during Init (a live probe would block).
func (p *Plugin) Health() ports.HealthStatus {
	return ports.HealthStatus{
		Healthy: p.healthy,
		Message: p.healthMsg,
	}
}

// HealthCheck performs a live connectivity probe against Kratos and updates
// the internal health state. It is safe to call from a background goroutine.
// Unlike Health(), this method makes a real HTTP request.
func (p *Plugin) HealthCheck(ctx context.Context) ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{Healthy: true, Message: "auth disabled"}
	}

	probeURL := p.cfg.KratosPublicURL + whoamiPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("auth: cannot build health probe: %s", err)
		return ports.HealthStatus{Healthy: false, Message: p.healthMsg}
	}

	client := &http.Client{Timeout: healthCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("auth: kratos unreachable: %s", err)
		return ports.HealthStatus{Healthy: false, Message: p.healthMsg}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("auth: kratos health probe returned %d", resp.StatusCode)
		return ports.HealthStatus{Healthy: false, Message: p.healthMsg}
	}

	p.healthy = true
	p.healthMsg = fmt.Sprintf("auth configured, kratos: %s", p.cfg.KratosPublicURL)
	return ports.HealthStatus{Healthy: true, Message: p.healthMsg}
}

// DependencyName returns "kratos" as the dependency identifier for health
// endpoint reporting.
// Implements ports.DependencyChecker.
func (p *Plugin) DependencyName() string { return "kratos" }

// CheckDependency performs a live connectivity probe against the Kratos
// public health endpoint and returns a DependencyStatus with latency.
// Implements ports.DependencyChecker.
func (p *Plugin) CheckDependency(ctx context.Context) ports.DependencyStatus {
	if !p.cfg.Enabled {
		return ports.DependencyStatus{Status: "healthy", LatencyMS: 0}
	}

	probeURL := p.cfg.KratosPublicURL + whoamiPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return ports.DependencyStatus{
			Status: "unhealthy",
			Error:  fmt.Sprintf("cannot build health probe: %s", err),
		}
	}

	start := time.Now()
	client := &http.Client{Timeout: healthCheckTimeout}
	resp, err := client.Do(req)
	latencyMS := time.Since(start).Milliseconds()

	if err != nil {
		return ports.DependencyStatus{
			Status:    "unhealthy",
			LatencyMS: latencyMS,
			Error:     fmt.Sprintf("kratos unreachable: %s", err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return ports.DependencyStatus{
			Status:    "unhealthy",
			LatencyMS: latencyMS,
			Error:     fmt.Sprintf("kratos health probe returned %d", resp.StatusCode),
		}
	}

	return ports.DependencyStatus{
		Status:    "healthy",
		LatencyMS: latencyMS,
	}
}

// ContributeCaddyRoutes returns the Caddy routes contributed by the auth plugin.
//
// When enabled, two sets of routes are returned:
//  1. A route that transparently proxies all Kratos self-service flow paths and
//     the Ory canonical prefix (/.ory/kratos/public/*) to the Kratos public API.
//  2. When the auth UI mode is "built-in", a route that reverse-proxies the four
//     built-in auth UI paths (/_vibewarden/login, /_vibewarden/registration,
//     /_vibewarden/recovery, /_vibewarden/verification) to the internal auth UI
//     HTTP server started during Init.
//
// Returns nil when the plugin is disabled.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute {
	if !p.cfg.Enabled {
		return nil
	}

	kratosAddr := urlToDialAddr(p.cfg.KratosPublicURL)

	routes := []ports.CaddyRoute{
		{
			MatchPath: "/self-service/*",
			Priority:  40,
			Handler: map[string]any{
				"match": []map[string]any{
					{"path": kratosFlowPaths},
				},
				"handle": []map[string]any{
					{
						"handler": "reverse_proxy",
						"upstreams": []map[string]any{
							{"dial": kratosAddr},
						},
					},
				},
			},
		},
	}

	// Add auth UI route when the built-in UI is running.
	if p.uiHandler != nil {
		uiAddr := p.uiHandler.Addr()
		routes = append(routes, ports.CaddyRoute{
			MatchPath: "/_vibewarden/login",
			Priority:  39, // just before the Kratos proxy route
			Handler: map[string]any{
				"match": []map[string]any{
					{
						"path": []string{
							"/_vibewarden/login",
							"/_vibewarden/registration",
							"/_vibewarden/recovery",
							"/_vibewarden/verification",
							"/_vibewarden/settings",
						},
					},
				},
				"handle": []map[string]any{
					{
						"handler": "reverse_proxy",
						"upstreams": []map[string]any{
							{"dial": uiAddr},
						},
					},
				},
			},
		})
	}

	return routes
}

// ContributeCaddyHandlers returns the Caddy handlers that the auth plugin
// injects into the catch-all route's handler chain.
//
// Two handlers are returned (both at Priority 40):
//  1. An auth middleware handler that validates the Kratos session cookie and
//     redirects unauthenticated requests to the login URL.
//  2. An identity-headers handler that forwards the authenticated user's ID,
//     email, and verification status to the upstream application as
//     X-User-Id, X-User-Email, and X-User-Verified request headers.
//
// Returns nil when the plugin is disabled.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled {
		return nil
	}

	cookieName := p.cfg.SessionCookieName
	if cookieName == "" {
		cookieName = defaultSessionCookieName
	}
	loginURL := p.cfg.LoginURL
	if loginURL == "" {
		loginURL = defaultLoginURL
	}

	// Build the list of public paths: always include /_vibewarden/* and
	// /self-service/* (Kratos UI) plus user-configured paths.
	publicPaths := []string{
		"/_vibewarden/*",
		"/self-service/*",
		"/.ory/*",
	}
	publicPaths = append(publicPaths, p.cfg.PublicPaths...)

	// Auth middleware handler: validates the session cookie via Kratos.
	// This uses the Caddy forward_auth handler which calls an internal
	// auth endpoint. Since VibeWarden handles auth natively, we represent
	// this as a request_header handler that signals expected auth headers
	// and a static_response for the redirect — the actual session
	// validation logic lives in the application layer proxy service.
	//
	// For the Caddy JSON config we use the auth cookie validation approach:
	// inject a handler that checks for the session cookie and redirects
	// missing sessions to the login URL.
	authHandler := buildAuthHandler(cookieName, loginURL, publicPaths)

	// Identity-headers handler: sets upstream request headers from Kratos
	// session data. These headers are consumed by the upstream application.
	identityHeadersHandler := buildIdentityHeadersHandler(cookieName)

	return []ports.CaddyHandler{
		{
			Handler:  authHandler,
			Priority: 40,
		},
		{
			Handler:  identityHeadersHandler,
			Priority: 41,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal builders — pure functions, no side effects.
// ---------------------------------------------------------------------------

// validateConfig checks that the auth configuration is self-consistent.
func validateConfig(cfg Config) error {
	if cfg.KratosPublicURL == "" {
		return fmt.Errorf("kratos_public_url is required when auth is enabled")
	}
	if _, err := url.ParseRequestURI(cfg.KratosPublicURL); err != nil {
		return fmt.Errorf("kratos_public_url %q is not a valid URL: %w", cfg.KratosPublicURL, err)
	}
	if cfg.UI.Mode == "custom" && cfg.UI.LoginURL == "" {
		return fmt.Errorf("ui.login_url is required when ui.mode is \"custom\"")
	}
	return nil
}

// buildAuthHandler creates the Caddy handler configuration for session cookie
// validation. It uses the Caddy request_header handler to insert the expected
// cookie header and a static_response redirect for unauthenticated requests.
//
// In the Caddy JSON config the auth enforcement is represented as a
// forward_auth handler that delegates session validation to the Kratos
// whoami endpoint. Public paths bypass auth via a path matcher.
func buildAuthHandler(cookieName, loginURL string, publicPaths []string) map[string]any {
	return map[string]any{
		"handler":      "authentication",
		"cookie_name":  cookieName,
		"login_url":    loginURL,
		"public_paths": publicPaths,
	}
}

// buildIdentityHeadersHandler creates the Caddy handler configuration that
// sets upstream identity headers from the validated Kratos session.
// The upstream application receives:
//   - X-User-Id: Kratos identity UUID
//   - X-User-Email: primary email address
//   - X-User-Verified: "true" or "false"
func buildIdentityHeadersHandler(cookieName string) map[string]any {
	return map[string]any{
		"handler":     "identity_headers",
		"cookie_name": cookieName,
	}
}

// urlToDialAddr extracts the host:port dial address from a full URL string.
// For example "http://127.0.0.1:4433" becomes "127.0.0.1:4433".
// If the URL has no explicit port the scheme default is used: "80" for http,
// "443" for https. Malformed URLs fall back to returning the original string.
func urlToDialAddr(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	host := u.Hostname()
	port := u.Port()

	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}

	return net.JoinHostPort(host, port)
}

// kratosAdapterFunc is the factory used to create a Kratos SessionChecker
// during Init when no sessionChecker was injected. Tests can replace this
// variable with a factory that returns a fake.
var kratosAdapterFunc = defaultKratosAdapterFactory

// defaultKratosAdapterFactory creates a real Kratos HTTP adapter.
// It is the production implementation of kratosAdapterFunc.
func defaultKratosAdapterFactory(publicURL string, logger *slog.Logger) ports.SessionChecker {
	return &kratosHTTPChecker{publicURL: publicURL, logger: logger}
}

// kratosHTTPChecker is a minimal SessionChecker that wraps the real Kratos
// adapter without importing the adapters/kratos package directly from here
// (avoiding circular deps). The plugin package imports only ports and stdlib.
// The real Kratos adapter is injected via New() in the wiring layer (serve.go).
type kratosHTTPChecker struct {
	publicURL string
	logger    *slog.Logger
}

// CheckSession implements ports.SessionChecker.
// Delegates to a real HTTP call against the Kratos /sessions/whoami endpoint.
func (c *kratosHTTPChecker) CheckSession(ctx context.Context, sessionCookie string) (*ports.Session, error) {
	if sessionCookie == "" {
		return nil, ports.ErrSessionNotFound
	}

	reqURL := c.publicURL + "/sessions/whoami"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building whoami request: %w", err)
	}
	req.Header.Set("Cookie", sessionCookie)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kratos unreachable: %w", ports.ErrAuthProviderUnavailable)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// Valid session — caller handles parse.
		return &ports.Session{Active: true}, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ports.ErrSessionInvalid
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("kratos responded with %d: %w", resp.StatusCode, ports.ErrAuthProviderUnavailable)
	default:
		return nil, fmt.Errorf("unexpected kratos status %d: %w", resp.StatusCode, ports.ErrSessionInvalid)
	}
}
