package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// writeScaffoldingMarker creates an AGENTS-VIBEWARDEN.md file in dir.
func writeScaffoldingMarker(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "AGENTS-VIBEWARDEN.md")
	if err := os.WriteFile(path, []byte("# VibeWarden Agent Context\n"), 0o644); err != nil {
		t.Fatalf("writing scaffolding marker: %v", err)
	}
}

// writeVibewardenYAML creates a minimal vibewarden.yaml in dir.
func writeVibewardenYAML(t *testing.T, dir string) {
	t.Helper()
	content := "server:\n  port: 8080\nupstream:\n  port: 3000\n"
	path := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}
}

// TestRequireScaffolding_MissingMarker verifies that RequireScaffolding returns
// an error when AGENTS-VIBEWARDEN.md is absent from the given directory.
func TestRequireScaffolding_MissingMarker(t *testing.T) {
	dir := t.TempDir()

	err := cmd.RequireScaffolding(dir)
	if err == nil {
		t.Fatal("expected error when AGENTS-VIBEWARDEN.md is missing, got nil")
	}

	for _, want := range []string{"vibew init", "vibew wrap", "vibewarden not initialized"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q; got: %v", want, err)
		}
	}
}

// TestRequireScaffolding_PresentMarker verifies that RequireScaffolding returns
// nil when AGENTS-VIBEWARDEN.md exists in the given directory.
func TestRequireScaffolding_PresentMarker(t *testing.T) {
	dir := t.TempDir()
	writeScaffoldingMarker(t, dir)

	if err := cmd.RequireScaffolding(dir); err != nil {
		t.Errorf("expected no error when marker is present, got: %v", err)
	}
}

// TestRequireScaffolding_ErrorMentionsInitAndWrap verifies the error message
// mentions both vibew init and vibew wrap.
func TestRequireScaffolding_ErrorMentionsInitAndWrap(t *testing.T) {
	tests := []struct {
		name    string
		wantMsg string
	}{
		{"mentions vibew init", "vibew init"},
		{"mentions vibew wrap", "vibew wrap"},
		{"mentions vibewarden not initialized", "vibewarden not initialized"},
	}

	dir := t.TempDir() // no marker written

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cmd.RequireScaffolding(dir)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error missing %q; got: %v", tt.wantMsg, err)
			}
		})
	}
}

// TestRequireScaffolding_YamlWithoutMarkerFails verifies that having
// vibewarden.yaml but no AGENTS-VIBEWARDEN.md is still rejected.
//
// This is the key scenario: an agent that manually created vibewarden.yaml
// without running vibew init or vibew wrap must be caught.
func TestRequireScaffolding_YamlWithoutMarkerFails(t *testing.T) {
	dir := t.TempDir()
	writeVibewardenYAML(t, dir) // vibewarden.yaml present, but NOT the marker

	err := cmd.RequireScaffolding(dir)
	if err == nil {
		t.Fatal("expected error when only vibewarden.yaml exists (no AGENTS-VIBEWARDEN.md)")
	}

	for _, want := range []string{"vibew init", "vibew wrap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q; got: %v", want, err)
		}
	}
}

// TestRequireScaffolding_BothFilesPresent verifies that having both
// vibewarden.yaml and AGENTS-VIBEWARDEN.md passes the scaffolding check.
func TestRequireScaffolding_BothFilesPresent(t *testing.T) {
	dir := t.TempDir()
	writeVibewardenYAML(t, dir)
	writeScaffoldingMarker(t, dir)

	if err := cmd.RequireScaffolding(dir); err != nil {
		t.Errorf("expected no error when both files exist, got: %v", err)
	}
}
