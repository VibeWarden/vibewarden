//go:build integration

// Package middleware contains integration tests for the auth middleware chain.
// These tests verify the complete middleware stack behaviour — unauthenticated
// requests are redirected to login, public paths bypass auth, and Kratos flow
// paths are accessible without a session.
//
// No external containers are required for these tests; they use fake
// implementations of ports.SessionChecker.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// fixedSessionChecker is a test-only SessionChecker that returns a fixed session
// or a fixed error on every call, regardless of the session cookie value.
type fixedSessionChecker struct {
	session *ports.Session
	err     error
}

func (f *fixedSessionChecker) CheckSession(_ context.Context, _ string) (*ports.Session, error) {
	return f.session, f.err
}

// TestAuthMiddleware_Integration_UnauthenticatedRedirect verifies that a request
// without a session cookie is redirected to the configured login URL.
func TestAuthMiddleware_Integration_UnauthenticatedRedirect(t *testing.T) {
	checker := &fixedSessionChecker{err: ports.ErrSessionNotFound}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/self-service/login/browser",
	}

	handler := AuthMiddleware(checker, cfg, slog.Default(), nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("status code = %d, want %d (redirect)", rec.Code, http.StatusFound)
	}

	location := rec.Header().Get("Location")
	if location != "/self-service/login/browser" {
		t.Errorf("Location = %q, want %q", location, "/self-service/login/browser")
	}
}

// TestAuthMiddleware_Integration_PublicPathBypass verifies that a request to a
// path matching a public path pattern is forwarded without any auth check.
func TestAuthMiddleware_Integration_PublicPathBypass(t *testing.T) {
	// Checker always returns an error; the middleware must NOT call it for
	// public paths.
	alwaysErr := &fixedSessionChecker{err: ports.ErrAuthProviderUnavailable}

	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/self-service/login/browser",
		PublicPaths:       []string{"/static/*", "/public/*"},
	}

	nextCalled := false
	handler := AuthMiddleware(alwaysErr, cfg, slog.Default(), nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	tests := []struct {
		name string
		path string
	}{
		{"vibewarden health", "/_vibewarden/health"},
		{"static asset", "/static/app.js"},
		{"public page", "/public/about"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled = false
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("path %s: status = %d, want %d", tt.path, rec.Code, http.StatusOK)
			}
			if !nextCalled {
				t.Errorf("path %s: next handler was not called (public path bypass failed)", tt.path)
			}
		})
	}
}

// TestAuthMiddleware_Integration_AuthProviderUnavailable verifies that when the
// session checker returns ErrAuthProviderUnavailable the middleware responds with
// 503 Service Unavailable (fail closed — never fail open).
func TestAuthMiddleware_Integration_AuthProviderUnavailable(t *testing.T) {
	checker := &fixedSessionChecker{err: ports.ErrAuthProviderUnavailable}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
	}

	handler := AuthMiddleware(checker, cfg, slog.Default(), nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "some-cookie"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d (service unavailable)", rec.Code, http.StatusServiceUnavailable)
	}
}

// TestAuthMiddleware_Integration_ValidSessionAllowsRequest verifies that a valid
// session allows the request through to the next handler.
func TestAuthMiddleware_Integration_ValidSessionAllowsRequest(t *testing.T) {
	validSession := &ports.Session{
		ID:     "sess-123",
		Active: true,
		Identity: ports.Identity{
			ID:    "identity-456",
			Email: "user@example.com",
		},
	}
	checker := &fixedSessionChecker{session: validSession}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
	}

	nextCalled := false
	handler := AuthMiddleware(checker, cfg, slog.Default(), nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}
	if !nextCalled {
		t.Error("next handler was not called for a valid session")
	}
}

// TestAuthMiddleware_Integration_KratosFlowPathsArePublic verifies that requests
// to Kratos self-service flow paths are treated as public and bypass auth.
// In the full system these paths are handled by the Caddy-level Kratos route
// before the auth middleware is involved; this test verifies the middleware
// layer also allows them through when configured as public paths.
func TestAuthMiddleware_Integration_KratosFlowPathsArePublic(t *testing.T) {
	// Checker always fails; we want to confirm the middleware does NOT call it.
	alwaysErr := &fixedSessionChecker{err: ports.ErrAuthProviderUnavailable}

	kratosFlowPaths := []string{
		"/self-service/*",
		"/.ory/kratos/public/*",
	}

	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/self-service/login/browser",
		PublicPaths:       kratosFlowPaths,
	}

	handler := AuthMiddleware(alwaysErr, cfg, slog.Default(), nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	tests := []string{
		"/self-service/login/browser",
		"/self-service/registration/browser",
		"/self-service/logout",
		"/self-service/settings/browser",
		"/.ory/kratos/public/sessions/whoami",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("path %s: status = %d, want %d (should bypass auth)", path, rec.Code, http.StatusOK)
			}
		})
	}
}
