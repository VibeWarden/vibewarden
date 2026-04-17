package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsMultiSiteDir_WithSites(t *testing.T) {
	dir := t.TempDir()
	sitesDir := filepath.Join(dir, "sites")
	if err := os.Mkdir(sitesDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	// Create a child directory.
	if err := os.Mkdir(filepath.Join(sitesDir, "blog"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if !isMultiSiteDir(dir) {
		t.Error("isMultiSiteDir() = false, want true when sites/ has a subdirectory")
	}
}

func TestIsMultiSiteDir_EmptySites(t *testing.T) {
	dir := t.TempDir()
	sitesDir := filepath.Join(dir, "sites")
	if err := os.Mkdir(sitesDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if isMultiSiteDir(dir) {
		t.Error("isMultiSiteDir() = true, want false when sites/ is empty")
	}
}

func TestIsMultiSiteDir_NoSitesDir(t *testing.T) {
	dir := t.TempDir()

	if isMultiSiteDir(dir) {
		t.Error("isMultiSiteDir() = true, want false when no sites/ directory exists")
	}
}

func TestIsMultiSiteDir_SitesIsFile(t *testing.T) {
	dir := t.TempDir()
	// Create sites as a file, not a directory.
	if err := os.WriteFile(filepath.Join(dir, "sites"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if isMultiSiteDir(dir) {
		t.Error("isMultiSiteDir() = true, want false when sites is a file")
	}
}

func TestIsMultiSiteDir_SitesWithOnlyFiles(t *testing.T) {
	dir := t.TempDir()
	sitesDir := filepath.Join(dir, "sites")
	if err := os.Mkdir(sitesDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	// Create a file (not a directory) inside sites/.
	if err := os.WriteFile(filepath.Join(sitesDir, "readme.txt"), []byte("info"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if isMultiSiteDir(dir) {
		t.Error("isMultiSiteDir() = true, want false when sites/ contains only files")
	}
}

func TestRunServeMultiSite_MissingGlobalConfig(t *testing.T) {
	// runServeMultiSite should work even when global.yaml is missing
	// (LoadGlobal returns defaults). However, it will fail at Caddy
	// start because there are no sites. We verify that it at least
	// gets past the global config loading step.
	dir := t.TempDir()
	sitesDir := filepath.Join(dir, "sites")
	if err := os.Mkdir(sitesDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	// Create a site so we get past the "no healthy sites" error.
	siteDir := filepath.Join(sitesDir, "testapp")
	if err := os.Mkdir(siteDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	siteConfig := `
server:
  host: 127.0.0.1
  port: 8443
upstream:
  host: 127.0.0.1
  port: 3000
tls:
  domain: testapp.example.com
`
	if err := os.WriteFile(filepath.Join(siteDir, "vibewarden.yaml"), []byte(siteConfig), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// We cannot fully run the proxy (it needs a real Caddy instance), but we
	// can verify that the function proceeds past config loading by checking
	// that it does not return a "loading global config" error.
	// The function will error at Caddy start which is expected in a test.
	// Use a cancelled context to avoid blocking.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runServeMultiSite(ctx, dir, "test")
	// We expect an error from the proxy (context cancelled), not from config loading.
	if err != nil && strings.Contains(err.Error(), "loading global config") {
		t.Errorf("unexpected global config error: %v", err)
	}
}

func TestRunServeMultiSite_DomainConflict(t *testing.T) {
	dir := t.TempDir()
	sitesDir := filepath.Join(dir, "sites")
	if err := os.Mkdir(sitesDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	sameConfig := `
server:
  host: 127.0.0.1
  port: 8443
upstream:
  host: 127.0.0.1
  port: 3000
tls:
  domain: same.example.com
`
	for _, name := range []string{"app1", "app2"} {
		siteDir := filepath.Join(sitesDir, name)
		if err := os.Mkdir(siteDir, 0o755); err != nil {
			t.Fatalf("Mkdir(%s): %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(siteDir, "vibewarden.yaml"), []byte(sameConfig), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runServeMultiSite(ctx, dir, "test")
	if err == nil {
		t.Fatal("expected error for duplicate domain")
	}
	if !strings.Contains(err.Error(), "domain validation failed") {
		t.Errorf("expected domain validation error, got: %v", err)
	}
}

func TestBuildMultiSiteLogger(t *testing.T) {
	tests := []struct {
		name  string
		level string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"error", "error"},
		{"default", ""},
		{"unknown", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := buildMultiSiteLogger(tt.level)
			if logger == nil {
				t.Error("buildMultiSiteLogger returned nil")
			}
		})
	}
}
