package probe_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/probe"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeProber is a test fake that returns pre-programmed responses in order.
// When the queue is exhausted it repeats the last entry indefinitely.
type fakeProber struct {
	responses []fakeResponse
	callCount int
}

type fakeResponse struct {
	doc ports.HealthDocument
	err error
}

func (f *fakeProber) Probe(_ context.Context, _ string) (ports.HealthDocument, error) {
	idx := f.callCount
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	f.callCount++
	r := f.responses[idx]
	return r.doc, r.err
}

// okDoc returns a healthy HealthDocument.
func okDoc() ports.HealthDocument {
	return ports.HealthDocument{
		Status:  "ok",
		Version: "0.18.4",
		Components: map[string]string{
			"sidecar":  "ok",
			"upstream": "ok",
		},
	}
}

// unknownDoc returns a degraded HealthDocument with upstream=unknown.
func unknownDoc() ports.HealthDocument {
	return ports.HealthDocument{
		Status:  "degraded",
		Version: "0.18.4",
		Components: map[string]string{
			"sidecar":  "ok",
			"upstream": "unknown",
		},
	}
}

// failingDoc returns a degraded HealthDocument with upstream=failing.
func failingDoc() ports.HealthDocument {
	return ports.HealthDocument{
		Status:  "degraded",
		Version: "0.18.4",
		Components: map[string]string{
			"sidecar":  "ok",
			"upstream": "failing",
		},
	}
}

// noSleep is an injectable sleep that does nothing, so tests run instantly.
func noSleep(_ time.Duration) {}

func opts(url string) probe.Options {
	return probe.Options{
		URL:         url,
		BootGapWait: 5 * time.Second,
		BootGapPoll: 100 * time.Millisecond,
		PerProbe:    3 * time.Second,
	}
}

func TestService_FirstProbeOK_NoRetry(t *testing.T) {
	prober := &fakeProber{responses: []fakeResponse{{doc: okDoc()}}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	result, err := svc.Run(context.Background(), opts("https://localhost:8443/_vibewarden/health"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if prober.callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retry expected)", prober.callCount)
	}
	if result.Doc.Components["upstream"] != "ok" {
		t.Errorf("upstream = %q, want %q", result.Doc.Components["upstream"], "ok")
	}
}

func TestService_FirstProbeUnknown_ThenOK(t *testing.T) {
	prober := &fakeProber{responses: []fakeResponse{
		{doc: unknownDoc()},
		{doc: unknownDoc()},
		{doc: okDoc()},
	}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	result, err := svc.Run(context.Background(), opts("https://localhost:8443/_vibewarden/health"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if prober.callCount != 3 {
		t.Errorf("callCount = %d, want 3", prober.callCount)
	}
	if result.Doc.Components["upstream"] != "ok" {
		t.Errorf("upstream = %q, want ok", result.Doc.Components["upstream"])
	}
}

func TestService_AllRetriesUnknown_ReturnsBootGapExhausted(t *testing.T) {
	// All probes return unknown. The service should exhaust the budget and
	// return ErrBootGapExhausted along with the last unknown Result.
	prober := &fakeProber{responses: []fakeResponse{
		{doc: unknownDoc()},
	}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	// Use a very short BootGapWait to avoid looping forever in tests.
	o := probe.Options{
		URL:         "https://localhost:8443/_vibewarden/health",
		BootGapWait: 200 * time.Millisecond,
		BootGapPoll: 100 * time.Millisecond,
		PerProbe:    3 * time.Second,
	}
	result, err := svc.Run(context.Background(), o)
	if err == nil {
		t.Fatal("expected ErrBootGapExhausted, got nil")
	}
	if !errors.Is(err, probe.ErrBootGapExhausted) {
		t.Errorf("expected ErrBootGapExhausted, got: %v", err)
	}
	if result.Doc.Components["upstream"] != "unknown" {
		t.Errorf("last doc upstream = %q, want unknown", result.Doc.Components["upstream"])
	}
}

func TestService_HardErrorOnFirstProbe_NoRetry(t *testing.T) {
	tests := []struct {
		name         string
		sentinel     error
		wantSentinel error
	}{
		{
			name:         "connection refused",
			sentinel:     ports.ErrProbeRefused,
			wantSentinel: ports.ErrProbeRefused,
		},
		{
			name:         "DNS failure",
			sentinel:     ports.ErrDNSFailure,
			wantSentinel: ports.ErrDNSFailure,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prober := &fakeProber{responses: []fakeResponse{
				{err: tt.sentinel},
			}}
			svc := probe.NewService(prober).WithSleep(noSleep)

			_, err := svc.Run(context.Background(), opts("https://localhost:8443/_vibewarden/health"))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tt.wantSentinel) {
				t.Errorf("expected %v, got: %v", tt.wantSentinel, err)
			}
			if prober.callCount != 1 {
				t.Errorf("callCount = %d, want 1 (no retry on hard error)", prober.callCount)
			}
		})
	}
}

func TestService_HardErrorDuringRetry_PropagatesImmediately(t *testing.T) {
	prober := &fakeProber{responses: []fakeResponse{
		{doc: unknownDoc()},            // first call → boot gap
		{err: ports.ErrProbeMalformed}, // second call → hard error during retry
	}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	_, err := svc.Run(context.Background(), opts("https://localhost:8443/_vibewarden/health"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ports.ErrProbeMalformed) {
		t.Errorf("expected ErrProbeMalformed, got: %v", err)
	}
	if prober.callCount != 2 {
		t.Errorf("callCount = %d, want 2", prober.callCount)
	}
}

func TestService_FailingUpstream_NoRetry(t *testing.T) {
	// "failing" is not "unknown" — no boot-gap retry.
	prober := &fakeProber{responses: []fakeResponse{{doc: failingDoc()}}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	result, err := svc.Run(context.Background(), opts("https://localhost:8443/_vibewarden/health"))
	if err != nil {
		t.Fatalf("expected no error (failing is not retried), got: %v", err)
	}
	if prober.callCount != 1 {
		t.Errorf("callCount = %d, want 1", prober.callCount)
	}
	if result.Doc.Components["upstream"] != "failing" {
		t.Errorf("upstream = %q, want failing", result.Doc.Components["upstream"])
	}
}

func TestService_ResultURL_PreservedOnError(t *testing.T) {
	targetURL := "https://localhost:8443/_vibewarden/health"
	prober := &fakeProber{responses: []fakeResponse{
		{err: ports.ErrProbeRefused},
	}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	result, _ := svc.Run(context.Background(), probe.Options{
		URL:         targetURL,
		BootGapWait: 100 * time.Millisecond,
		BootGapPoll: 50 * time.Millisecond,
	})
	if result.URL != targetURL {
		t.Errorf("result.URL = %q, want %q", result.URL, targetURL)
	}
}

// tlsErr wraps ports.ErrTLSHandshake the same way the adapter does (two-level
// wrapping) so errors.Is(err, ports.ErrTLSHandshake) returns true.
func tlsErr() error {
	return fmt.Errorf("%w: tls: internal error", ports.ErrTLSHandshake)
}

// TestService_TLSRetry_DefaultMode_NoRetry verifies that when EnvName is
// empty and the first probe returns ErrTLSHandshake, the service fails fast
// without retrying and without writing any progress messages.
func TestService_TLSRetry_DefaultMode_NoRetry(t *testing.T) {
	prober := &fakeProber{responses: []fakeResponse{{err: tlsErr()}}}
	var progress bytes.Buffer
	svc := probe.NewService(prober).WithSleep(noSleep)

	_, err := svc.Run(context.Background(), probe.Options{
		URL:            "https://example.com/_vibewarden/health",
		EnvName:        "", // default mode — no retry
		TLSRetryWait:   30 * time.Second,
		TLSRetryPoll:   2 * time.Second,
		BootGapWait:    10 * time.Second,
		BootGapPoll:    1 * time.Second,
		ProgressWriter: &progress,
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ports.ErrTLSHandshake) {
		t.Errorf("expected ErrTLSHandshake, got: %v", err)
	}
	if prober.callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retry in default mode)", prober.callCount)
	}
	if progress.Len() != 0 {
		t.Errorf("expected no progress output, got: %q", progress.String())
	}
}

// TestService_TLSRetry_EnvMode_SuccessAfterDelay verifies that when EnvName
// is set and the prober returns ErrTLSHandshake on the initial probe and twice
// more inside the loop then succeeds on the fourth call, the service retries
// and returns success. Progress messages: 1 initial "retrying 30s" + 3 elapsed
// (one per loop iteration before the successful probe) = 4 total lines.
func TestService_TLSRetry_EnvMode_SuccessAfterDelay(t *testing.T) {
	// responses[0]: initial probe → TLS error → enters loop
	// responses[1]: loop iter 1 → TLS error (continue)
	// responses[2]: loop iter 2 → TLS error (continue)
	// responses[3]: loop iter 3 → success
	prober := &fakeProber{responses: []fakeResponse{
		{err: tlsErr()},
		{err: tlsErr()},
		{err: tlsErr()},
		{doc: okDoc()},
	}}
	var progress bytes.Buffer
	svc := probe.NewService(prober).WithSleep(noSleep)

	result, err := svc.Run(context.Background(), probe.Options{
		URL:            "https://example.com/_vibewarden/health",
		EnvName:        "production",
		TLSRetryWait:   30 * time.Second,
		TLSRetryPoll:   2 * time.Second,
		BootGapWait:    10 * time.Second,
		BootGapPoll:    1 * time.Second,
		ProgressWriter: &progress,
	})

	if err != nil {
		t.Fatalf("expected no error after TLS retry success, got: %v", err)
	}
	if prober.callCount != 4 {
		t.Errorf("callCount = %d, want 4 (1 initial + 3 loop iterations)", prober.callCount)
	}
	if result.Doc.Components["upstream"] != "ok" {
		t.Errorf("upstream = %q, want ok", result.Doc.Components["upstream"])
	}

	// The progress buffer must contain:
	//   line 0: "Waiting for ACME issuance... (TLS handshake failed; retrying 30s)"
	//   line 1: "Waiting for ACME issuance... (0s elapsed)"  — loop iter 1
	//   line 2: "Waiting for ACME issuance... (0s elapsed)"  — loop iter 2
	//   line 3: "Waiting for ACME issuance... (0s elapsed)"  — loop iter 3 (before success probe)
	lines := strings.Split(strings.TrimRight(progress.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 progress lines, got %d: %v", len(lines), lines)
	}
	wantFirst := "Waiting for ACME issuance... (TLS handshake failed; retrying 30s)"
	if lines[0] != wantFirst {
		t.Errorf("progress[0] = %q, want %q", lines[0], wantFirst)
	}
	// Lines 1-3 must match the elapsed-time format.
	for i, line := range lines[1:] {
		if !strings.HasPrefix(line, "Waiting for ACME issuance... (") ||
			!strings.HasSuffix(line, "s elapsed)") {
			t.Errorf("progress[%d] = %q, want format 'Waiting for ACME issuance... (Ns elapsed)'", i+1, line)
		}
	}
}

// TestService_TLSRetry_EnvMode_Exhausted verifies that when EnvName is set
// and the prober returns ErrTLSHandshake indefinitely, the service returns
// ErrTLSRetryExhausted after the retry budget is exhausted.
func TestService_TLSRetry_EnvMode_Exhausted(t *testing.T) {
	prober := &fakeProber{responses: []fakeResponse{{err: tlsErr()}}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	_, err := svc.Run(context.Background(), probe.Options{
		URL:          "https://example.com/_vibewarden/health",
		EnvName:      "production",
		TLSRetryWait: 200 * time.Millisecond, // short budget for fast test
		TLSRetryPoll: 2 * time.Second,
		BootGapWait:  10 * time.Second,
		BootGapPoll:  1 * time.Second,
	})

	if err == nil {
		t.Fatal("expected ErrTLSRetryExhausted, got nil")
	}
	if !errors.Is(err, probe.ErrTLSRetryExhausted) {
		t.Errorf("expected ErrTLSRetryExhausted, got: %v", err)
	}
}

// TestService_TLSRetry_EnvMode_ErrorClassChange verifies that when the prober
// returns ErrTLSHandshake twice then ErrProbeRefused, the TLS retry loop exits
// immediately with ErrProbeRefused (not ErrTLSRetryExhausted).
func TestService_TLSRetry_EnvMode_ErrorClassChange(t *testing.T) {
	prober := &fakeProber{responses: []fakeResponse{
		{err: tlsErr()},
		{err: tlsErr()},
		{err: ports.ErrProbeRefused},
	}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	_, err := svc.Run(context.Background(), probe.Options{
		URL:          "https://example.com/_vibewarden/health",
		EnvName:      "production",
		TLSRetryWait: 30 * time.Second,
		TLSRetryPoll: 2 * time.Second,
		BootGapWait:  10 * time.Second,
		BootGapPoll:  1 * time.Second,
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ports.ErrProbeRefused) {
		t.Errorf("expected ErrProbeRefused on class change, got: %v", err)
	}
	if prober.callCount != 3 {
		t.Errorf("callCount = %d, want 3 (1 initial + 2 retries)", prober.callCount)
	}
}

// TestMaxIterations verifies the MaxIterations helper used to cap retry loops.
func TestMaxIterations(t *testing.T) {
	tests := []struct {
		name    string
		budget  time.Duration
		poll    time.Duration
		wantMin int
		wantMax int
	}{
		{
			name:    "production TLS defaults 30s/2s",
			budget:  30 * time.Second,
			poll:    2 * time.Second,
			wantMin: 15, // floor(30/2)=15, +2 margin = 17
			wantMax: 20,
		},
		{
			name:    "production boot-gap defaults 10s/1s",
			budget:  10 * time.Second,
			poll:    1 * time.Second,
			wantMin: 10, // floor(10/1)=10, +2 margin = 12
			wantMax: 15,
		},
		{
			name:    "test budget 200ms/2s (poll larger than budget)",
			budget:  200 * time.Millisecond,
			poll:    2 * time.Second,
			wantMin: 1,
			wantMax: 5,
		},
		{
			name:    "zero poll returns 1",
			budget:  10 * time.Second,
			poll:    0,
			wantMin: 1,
			wantMax: 1,
		},
		{
			name:    "equal budget and poll",
			budget:  5 * time.Second,
			poll:    5 * time.Second,
			wantMin: 1,
			wantMax: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := probe.MaxIterations(tt.budget, tt.poll)
			if got < tt.wantMin {
				t.Errorf("MaxIterations(%v, %v) = %d, want >= %d", tt.budget, tt.poll, got, tt.wantMin)
			}
			if got > tt.wantMax {
				t.Errorf("MaxIterations(%v, %v) = %d, want <= %d", tt.budget, tt.poll, got, tt.wantMax)
			}
		})
	}
}

// TestService_TLSRetry_IterationCapEnforced verifies that with a no-op sleep
// the TLS retry loop terminates after a bounded number of probe calls.
// Before the fix, ~750,000 iterations were observed for a 200ms budget.
func TestService_TLSRetry_IterationCapEnforced(t *testing.T) {
	prober := &fakeProber{responses: []fakeResponse{{err: tlsErr()}}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	// budget=200ms, poll=2s → MaxIterations returns floor(0.1)+2 = 2
	_, err := svc.Run(context.Background(), probe.Options{
		URL:          "https://example.com/_vibewarden/health",
		EnvName:      "production",
		TLSRetryWait: 200 * time.Millisecond,
		TLSRetryPoll: 2 * time.Second,
		BootGapWait:  10 * time.Second,
		BootGapPoll:  1 * time.Second,
	})

	if !errors.Is(err, probe.ErrTLSRetryExhausted) {
		t.Fatalf("expected ErrTLSRetryExhausted, got: %v", err)
	}

	// 1 initial probe + at most MaxIterations(200ms, 2s) loop probes = at most 3.
	// Without the iteration cap this would be ~750,000.
	const maxExpectedCalls = 20
	if prober.callCount > maxExpectedCalls {
		t.Errorf("callCount = %d, want <= %d (iteration cap not enforced — busy-spin bug)", prober.callCount, maxExpectedCalls)
	}
}

// TestService_BootGap_IterationCapEnforced verifies that with a no-op sleep
// the boot-gap loop terminates after a bounded number of probe calls.
// Before the fix, ~500,000 iterations were observed for a 200ms budget.
func TestService_BootGap_IterationCapEnforced(t *testing.T) {
	prober := &fakeProber{responses: []fakeResponse{{doc: unknownDoc()}}}
	svc := probe.NewService(prober).WithSleep(noSleep)

	// budget=200ms, poll=100ms → MaxIterations returns floor(2)+2 = 4
	_, err := svc.Run(context.Background(), probe.Options{
		URL:         "https://example.com/_vibewarden/health",
		BootGapWait: 200 * time.Millisecond,
		BootGapPoll: 100 * time.Millisecond,
		PerProbe:    3 * time.Second,
	})

	if !errors.Is(err, probe.ErrBootGapExhausted) {
		t.Fatalf("expected ErrBootGapExhausted, got: %v", err)
	}

	// 1 initial probe + at most MaxIterations(200ms, 100ms) loop probes = at most 5.
	// Without the iteration cap this would be ~500,000.
	const maxExpectedCalls = 20
	if prober.callCount > maxExpectedCalls {
		t.Errorf("callCount = %d, want <= %d (iteration cap not enforced — busy-spin bug)", prober.callCount, maxExpectedCalls)
	}
}
