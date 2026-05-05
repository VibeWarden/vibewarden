package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/domain/identity"
)

func init() {
	gocaddy.RegisterModule(AuthHandler{})
}

// AuthHandlerConfig is the JSON-serialisable configuration for the
// AuthHandler Caddy module.
type AuthHandlerConfig struct {
	// CookieName is the name of the Kratos session cookie.
	CookieName string `json:"cookie_name"`

	// LoginURL is the URL to redirect unauthenticated users to.
	LoginURL string `json:"login_url"`

	// PublicPaths is a list of path patterns that bypass authentication.
	PublicPaths []string `json:"public_paths"`

	// KratosURL is the base URL of the Kratos public API (e.g. "http://kratos:4433").
	KratosURL string `json:"kratos_url"`

	// RolePaths maps role names (e.g. "admin") to URL path patterns that
	// require that role. When set, authenticated users whose role does not
	// match receive HTTP 403. When empty, no role enforcement is performed.
	RolePaths map[string][]string `json:"role_paths,omitempty"`
}

// AuthHandler is a Caddy HTTP handler module that validates Kratos session
// cookies and enforces authentication. Unauthenticated requests are redirected
// to the configured login URL. Authenticated requests have identity headers
// (X-User-Id, X-User-Email, X-User-Verified, X-User-Role) injected for the upstream app.
//
// The module is registered under the name "vibewarden_authentication" and referenced from
// the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_authentication", "cookie_name": "...", "login_url": "...", "public_paths": [...], "kratos_url": "..."}
type AuthHandler struct {
	Config AuthHandlerConfig `json:"config"`
	logger *slog.Logger
	client *http.Client
}

// CaddyModule returns the Caddy module information.
func (AuthHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_authentication",
		New: func() gocaddy.Module { return new(AuthHandler) },
	}
}

// kratosDefaultPublicPaths contains the URL path patterns that must be
// exempted from authentication when auth.mode is "kratos". These paths
// cover the Kratos self-service browser flows, auth UI routes, the whoami
// endpoint, and error pages. They are automatically appended in Provision()
// so the user does not need to list them in auth.public_paths.
var kratosDefaultPublicPaths = []string{
	"/auth/*",
	"/self-service/*",
	"/login",
	"/registration",
	"/recovery",
	"/verification",
	"/error",
	"/sessions/whoami",
}

// Provision sets up the handler.
func (h *AuthHandler) Provision(_ gocaddy.Context) error {
	h.logger = slog.Default()
	h.client = &http.Client{Timeout: 5 * time.Second}

	// When a KratosURL is configured, automatically append the default Kratos
	// public paths so that auth flows and UI routes are never blocked by the
	// authentication check.
	if h.Config.KratosURL != "" {
		h.Config.PublicPaths = appendKratosPublicPaths(h.Config.PublicPaths)
	}

	return nil
}

// appendKratosPublicPaths appends kratosDefaultPublicPaths to the given paths,
// skipping any entries that are already present.
func appendKratosPublicPaths(existing []string) []string {
	set := make(map[string]bool, len(existing))
	for _, p := range existing {
		set[p] = true
	}
	result := make([]string, len(existing))
	copy(result, existing)
	for _, p := range kratosDefaultPublicPaths {
		if !set[p] {
			result = append(result, p)
		}
	}
	return result
}

// UnmarshalJSON implements custom unmarshalling to support both nested
// {"config": {...}} and flat {"cookie_name": "...", ...} JSON layouts.
func (h *AuthHandler) UnmarshalJSON(data []byte) error {
	// Try nested config first.
	var nested struct {
		Config AuthHandlerConfig `json:"config"`
	}
	if err := json.Unmarshal(data, &nested); err == nil && nested.Config.CookieName != "" {
		h.Config = nested.Config
		return nil
	}
	// Try flat structure.
	return json.Unmarshal(data, &h.Config)
}

// isPublicPath checks if the request path matches any public path pattern.
//
// For wildcard patterns ending in "/*" the prefix boundary is enforced: a
// pattern "/auth/*" matches "/auth", "/auth/", and "/auth/login", but NOT
// "/auth-evil" or "/authentic". The check requires either an exact prefix
// match or a match followed by a slash separator.
func (h *AuthHandler) isPublicPath(reqPath string) bool {
	for _, p := range h.Config.PublicPaths {
		if strings.HasSuffix(p, "/*") {
			prefix := strings.TrimSuffix(p, "/*")
			if reqPath == prefix || strings.HasPrefix(reqPath, prefix+"/") {
				return true
			}
		}
		matched, _ := path.Match(p, reqPath)
		if matched {
			return true
		}
		if p == reqPath {
			return true
		}
	}
	return false
}

// stripXUserHeaders removes all request headers whose name starts with
// "X-User-". It must be called at the top of ServeHTTP before any session
// validation so that a client cannot impersonate an authenticated user by
// injecting these headers directly.
func stripXUserHeaders(r *http.Request) {
	for key := range r.Header {
		if strings.HasPrefix(key, "X-User-") {
			delete(r.Header, key)
		}
	}
}

// kratosWhoamiResponse mirrors the relevant fields from the Kratos
// GET /sessions/whoami JSON response.
type kratosWhoamiResponse struct {
	Active   bool `json:"active"`
	Identity struct {
		ID                  string                 `json:"id"`
		Traits              map[string]any         `json:"traits"`
		VerifiableAddresses []kratosVerifiableAddr `json:"verifiable_addresses"`
	} `json:"identity"`
}

// kratosVerifiableAddr mirrors one entry in verifiable_addresses.
type kratosVerifiableAddr struct {
	Value    string `json:"value"`
	Via      string `json:"via"`
	Verified bool   `json:"verified"`
}

// callWhoami sends a GET request to the Kratos whoami endpoint with the
// session cookie and returns the parsed response. It returns an error if
// the request fails or Kratos responds with non-200.
func (h *AuthHandler) callWhoami(ctx context.Context, cookieValue string) (*kratosWhoamiResponse, error) {
	url := h.Config.KratosURL + "/sessions/whoami"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building whoami request: %w", err)
	}
	req.Header.Set("Cookie", h.Config.CookieName+"="+cookieValue)
	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kratos whoami request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("session invalid (401)")
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("kratos server error (%d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected kratos status (%d)", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading whoami response: %w", err)
	}

	var whoami kratosWhoamiResponse
	if err := json.Unmarshal(body, &whoami); err != nil {
		return nil, fmt.Errorf("parsing whoami response: %w", err)
	}

	if !whoami.Active {
		return nil, fmt.Errorf("session not active")
	}

	return &whoami, nil
}

// extractEmail extracts the primary email from the Kratos whoami response,
// checking verifiable_addresses first, then falling back to traits["email"].
func extractEmail(whoami *kratosWhoamiResponse) string {
	for _, addr := range whoami.Identity.VerifiableAddresses {
		if addr.Via == "email" {
			return addr.Value
		}
	}
	if email, ok := whoami.Identity.Traits["email"].(string); ok {
		return email
	}
	return ""
}

// extractVerified extracts the email verification status from the Kratos
// whoami response.
func extractVerified(whoami *kratosWhoamiResponse) bool {
	for _, addr := range whoami.Identity.VerifiableAddresses {
		if addr.Via == "email" {
			return addr.Verified
		}
	}
	return false
}

// setIdentityHeaders injects X-User-Id, X-User-Email, and X-User-Verified
// headers into the request based on the Kratos whoami response.
func setIdentityHeaders(r *http.Request, whoami *kratosWhoamiResponse) {
	if whoami.Identity.ID != "" {
		r.Header.Set("X-User-Id", whoami.Identity.ID)
	}
	if email := extractEmail(whoami); email != "" {
		r.Header.Set("X-User-Email", email)
	}
	if extractVerified(whoami) {
		r.Header.Set("X-User-Verified", "true")
	} else {
		r.Header.Set("X-User-Verified", "false")
	}
}

// extractRole extracts the role string from the Kratos whoami response and
// validates it via identity.NewRole. It returns identity.DefaultRole ("user")
// when the trait is absent, not a string, or not a recognised role value.
// When an unrecognised role value is encountered, a warning is logged.
func extractRole(whoami *kratosWhoamiResponse, logger *slog.Logger) identity.Role {
	raw, ok := whoami.Identity.Traits["role"]
	if !ok {
		return identity.DefaultRole()
	}
	roleStr, ok := raw.(string)
	if !ok || roleStr == "" {
		return identity.DefaultRole()
	}
	role, err := identity.NewRole(roleStr)
	if err != nil {
		logger.Warn("authentication: ignoring invalid role from identity traits",
			slog.String("role", roleStr),
			slog.String("identity_id", whoami.Identity.ID),
			slog.String("error", err.Error()),
		)
		return identity.DefaultRole()
	}
	return role
}

// matchRequiredRole checks whether the request path requires a specific role
// according to the configured RolePaths map. It returns the required role name
// and true if a match is found, or ("", false) if no role restriction applies.
func (h *AuthHandler) matchRequiredRole(reqPath string) (string, bool) {
	for role, patterns := range h.Config.RolePaths {
		for _, p := range patterns {
			if strings.HasSuffix(p, "/*") {
				prefix := strings.TrimSuffix(p, "*")
				if strings.HasPrefix(reqPath, prefix) {
					return role, true
				}
			}
			matched, _ := path.Match(p, reqPath)
			if matched {
				return role, true
			}
			if p == reqPath {
				return role, true
			}
		}
	}
	return "", false
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Strip all X-User-* headers from the incoming request before any session
	// validation. This is the Go-layer defence-in-depth complement to the
	// Caddy-layer wildcard delete in buildUserHeaderStripHandler.
	stripXUserHeaders(r)

	// Public paths: optional auth — check session if cookie present, set
	// headers if valid, but never redirect or block the request.
	if h.isPublicPath(r.URL.Path) {
		if cookie, err := r.Cookie(h.Config.CookieName); err == nil {
			if whoami, err := h.callWhoami(r.Context(), cookie.Value); err == nil {
				setIdentityHeaders(r, whoami)
				role := extractRole(whoami, h.logger)
				r.Header.Set("X-User-Role", role.String())
			}
			// If whoami fails, proceed without headers (don't redirect, don't error).
		}
		return next.ServeHTTP(w, r)
	}

	// Check for session cookie.
	cookie, err := r.Cookie(h.Config.CookieName)
	if err != nil {
		// No cookie — redirect to login.
		http.Redirect(w, r, h.Config.LoginURL, http.StatusFound)
		return nil
	}

	// Validate session via Kratos whoami.
	whoami, err := h.callWhoami(r.Context(), cookie.Value)
	if err != nil {
		h.logger.Error("authentication: kratos whoami failed",
			slog.String("error", err.Error()),
			slog.String("path", r.URL.Path),
		)
		http.Redirect(w, r, h.Config.LoginURL, http.StatusFound)
		return nil
	}

	// Set identity headers for the upstream app.
	setIdentityHeaders(r, whoami)

	// Always set the role header from traits. extractRole validates the
	// role value through the domain layer; invalid values are logged as
	// warnings and the default ("user") is used instead.
	role := extractRole(whoami, h.logger)
	r.Header.Set("X-User-Role", role.String())

	// Enforce role-based path restrictions when configured.
	if len(h.Config.RolePaths) > 0 {
		if requiredRole, ok := h.matchRequiredRole(r.URL.Path); ok {
			if role.String() != requiredRole {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden","message":"insufficient role for this path"}`))
				return nil
			}
		}
	}

	return next.ServeHTTP(w, r)
}

// Interface guards.
var (
	_ caddyhttp.MiddlewareHandler = (*AuthHandler)(nil)
	_ gocaddy.Module              = (*AuthHandler)(nil)
	_ gocaddy.Provisioner         = (*AuthHandler)(nil)
)
