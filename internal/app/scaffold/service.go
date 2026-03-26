package scaffold

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	vibeWardenYAML     = "vibewarden.yaml"
	dockerComposeYML   = "docker-compose.yml"
	vibeWardenVersionF = ".vibewarden-version"
	vibewShell         = "vibew"
	vibewPowerShell    = "vibew.ps1"
	vibewCmd           = "vibew.cmd"
)

// InitOptions carries the options supplied by the user when running
// `vibewarden init`.
type InitOptions struct {
	// UpstreamPort is the port of the protected application.
	UpstreamPort int

	// AuthEnabled enables Ory Kratos authentication scaffolding.
	AuthEnabled bool

	// RateLimitEnabled enables rate limiting scaffolding.
	RateLimitEnabled bool

	// TLSEnabled enables TLS scaffolding.
	TLSEnabled bool

	// TLSDomain is the domain for the TLS certificate.
	TLSDomain string

	// Force allows overwriting existing files.
	Force bool

	// SkipDocker skips docker-compose.yml generation.
	SkipDocker bool

	// Version is the VibeWarden release version written into .vibewarden-version.
	// When empty the wrapper falls back to the latest GitHub release at runtime.
	Version string

	// SkipWrapper skips generation of the vibew wrapper scripts.
	SkipWrapper bool
}

// Service orchestrates project scaffolding operations.
type Service struct {
	renderer ports.TemplateRenderer
	detector ports.ProjectDetector
}

// NewService creates a new scaffold Service.
func NewService(renderer ports.TemplateRenderer, detector ports.ProjectDetector) *Service {
	return &Service{
		renderer: renderer,
		detector: detector,
	}
}

// Init initialises VibeWarden in a project directory by generating
// vibewarden.yaml, (unless SkipDocker is set) docker-compose.yml, and
// (unless SkipWrapper is set) the vibew wrapper scripts.
//
// If any required file already exists and opts.Force is false, Init returns an
// error wrapping os.ErrExist.
func (s *Service) Init(_ context.Context, dir string, opts InitOptions) error {
	// Detect project to pick up port suggestions etc.
	project, err := s.detector.Detect(dir)
	if err != nil {
		return fmt.Errorf("detecting project: %w", err)
	}

	// Use detected port as fallback when the user did not supply one.
	upstreamPort := opts.UpstreamPort
	if upstreamPort == 0 {
		if project.DetectedPort > 0 {
			upstreamPort = project.DetectedPort
		} else {
			upstreamPort = 3000 // sensible default
		}
	}

	// Guard against unintentional overwrite.
	if !opts.Force {
		if project.HasVibeWardenConfig {
			return fmt.Errorf("vibewarden.yaml already exists in %q: %w", dir, os.ErrExist)
		}
		if !opts.SkipDocker && project.HasDockerCompose {
			return fmt.Errorf("docker-compose.yml already exists in %q: %w", dir, os.ErrExist)
		}
	}

	data := domainscaffold.TemplateData{
		UpstreamPort:     upstreamPort,
		AuthEnabled:      opts.AuthEnabled,
		RateLimitEnabled: opts.RateLimitEnabled,
		TLSEnabled:       opts.TLSEnabled,
		TLSDomain:        opts.TLSDomain,
		Version:          opts.Version,
	}

	// Render vibewarden.yaml.
	vwPath := filepath.Join(dir, vibeWardenYAML)
	if err := s.renderer.RenderToFile("vibewarden.yaml.tmpl", data, vwPath, opts.Force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering vibewarden.yaml: %w", err)
		}
		return fmt.Errorf("vibewarden.yaml already exists; use --force to overwrite: %w", err)
	}

	// Render docker-compose.yml unless skipped.
	if !opts.SkipDocker {
		dcPath := filepath.Join(dir, dockerComposeYML)
		if err := s.renderer.RenderToFile("docker-compose.yml.tmpl", data, dcPath, opts.Force); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("rendering docker-compose.yml: %w", err)
			}
			return fmt.Errorf("docker-compose.yml already exists; use --force to overwrite: %w", err)
		}
	}

	// Render vibew wrapper scripts unless skipped.
	if !opts.SkipWrapper {
		if err := s.renderWrappers(dir, data, opts.Force); err != nil {
			return err
		}
	}

	return nil
}

// renderWrappers generates vibew, vibew.ps1, vibew.cmd and .vibewarden-version
// in dir. The POSIX shell script is made executable (0o755).
func (s *Service) renderWrappers(dir string, data domainscaffold.TemplateData, force bool) error {
	// POSIX shell wrapper — must be executable.
	shellPath := filepath.Join(dir, vibewShell)
	if err := s.renderer.RenderToFile("vibew.tmpl", data, shellPath, force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering vibew: %w", err)
		}
		return fmt.Errorf("vibew already exists; use --force to overwrite: %w", err)
	}
	if err := os.Chmod(shellPath, 0o755); err != nil {
		return fmt.Errorf("setting vibew executable: %w", err)
	}

	// PowerShell wrapper.
	ps1Path := filepath.Join(dir, vibewPowerShell)
	if err := s.renderer.RenderToFile("vibew.ps1.tmpl", data, ps1Path, force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering vibew.ps1: %w", err)
		}
		return fmt.Errorf("vibew.ps1 already exists; use --force to overwrite: %w", err)
	}

	// Batch wrapper.
	cmdPath := filepath.Join(dir, vibewCmd)
	if err := s.renderer.RenderToFile("vibew.cmd.tmpl", data, cmdPath, force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering vibew.cmd: %w", err)
		}
		return fmt.Errorf("vibew.cmd already exists; use --force to overwrite: %w", err)
	}

	// Version pin file.
	versionPath := filepath.Join(dir, vibeWardenVersionF)
	if err := s.renderer.RenderToFile("vibewarden-version.tmpl", data, versionPath, force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering .vibewarden-version: %w", err)
		}
		return fmt.Errorf(".vibewarden-version already exists; use --force to overwrite: %w", err)
	}

	return nil
}
