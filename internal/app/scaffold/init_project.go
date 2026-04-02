package scaffold

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// initDirs lists the empty directories created inside the new project.
// Each receives a .gitkeep file so that they are committed to version control.
var initDirs = []string{
	filepath.Join("internal", "domain"),
	filepath.Join("internal", "ports"),
	filepath.Join("internal", "adapters"),
	filepath.Join("internal", "app"),
}

// sharedAgentTemplateFiles lists the agent templates that are shared across all
// language packs. They live in the agents/ subdirectory of the templates FS.
// Adding a new language pack requires only a language-specific dev.md and a
// project scaffold — architect.md, reviewer.md, and CLAUDE.md come from here.
var sharedAgentTemplateFiles = []struct {
	tmpl string
	dest string
}{
	{tmpl: "agents/architect.md.tmpl", dest: filepath.Join(".claude", "agents", "architect.md")},
	{tmpl: "agents/reviewer.md.tmpl", dest: filepath.Join(".claude", "agents", "reviewer.md")},
}

// goTemplateFiles maps each output path (relative to the project root) to the
// template name it is rendered from. These are Go-language-specific templates.
// Shared agent templates (architect.md, reviewer.md, CLAUDE.md) are in
// sharedAgentTemplateFiles and are rendered separately.
var goTemplateFiles = []struct {
	tmpl string
	dest string
	exec bool // whether the file should be made executable
}{
	{tmpl: "go/vibewarden.yaml.tmpl", dest: "vibewarden.yaml"},
	{tmpl: "go/go.mod.tmpl", dest: "go.mod"},
	{tmpl: "go/dockerfile.tmpl", dest: "Dockerfile"},
	{tmpl: "go/gitignore.tmpl", dest: ".gitignore"},
	{tmpl: "go/dev.md.tmpl", dest: filepath.Join(".claude", "agents", "dev.md")},
}

// InitProjectOptions carries options for full project scaffolding.
type InitProjectOptions struct {
	// ProjectName is the project/directory name.
	ProjectName string

	// ModulePath is the Go module path. When empty, defaults to ProjectName.
	ModulePath string

	// Port is the HTTP port. When zero, defaults to 3000.
	Port int

	// Language is the target language. Required.
	Language domainscaffold.Language

	// Force allows overwriting existing files.
	Force bool

	// Version is the VibeWarden release version written into .vibewarden-version.
	// When empty the wrapper falls back to the latest GitHub release at runtime.
	Version string
}

// InitProjectService scaffolds a complete new project from language-specific templates.
type InitProjectService struct {
	renderer ports.TemplateRenderer
}

// NewInitProjectService creates a new InitProjectService.
func NewInitProjectService(renderer ports.TemplateRenderer) *InitProjectService {
	return &InitProjectService{renderer: renderer}
}

// InitProject creates a new project directory with all scaffolded files.
//
// The directory parentDir/opts.ProjectName is created. If it already exists and
// is non-empty, the call returns an error wrapping os.ErrExist (unless
// opts.Force is true).
//
// The generated structure for --lang go is:
//
//	<project>/
//	├── cmd/<project>/main.go
//	├── internal/{domain,ports,adapters,app}/.gitkeep
//	├── .claude/agents/{architect,dev,reviewer}.md
//	├── CLAUDE.md
//	├── go.mod
//	├── Dockerfile
//	├── vibewarden.yaml
//	├── vibew, vibew.ps1, vibew.cmd, .vibewarden-version
//	└── .gitignore
//
// architect.md and reviewer.md are rendered from language-agnostic shared
// templates (agents/). dev.md is rendered from the Go-specific template.
// CLAUDE.md is rendered from the shared agents/claude.md.tmpl with Go-specific
// code conventions appended from go/claude.md.tmpl.
func (s *InitProjectService) InitProject(ctx context.Context, parentDir string, opts InitProjectOptions) error {
	if opts.Language != domainscaffold.LanguageGo {
		return fmt.Errorf("unsupported language %q (supported: go)", opts.Language)
	}

	if opts.ProjectName == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	if strings.ContainsAny(opts.ProjectName, "/\\") {
		return fmt.Errorf("project name must not contain path separators")
	}

	if opts.ModulePath == "" {
		opts.ModulePath = opts.ProjectName
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
		ModulePath:  opts.ModulePath,
		Port:        opts.Port,
		Language:    opts.Language,
	}

	// Render shared (language-agnostic) agent templates: architect.md, reviewer.md.
	for _, tf := range sharedAgentTemplateFiles {
		dest := filepath.Join(projectDir, tf.dest)
		if err := s.renderer.RenderToFile(tf.tmpl, data, dest, opts.Force); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("rendering %s: %w", tf.dest, err)
			}
			return fmt.Errorf("%s already exists; use --force to overwrite: %w", tf.dest, err)
		}
	}

	// Render CLAUDE.md by concatenating the shared base template with the
	// Go-specific code conventions appendix. This keeps CLAUDE.md output
	// identical to the pre-refactor output while the base content is now
	// shared across all language packs.
	if err := s.renderCombinedCLAUDEmd(projectDir, data, opts.Force); err != nil {
		return fmt.Errorf("rendering CLAUDE.md: %w", err)
	}

	// Render Go-language-specific template files.
	for _, tf := range goTemplateFiles {
		dest := filepath.Join(projectDir, tf.dest)
		if err := s.renderer.RenderToFile(tf.tmpl, data, dest, opts.Force); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("rendering %s: %w", tf.dest, err)
			}
			return fmt.Errorf("%s already exists; use --force to overwrite: %w", tf.dest, err)
		}
	}

	// Render main.go into cmd/<project>/.
	mainPath := filepath.Join(projectDir, "cmd", opts.ProjectName, "main.go")
	if err := s.renderer.RenderToFile("go/main.go.tmpl", data, mainPath, opts.Force); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering main.go: %w", err)
		}
		return fmt.Errorf("main.go already exists; use --force to overwrite: %w", err)
	}

	// Create empty package-structure directories with .gitkeep.
	for _, d := range initDirs {
		dir := filepath.Join(projectDir, d)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
		keepPath := filepath.Join(dir, ".gitkeep")
		if _, statErr := os.Stat(keepPath); errors.Is(statErr, os.ErrNotExist) {
			if writeErr := os.WriteFile(keepPath, nil, 0o600); writeErr != nil {
				return fmt.Errorf("creating .gitkeep in %s: %w", d, writeErr)
			}
		}
	}

	// Render the vibew wrapper scripts using the shared Service helper.
	// NewService with a nil detector is safe here because renderWrappers only
	// calls s.renderer, never s.detector.
	wrapSvc := NewService(s.renderer, nil)
	wrapData := domainscaffold.TemplateData{
		UpstreamPort: opts.Port,
		Version:      opts.Version,
	}
	if err := wrapSvc.renderWrappers(projectDir, wrapData, opts.Force); err != nil {
		return fmt.Errorf("rendering wrapper scripts: %w", err)
	}

	return nil
}

// renderCombinedCLAUDEmd renders CLAUDE.md by concatenating the shared
// agents/claude.md.tmpl with the language-specific go/claude.md.tmpl appendix.
// The combined output is written to CLAUDE.md in projectDir.
//
// This design keeps the vibew CLI reference and sidecar context in a single
// shared template. Adding a new language pack appends its own code conventions
// without duplicating the shared content.
func (s *InitProjectService) renderCombinedCLAUDEmd(projectDir string, data any, overwrite bool) error {
	dest := filepath.Join(projectDir, "CLAUDE.md")

	if !overwrite {
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("file already exists: %w", os.ErrExist)
		}
	}

	shared, err := s.renderer.Render("agents/claude.md.tmpl", data)
	if err != nil {
		return fmt.Errorf("rendering shared claude.md base: %w", err)
	}

	goConventions, err := s.renderer.Render("go/claude.md.tmpl", data)
	if err != nil {
		return fmt.Errorf("rendering go claude.md conventions: %w", err)
	}

	combined := bytes.Join([][]byte{shared, goConventions}, []byte("\n"))

	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}
	if err := os.WriteFile(dest, combined, 0o600); err != nil {
		return fmt.Errorf("writing CLAUDE.md: %w", err)
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
