package tlspreflight_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/tlspreflight"
)

func TestNewFromQuery_StatusMapping(t *testing.T) {
	// oldest is 50h before now — slot opens at oldest + 168h.
	baseTime := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		issued       int
		wantStatus   tlspreflight.Status
		wantRemain   int
		wantNextSlot bool // true when NextSlotAvailableAt should be non-zero
	}{
		{"0 issued — OK full budget", 0, tlspreflight.StatusOK, 5, false},
		{"1 issued — OK", 1, tlspreflight.StatusOK, 4, false},
		{"2 issued — OK", 2, tlspreflight.StatusOK, 3, false},
		{"3 issued — OK", 3, tlspreflight.StatusOK, 2, false},
		{"4 issued — WARN", 4, tlspreflight.StatusWarn, 1, false},
		{"5 issued — FAIL", 5, tlspreflight.StatusFail, 0, true},
		{"6 issued — FAIL clamped", 6, tlspreflight.StatusFail, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tlspreflight.NewFromQuery("example.com", tt.issued, baseTime)
			if r.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", r.Status, tt.wantStatus)
			}
			if r.RemainingBudget != tt.wantRemain {
				t.Errorf("RemainingBudget = %d, want %d", r.RemainingBudget, tt.wantRemain)
			}
			if tt.wantNextSlot && r.NextSlotAvailableAt.IsZero() {
				t.Errorf("NextSlotAvailableAt is zero, want non-zero")
			}
			if !tt.wantNextSlot && !r.NextSlotAvailableAt.IsZero() {
				t.Errorf("NextSlotAvailableAt = %v, want zero", r.NextSlotAvailableAt)
			}
			// Detail must always be non-empty.
			if r.Detail == "" {
				t.Error("Detail is empty")
			}
		})
	}
}

func TestRenderDetail_AllPaths(t *testing.T) {
	baseTime := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	expectedNextSlot := baseTime.Add(tlspreflight.BudgetWindow).Format(time.RFC1123)

	tests := []struct {
		name        string
		result      tlspreflight.Result
		wantContain []string
	}{
		{
			name:        "0 issued — full budget",
			result:      tlspreflight.NewFromQuery("example.com", 0, time.Time{}),
			wantContain: []string{"5 of 5 slots free", "example.com"},
		},
		{
			name:        "1 issued",
			result:      tlspreflight.NewFromQuery("example.com", 1, time.Time{}),
			wantContain: []string{"4 of 5 slots remaining", "example.com"},
		},
		{
			name:        "2 issued",
			result:      tlspreflight.NewFromQuery("example.com", 2, time.Time{}),
			wantContain: []string{"3 of 5 slots remaining", "example.com"},
		},
		{
			name:        "3 issued",
			result:      tlspreflight.NewFromQuery("example.com", 3, time.Time{}),
			wantContain: []string{"2 of 5 slots remaining", "example.com"},
		},
		{
			name:        "4 issued — WARN",
			result:      tlspreflight.NewFromQuery("example.com", 4, time.Time{}),
			wantContain: []string{"1 of 5 slots remaining this week", "example.com"},
		},
		{
			name:   "5 issued — FAIL contains opt-out flag",
			result: tlspreflight.NewFromQuery("example.com", 5, baseTime),
			wantContain: []string{
				"LE rate limit exhausted",
				"example.com",
				"next slot at",
				expectedNextSlot,
				"--skip-le-preflight",
			},
		},
		{
			name:   "6 issued — FAIL with observed suffix",
			result: tlspreflight.NewFromQuery("example.com", 6, baseTime),
			wantContain: []string{
				"LE rate limit exhausted",
				"observed 6 issuances",
				"--skip-le-preflight",
			},
		},
		{
			name:   "error — ErrCTUnavailable",
			result: tlspreflight.NewFromError("example.com", tlspreflight.ErrCTUnavailable),
			wantContain: []string{
				"crt.sh unreachable",
				"--skip-le-preflight",
			},
		},
		{
			name:   "error — ErrCTThrottled",
			result: tlspreflight.NewFromError("example.com", tlspreflight.ErrCTThrottled),
			wantContain: []string{
				"crt.sh throttled",
				"--skip-le-preflight",
			},
		},
		{
			name:   "error — ErrCTResponseMalformed",
			result: tlspreflight.NewFromError("example.com", tlspreflight.ErrCTResponseMalformed),
			wantContain: []string{
				"crt.sh returned unexpected response",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.Detail
			for _, want := range tt.wantContain {
				if !containsStr(got, want) {
					t.Errorf("Detail does not contain %q\ngot: %s", want, got)
				}
			}
		})
	}
}

func TestNewFromError_StatusIsWarn(t *testing.T) {
	errs := []error{
		tlspreflight.ErrCTUnavailable,
		tlspreflight.ErrCTThrottled,
		tlspreflight.ErrCTResponseMalformed,
		fmt.Errorf("some wrapped error: %w", tlspreflight.ErrCTUnavailable),
	}
	for _, err := range errs {
		r := tlspreflight.NewFromError("example.com", err)
		if r.Status != tlspreflight.StatusWarn {
			t.Errorf("NewFromError(%v).Status = %v, want WARN", err, r.Status)
		}
		if r.Err == nil {
			t.Errorf("NewFromError(%v).Err is nil, want non-nil", err)
		}
	}
}

func TestNewSkipped(t *testing.T) {
	r := tlspreflight.NewSkipped("localhost")
	if r.Status != tlspreflight.StatusWarn {
		t.Errorf("Status = %v, want WARN", r.Status)
	}
	if !containsStr(r.Detail, "cannot derive registered domain") {
		t.Errorf("Detail = %q, want 'cannot derive registered domain'", r.Detail)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
