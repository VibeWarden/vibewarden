package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeSessionChecker is a simple in-memory fake that implements
// ports.SessionChecker without any mocking framework.
type fakeSessionChecker struct {
	// sessions maps session cookie value (full "name=value" string) to
	// the session that should be returned.
	sessions map[string]*ports.Session
	// err, when non-nil, is returned for every CheckSession call
	// regardless of the cookie value.
	err error
}

func (f *fakeSessionChecker) CheckSession(_ context.Context, sessionCookie string) (*ports.Session, error) {
	if f.err != nil {
		return nil, f.err
	}
	s, ok := f.sessions[sessionCookie]
	if !ok {
		return nil, ports.ErrSessionNotFound
	}
	return s, nil
}

// validSession returns a non-nil, active session for use in tests.
func validSession() *ports.Session {
	return &ports.Session{
		ID:     "sess-abc",
		Active: true,
		Identity: ports.Identity{
			ID:            "user-123",
			Email:         "alice@example.com",
			EmailVerified: true,
		},
	}
}

// newTestLogger returns a no-op slog.Logger suitable for tests.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

// noopWriter discards all output.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestAuthMiddleware_UnauthenticatedRequest(t *testing.T) {
	checker := &fakeSessionChecker{sessions: map[string]*ports.Session{}}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	mw := AuthMiddleware(checker, cfg, newTestLogger())
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
	sess := validSession()
	checker := &fakeSessionChecker{
		sessions: map[string]*ports.Session{
			"ory_kratos_session=valid-token": sess,
		},
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	nextCalled := false
	var nextCtx context.Context

	mw := AuthMiddleware(checker, cfg, newTestLogger())
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

	gotSess, ok := SessionFromContext(nextCtx)
	if !ok {
		t.Fatal("session not stored in context")
	}
	if gotSess.ID != sess.ID {
		t.Errorf("context session ID = %q, want %q", gotSess.ID, sess.ID)
	}
}

func TestAuthMiddleware_PublicPathBypass(t *testing.T) {
	checker := &fakeSessionChecker{sessions: map[string]*ports.Session{}}
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
			mw := AuthMiddleware(checker, cfg, newTestLogger())
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			// No session cookie — but should not trigger redirect.
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
	checker := &fakeSessionChecker{sessions: map[string]*ports.Session{}}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
		PublicPaths:       []string{"/api/v1/*/public"},
	}

	tests := []struct {
		name        string
		path        string
		wantPublic  bool
	}{
		{"matched glob segment", "/api/v1/users/public", true},
		{"matched glob segment resources", "/api/v1/items/public", true},
		{"non-matching path", "/api/v1/users/private", false},
		{"too many segments not matched", "/api/v1/users/extra/public", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			mw := AuthMiddleware(checker, cfg, newTestLogger())
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			// No cookie; if path is public the next handler is called,
			// otherwise we get a redirect.
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
	checker := &fakeSessionChecker{
		err: fmt.Errorf("connection refused: %w", ports.ErrAuthProviderUnavailable),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	mw := AuthMiddleware(checker, cfg, newTestLogger())
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
	sess := validSession()
	checker := &fakeSessionChecker{
		sessions: map[string]*ports.Session{
			"ory_kratos_session=valid-token": sess,
		},
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	var receivedHeaders http.Header
	mw := AuthMiddleware(checker, cfg, newTestLogger())
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
	checker := &fakeSessionChecker{sessions: map[string]*ports.Session{}}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
		PublicPaths:       []string{"/health"},
	}

	var receivedHeaders http.Header
	mw := AuthMiddleware(checker, cfg, newTestLogger())
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
	checker := &fakeSessionChecker{sessions: map[string]*ports.Session{}}
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
			mw := AuthMiddleware(checker, cfg, newTestLogger())
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			// No session cookie.
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
	sess := validSession()
	checker := &fakeSessionChecker{
		sessions: map[string]*ports.Session{
			// default cookie name
			"ory_kratos_session=token123": sess,
		},
	}
	// Empty SessionCookieName and LoginURL — should use defaults.
	cfg := ports.AuthConfig{
		Enabled: true,
	}

	mw := AuthMiddleware(checker, cfg, newTestLogger())
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "token123"})
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
		name    string
		checkerErr error
	}{
		{"session not found", ports.ErrSessionNotFound},
		{"session invalid", ports.ErrSessionInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &fakeSessionChecker{err: tt.checkerErr}
			cfg := ports.AuthConfig{
				Enabled:           true,
				SessionCookieName: "ory_kratos_session",
				LoginURL:          "/login",
			}

			mw := AuthMiddleware(checker, cfg, newTestLogger())
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
