// Package probe implements the vibew probe use case: HTTPS health probe of
// /_vibewarden/health with a boot-gap retry policy and a TLS-handshake retry
// policy for ACME issuance.
package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ErrBootGapExhausted is returned by Service.Run when the boot-gap retry
// budget is exhausted with upstream still in "unknown" state. The caller
// receives the last observed Result alongside this error so it can render
// the degraded output block before exiting 1.
var ErrBootGapExhausted = errors.New("upstream probe has not converged within boot-gap window")

// ErrTLSRetryExhausted is returned by Service.Run when the TLS-handshake
// retry budget is exhausted with the endpoint still returning TLS handshake
// errors. This typically indicates ACME (Let's Encrypt) issuance is still in
// progress on the remote host. The rendered duration comes from
// Result.TLSRetryBudget, not from this sentinel string.
var ErrTLSRetryExhausted = errors.New("TLS handshake retry exhausted")

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

	// TLSRetryWait is the total budget for retrying TLS handshake failures
	// when EnvName is set (--env path). Default: 30s. Only engaged when the
	// first probe returns ports.ErrTLSHandshake and EnvName != "".
	TLSRetryWait time.Duration

	// TLSRetryPoll is the interval between probes during the TLS retry loop.
	// Default: 2s.
	TLSRetryPoll time.Duration

	// ProgressWriter receives human-readable progress lines during the TLS
	// retry loop (e.g. "Waiting for ACME issuance... (2s elapsed)"). If nil,
	// progress output is discarded. The CLI wires this to cmd.ErrOrStderr().
	ProgressWriter io.Writer
}

// DefaultOptions returns Options with the production defaults.
func DefaultOptions(url, envName string) Options {
	return Options{
		URL:          url,
		EnvName:      envName,
		BootGapWait:  10 * time.Second,
		BootGapPoll:  1 * time.Second,
		PerProbe:     3 * time.Second,
		TLSRetryWait: 30 * time.Second,
		TLSRetryPoll: 2 * time.Second,
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

	// TLSRetryBudget is the total TLS-retry window configured for this run. It
	// is populated from Options.TLSRetryWait before the TLS retry loop engages
	// so that renderTLSRetryExhausted can substitute the actual duration rather
	// than a hardcoded constant.
	TLSRetryBudget time.Duration
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

// Run probes opts.URL once. Two retry loops may engage in sequence:
//
//  1. TLS-retry loop (--env path only): if the first probe returns
//     ports.ErrTLSHandshake and opts.EnvName != "", retry every
//     opts.TLSRetryPoll for up to opts.TLSRetryWait. Progress lines are
//     written to opts.ProgressWriter. On exhaustion, returns
//     ErrTLSRetryExhausted. On error-class change, returns the new error
//     immediately. On success, falls through to the boot-gap loop below.
//     Default mode (EnvName == "") treats TLS errors as hard failures.
//
//  2. Boot-gap loop: if components.upstream is "unknown", retry every
//     opts.BootGapPoll for up to opts.BootGapWait. On exhaustion, returns
//     ErrBootGapExhausted alongside the last Result.
//
// All other errors are returned immediately without retry.
func (s *Service) Run(ctx context.Context, opts Options) (Result, error) {
	result := Result{
		URL:            opts.URL,
		EnvName:        opts.EnvName,
		TLSRetryBudget: opts.TLSRetryWait,
	}

	pw := opts.ProgressWriter
	if pw == nil {
		pw = io.Discard
	}

	doc, err := s.prober.Probe(ctx, opts.URL)
	if err != nil {
		// TLS-retry loop only engages when --env is set.
		if errors.Is(err, ports.ErrTLSHandshake) && opts.EnvName != "" {
			doc, err = s.runTLSRetry(ctx, opts, pw)
			if err != nil {
				return result, err
			}
		} else {
			// Hard error or default-mode TLS error: fail fast.
			return result, err
		}
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

// runTLSRetry implements the TLS-handshake retry loop. It is called when the
// first probe returned ports.ErrTLSHandshake and opts.EnvName != "". Progress
// lines are written to pw every iteration.
//
// Returns (doc, nil) on success, ErrTLSRetryExhausted on budget exhaustion, or
// the new error immediately when the error class changes.
func (s *Service) runTLSRetry(ctx context.Context, opts Options, pw io.Writer) (ports.HealthDocument, error) {
	retrySeconds := int(opts.TLSRetryWait.Seconds())
	fmt.Fprintf(pw, "Waiting for ACME issuance... (TLS handshake failed; retrying %ds)\n", retrySeconds)

	start := time.Now()
	deadline := start.Add(opts.TLSRetryWait)

	for {
		s.sleep(opts.TLSRetryPoll)

		elapsed := int(time.Since(start).Seconds())
		fmt.Fprintf(pw, "Waiting for ACME issuance... (%ds elapsed)\n", elapsed)

		if err := ctx.Err(); err != nil {
			return ports.HealthDocument{}, fmt.Errorf("probe context cancelled during TLS retry: %w", err)
		}

		doc, err := s.prober.Probe(ctx, opts.URL)
		if err == nil {
			// TLS resolved — fall through to boot-gap logic.
			return doc, nil
		}

		if !errors.Is(err, ports.ErrTLSHandshake) {
			// Error class changed — return the new error immediately.
			return ports.HealthDocument{}, err
		}

		if !time.Now().Before(deadline) {
			// Budget exhausted.
			return ports.HealthDocument{}, ErrTLSRetryExhausted
		}
	}
}
