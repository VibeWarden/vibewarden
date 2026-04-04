package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestInitCmd_RequiresLang verifies that omitting --lang returns a helpful error.
func TestInitCmd_RequiresLang(t *testing.T) {
	tests := []struct {
		name        string
		wantInError []string
	}{
		{
			name: "mentions --lang flag",
			wantInError: []string{
				"--lang",
				"go",
				"vibew init --lang go",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := cmd.NewRootCmd("test")
			var errOut bytes.Buffer
			root.SetErr(&errOut)
			root.SetArgs([]string{"init", "myproject"})

			err := root.Execute()
			if err == nil {
				t.Fatal("expected error when --lang is missing, got nil")
			}
			for _, want := range tt.wantInError {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected error to contain %q, got: %v", want, err)
				}
			}
		})
	}
}

// TestInitCmd_RejectsUnknownLang verifies an unsupported language is rejected with a helpful error.
func TestInitCmd_RejectsUnknownLang(t *testing.T) {
	tests := []struct {
		name        string
		lang        string
		wantInError []string
	}{
		{
			name: "unknown language ruby",
			lang: "ruby",
			wantInError: []string{
				"ruby",
				"go",
				"vibew init --lang go",
			},
		},
		{
			name: "unknown language python",
			lang: "python",
			wantInError: []string{
				"python",
				"go",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := cmd.NewRootCmd("test")
			var errOut bytes.Buffer
			root.SetErr(&errOut)
			root.SetArgs([]string{"init", "--lang", tt.lang, "myproject"})

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error for unknown lang %q, got nil", tt.lang)
			}
			for _, want := range tt.wantInError {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected error to contain %q, got: %v", want, err)
				}
			}
		})
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
		filepath.Join("newapp", "AGENTS-VIBEWARDEN.md"),
		filepath.Join("newapp", "AGENTS.md"),

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

// TestInitCmd_AppImageDefaultsToProjectName verifies that the generated
// vibewarden.yaml uses app.image derived from the project name rather than app.build.
func TestInitCmd_AppImageDefaultsToProjectName(t *testing.T) {
	tests := []struct {
		name        string
		lang        string
		projectName string
	}{
		{"go uses image", "go", "mygoapp"},
		{"kotlin uses image", "kotlin", "myktapp"},
		{"typescript uses image", "typescript", "mytsapp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chdir(dir); err != nil {
				t.Fatalf("chdir: %v", err)
			}

			root := cmd.NewRootCmd("test")
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs([]string{"init", "--lang", tt.lang, tt.projectName})

			if err := root.Execute(); err != nil {
				t.Fatalf("init failed: %v", err)
			}

			vwPath := filepath.Join(dir, tt.projectName, "vibewarden.yaml")
			data, err := os.ReadFile(vwPath) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("reading vibewarden.yaml: %v", err)
			}
			content := string(data)

			wantImage := "image: \"" + tt.projectName + ":latest\""
			if !strings.Contains(content, wantImage) {
				t.Errorf("vibewarden.yaml missing %q:\n%s", wantImage, content)
			}
			// "build:" must not appear as an active (uncommented) directive.
			for _, line := range strings.Split(content, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "build:") {
					t.Errorf("vibewarden.yaml must not have an active 'build:' directive by default; found: %q\n\nContent:\n%s", line, content)
				}
			}
		})
	}
}

// TestInitCmd_KotlinCreatesProject verifies that --lang kotlin scaffolds a project.
func TestInitCmd_KotlinCreatesProject(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "kotlin", "ktapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --lang kotlin failed: %v", err)
	}

	expectedFiles := []string{
		filepath.Join("ktapp", "vibewarden.yaml"),
		filepath.Join("ktapp", "build.gradle.kts"),
		filepath.Join("ktapp", "settings.gradle.kts"),
		filepath.Join("ktapp", "Dockerfile"),
		filepath.Join("ktapp", ".gitignore"),
		filepath.Join("ktapp", "CLAUDE.md"),
		filepath.Join("ktapp", "AGENTS-VIBEWARDEN.md"),
		filepath.Join("ktapp", "AGENTS.md"),

		filepath.Join("ktapp", ".vibewarden-version"),
		filepath.Join("ktapp", "src", "main", "kotlin", "ktapp", "ktapp", "Application.kt"),
	}

	for _, rel := range expectedFiles {
		full := filepath.Join(dir, rel)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected file %q to exist: %v", rel, err)
		}
	}
}

// TestInitCmd_KotlinDoesNotCreateGoFiles verifies that --lang kotlin does not
// create Go-specific files.
func TestInitCmd_KotlinDoesNotCreateGoFiles(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "kotlin", "ktonly"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --lang kotlin failed: %v", err)
	}

	goSpecificFiles := []string{
		filepath.Join("ktonly", "go.mod"),
		filepath.Join("ktonly", "cmd", "ktonly", "main.go"),
	}
	for _, rel := range goSpecificFiles {
		full := filepath.Join(dir, rel)
		if _, err := os.Stat(full); err == nil {
			t.Errorf("file %q must not exist for a Kotlin project", rel)
		}
	}
}

// TestInitCmd_KotlinPrintsSuccessMessage verifies the success message for Kotlin.
func TestInitCmd_KotlinPrintsSuccessMessage(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "kotlin", "ktmsg"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --lang kotlin failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "ktmsg") {
		t.Errorf("success message does not mention project name, got:\n%s", output)
	}
	if !strings.Contains(output, "vibew dev") {
		t.Errorf("success message should mention 'vibew dev', got:\n%s", output)
	}
	if !strings.Contains(output, "Application.kt") {
		t.Errorf("success message should mention Application.kt for Kotlin, got:\n%s", output)
	}
}

// TestInitCmd_TypeScriptCreatesProject verifies that --lang typescript scaffolds a project.
func TestInitCmd_TypeScriptCreatesProject(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "typescript", "tsapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --lang typescript failed: %v", err)
	}

	expectedFiles := []string{
		filepath.Join("tsapp", "vibewarden.yaml"),
		filepath.Join("tsapp", "package.json"),
		filepath.Join("tsapp", "tsconfig.json"),
		filepath.Join("tsapp", "Dockerfile"),
		filepath.Join("tsapp", ".gitignore"),
		filepath.Join("tsapp", "CLAUDE.md"),
		filepath.Join("tsapp", "AGENTS-VIBEWARDEN.md"),
		filepath.Join("tsapp", "AGENTS.md"),

		filepath.Join("tsapp", ".vibewarden-version"),
		filepath.Join("tsapp", "src", "index.ts"),
		filepath.Join("tsapp", "src", "domain", ".gitkeep"),
		filepath.Join("tsapp", "src", "ports", ".gitkeep"),
		filepath.Join("tsapp", "src", "adapters", ".gitkeep"),
		filepath.Join("tsapp", "src", "app", ".gitkeep"),
	}

	for _, rel := range expectedFiles {
		full := filepath.Join(dir, rel)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected file %q to exist: %v", rel, err)
		}
	}
}

// TestInitCmd_TypeScriptDoesNotCreateGoFiles verifies that --lang typescript does
// not create Go-specific files.
func TestInitCmd_TypeScriptDoesNotCreateGoFiles(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "typescript", "tsonly"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --lang typescript failed: %v", err)
	}

	goSpecificFiles := []string{
		filepath.Join("tsonly", "go.mod"),
		filepath.Join("tsonly", "cmd", "tsonly", "main.go"),
	}
	for _, rel := range goSpecificFiles {
		full := filepath.Join(dir, rel)
		if _, err := os.Stat(full); err == nil {
			t.Errorf("file %q must not exist for a TypeScript project", rel)
		}
	}
}

// TestInitCmd_TypeScriptPrintsSuccessMessage verifies the success message for TypeScript.
func TestInitCmd_TypeScriptPrintsSuccessMessage(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--lang", "typescript", "tsmsg"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --lang typescript failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "tsmsg") {
		t.Errorf("success message does not mention project name, got:\n%s", output)
	}
	if !strings.Contains(output, "vibew dev") {
		t.Errorf("success message should mention 'vibew dev', got:\n%s", output)
	}
	if !strings.Contains(output, "src/index.ts") {
		t.Errorf("success message should mention src/index.ts for TypeScript, got:\n%s", output)
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
	if !strings.Contains(output, "vibew dev") {
		t.Errorf("success message should mention 'vibew dev', got:\n%s", output)
	}
}
