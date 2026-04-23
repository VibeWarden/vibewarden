package tlspreflight_test

import (
	"context"
	"errors"
	"testing"
	"time"

	apptlspreflight "github.com/vibewarden/vibewarden/internal/app/tlspreflight"
	domaintlspreflight "github.com/vibewarden/vibewarden/internal/domain/tlspreflight"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeQuerier is a simple in-memory fake for ports.CertTransparencyQuerier.
type fakeQuerier struct {
	// responses maps registeredDomain → (records, error) to return.
	responses map[string]fakeResponse
}

type fakeResponse struct {
	records []ports.CrtShRecord
	err     error
}

func (f *fakeQuerier) Query(_ context.Context, registeredDomain string) ([]ports.CrtShRecord, error) {
	r, ok := f.responses[registeredDomain]
	if !ok {
		return nil, nil
	}
	return r.records, r.err
}

// fixedClock returns a func() time.Time that always returns t.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// leRecord builds a LE-issued ports.CrtShRecord with the given not_before offset
// from fixedNow.
func leRecord(fixedNow time.Time, offset time.Duration) ports.CrtShRecord {
	return ports.CrtShRecord{
		NotBefore:  fixedNow.Add(offset),
		IssuerName: "C=US, O=Let's Encrypt, CN=R3",
		CommonName: "example.com",
		NameValue:  "example.com",
	}
}

var fixedNow = time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)

// inWindow returns a leRecord issued N hours before fixedNow (inside the 168h window).
func inWindow(n int) ports.CrtShRecord {
	return leRecord(fixedNow, -time.Duration(n)*time.Hour)
}

// nRecords returns n LE records each issued 1h before fixedNow.
func nRecords(n int) []ports.CrtShRecord {
	recs := make([]ports.CrtShRecord, n)
	for i := range recs {
		recs[i] = inWindow(i + 1)
	}
	return recs
}

func TestService_Check_ThresholdTransitions(t *testing.T) {
	tests := []struct {
		name       string
		issued     int
		wantStatus domaintlspreflight.Status
		wantRemain int
	}{
		{"0/5 — OK full", 0, domaintlspreflight.StatusOK, 5},
		{"1/5 — OK", 1, domaintlspreflight.StatusOK, 4},
		{"2/5 — OK", 2, domaintlspreflight.StatusOK, 3},
		{"3/5 — OK", 3, domaintlspreflight.StatusOK, 2},
		{"4/5 — WARN", 4, domaintlspreflight.StatusWarn, 1},
		{"5/5 — FAIL", 5, domaintlspreflight.StatusFail, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQuerier{responses: map[string]fakeResponse{
				"example.com": {records: nRecords(tt.issued)},
			}}
			svc := apptlspreflight.NewService(q).WithClock(fixedClock(fixedNow))

			results := svc.Check(context.Background(), []string{"example.com"})
			if len(results) != 1 {
				t.Fatalf("len(results) = %d, want 1", len(results))
			}
			r := results[0]
			if r.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", r.Status, tt.wantStatus)
			}
			if r.RemainingBudget != tt.wantRemain {
				t.Errorf("RemainingBudget = %d, want %d", r.RemainingBudget, tt.wantRemain)
			}
		})
	}
}

func TestService_Check_ErrorSentinels(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{"unavailable", domaintlspreflight.ErrCTUnavailable, domaintlspreflight.ErrCTUnavailable},
		{"throttled", domaintlspreflight.ErrCTThrottled, domaintlspreflight.ErrCTThrottled},
		{"malformed", domaintlspreflight.ErrCTResponseMalformed, domaintlspreflight.ErrCTResponseMalformed},
		{"network wrapped", errors.New("dial tcp: connection refused"), nil}, // arbitrary error → WARN
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQuerier{responses: map[string]fakeResponse{
				"example.com": {err: tt.err},
			}}
			svc := apptlspreflight.NewService(q).WithClock(fixedClock(fixedNow))

			results := svc.Check(context.Background(), []string{"example.com"})
			if len(results) != 1 {
				t.Fatalf("len(results) = %d, want 1", len(results))
			}
			r := results[0]
			if r.Status != domaintlspreflight.StatusWarn {
				t.Errorf("Status = %v, want WARN", r.Status)
			}
			if r.Err == nil {
				t.Error("Err is nil, want non-nil")
			}
			if tt.wantErr != nil && !errors.Is(r.Err, tt.wantErr) {
				t.Errorf("errors.Is(Err, %v) = false, want true", tt.wantErr)
			}
		})
	}
}

func TestService_Check_MultiDomain_MixedOutcomes(t *testing.T) {
	q := &fakeQuerier{responses: map[string]fakeResponse{
		"example.com": {records: nRecords(5)},                     // FAIL
		"another.io":  {records: nRecords(4)},                     // WARN
		"fresh.dev":   {records: nil},                             // OK (no records)
		"unavail.net": {err: domaintlspreflight.ErrCTUnavailable}, // WARN via error
	}}
	svc := apptlspreflight.NewService(q).WithClock(fixedClock(fixedNow))

	results := svc.Check(context.Background(), []string{"example.com", "another.io", "fresh.dev", "unavail.net"})
	if len(results) != 4 {
		t.Fatalf("len(results) = %d, want 4", len(results))
	}

	want := []domaintlspreflight.Status{
		domaintlspreflight.StatusFail,
		domaintlspreflight.StatusWarn,
		domaintlspreflight.StatusOK,
		domaintlspreflight.StatusWarn,
	}
	for i, r := range results {
		if r.Status != want[i] {
			t.Errorf("results[%d].Status = %v, want %v", i, r.Status, want[i])
		}
	}
}

func TestService_Check_ResultOrder_PreservedFromInput(t *testing.T) {
	domains := []string{"z.com", "a.com", "m.com"}
	q := &fakeQuerier{responses: map[string]fakeResponse{
		"z.com": {records: nil},
		"a.com": {records: nRecords(5)},
		"m.com": {records: nRecords(4)},
	}}
	svc := apptlspreflight.NewService(q).WithClock(fixedClock(fixedNow))

	results := svc.Check(context.Background(), domains)
	for i, r := range results {
		if r.Domain != domains[i] {
			t.Errorf("results[%d].Domain = %q, want %q", i, r.Domain, domains[i])
		}
	}
}

func TestService_Check_FailNextSlotTime(t *testing.T) {
	oldest := fixedNow.Add(-100 * time.Hour)
	q := &fakeQuerier{responses: map[string]fakeResponse{
		"example.com": {records: []ports.CrtShRecord{
			{NotBefore: oldest, IssuerName: "C=US, O=Let's Encrypt, CN=R3"},
			{NotBefore: fixedNow.Add(-10 * time.Hour), IssuerName: "C=US, O=Let's Encrypt, CN=R3"},
			{NotBefore: fixedNow.Add(-20 * time.Hour), IssuerName: "C=US, O=Let's Encrypt, CN=R3"},
			{NotBefore: fixedNow.Add(-30 * time.Hour), IssuerName: "C=US, O=Let's Encrypt, CN=R3"},
			{NotBefore: fixedNow.Add(-40 * time.Hour), IssuerName: "C=US, O=Let's Encrypt, CN=R3"},
		}},
	}}
	svc := apptlspreflight.NewService(q).WithClock(fixedClock(fixedNow))

	results := svc.Check(context.Background(), []string{"example.com"})
	r := results[0]
	if r.Status != domaintlspreflight.StatusFail {
		t.Fatalf("Status = %v, want FAIL", r.Status)
	}
	expectedNext := oldest.Add(domaintlspreflight.BudgetWindow)
	if !r.NextSlotAvailableAt.Equal(expectedNext) {
		t.Errorf("NextSlotAvailableAt = %v, want %v", r.NextSlotAvailableAt, expectedNext)
	}
}
