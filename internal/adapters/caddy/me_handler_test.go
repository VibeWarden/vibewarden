package caddy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// TestMeHandler_CaddyModule verifies the Caddy module metadata.
func TestMeHandler_CaddyModule(t *testing.T) {
	info := MeHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_me" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_me")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}

	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*MeHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *MeHandler", mod)
	}
}

// TestMeHandler_InterfaceGuards verifies the handler satisfies required Caddy interfaces.
func TestMeHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*MeHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*MeHandler)(nil)
	var _ gocaddy.Module = (*MeHandler)(nil)
}

// TestMeHandler_Provision verifies that Provision initialises the handler.
func TestMeHandler_Provision(t *testing.T) {
	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  "http://kratos:4433",
	}}

	err := h.Provision(gocaddy.Context{})
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	if h.logger == nil {
		t.Error("Provision() did not set logger")
	}
	if h.client == nil {
		t.Error("Provision() did not set HTTP client")
	}
}

// TestMeHandler_UnmarshalJSON verifies both flat and nested JSON unmarshalling.
func TestMeHandler_UnmarshalJSON(t *testing.T) {
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
			var h MeHandler
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

// TestMeHandler_ServeHTTP_NoCookie verifies 401 when no session cookie is present.
func TestMeHandler_ServeHTTP_NoCookie(t *testing.T) {
	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  "http://unreachable:4433",
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, nil)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	var resp meUnauthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if resp.Authenticated != false {
		t.Errorf("authenticated = %v, want false", resp.Authenticated)
	}

	assertCacheControl(t, w)
	assertContentTypeJSON(t, w)
}

// TestMeHandler_ServeHTTP_ValidSession verifies 200 with correct session info.
func TestMeHandler_ServeHTTP_ValidSession(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/whoami" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "user-123",
				"traits": {"email": "user@example.com", "role": "admin"},
				"verifiable_addresses": [
					{"value": "user@example.com", "via": "email", "verified": true}
				]
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  kratosServer.URL,
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, nil)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp meSuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if resp.ID != "user-123" {
		t.Errorf("id = %q, want %q", resp.ID, "user-123")
	}
	if resp.Email != "user@example.com" {
		t.Errorf("email = %q, want %q", resp.Email, "user@example.com")
	}
	if resp.Verified != true {
		t.Errorf("verified = %v, want true", resp.Verified)
	}
	if resp.Role != "admin" {
		t.Errorf("role = %q, want %q", resp.Role, "admin")
	}

	assertCacheControl(t, w)
	assertContentTypeJSON(t, w)
}

// TestMeHandler_ServeHTTP_InvalidSession verifies 401 when Kratos returns 401.
func TestMeHandler_ServeHTTP_InvalidSession(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer kratosServer.Close()

	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  kratosServer.URL,
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "expired-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, nil)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	var resp meUnauthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if resp.Authenticated != false {
		t.Errorf("authenticated = %v, want false", resp.Authenticated)
	}

	assertCacheControl(t, w)
}

// TestMeHandler_ServeHTTP_KratosUnavailable verifies 502 when Kratos is unreachable.
func TestMeHandler_ServeHTTP_KratosUnavailable(t *testing.T) {
	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  "http://127.0.0.1:19999", // nothing listening
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "some-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, nil)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}

	var resp meErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if resp.Error != "bad_gateway" {
		t.Errorf("error = %q, want %q", resp.Error, "bad_gateway")
	}
	if resp.Message != "identity provider unavailable" {
		t.Errorf("message = %q, want %q", resp.Message, "identity provider unavailable")
	}

	assertCacheControl(t, w)
}

// TestMeHandler_ServeHTTP_KratosServerError verifies 502 when Kratos returns 5xx.
func TestMeHandler_ServeHTTP_KratosServerError(t *testing.T) {
	statusCodes := []int{500, 502, 503}

	for _, code := range statusCodes {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer kratosServer.Close()

			h := &MeHandler{Config: MeHandlerConfig{
				CookieName: "ory_kratos_session",
				KratosURL:  kratosServer.URL,
			}}
			if err := h.Provision(gocaddy.Context{}); err != nil {
				t.Fatalf("Provision() error = %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
			req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "token"})
			w := httptest.NewRecorder()

			_ = h.ServeHTTP(w, req, nil)

			if w.Code != http.StatusBadGateway {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
			}

			assertCacheControl(t, w)
		})
	}
}

// TestMeHandler_ServeHTTP_InactiveSession verifies 401 when session is not active.
func TestMeHandler_ServeHTTP_InactiveSession(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": false,
			"identity": {"id": "user-123", "traits": {}}
		}`))
	}))
	defer kratosServer.Close()

	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  kratosServer.URL,
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "inactive-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, nil)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	assertCacheControl(t, w)
}

// TestMeHandler_ServeHTTP_DefaultRole verifies that missing role trait defaults to "user".
func TestMeHandler_ServeHTTP_DefaultRole(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "user-456",
				"traits": {"email": "noone@example.com"},
				"verifiable_addresses": [
					{"value": "noone@example.com", "via": "email", "verified": false}
				]
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  kratosServer.URL,
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, nil)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp meSuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if resp.Role != "user" {
		t.Errorf("role = %q, want %q", resp.Role, "user")
	}
	if resp.Verified != false {
		t.Errorf("verified = %v, want false", resp.Verified)
	}
}

// TestMeHandler_ServeHTTP_EmailFromTraits verifies email extraction from traits
// when verifiable_addresses is absent.
func TestMeHandler_ServeHTTP_EmailFromTraits(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "user-789",
				"traits": {"email": "traits@example.com"}
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  kratosServer.URL,
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, nil)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	var resp meSuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if resp.Email != "traits@example.com" {
		t.Errorf("email = %q, want %q", resp.Email, "traits@example.com")
	}
}

// TestMeHandler_ServeHTTP_DoesNotCallNext verifies that the handler terminates
// the request and does not call the next handler.
func TestMeHandler_ServeHTTP_DoesNotCallNext(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "user-123",
				"traits": {"email": "u@e.com"},
				"verifiable_addresses": [{"value": "u@e.com", "via": "email", "verified": true}]
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "ory_kratos_session",
		KratosURL:  kratosServer.URL,
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	_ = h.ServeHTTP(w, req, next)

	if nextCalled {
		t.Error("next handler should not be called — MeHandler terminates the request")
	}
}

// TestMeHandler_ServeHTTP_ForwardsCookieToKratos verifies the handler sends
// the correct cookie to the Kratos whoami endpoint.
func TestMeHandler_ServeHTTP_ForwardsCookieToKratos(t *testing.T) {
	var receivedCookie string
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {"id": "user-123", "traits": {"email": "u@e.com"}}
		}`))
	}))
	defer kratosServer.Close()

	h := &MeHandler{Config: MeHandlerConfig{
		CookieName: "my_session",
		KratosURL:  kratosServer.URL,
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/me", nil)
	req.AddCookie(&http.Cookie{Name: "my_session", Value: "token-abc"})
	w := httptest.NewRecorder()

	_ = h.ServeHTTP(w, req, nil)

	want := "my_session=token-abc"
	if receivedCookie != want {
		t.Errorf("Kratos received cookie = %q, want %q", receivedCookie, want)
	}
}

// assertCacheControl checks that Cache-Control: no-store is set.
func assertCacheControl(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	cc := w.Header().Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

// assertContentTypeJSON checks that Content-Type is application/json.
func assertContentTypeJSON(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}
