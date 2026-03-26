// Package ops contains application services for operational commands
// (dev, status, doctor).
package ops

import (
	"context"
	"fmt"
	"io"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// DevService orchestrates the "vibewarden dev" use case.
// It starts the Docker Compose stack and prints service URLs.
type DevService struct {
	compose ports.ComposeRunner
}

// NewDevService creates a new DevService.
func NewDevService(compose ports.ComposeRunner) *DevService {
	return &DevService{compose: compose}
}

// DevOptions holds options for the dev command.
type DevOptions struct {
	// Observability enables the "observability" compose profile, which starts
	// Prometheus and Grafana alongside the core stack.
	Observability bool
}

// Run starts the Docker Compose stack and prints the service URLs.
// The cfg is used to derive service addresses for the post-start summary.
func (s *DevService) Run(ctx context.Context, cfg *config.Config, opts DevOptions, out io.Writer) error {
	var profiles []string
	if opts.Observability {
		profiles = append(profiles, "observability")
	}

	fmt.Fprintln(out, "Starting VibeWarden dev environment...")
	if opts.Observability {
		fmt.Fprintln(out, "Observability profile enabled (Prometheus + Grafana).")
	}

	if err := s.compose.Up(ctx, profiles); err != nil {
		return fmt.Errorf("starting dev environment: %w", err)
	}

	printServiceURLs(cfg, opts, out)
	return nil
}

// printServiceURLs writes a human-readable summary of the running services.
func printServiceURLs(cfg *config.Config, opts DevOptions, out io.Writer) {
	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	proxyPort := cfg.Server.Port
	if proxyPort == 0 {
		proxyPort = 8080
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Dev environment started. Service URLs:")
	fmt.Fprintf(out, "  Proxy (VibeWarden):  %s://localhost:%d\n", scheme, proxyPort)
	fmt.Fprintf(out, "  Health:              %s://localhost:%d/_vibewarden/health\n", scheme, proxyPort)
	fmt.Fprintf(out, "  Metrics:             %s://localhost:%d/_vibewarden/metrics\n", scheme, proxyPort)
	fmt.Fprintf(out, "  Kratos (public):     http://localhost:4433\n")
	fmt.Fprintf(out, "  Mailslurper (UI):    http://localhost:4437\n")
	if opts.Observability {
		fmt.Fprintf(out, "  Prometheus:          http://localhost:9090\n")
		fmt.Fprintf(out, "  Grafana:             http://localhost:3000\n")
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Tip: run 'vibewarden status' to check component health.")
}
