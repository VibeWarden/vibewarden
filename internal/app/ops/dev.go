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
	"strings"
	"time"

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

// sidecarServiceName is the Compose service name for the VibeWarden sidecar
// as defined in the generated docker-compose.yml template.
const sidecarServiceName = "vibewarden"

// SidecarSettleDuration is the time to wait after compose up before checking
// whether the sidecar container is still running. It is a package-level
// variable so that tests can override it to avoid real delays.
var SidecarSettleDuration = 5 * time.Second

// MaxContainerAge is the maximum allowed age of an app container before it is
// considered stale and automatically rebuilt. It is a package-level variable so
// that tests can override it without real delays.
var MaxContainerAge = 12 * time.Hour

// NowFunc is the clock function used by freshness checks. It is a package-level
// variable so that tests can inject a deterministic time without real delays.
var NowFunc = time.Now

// appServiceName is the Compose service name for the user's app as defined in
// the generated docker-compose.yml template.
const appServiceName = "app"

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

	// Verbose streams the full docker compose stderr to the user during
	// successful startup. When false, compose stderr is suppressed unless
	// docker compose up fails, in which case the captured stderr is always
	// surfaced so users can see the actual build error.
	Verbose bool
}

// Run generates runtime config files (when a generator is configured), then
// starts the Docker Compose stack and prints the service URLs.
// The cfg is used to derive service addresses for the post-start summary.
// When opts.Watch is true and a ConfigWatcher is wired, Run also starts the
// watch loop and blocks until ctx is cancelled.
func (s *DevService) Run(ctx context.Context, cfg *config.Config, opts DevOptions, out io.Writer) error {
	fmt.Fprintln(out, "Starting VibeWarden dev environment...")

	// Warn when letsencrypt is configured in dev mode — ACME challenges will
	// fail on localhost since the server is not publicly reachable.
	if cfg.TLS.Enabled && cfg.TLS.Provider == "letsencrypt" {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Warning: tls.provider is 'letsencrypt' -- ACME HTTP-01 challenges require a")
		fmt.Fprintln(out, "publicly reachable server. Local dev will use self-signed certificates instead.")
		fmt.Fprintln(out, "Set tls.provider: self-signed to suppress this warning.")
		fmt.Fprintln(out, "")
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

	// Pre-flight: detect and rebuild stale/mismatched app containers.
	if err := s.checkContainerFreshness(ctx, cfg, composeFile, out); err != nil {
		return err
	}

	upOpts := ports.ComposeUpOptions{}
	if opts.Verbose {
		// Stream compose stderr live when --verbose is set so users see
		// build progress in real time.
		upOpts.Stderr = out
	}
	if err := s.compose.Up(ctx, composeFile, nil, upOpts); err != nil {
		return fmt.Errorf("starting dev environment: %w", err)
	}

	// Post-start: verify the sidecar container is still running.
	if err := s.verifySidecar(ctx, composeFile, out); err != nil {
		return err
	}

	// Scan for any unhealthy containers and emit a warning above the
	// summary if found. Unhealthy-at-start is a UX warning, not a hard
	// failure — hard failures are caught by the stderr-surfacing path above.
	warning := s.scanUnhealthyContainers(ctx, composeFile)
	if warning != "" {
		fmt.Fprintln(out, warning)
	}

	printStartupSummary(cfg, opts, out)

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
				if err := s.generator.Generate(ctx, cfg.ToGeneratorInput(), generatedOutputDir); err != nil {
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
		if err := s.generator.Generate(ctx, cfg.ToGeneratorInput(), generatedOutputDir); err != nil {
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

// checkContainerFreshness inspects running app containers for staleness.
// It detects three conditions that indicate a stale container:
//  1. Project name mismatch — the container belongs to a different compose project.
//  2. Image mismatch — the container was built from a different image than configured.
//  3. Age exceeded — the container was created more than MaxContainerAge ago.
//
// When any mismatch is detected, it prints a diagnostic message and calls
// compose.Restart with --force-recreate --build to rebuild the app container.
//
// The check is skipped when:
//   - Neither cfg.App.Image nor cfg.App.Build is set (no app service configured).
//   - PS fails (graceful degradation, same pattern as verifySidecar).
//   - No app container is found in PS output.
func (s *DevService) checkContainerFreshness(ctx context.Context, cfg *config.Config, composeFile string, out io.Writer) error {
	if cfg.App.Image == "" && cfg.App.Build == "" {
		// Nothing to check when no app service is configured.
		return nil
	}

	containers, err := s.compose.PS(ctx, composeFile)
	if err != nil {
		// PS failure is not fatal — skip the freshness check gracefully.
		slog.Warn("could not check container freshness", "error", err)
		return nil
	}

	expectedProject := cfg.ComposeProjectName()
	expectedImage := cfg.App.Image

	for _, c := range containers {
		if c.Service != appServiceName {
			continue
		}

		reason := s.detectStaleness(c, expectedProject, expectedImage)
		if reason == "" {
			return nil
		}

		fmt.Fprintf(out, "Stale app container detected: %s\n", reason)
		fmt.Fprintln(out, "Rebuilding app container (--force-recreate --build)...")

		if err := s.compose.Restart(ctx, composeFile, []string{appServiceName}); err != nil {
			return fmt.Errorf("rebuilding stale app container: %w", err)
		}
		return nil
	}

	// No app container found — nothing to check.
	return nil
}

// detectStaleness returns a non-empty reason string when the container is
// considered stale. Returns an empty string when the container is fresh.
func (s *DevService) detectStaleness(c ports.ContainerInfo, expectedProject, expectedImage string) string {
	// Check project name mismatch.
	if c.Project != "" && expectedProject != "" && c.Project != expectedProject {
		return fmt.Sprintf("project name mismatch (container: %q, expected: %q)", c.Project, expectedProject)
	}

	// Check image mismatch.
	if c.Image != "" && expectedImage != "" && c.Image != expectedImage {
		return fmt.Sprintf("image mismatch (container: %q, expected: %q)", c.Image, expectedImage)
	}

	// Check container age.
	if !c.CreatedAt.IsZero() {
		age := NowFunc().Sub(c.CreatedAt)
		if age > MaxContainerAge {
			return fmt.Sprintf("container age %s exceeds maximum %s", age.Truncate(time.Minute), MaxContainerAge)
		}
	}

	return ""
}

// verifySidecar waits briefly after compose up, then checks whether the
// sidecar container is still running. If it exited or is restarting, the
// last few lines of its logs are printed and an error is returned so that
// the command exits non-zero instead of printing a misleading success message.
func (s *DevService) verifySidecar(ctx context.Context, composeFile string, out io.Writer) error {
	fmt.Fprintln(out, "Waiting for sidecar to settle...")

	select {
	case <-time.After(SidecarSettleDuration):
	case <-ctx.Done():
		return nil
	}

	containers, err := s.compose.PS(ctx, composeFile)
	if err != nil {
		// PS failure is not fatal — the containers might still be starting.
		slog.Warn("could not check sidecar status", "error", err)
		return nil
	}

	for _, c := range containers {
		if c.Service != sidecarServiceName {
			continue
		}

		state := strings.ToLower(c.State)
		if state == "running" {
			return nil
		}

		// Sidecar is not running — fetch logs for diagnosis.
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Sidecar container is not running (state: "+c.State+").")

		logs, logsErr := s.compose.Logs(ctx, composeFile, sidecarServiceName, 20)
		if logsErr != nil {
			slog.Warn("could not fetch sidecar logs", "error", logsErr)
		} else if logs != "" {
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Last sidecar logs:")
			fmt.Fprintln(out, logs)
		}

		fmt.Fprintln(out, "Sidecar failed to start — run vibew logs or vibew doctor for details.")
		return fmt.Errorf("sidecar failed to start (state: %s)", c.State) //nolint:err113 // dynamic user-facing error
	}

	// Sidecar container not found in PS output — might not be part of the
	// compose project (e.g. user-managed compose file). Not an error.
	return nil
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

// scanUnhealthyContainers inspects container health across the compose
// project and returns a warning line for the first unhealthy container found.
// Returns an empty string when all containers are healthy (or when PS fails —
// graceful degradation, same pattern as verifySidecar).
func (s *DevService) scanUnhealthyContainers(ctx context.Context, composeFile string) string {
	containers, err := s.compose.PS(ctx, composeFile)
	if err != nil {
		return ""
	}
	for _, c := range containers {
		if strings.EqualFold(c.Health, "unhealthy") {
			return fmt.Sprintf("Warning: service %q unhealthy — run 'vibew logs -f %s'", c.Service, c.Service)
		}
	}
	return ""
}

// summaryURL derives the user-facing URL for the started sidecar:
//   - scheme is "https" when TLS is enabled, "http" otherwise.
//   - host resolves "0.0.0.0", "", and "127.0.0.1" to "localhost".
//   - port falls back to 8443 when unset.
//
// It is a pure function for easy unit testing.
func summaryURL(cfg *config.Config) string {
	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	host := cfg.Server.Host
	switch host {
	case "", "0.0.0.0", "127.0.0.1":
		host = "localhost"
	}
	port := cfg.Server.Port
	if port == 0 {
		port = 8443
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// printStartupSummary writes the post-start hint block.
// Format:
//
//	Started. <url>
//	Logs: vibew logs -f
//	Stop: vibew down
func printStartupSummary(cfg *config.Config, _ DevOptions, out io.Writer) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Dev environment started.")
	fmt.Fprintf(out, "Started. %s\n", summaryURL(cfg))
	fmt.Fprintln(out, "Logs: vibew logs -f")
	fmt.Fprintln(out, "Stop: vibew down")
}
