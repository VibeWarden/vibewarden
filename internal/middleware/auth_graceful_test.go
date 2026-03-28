package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestAuthMiddleware_KratosUnavailable_EmitsAvailabilityEvent verifies that
// when Kratos becomes unavailable the middleware emits a single
// auth.provider_unavailable event (only on the first failure, not on every
// subsequent request).
func TestAuthMiddleware_KratosUnavailable_EmitsAvailabilityEvent(t *testing.T) {
	checker := &fakeSessionChecker{
		err: fmt.Errorf("dial error: %w", ports.ErrAuthProviderUnavailable),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
		KratosPublicURL:   "http://127.0.0.1:4433",
	}
	spy := &fakeEventLogger{}

	mw := AuthMiddleware(checker, cfg, newTestLogger(), spy, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// First request — Kratos is down.
	req1 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req1.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "some-token"})
	mw(next).ServeHTTP(httptest.NewRecorder(), req1)

	if !spy.hasEventType(events.EventTypeAuthProviderUnavailable) {
		t.Error("expected auth.provider_unavailable event on first failure, got none")
	}

	unavailableCount := 0
	for _, ev := range spy.logged {
		if ev.EventType == events.EventTypeAuthProviderUnavailable {
			unavailableCount++
		}
	}

	// Second request — Kratos still down. No additional availability event.
	spy.logged = nil
	req2 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req2.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "some-token"})
	mw(next).ServeHTTP(httptest.NewRecorder(), req2)

	for _, ev := range spy.logged {
		if ev.EventType == events.EventTypeAuthProviderUnavailable {
			t.Error("auth.provider_unavailable emitted again on second failure — expected no duplicate")
		}
	}
	_ = unavailableCount
}

// TestAuthMiddleware_KratosRecovery_EmitsRecoveredEvent verifies that when
// Kratos becomes available again after an outage, the middleware emits an
// auth.provider_recovered event exactly once.
func TestAuthMiddleware_KratosRecovery_EmitsRecoveredEvent(t *testing.T) {
	sess := validSession()
	// Start unhealthy.
	checker := &fakeSessionChecker{
		err: fmt.Errorf("dial error: %w", ports.ErrAuthProviderUnavailable),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
		KratosPublicURL:   "http://127.0.0.1:4433",
	}
	spy := &fakeEventLogger{}

	mw := AuthMiddleware(checker, cfg, newTestLogger(), spy, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Simulate an initial failure to set the unavailable state.
	req1 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req1.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "any-token"})
	mw(next).ServeHTTP(httptest.NewRecorder(), req1)

	// Now Kratos comes back — inject a valid session.
	spy.logged = nil
	checker.err = nil
	checker.sessions = map[string]*ports.Session{
		"ory_kratos_session=valid-token": sess,
	}

	req2 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req2.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	mw(next).ServeHTTP(httptest.NewRecorder(), req2)

	if !spy.hasEventType(events.EventTypeAuthProviderRecovered) {
		t.Error("expected auth.provider_recovered event after Kratos came back, got none")
	}
}

// TestAuthMiddleware_KratosRecovery_NoDoubleRecoveryEvent verifies that a
// second successful request after recovery does not emit another
// auth.provider_recovered event.
func TestAuthMiddleware_KratosRecovery_NoDoubleRecoveryEvent(t *testing.T) {
	sess := validSession()
	checker := &fakeSessionChecker{
		err: fmt.Errorf("dial error: %w", ports.ErrAuthProviderUnavailable),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}
	spy := &fakeEventLogger{}
	mw := AuthMiddleware(checker, cfg, newTestLogger(), spy, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// Fail once to set unavailable state.
	req1 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req1.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "t"})
	mw(next).ServeHTTP(httptest.NewRecorder(), req1)

	// Recover.
	checker.err = nil
	checker.sessions = map[string]*ports.Session{"ory_kratos_session=valid": sess}
	req2 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req2.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid"})
	mw(next).ServeHTTP(httptest.NewRecorder(), req2)

	// Second healthy request — should NOT emit another recovered event.
	spy.logged = nil
	req3 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req3.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid"})
	mw(next).ServeHTTP(httptest.NewRecorder(), req3)

	for _, ev := range spy.logged {
		if ev.EventType == events.EventTypeAuthProviderRecovered {
			t.Error("auth.provider_recovered emitted again on second healthy request — expected no duplicate")
		}
	}
}

// TestAuthMiddleware_AvailabilityEvent_PayloadContainsProviderURL checks that
// the auth.provider_unavailable event payload includes the provider URL.
func TestAuthMiddleware_AvailabilityEvent_PayloadContainsProviderURL(t *testing.T) {
	const wantURL = "http://127.0.0.1:4433"

	checker := &fakeSessionChecker{
		err: fmt.Errorf("down: %w", ports.ErrAuthProviderUnavailable),
	}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
		KratosPublicURL:   wantURL,
	}
	spy := &fakeEventLogger{}
	mw := AuthMiddleware(checker, cfg, newTestLogger(), spy, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "t"})
	mw(next).ServeHTTP(httptest.NewRecorder(), req)

	for _, ev := range spy.logged {
		if ev.EventType != events.EventTypeAuthProviderUnavailable {
			continue
		}
		got, ok := ev.Payload["provider_url"].(string)
		if !ok {
			t.Fatal("provider_url missing or not a string in auth.provider_unavailable payload")
		}
		if got != wantURL {
			t.Errorf("provider_url = %q, want %q", got, wantURL)
		}
		return
	}
	t.Error("auth.provider_unavailable event not found")
}

// TestAuthMiddleware_KratosUnavailable_Returns503 confirms the default
// fail-closed behavior (503 for all protected paths).
func TestAuthMiddleware_KratosUnavailable_Returns503(t *testing.T) {
	checker := &fakeSessionChecker{
		err: fmt.Errorf("dial: %w", ports.ErrAuthProviderUnavailable),
	}
	cfg := ports.AuthConfig{
		Enabled:             true,
		SessionCookieName:   "ory_kratos_session",
		LoginURL:            "/login",
		OnKratosUnavailable: ports.KratosUnavailable503,
	}
	mw := AuthMiddleware(checker, cfg, newTestLogger(), nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "t"})
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
