package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/identity"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeRateLimiter is a simple fake implementing ports.RateLimiter.
// Callers configure the result that Allow returns.
type fakeRateLimiter struct {
	result     ports.RateLimitResult
	calledKeys []string
}

func (f *fakeRateLimiter) Allow(_ context.Context, key string) ports.RateLimitResult {
	f.calledKeys = append(f.calledKeys, key)
	return f.result
}

func (f *fakeRateLimiter) Close() error { return nil }

// allowAll returns a fakeRateLimiter that always permits requests.
func allowAll() *fakeRateLimiter {
	return &fakeRateLimiter{
		result: ports.RateLimitResult{
			Allowed:   true,
			Remaining: 9,
			Limit:     10,
			Burst:     20,
		},
	}
}

// denyWithRetry returns a fakeRateLimiter that always denies with the given retry duration.
func denyWithRetry(retryAfter time.Duration, limit float64, burst int) *fakeRateLimiter {
	return &fakeRateLimiter{
		result: ports.RateLimitResult{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: retryAfter,
			Limit:      limit,
			Burst:      burst,
		},
	}
}

// defaultCfg returns a RateLimitConfig suitable for most middleware tests.
func defaultCfg() ports.RateLimitConfig {
	return ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: false,
		ExemptPaths:       nil,
	}
}

// fakeEventLogger is a spy that captures all events emitted through it.
// It implements ports.EventLogger without any real I/O.
type fakeEventLogger struct {
	logged []events.Event
}

func (f *fakeEventLogger) Log(_ context.Context, ev events.Event) error {
	f.logged = append(f.logged, ev)
	return nil
}

// hasEventType returns true if the spy captured at least one event of the given type.
func (f *fakeEventLogger) hasEventType(eventType string) bool {
	for _, ev := range f.logged {
		if ev.EventType == eventType {
			return true
		}
	}
	return false
}

func TestRateLimitMiddleware_RequestWithinLimit(t *testing.T) {
	ipLimiter := allowAll()
	userLimiter := allowAll()
	logger := newTestLogger()

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), logger, nil, nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "192.168.1.1:5000"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if !nextCalled {
		t.Error("expected next handler to be called, but it was not")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_IPLimitExceeded(t *testing.T) {
	retryDuration := 3 * time.Second
	ipLimiter := denyWithRetry(retryDuration, 10, 20)
	userLimiter := allowAll()
	spy := &fakeEventLogger{}

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), newTestLogger(), spy, nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if nextCalled {
		t.Error("expected next handler NOT to be called, but it was")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	// Retry-After header.
	got := w.Header().Get("Retry-After")
	if got != "3" {
		t.Errorf("Retry-After = %q, want %q", got, "3")
	}
	// JSON body.
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Error != "rate_limit_exceeded" {
		t.Errorf("body.Error = %q, want %q", body.Error, "rate_limit_exceeded")
	}
	if body.RetryAfterSeconds != 3 {
		t.Errorf("body.RetryAfterSeconds = %d, want 3", body.RetryAfterSeconds)
	}
	// User limiter must not have been called.
	if len(userLimiter.calledKeys) != 0 {
		t.Errorf("user limiter called unexpectedly: keys = %v", userLimiter.calledKeys)
	}
	// Structured event must have been emitted via EventLogger.
	if !spy.hasEventType(events.EventTypeRateLimitHit) {
		t.Error("expected rate_limit.hit event but none was logged")
	}
}

func TestRateLimitMiddleware_UserLimitExceeded(t *testing.T) {
	ipLimiter := allowAll()
	retryDuration := time.Second + 500*time.Millisecond // 1.5 s → ceil = 2
	userLimiter := denyWithRetry(retryDuration, 100, 200)
	spy := &fakeEventLogger{}

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), newTestLogger(), spy, nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "10.0.0.2:9999"
	r.Header.Set("X-User-Id", "user-abc")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if nextCalled {
		t.Error("expected next handler NOT to be called, but it was")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	// ceil(1.5) = 2
	got := w.Header().Get("Retry-After")
	if got != "2" {
		t.Errorf("Retry-After = %q, want %q", got, "2")
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.RetryAfterSeconds != 2 {
		t.Errorf("body.RetryAfterSeconds = %d, want 2", body.RetryAfterSeconds)
	}
	// IP limiter was called with the IP; user limiter with the user ID.
	if len(ipLimiter.calledKeys) == 0 || ipLimiter.calledKeys[0] != "10.0.0.2" {
		t.Errorf("ip limiter called with unexpected keys: %v", ipLimiter.calledKeys)
	}
	if len(userLimiter.calledKeys) == 0 || userLimiter.calledKeys[0] != "user-abc" {
		t.Errorf("user limiter called with unexpected keys: %v", userLimiter.calledKeys)
	}
	if !spy.hasEventType(events.EventTypeRateLimitHit) {
		t.Error("expected rate_limit.hit event but none was logged")
	}
}

func TestRateLimitMiddleware_UnauthenticatedSkipsUserLimiter(t *testing.T) {
	ipLimiter := allowAll()
	userLimiter := allowAll()
	logger := newTestLogger()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), logger, nil, nil, nil)
	handler := mw(next)

	// No X-User-Id header — unauthenticated request.
	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "10.0.0.3:9999"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if len(userLimiter.calledKeys) != 0 {
		t.Errorf("user limiter should not be called for unauthenticated requests, got keys: %v", userLimiter.calledKeys)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_ExemptPath(t *testing.T) {
	// Both limiters deny — but exempt paths must bypass all checks.
	ipLimiter := denyWithRetry(5*time.Second, 10, 20)
	userLimiter := denyWithRetry(5*time.Second, 100, 200)
	logger := newTestLogger()

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), logger, nil, nil, nil)
	handler := mw(next)

	// /_vibewarden/* is always exempt.
	r := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
	r.RemoteAddr = "10.0.0.4:9999"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if !nextCalled {
		t.Error("expected next handler to be called for exempt path")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for exempt path, got %d", w.Code)
	}
	// Neither limiter should be called.
	if len(ipLimiter.calledKeys) != 0 {
		t.Errorf("ip limiter should not be called for exempt path, got keys: %v", ipLimiter.calledKeys)
	}
}

func TestRateLimitMiddleware_CustomExemptPath(t *testing.T) {
	ipLimiter := denyWithRetry(5*time.Second, 10, 20)
	userLimiter := denyWithRetry(5*time.Second, 100, 200)
	logger := newTestLogger()

	cfg := ports.RateLimitConfig{
		Enabled:     true,
		ExemptPaths: []string{"/public/*"},
	}

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, cfg, logger, nil, nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/public/logo.png", nil)
	r.RemoteAddr = "10.0.0.5:9999"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if !nextCalled {
		t.Error("expected next handler to be called for custom exempt path")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for exempt path, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_429ContentType(t *testing.T) {
	ipLimiter := denyWithRetry(time.Second, 10, 20)
	userLimiter := allowAll()
	logger := newTestLogger()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), logger, nil, nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api", nil)
	r.RemoteAddr = "10.0.0.6:9999"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestRateLimitMiddleware_TrustProxyHeader(t *testing.T) {
	ipLimiter := allowAll()
	userLimiter := allowAll()
	logger := newTestLogger()

	cfg := ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: true,
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, cfg, logger, nil, nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Forwarded-For", "203.0.113.99")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	// IP limiter must have been called with the XFF IP, not RemoteAddr.
	if len(ipLimiter.calledKeys) == 0 {
		t.Fatal("ip limiter was not called")
	}
	if ipLimiter.calledKeys[0] != "203.0.113.99" {
		t.Errorf("ip limiter called with %q, want %q", ipLimiter.calledKeys[0], "203.0.113.99")
	}
}

func TestRateLimitMiddleware_RetryAfterRoundsUp(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter time.Duration
		wantHeader string
	}{
		{"exact second", 3 * time.Second, "3"},
		{"fractional rounds up", 2500 * time.Millisecond, "3"},
		{"sub-second rounds up to 1", 100 * time.Millisecond, "1"},
		{"zero stays zero", 0, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipLimiter := denyWithRetry(tt.retryAfter, 10, 20)
			logger := newTestLogger()
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

			mw := RateLimitMiddleware(ipLimiter, allowAll(), defaultCfg(), logger, nil, nil, nil)
			handler := mw(next)

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = "1.2.3.4:1234"
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, r)

			got := w.Header().Get("Retry-After")
			if got != tt.wantHeader {
				t.Errorf("Retry-After = %q, want %q", got, tt.wantHeader)
			}

			// Also verify JSON body matches.
			var body ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to decode response body: %v", err)
			}
			wantSeconds, _ := strconv.Atoi(tt.wantHeader)
			if body.RetryAfterSeconds != wantSeconds {
				t.Errorf("body.RetryAfterSeconds = %d, want %d", body.RetryAfterSeconds, wantSeconds)
			}
		})
	}
}

func TestRateLimitMiddleware_StructuredLogEvent(t *testing.T) {
	retryDuration := 2 * time.Second
	ipLimiter := denyWithRetry(retryDuration, 10, 20)
	spy := &fakeEventLogger{}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mw := RateLimitMiddleware(ipLimiter, allowAll(), defaultCfg(), newTestLogger(), spy, nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if !spy.hasEventType(events.EventTypeRateLimitHit) {
		t.Error("expected rate_limit.hit event but none was logged")
	}

	if len(spy.logged) == 0 {
		t.Fatal("no events were logged")
	}
	ev := spy.logged[0]
	if ev.SchemaVersion != events.SchemaVersion {
		t.Errorf("schema_version = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
	}
	if ev.Payload["limit_type"] != "ip" {
		t.Errorf("payload.limit_type = %v, want %q", ev.Payload["limit_type"], "ip")
	}
	if ev.Payload["path"] != "/api/resource" {
		t.Errorf("payload.path = %v, want %q", ev.Payload["path"], "/api/resource")
	}
}

func TestRateLimitMiddleware_EmptyClientIP_Returns403(t *testing.T) {
	// Both limiters allow — the request must be rejected before reaching them
	// because the client IP cannot be determined.
	ipLimiter := allowAll()
	userLimiter := allowAll()
	spy := &fakeEventLogger{}

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), newTestLogger(), spy, nil, nil)
	handler := mw(next)

	// RemoteAddr with no port causes net.SplitHostPort to fail, which makes
	// ExtractClientIP return "".
	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "no-port-addr"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if nextCalled {
		t.Error("expected next handler NOT to be called when client IP is empty")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden when client IP is empty, got %d", w.Code)
	}
	// Neither rate limiter should have been invoked.
	if len(ipLimiter.calledKeys) != 0 {
		t.Errorf("ip limiter should not be called when client IP is empty, got keys: %v", ipLimiter.calledKeys)
	}
	if len(userLimiter.calledKeys) != 0 {
		t.Errorf("user limiter should not be called when client IP is empty, got keys: %v", userLimiter.calledKeys)
	}
	// A structured event must have been emitted.
	if !spy.hasEventType(events.EventTypeRateLimitUnidentified) {
		t.Errorf("expected rate_limit.unidentified_client event, got events: %v", spy.logged)
	}
}

func TestRateLimitMiddleware_AuthenticatedBothLimitsChecked(t *testing.T) {
	ipLimiter := allowAll()
	userLimiter := allowAll()
	logger := newTestLogger()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), logger, nil, nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "10.0.0.7:8080"
	r.Header.Set("X-User-Id", "user-xyz")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if len(ipLimiter.calledKeys) == 0 {
		t.Error("ip limiter was not called for authenticated request")
	}
	if len(userLimiter.calledKeys) == 0 {
		t.Error("user limiter was not called for authenticated request")
	}
	if userLimiter.calledKeys[0] != "user-xyz" {
		t.Errorf("user limiter called with %q, want %q", userLimiter.calledKeys[0], "user-xyz")
	}
}

func TestRateLimitMiddleware_429IncludesRequestID(t *testing.T) {
	// When tracing is disabled, a 429 response must include a request_id field.
	ipLimiter := denyWithRetry(time.Second, 10, 20)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mw := RateLimitMiddleware(ipLimiter, allowAll(), defaultCfg(), newTestLogger(), nil, nil, nil)
	handler := mw(next)

	r := httptest.NewRequest(http.MethodGet, "/api", nil)
	r.RemoteAddr = "10.0.0.9:9999"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.RequestID == "" && body.TraceID == "" {
		t.Error("expected request_id or trace_id in 429 response body")
	}
	if body.Error != "rate_limit_exceeded" {
		t.Errorf("error = %q, want %q", body.Error, "rate_limit_exceeded")
	}
	if body.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", body.Status, http.StatusTooManyRequests)
	}
}

func TestRateLimitMiddleware_403ForbiddenIsJSON(t *testing.T) {
	// When the client IP cannot be determined, the 403 response must be JSON.
	ipLimiter := allowAll()
	userLimiter := allowAll()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), newTestLogger(), nil, nil, nil)
	handler := mw(next)

	// RemoteAddr with no port causes ExtractClientIP to return "".
	r := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	r.RemoteAddr = "no-port-addr"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Error != "forbidden" {
		t.Errorf("error = %q, want %q", body.Error, "forbidden")
	}
	if body.RequestID == "" && body.TraceID == "" {
		t.Error("expected request_id or trace_id in 403 response body")
	}
}

// TestRateLimitMiddleware_ContextIdentityUsedBeforeHeader is the behavioral
// regression guard for issue #1279.
//
// It asserts that per-user rate limiting FIRES for an authenticated request
// when identity is in the request context (set by AuthMiddleware) but the
// X-User-Id header has NOT yet been injected (i.e. IdentityHeadersMiddleware
// has not run). This validates that the ordering dependency is eliminated.
//
// The test also verifies the converse: without the context identity AND without
// the header, the user limiter is NOT called (unauthenticated request).
func TestRateLimitMiddleware_ContextIdentityUsedBeforeHeader(t *testing.T) {
	ident, err := identity.NewIdentity("user-ctx-001", "ctx@example.com", "kratos", true, nil)
	if err != nil {
		t.Fatalf("creating test identity: %v", err)
	}

	tests := []struct {
		name              string
		setupRequest      func(r *http.Request) *http.Request
		wantUserLimiterID string // empty means user limiter must NOT be called
	}{
		{
			name: "context identity present, no X-User-Id header — user limiter fires with correct ID",
			setupRequest: func(r *http.Request) *http.Request {
				// Store identity in context only — no header set.
				ctx := contextWithIdentity(r.Context(), ident)
				return r.WithContext(ctx)
			},
			wantUserLimiterID: "user-ctx-001",
		},
		{
			name: "X-User-Id header present, no context identity — user limiter fires (fallback path)",
			setupRequest: func(r *http.Request) *http.Request {
				r.Header.Set("X-User-Id", "user-hdr-002")
				return r
			},
			wantUserLimiterID: "user-hdr-002",
		},
		{
			name: "context identity takes precedence over X-User-Id header",
			setupRequest: func(r *http.Request) *http.Request {
				ctx := contextWithIdentity(r.Context(), ident)
				r = r.WithContext(ctx)
				// Set a different ID in the header; context must win.
				r.Header.Set("X-User-Id", "user-hdr-should-be-ignored")
				return r
			},
			wantUserLimiterID: "user-ctx-001",
		},
		{
			name: "neither context identity nor header — unauthenticated, user limiter not called",
			setupRequest: func(r *http.Request) *http.Request {
				return r // no context identity, no header
			},
			wantUserLimiterID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipLimiter := allowAll()
			userLimiter := allowAll()

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), newTestLogger(), nil, nil, nil)
			handler := mw(next)

			r := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
			r.RemoteAddr = "10.0.0.1:9999"
			r = tt.setupRequest(r)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}

			if tt.wantUserLimiterID == "" {
				if len(userLimiter.calledKeys) != 0 {
					t.Errorf("user limiter should not be called for unauthenticated request, got keys: %v", userLimiter.calledKeys)
				}
				return
			}

			if len(userLimiter.calledKeys) == 0 {
				t.Fatalf("user limiter was not called — per-user rate limiting silently skipped (regression of #1279)")
			}
			if userLimiter.calledKeys[0] != tt.wantUserLimiterID {
				t.Errorf("user limiter called with %q, want %q", userLimiter.calledKeys[0], tt.wantUserLimiterID)
			}
		})
	}
}

// TestRateLimitMiddleware_ContextIdentityTriggersUserLimit_BehavioralGuard is
// the crux behavioral test for issue #1279. It asserts that an authenticated
// user exceeding the per-user limit gets a 429 even when their IP is under
// the per-IP limit AND even when the X-User-Id header has not been set (i.e.
// identity comes solely from the request context).
//
// This guards against the silent no-op regression where per-user rate limiting
// was skipped entirely when RateLimitMiddleware ran before IdentityHeadersMiddleware.
func TestRateLimitMiddleware_ContextIdentityTriggersUserLimit_BehavioralGuard(t *testing.T) {
	ident, err := identity.NewIdentity("user-behavioral-test", "beh@example.com", "kratos", true, nil)
	if err != nil {
		t.Fatalf("creating test identity: %v", err)
	}

	// IP limit is generous — the user's IP will never be blocked.
	// User limit is tight — the user is blocked.
	ipLimiter := allowAll()
	userLimiter := denyWithRetry(2*time.Second, 5, 10)

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RateLimitMiddleware(ipLimiter, userLimiter, defaultCfg(), newTestLogger(), nil, nil, nil)
	handler := mw(next)

	// Build request with identity ONLY in context — no X-User-Id header.
	r := httptest.NewRequest(http.MethodGet, "/api/sensitive", nil)
	r.RemoteAddr = "10.1.2.3:8888"
	ctx := contextWithIdentity(r.Context(), ident)
	r = r.WithContext(ctx)
	// Deliberately do NOT set X-User-Id header to simulate RateLimitMiddleware
	// running before IdentityHeadersMiddleware.

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if nextCalled {
		t.Error("next handler must NOT be called when user limit is exceeded")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests, got %d — per-user limit was silently skipped (regression of #1279)", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header in 429 response")
	}

	// Confirm the user limiter was called with the correct ID from context.
	if len(userLimiter.calledKeys) == 0 {
		t.Fatal("user limiter was not called — per-user limiting silent no-op (regression of #1279)")
	}
	if userLimiter.calledKeys[0] != "user-behavioral-test" {
		t.Errorf("user limiter called with %q, want %q", userLimiter.calledKeys[0], "user-behavioral-test")
	}
}

// TestRateLimitMiddleware_AuditActorUserID is the regression guard for issue #1402.
//
// A per-user rate-limit hit must record which user tripped the limit in
// audit.Actor.UserID. Per-IP hits on unauthenticated requests carry no user ID.
func TestRateLimitMiddleware_AuditActorUserID(t *testing.T) {
	ident, err := identity.NewIdentity("user-audit-001", "audit@example.com", "kratos", true, nil)
	if err != nil {
		t.Fatalf("creating test identity: %v", err)
	}

	tests := []struct {
		name          string
		ipLimiter     *fakeRateLimiter
		userLimiter   *fakeRateLimiter
		setupRequest  func(r *http.Request) *http.Request
		wantLimitType string
		wantUserID    string
	}{
		{
			name:        "user limit hit with context identity carries user id",
			ipLimiter:   allowAll(),
			userLimiter: denyWithRetry(3*time.Second, 5, 10),
			setupRequest: func(r *http.Request) *http.Request {
				return r.WithContext(contextWithIdentity(r.Context(), ident))
			},
			wantLimitType: "user",
			wantUserID:    "user-audit-001",
		},
		{
			name:        "user limit hit with X-User-Id header carries user id",
			ipLimiter:   allowAll(),
			userLimiter: denyWithRetry(3*time.Second, 5, 10),
			setupRequest: func(r *http.Request) *http.Request {
				r.Header.Set("X-User-Id", "user-hdr-042")
				return r
			},
			wantLimitType: "user",
			wantUserID:    "user-hdr-042",
		},
		{
			name:          "ip limit hit on unauthenticated request has empty user id",
			ipLimiter:     denyWithRetry(3*time.Second, 5, 10),
			userLimiter:   allowAll(),
			setupRequest:  func(r *http.Request) *http.Request { return r },
			wantLimitType: "ip",
			wantUserID:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auditSpy := &fakeAuditEventLogger{}
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			mw := RateLimitMiddleware(tt.ipLimiter, tt.userLimiter, defaultCfg(), newTestLogger(), nil, auditSpy, nil)
			handler := mw(next)

			r := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
			r.RemoteAddr = "10.0.0.7:9999"
			r = tt.setupRequest(r)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, r)

			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("expected 429, got %d", w.Code)
			}

			ev, ok := auditSpy.lastEventOfType(audit.EventTypeRateLimitHit)
			if !ok {
				t.Fatalf("no %s audit event emitted", audit.EventTypeRateLimitHit)
			}
			if got := ev.Details["limit_type"]; got != tt.wantLimitType {
				t.Errorf("audit limit_type = %v, want %q", got, tt.wantLimitType)
			}
			if ev.Actor.UserID != tt.wantUserID {
				t.Errorf("audit Actor.UserID = %q, want %q", ev.Actor.UserID, tt.wantUserID)
			}
			if ev.Actor.IP != "10.0.0.7" {
				t.Errorf("audit Actor.IP = %q, want %q", ev.Actor.IP, "10.0.0.7")
			}
		})
	}
}
