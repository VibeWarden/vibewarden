package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/net/publicsuffix"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	crtshAdapter "github.com/vibewarden/vibewarden/internal/adapters/crtsh"
	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	apptlspreflight "github.com/vibewarden/vibewarden/internal/app/tlspreflight"
	"github.com/vibewarden/vibewarden/internal/config"
)

// NewDoctorCmd creates the "vibew doctor" subcommand.
//
// The command runs a series of independent diagnostics and reports problems.
// It exits with status 1 when any check fails so it can be used in scripts.
func NewDoctorCmd() *cobra.Command {
	var (
		configPath      string
		jsonOutput      bool
		skipLEPreflight bool
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose common configuration and environment issues",
		Long: `Run a series of independent diagnostics and report any issues found.

Checks are organised into two layers:

  Config & Docker (always runs):
    - vibewarden.yaml is present and parses without errors
    - Docker daemon is reachable (docker info)
    - Docker Compose v2+ is available (docker compose version)
    - Required ports are available (proxy port)
    - Generated files are present (.vibewarden/generated/docker-compose.yml)
    - If the stack is running: containers are healthy (docker compose ps)
    - ACME email configured when using ZeroSSL
    - Expected app image exists locally (image tag consistency)
    - LE rate-limit budget (when tls.provider is "letsencrypt")

  Local Runtime (always runs):
    - Upstream application is reachable (HTTP GET)
    - TLS certificate is valid (if self-signed)

Each check runs independently — a failure does not stop subsequent checks.
Exit code is 1 when any check fails.

The LE rate-limit check queries the public crt.sh Certificate Transparency log
to count certificates issued for your domain in the last 168 hours. This
reveals whether the Let's Encrypt 5-certs-per-domain-per-week budget is near
exhaustion before attempting TLS issuance. Pass --skip-le-preflight (or set
tls.skip_rate_limit_check: true in vibewarden.yaml) to suppress this check.

Note: querying crt.sh sends your domain name to a public service. The
certificate, once issued, will be publicly visible in CT logs anyway.

Examples:
  vibew doctor
  vibew doctor --config ./my-vibewarden.yaml
  vibew doctor --json
  vibew doctor --skip-le-preflight`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Load config — pass nil-safe; doctor will report missing config.
			cfg, loadErr := config.Load(configPath)
			if loadErr != nil {
				// Report but don't abort — doctor can still run Docker checks.
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not load config: %v\n", loadErr)
				cfg = nil
			}

			// If cfg is nil we use zero-value defaults so the service doesn't panic.
			if cfg == nil {
				cfg = &config.Config{}
			}

			workDir, err := os.Getwd()
			if err != nil {
				workDir = "."
			}

			compose := opsadapter.NewComposeAdapter()
			portChecker := opsadapter.NewNetPortChecker()
			httpClient := &http.Client{Timeout: 5 * time.Second}
			healthChecker := opsadapter.NewHTTPHealthChecker(httpClient)
			ownerProbe := opsadapter.NewVibeWardenHealthProbe(nil)

			proxyHost := cfg.Server.Host
			if proxyHost == "" {
				proxyHost = "127.0.0.1"
			}
			proxyPort := cfg.Server.Port
			if proxyPort == 0 {
				proxyPort = 8443
			}
			tlsResolver := opsapp.NewChainResolver(
				caddyadapter.NewInProcessResolver(cfg),
				caddyadapter.NewHandshakeResolver(cfg, proxyHost, proxyPort),
			)

			// Wire the LE rate-limit preflight service. The crt.sh HTTP client
			// uses a separate 10-second timeout per AC-8 (separate from the
			// 5-second healthChecker client above).
			ctClient := crtshAdapter.NewClient(&http.Client{Timeout: 10 * time.Second})
			leRateLimitSvc := apptlspreflight.NewService(ctClient)

			svc := opsapp.NewDoctorService(compose, portChecker, healthChecker).
				WithImageChecker(opsadapter.NewImageCheckerAdapter()).
				WithPortOwnerProbe(ownerProbe).
				WithTLSStateResolver(tlsResolver).
				WithLERateLimitService(leRateLimitSvc)

			label := configPath
			if label == "" {
				label = "vibewarden.yaml"
			}

			// Normalise domains to eTLD+1 for the LE rate-limit check.
			registeredDomains, skippedDomains := deriveRegisteredDomains(cfg)

			opts := opsapp.DoctorOptions{
				ConfigPath:          label,
				WorkDir:             workDir,
				JSON:                jsonOutput,
				SkipLEPreflight:     skipLEPreflight,
				LERegisteredDomains: registeredDomains,
				LESkippedDomains:    skippedDomains,
			}

			allOK, err := svc.Run(cmd.Context(), cfg, opts, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			if !allOK {
				return errors.New("one or more checks failed")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output results as JSON")
	cmd.Flags().BoolVar(&skipLEPreflight, "skip-le-preflight", false,
		"skip the Let's Encrypt rate-limit preflight check (equivalent to tls.skip_rate_limit_check: true)")

	return cmd
}

// deriveRegisteredDomains returns the deduplicated set of eTLD+1 domains to
// check for LE rate limits, plus a list of FQDNs that could not be normalised
// (e.g. single-label hostnames like "localhost").
//
// The registered slice is passed to the preflight service for CT queries.
// The skipped slice is passed so the doctor can emit a SeverityWarn result
// for each un-normalisable domain per ADR-090 instead of staying silent.
//
// In multi-site mode (ADR-068), the caller would extend this function to
// iterate over all site domains; for now, only the primary TLS domain is used.
func deriveRegisteredDomains(cfg *config.Config) (registered []string, skipped []string) {
	if cfg.TLS.Domain == "" {
		return nil, nil
	}
	reg, err := publicsuffix.EffectiveTLDPlusOne(cfg.TLS.Domain)
	if err != nil {
		// Single-label hostnames (e.g. "localhost") cannot be normalised to
		// eTLD+1. Return the FQDN in the skipped slice so the doctor emits
		// a WARN result instead of silently omitting the check.
		return nil, []string{cfg.TLS.Domain}
	}
	return []string{reg}, nil
}
