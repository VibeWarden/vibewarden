package caddy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// TestIdentityHeadersHandler_CaddyModule verifies the Caddy module metadata.
func TestIdentityHeadersHandler_CaddyModule(t *testing.T) {
	info := IdentityHeadersHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_identity_headers" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_identity_headers")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}

	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*IdentityHeadersHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *IdentityHeadersHandler", mod)
	}
}

// TestIdentityHeadersHandler_InterfaceGuards verifies the handler satisfies required Caddy interfaces.
func TestIdentityHeadersHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*IdentityHeadersHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*IdentityHeadersHandler)(nil)
	var _ gocaddy.Module = (*IdentityHeadersHandler)(nil)
}

// TestIdentityHeadersHandler_Provision verifies that Provision succeeds.
func TestIdentityHeadersHandler_Provision(t *testing.T) {
	h := &IdentityHeadersHandler{Config: IdentityHeadersHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  "http://kratos:4433",
	}}

	err := h.Provision(gocaddy.Context{})
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
}

// TestIdentityHeadersHandler_UnmarshalJSON verifies both flat and nested JSON unmarshalling.
func TestIdentityHeadersHandler_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantCookieName string
		wantKratosURL  string
		wantErr        bool
	}{
		{
			name:           "flat structure",
			input:          `{"cookie_name":"sess","kratos_url":"http://k:4433"}`,
			wantCookieName: "sess",
			wantKratosURL:  "http://k:4433",
		},
		{
			name:           "nested config",
			input:          `{"config":{"cookie_name":"sess","kratos_url":"http://k:4433"}}`,
			wantCookieName: "sess",
			wantKratosURL:  "http://k:4433",
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var h IdentityHeadersHandler
			err := json.Unmarshal([]byte(tt.input), &h)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if h.Config.CookieName != tt.wantCookieName {
				t.Errorf("CookieName = %q, want %q", h.Config.CookieName, tt.wantCookieName)
			}
			if h.Config.KratosURL != tt.wantKratosURL {
				t.Errorf("KratosURL = %q, want %q", h.Config.KratosURL, tt.wantKratosURL)
			}
		})
	}
}

// TestIdentityHeadersHandler_ServeHTTP_PassThrough verifies the handler passes
// through to the next handler without modifying the request.
func TestIdentityHeadersHandler_ServeHTTP_PassThrough(t *testing.T) {
	h := &IdentityHeadersHandler{Config: IdentityHeadersHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  "http://kratos:4433",
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Errorf("ServeHTTP() error = %v", err)
	}
	if !nextCalled {
		t.Error("expected next handler to be called")
	}
}

// TestIdentityHeadersHandler_ServeHTTP_PreservesExistingHeaders verifies the
// handler does not modify identity headers already set by the auth handler.
func TestIdentityHeadersHandler_ServeHTTP_PreservesExistingHeaders(t *testing.T) {
	h := &IdentityHeadersHandler{Config: IdentityHeadersHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  "http://kratos:4433",
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	var capturedHeaders http.Header
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	// Simulate headers already set by the auth handler.
	req.Header.Set("X-User-Id", "user-123")
	req.Header.Set("X-User-Email", "user@example.com")
	req.Header.Set("X-User-Verified", "true")
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if capturedHeaders == nil {
		t.Fatal("next handler was not called")
	}
	if got := capturedHeaders.Get("X-User-Id"); got != "user-123" {
		t.Errorf("X-User-Id = %q, want %q", got, "user-123")
	}
	if got := capturedHeaders.Get("X-User-Email"); got != "user@example.com" {
		t.Errorf("X-User-Email = %q, want %q", got, "user@example.com")
	}
	if got := capturedHeaders.Get("X-User-Verified"); got != "true" {
		t.Errorf("X-User-Verified = %q, want %q", got, "true")
	}
}
