package probe_test

import (
	"context"
	"errors"
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
