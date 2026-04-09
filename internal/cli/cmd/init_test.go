package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestInitCmd_CreatesProjectDir verifies that the named project directory is created.
func TestInitCmd_CreatesProjectDir(t *testing.T) {
	dir := t.TempDir()

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "testproject"})

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

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

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "newapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	expectedFiles := []string{
		filepath.Join("newapp", "vibewarden.yaml"),
		filepath.Join("newapp", "Dockerfile"),
		filepath.Join("newapp", ".gitignore"),
		filepath.Join("newapp", "AGENTS-VIBEWARDEN.md"),
		filepath.Join("newapp", "AGENTS.md"),
		filepath.Join("newapp", ".vibewarden-version"),
	}

	for _, rel := range expectedFiles {
		full := filepath.Join(dir, rel)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected file %q to exist: %v", rel, err)
		}
	}

	// CLAUDE.md and .claude/commands/ must NOT be created.
	absentFiles := []string{
		filepath.Join("newapp", "CLAUDE.md"),
		filepath.Join("newapp", ".claude"),
	}
	for _, rel := range absentFiles {
		full := filepath.Join(dir, rel)
		if _, err := os.Stat(full); err == nil {
			t.Errorf("file/directory %q must not exist", rel)
		}
	}
}

// TestInitCmd_ErrorsOnNonEmptyDir verifies an error is returned when the target
// directory already exists and contains files.
func TestInitCmd_ErrorsOnNonEmptyDir(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

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
	root.SetArgs([]string{"init", "occupied"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error for non-empty directory, got nil")
	}
}

// TestInitCmd_ForceOverwrites verifies --force allows overwriting existing dirs.
func TestInitCmd_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

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
	root.SetArgs([]string{"init", "--force", "myapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --force failed: %v", err)
	}

	// Core files must exist after force.
	if _, err := os.Stat(filepath.Join(dir, "myapp", "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml to exist after --force: %v", err)
	}
}

// TestInitCmd_CustomPort verifies --port is reflected in generated files.
func TestInitCmd_CustomPort(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--port", "8080", "portapp"})

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

// TestInitCmd_AppBuildDefaultsToCurrentDir verifies that the generated
// vibewarden.yaml uses app.build = "." by default rather than app.image.
func TestInitCmd_AppBuildDefaultsToCurrentDir(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "myapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	vwPath := filepath.Join(dir, "myapp", "vibewarden.yaml")
	data, err := os.ReadFile(vwPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("reading vibewarden.yaml: %v", err)
	}
	content := string(data)

	// app.build must be the active directive.
	if !strings.Contains(content, `build: "."`) {
		t.Errorf("vibewarden.yaml missing active 'build: \".\"':\n%s", content)
	}
	// app.image must not appear as an active (uncommented) directive.
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "image:") {
			t.Errorf("vibewarden.yaml must not have an active 'image:' directive by default; found: %q\n\nContent:\n%s", line, content)
		}
	}
}

// TestInitCmd_DotScaffoldsInCurrentDir verifies that "." as project name scaffolds
// files into the current working directory using the directory's base name.
func TestInitCmd_DotScaffoldsInCurrentDir(t *testing.T) {
	tests := []struct {
		name    string
		args    []string // arguments after "init"
		dirName string   // name of the temp subdirectory to cd into
	}{
		{
			name:    "positional dot",
			args:    []string{"init", "."},
			dirName: "myapp",
		},
		{
			name:    "flag dot",
			args:    []string{"init", "--name", "."},
			dirName: "flagdot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a named subdirectory so we have a meaningful base name.
			parent := t.TempDir()
			projectDir := filepath.Join(parent, tt.dirName)
			if err := os.MkdirAll(projectDir, 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			origDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			if err := os.Chdir(projectDir); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })

			root := cmd.NewRootCmd("test")
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs(tt.args)

			if err := root.Execute(); err != nil {
				t.Fatalf("init command failed: %v", err)
			}

			// Files must be written directly into the project dir, not a subdir.
			expectedFiles := []string{
				"vibewarden.yaml",
				"Dockerfile",
				".gitignore",
				"AGENTS-VIBEWARDEN.md",
				"AGENTS.md",
				".vibewarden-version",
			}
			for _, rel := range expectedFiles {
				full := filepath.Join(projectDir, rel)
				if _, err := os.Stat(full); err != nil {
					t.Errorf("expected file %q to exist in current dir: %v", rel, err)
				}
			}

			// No subdirectory named after the dir should have been created.
			unwanted := filepath.Join(parent, tt.dirName, tt.dirName)
			if _, err := os.Stat(unwanted); err == nil {
				t.Errorf("unexpected subdirectory %q was created; files should be in cwd", unwanted)
			}
		})
	}
}

// TestInitCmd_DotUsesBaseName verifies that when "." is supplied the project name
// in the success message is the current directory's base name, not ".".
func TestInitCmd_DotUsesBaseName(t *testing.T) {
	parent := t.TempDir()
	projectDir := filepath.Join(parent, "basenamedir")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "."})

	if err := root.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "basenamedir") {
		t.Errorf("success message should contain directory base name %q, got:\n%s", "basenamedir", output)
	}
	// The success message must NOT show "cd basenamedir" because the user is already there.
	if strings.Contains(output, "cd basenamedir") {
		t.Errorf("success message must not show 'cd basenamedir' when scaffolding in current dir, got:\n%s", output)
	}
}

// TestInitCmd_DotErrorsOnNonEmptyDirWithoutForce verifies that scaffolding with "."
// into a non-empty directory fails unless --force is passed.
func TestInitCmd_DotErrorsOnNonEmptyDirWithoutForce(t *testing.T) {
	parent := t.TempDir()
	projectDir := filepath.Join(parent, "occupied")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Pre-populate with an existing file.
	if err := os.WriteFile(filepath.Join(projectDir, "existing.txt"), []byte("data"), 0o600); err != nil {
		t.Fatalf("writefile: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", "."})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error when scaffolding with '.' into non-empty directory, got nil")
	}
}

// TestInitCmd_DotForceOverwritesCurrentDir verifies that --force allows scaffolding
// into an existing non-empty current directory when "." is used.
func TestInitCmd_DotForceOverwritesCurrentDir(t *testing.T) {
	parent := t.TempDir()
	projectDir := filepath.Join(parent, "forceapp")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("writefile: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "--force", "."})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --force . failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml to exist after --force .: %v", err)
	}
}

// TestInitCmd_DotWorks verifies vibew init . works end-to-end.
func TestInitCmd_DotWorks(t *testing.T) {
	parent := t.TempDir()
	projectDir := filepath.Join(parent, "dotproject")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "."})

	if err := root.Execute(); err != nil {
		t.Fatalf("init . failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("vibewarden.yaml should exist after init .: %v", err)
	}
}

// TestInitCmd_PrintsSuccessMessage verifies a success message is printed.
func TestInitCmd_PrintsSuccessMessage(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "successapp"})

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

// TestInitCmd_NoLangFlag verifies that the --lang flag no longer exists.
func TestInitCmd_NoLangFlag(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"init", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)

	// Execute will succeed (help output), we just check flags.
	_ = root.Execute()
	helpOutput := out.String()
	if strings.Contains(helpOutput, "--lang") {
		t.Errorf("--lang flag must not exist on init command, but appears in help:\n%s", helpOutput)
	}
}

// TestInitCmd_NoModuleFlag verifies that the --module flag no longer exists.
func TestInitCmd_NoModuleFlag(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"init", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)

	_ = root.Execute()
	helpOutput := out.String()
	if strings.Contains(helpOutput, "--module") {
		t.Errorf("--module flag must not exist on init command, but appears in help:\n%s", helpOutput)
	}
}

// TestInitCmd_NoGroupFlag verifies that the --group flag no longer exists.
func TestInitCmd_NoGroupFlag(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"init", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)

	_ = root.Execute()
	helpOutput := out.String()
	if strings.Contains(helpOutput, "--group") {
		t.Errorf("--group flag must not exist on init command, but appears in help:\n%s", helpOutput)
	}
}
