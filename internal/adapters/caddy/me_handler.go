package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/domain/identity"
)

func init() {
	gocaddy.RegisterModule(MeHandler{})
}

// MeHandlerConfig is the JSON-serialisable configuration for the
// MeHandler Caddy module.
type MeHandlerConfig struct {
	// CookieName is the name of the Kratos session cookie.
	CookieName string `json:"cookie_name"`

	// KratosURL is the base URL of the Kratos public API (e.g. "http://kratos:4433").
	KratosURL string `json:"kratos_url"`
}

// MeHandler is a Caddy HTTP handler module that serves the /_vibewarden/me
// endpoint. It reads the Kratos session cookie from the request, validates the
// session via the Kratos whoami endpoint, and returns a JSON response with the
// authenticated user's session information.
//
// Responses:
//   - 200 + {"id","email","verified","role"} on valid session
//   - 401 + {"authenticated":false} on missing or invalid session
//   - 502 + {"error":"bad_gateway","message":"identity provider unavailable"} when Kratos is unreachable
//
// The Cache-Control: no-store header is always set to prevent caching of
// session information.
//
// The module is registered under the name "vibewarden_me" and referenced from
// the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_me", "cookie_name": "...", "kratos_url": "..."}
type MeHandler struct {
	Config MeHandlerConfig `json:"config"`
	logger *slog.Logger
	client *http.Client
}

// CaddyModule returns the Caddy module information.
func (MeHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_me",
		New: func() gocaddy.Module { return new(MeHandler) },
	}
}

// Provision sets up the handler.
func (h *MeHandler) Provision(_ gocaddy.Context) error {
	h.logger = slog.Default()
	h.client = &http.Client{Timeout: 5 * time.Second}
	return nil
}

// UnmarshalJSON implements custom unmarshalling to support both nested
// {"config": {...}} and flat {"cookie_name": "...", ...} JSON layouts.
func (h *MeHandler) UnmarshalJSON(data []byte) error {
	// Try nested config first.
	var nested struct {
		Config MeHandlerConfig `json:"config"`
	}
	if err := json.Unmarshal(data, &nested); err == nil && nested.Config.CookieName != "" {
		h.Config = nested.Config
		return nil
	}
	// Try flat structure.
	return json.Unmarshal(data, &h.Config)
}

// meWhoamiResponse mirrors the relevant fields from the Kratos
// GET /sessions/whoami JSON response for the /me endpoint.
type meWhoamiResponse struct {
	Active   bool `json:"active"`
	Identity struct {
		ID                  string             `json:"id"`
		Traits              map[string]any     `json:"traits"`
		VerifiableAddresses []meVerifiableAddr `json:"verifiable_addresses"`
	} `json:"identity"`
}

// meVerifiableAddr mirrors one entry in verifiable_addresses.
type meVerifiableAddr struct {
	Value    string `json:"value"`
	Via      string `json:"via"`
	Verified bool   `json:"verified"`
}

// meSuccessResponse is the JSON body returned on 200 OK.
type meSuccessResponse struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
	Role     string `json:"role"`
}

// meUnauthResponse is the JSON body returned on 401 Unauthorized.
type meUnauthResponse struct {
	Authenticated bool `json:"authenticated"`
}

// meErrorResponse is the JSON body returned on 502 Bad Gateway.
type meErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// callMeWhoami sends a GET request to the Kratos whoami endpoint with the
// session cookie. It returns the parsed response, a flag indicating whether
// the error is a connectivity issue (for 502 vs 401 distinction), and an error.
func (h *MeHandler) callMeWhoami(ctx context.Context, cookieValue string) (*meWhoamiResponse, bool, error) {
	url := h.Config.KratosURL + "/sessions/whoami"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("building whoami request: %w", err)
	}
	req.Header.Set("Cookie", h.Config.CookieName+"="+cookieValue)
	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		// Connection failure — Kratos is unreachable.
		return nil, true, fmt.Errorf("kratos whoami request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, false, fmt.Errorf("session invalid (401)")
	}
	if resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("kratos server error (%d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected kratos status (%d)", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, fmt.Errorf("reading whoami response: %w", err)
	}

	var whoami meWhoamiResponse
	if err := json.Unmarshal(body, &whoami); err != nil {
		return nil, false, fmt.Errorf("parsing whoami response: %w", err)
	}

	if !whoami.Active {
		return nil, false, fmt.Errorf("session not active")
	}

	return &whoami, false, nil
}

// meExtractEmail extracts the primary email from the whoami response,
// checking verifiable_addresses first, then falling back to traits["email"].
func meExtractEmail(whoami *meWhoamiResponse) string {
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

// meExtractVerified extracts the email verification status from the whoami response.
func meExtractVerified(whoami *meWhoamiResponse) bool {
	for _, addr := range whoami.Identity.VerifiableAddresses {
		if addr.Via == "email" {
			return addr.Verified
		}
	}
	return false
}

// meExtractRole extracts the role string from the whoami response and validates
// it via identity.NewRole. Returns identity.DefaultRole ("user") when the trait
// is absent, not a string, or not a recognised role value.
func meExtractRole(whoami *meWhoamiResponse, logger *slog.Logger) identity.Role {
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
		logger.Warn("me: ignoring invalid role from identity traits",
			slog.String("role", roleStr),
			slog.String("identity_id", whoami.Identity.ID),
			slog.String("error", err.Error()),
		)
		return identity.DefaultRole()
	}
	return role
}

// writeJSON is a helper that writes a JSON response with the given status code.
func (h *MeHandler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error("me: failed to write response",
			slog.String("error", err.Error()),
		)
	}
}

// ServeHTTP implements caddyhttp.Handler. It terminates the request and returns
// a JSON response — it does not call the next handler in the chain.
func (h *MeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	// Check for session cookie.
	cookie, err := r.Cookie(h.Config.CookieName)
	if err != nil {
		h.writeJSON(w, http.StatusUnauthorized, meUnauthResponse{Authenticated: false})
		return nil
	}

	// Validate session via Kratos whoami.
	whoami, isConnErr, err := h.callMeWhoami(r.Context(), cookie.Value)
	if err != nil {
		if isConnErr {
			h.logger.Error("me: kratos unavailable",
				slog.String("error", err.Error()),
			)
			h.writeJSON(w, http.StatusBadGateway, meErrorResponse{
				Error:   "bad_gateway",
				Message: "identity provider unavailable",
			})
			return nil
		}
		// Invalid/expired session.
		h.writeJSON(w, http.StatusUnauthorized, meUnauthResponse{Authenticated: false})
		return nil
	}

	role := meExtractRole(whoami, h.logger)

	h.writeJSON(w, http.StatusOK, meSuccessResponse{
		ID:       whoami.Identity.ID,
		Email:    meExtractEmail(whoami),
		Verified: meExtractVerified(whoami),
		Role:     role.String(),
	})
	return nil
}

// Interface guards.
var (
	_ caddyhttp.MiddlewareHandler = (*MeHandler)(nil)
	_ gocaddy.Module              = (*MeHandler)(nil)
	_ gocaddy.Provisioner         = (*MeHandler)(nil)
)
