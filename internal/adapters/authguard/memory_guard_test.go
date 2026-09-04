package authguard_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "github.com/vibewarden/vibewarden/internal/adapters/authguard"
	domain "github.com/vibewarden/vibewarden/internal/domain/authguard"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeClock is a manually advanced time source for deterministic tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func testPolicy() domain.Policy {
	return domain.Policy{
		Threshold: 3,
		Window:    time.Minute,
		Cooldown:  2 * time.Minute,
	}
}

// newTestGuard builds a MemoryGuard on a fake clock with the small test policy.
func newTestGuard(t *testing.T, maxEntries int) (*adapter.MemoryGuard, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	g, err := adapter.NewMemoryGuard(testPolicy(), maxEntries, adapter.WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewMemoryGuard: %v", err)
	}
	return g, clock
}

func TestNewMemoryGuard_Validation(t *testing.T) {
	tests := []struct {
		name       string
		policy     domain.Policy
		maxEntries int
		wantErr    bool
	}{
		{"valid", domain.DefaultPolicy(), 10, false},
		{"zero threshold", domain.Policy{Threshold: 0, Window: time.Minute, Cooldown: time.Minute}, 10, true},
		{"zero window", domain.Policy{Threshold: 1, Window: 0, Cooldown: time.Minute}, 10, true},
		{"zero cooldown", domain.Policy{Threshold: 1, Window: time.Minute, Cooldown: 0}, 10, true},
		{"zero max entries", domain.DefaultPolicy(), 0, true},
		{"negative max entries", domain.DefaultPolicy(), -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := adapter.NewMemoryGuard(tt.policy, tt.maxEntries)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMemoryGuard() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewDefaultMemoryGuard(t *testing.T) {
	g, err := adapter.NewDefaultMemoryGuard()
	if err != nil {
		t.Fatalf("NewDefaultMemoryGuard: %v", err)
	}
	st := g.Status("10.0.0.1")
	if st.Threshold != domain.DefaultThreshold {
		t.Errorf("Threshold = %d, want %d", st.Threshold, domain.DefaultThreshold)
	}
	if st.Window != domain.DefaultWindow {
		t.Errorf("Window = %v, want %v", st.Window, domain.DefaultWindow)
	}
	if st.Cooldown != domain.DefaultCooldown {
		t.Errorf("Cooldown = %v, want %v", st.Cooldown, domain.DefaultCooldown)
	}
}

func TestMemoryGuard_UnderThresholdIsNotLockedOut(t *testing.T) {
	g, _ := newTestGuard(t, 100)

	for i := 1; i < testPolicy().Threshold; i++ {
		st := g.RecordFailure("10.0.0.1")
		if st.Tripped {
			t.Fatalf("failure %d tripped the lockout early", i)
		}
		if st.LockedOut {
			t.Fatalf("failure %d reported LockedOut before the threshold", i)
		}
		if st.Failures != i {
			t.Errorf("failure %d: Failures = %d, want %d", i, st.Failures, i)
		}
	}

	if st := g.Status("10.0.0.1"); st.LockedOut {
		t.Error("Status reported LockedOut below the threshold")
	}
}

func TestMemoryGuard_ThresholdTripsExactlyOnce(t *testing.T) {
	g, _ := newTestGuard(t, 100)
	p := testPolicy()

	var trips int
	for i := 1; i <= p.Threshold; i++ {
		if g.RecordFailure("10.0.0.1").Tripped {
			trips++
			if i != p.Threshold {
				t.Errorf("tripped on failure %d, want %d", i, p.Threshold)
			}
		}
	}
	if trips != 1 {
		t.Errorf("trips = %d, want exactly 1", trips)
	}

	// The tripping call itself must not report LockedOut: that request is still
	// answered with 401, not 429.
	st := g.Status("10.0.0.1")
	if !st.LockedOut {
		t.Fatal("expected LockedOut after the threshold was reached")
	}
	if st.RetryAfter != p.Cooldown {
		t.Errorf("RetryAfter = %v, want %v", st.RetryAfter, p.Cooldown)
	}

	// Further failures during the cooldown never trip again.
	for i := 0; i < 5; i++ {
		if g.RecordFailure("10.0.0.1").Tripped {
			t.Fatal("a second trip was reported during the same lockout episode")
		}
	}
}

func TestMemoryGuard_TrippingCallReportsRetryAfterButNotLockedOut(t *testing.T) {
	g, _ := newTestGuard(t, 100)
	p := testPolicy()

	var st ports.LockoutStatus
	for i := 0; i < p.Threshold; i++ {
		st = g.RecordFailure("10.0.0.1")
	}

	if !st.Tripped {
		t.Fatal("expected Tripped on the threshold failure")
	}
	if st.LockedOut {
		t.Error("the tripping call must not report LockedOut; that request still gets 401")
	}
	if st.RetryAfter != p.Cooldown {
		t.Errorf("RetryAfter = %v, want %v", st.RetryAfter, p.Cooldown)
	}
	if st.Failures != p.Threshold {
		t.Errorf("Failures = %d, want %d", st.Failures, p.Threshold)
	}
}

func TestMemoryGuard_CooldownExpiryAllowsRetry(t *testing.T) {
	g, clock := newTestGuard(t, 100)
	p := testPolicy()

	for i := 0; i < p.Threshold; i++ {
		g.RecordFailure("10.0.0.1")
	}
	if !g.Status("10.0.0.1").LockedOut {
		t.Fatal("expected the client to be locked out")
	}

	clock.Advance(p.Cooldown - time.Second)
	if !g.Status("10.0.0.1").LockedOut {
		t.Error("expected the client to still be locked out just before expiry")
	}

	clock.Advance(2 * time.Second)
	st := g.Status("10.0.0.1")
	if st.LockedOut {
		t.Error("expected the lockout to have expired")
	}
	if st.Failures != 0 {
		t.Errorf("Failures = %d, want 0 after cooldown expiry", st.Failures)
	}

	// A fresh streak needs the full threshold again.
	if g.RecordFailure("10.0.0.1").Tripped {
		t.Error("the first failure after cooldown expiry must not re-trip immediately")
	}
}

func TestMemoryGuard_WindowExpiryRestartsStreak(t *testing.T) {
	g, clock := newTestGuard(t, 100)

	g.RecordFailure("10.0.0.1")
	g.RecordFailure("10.0.0.1")

	clock.Advance(2 * time.Minute) // beyond the 1-minute window

	st := g.RecordFailure("10.0.0.1")
	if st.Tripped {
		t.Error("a failure after the window elapsed must restart the streak, not trip")
	}
	if st.Failures != 1 {
		t.Errorf("Failures = %d, want 1 after the window elapsed", st.Failures)
	}
}

func TestMemoryGuard_SuccessResetsCounter(t *testing.T) {
	g, _ := newTestGuard(t, 100)
	p := testPolicy()

	for i := 0; i < p.Threshold-1; i++ {
		g.RecordFailure("10.0.0.1")
	}

	g.RecordSuccess("10.0.0.1")

	if st := g.Status("10.0.0.1"); st.Failures != 0 {
		t.Errorf("Failures = %d, want 0 after RecordSuccess", st.Failures)
	}
	if got := g.Len(); got != 0 {
		t.Errorf("tracked entries = %d, want 0 after RecordSuccess", got)
	}

	st := g.RecordFailure("10.0.0.1")
	if st.Tripped {
		t.Error("the counter must start from zero after a success")
	}
	if st.Failures != 1 {
		t.Errorf("Failures = %d, want 1 after a success then one failure", st.Failures)
	}
}

func TestMemoryGuard_KeysAreIsolated(t *testing.T) {
	g, _ := newTestGuard(t, 100)
	p := testPolicy()

	for i := 0; i < p.Threshold; i++ {
		g.RecordFailure("10.0.0.1")
	}

	if !g.Status("10.0.0.1").LockedOut {
		t.Fatal("expected 10.0.0.1 to be locked out")
	}
	if g.Status("10.0.0.2").LockedOut {
		t.Error("10.0.0.2 must not be affected by another client's lockout")
	}
	if st := g.RecordFailure("10.0.0.2"); st.Tripped || st.Failures != 1 {
		t.Errorf("10.0.0.2 status = %+v, want an independent first failure", st)
	}
}

func TestMemoryGuard_StatusNeverCreatesOrIncrements(t *testing.T) {
	g, _ := newTestGuard(t, 100)

	for i := 0; i < 100; i++ {
		g.Status(fmt.Sprintf("10.0.0.%d", i))
	}
	if got := g.Len(); got != 0 {
		t.Errorf("tracked entries = %d, want 0 — Status must not allocate", got)
	}

	g.RecordFailure("10.0.0.1")
	for i := 0; i < 50; i++ {
		g.Status("10.0.0.1")
	}
	if st := g.Status("10.0.0.1"); st.Failures != 1 {
		t.Errorf("Failures = %d, want 1 — Status must not increment", st.Failures)
	}
}

func TestMemoryGuard_EmptyKeyIsIgnored(t *testing.T) {
	g, _ := newTestGuard(t, 100)

	if st := g.RecordFailure(""); st.Tripped || st.LockedOut || st.Failures != 0 {
		t.Errorf("RecordFailure(\"\") = %+v, want a zero status", st)
	}
	if st := g.Status(""); st.LockedOut {
		t.Error("Status(\"\") must never report a lockout")
	}
	g.RecordSuccess("")
	if got := g.Len(); got != 0 {
		t.Errorf("tracked entries = %d, want 0 — the empty key must never be tracked", got)
	}
}

func TestMemoryGuard_BoundedUnderKeyFlood(t *testing.T) {
	const maxEntries = 10_000
	clock := newFakeClock()
	g, err := adapter.NewMemoryGuard(testPolicy(), maxEntries, adapter.WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewMemoryGuard: %v", err)
	}

	for i := 0; i < maxEntries+1; i++ {
		g.RecordFailure(fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256))
		if got := g.Len(); got > maxEntries {
			t.Fatalf("tracked entries = %d after %d keys, want <= %d", got, i+1, maxEntries)
		}
	}

	if got := g.Len(); got > maxEntries {
		t.Errorf("tracked entries = %d, want <= %d", got, maxEntries)
	}
}

func TestMemoryGuard_EvictionPrefersIdleEntries(t *testing.T) {
	const maxEntries = 4
	g, clock := newTestGuard(t, maxEntries)
	p := testPolicy()

	// victim is locked out and must survive the sweep.
	for i := 0; i < p.Threshold; i++ {
		g.RecordFailure("10.0.0.1")
	}

	// Fill the rest of the map with single-failure entries, then let their
	// windows expire so the sweep classifies them as idle.
	for i := 2; i <= maxEntries; i++ {
		g.RecordFailure(fmt.Sprintf("10.0.0.%d", i))
	}
	if got := g.Len(); got != maxEntries {
		t.Fatalf("tracked entries = %d, want %d before eviction", got, maxEntries)
	}

	clock.Advance(90 * time.Second) // past the window, inside the cooldown

	// A new key forces makeRoom; the idle single-failure entries go first.
	g.RecordFailure("10.0.1.1")

	if got := g.Len(); got > maxEntries {
		t.Errorf("tracked entries = %d, want <= %d", got, maxEntries)
	}
	if !g.Status("10.0.0.1").LockedOut {
		t.Error("an active lockout must survive an eviction sweep triggered by other keys")
	}
}

func TestMemoryGuard_EvictsOldestWhenNothingIsIdle(t *testing.T) {
	const maxEntries = 3
	g, clock := newTestGuard(t, maxEntries)

	// Three live streaks, each touched at a distinct instant.
	g.RecordFailure("10.0.0.1")
	clock.Advance(time.Second)
	g.RecordFailure("10.0.0.2")
	clock.Advance(time.Second)
	g.RecordFailure("10.0.0.3")

	// A fourth key: nothing is idle, so the least recently seen entry goes.
	g.RecordFailure("10.0.0.4")

	if got := g.Len(); got != maxEntries {
		t.Errorf("tracked entries = %d, want %d", got, maxEntries)
	}
	if st := g.Status("10.0.0.1"); st.Failures != 0 {
		t.Errorf("oldest entry Failures = %d, want 0 (it should have been evicted)", st.Failures)
	}
	if st := g.Status("10.0.0.4"); st.Failures != 1 {
		t.Errorf("newest entry Failures = %d, want 1", st.Failures)
	}
}

// TestMemoryGuard_ConcurrentFailuresTripOnce is the -race guarantee behind
// "exactly one audit.auth.lockout event per lockout episode".
func TestMemoryGuard_ConcurrentFailuresTripOnce(t *testing.T) {
	g, err := adapter.NewMemoryGuard(domain.DefaultPolicy(), adapter.DefaultMaxEntries)
	if err != nil {
		t.Fatalf("NewMemoryGuard: %v", err)
	}

	const goroutines = 64
	var trips atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 20; j++ {
				if g.RecordFailure("10.0.0.1").Tripped {
					trips.Add(1)
				}
				g.Status("10.0.0.1")
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := trips.Load(); got != 1 {
		t.Errorf("trips = %d, want exactly 1 across %d concurrent goroutines", got, goroutines)
	}
}

// Compile-time check that the adapter satisfies the port.
var _ ports.AuthLockoutGuard = (*adapter.MemoryGuard)(nil)
