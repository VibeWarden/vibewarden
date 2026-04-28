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
		".gitignore",
		"AGENTS-VIBEWARDEN.md",
		"AGENTS.md",
	}

	for _, rel := range expectedFiles {
		full := filepath.Join(projectDir, rel)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected file %q to exist: %v", rel, err)
		}
	}

	// Dockerfile, .dockerignore, CLAUDE.md, .claude/commands/, and
	// .vibewarden-version must NOT be created.
	absentFiles := []string{
		"Dockerfile",
		".dockerignore",
		"CLAUDE.md",
		".claude",
		".vibewarden-version",
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

// TestInitCmd_NameFlagRegistered verifies that the --name flag exists on init.
func TestInitCmd_NameFlagRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	initCmd, _, err := root.Find([]string{"init"})
	if err != nil {
		t.Fatalf("Find(init) error: %v", err)
	}
	if initCmd.Flags().Lookup("name") == nil {
		t.Error("expected --name flag on init command")
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

// TestInitCmd_NameFlag_WritesNameToConfig verifies that --name sets the
// top-level name: field in the generated vibewarden.yaml so that Docker Compose
// uses a project-scoped image name instead of the generic "vibewarden-app".
//
// Artifact test for #955 and #959.
func TestInitCmd_NameFlag_WritesNameToConfig(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "testproj")
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
	root.SetArgs([]string{"init", "--name", "my-cool-project"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --name failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectDir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading vibewarden.yaml: %v", err)
	}

	if !strings.Contains(string(data), `name: "my-cool-project"`) {
		t.Errorf("vibewarden.yaml should contain 'name: \"my-cool-project\"', got:\n%s", data)
	}
}

// TestInitCmd_NonInteractiveFlag_RegisteredAndDescribed verifies that
// `--non-interactive` is a registered flag on `vibew init` and that it
// appears in `--help` output. This is the agent-discoverability guarantee
// that motivated introducing the flag (issue #1065).
func TestInitCmd_NonInteractiveFlag_RegisteredAndDescribed(t *testing.T) {
	root := cmd.NewRootCmd("test")
	initCmd, _, err := root.Find([]string{"init"})
	if err != nil {
		t.Fatalf("Find(init) error: %v", err)
	}
	if initCmd.Flags().Lookup("non-interactive") == nil {
		t.Fatal("expected --non-interactive flag on init command")
	}

	root.SetArgs([]string{"init", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)

	_ = root.Execute()
	helpOutput := out.String()
	if !strings.Contains(helpOutput, "--non-interactive") {
		t.Errorf("--non-interactive flag must appear in help output:\n%s", helpOutput)
	}
}

// TestInitCmd_NonInteractiveFlag_SkipsPromptsWithTTY verifies that
// passing `--non-interactive` bypasses the description prompt even when
// stdin IS a TTY. Regression guard for #1065.
//
// The test sets IsTTY to true and deliberately does NOT wire a stdin
// pipe: if the flag is ignored, the scaffolder blocks forever reading
// from an empty os.Stdin (or a real terminal). Success is measured by
// the init command completing without blocking and PROJECT.md not being
// created (empty description).
func TestInitCmd_NonInteractiveFlag_SkipsPromptsWithTTY(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "noninteractiveapp")
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

	// Override IsTTY to claim interactive — the flag must still win.
	origIsTTY := cmd.IsTTY
	cmd.IsTTY = func(*os.File) bool { return true }
	t.Cleanup(func() { cmd.IsTTY = origIsTTY })

	root := cmd.NewRootCmd("test")
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--non-interactive"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --non-interactive failed: %v", err)
	}

	// vibewarden.yaml should exist — the scaffold ran to completion
	// without waiting for input.
	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml after non-interactive init: %v", err)
	}
	// PROJECT.md must NOT exist — no description was provided and the
	// prompt was skipped.
	if _, err := os.Stat(filepath.Join(projectDir, "PROJECT.md")); err == nil {
		t.Error("PROJECT.md must not exist when --non-interactive skips the describe prompt")
	}
}

// TestInitCmd_NonInteractiveFlag_NoTTYStillSkips is a regression guard for
// the pre-existing no-TTY auto-detection path: even with the flag absent,
// when IsTTY reports false, no prompt should fire.
func TestInitCmd_NonInteractiveFlag_NoTTYStillSkips(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "nottyapp")
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

	// IsTTY already returns false via TestMain; assert init completes.
	root := cmd.NewRootCmd("test")
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init (no TTY, no flag) failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml after non-TTY init: %v", err)
	}
}

// TestInitCmd_NonInteractiveFlag_NoTTYWithFlag verifies the combination is
// a no-op — stdin is already non-TTY, so the flag changes nothing.
func TestInitCmd_NonInteractiveFlag_NoTTYWithFlag(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "nottyflagapp")
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
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--non-interactive"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --non-interactive (no TTY) failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml: %v", err)
	}
}

// TestInitCmd_PositionalArgsError verifies that passing any positional argument
// to vibew init returns the prescribed actionable error message that explains
// the cwd-derivation convention, instead of cobra's generic unknown-command
// message.
func TestInitCmd_PositionalArgsError(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"single positional", []string{"init", "my-project"}},
		{"multiple positionals", []string{"init", "a", "b"}},
		{"positional after flag", []string{"init", "--name", "foo", "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := cmd.NewRootCmd("test")
			var errOut bytes.Buffer
			root.SetErr(&errOut)
			root.SetArgs(tt.args)

			err := root.Execute()
			if err == nil {
				t.Fatal("expected non-nil error, got nil")
			}

			stderr := errOut.String()
			if !strings.Contains(stderr, "takes no arguments") {
				t.Errorf("stderr missing 'takes no arguments'; got:\n%s", stderr)
			}
			if !strings.Contains(stderr, "name is derived from dirname") {
				t.Errorf("stderr missing 'name is derived from dirname'; got:\n%s", stderr)
			}
		})
	}
}

// TestInitCmd_NoNameFlag_DefaultsToDirname verifies that when --name is not set,
// vibewarden.yaml contains name: <dirname> (the directory basename). This ensures
// ComposeProjectName() always resolves to a predictable value in both dev and
// bundle environments. Guard for #1199.
func TestInitCmd_NoNameFlag_DefaultsToDirname(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "testproj2")
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
		t.Fatalf("init without --name failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectDir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading vibewarden.yaml: %v", err)
	}

	// vibewarden.yaml must contain name: "testproj2" (dirname) even without --name.
	if !strings.Contains(string(data), `name: "testproj2"`) {
		t.Errorf("vibewarden.yaml should contain 'name: \"testproj2\"' (dirname default), got:\n%s", data)
	}
}
