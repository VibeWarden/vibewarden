// Package ops contains application services for operational commands
// (dev, status, doctor).
package ops

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// buildInstructionsByLang maps known language identifiers (detected from common
// project files) to human-readable build command examples.
var buildInstructionsByLang = map[string]string{
	"go":         "go build -o bin/<project> ./cmd/<project> && vibew build",
	"kotlin":     "./gradlew buildFatJar && vibew build",
	"typescript": "npm run build && vibew build",
	"node":       "npm run build && vibew build",
}

const generatedOutputDir = ".vibewarden/generated"

// DevService orchestrates the "vibew dev" use case.
// It optionally generates runtime configuration files from vibewarden.yaml
// before starting the Docker Compose stack and can watch the config file for
// changes when --watch is enabled.
type DevService struct {
	compose      ports.ComposeRunner
	generator    ports.ConfigGenerator    // optional; nil disables generation
	watcher      ports.ConfigWatcher      // optional; nil disables file watching
	imageChecker ports.DockerImageChecker // optional; nil disables pre-flight image check
}

// NewDevService creates a new DevService without config generation or file watching.
// Use NewDevServiceWithGenerator to enable automatic config generation.
// Use NewDevServiceWithWatcher to also enable config-file watching.
func NewDevService(compose ports.ComposeRunner) *DevService {
	return &DevService{compose: compose}
}

// NewDevServiceWithGenerator creates a DevService that calls generator.Generate
// before starting the compose stack.
func NewDevServiceWithGenerator(compose ports.ComposeRunner, generator ports.ConfigGenerator) *DevService {
	return &DevService{compose: compose, generator: generator}
}

// NewDevServiceWithWatcher creates a DevService that generates config before
// starting the stack and watches the config file for changes, re-generating and
// restarting on each debounced change event.
func NewDevServiceWithWatcher(compose ports.ComposeRunner, generator ports.ConfigGenerator, watcher ports.ConfigWatcher) *DevService {
	return &DevService{compose: compose, generator: generator, watcher: watcher}
}

// WithImageChecker attaches a DockerImageChecker to the DevService.
// When set, Run performs a pre-flight check that the app service image exists
// locally before starting the compose stack.
func (s *DevService) WithImageChecker(checker ports.DockerImageChecker) *DevService {
	s.imageChecker = checker
	return s
}

// DevOptions holds options for the dev command.
type DevOptions struct {
	// Observability enables the "observability" compose profile, which starts
	// Prometheus and Grafana alongside the core stack.
	Observability bool

	// Watch enables file-system watching of vibewarden.yaml.  When true,
	// any write to the config file triggers a regenerate + compose restart
	// cycle after a 500 ms debounce window.  Requires a ConfigWatcher to be
	// wired into the DevService.
	Watch bool

	// ConfigPath is the path to vibewarden.yaml that should be watched.
	// When empty the default "./vibewarden.yaml" is used.
	ConfigPath string

	// DetectedLang is the programming language detected in the project directory
	// (e.g. "go", "kotlin", "typescript"). When non-empty it is used to provide
	// language-specific build instructions in the pre-flight image-missing error.
	DetectedLang string
}

// Run generates runtime config files (when a generator is configured), then
// starts the Docker Compose stack and prints the service URLs.
// The cfg is used to derive service addresses for the post-start summary.
// When opts.Watch is true and a ConfigWatcher is wired, Run also starts the
// watch loop and blocks until ctx is cancelled.
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

	// Pre-flight: verify the app image exists when using a pre-built image.
	if err := s.checkAppImage(ctx, cfg, opts, out); err != nil {
		return err
	}

	if err := s.compose.Up(ctx, composeFile, profiles); err != nil {
		return fmt.Errorf("starting dev environment: %w", err)
	}

	printServiceURLs(cfg, opts, out)

	if opts.Watch && s.watcher != nil {
		return s.watchLoop(ctx, cfg, opts, composeFile, out)
	}
	return nil
}

// watchLoop watches vibewarden.yaml for changes and, on each debounced event,
// re-generates configuration files and restarts the compose stack.
// It blocks until ctx is cancelled.
func (s *DevService) watchLoop(ctx context.Context, cfg *config.Config, opts DevOptions, composeFile string, out io.Writer) error {
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = "vibewarden.yaml"
	}

	ch, err := s.watcher.Watch(ctx, configPath)
	if err != nil {
		return fmt.Errorf("starting config watcher: %w", err)
	}

	fmt.Fprintf(out, "Watching %s for changes (press Ctrl+C to stop)...\n", configPath)

	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ch:
			if !ok {
				// Channel closed — watcher stopped.
				return nil
			}
			slog.Info("config changed, regenerating...")
			fmt.Fprintln(out, "config changed, regenerating...")

			if s.generator != nil {
				if err := s.generator.Generate(ctx, cfg, generatedOutputDir); err != nil {
					slog.Error("regeneration failed", "error", err)
					fmt.Fprintf(out, "regeneration failed: %v\n", err)
					continue
				}
			}

			if err := s.compose.Restart(ctx, composeFile, nil); err != nil {
				slog.Error("compose restart failed", "error", err)
				fmt.Fprintf(out, "compose restart failed: %v\n", err)
			}
		}
	}
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

// checkAppImage verifies that the app service image exists in the local Docker
// daemon before attempting to start the compose stack.
//
// The check is skipped when:
//   - No imageChecker is wired (s.imageChecker == nil).
//   - cfg.App.Image is empty (no image configured).
//   - cfg.App.Build is set (compose will build the image itself).
//
// When the image is missing, a descriptive error with language-specific build
// instructions is returned so the user knows exactly what to do.
func (s *DevService) checkAppImage(ctx context.Context, cfg *config.Config, opts DevOptions, out io.Writer) error {
	if s.imageChecker == nil {
		return nil
	}
	if cfg.App.Image == "" || cfg.App.Build != "" {
		// Nothing to check: no image configured, or compose builds it.
		return nil
	}

	image := cfg.App.Image
	fmt.Fprintf(out, "Checking app image: %s\n", image)

	exists, err := s.imageChecker.ImageExists(ctx, image)
	if err != nil {
		return fmt.Errorf("checking app image %q: %w", image, err)
	}
	if exists {
		return nil
	}

	return buildMissingImageError(image, opts.DetectedLang)
}

// buildMissingImageError constructs a descriptive error for a missing Docker
// image, including language-specific instructions when available.
func buildMissingImageError(image, lang string) error {
	msg := fmt.Sprintf("app image %q not found in the local Docker daemon.\n", image)
	msg += "Build the image first, then run `vibew dev` again.\n\n"

	if instructions, ok := buildInstructionsByLang[lang]; ok {
		msg += fmt.Sprintf("For %s projects:\n  %s\n", lang, instructions)
	} else {
		msg += "Build steps:\n"
		msg += "  1. Build your application binary / artifact.\n"
		msg += "  2. Run `vibew build` to build the Docker image.\n"
		msg += "\n"
		msg += "Tip: set `app.build: \".\"` in vibewarden.yaml to have Compose build the\n"
		msg += "image automatically on every `vibew dev` run (no pre-build required)."
	}

	return fmt.Errorf("%s", msg) //nolint:err113 // dynamic user-facing error message
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
	fmt.Fprintln(out, "Tip: run 'vibew status' to check component health.")
}
