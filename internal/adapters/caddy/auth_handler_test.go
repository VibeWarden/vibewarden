package caddy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/domain/identity"
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

// TestAuthHandler_PublicPath_WithValidSession_SetsRoleHeader verifies that a
// public path with a valid session also sets the X-User-Role header.
//
// Regression test: the public path code path called setIdentityHeaders but not
// extractRole, so X-User-Role was missing on authenticated public paths.
func TestAuthHandler_PublicPath_WithValidSession_SetsRoleHeader(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "admin-1",
				"traits": {"email": "admin@example.com", "role": "admin"},
				"verifiable_addresses": [
					{"value": "admin@example.com", "via": "email", "verified": true}
				]
			}
		}`))
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
	if got := capturedHeaders.Get("X-User-Role"); got != "admin" {
		t.Errorf("X-User-Role = %q, want %q", got, "admin")
	}
}

// TestAuthHandler_RoleHeader_Set verifies that the X-User-Role header is set
// from the Kratos identity traits.role field.
func TestAuthHandler_RoleHeader_Set(t *testing.T) {
	tests := []struct {
		name     string
		traits   string
		wantRole string
	}{
		{
			name:     "admin role",
			traits:   `{"email":"u@e.com","role":"admin"}`,
			wantRole: "admin",
		},
		{
			name:     "moderator role",
			traits:   `{"email":"u@e.com","role":"moderator"}`,
			wantRole: "moderator",
		},
		{
			name:     "user role explicit",
			traits:   `{"email":"u@e.com","role":"user"}`,
			wantRole: "user",
		},
		{
			name:     "missing role defaults to user",
			traits:   `{"email":"u@e.com"}`,
			wantRole: "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{
					"active": true,
					"identity": {
						"id": "user-123",
						"traits": %s,
						"verifiable_addresses": [
							{"value": "u@e.com", "via": "email", "verified": true}
						]
					}
				}`, tt.traits)
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
			if got := capturedHeaders.Get("X-User-Role"); got != tt.wantRole {
				t.Errorf("X-User-Role = %q, want %q", got, tt.wantRole)
			}
		})
	}
}

// TestAuthHandler_RolePath_Allowed verifies that a user with the required role
// can access a role-restricted path.
func TestAuthHandler_RolePath_Allowed(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "admin-1",
				"traits": {"email": "admin@e.com", "role": "admin"},
				"verifiable_addresses": [
					{"value": "admin@e.com", "via": "email", "verified": true}
				]
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
		KratosURL:  kratosServer.URL,
		RolePaths: map[string][]string{
			"admin": {"/admin/*"},
		},
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

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	if !nextCalled {
		t.Error("expected next handler to be called for admin user on /admin/*")
	}
}

// TestAuthHandler_RolePath_Denied verifies that a user without the required role
// receives HTTP 403 with the correct JSON body.
func TestAuthHandler_RolePath_Denied(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "user-1",
				"traits": {"email": "user@e.com", "role": "user"},
				"verifiable_addresses": [
					{"value": "user@e.com", "via": "email", "verified": true}
				]
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
		KratosURL:  kratosServer.URL,
		RolePaths: map[string][]string{
			"admin": {"/admin/*"},
		},
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	if nextCalled {
		t.Error("next handler should not be called for user role on /admin/*")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	wantBody := `{"error":"forbidden","message":"insufficient role for this path"}`
	if got := w.Body.String(); got != wantBody {
		t.Errorf("body = %q, want %q", got, wantBody)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

// TestAuthHandler_RolePath_PublicPath_Skipped verifies that public paths bypass
// role enforcement entirely.
func TestAuthHandler_RolePath_PublicPath_Skipped(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName:  "ory_kratos_session",
		LoginURL:    "/login",
		PublicPaths: []string{"/admin/public/*"},
		KratosURL:   "http://unreachable:4433",
		RolePaths: map[string][]string{
			"admin": {"/admin/*"},
		},
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

	// /admin/public/info should bypass auth (and therefore role checks)
	req := httptest.NewRequest(http.MethodGet, "/admin/public/info", nil)
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	if !nextCalled {
		t.Error("expected next handler to be called for public path")
	}
}

// TestAuthHandler_NoRolePaths_HeaderOnly verifies that when no role_paths are
// configured, the X-User-Role header is still set but no role enforcement occurs.
func TestAuthHandler_NoRolePaths_HeaderOnly(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "user-1",
				"traits": {"email": "user@e.com", "role": "user"},
				"verifiable_addresses": [
					{"value": "user@e.com", "via": "email", "verified": true}
				]
			}
		}`))
	}))
	defer kratosServer.Close()

	h := &AuthHandler{Config: AuthHandlerConfig{
		CookieName: "ory_kratos_session",
		LoginURL:   "/login",
		KratosURL:  kratosServer.URL,
		// No RolePaths configured
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

	// Request to /admin/* should pass through since no role_paths are configured
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if capturedHeaders == nil {
		t.Fatal("next handler was not called")
	}
	if got := capturedHeaders.Get("X-User-Role"); got != "user" {
		t.Errorf("X-User-Role = %q, want %q", got, "user")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (no role enforcement expected)", w.Code, http.StatusOK)
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

// discardLogger returns a logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

// ---------------------------------------------------------------------------
// Direct unit tests for extractRole
// ---------------------------------------------------------------------------

// TestExtractRole verifies the extractRole function in isolation.
func TestExtractRole(t *testing.T) {
	tests := []struct {
		name     string
		traits   map[string]any
		wantRole string
	}{
		{
			name:     "valid admin role",
			traits:   map[string]any{"email": "u@e.com", "role": "admin"},
			wantRole: "admin",
		},
		{
			name:     "valid moderator role",
			traits:   map[string]any{"email": "u@e.com", "role": "moderator"},
			wantRole: "moderator",
		},
		{
			name:     "valid user role",
			traits:   map[string]any{"email": "u@e.com", "role": "user"},
			wantRole: "user",
		},
		{
			name:     "missing role defaults to user",
			traits:   map[string]any{"email": "u@e.com"},
			wantRole: "user",
		},
		{
			name:     "empty string role defaults to user",
			traits:   map[string]any{"email": "u@e.com", "role": ""},
			wantRole: "user",
		},
		{
			name:     "non-string role defaults to user",
			traits:   map[string]any{"email": "u@e.com", "role": 42},
			wantRole: "user",
		},
		{
			name:     "invalid role value defaults to user and logs warning",
			traits:   map[string]any{"email": "u@e.com", "role": "superadmin"},
			wantRole: "user",
		},
		{
			name:     "nil traits defaults to user",
			traits:   nil,
			wantRole: "user",
		},
	}

	logger := discardLogger()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			whoami := &kratosWhoamiResponse{}
			whoami.Identity.Traits = tt.traits
			got := extractRole(whoami, logger)
			if got.String() != tt.wantRole {
				t.Errorf("extractRole() = %q, want %q", got.String(), tt.wantRole)
			}
		})
	}
}

// TestExtractRole_InvalidValue_ReturnsDefaultRole verifies that an unrecognised
// role value is rejected and the default role is returned.
func TestExtractRole_InvalidValue_ReturnsDefaultRole(t *testing.T) {
	logger := discardLogger()
	whoami := &kratosWhoamiResponse{}
	whoami.Identity.Traits = map[string]any{"role": "superadmin"}
	whoami.Identity.ID = "user-123"

	got := extractRole(whoami, logger)
	if got != identity.DefaultRole() {
		t.Errorf("extractRole() = %q for invalid role, want default %q", got.String(), identity.DefaultRole().String())
	}
}

// ---------------------------------------------------------------------------
// Direct unit tests for matchRequiredRole
// ---------------------------------------------------------------------------

// TestMatchRequiredRole verifies the matchRequiredRole function in isolation.
func TestMatchRequiredRole(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		RolePaths: map[string][]string{
			"admin":     {"/admin/*"},
			"moderator": {"/mod/queue", "/mod/reports/*"},
		},
	}}

	tests := []struct {
		name     string
		reqPath  string
		wantRole string
		wantOK   bool
	}{
		{
			name:     "admin prefix match",
			reqPath:  "/admin/dashboard",
			wantRole: "admin",
			wantOK:   true,
		},
		{
			name:     "admin nested path",
			reqPath:  "/admin/users/edit",
			wantRole: "admin",
			wantOK:   true,
		},
		{
			name:     "moderator exact match",
			reqPath:  "/mod/queue",
			wantRole: "moderator",
			wantOK:   true,
		},
		{
			name:     "moderator prefix match",
			reqPath:  "/mod/reports/123",
			wantRole: "moderator",
			wantOK:   true,
		},
		{
			name:    "unmatched path",
			reqPath: "/public/info",
			wantOK:  false,
		},
		{
			name:    "root path",
			reqPath: "/",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRole, gotOK := h.matchRequiredRole(tt.reqPath)
			if gotOK != tt.wantOK {
				t.Errorf("matchRequiredRole(%q) ok = %v, want %v", tt.reqPath, gotOK, tt.wantOK)
			}
			if gotOK && gotRole != tt.wantRole {
				t.Errorf("matchRequiredRole(%q) role = %q, want %q", tt.reqPath, gotRole, tt.wantRole)
			}
		})
	}
}

// TestMatchRequiredRole_PrefixBoundary verifies that /admin/* does not match
// /administrator or /admins — only paths under /admin/.
//
// Regression test: TrimSuffix(p, "/*") produced "/admin" which matched
// "/administrator". Fixed to TrimSuffix(p, "*") to preserve the trailing slash.
func TestMatchRequiredRole_PrefixBoundary(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		RolePaths: map[string][]string{
			"admin": {"/admin/*"},
		},
	}}

	tests := []struct {
		path   string
		wantOK bool
	}{
		{"/admin/dashboard", true},
		{"/admin/users/edit", true},
		{"/administrator", false},
		{"/admins/list", false},
		{"/admin-panel", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			_, ok := h.matchRequiredRole(tt.path)
			if ok != tt.wantOK {
				t.Errorf("matchRequiredRole(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
			}
		})
	}
}

// TestMatchRequiredRole_NoRolePaths verifies that when RolePaths is empty,
// no role is required for any path.
func TestMatchRequiredRole_NoRolePaths(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{}}

	_, ok := h.matchRequiredRole("/admin/dashboard")
	if ok {
		t.Error("matchRequiredRole() ok = true with empty RolePaths, want false")
	}
}

// ---------------------------------------------------------------------------
// Security regression tests — #1264
// ---------------------------------------------------------------------------

// TestAuthHandler_isPublicPath_PrefixBoundaryBypass verifies that a URL-space
// sibling (e.g. "/auth-evil") is NOT matched by a wildcard public-path rule
// (e.g. "/auth/*"). Fixes the bare-prefix check that treated strings.HasPrefix
// as a path match.
func TestAuthHandler_isPublicPath_PrefixBoundaryBypass(t *testing.T) {
	h := &AuthHandler{Config: AuthHandlerConfig{
		PublicPaths: []string{"/auth/*", "/public/*"},
	}}

	tests := []struct {
		path    string
		want    bool
		comment string
	}{
		{"/auth/login", true, "sub-path of /auth/* must match"},
		{"/auth/", true, "trailing slash of /auth/* must match"},
		{"/auth", true, "exact prefix /auth must match"},
		{"/auth-evil", false, "sibling prefix /auth-evil must NOT match /auth/*"},
		{"/authentic", false, "sibling prefix /authentic must NOT match /auth/*"},
		{"/public/page", true, "sub-path of /public/* must match"},
		{"/public-secret", false, "sibling /public-secret must NOT match /public/*"},
		{"/public-secret/admin", false, "deep sibling must NOT match /public/*"},
		{"/other", false, "unrelated path must not match"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := h.isPublicPath(tt.path)
			if got != tt.want {
				t.Errorf("isPublicPath(%q) = %v, want %v — %s", tt.path, got, tt.want, tt.comment)
			}
		})
	}
}

// TestStripXUserHeaders verifies that stripXUserHeaders removes every header
// whose canonical name begins with "X-User-" and does not touch other headers.
func TestStripXUserHeaders(t *testing.T) {
	tests := []struct {
		name       string
		incoming   map[string]string
		wantAbsent []string
		wantKept   []string
	}{
		{
			name: "strips all X-User-* variants",
			incoming: map[string]string{
				"X-User-Id":       "usr-123",
				"X-User-Email":    "evil@example.com",
				"X-User-Verified": "true",
				"X-User-Role":     "admin",
				"X-User-Name":     "injected-admin",
			},
			wantAbsent: []string{
				"X-User-Id", "X-User-Email", "X-User-Verified", "X-User-Role", "X-User-Name",
			},
		},
		{
			name: "does not strip unrelated headers",
			incoming: map[string]string{
				"X-User-Id":      "usr-456",
				"Authorization":  "Bearer tok",
				"X-Request-Id":   "req-1",
				"Content-Length": "0",
			},
			wantAbsent: []string{"X-User-Id"},
			wantKept:   []string{"Authorization", "X-Request-Id", "Content-Length"},
		},
		{
			name:       "no X-User-* headers — no-op",
			incoming:   map[string]string{"Accept": "application/json"},
			wantAbsent: nil,
			wantKept:   []string{"Accept"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tt.incoming {
				req.Header.Set(k, v)
			}

			stripXUserHeaders(req)

			for _, absent := range tt.wantAbsent {
				if got := req.Header.Get(absent); got != "" {
					t.Errorf("header %q = %q after strip, want empty", absent, got)
				}
			}
			for _, kept := range tt.wantKept {
				if got := req.Header.Get(kept); got == "" {
					t.Errorf("header %q was stripped but should not have been", kept)
				}
			}
		})
	}
}

// TestAuthHandler_ServeHTTP_StripsForggedXUserHeaders verifies that forged
// X-User-* headers sent by a client do not reach the upstream application,
// even for the X-User-Name header that was not in the original strip list.
//
// The fake Kratos returns verified=false and role="user" so that forged
// values ("true", "admin") are distinguishable from the real handler-set values.
//
// Regression test for #1264 (Part A).
func TestAuthHandler_ServeHTTP_StripsForggedXUserHeaders(t *testing.T) {
	// Fake Kratos that returns a valid session with verified=false and no role.
	// This makes forged "true"/"admin" values distinguishable from the real
	// handler-set "false"/"user" values.
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"active": true,
			"identity": {
				"id": "real-user-id",
				"traits": {"email": "real@example.com"},
				"verifiable_addresses": [
					{"value": "real@example.com", "via": "email", "verified": false}
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

	// forgedHeaders maps each header name to a forged value that differs from
	// what the handler legitimately sets:
	//   X-User-Id       real="real-user-id"  forged="forged-id"
	//   X-User-Email    real="real@example.com" forged="forged@evil.com"
	//   X-User-Verified real="false"          forged="true"
	//   X-User-Role     real="user"           forged="admin"
	//   X-User-Name     real=(not set)        forged="injected-admin"
	forgedHeaders := []struct {
		name  string
		value string
	}{
		{"X-User-Id", "forged-id"},
		{"X-User-Email", "forged@evil.com"},
		{"X-User-Verified", "true"},
		{"X-User-Role", "admin"},
		{"X-User-Name", "injected-admin"},
	}

	for _, fh := range forgedHeaders {
		t.Run("forged "+fh.name+" stripped", func(t *testing.T) {
			var capturedHeaders http.Header
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				capturedHeaders = r.Header.Clone()
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
			// Inject the forged header — simulates a client bypass attempt.
			req.Header.Set(fh.name, fh.value)
			w := httptest.NewRecorder()

			if err := h.ServeHTTP(w, req, next); err != nil {
				t.Fatalf("ServeHTTP() error = %v", err)
			}
			if capturedHeaders == nil {
				t.Fatal("next handler was not called")
			}
			// The value reaching upstream must be the real value set by the
			// handler (or absent), never the forged client value.
			got := capturedHeaders.Get(fh.name)
			if got == fh.value {
				t.Errorf("header %q = %q (forged value) reached upstream; expected it to be stripped and overwritten", fh.name, fh.value)
			}
		})
	}
}
