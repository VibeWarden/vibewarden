// Package deploy provides the application service that deploys a VibeWarden
// project to a remote server over SSH.
package deploy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

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

// Service orchestrates the "vibew deploy" use case.
// It generates runtime config, transfers files to the remote, starts Docker
// Compose, and verifies the sidecar health endpoint.
type Service struct {
	executor  ports.RemoteExecutor
	generator ports.ConfigGenerator
	httpDo    func(req *http.Request) (*http.Response, error)
}

// NewService creates a Service.
// executor handles SSH commands and rsync transfers.
// generator is used to produce the .vibewarden/generated/ files before transfer.
// httpDo is the HTTP function used for health checks; pass nil to use the default
// http.DefaultClient.Do.
func NewService(
	executor ports.RemoteExecutor,
	generator ports.ConfigGenerator,
	httpDo func(req *http.Request) (*http.Response, error),
) *Service {
	if httpDo == nil {
		httpDo = http.DefaultClient.Do
	}
	return &Service{
		executor:  executor,
		generator: generator,
		httpDo:    httpDo,
	}
}

// RunOptions holds parameters for a deploy run.
type RunOptions struct {
	// ConfigPath is the path to vibewarden.yaml on the local filesystem.
	ConfigPath string

	// ProjectName is used as the remote sub-directory name under remoteBaseDir.
	// When empty it is derived from the basename of the directory containing
	// ConfigPath.
	ProjectName string

	// GeneratedDir is the local directory where generated files are written
	// before transfer. Defaults to ".vibewarden/generated" when empty.
	GeneratedDir string

	// Out is the writer used for progress messages. May be nil (output is
	// discarded).
	Out io.Writer
}

// Deploy runs the full deployment flow:
//  1. Load and validate config
//  2. Generate runtime files
//  3. Verify Docker + Docker Compose on the remote
//  4. rsync generated files + vibewarden.yaml to the remote
//  5. docker compose pull && docker compose up -d
//  6. Health check
func (s *Service) Deploy(ctx context.Context, cfg *config.Config, opts RunOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	projectName := opts.ProjectName
	if projectName == "" {
		projectName = projectNameFromConfig(opts.ConfigPath)
	}
	remoteDir := remoteBaseDir + "/" + projectName + "/"

	// Step 1: generate runtime configuration files.
	fmt.Fprintln(out, "Generating runtime configuration files...")
	generatedDir := opts.GeneratedDir
	if generatedDir == "" {
		generatedDir = ".vibewarden/generated"
	}
	if err := s.generator.Generate(ctx, cfg, generatedDir); err != nil {
		return fmt.Errorf("generating config files: %w", err)
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

	// rsync generated files.
	if err := s.executor.Transfer(ctx, generatedDir, remoteDir, true); err != nil {
		return fmt.Errorf("transferring generated files: %w", err)
	}

	// rsync vibewarden.yaml (single file: wrap in a temp dir is not needed —
	// use rsync with explicit source file).
	configDir := filepath.Dir(opts.ConfigPath)
	configFile := filepath.Base(opts.ConfigPath)
	configSrc := filepath.Join(configDir, configFile)
	if err := s.executor.Transfer(ctx, configSrc, remoteDir+configFile, false); err != nil {
		return fmt.Errorf("transferring %s: %w", configFile, err)
	}

	// Step 4: start Docker Compose on the remote.
	fmt.Fprintln(out, "Pulling Docker images on remote...")
	pullCmd := fmt.Sprintf("cd %s && docker compose pull", remoteDir)
	if _, err := s.executor.Run(ctx, pullCmd); err != nil {
		return fmt.Errorf("docker compose pull: %w", err)
	}

	fmt.Fprintln(out, "Starting services on remote...")
	upCmd := fmt.Sprintf("cd %s && docker compose up -d", remoteDir)
	if _, err := s.executor.Run(ctx, upCmd); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}

	// Step 5: health check.
	port := cfg.Server.Port
	if port == 0 {
		port = defaultHealthPort
	}
	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	healthURL := fmt.Sprintf("%s://localhost:%d/_vibewarden/health", scheme, port)
	fmt.Fprintf(out, "Waiting for sidecar health check at %s...\n", healthURL)
	if err := s.waitHealthy(ctx, healthURL, out); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	fmt.Fprintln(out, "Deploy complete.")
	return nil
}

// StatusOptions holds parameters for the deploy status command.
type StatusOptions struct {
	// Out is the writer used for status output. May be nil.
	Out io.Writer
}

// Status fetches Docker Compose service state from the remote.
func (s *Service) Status(ctx context.Context, opts StatusOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	output, err := s.executor.Run(ctx, "docker compose --project-directory ~/vibewarden/ ps")
	if err != nil {
		return fmt.Errorf("fetching remote status: %w", err)
	}
	fmt.Fprintln(out, output)
	return nil
}

// LogsOptions holds parameters for the deploy logs command.
type LogsOptions struct {
	// Lines is the number of log lines to retrieve (0 = all).
	Lines int
	// Out is the writer used for log output. May be nil.
	Out io.Writer
}

// Logs retrieves Docker Compose logs from the remote.
func (s *Service) Logs(ctx context.Context, opts LogsOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	cmd := "docker compose --project-directory ~/vibewarden/ logs"
	if opts.Lines > 0 {
		cmd += fmt.Sprintf(" --tail=%d", opts.Lines)
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

// waitHealthy polls healthURL until the sidecar responds with a 2xx status or
// the context deadline / healthCheckTimeout expires.
func (s *Service) waitHealthy(ctx context.Context, healthURL string, out io.Writer) error {
	deadline := time.Now().Add(healthCheckTimeout)
	attempt := 0

	for {
		attempt++
		ok, err := s.checkHealth(ctx, healthURL)
		if ok {
			fmt.Fprintln(out, "Sidecar is healthy.")
			return nil
		}
		if err != nil {
			fmt.Fprintf(out, "  attempt %d: %v\n", attempt, err)
		} else {
			fmt.Fprintf(out, "  attempt %d: not yet healthy\n", attempt)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("sidecar did not become healthy within %s", healthCheckTimeout)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for health: %w", ctx.Err())
		case <-time.After(healthCheckInterval):
		}
	}
}

// checkHealth performs a single GET request to healthURL and returns true when
// the response status is 2xx.
func (s *Service) checkHealth(ctx context.Context, healthURL string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false, fmt.Errorf("creating health request: %w", err)
	}
	resp, err := s.httpDo(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close() //nolint:errcheck
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

// projectNameFromConfig derives a project name from the config file path.
// It returns the base name of the directory containing the config file, which
// is the project directory name by convention.
func projectNameFromConfig(configPath string) string {
	if configPath == "" {
		return "vibewarden"
	}
	dir := filepath.Dir(filepath.Clean(configPath))
	name := filepath.Base(dir)
	// Sanitise: replace spaces and dots with dashes.
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, ".", "-")
	if name == "" || name == "." {
		return "vibewarden"
	}
	return name
}
