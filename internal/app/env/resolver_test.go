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

// TestValidateEnvName_RejectsTraversalNames covers the full set of malicious
// and malformed env name inputs that must be rejected before any filesystem
// access occurs. Each bad name must return ErrInvalidEnvName.
func TestValidateEnvName_RejectsTraversalNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"), minimalBase(8443))
	r := envpkg.NewFileResolver(dir)

	tests := []struct {
		name  string
		input string
	}{
		{"dot-dot", ".."},
		{"dot-dot-slash", "../etc/passwd"},
		{"deep traversal", "../../foo"},
		{"slash in name", "foo/bar"},
		{"backslash in name", `foo\bar`},
		{"leading dot", ".hidden"},
		{"nul byte", "foo\x00bar"},
		{"space", "foo bar"},
		{"at sign", "foo@bar"},
		{"colon", "foo:bar"},
		{"single dot", "."},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.Resolve(tt.input)
			if err == nil {
				t.Errorf("Resolve(%q) expected error, got nil -- path traversal not blocked", tt.input)
				return
			}
			if !errors.Is(err, envpkg.ErrInvalidEnvName) {
				t.Errorf("Resolve(%q) expected ErrInvalidEnvName, got: %v", tt.input, err)
			}
		})
	}
}

// TestFileResolver_Resolve_SymlinkEscape verifies that a legitimately-named
// override file that is a symlink pointing outside the project root is rejected
// by the EvalSymlinks containment check, not just by the name allowlist.
//
// This test guards the defense-in-depth layer added for CVE fix #1269: a name
// like "prod" passes the allowlist, so vibewarden.prod.yaml is a valid path.
// But if that file is a symlink to /etc/passwd, the resolver must reject it
// before LoadMergedConfig reads through the symlink.
func TestFileResolver_Resolve_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create a target file outside the project root with sentinel content.
	sentinel := "SENTINEL_CONTENT_MUST_NOT_BE_READ"
	targetFile := filepath.Join(outside, "secret.yaml")
	writeFile(t, targetFile, sentinel)

	// Also write a valid vibewarden.yaml so the base-config check passes.
	writeFile(t, filepath.Join(root, "vibewarden.yaml"), minimalBase(8443))

	// Create a symlink inside the project root that points to the outside target.
	symlinkPath := filepath.Join(root, "vibewarden.prod.yaml")
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	r := envpkg.NewFileResolver(root)
	_, err := r.Resolve("prod")
	if err == nil {
		t.Fatal("Resolve(\"prod\") expected error for symlink escape, got nil")
	}
	if !errors.Is(err, envpkg.ErrInvalidEnvName) {
		t.Errorf("expected ErrInvalidEnvName, got: %v", err)
	}
}

// TestResolvePath_ReturnsPathWithoutLoadingConfig verifies that ResolvePath
// returns the override path for a valid env name even when the override file
// contains unknown YAML keys that would cause config.Load / LoadMergedConfig
// to fail validation. This is the primary regression guard for #1301: callers
// such as vibew validate must receive the path so they can run their own
// stricter loader (config.LoadStrict) rather than have the lenient merge
// pre-validate and silently swallow the typo.
func TestResolvePath_ReturnsPathWithoutLoadingConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"),
		"server:\n  port: 8080\nupstream:\n  port: 3000\n")
	// Unknown key tls.dmain would cause Resolve to fail via LoadMergedConfig+Validate.
	writeFile(t, filepath.Join(dir, "vibewarden.production.yaml"),
		"tls:\n  provider: letsencrypt\n  dmain: example.com\n")

	r := envpkg.NewFileResolver(dir)
	got, err := r.ResolvePath("production")
	if err != nil {
		t.Fatalf("ResolvePath(\"production\") unexpected error: %v", err)
	}
	want := filepath.Join(dir, "vibewarden.production.yaml")
	if got != want {
		t.Errorf("ResolvePath(\"production\") = %q, want %q", got, want)
	}
}

// TestResolvePath_EmptyName returns empty path without error.
func TestResolvePath_EmptyName(t *testing.T) {
	dir := t.TempDir()
	r := envpkg.NewFileResolver(dir)
	got, err := r.ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath(\"\") unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("ResolvePath(\"\") = %q, want empty", got)
	}
}

// TestResolvePath_AbsentOverride returns empty path without error when the
// override file does not exist — base-only mode.
func TestResolvePath_AbsentOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"), minimalBase(8443))
	r := envpkg.NewFileResolver(dir)
	got, err := r.ResolvePath("production")
	if err != nil {
		t.Fatalf("ResolvePath(\"production\") unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("ResolvePath(\"production\") = %q, want empty (absent override)", got)
	}
}

// TestResolvePath_MaliciousName verifies traversal names are rejected.
func TestResolvePath_MaliciousName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"), minimalBase(8443))
	r := envpkg.NewFileResolver(dir)

	malicious := []string{
		"../etc/passwd",
		"../../foo",
		"foo/bar",
		".hidden",
		"foo\x00bar",
		".",
		"..",
	}
	for _, name := range malicious {
		_, err := r.ResolvePath(name)
		if err == nil {
			t.Errorf("ResolvePath(%q) expected error, got nil", name)
			continue
		}
		if !errors.Is(err, envpkg.ErrInvalidEnvName) {
			t.Errorf("ResolvePath(%q) expected ErrInvalidEnvName, got: %v", name, err)
		}
	}
}

// TestResolvePath_SymlinkEscape verifies a symlink pointing outside project
// root is rejected by the containment check.
func TestResolvePath_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	writeFile(t, filepath.Join(root, "vibewarden.yaml"), minimalBase(8443))
	writeFile(t, filepath.Join(outside, "secret.yaml"), "SENTINEL")

	symlinkPath := filepath.Join(root, "vibewarden.prod.yaml")
	if err := os.Symlink(filepath.Join(outside, "secret.yaml"), symlinkPath); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	r := envpkg.NewFileResolver(root)
	_, err := r.ResolvePath("prod")
	if err == nil {
		t.Fatal("ResolvePath(\"prod\") expected error for symlink escape, got nil")
	}
	if !errors.Is(err, envpkg.ErrInvalidEnvName) {
		t.Errorf("expected ErrInvalidEnvName, got: %v", err)
	}
}

// TestResolvePath_ValidOverride verifies a legitimate override returns its path.
func TestResolvePath_ValidOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vibewarden.yaml"), minimalBase(8443))
	writeFile(t, filepath.Join(dir, "vibewarden.staging.yaml"),
		"server:\n  port: 9443\n")

	r := envpkg.NewFileResolver(dir)
	got, err := r.ResolvePath("staging")
	if err != nil {
		t.Fatalf("ResolvePath(\"staging\") unexpected error: %v", err)
	}
	want := filepath.Join(dir, "vibewarden.staging.yaml")
	if got != want {
		t.Errorf("ResolvePath(\"staging\") = %q, want %q", got, want)
	}
}

// TestValidateEnvName_AcceptsLegitimateNames verifies that well-formed env
// names are accepted by the resolver given a matching override file exists.
func TestValidateEnvName_AcceptsLegitimateNames(t *testing.T) {
	tests := []struct {
		name    string
		envName string
	}{
		{"simple", "prod"},
		{"hyphen", "staging-eu"},
		{"underscore", "local_dev"},
		{"mixed case", "StageUS"},
		{"digits", "env01"},
		{"all allowed chars", "My-Env_123"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "vibewarden.yaml"), minimalBase(8443))
			writeFile(t, filepath.Join(dir, "vibewarden."+tt.envName+".yaml"),
				"tls:\n  domain: example.com\n")

			r := envpkg.NewFileResolver(dir)
			resolved, err := r.Resolve(tt.envName)
			if err != nil {
				t.Errorf("Resolve(%q) unexpected error: %v", tt.envName, err)
				return
			}
			if resolved.EnvName != tt.envName {
				t.Errorf("EnvName = %q, want %q", resolved.EnvName, tt.envName)
			}
		})
	}
}
