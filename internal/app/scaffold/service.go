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
	vibeWardenYAML   = "vibewarden.yaml"
	dockerComposeYML = "docker-compose.yml"
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
// vibewarden.yaml and (unless SkipDocker is set) docker-compose.yml.
//
// If either file already exists and opts.Force is false, Init returns an error
// wrapping os.ErrExist.
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

	return nil
}
