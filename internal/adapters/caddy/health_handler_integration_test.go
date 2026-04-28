package caddy

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"

	healthadapter "github.com/vibewarden/vibewarden/internal/adapters/health"
	domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
)

// TestHealthHandler_Integration drives a real HTTPChecker against a test HTTP
// server and verifies the HealthHandler renders the expected upstream state
// transitions. No external dependencies — everything runs in-process.
func TestHealthHandler_Integration(t *testing.T) {
	// returnOK is shared between the test goroutine (writer) and the httptest
	// server handler goroutine (reader), so it must be accessed atomically.
	var returnOK atomic.Bool
	returnOK.Store(true)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if returnOK.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer upstream.Close()

	domainCfg := domainheal.Config{
		Enabled:            true,
		Path:               "/health",
		Interval:           20 * time.Millisecond,
		Timeout:            10 * time.Millisecond,
		UnhealthyThreshold: 2,
		HealthyThreshold:   2,
	}

	checker, err := healthadapter.NewHTTPCheckerFromURL(upstream.URL, domainCfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPCheckerFromURL: %v", err)
	}

	if err := checker.Start(t.Context()); err != nil {
		t.Fatalf("checker.Start: %v", err)
	}
	defer func() { _ = checker.Stop(t.Context()) }()

	h := &HealthHandler{
		checker: checker,
	}
	_ = h.ProvisionWith(gocaddy.Context{}, RuntimeServices{UpstreamHealthChecker: checker})

	// Eventually the upstream should be reported as "ok" (after 2 successes).
	assertUpstreamState(t, h, "ok", 2*time.Second)

	// Now make the upstream fail.
	returnOK.Store(false)
	// After 2 failures the state should flip to "failing".
	assertUpstreamState(t, h, "failing", 2*time.Second)

	// Bring the upstream back.
	returnOK.Store(true)
	// After 2 successes the state should return to "ok".
	assertUpstreamState(t, h, "ok", 2*time.Second)
}

// assertUpstreamState polls the handler until components.upstream equals want
// or the deadline expires.
func assertUpstreamState(t *testing.T, h *HealthHandler, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := serveHealthStr(t, h)
		if s == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	// One final check to get the actual value for the error message.
	got := serveHealthStr(t, h)
	if got != want {
		t.Errorf("components.upstream = %q after %v, want %q", got, timeout, want)
	}
}

// serveHealthStr returns the components.upstream string from a handler response.
func serveHealthStr(t *testing.T, h *HealthHandler) string {
	t.Helper()
	out := serveHealth(t, h)
	comps, _ := out["components"].(map[string]any)
	if comps == nil {
		return ""
	}
	s, _ := comps["upstream"].(string)
	return s
}
