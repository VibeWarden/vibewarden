package scaffold

import (
	"bytes"
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

// kotlinTemplateFiles maps each output path (relative to the project root) to
// the template name it is rendered from. These are Kotlin-language-specific
// templates using the Ktor framework. Shared agent templates are in
// sharedAgentTemplateFiles and are rendered separately.
var kotlinTemplateFiles = []struct {
	tmpl string
	dest string
}{
	{tmpl: "kotlin/vibewarden.yaml.tmpl", dest: "vibewarden.yaml"},
	{tmpl: "kotlin/build.gradle.kts.tmpl", dest: "build.gradle.kts"},
	{tmpl: "kotlin/settings.gradle.kts.tmpl", dest: "settings.gradle.kts"},
	{tmpl: "kotlin/dockerfile.tmpl", dest: "Dockerfile"},
	{tmpl: "kotlin/gitignore.tmpl", dest: ".gitignore"},
	{tmpl: "kotlin/dev.md.tmpl", dest: filepath.Join(".claude", "agents", "dev.md")},
}

// typescriptTemplateFiles maps each output path (relative to the project root)
// to the template name it is rendered from. These are TypeScript-language-specific
// templates using the Express framework. Shared agent templates are in
// sharedAgentTemplateFiles and are rendered separately.
var typescriptTemplateFiles = []struct {
	tmpl string
	dest string
}{
	{tmpl: "typescript/vibewarden.yaml.tmpl", dest: "vibewarden.yaml"},
	{tmpl: "typescript/package.json.tmpl", dest: "package.json"},
	{tmpl: "typescript/tsconfig.json.tmpl", dest: "tsconfig.json"},
	{tmpl: "typescript/dockerfile.tmpl", dest: "Dockerfile"},
	{tmpl: "typescript/gitignore.tmpl", dest: ".gitignore"},
	{tmpl: "typescript/dev.md.tmpl", dest: filepath.Join(".claude", "agents", "dev.md")},
}

// typescriptInitDirs lists the directories created for TypeScript projects.
// They mirror the hexagonal architecture layout expected by the TS source tree.
var typescriptInitDirs = []string{
	filepath.Join("src", "domain"),
	filepath.Join("src", "ports"),
	filepath.Join("src", "adapters"),
	filepath.Join("src", "app"),
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

	// Description is an optional one-line description of what the project builds.
	// When set it is included in PROJECT.md and injected into agent templates.
	Description string

	// GroupID is the JVM group identifier (e.g., "com.mycompany").
	// Used for Kotlin/JVM package paths. Defaults to sanitized project name.
	GroupID string
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
	switch opts.Language {
	case domainscaffold.LanguageGo, domainscaffold.LanguageKotlin, domainscaffold.LanguageTypeScript:
		// supported
	default:
		return fmt.Errorf("unsupported language %q (supported: go, kotlin, typescript)", opts.Language)
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

	pkgName := domainscaffold.SanitizePackageName(opts.ProjectName)
	groupID := opts.GroupID
	if groupID == "" {
		groupID = pkgName // simple default: project name as group
	}

	data := domainscaffold.InitProjectData{
		ProjectName: opts.ProjectName,
		ModulePath:  opts.ModulePath,
		PackageName: pkgName,
		GroupID:     groupID,
		Port:        opts.Port,
		Language:    opts.Language,
		Description: opts.Description,
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
	// language-specific code conventions appendix. This keeps the vibew CLI
	// reference and sidecar context in a single shared template while each
	// language pack appends its own code conventions.
	if err := s.renderCombinedCLAUDEmd(projectDir, opts.Language, data, opts.Force); err != nil {
		return fmt.Errorf("rendering CLAUDE.md: %w", err)
	}

	// Render language-specific template files and entry point.
	switch opts.Language {
	case domainscaffold.LanguageKotlin:
		if err := s.renderKotlinFiles(projectDir, data, opts.Force); err != nil {
			return err
		}
	case domainscaffold.LanguageTypeScript:
		if err := s.renderTypeScriptFiles(projectDir, data, opts.Force); err != nil {
			return err
		}
		// Create empty source-tree directories with .gitkeep.
		// These mirror the hexagonal architecture layout expected by TypeScript projects.
		for _, d := range typescriptInitDirs {
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
	default: // LanguageGo
		if err := s.renderGoFiles(projectDir, opts.ProjectName, data, opts.Force); err != nil {
			return err
		}
		// Create empty package-structure directories with .gitkeep.
		// These mirror the hexagonal architecture directory layout expected by Go projects.
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

// renderCombinedCLAUDEmd renders CLAUDE.md by concatenating the shared
// agents/claude.md.tmpl with the language-specific <lang>/claude.md.tmpl appendix.
// The combined output is written to CLAUDE.md in projectDir.
//
// This design keeps the vibew CLI reference and sidecar context in a single
// shared template. Adding a new language pack appends its own code conventions
// without duplicating the shared content.
func (s *InitProjectService) renderCombinedCLAUDEmd(projectDir string, lang domainscaffold.Language, data any, overwrite bool) error {
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

	conventionsTmpl := string(lang) + "/claude.md.tmpl"
	langConventions, err := s.renderer.Render(conventionsTmpl, data)
	if err != nil {
		return fmt.Errorf("rendering %s claude.md conventions: %w", lang, err)
	}

	combined := bytes.Join([][]byte{shared, langConventions}, []byte("\n"))

	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}
	if err := os.WriteFile(dest, combined, 0o600); err != nil {
		return fmt.Errorf("writing CLAUDE.md: %w", err)
	}
	return nil
}

// renderGoFiles renders all Go-specific template files into projectDir.
func (s *InitProjectService) renderGoFiles(projectDir, projectName string, data any, overwrite bool) error {
	for _, tf := range goTemplateFiles {
		dest := filepath.Join(projectDir, tf.dest)
		if err := s.renderer.RenderToFile(tf.tmpl, data, dest, overwrite); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("rendering %s: %w", tf.dest, err)
			}
			return fmt.Errorf("%s already exists; use --force to overwrite: %w", tf.dest, err)
		}
	}

	// Render main.go into cmd/<project>/.
	mainPath := filepath.Join(projectDir, "cmd", projectName, "main.go")
	if err := s.renderer.RenderToFile("go/main.go.tmpl", data, mainPath, overwrite); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering main.go: %w", err)
		}
		return fmt.Errorf("main.go already exists; use --force to overwrite: %w", err)
	}
	return nil
}

// renderKotlinFiles renders all Kotlin-specific template files into projectDir.
// The Application.kt entry point is placed at the standard Gradle/Maven source
// path: src/main/kotlin/com/example/<project>/Application.kt.
func (s *InitProjectService) renderKotlinFiles(projectDir string, data domainscaffold.InitProjectData, overwrite bool) error {
	for _, tf := range kotlinTemplateFiles {
		dest := filepath.Join(projectDir, tf.dest)
		if err := s.renderer.RenderToFile(tf.tmpl, data, dest, overwrite); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("rendering %s: %w", tf.dest, err)
			}
			return fmt.Errorf("%s already exists; use --force to overwrite: %w", tf.dest, err)
		}
	}

	// Render Application.kt into the standard Gradle source set path.
	// GroupID "com.mycompany" → "com/mycompany", PackageName "my_app" → "my_app"
	groupParts := filepath.Join(strings.Split(data.GroupID, ".")...)
	appPath := filepath.Join(
		projectDir,
		"src", "main", "kotlin",
		groupParts, data.PackageName,
		"Application.kt",
	)
	if err := s.renderer.RenderToFile("kotlin/Application.kt.tmpl", data, appPath, overwrite); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering Application.kt: %w", err)
		}
		return fmt.Errorf("Application.kt already exists; use --force to overwrite: %w", err) //nolint:revive,staticcheck // user-facing CLI hint: intentional capitalization
	}
	return nil
}

// renderTypeScriptFiles renders all TypeScript-specific template files into
// projectDir. The index.ts entry point is placed at src/index.ts following
// the conventional TypeScript project source layout.
func (s *InitProjectService) renderTypeScriptFiles(projectDir string, data domainscaffold.InitProjectData, overwrite bool) error {
	for _, tf := range typescriptTemplateFiles {
		dest := filepath.Join(projectDir, tf.dest)
		if err := s.renderer.RenderToFile(tf.tmpl, data, dest, overwrite); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("rendering %s: %w", tf.dest, err)
			}
			return fmt.Errorf("%s already exists; use --force to overwrite: %w", tf.dest, err)
		}
	}

	// Render index.ts into the conventional TypeScript source directory.
	indexPath := filepath.Join(projectDir, "src", "index.ts")
	if err := s.renderer.RenderToFile("typescript/index.ts.tmpl", data, indexPath, overwrite); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("rendering index.ts: %w", err)
		}
		return fmt.Errorf("index.ts already exists; use --force to overwrite: %w", err)
	}
	return nil
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
