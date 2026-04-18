package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalYAML is a minimal vibewarden.yaml for testing add commands.
const minimalYAML = `server:
  host: "127.0.0.1"
  port: 8080
upstream:
  host: "127.0.0.1"
  port: 3000
log:
  level: "info"
  format: "json"
security_headers:
  enabled: true
tls:
  enabled: false
`

func writeTestConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}
}

func TestAddWAFCmd_DefaultMode(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, minimalYAML)

	cmd := newAddWAFCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"waf"`) {
		t.Errorf("output should mention waf feature, got:\n%s", output)
	}

	content, err := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading updated config: %v", err)
	}
	yaml := string(content)

	wantStrings := []string{"waf:", "enabled: true", "mode: detect", "sqli: true", "xss: true"}
	for _, want := range wantStrings {
		if !strings.Contains(yaml, want) {
			t.Errorf("config should contain %q, got:\n%s", want, yaml)
		}
	}
}

func TestAddWAFCmd_BlockMode(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, minimalYAML)

	cmd := newAddWAFCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--mode", "block", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading updated config: %v", err)
	}
	yaml := string(content)

	if !strings.Contains(yaml, "mode: block") {
		t.Errorf("config should contain mode: block, got:\n%s", yaml)
	}
}

func TestAddWAFCmd_InvalidMode(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, minimalYAML)

	cmd := newAddWAFCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--mode", "invalid", dir})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid mode, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error should mention invalid mode, got: %v", err)
	}
}

func TestAddWAFCmd_AlreadyEnabled(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, minimalYAML+"\nwaf:\n  enabled: true\n  mode: detect\n")

	cmd := newAddWAFCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("already-enabled should not return error (handled gracefully), got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "already enabled") {
		t.Errorf("output should mention already enabled, got:\n%s", output)
	}
}

func TestAddWAFCmd_NoConfig(t *testing.T) {
	dir := t.TempDir()
	// Do not write any vibewarden.yaml.

	cmd := newAddWAFCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{dir})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when config is missing, got nil")
	}
	if !strings.Contains(err.Error(), "vibew wrap") {
		t.Errorf("error should suggest running 'vibew wrap', got: %v", err)
	}
}

func TestAddWAFCmd_ModeValidation(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{"detect mode is valid", "detect", false},
		{"block mode is valid", "block", false},
		{"empty mode is invalid", "", true},
		{"unknown mode is invalid", "passthrough", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestConfig(t, dir, minimalYAML)

			cmd := newAddWAFCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs([]string{"--mode", tt.mode, dir})

			err := cmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("mode=%q: error = %v, wantErr %v", tt.mode, err, tt.wantErr)
			}
		})
	}
}
