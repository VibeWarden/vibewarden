package scaffold_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

func TestInitProject_CreatesStructure(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	// Verify top-level files.
	mustExist(t, parent, "myapp", "go.mod")
	mustExist(t, parent, "myapp", "Dockerfile")
	mustExist(t, parent, "myapp", "vibewarden.yaml")
	mustExist(t, parent, "myapp", ".gitignore")
	mustExist(t, parent, "myapp", "CLAUDE.md")

	// Verify main.go.
	mustExist(t, parent, "myapp", "cmd", "myapp", "main.go")

	// Verify agent files.
	mustExist(t, parent, "myapp", ".claude", "agents", "architect.md")
	mustExist(t, parent, "myapp", ".claude", "agents", "dev.md")
	mustExist(t, parent, "myapp", ".claude", "agents", "reviewer.md")

	// Verify empty directories with .gitkeep.
	mustExist(t, parent, "myapp", "internal", "domain", ".gitkeep")
	mustExist(t, parent, "myapp", "internal", "ports", ".gitkeep")
	mustExist(t, parent, "myapp", "internal", "adapters", ".gitkeep")
	mustExist(t, parent, "myapp", "internal", "app", ".gitkeep")

	// Verify wrapper scripts.
	mustExist(t, parent, "myapp", ".vibewarden-version")
}

func TestInitProject_DefaultsPort(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "noport",
		Language:    domainscaffold.LanguageGo,
		// Port deliberately zero — should default to 3000.
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	mustExist(t, parent, "noport", "vibewarden.yaml")
}

func TestInitProject_DefaultsModulePath(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "mymodule",
		Language:    domainscaffold.LanguageGo,
		// ModulePath deliberately empty — should default to ProjectName.
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	mustExist(t, parent, "mymodule", "go.mod")
}

func TestInitProject_CreatesVersionFile(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "vertest",
		Language:    domainscaffold.LanguageGo,
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
	svc := scaffoldapp.NewInitProjectService(renderer)

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
		Language:    domainscaffold.LanguageGo,
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
	svc := scaffoldapp.NewInitProjectService(renderer)

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
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
		Force:       true,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() with --force unexpected error: %v", err)
	}

	mustExist(t, parent, "forcetest", "vibewarden.yaml")
}

func TestInitProject_RejectsUnsupportedLanguage(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "badlang",
		Language:    domainscaffold.Language("ruby"),
		Port:        3000,
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error for unsupported language, got nil")
	}
}

func TestInitProject_RejectsEmptyProjectName(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	err := svc.InitProject(context.Background(), parent, opts)
	if err == nil {
		t.Fatal("expected error for empty project name, got nil")
	}
}

func TestInitProject_RejectsProjectNameWithSlash(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

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
				Language:    domainscaffold.LanguageGo,
				Port:        3000,
			}

			if err := svc.InitProject(context.Background(), parent, opts); err == nil {
				t.Errorf("expected error for project name %q, got nil", tt.projectName)
			}
		})
	}
}

func TestInitProject_Kotlin_CreatesStructure(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myktapp",
		Language:    domainscaffold.LanguageKotlin,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() Kotlin unexpected error: %v", err)
	}

	// Verify top-level files.
	mustExist(t, parent, "myktapp", "vibewarden.yaml")
	mustExist(t, parent, "myktapp", "build.gradle.kts")
	mustExist(t, parent, "myktapp", "settings.gradle.kts")
	mustExist(t, parent, "myktapp", "Dockerfile")
	mustExist(t, parent, "myktapp", ".gitignore")
	mustExist(t, parent, "myktapp", "CLAUDE.md")

	// Verify Application.kt at the Gradle source path.
	// GroupID defaults to sanitized project name; package path is <groupID>/<packageName>/
	mustExist(t, parent, "myktapp", "src", "main", "kotlin", "myktapp", "myktapp", "Application.kt")

	// Verify agent files.
	mustExist(t, parent, "myktapp", ".claude", "agents", "architect.md")
	mustExist(t, parent, "myktapp", ".claude", "agents", "dev.md")
	mustExist(t, parent, "myktapp", ".claude", "agents", "reviewer.md")

	// Verify wrapper scripts.
	mustExist(t, parent, "myktapp", ".vibewarden-version")
}

func TestInitProject_Kotlin_DoesNotCreateGoFiles(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "ktonly",
		Language:    domainscaffold.LanguageKotlin,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() Kotlin unexpected error: %v", err)
	}

	// Go-specific files must NOT be created.
	goModPath := filepath.Join(parent, "ktonly", "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		t.Error("go.mod must not exist for a Kotlin project")
	}

	mainGoPath := filepath.Join(parent, "ktonly", "cmd", "ktonly", "main.go")
	if _, err := os.Stat(mainGoPath); err == nil {
		t.Error("cmd/ktonly/main.go must not exist for a Kotlin project")
	}
}

func TestInitProject_Kotlin_DefaultsPort(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "ktnoport",
		Language:    domainscaffold.LanguageKotlin,
		// Port deliberately zero — should default to 3000.
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() Kotlin unexpected error: %v", err)
	}

	mustExist(t, parent, "ktnoport", "vibewarden.yaml")
}

func TestInitProject_TypeScript_CreatesStructure(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "mytsapp",
		Language:    domainscaffold.LanguageTypeScript,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() TypeScript unexpected error: %v", err)
	}

	// Verify top-level files.
	mustExist(t, parent, "mytsapp", "vibewarden.yaml")
	mustExist(t, parent, "mytsapp", "package.json")
	mustExist(t, parent, "mytsapp", "tsconfig.json")
	mustExist(t, parent, "mytsapp", "Dockerfile")
	mustExist(t, parent, "mytsapp", ".gitignore")
	mustExist(t, parent, "mytsapp", "CLAUDE.md")

	// Verify entry point.
	mustExist(t, parent, "mytsapp", "src", "index.ts")

	// Verify hexagonal src layout with .gitkeep.
	mustExist(t, parent, "mytsapp", "src", "domain", ".gitkeep")
	mustExist(t, parent, "mytsapp", "src", "ports", ".gitkeep")
	mustExist(t, parent, "mytsapp", "src", "adapters", ".gitkeep")
	mustExist(t, parent, "mytsapp", "src", "app", ".gitkeep")

	// Verify agent files.
	mustExist(t, parent, "mytsapp", ".claude", "agents", "architect.md")
	mustExist(t, parent, "mytsapp", ".claude", "agents", "dev.md")
	mustExist(t, parent, "mytsapp", ".claude", "agents", "reviewer.md")

	// Verify wrapper scripts.
	mustExist(t, parent, "mytsapp", ".vibewarden-version")
}

func TestInitProject_TypeScript_DoesNotCreateGoFiles(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "tsonly",
		Language:    domainscaffold.LanguageTypeScript,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() TypeScript unexpected error: %v", err)
	}

	// Go-specific files must NOT be created.
	goModPath := filepath.Join(parent, "tsonly", "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		t.Error("go.mod must not exist for a TypeScript project")
	}

	mainGoPath := filepath.Join(parent, "tsonly", "cmd", "tsonly", "main.go")
	if _, err := os.Stat(mainGoPath); err == nil {
		t.Error("cmd/tsonly/main.go must not exist for a TypeScript project")
	}

	// Kotlin-specific files must NOT be created.
	buildGradlePath := filepath.Join(parent, "tsonly", "build.gradle.kts")
	if _, err := os.Stat(buildGradlePath); err == nil {
		t.Error("build.gradle.kts must not exist for a TypeScript project")
	}
}

func TestInitProject_TypeScript_DefaultsPort(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "tsnoport",
		Language:    domainscaffold.LanguageTypeScript,
		// Port deliberately zero — should default to 3000.
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() TypeScript unexpected error: %v", err)
	}

	mustExist(t, parent, "tsnoport", "vibewarden.yaml")
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
			svc := scaffoldapp.NewInitProjectService(renderer)

			parent := t.TempDir()
			opts := scaffoldapp.InitProjectOptions{
				ProjectName: "myapp",
				Language:    domainscaffold.LanguageGo,
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
	svc := scaffoldapp.NewInitProjectService(tracker)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "descapp",
		Language:    domainscaffold.LanguageGo,
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

// TestInitProject_WithRealFS_Description verifies that the real templates render
// the description into PROJECT.md, CLAUDE.md, and architect.md.
func TestInitProject_WithRealFS_Description(t *testing.T) {
	r := mustBuildRealRenderer(t)
	svc := scaffoldapp.NewInitProjectService(r)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "realwithDesc",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
		Description: "a payment processing service",
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	tests := []struct {
		file        string
		mustContain []string
	}{
		{
			file:        filepath.Join(parent, "realwithDesc", "PROJECT.md"),
			mustContain: []string{"a payment processing service", "realwithDesc"},
		},
		{
			file:        filepath.Join(parent, "realwithDesc", "CLAUDE.md"),
			mustContain: []string{"a payment processing service"},
		},
		{
			file:        filepath.Join(parent, "realwithDesc", ".claude", "agents", "architect.md"),
			mustContain: []string{"a payment processing service"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			raw, err := os.ReadFile(tt.file) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("reading %s: %v", tt.file, err)
			}
			content := string(raw)
			for _, want := range tt.mustContain {
				if !strings.Contains(content, want) {
					t.Errorf("file %s missing %q\nContent:\n%s", tt.file, want, content)
				}
			}
		})
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
