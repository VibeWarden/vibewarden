package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/auth"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeAuditEventLogger is a spy that captures all audit events emitted through it.
// It implements ports.AuditEventLogger without any real I/O.
type fakeAuditEventLogger struct {
	logged []audit.AuditEvent
}

func (f *fakeAuditEventLogger) Log(_ context.Context, ev audit.AuditEvent) error {
	f.logged = append(f.logged, ev)
	return nil
}

// hasEventType returns true if the spy captured at least one audit event of the given type.
func (f *fakeAuditEventLogger) hasEventType(eventType audit.EventType) bool {
	for _, ev := range f.logged {
		if ev.EventType == eventType {
			return true
		}
	}
	return false
}

// lastEventOfType returns the last captured audit event matching the given type.
// Returns (zero, false) when no matching event exists.
func (f *fakeAuditEventLogger) lastEventOfType(eventType audit.EventType) (audit.AuditEvent, bool) {
	for i := len(f.logged) - 1; i >= 0; i-- {
		if f.logged[i].EventType == eventType {
			return f.logged[i], true
		}
	}
	return audit.AuditEvent{}, false
}

// Compile-time check: fakeAuditEventLogger satisfies ports.AuditEventLogger.
var _ ports.AuditEventLogger = (*fakeAuditEventLogger)(nil)

// ---------------------------------------------------------------------------
// AuthMiddleware audit tests
// ---------------------------------------------------------------------------

func TestAuthMiddleware_EmitsAuditAuthSuccessEvent(t *testing.T) {
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
	auditSpy := &fakeAuditEventLogger{}

	mw := AuthMiddleware(checker, cfg, newTestLogger(), nil, auditSpy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !auditSpy.hasEventType(audit.EventTypeAuthSuccess) {
		t.Error("expected audit.auth.success event but none was logged")
	}
	ev, ok := auditSpy.lastEventOfType(audit.EventTypeAuthSuccess)
	if !ok {
		t.Fatal("no audit.auth.success event found")
	}
	if ev.Outcome != audit.OutcomeSuccess {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeSuccess)
	}
	if ev.Actor.UserID != sess.Identity.ID {
		t.Errorf("actor.user_id = %q, want %q", ev.Actor.UserID, sess.Identity.ID)
	}
	if ev.Target.Path != "/dashboard" {
		t.Errorf("target.path = %q, want %q", ev.Target.Path, "/dashboard")
	}
	if ev.Timestamp.IsZero() {
		t.Error("timestamp must not be zero")
	}
}

func TestAuthMiddleware_EmitsAuditAuthFailureOnMissingCookie(t *testing.T) {
	checker := &fakeSessionChecker{sessions: map[string]*ports.Session{}}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}
	auditSpy := &fakeAuditEventLogger{}

	mw := AuthMiddleware(checker, cfg, newTestLogger(), nil, auditSpy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !auditSpy.hasEventType(audit.EventTypeAuthFailure) {
		t.Error("expected audit.auth.failure event but none was logged")
	}
	ev, ok := auditSpy.lastEventOfType(audit.EventTypeAuthFailure)
	if !ok {
		t.Fatal("no audit.auth.failure event found")
	}
	if ev.Outcome != audit.OutcomeFailure {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeFailure)
	}
	if ev.Details["reason"] != "missing session cookie" {
		t.Errorf("details.reason = %v, want %q", ev.Details["reason"], "missing session cookie")
	}
}

func TestAuthMiddleware_EmitsAuditAuthFailureOnInvalidSession(t *testing.T) {
	checker := &fakeSessionChecker{err: ports.ErrSessionInvalid}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}
	auditSpy := &fakeAuditEventLogger{}

	mw := AuthMiddleware(checker, cfg, newTestLogger(), nil, auditSpy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "bad-token"})
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !auditSpy.hasEventType(audit.EventTypeAuthFailure) {
		t.Error("expected audit.auth.failure event but none was logged")
	}
}

func TestAuthMiddleware_NilAuditLoggerDoesNotPanic(t *testing.T) {
	checker := &fakeSessionChecker{sessions: map[string]*ports.Session{}}
	cfg := ports.AuthConfig{
		Enabled:           true,
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/login",
	}

	// nil auditLogger must not cause a panic.
	mw := AuthMiddleware(checker, cfg, newTestLogger(), nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)
}

// ---------------------------------------------------------------------------
// AdminAuthMiddleware audit tests
// ---------------------------------------------------------------------------

func TestAdminAuthMiddleware_EmitsAuditSuccessOnValidToken(t *testing.T) {
	cfg := ports.AdminAuthConfig{Enabled: true, Token: "secret-token"}
	auditSpy := &fakeAuditEventLogger{}

	mw := AdminAuthMiddleware(cfg, auditSpy)
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	req.Header.Set(adminKeyHeader, "secret-token")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !nextCalled {
		t.Fatal("next handler was not called")
	}
	if !auditSpy.hasEventType(audit.EventTypeAuthSuccess) {
		t.Error("expected audit.auth.success event but none was logged")
	}
	ev, ok := auditSpy.lastEventOfType(audit.EventTypeAuthSuccess)
	if !ok {
		t.Fatal("no audit.auth.success event found")
	}
	if ev.Outcome != audit.OutcomeSuccess {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeSuccess)
	}
	if ev.Target.Path != "/_vibewarden/admin/users" {
		t.Errorf("target.path = %q, want %q", ev.Target.Path, "/_vibewarden/admin/users")
	}
}

func TestAdminAuthMiddleware_EmitsAuditFailureOnWrongToken(t *testing.T) {
	cfg := ports.AdminAuthConfig{Enabled: true, Token: "correct-token"}
	auditSpy := &fakeAuditEventLogger{}

	mw := AdminAuthMiddleware(cfg, auditSpy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	req.Header.Set(adminKeyHeader, "wrong-token")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !auditSpy.hasEventType(audit.EventTypeAuthFailure) {
		t.Error("expected audit.auth.failure event but none was logged")
	}
	ev, ok := auditSpy.lastEventOfType(audit.EventTypeAuthFailure)
	if !ok {
		t.Fatal("no audit.auth.failure event found")
	}
	if ev.Outcome != audit.OutcomeFailure {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeFailure)
	}
}

func TestAdminAuthMiddleware_NilAuditLoggerDoesNotPanic(t *testing.T) {
	cfg := ports.AdminAuthConfig{Enabled: true, Token: "secret-token"}

	mw := AdminAuthMiddleware(cfg, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	req.Header.Set(adminKeyHeader, "wrong-token")
	w := httptest.NewRecorder()
	// Must not panic.
	mw(next).ServeHTTP(w, req)
}

// ---------------------------------------------------------------------------
// APIKeyMiddleware audit tests
// ---------------------------------------------------------------------------

func TestAPIKeyMiddleware_EmitsAuditSuccessEvent(t *testing.T) {
	apiKey := validAPIKey()
	validator := &fakeAPIKeyValidator{
		keys: map[string]*auth.APIKey{
			"vw_test_secret": apiKey,
		},
	}
	cfg := ports.APIKeyConfig{}
	auditSpy := &fakeAuditEventLogger{}

	mw := APIKeyMiddleware(validator, cfg, nil, auditSpy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "vw_test_secret")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !auditSpy.hasEventType(audit.EventTypeAuthAPIKeySuccess) {
		t.Error("expected audit.auth.api_key.success event but none was logged")
	}
	ev, ok := auditSpy.lastEventOfType(audit.EventTypeAuthAPIKeySuccess)
	if !ok {
		t.Fatal("no audit.auth.api_key.success event found")
	}
	if ev.Outcome != audit.OutcomeSuccess {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeSuccess)
	}
	if ev.Actor.APIKeyName != apiKey.Name {
		t.Errorf("actor.api_key_name = %q, want %q", ev.Actor.APIKeyName, apiKey.Name)
	}
	if ev.Target.Path != "/api/data" {
		t.Errorf("target.path = %q, want %q", ev.Target.Path, "/api/data")
	}
}

func TestAPIKeyMiddleware_EmitsAuditFailureOnMissingKey(t *testing.T) {
	validator := &fakeAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	cfg := ports.APIKeyConfig{}
	auditSpy := &fakeAuditEventLogger{}

	mw := APIKeyMiddleware(validator, cfg, nil, auditSpy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !auditSpy.hasEventType(audit.EventTypeAuthAPIKeyFailure) {
		t.Error("expected audit.auth.api_key.failure event but none was logged")
	}
	ev, ok := auditSpy.lastEventOfType(audit.EventTypeAuthAPIKeyFailure)
	if !ok {
		t.Fatal("no audit.auth.api_key.failure event found")
	}
	if ev.Outcome != audit.OutcomeFailure {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeFailure)
	}
	if ev.Details["reason"] != "missing api key" {
		t.Errorf("details.reason = %v, want %q", ev.Details["reason"], "missing api key")
	}
}

func TestAPIKeyMiddleware_EmitsAuditFailureOnInvalidKey(t *testing.T) {
	validator := &fakeAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	cfg := ports.APIKeyConfig{}
	auditSpy := &fakeAuditEventLogger{}

	mw := APIKeyMiddleware(validator, cfg, nil, auditSpy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "bad-key")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !auditSpy.hasEventType(audit.EventTypeAuthAPIKeyFailure) {
		t.Error("expected audit.auth.api_key.failure event but none was logged")
	}
}

func TestAPIKeyMiddleware_EmitsAuditForbiddenEvent(t *testing.T) {
	apiKey := &auth.APIKey{
		Name:    "read-only",
		KeyHash: auth.HashKey("vw_ro_key"),
		Scopes:  []auth.Scope{"read"},
		Active:  true,
	}
	validator := &fakeAPIKeyValidator{
		keys: map[string]*auth.APIKey{"vw_ro_key": apiKey},
	}
	cfg := ports.APIKeyConfig{ScopeRules: scopeRules()}
	auditSpy := &fakeAuditEventLogger{}

	mw := APIKeyMiddleware(validator, cfg, nil, auditSpy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// POST requires "write" but key only has "read".
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items", nil)
	req.Header.Set("X-API-Key", "vw_ro_key")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if !auditSpy.hasEventType(audit.EventTypeAuthAPIKeyForbidden) {
		t.Error("expected audit.auth.api_key.forbidden event but none was logged")
	}
	ev, ok := auditSpy.lastEventOfType(audit.EventTypeAuthAPIKeyForbidden)
	if !ok {
		t.Fatal("no audit.auth.api_key.forbidden event found")
	}
	if ev.Outcome != audit.OutcomeFailure {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeFailure)
	}
	if ev.Actor.APIKeyName != "read-only" {
		t.Errorf("actor.api_key_name = %q, want %q", ev.Actor.APIKeyName, "read-only")
	}
}

func TestAPIKeyMiddleware_NilAuditLoggerDoesNotPanic(t *testing.T) {
	validator := &fakeAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	cfg := ports.APIKeyConfig{}

	mw := APIKeyMiddleware(validator, cfg, nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	w := httptest.NewRecorder()
	// Must not panic.
	mw(next).ServeHTTP(w, req)
}

// ---------------------------------------------------------------------------
// RateLimitMiddleware audit tests
// ---------------------------------------------------------------------------

func TestRateLimitMiddleware_EmitsAuditRateLimitHitEvent(t *testing.T) {
	retryDuration := 3 * time.Second
	ipLimiter := denyWithRetry(retryDuration, 10, 20)
	userLimiter := allowAll()
	auditSpy := &fakeAuditEventLogger{}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), newTestLogger(), nil, auditSpy)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if !auditSpy.hasEventType(audit.EventTypeRateLimitHit) {
		t.Error("expected audit.rate_limit.hit event but none was logged")
	}
	ev, ok := auditSpy.lastEventOfType(audit.EventTypeRateLimitHit)
	if !ok {
		t.Fatal("no audit.rate_limit.hit event found")
	}
	if ev.Outcome != audit.OutcomeFailure {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeFailure)
	}
	if ev.Actor.IP != "10.0.0.1" {
		t.Errorf("actor.ip = %q, want %q", ev.Actor.IP, "10.0.0.1")
	}
	if ev.Target.Path != "/api/data" {
		t.Errorf("target.path = %q, want %q", ev.Target.Path, "/api/data")
	}
	if ev.Details["limit_type"] != "ip" {
		t.Errorf("details.limit_type = %v, want %q", ev.Details["limit_type"], "ip")
	}
}

func TestRateLimitMiddleware_EmitsAuditRateLimitHitOnUserLimit(t *testing.T) {
	ipLimiter := allowAll()
	userLimiter := denyWithRetry(2*time.Second, 100, 200)
	auditSpy := &fakeAuditEventLogger{}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), newTestLogger(), nil, auditSpy)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "10.0.0.2:9999"
	r.Header.Set("X-User-Id", "user-abc")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !auditSpy.hasEventType(audit.EventTypeRateLimitHit) {
		t.Error("expected audit.rate_limit.hit event but none was logged")
	}
	ev, ok := auditSpy.lastEventOfType(audit.EventTypeRateLimitHit)
	if !ok {
		t.Fatal("no audit.rate_limit.hit event found")
	}
	if ev.Details["limit_type"] != "user" {
		t.Errorf("details.limit_type = %v, want %q", ev.Details["limit_type"], "user")
	}
}

func TestRateLimitMiddleware_NilAuditLoggerDoesNotPanic(t *testing.T) {
	ipLimiter := denyWithRetry(time.Second, 10, 20)
	userLimiter := allowAll()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), newTestLogger(), nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "10.0.0.3:9999"
	w := httptest.NewRecorder()
	// Must not panic.
	handler.ServeHTTP(w, r)
}
