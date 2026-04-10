package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeSessionChecker implements the deprecated ports.SessionChecker for testing.
type fakeSessionChecker struct {
	session *ports.Session
	err     error
}

func (f *fakeSessionChecker) CheckSession(_ context.Context, _ string) (*ports.Session, error) {
	return f.session, f.err
}

func TestSessionCheckerToIdentityProvider_Name(t *testing.T) {
	provider := middleware.SessionCheckerToIdentityProvider(&fakeSessionChecker{}, "")
	if got := provider.Name(); got != "kratos" {
		t.Errorf("Name() = %q, want %q", got, "kratos")
	}
}

func TestSessionCheckerToIdentityProvider_ValidSession(t *testing.T) {
	checker := &fakeSessionChecker{
		session: &ports.Session{
			ID:     "session-123",
			Active: true,
			Identity: ports.Identity{
				ID: "user-456",
				Traits: map[string]any{
					"email": "test@example.com",
				},
			},
		},
	}

	provider := middleware.SessionCheckerToIdentityProvider(checker, "my_session")

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "my_session", Value: "token-abc"})

	result := provider.Authenticate(context.Background(), req)
	if !result.Authenticated {
		t.Fatalf("expected success, got failure: %s — %s", result.Reason, result.Message)
	}
	if result.Identity.ID() != "user-456" {
		t.Errorf("ID = %q, want %q", result.Identity.ID(), "user-456")
	}
}

func TestSessionCheckerToIdentityProvider_NoCookie(t *testing.T) {
	provider := middleware.SessionCheckerToIdentityProvider(&fakeSessionChecker{}, "")

	req := httptest.NewRequest("GET", "/", nil)
	// no cookie

	result := provider.Authenticate(context.Background(), req)
	if result.Authenticated {
		t.Fatal("expected failure when no cookie present")
	}
	if result.Reason != "no_credentials" {
		t.Errorf("Reason = %q, want %q", result.Reason, "no_credentials")
	}
}

func TestSessionCheckerToIdentityProvider_InvalidSession(t *testing.T) {
	checker := &fakeSessionChecker{err: ports.ErrSessionInvalid}
	provider := middleware.SessionCheckerToIdentityProvider(checker, "")

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "expired"})

	result := provider.Authenticate(context.Background(), req)
	if result.Authenticated {
		t.Fatal("expected failure for invalid session")
	}
	if result.Reason != "session_invalid" {
		t.Errorf("Reason = %q, want %q", result.Reason, "session_invalid")
	}
}

func TestSessionCheckerToIdentityProvider_SessionNotFound(t *testing.T) {
	checker := &fakeSessionChecker{err: ports.ErrSessionNotFound}
	provider := middleware.SessionCheckerToIdentityProvider(checker, "")

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "unknown"})

	result := provider.Authenticate(context.Background(), req)
	if result.Authenticated {
		t.Fatal("expected failure for not found session")
	}
	if result.Reason != "session_not_found" {
		t.Errorf("Reason = %q, want %q", result.Reason, "session_not_found")
	}
}

func TestSessionCheckerToIdentityProvider_ProviderUnavailable(t *testing.T) {
	checker := &fakeSessionChecker{err: ports.ErrAuthProviderUnavailable}
	provider := middleware.SessionCheckerToIdentityProvider(checker, "")

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "token"})

	result := provider.Authenticate(context.Background(), req)
	if result.Authenticated {
		t.Fatal("expected failure for unavailable provider")
	}
	if result.Reason != "provider_unavailable" {
		t.Errorf("Reason = %q, want %q", result.Reason, "provider_unavailable")
	}
}

func TestSessionCheckerToIdentityProvider_DefaultCookieName(t *testing.T) {
	checker := &fakeSessionChecker{
		session: &ports.Session{
			ID:     "s1",
			Active: true,
			Identity: ports.Identity{
				ID:     "u1",
				Traits: map[string]any{},
			},
		},
	}

	// empty cookie name should default to ory_kratos_session
	provider := middleware.SessionCheckerToIdentityProvider(checker, "")

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "token"})

	result := provider.Authenticate(context.Background(), req)
	if !result.Authenticated {
		t.Fatalf("expected success with default cookie name, got: %s", result.Reason)
	}
}
