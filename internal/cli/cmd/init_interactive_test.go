package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestInitCmd_DescribeFlag verifies that --describe writes PROJECT.md and
// mentions the description in the success message.
func TestInitCmd_DescribeFlag(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{
		"init", "--lang", "go",
		"--describe", "a task management API",
		"describeapp",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --describe failed: %v", err)
	}

	// PROJECT.md must exist.
	projectMDPath := filepath.Join(dir, "describeapp", "PROJECT.md")
	data, err := os.ReadFile(projectMDPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("PROJECT.md not created: %v", err)
	}
	if !strings.Contains(string(data), "a task management API") {
		t.Errorf("PROJECT.md does not contain description:\n%s", string(data))
	}
	if !strings.Contains(string(data), "describeapp") {
		t.Errorf("PROJECT.md does not contain project name:\n%s", string(data))
	}

	// Success message must mention the description.
	if !strings.Contains(out.String(), "a task management API") {
		t.Errorf("success message does not mention description:\n%s", out.String())
	}
	// Success message must mention PROJECT.md.
	if !strings.Contains(out.String(), "PROJECT.md") {
		t.Errorf("success message does not mention PROJECT.md:\n%s", out.String())
	}
}

// TestInitCmd_DescribeInjectsCLAUDEmd verifies that --describe injects the
// description into CLAUDE.md.
func TestInitCmd_DescribeInjectsCLAUDEmd(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{
		"init", "--lang", "go",
		"--describe", "an inventory management system",
		"invapp",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	claudePath := filepath.Join(dir, "invapp", "CLAUDE.md")
	data, err := os.ReadFile(claudePath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("CLAUDE.md not found: %v", err)
	}
	if !strings.Contains(string(data), "an inventory management system") {
		t.Errorf("CLAUDE.md does not contain description:\n%s", string(data))
	}
}

// TestInitCmd_DescribeInjectsArchitectMD verifies that --describe injects the
// description into .claude/agents/architect.md.
func TestInitCmd_DescribeInjectsArchitectMD(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{
		"init", "--lang", "go",
		"--describe", "a real-time chat service",
		"chatapp",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	archPath := filepath.Join(dir, "chatapp", ".claude", "agents", "architect.md")
	data, err := os.ReadFile(archPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("architect.md not found: %v", err)
	}
	if !strings.Contains(string(data), "a real-time chat service") {
		t.Errorf("architect.md does not contain description:\n%s", string(data))
	}
}

// TestInitCmd_NoDescribeNoProjectMD verifies that when --describe is omitted,
// PROJECT.md is not written.
func TestInitCmd_NoDescribeNoProjectMD(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--lang", "go", "nodescapp"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	projectMDPath := filepath.Join(dir, "nodescapp", "PROJECT.md")
	if _, err := os.Stat(projectMDPath); err == nil {
		t.Error("PROJECT.md must not exist when --describe is not given")
	}
}

// TestInitCmd_NameFlag verifies that --name sets the project name.
func TestInitCmd_NameFlag(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--lang", "go", "--name", "namedproject"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --name failed: %v", err)
	}

	projectDir := filepath.Join(dir, "namedproject")
	if _, err := os.Stat(projectDir); err != nil {
		t.Errorf("expected project directory %q to exist: %v", projectDir, err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("vibewarden.yaml not found in --name project: %v", err)
	}
}

// TestInitCmd_NameFlagOverriddenByPositionalArg verifies that a positional
// argument takes priority over --name when both are given.
func TestInitCmd_NameFlagOverriddenByPositionalArg(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	root := cmd.NewRootCmd("test")
	root.SetOut(&bytes.Buffer{})
	// Positional arg "positional" should win over --name "fromflag".
	root.SetArgs([]string{"init", "--lang", "go", "--name", "fromflag", "positional"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// "positional" directory must exist.
	if _, err := os.Stat(filepath.Join(dir, "positional")); err != nil {
		t.Errorf("expected positional/ to exist: %v", err)
	}
	// "fromflag" directory must NOT exist.
	if _, err := os.Stat(filepath.Join(dir, "fromflag")); err == nil {
		t.Error("fromflag/ must not exist when positional arg is given")
	}
}

// TestInitCmd_Interactive simulates an interactive session by temporarily
// overriding cmd.IsTTY to return true and feeding stdin via a pipe.
func TestInitCmd_Interactive(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Create a pipe to simulate user input.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	// Write: language, project name, description (each terminated by newline).
	input := "go\ninteractiveapp\na cool interactive project\n"
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("writing to pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing write end: %v", err)
	}

	// Redirect stdin to the pipe for the duration of this test.
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
	})

	// Override IsTTY to report interactive.
	origIsTTY := cmd.IsTTY
	cmd.IsTTY = func(*os.File) bool { return true }
	t.Cleanup(func() { cmd.IsTTY = origIsTTY })

	root := cmd.NewRootCmd("test")
	root.SetOut(&bytes.Buffer{})
	// No --lang or positional arg — should be filled by interactive prompts.
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("interactive init failed: %v", err)
	}

	// Project directory must exist.
	projectDir := filepath.Join(dir, "interactiveapp")
	if _, err := os.Stat(projectDir); err != nil {
		t.Errorf("expected project dir %q: %v", projectDir, err)
	}

	// PROJECT.md must contain the description.
	data, err := os.ReadFile(filepath.Join(projectDir, "PROJECT.md")) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("PROJECT.md not found: %v", err)
	}
	if !strings.Contains(string(data), "a cool interactive project") {
		t.Errorf("PROJECT.md missing description:\n%s", string(data))
	}
}

// TestInitCmd_InteractiveSkipsDescribeWhenEmpty verifies that when the user
// provides no description (empty line), PROJECT.md is not written.
func TestInitCmd_InteractiveSkipsDescribeWhenEmpty(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	// Language and name given, empty description.
	if _, err := w.WriteString("go\nnodescapp2\n\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing write end: %v", err)
	}

	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
	})

	origIsTTY := cmd.IsTTY
	cmd.IsTTY = func(*os.File) bool { return true }
	t.Cleanup(func() { cmd.IsTTY = origIsTTY })

	root := cmd.NewRootCmd("test")
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("interactive init failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "nodescapp2", "PROJECT.md")); err == nil {
		t.Error("PROJECT.md must not be written when description is empty")
	}
}

// TestInitCmd_NonInteractiveRequiresLang verifies that in non-interactive mode
// (IsTTY=false, no --lang) an error is returned.
func TestInitCmd_NonInteractiveRequiresLang(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", "myproject"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --lang missing in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "--lang") {
		t.Errorf("error should mention --lang, got: %v", err)
	}
	if !strings.Contains(err.Error(), "non-interactive") {
		t.Errorf("error should mention non-interactive, got: %v", err)
	}
}
