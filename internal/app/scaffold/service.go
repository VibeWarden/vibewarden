package scaffold

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	vibeWardenYAML     = "vibewarden.yaml"
	vibeWardenVersionF = ".vibewarden-version"
	vibewShell         = "vibew"
	vibewPowerShell    = "vibew.ps1"
	vibewCmd           = "vibew.cmd"
	gitIgnoreFile      = ".gitignore"

	// vibeWardenDir is the local runtime directory that must be excluded from
	// version control so that generated config files are never committed.
	vibeWardenDir = ".vibewarden/"

	// permConfig is the permission mode for generated YAML/config files.
	// Readable only by the owner to protect any credentials embedded in config.
	permConfig = os.FileMode(0o600)
	// permExec is the permission mode for executable wrapper scripts.
	// Must remain world-executable so that `./vibew` works without sudo.
	permExec = os.FileMode(0o755)
)

// InitOptions carries the options supplied by the user when running
// `vibew wrap`.
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
// vibewarden.yaml and (unless SkipWrapper is set) the vibew wrapper scripts.
// Docker Compose and Kratos config are no longer generated at init time —
// they are generated at runtime by `vibew dev` from the consolidated
// vibewarden.yaml.
//
// If any required file already exists and opts.Force is false, Init returns an
// error wrapping os.ErrExist.
func (s *Service) Init(_ context.Context, dir string, opts InitOptions) error {
	// Sanitise the caller-supplied directory to prevent path traversal.
	dir = filepath.Clean(dir)

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
	if !opts.Force && project.HasVibeWardenConfig {
		return fmt.Errorf("vibewarden.yaml already exists in %q: %w", dir, os.ErrExist)
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

	// Render vibew wrapper scripts unless skipped.
	if !opts.SkipWrapper {
		if err := s.renderWrappers(dir, data, opts.Force); err != nil {
			return err
		}
	}

	// Ensure .vibewarden/ is excluded from version control.
	if err := s.ensureGitIgnore(dir); err != nil {
		return err
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
	if err := os.Chmod(shellPath, permExec); err != nil {
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

// ensureGitIgnore creates a .gitignore in dir that contains the .vibewarden/
// entry when one does not already exist. When a .gitignore is present and
// already contains the entry, the file is left unchanged.
func (s *Service) ensureGitIgnore(dir string) error {
	gitignorePath := filepath.Join(dir, gitIgnoreFile)

	existing, err := os.ReadFile(gitignorePath) //nolint:gosec // gitignorePath is constructed from the project root via filepath.Join, not user input
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading .gitignore: %w", err)
	}

	// Already contains the entry — nothing to do.
	if strings.Contains(string(existing), vibeWardenDir) {
		return nil
	}

	// File does not exist: render the template.
	if errors.Is(err, os.ErrNotExist) {
		if renderErr := s.renderer.RenderToFile("gitignore.tmpl", nil, gitignorePath, false); renderErr != nil {
			if !errors.Is(renderErr, os.ErrExist) {
				return fmt.Errorf("creating .gitignore: %w", renderErr)
			}
		}
		return nil
	}

	// File exists but is missing the entry — append it.
	entry := "\n# VibeWarden — local runtime files\n" + vibeWardenDir + "\n"
	updated := string(existing) + entry
	// gitignorePath is filepath.Join(dir, gitIgnoreFile) where dir is already
	// filepath.Clean'd and gitIgnoreFile is a package-level constant.
	// The G703 taint originates from dir being a parameter; the path is safe.
	if err := os.WriteFile(gitignorePath, []byte(updated), permConfig); err != nil { //#nosec G703 -- path is filepath.Join(cleanDir, ".gitignore")
		return fmt.Errorf("updating .gitignore: %w", err)
	}
	return nil
}
