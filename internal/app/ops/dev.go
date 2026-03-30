// Package ops contains application services for operational commands
// (dev, status, doctor).
package ops

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const generatedOutputDir = ".vibewarden/generated"

// DevService orchestrates the "vibewarden dev" use case.
// It optionally generates runtime configuration files from vibewarden.yaml
// before starting the Docker Compose stack.
type DevService struct {
	compose   ports.ComposeRunner
	generator ports.ConfigGenerator // optional; nil disables generation
}

// NewDevService creates a new DevService without config generation.
// Use NewDevServiceWithGenerator to enable automatic config generation.
func NewDevService(compose ports.ComposeRunner) *DevService {
	return &DevService{compose: compose}
}

// NewDevServiceWithGenerator creates a DevService that calls generator.Generate
// before starting the compose stack.
func NewDevServiceWithGenerator(compose ports.ComposeRunner, generator ports.ConfigGenerator) *DevService {
	return &DevService{compose: compose, generator: generator}
}

// DevOptions holds options for the dev command.
type DevOptions struct {
	// Observability enables the "observability" compose profile, which starts
	// Prometheus and Grafana alongside the core stack.
	Observability bool
}

// Run generates runtime config files (when a generator is configured), then
// starts the Docker Compose stack and prints the service URLs.
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

	// Determine the compose file path to use.
	composeFile, err := s.resolveComposeFile(ctx, cfg, out)
	if err != nil {
		return fmt.Errorf("resolving compose file: %w", err)
	}

	if err := s.compose.Up(ctx, composeFile, profiles); err != nil {
		return fmt.Errorf("starting dev environment: %w", err)
	}

	printServiceURLs(cfg, opts, out)
	return nil
}

// resolveComposeFile determines the docker-compose.yml path to pass to
// docker compose up:
//
//  1. When a ConfigGenerator is wired, generate files under
//     .vibewarden/generated/ and return the generated compose file path.
//  2. When a hand-crafted docker-compose.yml exists in the working directory,
//     return an empty string so docker compose uses its default discovery.
//  3. Otherwise return an empty string (backward-compatible fallback).
func (s *DevService) resolveComposeFile(ctx context.Context, cfg *config.Config, out io.Writer) (string, error) {
	if s.generator != nil {
		fmt.Fprintln(out, "Generating runtime configuration files...")
		if err := s.generator.Generate(ctx, cfg, generatedOutputDir); err != nil {
			return "", fmt.Errorf("generating config: %w", err)
		}
		composePath := filepath.Join(generatedOutputDir, "docker-compose.yml")
		fmt.Fprintf(out, "Generated files written to %s\n", generatedOutputDir)
		return composePath, nil
	}

	// No generator: fall back to an existing docker-compose.yml in the cwd.
	if _, err := os.Stat("docker-compose.yml"); err == nil {
		return "", nil // docker compose will pick it up automatically
	}

	// Nothing available — return empty and let docker compose fail with a
	// clear error message.
	return "", nil
}

// printServiceURLs writes a human-readable summary of the running services.
func printServiceURLs(cfg *config.Config, opts DevOptions, out io.Writer) {
	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	proxyPort := cfg.Server.Port
	if proxyPort == 0 {
		proxyPort = 8443
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
