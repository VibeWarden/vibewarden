package caddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/domain/auth"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// stubAPIKeyValidator is an in-memory fake that implements ports.APIKeyValidator.
type stubAPIKeyValidator struct {
	keys map[string]*auth.APIKey
	err  error
}

func (s *stubAPIKeyValidator) Validate(_ context.Context, plaintextKey string) (*auth.APIKey, error) {
	if s.err != nil {
		return nil, s.err
	}
	k, ok := s.keys[plaintextKey]
	if !ok {
		return nil, ports.ErrAPIKeyInvalid
	}
	return k, nil
}

// validStubKey returns a well-formed, active APIKey for use in handler tests.
func validStubKey() *auth.APIKey {
	return &auth.APIKey{
		Name:    "test-key",
		KeyHash: auth.HashKey("vw_test_secret"),
		Scopes:  []auth.Scope{"read"},
		Active:  true,
	}
}

// nextHandlerStatus returns a caddyhttp.Handler that writes the given status code.
func nextHandlerStatus(code int) caddyhttp.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(code)
		return nil
	}
}

// ---------------------------------------------------------------------------
// Module registration
// ---------------------------------------------------------------------------

// TestAPIKeyHandler_CaddyModule verifies the Caddy module metadata.
func TestAPIKeyHandler_CaddyModule(t *testing.T) {
	info := APIKeyHandler{}.CaddyModule()

	if info.ID != "http.handlers.api_key_auth" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.api_key_auth")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}
	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*APIKeyHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *APIKeyHandler", mod)
	}
}

// TestAPIKeyHandler_InterfaceGuards verifies the handler satisfies required
// Caddy interfaces at compile time.
func TestAPIKeyHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*APIKeyHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*APIKeyHandler)(nil)
	var _ gocaddy.Module = (*APIKeyHandler)(nil)
}

// ---------------------------------------------------------------------------
// Provision
// ---------------------------------------------------------------------------

// TestAPIKeyHandler_ProvisionWith_NilValidator verifies that ProvisionWith
// with a nil APIKeyValidator puts the handler in fail-closed mode (no error
// returned — the handler deals with this at ServeHTTP time).
func TestAPIKeyHandler_ProvisionWith_NilValidator(t *testing.T) {
	h := &APIKeyHandler{}
	if err := h.ProvisionWith(RuntimeServices{}); err != nil {
		t.Fatalf("ProvisionWith(nil validator) returned unexpected error: %v", err)
	}
	if !h.nilValidator {
		t.Error("nilValidator should be true when APIKeyValidator is nil")
	}
}

// TestAPIKeyHandler_ProvisionWith_ValidValidator verifies that ProvisionWith
// succeeds and clears nilValidator when a real validator is provided.
func TestAPIKeyHandler_ProvisionWith_ValidValidator(t *testing.T) {
	validator := &stubAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	h := &APIKeyHandler{}
	if err := h.ProvisionWith(RuntimeServices{APIKeyValidator: validator}); err != nil {
		t.Fatalf("ProvisionWith(validator) returned unexpected error: %v", err)
	}
	if h.nilValidator {
		t.Error("nilValidator should be false when APIKeyValidator is provided")
	}
	if h.handler == nil {
		t.Error("handler should be set after successful Provision")
	}
}

// ---------------------------------------------------------------------------
// ServeHTTP
// ---------------------------------------------------------------------------

// TestAPIKeyHandler_ServeHTTP_NilValidator verifies the fail-closed behaviour:
// when the validator was absent at Provision time, every request receives 500.
func TestAPIKeyHandler_ServeHTTP_NilValidator(t *testing.T) {
	h := &APIKeyHandler{}
	_ = h.ProvisionWith(RuntimeServices{}) // sets nilValidator = true

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "any-key")
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, nextHandlerStatus(http.StatusOK)); err != nil {
		t.Fatalf("ServeHTTP returned unexpected error: %v", err)
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (fail-closed)", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] != "auth misconfigured" {
		t.Errorf("error = %q, want %q", body["error"], "auth misconfigured")
	}
}

// TestAPIKeyHandler_ServeHTTP_MissingKey verifies that a request without the
// configured header is rejected with 401.
func TestAPIKeyHandler_ServeHTTP_MissingKey(t *testing.T) {
	validator := &stubAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	h := &APIKeyHandler{Config: APIKeyHandlerConfig{Header: "X-API-Key"}}
	_ = h.ProvisionWith(RuntimeServices{APIKeyValidator: validator})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	// No X-API-Key header.
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, nextHandlerStatus(http.StatusOK)); err != nil {
		t.Fatalf("ServeHTTP returned unexpected error: %v", err)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestAPIKeyHandler_ServeHTTP_InvalidKey verifies that an unrecognised key
// is rejected with 401.
func TestAPIKeyHandler_ServeHTTP_InvalidKey(t *testing.T) {
	validator := &stubAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	h := &APIKeyHandler{Config: APIKeyHandlerConfig{Header: "X-API-Key"}}
	_ = h.ProvisionWith(RuntimeServices{APIKeyValidator: validator})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "bad-key")
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, nextHandlerStatus(http.StatusOK)); err != nil {
		t.Fatalf("ServeHTTP returned unexpected error: %v", err)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestAPIKeyHandler_ServeHTTP_ValidKey verifies that a valid key passes
// through to the next handler (200 OK).
func TestAPIKeyHandler_ServeHTTP_ValidKey(t *testing.T) {
	key := validStubKey()
	validator := &stubAPIKeyValidator{keys: map[string]*auth.APIKey{"vw_test_secret": key}}
	h := &APIKeyHandler{Config: APIKeyHandlerConfig{Header: "X-API-Key"}}
	_ = h.ProvisionWith(RuntimeServices{APIKeyValidator: validator})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "vw_test_secret")
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, nextHandlerStatus(http.StatusOK)); err != nil {
		t.Fatalf("ServeHTTP returned unexpected error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// TestAPIKeyHandler_ServeHTTP_ForbiddenScope verifies that a valid key that
// does not hold the required scope for the path is rejected with 403.
func TestAPIKeyHandler_ServeHTTP_ForbiddenScope(t *testing.T) {
	// Key has only "read" scope; the admin path requires "admin".
	key := validStubKey() // scopes: ["read"]
	validator := &stubAPIKeyValidator{keys: map[string]*auth.APIKey{"vw_test_secret": key}}
	h := &APIKeyHandler{
		Config: APIKeyHandlerConfig{
			Header: "X-API-Key",
			ScopeRules: []APIKeyScopeRuleConfig{
				{
					Path:           "/admin/*",
					Methods:        nil,
					RequiredScopes: []string{"admin"},
				},
			},
		},
	}
	_ = h.ProvisionWith(RuntimeServices{APIKeyValidator: validator})

	req := httptest.NewRequest(http.MethodPost, "/admin/users", nil)
	req.Header.Set("X-API-Key", "vw_test_secret")
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, nextHandlerStatus(http.StatusOK)); err != nil {
		t.Fatalf("ServeHTTP returned unexpected error: %v", err)
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// TestAPIKeyHandler_ServeHTTP_ValidKey_ScopeMatch verifies that a key holding
// the required scopes for a restricted path is allowed through.
func TestAPIKeyHandler_ServeHTTP_ValidKey_ScopeMatch(t *testing.T) {
	key := &auth.APIKey{
		Name:    "admin-key",
		KeyHash: auth.HashKey("vw_admin_secret"),
		Scopes:  []auth.Scope{"admin"},
		Active:  true,
	}
	validator := &stubAPIKeyValidator{keys: map[string]*auth.APIKey{"vw_admin_secret": key}}
	h := &APIKeyHandler{
		Config: APIKeyHandlerConfig{
			Header: "X-API-Key",
			ScopeRules: []APIKeyScopeRuleConfig{
				{
					Path:           "/admin/*",
					RequiredScopes: []string{"admin"},
				},
			},
		},
	}
	_ = h.ProvisionWith(RuntimeServices{APIKeyValidator: validator})

	req := httptest.NewRequest(http.MethodPost, "/admin/users", nil)
	req.Header.Set("X-API-Key", "vw_admin_secret")
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, nextHandlerStatus(http.StatusOK)); err != nil {
		t.Fatalf("ServeHTTP returned unexpected error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ---------------------------------------------------------------------------
// UnmarshalJSON
// ---------------------------------------------------------------------------

// TestAPIKeyHandler_UnmarshalJSON_FlatStructure verifies that the flat
// map[string]any structure produced by buildAPIKeyHandler in plugin.go
// is correctly decoded by UnmarshalJSON.
func TestAPIKeyHandler_UnmarshalJSON_FlatStructure(t *testing.T) {
	raw := `{
		"handler": "api_key_auth",
		"header": "Authorization",
		"scope_rules": [
			{"path": "/admin/*", "methods": ["POST"], "required_scopes": ["admin"]}
		]
	}`

	h := &APIKeyHandler{}
	if err := json.Unmarshal([]byte(raw), h); err != nil {
		t.Fatalf("UnmarshalJSON error: %v", err)
	}
	if h.Config.Header != "Authorization" {
		t.Errorf("Config.Header = %q, want %q", h.Config.Header, "Authorization")
	}
	if len(h.Config.ScopeRules) != 1 {
		t.Fatalf("len(ScopeRules) = %d, want 1", len(h.Config.ScopeRules))
	}
	if h.Config.ScopeRules[0].Path != "/admin/*" {
		t.Errorf("ScopeRules[0].Path = %q, want %q", h.Config.ScopeRules[0].Path, "/admin/*")
	}
}

// TestAPIKeyHandler_UnmarshalJSON_NestedConfig verifies that the nested config
// structure is also correctly decoded.
func TestAPIKeyHandler_UnmarshalJSON_NestedConfig(t *testing.T) {
	raw := `{
		"config": {
			"header": "X-Custom-Key",
			"scope_rules": []
		}
	}`

	h := &APIKeyHandler{}
	if err := json.Unmarshal([]byte(raw), h); err != nil {
		t.Fatalf("UnmarshalJSON error: %v", err)
	}
	if h.Config.Header != "X-Custom-Key" {
		t.Errorf("Config.Header = %q, want %q", h.Config.Header, "X-Custom-Key")
	}
}
