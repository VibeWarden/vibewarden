// Package deploy provides the application service that deploys a VibeWarden
// project to a remote server over SSH.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ErrHealthCheck is returned when the sidecar health check fails after
// deployment. Files have been deployed and services started, but the
// sidecar did not become healthy within the timeout.
var ErrHealthCheck = errors.New("health check failed")

// DriftError is returned when remote files have diverged from local files
// and --force was not specified. It carries the list of changes that would
// be applied so the CLI can display them to the user.
type DriftError struct {
	// Changes is a list of human-readable descriptions of files that would
	// be modified or deleted on the remote host.
	Changes []string
}

// Error implements the error interface.
func (e *DriftError) Error() string {
	var b strings.Builder
	b.WriteString("remote files have been modified since last deploy:\n")
	for _, c := range e.Changes {
		b.WriteString("  ")
		b.WriteString(c)
		b.WriteString("\n")
	}
	b.WriteString("\nRun 'vibew deploy --force' to overwrite, or back up changes first.")
	return b.String()
}

const (
	// remoteBaseDir is the root directory on the remote host where all
	// VibeWarden projects are deployed.
	remoteBaseDir = "~/vibewarden"

	// healthCheckTimeout is the maximum time to wait for the sidecar to become
	// healthy after starting Docker Compose.
	healthCheckTimeout = 60 * time.Second

	// healthCheckInterval is the delay between successive health check attempts.
	healthCheckInterval = 3 * time.Second

	// defaultHealthPort is the port used when cfg.Server.Port is zero.
	defaultHealthPort = 8443
)

// buildContextExcludes is the list of rsync --exclude patterns used when
// transferring the app build context to the remote host. These patterns
// prevent the project's original files from overwriting the deploy bundle's
// merged versions (e.g. the merged vibewarden.yaml with production TLS
// settings must not be clobbered by the base config from the source tree).
var buildContextExcludes = []string{
	"vibewarden.yaml",
	"vibewarden.*.yaml",
	".vibewarden",
	".credentials",
	".env",
	".env.template",
	"docker-compose.yml",
}

// Service orchestrates the "vibew deploy" use case.
// It generates runtime config, transfers files to the remote, starts Docker
// Compose, and verifies the sidecar health endpoint.
type Service struct {
	executor      ports.RemoteExecutor
	generator     ports.ConfigGenerator
	imageExporter ports.ImageExporter
}

// NewService creates a Service.
// executor handles SSH commands and rsync transfers.
// generator is used to produce the .vibewarden/generated/ files before transfer.
func NewService(
	executor ports.RemoteExecutor,
	generator ports.ConfigGenerator,
) *Service {
	return &Service{
		executor:  executor,
		generator: generator,
	}
}

// WithImageExporter sets the ImageExporter on the Service and returns it for
// chaining. The exporter is used to save Docker images from the local daemon so
// they can be transferred to the remote host when the image name has no registry
// prefix (bare name like "myapp:latest").
func (s *Service) WithImageExporter(exporter ports.ImageExporter) *Service {
	s.imageExporter = exporter
	return s
}

// RunOptions holds parameters for a deploy run.
type RunOptions struct {
	// ConfigPath is the path to vibewarden.yaml on the local filesystem.
	ConfigPath string

	// ProdConfigPath is the optional path to the production override file
	// (e.g. vibewarden.production.yaml). When set, its values are deep-merged
	// on top of the base config before writing to the deploy bundle.
	ProdConfigPath string

	// ProjectName is used as the remote sub-directory name under remoteBaseDir.
	// When empty it is derived from the basename of the directory containing
	// ConfigPath.
	ProjectName string

	// GeneratedDir is the local directory where generated files are written
	// before transfer. Defaults to ".vibewarden/generated" when empty.
	GeneratedDir string

	// Env is the deployment environment name (e.g. "production", "staging").
	// Defaults to "production" when empty.
	Env string

	// Force, when true, skips the drift detection warning and overwrites
	// remote files unconditionally. When false (the default), a dry-run rsync
	// is performed before the real transfer and the deploy aborts with an
	// actionable message if remote files have diverged.
	Force bool

	// Out is the writer used for progress messages. May be nil (output is
	// discarded).
	Out io.Writer
}

// Deploy runs the full deployment flow:
//  1. Bundle all deploy files locally into .vibewarden/deploy/
//  2. Verify Docker + Docker Compose on the remote
//  3. rsync .vibewarden/deploy/ to the remote
//     When app.build is set, the app source directory is also transferred so
//     that Docker can build the image remotely.
//  4. docker compose up -d --build  (build mode) or
//     docker compose pull && docker compose up -d  (image mode)
//  5. Health check
func (s *Service) Deploy(ctx context.Context, cfg *config.Config, opts RunOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	projectName := opts.ProjectName
	if projectName == "" {
		projectName = ProjectNameFromConfig(opts.ConfigPath)
	}
	remoteDir := remoteBaseDir + "/" + projectName + "/"

	// Step 1: produce the local deploy bundle.
	fmt.Fprintln(out, "Generating deploy bundle...")
	bundleDir := opts.GeneratedDir
	if bundleDir == "" {
		bundleDir = defaultBundleDir
	}
	if err := s.Bundle(ctx, BundleOptions{
		Config:         cfg,
		ConfigPath:     opts.ConfigPath,
		ProdConfigPath: opts.ProdConfigPath,
		ProjectName:    projectName,
		MultiSite:      false,
		OutputDir:      bundleDir,
		Env:            opts.Env,
	}); err != nil {
		return fmt.Errorf("creating deploy bundle: %w", err)
	}

	// Step 2: verify prerequisites on the remote.
	fmt.Fprintln(out, "Verifying remote prerequisites...")
	if err := s.checkRemotePrerequisites(ctx); err != nil {
		return fmt.Errorf("remote prerequisites check failed: %w", err)
	}

	// Step 3: transfer files.
	fmt.Fprintf(out, "Transferring files to remote %s...\n", remoteDir)

	// Ensure the remote directory exists.
	if _, err := s.executor.Run(ctx, "mkdir -p "+remoteDir); err != nil {
		return fmt.Errorf("creating remote directory: %w", err)
	}

	// Drift detection: before overwriting remote files with --delete, check
	// what would change and abort unless --force is set.
	if !opts.Force {
		changes, err := s.executor.DryRunTransfer(ctx, bundleDir, remoteDir)
		if err != nil {
			// If the dry-run fails (e.g. remote directory does not exist yet on
			// first deploy), fall through -- the real rsync will create it.
			fmt.Fprintf(out, "Note: drift detection skipped (%v)\n", err)
		} else if len(changes) > 0 {
			return &DriftError{Changes: changes}
		}
	}

	// rsync the deploy bundle.
	if err := s.executor.Transfer(ctx, bundleDir, remoteDir, true); err != nil {
		return fmt.Errorf("transferring deploy bundle: %w", err)
	}

	// When app.build is set the image must be built on the remote host.
	// Transfer the app source (the build context directory) so that
	// `docker compose up --build` can build the image remotely.
	//
	// Use TransferExcluding to prevent the project's original config files
	// (vibewarden.yaml, docker-compose.yml, etc.) from overwriting the
	// merged bundle files that were already deployed in the previous step.
	// This is critical when app.build is "." because the build context
	// directory overlaps with the deploy bundle directory on the remote.
	if cfg.App.Build != "" {
		projectRoot := filepath.Dir(filepath.Clean(opts.ConfigPath))
		buildContextLocal := filepath.Join(projectRoot, cfg.App.Build)
		buildContextRemote := remoteDir + strings.TrimPrefix(strings.TrimSuffix(cfg.App.Build, "/"), "./") + "/"
		fmt.Fprintf(out, "Transferring app build context (%s) to remote...\n", cfg.App.Build)
		if err := s.executor.TransferExcluding(ctx, buildContextLocal, buildContextRemote, false, buildContextExcludes); err != nil {
			return fmt.Errorf("transferring app build context: %w", err)
		}
	}

	// Step 4: transfer local image if using a bare image name (no registry prefix)
	// and an image exporter is configured.
	// This must happen before docker compose up so the image is available on the
	// remote daemon.
	localImageTransferred := false
	if cfg.App.Image != "" && isLocalImage(cfg.App.Image) && s.imageExporter != nil {
		if err := s.transferLocalImage(ctx, cfg.App.Image, remoteDir, out); err != nil {
			return fmt.Errorf("transferring local image: %w", err)
		}
		localImageTransferred = true
	}

	// Step 5: start Docker Compose on the remote.
	// Always pull the latest sidecar image before starting, regardless of mode.
	// In build mode the app image is built locally, but the sidecar image may be
	// cached from an older version, so we explicitly pull it first.
	fmt.Fprintln(out, "Pulling latest sidecar image on remote...")
	pullSidecarCmd := fmt.Sprintf("cd %s && docker compose pull vibewarden", remoteDir)
	if _, err := s.executor.Run(ctx, pullSidecarCmd); err != nil {
		return fmt.Errorf("docker compose pull vibewarden: %w", err)
	}

	// When app.build is set, build the image remotely instead of pulling the app image.
	if cfg.App.Build != "" {
		fmt.Fprintln(out, "Building and starting services on remote...")
		upCmd := fmt.Sprintf("cd %s && docker compose up -d --build --force-recreate", remoteDir)
		if _, err := s.executor.Run(ctx, upCmd); err != nil {
			return fmt.Errorf("docker compose up: %w", err)
		}
	} else if localImageTransferred {
		// Local image was already transferred -- skip pulling and just start.
		fmt.Fprintln(out, "Starting services on remote...")
		upCmd := fmt.Sprintf("cd %s && docker compose up -d --force-recreate", remoteDir)
		if _, err := s.executor.Run(ctx, upCmd); err != nil {
			return fmt.Errorf("docker compose up: %w", err)
		}
	} else {
		fmt.Fprintln(out, "Pulling Docker images on remote...")
		pullCmd := fmt.Sprintf("cd %s && docker compose pull", remoteDir)
		if _, err := s.executor.Run(ctx, pullCmd); err != nil {
			return fmt.Errorf("docker compose pull: %w", err)
		}

		fmt.Fprintln(out, "Starting services on remote...")
		upCmd := fmt.Sprintf("cd %s && docker compose up -d --force-recreate", remoteDir)
		if _, err := s.executor.Run(ctx, upCmd); err != nil {
			return fmt.Errorf("docker compose up: %w", err)
		}
	}

	// Step 6: health check -- run curl on the remote so the probe is independent
	// of DNS propagation, external port availability, and TLS certificate issuance.
	// Use the merged config (base + prod overlay) for the correct port and TLS state.
	healthCfg, err := loadMergedConfig(cfg, opts.ProdConfigPath)
	if err != nil {
		return err
	}
	port := healthCfg.Server.Port
	if port == 0 {
		port = defaultHealthPort
	}
	healthURL := healthCheckURL(port, healthCfg.TLS.Enabled)
	fmt.Fprintf(out, "Waiting for sidecar health check at %s (via SSH)...\n", healthURL)
	if !s.waitHealthy(ctx, port, healthCfg.TLS.Enabled, out) {
		fmt.Fprintln(out, "Deploy completed but health check failed — verify with: vibew deploy status")
		return ErrHealthCheck
	}

	fmt.Fprintln(out, "Deploy complete.")
	return nil
}

// StatusOptions holds parameters for the deploy status command.
type StatusOptions struct {
	// ConfigPath is the path to vibewarden.yaml on the local filesystem.
	// It is used to derive the project name (and therefore the remote directory)
	// in exactly the same way as RunOptions.ConfigPath in Deploy. When empty,
	// the project name falls back to "vibewarden".
	ConfigPath string

	// ProjectName overrides the project name derived from ConfigPath.
	ProjectName string

	// Out is the writer used for status output. May be nil.
	Out io.Writer
}

// Status fetches Docker Compose service state from the remote.
func (s *Service) Status(ctx context.Context, opts StatusOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	projectName := opts.ProjectName
	if projectName == "" {
		projectName = ProjectNameFromConfig(opts.ConfigPath)
	}
	remoteDir := remoteBaseDir + "/" + projectName + "/"

	output, err := s.executor.Run(ctx, "docker compose --project-directory "+remoteDir+" ps")
	if err != nil {
		return fmt.Errorf("fetching remote status: %w", err)
	}
	fmt.Fprintln(out, output)
	return nil
}

// LogsOptions holds parameters for the deploy logs command.
type LogsOptions struct {
	// ConfigPath is the path to vibewarden.yaml on the local filesystem.
	// It is used to derive the project name (and therefore the remote directory)
	// in exactly the same way as RunOptions.ConfigPath in Deploy. When empty,
	// the project name falls back to "vibewarden".
	ConfigPath string

	// ProjectName overrides the project name derived from ConfigPath.
	ProjectName string

	// Lines is the number of log lines to retrieve (0 = all).
	Lines int

	// Follow streams new log lines continuously, like "docker compose logs -f".
	// When true the command runs until the context is cancelled (e.g. Ctrl-C).
	// Output is written directly to Out in real-time without buffering.
	Follow bool

	// Out is the writer used for log output. May be nil.
	Out io.Writer
}

// Logs retrieves Docker Compose logs from the remote.
// When opts.Follow is true the command streams log output in real-time by
// using RunStream; the call blocks until the context is cancelled. When false
// the output is fetched in a single buffered Run call and written to opts.Out.
func (s *Service) Logs(ctx context.Context, opts LogsOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	projectName := opts.ProjectName
	if projectName == "" {
		projectName = ProjectNameFromConfig(opts.ConfigPath)
	}
	remoteDir := remoteBaseDir + "/" + projectName + "/"

	cmd := "docker compose --project-directory " + remoteDir + " logs"
	if opts.Lines > 0 {
		cmd += fmt.Sprintf(" --tail=%d", opts.Lines)
	}
	if opts.Follow {
		cmd += " -f"
		if err := s.executor.RunStream(ctx, cmd, out, out); err != nil {
			return fmt.Errorf("streaming remote logs: %w", err)
		}
		return nil
	}

	output, err := s.executor.Run(ctx, cmd)
	if err != nil {
		return fmt.Errorf("fetching remote logs: %w", err)
	}
	fmt.Fprintln(out, output)
	return nil
}

// ListSites returns the names of all site subdirectories under
// ~/vibewarden/sites/ on the remote host. An empty slice is returned
// when no sites exist or the directory is missing.
func (s *Service) ListSites(ctx context.Context) ([]string, error) {
	output, err := s.executor.Run(ctx, "ls -1 "+sitesDir+" 2>/dev/null || true")
	if err != nil {
		return nil, fmt.Errorf("listing remote sites: %w", err)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}
	return strings.Split(output, "\n"), nil
}

// StatusMultiApp fetches Docker Compose service state for multi-app deployments.
// When app is non-empty, it targets that specific site. When app is empty, it
// shows the sidecar status plus all site statuses.
func (s *Service) StatusMultiApp(ctx context.Context, app string, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}

	if app != "" {
		siteDir := sitesDir + app + "/"
		output, err := s.executor.Run(ctx, "docker compose --project-directory "+siteDir+" ps")
		if err != nil {
			return fmt.Errorf("fetching status for site %q: %w", app, err)
		}
		fmt.Fprintf(out, "=== Site: %s ===\n", app)
		fmt.Fprintln(out, output)
		return nil
	}

	// Show sidecar status.
	sidecarOutput, err := s.executor.Run(ctx, "docker compose --project-directory "+sidecarDir+" ps")
	if err != nil {
		return fmt.Errorf("fetching sidecar status: %w", err)
	}
	fmt.Fprintln(out, "=== Sidecar ===")
	fmt.Fprintln(out, sidecarOutput)

	// Enumerate and show each site.
	sites, err := s.ListSites(ctx)
	if err != nil {
		return err
	}
	for _, name := range sites {
		siteDir := sitesDir + name + "/"
		output, siteErr := s.executor.Run(ctx, "docker compose --project-directory "+siteDir+" ps")
		fmt.Fprintf(out, "=== Site: %s ===\n", name)
		if siteErr != nil {
			fmt.Fprintf(out, "  error: %v\n", siteErr)
			continue
		}
		fmt.Fprintln(out, output)
	}
	return nil
}

// LogsMultiApp fetches Docker Compose logs for multi-app deployments.
// When app is non-empty, it targets that specific site. When app is empty, it
// shows logs from the sidecar container.
func (s *Service) LogsMultiApp(ctx context.Context, app string, opts LogsOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	var dir string
	if app != "" {
		dir = sitesDir + app + "/"
	} else {
		dir = sidecarDir
	}

	cmd := "docker compose --project-directory " + dir + " logs"
	if opts.Lines > 0 {
		cmd += fmt.Sprintf(" --tail=%d", opts.Lines)
	}
	if opts.Follow {
		cmd += " -f"
		if err := s.executor.RunStream(ctx, cmd, out, out); err != nil {
			return fmt.Errorf("streaming remote logs: %w", err)
		}
		return nil
	}

	output, err := s.executor.Run(ctx, cmd)
	if err != nil {
		return fmt.Errorf("fetching remote logs: %w", err)
	}
	fmt.Fprintln(out, output)
	return nil
}

// checkRemotePrerequisites verifies that docker and docker compose are available
// on the remote host.
func (s *Service) checkRemotePrerequisites(ctx context.Context) error {
	if _, err := s.executor.Run(ctx, "which docker"); err != nil {
		return fmt.Errorf("docker not found on remote: %w", err)
	}
	if _, err := s.executor.Run(ctx, "docker compose version"); err != nil {
		return fmt.Errorf("docker compose not found on remote: %w", err)
	}
	return nil
}

// waitHealthy polls the sidecar health endpoint on the remote host via SSH
// until curl reports success (exit code 0) or the context deadline /
// healthCheckTimeout expires.
//
// When tlsEnabled is true the probe uses HTTPS with the -k flag (skip TLS
// certificate verification) because we are hitting localhost, not the public
// domain. When TLS is disabled the probe uses plain HTTP.
//
// Running the probe over SSH avoids all DNS propagation, firewall, and TLS
// certificate issuance dependencies — the request is made from inside the
// server to its own localhost interface.
//
// Returns true when the sidecar becomes healthy, false on timeout or context
// cancellation. When false, a warning is printed suggesting diagnostic
// commands the operator can run.
func (s *Service) waitHealthy(ctx context.Context, port int, tlsEnabled bool, out io.Writer) bool {
	cmd := healthCheckCmd(port, tlsEnabled)
	deadline := time.Now().Add(healthCheckTimeout)
	attempt := 0
	var lastErr error

	for {
		attempt++
		_, err := s.executor.Run(ctx, cmd)
		if err == nil {
			fmt.Fprintln(out, "Sidecar is healthy.")
			return true
		}
		lastErr = err
		fmt.Fprintf(out, "  attempt %d: %v\n", attempt, err)

		if time.Now().After(deadline) {
			fmt.Fprintf(out, "Warning: health check timed out (last error: %v). Services may still be starting.\n  Run: vibew deploy status --target <host> to check.\n  Run: vibew doctor --target <host> to diagnose.\n", lastErr)
			return false
		}

		select {
		case <-ctx.Done():
			fmt.Fprintf(out, "Warning: health check cancelled (%v). Services may still be starting.\n  Run: vibew deploy status --target <host> to check.\n  Run: vibew doctor --target <host> to diagnose.\n", ctx.Err())
			return false
		case <-time.After(healthCheckInterval):
		}
	}
}

// healthCheckURL returns the health check URL using HTTPS when TLS is enabled
// and HTTP otherwise. The host is always localhost because the probe runs on
// the remote server itself via SSH.
func healthCheckURL(port int, tlsEnabled bool) string {
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	return fmt.Sprintf("%s://localhost:%d/_vibewarden/health", scheme, port)
}

// healthCheckCmd returns the curl command used to probe the health endpoint.
// When TLS is enabled it adds the -k flag to skip certificate verification,
// since the probe targets localhost rather than the public domain.
func healthCheckCmd(port int, tlsEnabled bool) string {
	if tlsEnabled {
		return fmt.Sprintf("curl -sfk https://localhost:%d/_vibewarden/health", port)
	}
	return fmt.Sprintf("curl -sf http://localhost:%d/_vibewarden/health", port)
}

// ProjectNameFromConfig derives a project name from the config file path.
// It returns the base name of the directory containing the config file, which
// is the project directory name by convention. It is exported so the CLI can
// compute the remote directory before calling Deploy.
//
// When configPath is empty the current working directory name is used as the
// project name. When configPath is relative (e.g. "vibewarden.prod.yaml") it is
// resolved to an absolute path first so that filepath.Dir returns the real
// directory rather than ".".
func ProjectNameFromConfig(configPath string) string {
	if configPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "vibewarden"
		}
		configPath = filepath.Join(wd, "vibewarden.yaml")
	}

	abs, err := filepath.Abs(configPath)
	if err != nil {
		return "vibewarden"
	}

	dir := filepath.Dir(abs)
	name := filepath.Base(dir)
	// Sanitise: replace spaces and dots with dashes.
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, ".", "-")
	if name == "" || name == "." {
		return "vibewarden"
	}
	return name
}
