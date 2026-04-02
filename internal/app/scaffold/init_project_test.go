package scaffold_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	mustExist(t, parent, "myapp", "vibew")
	mustExist(t, parent, "myapp", "vibew.ps1")
	mustExist(t, parent, "myapp", "vibew.cmd")
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

func TestInitProject_SetsExecutableBit(t *testing.T) {
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "exectest",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	vibewPath := filepath.Join(parent, "exectest", "vibew")
	info, err := os.Stat(vibewPath)
	if err != nil {
		t.Fatalf("stat vibew: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("vibew is not executable: mode=%s", info.Mode())
	}
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
		Language:    domainscaffold.Language("typescript"),
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

// mustExist is a test helper that fails if the file at path does not exist.
func mustExist(t *testing.T, parts ...string) {
	t.Helper()
	path := filepath.Join(parts...)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file %q to exist: %v", path, err)
	}
}
