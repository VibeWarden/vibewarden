package authguard_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/authguard"
)

// testPolicy is a small policy so tests do not have to loop ten times.
func testPolicy() authguard.Policy {
	return authguard.Policy{
		Threshold: 3,
		Window:    time.Minute,
		Cooldown:  2 * time.Minute,
	}
}

func TestDefaultPolicy(t *testing.T) {
	p := authguard.DefaultPolicy()
	if p.Threshold != authguard.DefaultThreshold {
		t.Errorf("Threshold = %d, want %d", p.Threshold, authguard.DefaultThreshold)
	}
	if p.Window != authguard.DefaultWindow {
		t.Errorf("Window = %v, want %v", p.Window, authguard.DefaultWindow)
	}
	if p.Cooldown != authguard.DefaultCooldown {
		t.Errorf("Cooldown = %v, want %v", p.Cooldown, authguard.DefaultCooldown)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("DefaultPolicy().Validate() = %v, want nil", err)
	}
}

func TestPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  authguard.Policy
		wantErr bool
	}{
		{"valid", authguard.Policy{Threshold: 1, Window: time.Second, Cooldown: time.Second}, false},
		{"zero threshold", authguard.Policy{Threshold: 0, Window: time.Second, Cooldown: time.Second}, true},
		{"negative threshold", authguard.Policy{Threshold: -1, Window: time.Second, Cooldown: time.Second}, true},
		{"zero window", authguard.Policy{Threshold: 1, Window: 0, Cooldown: time.Second}, true},
		{"negative window", authguard.Policy{Threshold: 1, Window: -time.Second, Cooldown: time.Second}, true},
		{"zero cooldown", authguard.Policy{Threshold: 1, Window: time.Second, Cooldown: 0}, true},
		{"negative cooldown", authguard.Policy{Threshold: 1, Window: time.Second, Cooldown: -time.Second}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAttempts_ZeroValueIsNotLockedOut(t *testing.T) {
	var a authguard.Attempts
	locked, retryAfter := a.LockedOut(time.Now())
	if locked {
		t.Error("zero-value Attempts should not be locked out")
	}
	if retryAfter != 0 {
		t.Errorf("retryAfter = %v, want 0", retryAfter)
	}
	if a.Failures() != 0 {
		t.Errorf("Failures() = %d, want 0", a.Failures())
	}
	if !a.LockedUntil().IsZero() {
		t.Errorf("LockedUntil() = %v, want zero time", a.LockedUntil())
	}
}

// TestAttempts_FailureSequences drives a scripted sequence of failures against
// a fresh Attempts and asserts which offsets trip the lockout. Offsets are
// relative to a fixed base instant, so all timing is deterministic.
func TestAttempts_FailureSequences(t *testing.T) {
	base := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	p := testPolicy()

	tests := []struct {
		name string
		// offsets at which a failure is recorded.
		offsets []time.Duration
		// wantTripped[i] is the expected tripped result for offsets[i].
		wantTripped []bool
		// wantFailures is the streak length after the last recorded failure.
		wantFailures int
	}{
		{
			name:         "under threshold does not trip",
			offsets:      []time.Duration{0, time.Second, 2 * time.Second},
			wantTripped:  []bool{false, false, true},
			wantFailures: 3,
		},
		{
			name:         "two failures leave the counter armed but not tripped",
			offsets:      []time.Duration{0, time.Second},
			wantTripped:  []bool{false, false},
			wantFailures: 2,
		},
		{
			name:         "failure after the window restarts the streak",
			offsets:      []time.Duration{0, 10 * time.Second, 70 * time.Second},
			wantTripped:  []bool{false, false, false},
			wantFailures: 1,
		},
		{
			name:         "window is anchored on the first failure of the streak",
			offsets:      []time.Duration{0, 59 * time.Second, 61 * time.Second},
			wantTripped:  []bool{false, false, false},
			wantFailures: 1,
		},
		{
			name:         "streak survives a failure exactly at the window edge",
			offsets:      []time.Duration{0, 30 * time.Second, time.Minute},
			wantTripped:  []bool{false, false, true},
			wantFailures: 3,
		},
		{
			name:         "failures during cooldown are not counted",
			offsets:      []time.Duration{0, 0, 0, time.Second, 2 * time.Second},
			wantTripped:  []bool{false, false, true, false, false},
			wantFailures: 3,
		},
		{
			name: "cooldown expiry resets the counter to zero",
			offsets: []time.Duration{
				0, 0, 0, // trips at the third failure
				3 * time.Minute, // after the 2-minute cooldown
			},
			wantTripped:  []bool{false, false, true, false},
			wantFailures: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a authguard.Attempts
			for i, off := range tt.offsets {
				got := a.RecordFailure(base.Add(off), p)
				if got != tt.wantTripped[i] {
					t.Errorf("RecordFailure(+%v) tripped = %v, want %v", off, got, tt.wantTripped[i])
				}
			}
			if a.Failures() != tt.wantFailures {
				t.Errorf("Failures() = %d, want %d", a.Failures(), tt.wantFailures)
			}
		})
	}
}

func TestAttempts_LockedOutLifecycle(t *testing.T) {
	base := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	p := testPolicy()

	var a authguard.Attempts
	for i := 0; i < p.Threshold; i++ {
		a.RecordFailure(base, p)
	}

	tests := []struct {
		name           string
		at             time.Duration
		wantLocked     bool
		wantRetryAfter time.Duration
	}{
		{"immediately after tripping", 0, true, 2 * time.Minute},
		{"halfway through the cooldown", time.Minute, true, time.Minute},
		{"one nanosecond before expiry", 2*time.Minute - time.Nanosecond, true, time.Nanosecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locked, retryAfter := a.LockedOut(base.Add(tt.at))
			if locked != tt.wantLocked {
				t.Errorf("LockedOut(+%v) locked = %v, want %v", tt.at, locked, tt.wantLocked)
			}
			if retryAfter != tt.wantRetryAfter {
				t.Errorf("LockedOut(+%v) retryAfter = %v, want %v", tt.at, retryAfter, tt.wantRetryAfter)
			}
		})
	}

	// Exactly at the expiry instant the lockout releases and state is cleared.
	locked, retryAfter := a.LockedOut(base.Add(2 * time.Minute))
	if locked {
		t.Error("expected the lockout to release at the cooldown boundary")
	}
	if retryAfter != 0 {
		t.Errorf("retryAfter = %v, want 0 after release", retryAfter)
	}
	if a.Failures() != 0 {
		t.Errorf("Failures() = %d, want 0 after cooldown expiry", a.Failures())
	}
	if !a.LockedUntil().IsZero() {
		t.Errorf("LockedUntil() = %v, want zero after cooldown expiry", a.LockedUntil())
	}
}

func TestAttempts_ClockMovingBackwardsDoesNotReleaseEarly(t *testing.T) {
	base := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	p := testPolicy()

	var a authguard.Attempts
	for i := 0; i < p.Threshold; i++ {
		a.RecordFailure(base, p)
	}

	locked, _ := a.LockedOut(base.Add(-time.Hour))
	if !locked {
		t.Error("a backwards clock jump must extend the lockout, not release it")
	}
}

func TestAttempts_ResetClearsEverything(t *testing.T) {
	base := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	p := testPolicy()

	var a authguard.Attempts
	a.RecordFailure(base, p)
	a.RecordFailure(base, p)
	a.Reset()

	if a.Failures() != 0 {
		t.Errorf("Failures() = %d, want 0 after Reset", a.Failures())
	}

	// The next failure starts a brand-new streak, so the threshold is not
	// reached one failure early.
	if tripped := a.RecordFailure(base, p); tripped {
		t.Error("Reset must restart the streak; the first failure after it must not trip")
	}
	if a.Failures() != 1 {
		t.Errorf("Failures() = %d, want 1 after Reset + one failure", a.Failures())
	}
}

func TestAttempts_ResetDuringLockoutReleasesIt(t *testing.T) {
	base := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	p := testPolicy()

	var a authguard.Attempts
	for i := 0; i < p.Threshold; i++ {
		a.RecordFailure(base, p)
	}
	a.Reset()

	if locked, _ := a.LockedOut(base); locked {
		t.Error("Reset must clear an active lockout")
	}
}

func TestAttempts_Idle(t *testing.T) {
	base := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	p := testPolicy()

	tests := []struct {
		name string
		// setup records failures at the given offsets.
		failures []time.Duration
		at       time.Duration
		wantIdle bool
	}{
		{"fresh entity is idle", nil, 0, true},
		{"live streak is not idle", []time.Duration{0}, 10 * time.Second, false},
		{"expired streak is idle", []time.Duration{0}, 2 * time.Minute, true},
		{"active lockout is not idle", []time.Duration{0, 0, 0}, time.Minute, false},
		{"expired lockout is idle", []time.Duration{0, 0, 0}, 5 * time.Minute, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a authguard.Attempts
			for _, off := range tt.failures {
				a.RecordFailure(base.Add(off), p)
			}
			if got := a.Idle(base.Add(tt.at), p); got != tt.wantIdle {
				t.Errorf("Idle(+%v) = %v, want %v", tt.at, got, tt.wantIdle)
			}
		})
	}
}

// TestAttempts_DefaultPolicyTripsAtTen pins the shipped defaults end to end.
func TestAttempts_DefaultPolicyTripsAtTen(t *testing.T) {
	base := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	p := authguard.DefaultPolicy()

	var a authguard.Attempts
	for i := 1; i <= authguard.DefaultThreshold; i++ {
		tripped := a.RecordFailure(base.Add(time.Duration(i)*time.Second), p)
		wantTripped := i == authguard.DefaultThreshold
		if tripped != wantTripped {
			t.Errorf("failure %d: tripped = %v, want %v", i, tripped, wantTripped)
		}
	}

	locked, retryAfter := a.LockedOut(base.Add(time.Duration(authguard.DefaultThreshold) * time.Second))
	if !locked {
		t.Fatal("expected the client to be locked out after the tenth failure")
	}
	if retryAfter != authguard.DefaultCooldown {
		t.Errorf("retryAfter = %v, want %v", retryAfter, authguard.DefaultCooldown)
	}
}
