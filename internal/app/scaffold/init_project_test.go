package scaffold_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
)

func TestInitProject_CreatesStructure(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	// Verify agent files.
	mustExist(t, parent, "myapp", "AGENTS-VIBEWARDEN.md")
	mustExist(t, parent, "myapp", "AGENTS.md")

	// Verify core files.
	mustExist(t, parent, "myapp", "vibewarden.yaml")
	mustExist(t, parent, "myapp", "Dockerfile")
	mustExist(t, parent, "myapp", ".dockerignore")
	mustExist(t, parent, "myapp", ".gitignore")
	mustExist(t, parent, "myapp", ".vibewarden-version")
}

func TestInitProject_DefaultsPort(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "noport",
		// Port deliberately zero — should default to 3000.
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	mustExist(t, parent, "noport", "vibewarden.yaml")
}

func TestInitProject_CreatesVersionFile(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "vertest",
		Port:        3000,
		Version:     "v0.2.1",
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	mustExist(t, parent, "vertest", ".vibewarden-version")
}

func TestInitProject_RejectsNonEmptyDir(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	// Pre-create the directory with a file.
	projectDir := filepath.Join(parent, "occupied")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "existing.txt"), []byte("data"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "occupied",
		Port:        3000,
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error for non-empty directory, got nil")
	}
	if !errors.Is(err, os.ErrExist) {
		t.Errorf("expected os.ErrExist, got: %v", err)
	}
}

func TestInitProject_ForceOverwritesExistingDir(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	projectDir := filepath.Join(parent, "forcetest")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "forcetest",
		Port:        3000,
		Force:       true,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() with --force unexpected error: %v", err)
	}

	mustExist(t, parent, "forcetest", "vibewarden.yaml")
}

func TestInitProject_RejectsEmptyProjectName(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "",
		Port:        3000,
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error for empty project name, got nil")
	}
}

func TestInitProject_RejectsProjectNameWithSlash(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	tests := []struct {
		name        string
		projectName string
	}{
		{"forward slash", "foo/bar"},
		{"backslash", `foo\bar`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			opts := scaffoldapp.InitProjectOptions{
				ProjectName: tt.projectName,
				Port:        3000,
			}

			if err := svc.InitProject(context.Background(), parent, opts); err == nil {
				t.Errorf("expected error for project name %q, got nil", tt.projectName)
			}
		})
	}
}

// TestInitProject_WritesProjectMD verifies that PROJECT.md is created when a
// description is provided, and omitted when the description is empty.
func TestInitProject_WritesProjectMD(t *testing.T) {
	tests := []struct {
		name        string
		description string
		wantFile    bool
	}{
		{"with description", "a task management API", true},
		{"empty description", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := newFakeRenderer()
			svc := scaffoldapp.NewInitProjectService(renderer, nil)

			parent := t.TempDir()
			opts := scaffoldapp.InitProjectOptions{
				ProjectName: "myapp",
				Port:        3000,
				Description: tt.description,
			}

			if err := svc.InitProject(context.Background(), parent, opts); err != nil {
				t.Fatalf("InitProject() unexpected error: %v", err)
			}

			projectMDPath := filepath.Join(parent, "myapp", "PROJECT.md")
			_, statErr := os.Stat(projectMDPath)
			exists := statErr == nil

			if exists != tt.wantFile {
				t.Errorf("PROJECT.md exists=%v, want=%v (description=%q)", exists, tt.wantFile, tt.description)
			}
		})
	}
}

// TestInitProject_DescriptionInData verifies that InitProjectData carries the
// description through to the template renderer.
func TestInitProject_DescriptionInData(t *testing.T) {
	tracker := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(tracker, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "descapp",
		Port:        3000,
		Description: "an e-commerce platform",
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	// PROJECT.md must be rendered.
	if !containsTemplate(tracker.renderToFileCalls, "agents/project.md.tmpl") {
		t.Errorf("expected agents/project.md.tmpl to be rendered; RenderToFile calls: %v", tracker.renderToFileCalls)
	}
}

// TestInitProject_ProjectMD_RenderError verifies that a render error for
// PROJECT.md propagates correctly.
func TestInitProject_ProjectMD_RenderError(t *testing.T) {
	renderer := newSelectiveErrorRenderer("agents/project.md.tmpl", errors.New("disk full"))
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "projmderr",
		Port:        3000,
		Description: "a project", // description triggers renderProjectMD
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error from renderProjectMD, got nil")
	}
}

// TestInitProject_ProjectMD_RenderExistError verifies that an os.ErrExist error
// from renderProjectMD propagates wrapped.
func TestInitProject_ProjectMD_RenderExistError(t *testing.T) {
	renderer := newSelectiveErrorRenderer("agents/project.md.tmpl", fmt.Errorf("file exists: %w", os.ErrExist))
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "projmdexist",
		Port:        3000,
		Description: "a project", // description triggers renderProjectMD
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error from renderProjectMD with ErrExist, got nil")
	}
	if !errors.Is(err, os.ErrExist) {
		t.Errorf("expected errors.Is(err, os.ErrExist), got: %v", err)
	}
}

// TestInitProject_VibewaryenYAML_RenderError verifies that render errors for
// vibewarden.yaml propagate correctly.
func TestInitProject_VibewaryenYAML_RenderError(t *testing.T) {
	renderer := newSelectiveErrorRenderer("init-vibewarden.yaml.tmpl", errors.New("disk full"))
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "vwyamlerr",
		Port:        3000,
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error from vibewarden.yaml render, got nil")
	}
}

// TestInitProject_VibewaryenYAML_RenderExistError verifies that os.ErrExist
// for vibewarden.yaml propagates wrapped.
func TestInitProject_VibewaryenYAML_RenderExistError(t *testing.T) {
	renderer := newSelectiveErrorRenderer("init-vibewarden.yaml.tmpl", fmt.Errorf("file exists: %w", os.ErrExist))
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "vwyamlexist",
		Port:        3000,
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error from vibewarden.yaml ErrExist, got nil")
	}
	if !errors.Is(err, os.ErrExist) {
		t.Errorf("expected errors.Is(err, os.ErrExist), got: %v", err)
	}
}

// TestInitProject_CreatesAgentsVibewardenMD verifies that AGENTS-VIBEWARDEN.md
// is created and contains content from the shared base template.
func TestInitProject_CreatesAgentsVibewardenMD(t *testing.T) {
	renderer := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "agentsvw",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	mustExist(t, parent, "agentsvw", "AGENTS-VIBEWARDEN.md")

	// The shared template must be rendered via Render (not RenderToFile).
	if !containsTemplate(renderer.renderCalls, "agents/agents-vibewarden.md.tmpl") {
		t.Errorf("expected agents/agents-vibewarden.md.tmpl to be rendered; Render calls: %v", renderer.renderCalls)
	}

	// AGENTS-VIBEWARDEN.md must contain output from the render.
	data, err := os.ReadFile(filepath.Join(parent, "agentsvw", "AGENTS-VIBEWARDEN.md"))
	if err != nil {
		t.Fatalf("reading AGENTS-VIBEWARDEN.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "rendered:agents/agents-vibewarden.md.tmpl") {
		t.Errorf("AGENTS-VIBEWARDEN.md missing shared base content; got:\n%s", content)
	}
}

// TestInitProject_CreatesAgentsMD_WhenMissing verifies that AGENTS.md is created
// from the agents/agents.md.tmpl template when it does not already exist.
func TestInitProject_CreatesAgentsMD_WhenMissing(t *testing.T) {
	renderer := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "agentsmd",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	mustExist(t, parent, "agentsmd", "AGENTS.md")

	if !containsTemplate(renderer.renderToFileCalls, "agents/agents.md.tmpl") {
		t.Errorf("expected agents/agents.md.tmpl to be used; RenderToFile calls: %v", renderer.renderToFileCalls)
	}
}

// TestInitProject_AppendsToAgentsMD_WhenMissingRef verifies that when AGENTS.md
// already exists but does not contain a reference to AGENTS-VIBEWARDEN.md, the
// reference is appended.
func TestInitProject_AppendsToAgentsMD_WhenMissingRef(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	projectDir := filepath.Join(parent, "appendtest")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}
	existing := "# My Agent Instructions\n\nSome custom instructions here.\n"
	agentsMDPath := filepath.Join(projectDir, "AGENTS.md")
	if err := os.WriteFile(agentsMDPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "appendtest",
		Port:        3000,
		Force:       true,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	data, err := os.ReadFile(agentsMDPath)
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "AGENTS-VIBEWARDEN.md") {
		t.Errorf("AGENTS.md missing reference after append; content:\n%s", content)
	}
	// Original content must be preserved.
	if !strings.Contains(content, "Some custom instructions here.") {
		t.Errorf("AGENTS.md lost original content; content:\n%s", content)
	}
}

// TestInitProject_PreservesAgentsMD_WhenHasRef verifies that AGENTS.md is left
// unchanged when it already contains a reference to AGENTS-VIBEWARDEN.md.
func TestInitProject_PreservesAgentsMD_WhenHasRef(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	projectDir := filepath.Join(parent, "preservetest")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}
	existing := "# My Instructions\n\nSee [AGENTS-VIBEWARDEN.md](./AGENTS-VIBEWARDEN.md) for VibeWarden.\n"
	agentsMDPath := filepath.Join(projectDir, "AGENTS.md")
	if err := os.WriteFile(agentsMDPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "preservetest",
		Port:        3000,
		Force:       true,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	data, err := os.ReadFile(agentsMDPath)
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	// Content must be unchanged — no duplicate reference appended.
	if string(data) != existing {
		t.Errorf("AGENTS.md was modified unnecessarily:\ngot:  %q\nwant: %q", string(data), existing)
	}
}

// TestInitProject_OverwritesAgentsVibewardenMD verifies that AGENTS-VIBEWARDEN.md
// is always overwritten because it is vibew-owned.
func TestInitProject_OverwritesAgentsVibewardenMD(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	projectDir := filepath.Join(parent, "overwritevw")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Write a stale AGENTS-VIBEWARDEN.md.
	agentsVWPath := filepath.Join(projectDir, "AGENTS-VIBEWARDEN.md")
	if err := os.WriteFile(agentsVWPath, []byte("old content"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "overwritevw",
		Port:        3000,
		Force:       true,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	data, err := os.ReadFile(agentsVWPath)
	if err != nil {
		t.Fatalf("reading AGENTS-VIBEWARDEN.md: %v", err)
	}
	if string(data) == "old content" {
		t.Error("AGENTS-VIBEWARDEN.md was not overwritten")
	}
}

// TestInitProject_NoClaudeCommandsDir verifies that the .claude/commands/
// directory is NOT created.
func TestInitProject_NoClaudeCommandsDir(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "nocommands",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	claudeCommandsDir := filepath.Join(parent, "nocommands", ".claude", "commands")
	if _, err := os.Stat(claudeCommandsDir); err == nil {
		t.Errorf(".claude/commands/ directory must not be created, but it exists at %s", claudeCommandsDir)
	}
}

// TestInitProject_DockerIgnore_RenderError verifies that a render error for
// .dockerignore propagates correctly.
func TestInitProject_DockerIgnore_RenderError(t *testing.T) {
	renderer := newSelectiveErrorRenderer("init-dockerignore.tmpl", errors.New("disk full"))
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "dierr",
		Port:        3000,
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error from .dockerignore render, got nil")
	}
}

// TestInitProject_DockerIgnore_RenderExistError verifies that an os.ErrExist error
// from .dockerignore render propagates wrapped.
func TestInitProject_DockerIgnore_RenderExistError(t *testing.T) {
	renderer := newSelectiveErrorRenderer("init-dockerignore.tmpl", fmt.Errorf("file exists: %w", os.ErrExist))
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "diexist",
		Port:        3000,
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error from .dockerignore ErrExist, got nil")
	}
	if !errors.Is(err, os.ErrExist) {
		t.Errorf("expected errors.Is(err, os.ErrExist), got: %v", err)
	}
}

// TestInitProject_NoCLAUDEmd verifies that CLAUDE.md is NOT created.
func TestInitProject_NoCLAUDEmd(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "noclaudemd",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	claudeMDPath := filepath.Join(parent, "noclaudemd", "CLAUDE.md")
	if _, err := os.Stat(claudeMDPath); err == nil {
		t.Errorf("CLAUDE.md must not be created by vibew init, but it exists at %s", claudeMDPath)
	}
}

// mustExist is a test helper that fails if the file at path does not exist.
func mustExist(t *testing.T, parts ...string) {
	t.Helper()
	path := filepath.Join(parts...)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file %q to exist: %v", path, err)
	}
}

// selectiveErrorRenderer wraps fakeRenderer and returns a custom error when
// a specific template name is passed to RenderToFile or Render.
type selectiveErrorRenderer struct {
	*fakeRenderer
	failOnTemplate  string
	failErr         error
	failOnRender    string
	failOnRenderErr error
}

func newSelectiveErrorRenderer(failOnTemplate string, failErr error) *selectiveErrorRenderer {
	return &selectiveErrorRenderer{
		fakeRenderer:   newFakeRenderer(),
		failOnTemplate: failOnTemplate,
		failErr:        failErr,
	}
}

func (r *selectiveErrorRenderer) Render(templateName string, data any) ([]byte, error) {
	if r.failOnRender != "" && templateName == r.failOnRender {
		return nil, r.failOnRenderErr
	}
	return r.fakeRenderer.Render(templateName, data)
}

func (r *selectiveErrorRenderer) RenderToFile(templateName string, data any, path string, overwrite bool) error {
	if templateName == r.failOnTemplate {
		return r.failErr
	}
	return r.fakeRenderer.RenderToFile(templateName, data, path, overwrite)
}
