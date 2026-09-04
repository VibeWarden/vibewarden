package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeLockoutGuard is a scripted ports.AuthLockoutGuard. It is hand-written on
// purpose: the middleware package must not import an adapter, so the guard's
// real timing behaviour is covered by the adapter's own tests and the
// middleware is tested against scripted statuses only.
type fakeLockoutGuard struct {
	// status is returned by Status for every key.
	status ports.LockoutStatus

	// failure is returned by RecordFailure for every key.
	failure ports.LockoutStatus

	statusKeys  []string
	failureKeys []string
	successKeys []string
}

func (f *fakeLockoutGuard) Status(key string) ports.LockoutStatus {
	f.statusKeys = append(f.statusKeys, key)
	return f.status
}

func (f *fakeLockoutGuard) RecordFailure(key string) ports.LockoutStatus {
	f.failureKeys = append(f.failureKeys, key)
	return f.failure
}

func (f *fakeLockoutGuard) RecordSuccess(key string) {
	f.successKeys = append(f.successKeys, key)
}

func (f *fakeLockoutGuard) touched() bool {
	return len(f.statusKeys)+len(f.failureKeys)+len(f.successKeys) > 0
}

var _ ports.AuthLockoutGuard = (*fakeLockoutGuard)(nil)

// lockoutTestConfig is the admin config shared by the lockout tests.
func lockoutTestConfig() ports.AdminAuthConfig {
	return ports.AdminAuthConfig{Enabled: true, Token: "correct-token"}
}

// newAdminRequest builds a gated admin request from the given client address.
func newAdminRequest(remoteAddr, token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	req.RemoteAddr = remoteAddr
	if token != "" {
		req.Header.Set(adminKeyHeader, token)
	}
	return req
}

func TestAdminAuthMiddleware_UnderThresholdEmitsFailureAndKeeps401(t *testing.T) {
	guard := &fakeLockoutGuard{
		failure: ports.LockoutStatus{Failures: 3, Threshold: 10, Window: time.Minute, Cooldown: time.Minute},
	}
	auditSpy := &fakeAuditEventLogger{}

	mw := AdminAuthMiddleware(lockoutTestConfig(), auditSpy, guard)
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, newAdminRequest("203.0.113.7:5555", "wrong-token"))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if got := w.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("WWW-Authenticate header missing on the 401 path")
	}
	if !auditSpy.hasEventType(audit.EventTypeAuthFailure) {
		t.Error("expected audit.auth.failure below the threshold")
	}
	if auditSpy.hasEventType(audit.EventTypeAuthLockout) {
		t.Error("audit.auth.lockout must not be emitted below the threshold")
	}
	if len(guard.failureKeys) != 1 || guard.failureKeys[0] != "203.0.113.7" {
		t.Errorf("RecordFailure keys = %v, want [203.0.113.7]", guard.failureKeys)
	}
}

func TestAdminAuthMiddleware_TrippingFailureEmitsLockoutEventInsteadOfFailure(t *testing.T) {
	guard := &fakeLockoutGuard{
		failure: ports.LockoutStatus{
			Tripped:    true,
			RetryAfter: time.Minute,
			Failures:   10,
			Threshold:  10,
			Window:     time.Minute,
			Cooldown:   time.Minute,
		},
	}
	auditSpy := &fakeAuditEventLogger{}

	mw := AdminAuthMiddleware(lockoutTestConfig(), auditSpy, guard)
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, newAdminRequest("203.0.113.7:5555", "wrong-token"))

	// The tripping request is still a 401 — the 429 starts on the next request.
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d on the tripping request", w.Code, http.StatusUnauthorized)
	}

	if auditSpy.hasEventType(audit.EventTypeAuthFailure) {
		t.Error("audit.auth.failure must be replaced by audit.auth.lockout on the tripping request")
	}

	var lockouts int
	for _, ev := range auditSpy.logged {
		if ev.EventType == audit.EventTypeAuthLockout {
			lockouts++
		}
	}
	if lockouts != 1 {
		t.Fatalf("audit.auth.lockout events = %d, want exactly 1", lockouts)
	}

	ev, _ := auditSpy.lastEventOfType(audit.EventTypeAuthLockout)
	if ev.Outcome != audit.OutcomeFailure {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeFailure)
	}
	if ev.Actor.IP != "203.0.113.7" {
		t.Errorf("actor.ip = %q, want %q", ev.Actor.IP, "203.0.113.7")
	}
	if ev.Target.Path != "/_vibewarden/admin/users" {
		t.Errorf("target.path = %q, want %q", ev.Target.Path, "/_vibewarden/admin/users")
	}

	wantDetails := map[string]any{
		"method":              http.MethodGet,
		"failures":            10,
		"threshold":           10,
		"window_seconds":      60,
		"cooldown_seconds":    60,
		"retry_after_seconds": 60,
	}
	for k, want := range wantDetails {
		if got := ev.Details[k]; got != want {
			t.Errorf("details[%q] = %v, want %v", k, got, want)
		}
	}
}

func TestAdminAuthMiddleware_LockedOutReturns429WithoutAuditOrTokenCheck(t *testing.T) {
	guard := &fakeLockoutGuard{
		status: ports.LockoutStatus{
			LockedOut:  true,
			RetryAfter: 42 * time.Second,
			Failures:   10,
			Threshold:  10,
			Window:     time.Minute,
			Cooldown:   time.Minute,
		},
	}
	auditSpy := &fakeAuditEventLogger{}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := AdminAuthMiddleware(lockoutTestConfig(), auditSpy, guard)
	w := httptest.NewRecorder()
	// The CORRECT token is presented: a locked-out client must still get 429,
	// which proves the token was never compared on this path.
	mw(next).ServeHTTP(w, newAdminRequest("203.0.113.7:5555", "correct-token"))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if nextCalled {
		t.Error("next handler must not be called for a locked-out client")
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("WWW-Authenticate = %q, want absent on the 429 path", got)
	}

	retryAfter := w.Header().Get("Retry-After")
	secs, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After = %q, want an integer number of seconds: %v", retryAfter, err)
	}
	if secs != 42 {
		t.Errorf("Retry-After = %d, want 42", secs)
	}

	var body ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Error != "too_many_failed_attempts" {
		t.Errorf("body.error = %q, want %q", body.Error, "too_many_failed_attempts")
	}
	if body.Status != http.StatusTooManyRequests {
		t.Errorf("body.status = %d, want %d", body.Status, http.StatusTooManyRequests)
	}
	if body.RetryAfterSeconds != 42 {
		t.Errorf("body.retry_after_seconds = %d, want 42", body.RetryAfterSeconds)
	}

	if len(auditSpy.logged) != 0 {
		t.Errorf("audit events = %d, want 0 while in cooldown", len(auditSpy.logged))
	}
	if len(guard.failureKeys) != 0 {
		t.Errorf("RecordFailure calls = %d, want 0 while in cooldown", len(guard.failureKeys))
	}
	if len(guard.successKeys) != 0 {
		t.Errorf("RecordSuccess calls = %d, want 0 while in cooldown", len(guard.successKeys))
	}
}

func TestAdminAuthMiddleware_SuccessResetsTheCounter(t *testing.T) {
	guard := &fakeLockoutGuard{}
	auditSpy := &fakeAuditEventLogger{}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := AdminAuthMiddleware(lockoutTestConfig(), auditSpy, guard)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, newAdminRequest("203.0.113.7:5555", "correct-token"))

	if !nextCalled {
		t.Fatal("next handler was not called on a valid token")
	}
	if len(guard.successKeys) != 1 || guard.successKeys[0] != "203.0.113.7" {
		t.Errorf("RecordSuccess keys = %v, want [203.0.113.7]", guard.successKeys)
	}
	if len(guard.failureKeys) != 0 {
		t.Errorf("RecordFailure calls = %d, want 0 on a valid token", len(guard.failureKeys))
	}
	if !auditSpy.hasEventType(audit.EventTypeAuthSuccess) {
		t.Error("expected audit.auth.success on a valid token")
	}
}

func TestAdminAuthMiddleware_KeysTheGuardOnTheClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		wantKey    string
	}{
		{"ipv4 with port", "198.51.100.4:1234", "198.51.100.4"},
		{"ipv6 with port", "[2001:db8::1]:1234", "2001:db8::1"},
		{"bare ipv4", "198.51.100.5", "198.51.100.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := &fakeLockoutGuard{}
			mw := AdminAuthMiddleware(lockoutTestConfig(), nil, guard)
			w := httptest.NewRecorder()
			mw(okHandler).ServeHTTP(w, newAdminRequest(tt.remoteAddr, "wrong-token"))

			if len(guard.failureKeys) != 1 || guard.failureKeys[0] != tt.wantKey {
				t.Errorf("RecordFailure keys = %v, want [%s]", guard.failureKeys, tt.wantKey)
			}
		})
	}
}

func TestAdminAuthMiddleware_UnresolvableClientIPSkipsTheGuard(t *testing.T) {
	// A client whose IP cannot be resolved must never share a bucket with every
	// other unidentifiable client — the guard is skipped entirely.
	guard := &fakeLockoutGuard{
		status: ports.LockoutStatus{LockedOut: true, RetryAfter: time.Minute},
	}
	auditSpy := &fakeAuditEventLogger{}

	mw := AdminAuthMiddleware(lockoutTestConfig(), auditSpy, guard)
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, newAdminRequest("", "wrong-token"))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (the token must still be evaluated)", w.Code, http.StatusUnauthorized)
	}
	if guard.touched() {
		t.Errorf("guard was called with an unresolvable client IP: status=%v failure=%v success=%v",
			guard.statusKeys, guard.failureKeys, guard.successKeys)
	}
	if !auditSpy.hasEventType(audit.EventTypeAuthFailure) {
		t.Error("expected the normal audit.auth.failure event when the guard is skipped")
	}
}

func TestAdminAuthMiddleware_NilGuardPreservesLegacyBehaviour(t *testing.T) {
	auditSpy := &fakeAuditEventLogger{}
	mw := AdminAuthMiddleware(lockoutTestConfig(), auditSpy, nil)

	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, newAdminRequest("203.0.113.7:5555", "wrong-token"))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("WWW-Authenticate header missing with a nil guard")
	}
	if !auditSpy.hasEventType(audit.EventTypeAuthFailure) {
		t.Error("expected audit.auth.failure with a nil guard")
	}

	w = httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, newAdminRequest("203.0.113.7:5555", "correct-token"))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d with a valid token and a nil guard", w.Code, http.StatusOK)
	}
}

func TestAdminAuthMiddleware_GuardUntouchedOnUngatedPaths(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		enabled bool
		token   string
	}{
		{"non-admin path", "/dashboard", true, "correct-token"},
		{"ui carve-out", "/_vibewarden/admin/ui/index.html", true, "correct-token"},
		{"bare ui path", "/_vibewarden/admin/ui", true, "correct-token"},
		{"admin disabled", "/_vibewarden/admin/users", false, "correct-token"},
		{"empty configured token", "/_vibewarden/admin/users", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := &fakeLockoutGuard{
				status: ports.LockoutStatus{LockedOut: true, RetryAfter: time.Minute},
			}
			cfg := ports.AdminAuthConfig{Enabled: tt.enabled, Token: tt.token}
			mw := AdminAuthMiddleware(cfg, nil, guard)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.RemoteAddr = "203.0.113.7:5555"
			w := httptest.NewRecorder()
			mw(okHandler).ServeHTTP(w, req)

			if guard.touched() {
				t.Errorf("guard was consulted on %q: status=%v failure=%v success=%v",
					tt.path, guard.statusKeys, guard.failureKeys, guard.successKeys)
			}
			if w.Code == http.StatusTooManyRequests {
				t.Errorf("path %q returned 429; ungated paths must never be throttled", tt.path)
			}
		})
	}
}
