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

// TestAuthHandler_CaddyModule verifies the Caddy module metadata.
func TestAuthHandler_CaddyModule(t *testing.T) {
	info := AuthHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_authentication" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_authentication")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}

	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*AuthHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *AuthHandler", mod)
	}
}

// TestAuthHandler_InterfaceGuards verifies the handler satisfies required Caddy interfaces.
func TestAuthHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*AuthHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*AuthHandler)(nil)
	var _ gocaddy.Module = (*AuthHandler)(nil)
}

// TestAuthHandler_Provision verifies that Provision initialises the handler.
func TestAuthHandler_Provision(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
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

// TestAuthHandler_UnmarshalJSON verifies both flat and nested JSON unmarshalling.
func TestAuthHandler_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantCookieName string
		wantLoginURL   string
		wantKratosURL  string
		wantErr        bool
	}{
		{
			name:           "flat structure",
			input:          `{"cookie_name":"sess","login_url":"/login","kratos_url":"http://k:4433","public_paths":["/pub"]}`,
			wantCookieName: "sess",
			wantLoginURL:   "/login",
			wantKratosURL:  "http://k:4433",
		},
		{
			name:           "nested config",
			input:          `{"config":{"cookie_name":"sess","login_url":"/login","kratos_url":"http://k:4433"}}`,
			wantCookieName: "sess",
			wantLoginURL:   "/login",
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
			var h AuthHandler
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
			if h.Config.LoginURL != tt.wantLoginURL {
				t.Errorf("LoginURL = %q, want %q", h.Config.LoginURL, tt.wantLoginURL)
			}
			if h.Config.KratosURL != tt.wantKratosURL {
				t.Errorf("KratosURL = %q, want %q", h.Config.KratosURL, tt.wantKratosURL)
			}
		})
	}
}

// TestAuthHandler_isPublicPath verifies public path matching.
func TestAuthHandler_isPublicPath(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		PublicPaths: []string{
			"/_vibewarden/*",
			"/self-service/*",
			"/health",
			"/api/docs",
		},
	}}

	tests := []struct {
		path string
		want bool
	}{
		{"/_vibewarden/health", true},
		{"/_vibewarden/admin/users", true},
		{"/self-service/login/browser", true},
		{"/health", true},
		{"/api/docs", true},
		{"/app/page", false},
		{"/", false},
		{"/api/v1/users", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := h.isPublicPath(tt.path)
			if got != tt.want {
				t.Errorf("isPublicPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestAuthHandler_ServeHTTP_PublicPathBypass verifies that public paths skip authentication.
func TestAuthHandler_ServeHTTP_PublicPathBypass(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName:  "ory_kratos_session",
		LoginURL:    "/login",
		PublicPaths: []string{"/_vibewarden/*", "/health"},
		KratosURL:   "http://unreachable:4433", // should not be called
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	tests := []struct {
		path string
	}{
		{"/_vibewarden/health"},
		{"/health"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			nextCalled := false
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			err := h.ServeHTTP(w, req, next)
			if err != nil {
				t.Errorf("ServeHTTP() error = %v", err)
			}
			if !nextCalled {
				t.Error("expected next handler to be called for public path")
			}
		})
	}
}

// TestAuthHandler_ServeHTTP_NoCookie verifies redirect when no session cookie is present.
func TestAuthHandler_ServeHTTP_NoCookie(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/self-service/login/browser",
		KratosURL:  "http://unreachable:4433",
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	if nextCalled {
		t.Error("next handler should not be called without session cookie")
	}
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/self-service/login/browser" {
		t.Errorf("Location = %q, want %q", loc, "/self-service/login/browser")
	}
}

// TestAuthHandler_ServeHTTP_ValidSession verifies successful authentication
// with identity headers set.
func TestAuthHandler_ServeHTTP_ValidSession(t *testing.T) {
	// Start a fake Kratos server.
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
				"traits": {"email": "user@example.com"},
				"verifiable_addresses": [
					{"value": "user@example.com", "via": "email", "verified": true}
				]
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
		KratosURL:  kratosServer.URL,
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

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
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

// TestAuthHandler_ServeHTTP_InvalidSession verifies redirect when Kratos
// returns 401.
func TestAuthHandler_ServeHTTP_InvalidSession(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer kratosServer.Close()

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
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

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "expired-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	if nextCalled {
		t.Error("next handler should not be called with invalid session")
	}
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
}

// TestAuthHandler_ServeHTTP_KratosUnavailable verifies redirect when Kratos is unreachable.
func TestAuthHandler_ServeHTTP_KratosUnavailable(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
		KratosURL:  "http://127.0.0.1:19999", // nothing listening
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "some-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	if nextCalled {
		t.Error("next handler should not be called when Kratos is unavailable")
	}
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
}

// TestAuthHandler_ServeHTTP_InactiveSession verifies redirect when Kratos
// returns an inactive session.
func TestAuthHandler_ServeHTTP_InactiveSession(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": false,
			"identity": {"id": "user-123", "traits": {}}
		}`))
	}))
	defer kratosServer.Close()

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
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

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "inactive-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	if nextCalled {
		t.Error("next handler should not be called with inactive session")
	}
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
}

// TestAuthHandler_ServeHTTP_UnverifiedEmail verifies that X-User-Verified is
// "false" when the email is not verified.
func TestAuthHandler_ServeHTTP_UnverifiedEmail(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "user-456",
				"traits": {"email": "new@example.com"},
				"verifiable_addresses": [
					{"value": "new@example.com", "via": "email", "verified": false}
				]
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
		KratosURL:  kratosServer.URL,
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

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if capturedHeaders == nil {
		t.Fatal("next handler was not called")
	}
	if got := capturedHeaders.Get("X-User-Verified"); got != "false" {
		t.Errorf("X-User-Verified = %q, want %q", got, "false")
	}
}

// TestAuthHandler_ServeHTTP_EmailFromTraits verifies that the email is
// extracted from traits when verifiable_addresses is absent.
func TestAuthHandler_ServeHTTP_EmailFromTraits(t *testing.T) {
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

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
		KratosURL:  kratosServer.URL,
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

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if capturedHeaders == nil {
		t.Fatal("next handler was not called")
	}
	if got := capturedHeaders.Get("X-User-Email"); got != "traits@example.com" {
		t.Errorf("X-User-Email = %q, want %q", got, "traits@example.com")
	}
}

// TestAuthHandler_ServeHTTP_ForwardsCookieToKratos verifies the handler sends
// the correct cookie to the Kratos whoami endpoint.
func TestAuthHandler_ServeHTTP_ForwardsCookieToKratos(t *testing.T) {
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

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "my_session",
		LoginURL:   "/login",
		KratosURL:  kratosServer.URL,
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "my_session", Value: "token-abc"})
	w := httptest.NewRecorder()

	_ = h.ServeHTTP(w, req, next)

	want := "my_session=token-abc"
	if receivedCookie != want {
		t.Errorf("Kratos received cookie = %q, want %q", receivedCookie, want)
	}
}

// TestAuthHandler_Provision_AppendKratosPublicPaths verifies that Provision
// automatically appends kratosDefaultPublicPaths when KratosURL is set, and
// does not duplicate paths that are already present.
//
// Regression test for #977 and #978.
func TestAuthHandler_Provision_AppendKratosPublicPaths(t *testing.T) {
	tests := []struct {
		name       string
		kratosURL  string
		initial    []string
		wantPaths  []string
		wantAbsent []string
	}{
		{
			name:      "appends all default paths when none present",
			kratosURL: "http://kratos:4433",
			initial:   []string{"/_vibewarden/*"},
			wantPaths: []string{
				"/_vibewarden/*",
				"/auth/*",
				"/self-service/*",
				"/login",
				"/registration",
				"/recovery",
				"/verification",
				"/error",
				"/sessions/whoami",
			},
		},
		{
			name:      "does not duplicate existing paths",
			kratosURL: "http://kratos:4433",
			initial:   []string{"/auth/*", "/self-service/*"},
			wantPaths: []string{
				"/auth/*",
				"/self-service/*",
				"/login",
				"/registration",
				"/recovery",
				"/verification",
				"/error",
				"/sessions/whoami",
			},
		},
		{
			name:       "no append when KratosURL is empty",
			kratosURL:  "",
			initial:    []string{"/_vibewarden/*"},
			wantPaths:  []string{"/_vibewarden/*"},
			wantAbsent: []string{"/auth/*", "/self-service/*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &AuthHandler{Config: AuthHandlerConfig{
				CookieName:  "ory_kratos_session",
				LoginURL:    "/login",
				KratosURL:   tt.kratosURL,
				PublicPaths: tt.initial,
			}}

			err := h.Provision(gocaddy.Context{})
			if err != nil {
				t.Fatalf("Provision() error = %v", err)
			}

			pathSet := make(map[string]bool)
			for _, p := range h.Config.PublicPaths {
				pathSet[p] = true
			}

			for _, want := range tt.wantPaths {
				if !pathSet[want] {
					t.Errorf("PublicPaths missing %q, got %v", want, h.Config.PublicPaths)
				}
			}

			for _, absent := range tt.wantAbsent {
				if pathSet[absent] {
					t.Errorf("PublicPaths should not contain %q, got %v", absent, h.Config.PublicPaths)
				}
			}
		})
	}
}

// TestAuthHandler_Provision_KratosPathsBypass verifies that after Provision
// the automatically-added Kratos paths bypass authentication.
//
// Regression test for #978.
func TestAuthHandler_Provision_KratosPathsBypass(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
		KratosURL:  "http://kratos:4433",
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	bypassPaths := []string{
		"/auth/login",
		"/auth/registration",
		"/self-service/login/browser",
		"/self-service/registration/flows",
		"/login",
		"/registration",
		"/recovery",
		"/verification",
		"/error",
		"/sessions/whoami",
	}

	for _, p := range bypassPaths {
		t.Run(p, func(t *testing.T) {
			if !h.isPublicPath(p) {
				t.Errorf("isPublicPath(%q) = false after Provision(), want true", p)
			}
		})
	}
}

// TestAuthHandler_PublicPath_WithValidSession_SetsHeaders verifies that a
// public path with a valid Kratos session cookie sets identity headers on
// the request without blocking or redirecting.
func TestAuthHandler_PublicPath_WithValidSession_SetsHeaders(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/whoami" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "user-pub-123",
				"traits": {"email": "pub@example.com"},
				"verifiable_addresses": [
					{"value": "pub@example.com", "via": "email", "verified": true}
				]
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName:  "ory_kratos_session",
		LoginURL:    "/login",
		PublicPaths: []string{"/public", "/home"},
		KratosURL:   kratosServer.URL,
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

	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if capturedHeaders == nil {
		t.Fatal("next handler was not called")
	}
	if w.Code == http.StatusFound {
		t.Error("public path should not redirect even with session check")
	}
	if got := capturedHeaders.Get("X-User-Id"); got != "user-pub-123" {
		t.Errorf("X-User-Id = %q, want %q", got, "user-pub-123")
	}
	if got := capturedHeaders.Get("X-User-Email"); got != "pub@example.com" {
		t.Errorf("X-User-Email = %q, want %q", got, "pub@example.com")
	}
	if got := capturedHeaders.Get("X-User-Verified"); got != "true" {
		t.Errorf("X-User-Verified = %q, want %q", got, "true")
	}
}

// TestAuthHandler_PublicPath_WithInvalidSession_NoHeaders verifies that a
// public path with an expired/invalid session cookie still passes through
// without setting identity headers and without redirecting.
func TestAuthHandler_PublicPath_WithInvalidSession_NoHeaders(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer kratosServer.Close()

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName:  "ory_kratos_session",
		LoginURL:    "/login",
		PublicPaths: []string{"/public"},
		KratosURL:   kratosServer.URL,
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	var capturedHeaders http.Header
	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "expired-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if !nextCalled {
		t.Error("next handler should be called for public path even with invalid session")
	}
	if w.Code == http.StatusFound {
		t.Error("public path should not redirect even with invalid session")
	}
	if got := capturedHeaders.Get("X-User-Id"); got != "" {
		t.Errorf("X-User-Id = %q, want empty", got)
	}
	if got := capturedHeaders.Get("X-User-Email"); got != "" {
		t.Errorf("X-User-Email = %q, want empty", got)
	}
	if got := capturedHeaders.Get("X-User-Verified"); got != "" {
		t.Errorf("X-User-Verified = %q, want empty", got)
	}
}

// TestAuthHandler_PublicPath_NoCookie_NoHeaders verifies that a public path
// without any session cookie passes through without identity headers and
// without redirecting.
func TestAuthHandler_PublicPath_NoCookie_NoHeaders(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName:  "ory_kratos_session",
		LoginURL:    "/login",
		PublicPaths: []string{"/public"},
		KratosURL:   "http://unreachable:4433", // should not be called
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	var capturedHeaders http.Header
	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	// No cookie added.
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if !nextCalled {
		t.Error("next handler should be called for public path without cookie")
	}
	if w.Code == http.StatusFound {
		t.Error("public path should not redirect without cookie")
	}
	if got := capturedHeaders.Get("X-User-Id"); got != "" {
		t.Errorf("X-User-Id = %q, want empty", got)
	}
	if got := capturedHeaders.Get("X-User-Email"); got != "" {
		t.Errorf("X-User-Email = %q, want empty", got)
	}
	if got := capturedHeaders.Get("X-User-Verified"); got != "" {
		t.Errorf("X-User-Verified = %q, want empty", got)
	}
}

// TestAuthHandler_ServeHTTP_KratosServerError verifies redirect when Kratos
// returns a 5xx error.
func TestAuthHandler_ServeHTTP_KratosServerError(t *testing.T) {
	statusCodes := []int{500, 502, 503}

	for _, code := range statusCodes {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer kratosServer.Close()

			h := &AuthHandler{Config: AuthHandlerConfig{
				CookieName: "ory_kratos_session",
				LoginURL:   "/login",
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

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "token"})
			w := httptest.NewRecorder()

			_ = h.ServeHTTP(w, req, next)

			if nextCalled {
				t.Error("next handler should not be called on Kratos server error")
			}
			if w.Code != http.StatusFound {
				t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
			}
		})
	}
}
