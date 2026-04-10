package scaffold

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// InitProjectOptions carries options for full project scaffolding.
type InitProjectOptions struct {
	// ProjectName is the project/directory name.
	ProjectName string

	// Port is the HTTP port. When zero, defaults to 3000.
	Port int

	// Force allows overwriting existing files.
	Force bool

	// Version is the VibeWarden release version written into .vibewarden-version.
	// When empty the wrapper falls back to the latest GitHub release at runtime.
	Version string

	// Description is an optional one-line description of what the project builds.
	// When set it is included in PROJECT.md and injected into agent templates.
	Description string
}

// InitProjectService scaffolds a complete new project.
type InitProjectService struct {
	renderer ports.TemplateRenderer
}

// NewInitProjectService creates a new InitProjectService.
// The skillsFS parameter is accepted for backwards compatibility but is no
// longer used; pass nil.
func NewInitProjectService(renderer ports.TemplateRenderer, _ any) *InitProjectService {
	return &InitProjectService{renderer: renderer}
}

// InitProject creates a new project directory with all scaffolded files.
//
// The directory parentDir/opts.ProjectName is created. If it already exists and
// is non-empty, the call returns an error wrapping os.ErrExist (unless
// opts.Force is true).
//
// The generated structure is:
//
//	<project>/
//	├── AGENTS.md                  (user-owned, references AGENTS-VIBEWARDEN.md)
//	├── AGENTS-VIBEWARDEN.md       (vibew-owned, regenerated on updates)
//	├── vibewarden.yaml
//	├── Dockerfile                 (generic placeholder)
//	├── .dockerignore
//	├── .gitignore
//	└── .vibewarden-version
//
// AGENTS-VIBEWARDEN.md consolidates all vibew-specific agent instructions.
// AGENTS.md is user-owned; it is created with a reference to AGENTS-VIBEWARDEN.md
// when absent, or the reference is appended when it is missing from an existing file.
func (s *InitProjectService) InitProject(ctx context.Context, parentDir string, opts InitProjectOptions) error {
	if opts.ProjectName == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	if strings.ContainsAny(opts.ProjectName, "/\\") {
		return fmt.Errorf("project name must not contain path separators")
	}

	if opts.Port == 0 {
		opts.Port = 3000
	}

	projectDir := filepath.Join(filepath.Clean(parentDir), opts.ProjectName)

	if !opts.Force {
		if err := assertEmptyOrAbsent(projectDir); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		return fmt.Errorf("creating project directory: %w", err)
	}

	data := domainscaffold.InitProjectData{
		ProjectName: opts.ProjectName,
		Port:        opts.Port,
		Description: opts.Description,
	}

	// Render AGENTS-VIBEWARDEN.md — always overwritten (vibew-owned file).
	if err := s.renderAgentsVibewardenMD(projectDir, data); err != nil {
		return fmt.Errorf("rendering AGENTS-VIBEWARDEN.md: %w", err)
	}

	// Ensure AGENTS.md exists and contains a reference to AGENTS-VIBEWARDEN.md.
	if err := s.ensureAgentsMD(projectDir); err != nil {
		return fmt.Errorf("ensuring AGENTS.md: %w", err)
	}

	// Render vibewarden.yaml.
	if err := s.renderer.RenderToFile("init-vibewarden.yaml.tmpl", data, filepath.Join(projectDir, "vibewarden.yaml"), opts.Force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering vibewarden.yaml: %w", err)
		}
		return fmt.Errorf("vibewarden.yaml already exists; use --force to overwrite: %w", err)
	}

	// Render generic Dockerfile.
	if err := s.renderer.RenderToFile("init-dockerfile.tmpl", data, filepath.Join(projectDir, "Dockerfile"), opts.Force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering Dockerfile: %w", err)
		}
		return fmt.Errorf("dockerfile already exists; use --force to overwrite: %w", err)
	}

	// Render .dockerignore.
	if err := s.renderer.RenderToFile("init-dockerignore.tmpl", data, filepath.Join(projectDir, ".dockerignore"), opts.Force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering .dockerignore: %w", err)
		}
		return fmt.Errorf(".dockerignore already exists; use --force to overwrite: %w", err)
	}

	// Render .gitignore.
	if err := s.renderer.RenderToFile("init-gitignore.tmpl", data, filepath.Join(projectDir, ".gitignore"), opts.Force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering .gitignore: %w", err)
		}
		return fmt.Errorf(".gitignore already exists; use --force to overwrite: %w", err)
	}

	// Write PROJECT.md when a description was supplied.
	if opts.Description != "" {
		if err := s.renderProjectMD(projectDir, data, opts.Force); err != nil {
			return fmt.Errorf("rendering PROJECT.md: %w", err)
		}
	}

	// Write .vibewarden-version for version pinning.
	versionPath := filepath.Join(projectDir, ".vibewarden-version")
	if opts.Version != "" {
		if err := os.WriteFile(versionPath, []byte(opts.Version+"\n"), 0o600); err != nil {
			return fmt.Errorf("writing .vibewarden-version: %w", err)
		}
	} else {
		if err := os.WriteFile(versionPath, []byte(""), 0o600); err != nil {
			return fmt.Errorf("writing .vibewarden-version: %w", err)
		}
	}

	// Initialize a git repository and create an initial commit.
	if err := s.initGitRepo(projectDir); err != nil {
		// Non-fatal — log and continue. The project is usable without git.
		fmt.Fprintf(os.Stderr, "warning: could not initialize git repo: %v\n", err)
	}

	return nil
}

// initGitRepo runs `git init` and creates an initial commit in projectDir.
// Requires git to be installed. Returns an error if git is not available.
func (s *InitProjectService) initGitRepo(dir string) error {
	cmds := []struct {
		args []string
	}{
		{[]string{"git", "init"}},
		{[]string{"git", "add", "."}},
		{[]string{"git", "commit", "-m", "Initial commit — scaffolded with vibew init"}},
	}
	for _, c := range cmds {
		cmd := exec.CommandContext(context.Background(), c.args[0], c.args[1:]...) //nolint:gosec // args are static strings
		cmd.Dir = dir
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", c.args[0], err)
		}
	}
	return nil
}

// renderAgentsVibewardenMD renders AGENTS-VIBEWARDEN.md from the shared
// agents/agents-vibewarden.md.tmpl template.
//
// AGENTS-VIBEWARDEN.md is always overwritten because it is vibew-owned.
func (s *InitProjectService) renderAgentsVibewardenMD(projectDir string, data any) error {
	dest := filepath.Join(projectDir, "AGENTS-VIBEWARDEN.md")

	rendered, err := s.renderer.Render("agents/agents-vibewarden.md.tmpl", data)
	if err != nil {
		return fmt.Errorf("rendering agents-vibewarden.md base: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}
	if err := os.WriteFile(dest, rendered, 0o600); err != nil {
		return fmt.Errorf("writing AGENTS-VIBEWARDEN.md: %w", err)
	}
	return nil
}

// ensureAgentsMD ensures AGENTS.md exists and contains a reference to
// AGENTS-VIBEWARDEN.md. It delegates to the package-level ensureAgentsMD
// function so that the same logic is shared with AgentContextService.
func (s *InitProjectService) ensureAgentsMD(projectDir string) error {
	dest := filepath.Join(projectDir, "AGENTS.md")
	return ensureAgentsMD(s.renderer, dest)
}

// renderProjectMD renders PROJECT.md from the shared project-md template into
// projectDir. PROJECT.md captures the project description so that AI coding
// assistants always have context about the project's purpose.
func (s *InitProjectService) renderProjectMD(projectDir string, data any, overwrite bool) error {
	dest := filepath.Join(projectDir, "PROJECT.md")
	if err := s.renderer.RenderToFile("agents/project.md.tmpl", data, dest, overwrite); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering PROJECT.md: %w", err)
		}
		return fmt.Errorf("PROJECT.md already exists; use --force to overwrite: %w", err)
	}
	return nil
}

// assertEmptyOrAbsent returns an error wrapping os.ErrExist when projectDir
// exists and contains at least one entry.
func assertEmptyOrAbsent(projectDir string) error {
	entries, err := os.ReadDir(projectDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking project directory: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf(
			"directory %q already exists and is not empty: %w",
			projectDir, os.ErrExist,
		)
	}
	return nil
}
