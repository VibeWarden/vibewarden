package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestInitCmd_ScaffoldsInCurrentDir verifies that vibew init scaffolds files
// directly in the current working directory.
func TestInitCmd_ScaffoldsInCurrentDir(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "testproject")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
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
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml in cwd %q: %v", projectDir, err)
	}
}

// TestInitCmd_GeneratesAllFiles verifies all expected files are created in cwd.
func TestInitCmd_GeneratesAllFiles(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "newapp")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
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
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

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
			t.Errorf("expected file %q to exist: %v", rel, err)
		}
	}

	// CLAUDE.md and .claude/commands/ must NOT be created.
	absentFiles := []string{
		"CLAUDE.md",
		".claude",
	}
	for _, rel := range absentFiles {
		full := filepath.Join(projectDir, rel)
		if _, err := os.Stat(full); err == nil {
			t.Errorf("file/directory %q must not exist", rel)
		}
	}
}

// TestInitCmd_ErrorsOnNonEmptyDir verifies an error is returned when the
// current directory already contains files (without --force).
func TestInitCmd_ErrorsOnNonEmptyDir(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "occupied")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
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
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error for non-empty directory, got nil")
	}
}

// TestInitCmd_ForceOverwrites verifies --force allows overwriting existing files in cwd.
func TestInitCmd_ForceOverwrites(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "myapp")
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
	root.SetArgs([]string{"init", "--force"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --force failed: %v", err)
	}

	// Core files must exist after force.
	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml to exist after --force: %v", err)
	}
}

// TestInitCmd_CustomPort verifies --port is reflected in generated files.
func TestInitCmd_CustomPort(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "portapp")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
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
	root.SetArgs([]string{"init", "--port", "8080"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	vwPath := filepath.Join(projectDir, "vibewarden.yaml")
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
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "myapp")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
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
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	vwPath := filepath.Join(projectDir, "vibewarden.yaml")
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

// TestInitCmd_CwdWorks verifies vibew init works end-to-end in cwd.
func TestInitCmd_CwdWorks(t *testing.T) {
	parent := scaffoldTestDir(t, false)
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
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("vibewarden.yaml should exist after init: %v", err)
	}
}

// TestInitCmd_UsesBaseNameFromCwd verifies that the project name in the
// success message is the current directory's base name.
func TestInitCmd_UsesBaseNameFromCwd(t *testing.T) {
	parent := scaffoldTestDir(t, false)
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
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "basenamedir") {
		t.Errorf("success message should contain directory base name %q, got:\n%s", "basenamedir", output)
	}
}

// TestInitCmd_ErrorsOnNonEmptyCwdWithoutForce verifies that scaffolding into
// a non-empty cwd fails unless --force is passed.
func TestInitCmd_ErrorsOnNonEmptyCwdWithoutForce(t *testing.T) {
	parent := scaffoldTestDir(t, false)
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
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error when scaffolding into non-empty directory, got nil")
	}
}

// TestInitCmd_ForceOverwritesCurrentDir verifies that --force allows
// scaffolding into an existing non-empty current directory.
func TestInitCmd_ForceOverwritesCurrentDir(t *testing.T) {
	parent := scaffoldTestDir(t, false)
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
	root.SetArgs([]string{"init", "--force"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --force failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml to exist after --force: %v", err)
	}
}

// TestInitCmd_PrintsSuccessMessage verifies a success message is printed.
func TestInitCmd_PrintsSuccessMessage(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "successapp")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
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
	root.SetArgs([]string{"init"})

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

// TestInitCmd_NoNameFlag verifies that the --name flag no longer exists.
func TestInitCmd_NoNameFlag(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"init", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)

	_ = root.Execute()
	helpOutput := out.String()
	if strings.Contains(helpOutput, "--name") {
		t.Errorf("--name flag must not exist on init command, but appears in help:\n%s", helpOutput)
	}
}

// TestInitCmd_RejectsPositionalArgs verifies that passing a positional arg fails.
func TestInitCmd_RejectsPositionalArgs(t *testing.T) {
	_ = scaffoldTestDir(t, true)

	root := cmd.NewRootCmd("test")
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", "someproject"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error when passing positional arg, got nil")
	}
}

// TestInitCmd_HelpOutput verifies that help text reflects the cwd-only behavior.
func TestInitCmd_HelpOutput(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"init", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)

	_ = root.Execute()
	helpOutput := out.String()

	// Must not mention [project-name] since positional args are removed.
	if strings.Contains(helpOutput, "[project-name]") {
		t.Errorf("help should not mention [project-name]:\n%s", helpOutput)
	}
	// Must mention current directory.
	if !strings.Contains(helpOutput, "current") {
		t.Errorf("help should mention 'current' (directory):\n%s", helpOutput)
	}
}
