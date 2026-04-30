// Package probe implements the vibew probe use case: HTTPS health probe of
// /_vibewarden/health with a boot-gap retry policy.
package probe

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ErrBootGapExhausted is returned by Service.Run when the boot-gap retry
// budget is exhausted with upstream still in "unknown" state. The caller
// receives the last observed Result alongside this error so it can render
// the degraded output block before exiting 1.
var ErrBootGapExhausted = errors.New("upstream probe has not converged within boot-gap window")

// Options is the parameter object for Service.Run. All duration fields have
// safe defaults applied by DefaultOptions.
type Options struct {
	// URL is the fully-qualified HTTPS URL to probe, e.g.
	// "https://localhost:8443/_vibewarden/health".
	URL string

	// EnvName is the --env flag value passed by the CLI. It is echoed in the
	// Result so the renderer can customise the summary line. Empty for the
	// default (dev) probe.
	EnvName string

	// BootGapWait is the total budget for waiting on a soft "upstream:unknown"
	// state before declaring boot-gap exhaustion. Default: 10s.
	//
	// This window matches ADR-098's stated boot-gap range. If a future change
	// to the background probe lengthens that window, this constant must grow
	// with it — flagged as a maintenance dependency per ADR-102.
	BootGapWait time.Duration

	// BootGapPoll is the interval between retries during the boot-gap wait.
	// Default: 1s.
	BootGapPoll time.Duration

	// PerProbe is the per-request timeout forwarded to the prober via the
	// context. Not currently used to set a per-probe deadline (the HTTP client
	// timeout set at construction handles that), but kept for future use.
	PerProbe time.Duration
}

// DefaultOptions returns Options with the production defaults.
func DefaultOptions(url, envName string) Options {
	return Options{
		URL:         url,
		EnvName:     envName,
		BootGapWait: 10 * time.Second,
		BootGapPoll: 1 * time.Second,
		PerProbe:    3 * time.Second,
	}
}

// Result is what the CLI renders. The URL is kept separate from Doc so the
// renderer can print the URL even on hard-error paths where Doc is empty.
type Result struct {
	// URL is the target that was probed.
	URL string

	// Doc is the parsed health document. Zero-value when a hard error occurred.
	Doc ports.HealthDocument

	// EnvName is the environment name (from --env), empty for dev/default.
	EnvName string
}

// Service orchestrates the probe use case: single-shot probe + boot-gap retry.
// Construct with NewService.
type Service struct {
	prober ports.HealthProber
	sleep  func(d time.Duration) // injectable for tests; defaults to time.Sleep
}

// NewService constructs a Service backed by the given prober.
// The sleep function defaults to time.Sleep; inject a no-op for tests.
func NewService(prober ports.HealthProber) *Service {
	return &Service{
		prober: prober,
		sleep:  time.Sleep,
	}
}

// WithSleep returns a copy of the service with the sleep function replaced.
// Used in tests to avoid real sleeps.
func (s *Service) WithSleep(fn func(time.Duration)) *Service {
	return &Service{prober: s.prober, sleep: fn}
}

// Run probes opts.URL once. If the first probe's components.upstream is
// "unknown", it retries every opts.BootGapPoll until either the upstream
// reports a non-unknown state or opts.BootGapWait elapses. On budget
// exhaustion it returns the last observed Result alongside ErrBootGapExhausted.
//
// Hard errors (ErrProbeRefused, ErrProbeMalformed, *ProbeNon200Error) are
// returned immediately without retry.
func (s *Service) Run(ctx context.Context, opts Options) (Result, error) {
	result := Result{URL: opts.URL, EnvName: opts.EnvName}

	doc, err := s.prober.Probe(ctx, opts.URL)
	if err != nil {
		// Hard errors: do not retry.
		return result, err
	}

	result.Doc = doc

	// Upstream is in a definitive state — return immediately.
	if doc.Components["upstream"] != "unknown" {
		return result, nil
	}

	// Boot-gap retry: upstream is "unknown". Keep polling until it clears or
	// the budget is exhausted.
	deadline := time.Now().Add(opts.BootGapWait)
	for time.Now().Before(deadline) {
		s.sleep(opts.BootGapPoll)

		// Re-check context before each retry.
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("probe context cancelled during boot-gap wait: %w", err)
		}

		doc, err = s.prober.Probe(ctx, opts.URL)
		if err != nil {
			// Hard error during retry — propagate immediately.
			return result, err
		}
		result.Doc = doc

		if doc.Components["upstream"] != "unknown" {
			return result, nil
		}
	}

	// Budget exhausted with upstream still unknown.
	return result, ErrBootGapExhausted
}
