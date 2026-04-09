package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestNewContextCmd_HelpWhenNoSubcommand(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"context", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "refresh") {
		t.Errorf("expected help output to mention 'refresh', got: %q", out)
	}
}

func TestContextRefresh_NoExistingFiles_NoForce(t *testing.T) {
	// When no context files exist and --force is not set, the command should
	// report that nothing was found but not error.
	dir := t.TempDir()

	// Write a minimal vibewarden.yaml so config.Load succeeds.
	cfgContent := `
server:
  port: 8080
upstream:
  port: 3000
`
	cfgPath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"context", "refresh", "--config", cfgPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v\nstderr: %s", err, errBuf.String())
	}

	out := outBuf.String()
	if !strings.Contains(out, "No context files found") && !strings.Contains(out, "Skipped") {
		t.Errorf("expected 'no files' or 'skipped' message, got: %q", out)
	}
}

func TestContextRefresh_WithForce_CreatesFiles(t *testing.T) {
	dir := t.TempDir()

	cfgContent := `
server:
  port: 8080
upstream:
  port: 3000
rate_limit:
  enabled: true
tls:
  enabled: false
  provider: self-signed
`
	cfgPath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Change to the temp dir so relative paths used by AgentContextService work.
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"context", "refresh", "--config", cfgPath, "--force"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v\nstderr: %s", err, errBuf.String())
	}

	out := outBuf.String()
	if !strings.Contains(out, "refreshed") {
		t.Errorf("expected 'refreshed' in output, got: %q", out)
	}

	// Verify agent context files were actually created.
	expectedFiles := []string{
		"AGENTS-VIBEWARDEN.md",
		"AGENTS.md",
	}
	for _, f := range expectedFiles {
		absPath := filepath.Join(dir, f)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			t.Errorf("expected file %q to be created, but it does not exist", absPath)
		}
	}

	// CLAUDE.md must NOT be created.
	claudePath := filepath.Join(dir, ".claude", "CLAUDE.md")
	if _, err := os.Stat(claudePath); err == nil {
		t.Errorf(".claude/CLAUDE.md must not be created by context refresh")
	}
}

func TestContextRefresh_ExistingAgentsVibewarden_RefreshedWithoutForce(t *testing.T) {
	dir := t.TempDir()

	cfgContent := `
server:
  port: 8080
upstream:
  port: 3000
`
	cfgPath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Pre-create AGENTS-VIBEWARDEN.md.
	vibewardenFile := filepath.Join(dir, "AGENTS-VIBEWARDEN.md")
	if err := os.WriteFile(vibewardenFile, []byte("old content"), 0600); err != nil {
		t.Fatalf("writing AGENTS-VIBEWARDEN.md: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"context", "refresh", "--config", cfgPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v\nstderr: %s", err, errBuf.String())
	}

	// AGENTS-VIBEWARDEN.md existed, so it should be refreshed (new content != "old content").
	content, err := os.ReadFile(vibewardenFile)
	if err != nil {
		t.Fatalf("reading AGENTS-VIBEWARDEN.md: %v", err)
	}
	if string(content) == "old content" {
		t.Errorf("AGENTS-VIBEWARDEN.md was not refreshed: content still %q", content)
	}
}

func TestContextRefresh_InvalidConfig(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"context", "refresh", "--config", "/nonexistent/vibewarden.yaml"})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error for invalid config path, got nil")
	}
}
