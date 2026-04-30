package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	healthadapter "github.com/vibewarden/vibewarden/internal/adapters/health"
	envapp "github.com/vibewarden/vibewarden/internal/app/env"
	probeapp "github.com/vibewarden/vibewarden/internal/app/probe"
	"github.com/vibewarden/vibewarden/internal/config"
)

const (
	probeHealthPath = "/_vibewarden/health"
	probeTimeout    = 3 * time.Second
)

// NewProbeCmd creates the "vibew probe" subcommand.
//
// The command performs an HTTPS GET against the sidecar's /_vibewarden/health
// endpoint using Go's stdlib HTTP client, bypassing the host's TLS stack
// (and the macOS LibreSSL friction documented in #1224). When --env is given,
// the command resolves the named production override config, reads
// tls.domain, and probes the remote endpoint with full TLS verification.
//
// Exit codes:
//   - 0: components.upstream == "ok" (stack is fully healthy)
//   - 1: any other state (refused, malformed, non-200, boot-gap exhausted, failing)
func NewProbeCmd() *cobra.Command {
	var envName string

	cmd := &cobra.Command{
		Use:           "probe [--env <name>]",
		Short:         "Probe the sidecar's /_vibewarden/health endpoint",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		Long: `Probe the VibeWarden sidecar's /_vibewarden/health endpoint over HTTPS.

Uses Go's stdlib TLS stack, bypassing macOS LibreSSL friction with self-signed
dev certificates (see #1224 for the advisory background).

Default (no --env flag):
  Reads server.port from vibewarden.yaml.
  Probes https://localhost:<port>/_vibewarden/health with InsecureSkipVerify=true.
  Suitable for verifying a running vibew dev stack.

With --env <name>:
  Loads vibewarden.<name>.yaml (must exist in the project directory).
  Reads tls.domain from the merged config.
  Probes https://<tls.domain>/_vibewarden/health with full TLS verification.
  Suitable for verifying a production deployment.

Boot-gap handling:
  When components.upstream is "unknown" (the sidecar has not yet completed its
  first upstream probe cycle), vibew probe retries every 1s for up to 10s.
  If the upstream does not converge within that window, the command exits 1.

Examples:
  vibew probe
  vibew probe --env production
  vibew probe --env staging`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProbe(cmd, envName)
		},
	}

	cmd.Flags().StringVar(&envName, "env", "", "environment name; loads vibewarden.<name>.yaml and probes tls.domain")

	return cmd
}

// runProbe implements the probe command logic. It returns nil on exit-0 paths
// and a non-nil error on exit-1 paths. The error message is suppressed (caller
// prints nothing) because all user-visible output is written to cmd.OutOrStdout()
// by the renderer.
func runProbe(cmd *cobra.Command, envName string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	w := cmd.OutOrStdout()

	if envName == "" {
		// Dev path: load base config, probe localhost.
		cfg, err := config.Load("")
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		port := cfg.Server.Port
		if port == 0 {
			port = 8443
		}
		targetURL := fmt.Sprintf("https://localhost:%d%s", port, probeHealthPath)

		prober := healthadapter.NewLocalhostProber(probeTimeout)
		svc := probeapp.NewService(prober)

		opts := probeapp.DefaultOptions(targetURL, "")
		result, probeErr := svc.Run(cmd.Context(), opts)
		probeapp.Render(w, result, probeErr)

		return exitCode(result, probeErr)
	}

	// Named-env path: resolve the env, read tls.domain, probe with strict TLS.
	resolver := envapp.NewFileResolver(cwd)
	resolved, err := resolver.Resolve(envName)
	if err != nil {
		if errors.Is(err, envapp.ErrOverrideConfigMissing) {
			return fmt.Errorf("config file not found: vibewarden.%s.yaml", envName)
		}
		if errors.Is(err, envapp.ErrBaseConfigMissing) {
			return fmt.Errorf("config file not found: vibewarden.yaml")
		}
		return fmt.Errorf("resolving env %q: %w", envName, err)
	}

	domain := resolved.Cfg.TLS.Domain
	if domain == "" {
		return fmt.Errorf("tls.domain is empty in merged config; cannot resolve probe target for --env %q", envName)
	}

	targetURL := fmt.Sprintf("https://%s%s", domain, probeHealthPath)

	prober := healthadapter.NewStrictProber(probeTimeout)
	svc := probeapp.NewService(prober)

	opts := probeapp.DefaultOptions(targetURL, envName)
	opts.ProgressWriter = cmd.ErrOrStderr()
	result, probeErr := svc.Run(cmd.Context(), opts)
	probeapp.Render(w, result, probeErr)

	return exitCode(result, probeErr)
}

// exitCode maps a probe result + error to a cobra-compatible return value.
//
// Cobra exits 0 when RunE returns nil. To signal exit 1 without printing the
// cobra error message, we call os.Exit(1) directly after rendering — but to
// keep RunE testable, we instead return a sentinel that the caller can
// intercept in tests. In production (main → Execute), cobra prints any
// non-nil error returned from RunE to stderr; we suppress that by returning a
// special value that the CLI command checks after Render.
//
// The simplest approach: return nil on success, return a wrapped error on
// failure so cobra exits 1. The rendered output is already on stdout — cobra's
// error line goes to stderr and doesn't interfere with the rendered output.
func exitCode(result probeapp.Result, err error) error {
	if err == nil && result.Doc.Components["upstream"] == "ok" {
		return nil
	}
	// Return a sentinel that causes cobra to exit 1. We don't need cobra to
	// print this — the renderer already wrote the user-facing message. But
	// cobra always prints the error from RunE; we use an empty string trick
	// via a silent error type.
	return errSilentExit
}

// errSilentExit is a sentinel error that causes the cobra command to exit
// with code 1 without printing an error message. The probe renderer has
// already written all user-facing output.
var errSilentExit = silentError{}

// silentError is an error type that cobra's Execute() will exit-1 on but
// whose Error() returns an empty string so cobra's default stderr print is
// invisible.
type silentError struct{}

func (silentError) Error() string { return "" }

// Compile-time assertion that silentError implements error.
var _ error = silentError{}
