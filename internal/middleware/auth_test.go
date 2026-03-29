package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/identity"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeIdentityProvider is a simple in-memory fake that implements
// ports.IdentityProvider without any mocking framework.
type fakeIdentityProvider struct {
	// result is returned on every Authenticate call when err is nil.
	result identity.AuthResult
}

func (f *fakeIdentityProvider) Name() string { return "fake" }

func (f *fakeIdentityProvider) Authenticate(_ context.Context, _ *http.Request) identity.AuthResult {
	return f.result
}

// fakeIdentityProviderWithCookie returns different results based on whether the
// configured cookie is present in the request.
type fakeIdentityProviderWithCookie struct {
	cookieName   string
	validCookies map[string]identity.Identity // cookie value → identity
	fallbackErr  identity.AuthResult          // returned when cookie absent
}

func (f *fakeIdentityProviderWithCookie) Name() string { return "fake-cookie" }

func (f *fakeIdentityProviderWithCookie) Authenticate(_ context.Context, r *http.Request) identity.AuthResult {
	cookie, err := r.Cookie(f.cookieName)
	if err != nil {
		if f.fallbackErr.Reason != "" {
			return f.fallbackErr
		}
		return identity.Failure("no_credentials", "no session cookie")
	}
	ident, ok := f.validCookies[cookie.Value]
	if !ok {
		return identity.Failure("session_not_found", "session does not exist")
	}
	return identity.Success(ident)
}

// validIdentity returns a non-zero identity for use in tests.
func validIdentity() identity.Identity {
	ident, _ := identity.NewIdentity("user-123", "alice@example.com", "kratos", true, nil)
	return ident
}

// newTestLogger returns a no-op slog.Logger suitable for tests.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

// noopWriter discards all output.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestAuthMiddleware_UnauthenticatedRequest(t *testing.T) {
	provider := &fakeIdentityProvider{
		result: identity.Failure("no_credentials", "no session cookie"),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect)", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/login" {
		t.Errorf("Location = %q, want %q", loc, "/login")
	}
}

func TestAuthMiddleware_AuthenticatedRequest(t *testing.T) {
	ident := validIdentity()
	provider := &fakeIdentityProviderWithCookie{
		cookieName: "ory_kratos_session",
		validCookies: map[string]identity.Identity{
			"valid-token": ident,
		},
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	nextCalled := false
	var nextCtx context.Context

	mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		nextCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !nextCalled {
		t.Fatal("next handler was not called for authenticated request")
	}

	gotIdent, ok := IdentityFromContext(nextCtx)
	if !ok {
		t.Fatal("identity not stored in context")
	}
	if gotIdent.ID() != ident.ID() {
		t.Errorf("context identity ID = %q, want %q", gotIdent.ID(), ident.ID())
	}
}

func TestAuthMiddleware_PublicPathBypass(t *testing.T) {
	// Provider always returns failure — public paths must bypass auth entirely.
	provider := &fakeIdentityProvider{
		result: identity.Failure("no_credentials", "no session cookie"),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
		PublicPaths:       []string{"/health", "/static/*"},
	}

	tests := []struct {
		name string
		path string
	}{
		{"exact public path", "/health"},
		{"wildcard public path", "/static/app.js"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			// No cookie — but should not trigger redirect.
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			if !nextCalled {
				t.Errorf("next handler not called for public path %q", tt.path)
			}
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for public path %q", w.Code, http.StatusOK, tt.path)
			}
		})
	}
}

func TestAuthMiddleware_GlobPatternMatching(t *testing.T) {
	provider := &fakeIdentityProvider{
		result: identity.Failure("no_credentials", "no session cookie"),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
		PublicPaths:       []string{"/api/v1/*/public"},
	}

	tests := []struct {
		name       string
		path       string
		wantPublic bool
	}{
		{"matched glob segment", "/api/v1/users/public", true},
		{"matched glob segment resources", "/api/v1/items/public", true},
		{"non-matching path", "/api/v1/users/private", false},
		{"too many segments not matched", "/api/v1/users/extra/public", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			if tt.wantPublic && !nextCalled {
				t.Errorf("path %q should be public but next was not called", tt.path)
			}
			if !tt.wantPublic && nextCalled {
				t.Errorf("path %q should be protected but next was called", tt.path)
			}
			if !tt.wantPublic && w.Code != http.StatusFound {
				t.Errorf("path %q: status = %d, want %d", tt.path, w.Code, http.StatusFound)
			}
		})
	}
}

func TestAuthMiddleware_ProviderUnavailable(t *testing.T) {
	provider := &fakeIdentityProvider{
		result: identity.Failure("provider_unavailable", "connection refused: auth provider unavailable"),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "some-token"})
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (503)", w.Code, http.StatusServiceUnavailable)
	}
}

func TestAuthMiddleware_XUserHeadersStripped(t *testing.T) {
	ident := validIdentity()
	provider := &fakeIdentityProviderWithCookie{
		cookieName: "ory_kratos_session",
		validCookies: map[string]identity.Identity{
			"valid-token": ident,
		},
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	var receivedHeaders http.Header
	mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	// Inject spoofed identity headers.
	req.Header.Set("X-User-Id", "evil-id")
	req.Header.Set("X-User-Email", "evil@attacker.com")
	req.Header.Set("X-User-Custom", "spoofed")
	req.Header.Set("X-Other-Header", "kept") // non-X-User-* must be kept

	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	for _, h := range []string{"X-User-Id", "X-User-Email", "X-User-Custom"} {
		if receivedHeaders.Get(h) != "" {
			t.Errorf("header %q should have been stripped, but got %q", h, receivedHeaders.Get(h))
		}
	}

	if receivedHeaders.Get("X-Other-Header") == "" {
		t.Error("X-Other-Header should not have been stripped")
	}
}

func TestAuthMiddleware_XUserHeadersStrippedOnPublicPath(t *testing.T) {
	provider := &fakeIdentityProvider{
		result: identity.Failure("no_credentials", "no session cookie"),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
		PublicPaths:       []string{"/health"},
	}

	var receivedHeaders http.Header
	mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-User-Id", "evil-id")

	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if receivedHeaders.Get("X-User-Id") != "" {
		t.Errorf("X-User-Id should have been stripped on public path, got %q", receivedHeaders.Get("X-User-Id"))
	}
}

func TestAuthMiddleware_VibewardenPrefixAlwaysPublic(t *testing.T) {
	provider := &fakeIdentityProvider{
		result: identity.Failure("no_credentials", "no session cookie"),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
		// No explicit public paths — /_vibewarden/* must still be public.
	}

	tests := []struct {
		name string
		path string
	}{
		{"health endpoint", "/_vibewarden/health"},
		{"metrics endpoint", "/_vibewarden/metrics"},
		{"any sub-path", "/_vibewarden/anything"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			if !nextCalled {
				t.Errorf("next handler not called for always-public path %q", tt.path)
			}
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for path %q", w.Code, http.StatusOK, tt.path)
			}
		})
	}
}

func TestAuthMiddleware_DefaultCookieNameAndLoginURL(t *testing.T) {
	ident := validIdentity()
	provider := &fakeIdentityProviderWithCookie{
		cookieName: defaultSessionCookieName,
		validCookies: map[string]identity.Identity{
			"token123": ident,
		},
	}
	// Empty SessionCookieName and LoginURL — should use defaults.
	cfg := ports.AuthConfig{
		Enabled: true,
	}

	mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	req.AddCookie(&http.Cookie{Name: defaultSessionCookieName, Value: "token123"})
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next handler should have been called with default cookie name")
	}

	// Now test redirect to default login URL.
	req2 := httptest.NewRequest(http.MethodGet, "/page", nil)
	w2 := httptest.NewRecorder()
	mw(next).ServeHTTP(w2, req2)

	if w2.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w2.Code, http.StatusFound)
	}
	if loc := w2.Header().Get("Location"); loc != defaultLoginURL {
		t.Errorf("Location = %q, want %q", loc, defaultLoginURL)
	}
}

func TestAuthMiddleware_InvalidSessionRedirects(t *testing.T) {
	tests := []struct {
		name   string
		reason string
	}{
		{"session not found", "session_not_found"},
		{"session invalid", "session_invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeIdentityProvider{
				result: identity.Failure(tt.reason, tt.reason),
			}
			cfg := ports.AuthConfig{
				Enabled:           true,
				SessionCookieName: "ory_kratos_session",
				LoginURL:          "/login",
			}

			mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "bad-token"})
			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			if w.Code != http.StatusFound {
				t.Errorf("status = %d, want %d (redirect)", w.Code, http.StatusFound)
			}
			if loc := w.Header().Get("Location"); loc != "/login" {
				t.Errorf("Location = %q, want %q", loc, "/login")
			}
		})
	}
}

func TestAuthMiddleware_EmitsAuthSuccessEvent(t *testing.T) {
	ident := validIdentity()
	provider := &fakeIdentityProvider{
		result: identity.Success(ident),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}
	spy := &fakeEventLogger{}

	mw := AuthMiddleware(provider, cfg, newTestLogger(), spy, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !spy.hasEventType(events.EventTypeAuthSuccess) {
		t.Error("expected auth.success event but none was logged")
	}
	if len(spy.logged) == 0 {
		t.Fatal("no events logged")
	}
	ev := spy.logged[0]
	if ev.SchemaVersion != events.SchemaVersion {
		t.Errorf("schema_version = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
	}
	if ev.Payload["identity_id"] != ident.ID() {
		t.Errorf("payload.identity_id = %v, want %q", ev.Payload["identity_id"], ident.ID())
	}
	if ev.Payload["email"] != ident.Email() {
		t.Errorf("payload.email = %v, want %q", ev.Payload["email"], ident.Email())
	}
}

func TestAuthMiddleware_EmitsAuthFailedEvent(t *testing.T) {
	provider := &fakeIdentityProvider{
		result: identity.Failure("no_credentials", "missing session cookie"),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}
	spy := &fakeEventLogger{}

	mw := AuthMiddleware(provider, cfg, newTestLogger(), spy, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// No cookie — should emit auth.failed.
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !spy.hasEventType(events.EventTypeAuthFailed) {
		t.Error("expected auth.failed event but none was logged")
	}
	if len(spy.logged) == 0 {
		t.Fatal("no events logged")
	}
	ev := spy.logged[0]
	if ev.Payload["reason"] != "missing session cookie" {
		t.Errorf("payload.reason = %v, want %q", ev.Payload["reason"], "missing session cookie")
	}
}

func TestAuthMiddleware_NilEventLoggerDoesNotPanic(t *testing.T) {
	provider := &fakeIdentityProvider{
		result: identity.Failure("no_credentials", "no session cookie"),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	// Must not panic.
	mw(next).ServeHTTP(w, req)
}

func TestAuthMiddleware_503IsJSON(t *testing.T) {
	provider := &fakeIdentityProvider{
		result: identity.Failure("provider_unavailable", "auth provider unavailable"),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	mw := AuthMiddleware(provider, cfg, newTestLogger(), nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "some-token"})
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Error != "auth_provider_unavailable" {
		t.Errorf("error = %q, want %q", body.Error, "auth_provider_unavailable")
	}
	if body.RequestID == "" && body.TraceID == "" {
		t.Error("expected request_id or trace_id in 503 response body")
	}
}
