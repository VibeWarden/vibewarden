package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestInitCmd_RequiresLang verifies that omitting --lang returns an error.
func TestInitCmd_RequiresLang(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", "myproject"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --lang is missing, got nil")
	}
	if !strings.Contains(err.Error(), "--lang") {
		t.Errorf("expected error to mention --lang, got: %v", err)
	}
}

// TestInitCmd_RejectsUnknownLang verifies an unsupported language is rejected.
func TestInitCmd_RejectsUnknownLang(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", "--lang", "typescript", "myproject"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unknown lang, got nil")
	}
	if !strings.Contains(err.Error(), "typescript") {
		t.Errorf("expected error to mention 'typescript', got: %v", err)
	}
}

// TestInitCmd_CreatesProjectDir verifies that the named project directory is created.
func TestInitCmd_CreatesProjectDir(t *testing.T) {
	dir := t.TempDir()

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "go", "testproject"})

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := root.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	projectDir := filepath.Join(dir, "testproject")
	if _, err := os.Stat(projectDir); err != nil {
		t.Errorf("expected project directory %q to exist: %v", projectDir, err)
	}
}

// TestInitCmd_GeneratesAllFiles verifies all expected files are created.
func TestInitCmd_GeneratesAllFiles(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "go", "newapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	expectedFiles := []string{
		filepath.Join("newapp", "vibewarden.yaml"),
		filepath.Join("newapp", "go.mod"),
		filepath.Join("newapp", "Dockerfile"),
		filepath.Join("newapp", ".gitignore"),
		filepath.Join("newapp", "CLAUDE.md"),
		filepath.Join("newapp", "cmd", "newapp", "main.go"),
		filepath.Join("newapp", ".claude", "agents", "architect.md"),
		filepath.Join("newapp", ".claude", "agents", "dev.md"),
		filepath.Join("newapp", ".claude", "agents", "reviewer.md"),
		filepath.Join("newapp", "vibew"),
		filepath.Join("newapp", "vibew.ps1"),
		filepath.Join("newapp", "vibew.cmd"),
		filepath.Join("newapp", ".vibewarden-version"),
		filepath.Join("newapp", "internal", "domain", ".gitkeep"),
		filepath.Join("newapp", "internal", "ports", ".gitkeep"),
		filepath.Join("newapp", "internal", "adapters", ".gitkeep"),
		filepath.Join("newapp", "internal", "app", ".gitkeep"),
	}

	for _, rel := range expectedFiles {
		full := filepath.Join(dir, rel)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected file %q to exist: %v", rel, err)
		}
	}
}

// TestInitCmd_ErrorsOnNonEmptyDir verifies an error is returned when the target
// directory already exists and contains files.
func TestInitCmd_ErrorsOnNonEmptyDir(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Pre-populate the directory.
	projectDir := filepath.Join(dir, "occupied")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "existing.txt"), []byte("data"), 0o600); err != nil {
		t.Fatalf("writefile: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", "--lang", "go", "occupied"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for non-empty directory, got nil")
	}
}

// TestInitCmd_ForceOverwrites verifies --force allows overwriting existing dirs.
func TestInitCmd_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Pre-populate the directory.
	projectDir := filepath.Join(dir, "myapp")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("writefile: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "go", "--force", "myapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --force failed: %v", err)
	}

	// Core files must exist after force.
	if _, err := os.Stat(filepath.Join(dir, "myapp", "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml to exist after --force: %v", err)
	}
}

// TestInitCmd_CustomModulePath verifies --module sets the Go module path.
func TestInitCmd_CustomModulePath(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{
		"init", "--lang", "go",
		"--module", "github.com/org/myproject",
		"myproject",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	goModPath := filepath.Join(dir, "myproject", "go.mod")
	data, err := os.ReadFile(goModPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	if !strings.Contains(string(data), "github.com/org/myproject") {
		t.Errorf("go.mod does not contain expected module path:\n%s", string(data))
	}
}

// TestInitCmd_CustomPort verifies --port is reflected in generated files.
func TestInitCmd_CustomPort(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "go", "--port", "8080", "portapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	vwPath := filepath.Join(dir, "portapp", "vibewarden.yaml")
	data, err := os.ReadFile(vwPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("reading vibewarden.yaml: %v", err)
	}
	if !strings.Contains(string(data), "8080") {
		t.Errorf("vibewarden.yaml does not contain port 8080:\n%s", string(data))
	}
}

// TestInitCmd_PrintsSuccessMessage verifies a success message is printed.
func TestInitCmd_PrintsSuccessMessage(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "go", "successapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "successapp") {
		t.Errorf("success message does not mention project name, got:\n%s", output)
	}
	if !strings.Contains(output, "./vibew dev") {
		t.Errorf("success message should mention './vibew dev', got:\n%s", output)
	}
}
