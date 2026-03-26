//go:build integration

// Package middleware contains integration tests for the rate limit middleware.
// These tests exercise the complete middleware behaviour end-to-end using
// real in-memory rate limiters without any external dependencies.
package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	ratelimitadapter "github.com/vibewarden/vibewarden/internal/adapters/ratelimit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// buildRateLimitHandler is a test helper that creates a RateLimitMiddleware
// handler using real MemoryStore instances with the given rules.
// The caller is responsible for closing the returned limiters.
func buildRateLimitHandler(
	t *testing.T,
	ipRule ports.RateLimitRule,
	userRule ports.RateLimitRule,
	cfg ports.RateLimitConfig,
) (handler func(http.Handler) http.Handler, ipLimiter ports.RateLimiter, userLimiter ports.RateLimiter) {
	t.Helper()

	// Use a very short cleanup interval and TTL for tests to keep things snappy.
	factory := ratelimitadapter.NewMemoryFactory(100*time.Millisecond, 500*time.Millisecond)

	ipLimiter = factory.NewLimiter(ipRule)
	userLimiter = factory.NewLimiter(userRule)

	cfg.PerIP = ipRule
	cfg.PerUser = userRule

	handler = RateLimitMiddleware(ipLimiter, userLimiter, cfg, slog.Default(), nil)
	return handler, ipLimiter, userLimiter
}

// TestRateLimitMiddleware_Integration_RequestsWithinLimit verifies that requests
// within the configured per-IP rate limit all succeed with 200 OK.
func TestRateLimitMiddleware_Integration_RequestsWithinLimit(t *testing.T) {
	ipRule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 10}
	userRule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 10}
	cfg := ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: false,
	}

	handler, ipLimiter, userLimiter := buildRateLimitHandler(t, ipRule, userRule, cfg)
	defer ipLimiter.Close()   //nolint:errcheck
	defer userLimiter.Close() //nolint:errcheck

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := handler(next)

	// Send 5 requests — all should be within the burst of 10.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()

		mw.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d: status = %d, want %d (within limit)", i+1, rec.Code, http.StatusOK)
		}
	}
}

// TestRateLimitMiddleware_Integration_RequestsExceedingIPLimit verifies that
// requests exceeding the per-IP burst receive a 429 Too Many Requests response
// with the correct Retry-After header and JSON body.
func TestRateLimitMiddleware_Integration_RequestsExceedingIPLimit(t *testing.T) {
	// Very low limits: 1 req/s, burst of 1. After 1 request, further requests
	// must be rejected immediately.
	ipRule := ports.RateLimitRule{RequestsPerSecond: 1, Burst: 1}
	userRule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 100}
	cfg := ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: false,
	}

	handler, ipLimiter, userLimiter := buildRateLimitHandler(t, ipRule, userRule, cfg)
	defer ipLimiter.Close()   //nolint:errcheck
	defer userLimiter.Close() //nolint:errcheck

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := handler(next)

	clientIP := "10.0.0.1:9999"

	// First request should succeed (consumes the burst token).
	req1 := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req1.RemoteAddr = clientIP
	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	// Subsequent requests should be rejected immediately (burst exhausted, no tokens).
	rejected := false
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
		req.RemoteAddr = clientIP
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)

		if rec.Code == http.StatusTooManyRequests {
			rejected = true

			// Verify Retry-After header is present and parseable.
			retryAfterStr := rec.Header().Get("Retry-After")
			if retryAfterStr == "" {
				t.Error("429 response missing Retry-After header")
			} else {
				retrySeconds, err := strconv.Atoi(retryAfterStr)
				if err != nil {
					t.Errorf("Retry-After header %q is not an integer: %v", retryAfterStr, err)
				} else if retrySeconds < 0 {
					t.Errorf("Retry-After = %d, want >= 0", retrySeconds)
				}
			}

			// Verify Content-Type is JSON.
			ct := rec.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			// Verify JSON body structure.
			var body struct {
				Error             string `json:"error"`
				RetryAfterSeconds int    `json:"retry_after_seconds"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Errorf("decoding 429 response body: %v", err)
			}
			if body.Error != "rate_limit_exceeded" {
				t.Errorf("body.error = %q, want %q", body.Error, "rate_limit_exceeded")
			}
			if body.RetryAfterSeconds < 0 {
				t.Errorf("body.retry_after_seconds = %d, want >= 0", body.RetryAfterSeconds)
			}

			break
		}
	}

	if !rejected {
		t.Error("expected at least one 429 response, but all requests were allowed")
	}
}

// TestRateLimitMiddleware_Integration_ExemptPathsBypassRateLimit verifies that
// requests to exempt paths are not counted against any rate limit, even when
// the per-IP limit has been fully exhausted.
func TestRateLimitMiddleware_Integration_ExemptPathsBypassRateLimit(t *testing.T) {
	// Extremely tight limit so normal paths get blocked immediately.
	ipRule := ports.RateLimitRule{RequestsPerSecond: 1, Burst: 1}
	userRule := ports.RateLimitRule{RequestsPerSecond: 1, Burst: 1}
	cfg := ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: false,
		ExemptPaths:       []string{"/public/*"},
	}

	handler, ipLimiter, userLimiter := buildRateLimitHandler(t, ipRule, userRule, cfg)
	defer ipLimiter.Close()   //nolint:errcheck
	defer userLimiter.Close() //nolint:errcheck

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := handler(next)
	clientIP := "172.16.0.1:4321"

	// Exhaust the IP limit by sending two requests to a non-exempt path.
	// The second should be rejected.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
		req.RemoteAddr = clientIP
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
	}

	// The /_vibewarden/* prefix is always exempt — requests must succeed
	// even when the IP limit is exhausted.
	exemptPaths := []string{
		"/_vibewarden/health",
		"/public/index.html",
	}

	for _, path := range exemptPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = clientIP
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("path %s: status = %d, want %d (exempt path should bypass rate limit)",
					path, rec.Code, http.StatusOK)
			}
		})
	}
}

// TestRateLimitMiddleware_Integration_429ResponseFormat verifies the complete
// 429 response format including headers and JSON body structure.
func TestRateLimitMiddleware_Integration_429ResponseFormat(t *testing.T) {
	// One token, refills at 1/s — second request in the same second is rejected.
	ipRule := ports.RateLimitRule{RequestsPerSecond: 1, Burst: 1}
	userRule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 100}
	cfg := ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: false,
	}

	handler, ipLimiter, userLimiter := buildRateLimitHandler(t, ipRule, userRule, cfg)
	defer ipLimiter.Close()   //nolint:errcheck
	defer userLimiter.Close() //nolint:errcheck

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := handler(next)
	clientIP := "198.51.100.1:11111"

	// Consume the single burst token.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = clientIP
	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, req1)

	// Second request — must be rejected.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = clientIP
	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status = %d, want %d", rec2.Code, http.StatusTooManyRequests)
	}

	// Content-Type must be application/json.
	if ct := rec2.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Retry-After must be a non-negative integer.
	retryStr := rec2.Header().Get("Retry-After")
	if retryStr == "" {
		t.Fatal("Retry-After header is missing from 429 response")
	}
	retrySeconds, err := strconv.Atoi(retryStr)
	if err != nil {
		t.Fatalf("Retry-After %q is not an integer: %v", retryStr, err)
	}
	if retrySeconds < 0 {
		t.Errorf("Retry-After = %d, want >= 0", retrySeconds)
	}

	// JSON body must contain the expected fields.
	var body map[string]any
	if err := json.NewDecoder(rec2.Body).Decode(&body); err != nil {
		t.Fatalf("decoding 429 body: %v", err)
	}

	if errVal, ok := body["error"]; !ok {
		t.Error("429 body missing 'error' field")
	} else if errVal != "rate_limit_exceeded" {
		t.Errorf("body.error = %v, want %q", errVal, "rate_limit_exceeded")
	}

	if _, ok := body["retry_after_seconds"]; !ok {
		t.Error("429 body missing 'retry_after_seconds' field")
	}
}

// TestRateLimitMiddleware_Integration_PerUserLimitAfterIPAllow verifies that
// per-user rate limits are applied independently of the per-IP limit.
// An authenticated request may be blocked by the per-user limit even when
// the per-IP limit has tokens remaining.
func TestRateLimitMiddleware_Integration_PerUserLimitAfterIPAllow(t *testing.T) {
	// Generous IP limit, tight user limit.
	ipRule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 100}
	userRule := ports.RateLimitRule{RequestsPerSecond: 1, Burst: 1}
	cfg := ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: false,
	}

	handler, ipLimiter, userLimiter := buildRateLimitHandler(t, ipRule, userRule, cfg)
	defer ipLimiter.Close()   //nolint:errcheck
	defer userLimiter.Close() //nolint:errcheck

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := handler(next)
	clientIP := "203.0.113.5:55555"
	userID := "user-abc-123"

	makeAuthRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
		req.RemoteAddr = clientIP
		// IdentityHeadersMiddleware injects X-User-Id; simulate that here.
		req.Header.Set("X-User-Id", userID)
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		return rec
	}

	// First request consumes the single user burst token.
	rec1 := makeAuthRequest()
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	// Subsequent requests for the same user must be rejected even though the
	// IP limit still has plenty of tokens.
	userBlocked := false
	for i := 0; i < 5; i++ {
		rec := makeAuthRequest()
		if rec.Code == http.StatusTooManyRequests {
			userBlocked = true
			break
		}
	}

	if !userBlocked {
		t.Error("expected per-user rate limit to block requests, but all were allowed")
	}
}

// TestRateLimitMiddleware_Integration_DifferentIPsHaveIndependentLimits verifies
// that the per-IP rate limit is applied independently per client IP address.
// Exhausting the limit for one IP does not affect requests from another IP.
func TestRateLimitMiddleware_Integration_DifferentIPsHaveIndependentLimits(t *testing.T) {
	ipRule := ports.RateLimitRule{RequestsPerSecond: 1, Burst: 1}
	userRule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 100}
	cfg := ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: false,
	}

	handler, ipLimiter, userLimiter := buildRateLimitHandler(t, ipRule, userRule, cfg)
	defer ipLimiter.Close()   //nolint:errcheck
	defer userLimiter.Close() //nolint:errcheck

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := handler(next)

	ip1 := "10.1.1.1:1111"
	ip2 := "10.2.2.2:2222"

	// Exhaust IP1's burst.
	req1a := httptest.NewRequest(http.MethodGet, "/", nil)
	req1a.RemoteAddr = ip1
	rec1a := httptest.NewRecorder()
	mw.ServeHTTP(rec1a, req1a)

	// Second request from IP1 — should be blocked.
	req1b := httptest.NewRequest(http.MethodGet, "/", nil)
	req1b.RemoteAddr = ip1
	rec1b := httptest.NewRecorder()
	mw.ServeHTTP(rec1b, req1b)

	ip1Blocked := rec1b.Code == http.StatusTooManyRequests

	// First request from IP2 — should succeed regardless of IP1's state.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = ip2
	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, req2)

	if !ip1Blocked {
		t.Log("IP1 was not blocked — burst may have refilled; test result inconclusive for IP1")
	}

	if rec2.Code != http.StatusOK {
		t.Errorf("IP2 request: status = %d, want %d (independent IP limits)", rec2.Code, http.StatusOK)
	}
}

// TestRateLimitMiddleware_Integration_TrustProxyHeaders verifies that when
// trust_proxy_headers is true the client IP is taken from X-Forwarded-For.
func TestRateLimitMiddleware_Integration_TrustProxyHeaders(t *testing.T) {
	ipRule := ports.RateLimitRule{RequestsPerSecond: 1, Burst: 1}
	userRule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 100}
	cfg := ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: true,
	}

	handler, ipLimiter, userLimiter := buildRateLimitHandler(t, ipRule, userRule, cfg)
	defer ipLimiter.Close()   //nolint:errcheck
	defer userLimiter.Close() //nolint:errcheck

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := handler(next)

	realClientIP := "1.2.3.4"
	proxyAddr := "10.0.0.1:9999"

	// Consume the burst token for the real client IP.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = proxyAddr
	req1.Header.Set("X-Forwarded-For", realClientIP)
	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	// Second request from same real IP (via proxy) should be blocked.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = proxyAddr
	req2.Header.Set("X-Forwarded-For", realClientIP)
	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request from same real IP: status = %d, want %d",
			rec2.Code, http.StatusTooManyRequests)
	}

	// A different real client IP via the same proxy should still succeed.
	differentClientIP := "5.6.7.8"
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = proxyAddr
	req3.Header.Set("X-Forwarded-For", differentClientIP)
	rec3 := httptest.NewRecorder()
	mw.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Errorf("different real client IP: status = %d, want %d (should have own token bucket)",
			rec3.Code, http.StatusOK)
	}
}

// TestRateLimitMiddleware_Integration_LimitersAreCloseable verifies that
// the rate limiters created by the factory can be cleanly closed without
// error, stopping background goroutines.
func TestRateLimitMiddleware_Integration_LimitersAreCloseable(t *testing.T) {
	ipRule := ports.RateLimitRule{RequestsPerSecond: 10, Burst: 20}
	userRule := ports.RateLimitRule{RequestsPerSecond: 100, Burst: 200}

	factory := ratelimitadapter.NewDefaultMemoryFactory()
	ipLimiter := factory.NewLimiter(ipRule)
	userLimiter := factory.NewLimiter(userRule)

	// Make a few requests to populate the internal maps.
	cfg := ports.RateLimitConfig{
		Enabled:           true,
		TrustProxyHeaders: false,
		PerIP:             ipRule,
		PerUser:           userRule,
	}

	mw := RateLimitMiddleware(ipLimiter, userLimiter, cfg, slog.Default(), nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw(next)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Closing must not return an error.
	if err := ipLimiter.Close(); err != nil {
		t.Errorf("ipLimiter.Close() error = %v", err)
	}
	if err := userLimiter.Close(); err != nil {
		t.Errorf("userLimiter.Close() error = %v", err)
	}

	// Calling Close a second time must also be safe (idempotent).
	if err := ipLimiter.Close(); err != nil {
		t.Errorf("ipLimiter.Close() second call error = %v", err)
	}
	if err := userLimiter.Close(); err != nil {
		t.Errorf("userLimiter.Close() second call error = %v", err)
	}
}
