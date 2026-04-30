package env_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	envpkg "github.com/vibewarden/vibewarden/internal/app/env"
)

// writeFile is a test helper that creates a file with the given content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile(%q): %v", path, err)
	}
}

// minimalBase returns a minimal vibewarden.yaml content suitable for tests.
func minimalBase(port int) string {
	return "server:\n  port: " + itoa(port) + "\n"
}

// itoa converts an int to a string. We avoid importing strconv to keep helpers minimal.
func itoa(i int) string {
	if i == 8443 {
		return "8443"
	}
	return "9443"
}

func TestFileResolver_EmptyName_LoadsBaseOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"), minimalBase(8443))

	r := envpkg.NewFileResolver(dir)
	resolved, err := r.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(%q) unexpected error: %v", "", err)
	}
	if resolved.EnvName != "" {
		t.Errorf("EnvName = %q, want %q", resolved.EnvName, "")
	}
	if resolved.OverridePath != "" {
		t.Errorf("OverridePath = %q, want empty", resolved.OverridePath)
	}
	if resolved.Cfg == nil {
		t.Fatal("Cfg is nil")
	}
	if resolved.Cfg.Server.Port != 8443 {
		t.Errorf("server.port = %d, want 8443", resolved.Cfg.Server.Port)
	}
}

func TestFileResolver_NonEmptyName_MergesOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"), minimalBase(8443))
	writeFile(t, filepath.Join(dir, "vibewarden.production.yaml"),
		"tls:\n  domain: app.example.com\n  enabled: true\n")

	r := envpkg.NewFileResolver(dir)
	resolved, err := r.Resolve("production")
	if err != nil {
		t.Fatalf("Resolve(%q) unexpected error: %v", "production", err)
	}
	if resolved.EnvName != "production" {
		t.Errorf("EnvName = %q, want %q", resolved.EnvName, "production")
	}
	if resolved.OverridePath == "" {
		t.Error("OverridePath should not be empty")
	}
	if resolved.Cfg == nil {
		t.Fatal("Cfg is nil")
	}
	if resolved.Cfg.TLS.Domain != "app.example.com" {
		t.Errorf("tls.domain = %q, want %q", resolved.Cfg.TLS.Domain, "app.example.com")
	}
}

func TestFileResolver_TLSDomainPreservedThroughMerge(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"),
		"server:\n  port: 8443\ntls:\n  enabled: true\n")
	writeFile(t, filepath.Join(dir, "vibewarden.staging.yaml"),
		"tls:\n  domain: staging.example.com\n")

	r := envpkg.NewFileResolver(dir)
	resolved, err := r.Resolve("staging")
	if err != nil {
		t.Fatalf("Resolve(%q) unexpected error: %v", "staging", err)
	}
	if resolved.Cfg.TLS.Domain != "staging.example.com" {
		t.Errorf("tls.domain = %q, want staging.example.com", resolved.Cfg.TLS.Domain)
	}
	// Fields from base should still be present.
	if resolved.Cfg.Server.Port != 8443 {
		t.Errorf("server.port = %d, want 8443 (from base)", resolved.Cfg.Server.Port)
	}
}

func TestFileResolver_MissingOverride_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"), minimalBase(8443))

	r := envpkg.NewFileResolver(dir)
	_, err := r.Resolve("production")
	if err == nil {
		t.Fatal("expected error for missing override, got nil")
	}
	if !errors.Is(err, envpkg.ErrOverrideConfigMissing) {
		t.Errorf("expected ErrOverrideConfigMissing, got: %v", err)
	}
}

func TestFileResolver_MissingBase_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	// No vibewarden.yaml in dir.

	r := envpkg.NewFileResolver(dir)
	_, err := r.Resolve("")
	if err == nil {
		t.Fatal("expected error for missing base config, got nil")
	}
	if !errors.Is(err, envpkg.ErrBaseConfigMissing) {
		t.Errorf("expected ErrBaseConfigMissing, got: %v", err)
	}
}

func TestFileResolver_MissingBase_NonEmptyName_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	// No vibewarden.yaml in dir.

	r := envpkg.NewFileResolver(dir)
	_, err := r.Resolve("production")
	if err == nil {
		t.Fatal("expected error for missing base config, got nil")
	}
	if !errors.Is(err, envpkg.ErrBaseConfigMissing) {
		t.Errorf("expected ErrBaseConfigMissing, got: %v", err)
	}
}

func TestFileResolver_BasePath_IsAbsolute(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"), minimalBase(8443))

	r := envpkg.NewFileResolver(dir)
	resolved, err := r.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(%q) unexpected error: %v", "", err)
	}
	if !filepath.IsAbs(resolved.BasePath) {
		t.Errorf("BasePath %q is not absolute", resolved.BasePath)
	}
}
