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
}

// AuthHandler is a Caddy HTTP handler module that validates Kratos session
// cookies and enforces authentication. Unauthenticated requests are redirected
// to the configured login URL. Authenticated requests have identity headers
// (X-User-Id, X-User-Email, X-User-Verified) injected for the upstream app.
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

// Provision sets up the handler.
func (h *AuthHandler) Provision(_ gocaddy.Context) error {
	h.logger = slog.Default()
	h.client = &http.Client{Timeout: 5 * time.Second}
	return nil
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
func (h *AuthHandler) isPublicPath(reqPath string) bool {
	for _, p := range h.Config.PublicPaths {
		if strings.HasSuffix(p, "/*") {
			prefix := strings.TrimSuffix(p, "/*")
			if strings.HasPrefix(reqPath, prefix) {
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

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Skip public paths.
	if h.isPublicPath(r.URL.Path) {
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

	return next.ServeHTTP(w, r)
}

// Interface guards.
var (
	_ caddyhttp.MiddlewareHandler = (*AuthHandler)(nil)
	_ gocaddy.Module              = (*AuthHandler)(nil)
	_ gocaddy.Provisioner         = (*AuthHandler)(nil)
)
