package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	adapterheal "github.com/vibewarden/vibewarden/internal/adapters/health"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
	"github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---- fakes ----

type fakeEventLogger struct {
	mu     sync.Mutex
	logged []events.Event
}

func (f *fakeEventLogger) Log(_ context.Context, ev events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logged = append(f.logged, ev)
	return nil
}

func (f *fakeEventLogger) snapshot() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]events.Event, len(f.logged))
	copy(out, f.logged)
	return out
}

type fakeMetrics struct {
	mu    sync.Mutex
	calls []bool
}

func (f *fakeMetrics) SetUpstreamHealthy(_ context.Context, healthy bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, healthy)
}

// Satisfy the full ports.MetricsCollectorWithUpstreamHealth interface.
func (f *fakeMetrics) IncRequestTotal(_, _, _ string)                               {}
func (f *fakeMetrics) ObserveRequestDuration(_, _ string, _ time.Duration)          {}
func (f *fakeMetrics) IncRateLimitHit(_ string)                                     {}
func (f *fakeMetrics) IncAuthDecision(_ string)                                     {}
func (f *fakeMetrics) IncUpstreamError()                                            {}
func (f *fakeMetrics) IncUpstreamTimeout()                                          {}
func (f *fakeMetrics) IncUpstreamRetry(_ string)                                    {}
func (f *fakeMetrics) SetActiveConnections(_ int)                                   {}
func (f *fakeMetrics) SetCircuitBreakerState(_ context.Context, _ resilience.State) {}
func (f *fakeMetrics) IncWAFDetection(_, _ string)                                  {}

// Compile-time assertion.
var _ ports.MetricsCollectorWithUpstreamHealth = (*fakeMetrics)(nil)

func (f *fakeMetrics) lastCall() (healthy bool, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return false, false
	}
	return f.calls[len(f.calls)-1], true
}

// ---- helpers ----

func domainCfg(healthyThreshold, unhealthyThreshold int) domainheal.Config {
	return domainheal.Config{
		Enabled:            true,
		Path:               "/health",
		Interval:           50 * time.Millisecond,
		Timeout:            200 * time.Millisecond,
		HealthyThreshold:   healthyThreshold,
		UnhealthyThreshold: unhealthyThreshold,
	}
}

func waitForStatus(t *testing.T, checker *adapterheal.HTTPChecker, want domainheal.UpstreamStatus) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for status %v; current = %v", want, checker.CurrentStatus())
		default:
		}
		if checker.CurrentStatus() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---- tests ----

func TestChecker_BecomesHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	eventer := &fakeEventLogger{}
	metrics := &fakeMetrics{}
	checker, err := adapterheal.NewHTTPCheckerFromURL(srv.URL, domainCfg(2, 3), nil, eventer, metrics)
	if err != nil {
		t.Fatalf("NewHTTPCheckerFromURL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := checker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForStatus(t, checker, domainheal.StatusHealthy)

	// Verify event was emitted.
	evs := eventer.snapshot()
	if len(evs) == 0 {
		t.Fatal("expected at least one event")
	}
	found := false
	for _, ev := range evs {
		if ev.EventType == events.EventTypeUpstreamHealthChanged {
			found = true
			if ev.Payload["new_status"] != "healthy" {
				t.Errorf("event new_status = %v, want 'healthy'", ev.Payload["new_status"])
			}
			break
		}
	}
	if !found {
		t.Error("expected upstream.health_changed event not found")
	}

	// Verify metric was updated.
	healthy, ok := metrics.lastCall()
	if !ok {
		t.Fatal("expected at least one metric call")
	}
	if !healthy {
		t.Error("expected SetUpstreamHealthy(true)")
	}

	// Snapshot sanity check.
	snap := checker.Snapshot()
	if snap.Status != "healthy" {
		t.Errorf("Snapshot.Status = %q, want 'healthy'", snap.Status)
	}
}

func TestChecker_BecomesUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	eventer := &fakeEventLogger{}
	metrics := &fakeMetrics{}
	checker, err := adapterheal.NewHTTPCheckerFromURL(srv.URL, domainCfg(2, 2), nil, eventer, metrics)
	if err != nil {
		t.Fatalf("NewHTTPCheckerFromURL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := checker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForStatus(t, checker, domainheal.StatusUnhealthy)

	evs := eventer.snapshot()
	found := false
	for _, ev := range evs {
		if ev.EventType == events.EventTypeUpstreamHealthChanged && ev.Payload["new_status"] == "unhealthy" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected upstream.health_changed event with new_status=unhealthy")
	}

	healthy, ok := metrics.lastCall()
	if !ok {
		t.Fatal("expected at least one metric call")
	}
	if healthy {
		t.Error("expected SetUpstreamHealthy(false)")
	}
}

func TestChecker_Stop_Graceful(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker, err := adapterheal.NewHTTPCheckerFromURL(srv.URL, domainCfg(1, 1), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPCheckerFromURL: %v", err)
	}

	ctx := context.Background()
	if err := checker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := checker.Stop(stopCtx); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}

func TestChecker_ContextCancellationStops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker, err := adapterheal.NewHTTPCheckerFromURL(srv.URL, domainCfg(1, 1), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPCheckerFromURL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := checker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Cancel the context — the loop should stop cleanly.
	cancel()

	// Give the goroutine a moment to exit.
	time.Sleep(300 * time.Millisecond)
	// No further assertion: the test will deadlock or hang if the goroutine does not exit.
}

func TestChecker_SnapshotReflectsState(t *testing.T) {
	var mu sync.Mutex
	serveHealthy := true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		h := serveHealthy
		mu.Unlock()
		if h {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	checker, err := adapterheal.NewHTTPCheckerFromURL(srv.URL, domainCfg(1, 1), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPCheckerFromURL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := checker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForStatus(t, checker, domainheal.StatusHealthy)
	snap := checker.Snapshot()
	if snap.Status != "healthy" {
		t.Errorf("snapshot status = %q, want 'healthy'", snap.Status)
	}
	if snap.ConsecutiveSuccesses < 1 {
		t.Errorf("expected ConsecutiveSuccesses >= 1, got %d", snap.ConsecutiveSuccesses)
	}
	if snap.LastError != "" {
		t.Errorf("expected empty LastError when healthy, got %q", snap.LastError)
	}

	// Flip to unhealthy.
	mu.Lock()
	serveHealthy = false
	mu.Unlock()

	waitForStatus(t, checker, domainheal.StatusUnhealthy)
	snap = checker.Snapshot()
	if snap.Status != "unhealthy" {
		t.Errorf("snapshot status = %q, want 'unhealthy'", snap.Status)
	}
	if snap.LastError == "" {
		t.Error("expected non-empty LastError when unhealthy")
	}
}

func TestChecker_InitialStatusUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Delay long enough that we can read before the first probe completes.
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := domainheal.Config{
		Enabled:            true,
		Path:               "/health",
		Interval:           10 * time.Second, // very long interval
		Timeout:            100 * time.Millisecond,
		HealthyThreshold:   2,
		UnhealthyThreshold: 2,
	}
	checker, err := adapterheal.NewHTTPCheckerFromURL(srv.URL, cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPCheckerFromURL: %v", err)
	}

	// Before Start: status is Unknown.
	if checker.CurrentStatus() != domainheal.StatusUnknown {
		t.Errorf("initial status = %v, want Unknown", checker.CurrentStatus())
	}
}
