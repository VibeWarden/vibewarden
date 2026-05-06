package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	envapp "github.com/vibewarden/vibewarden/internal/app/env"
)

// TestResolveEnvOverridePath covers the canonical env-override resolution that
// now backs both `vibew bundle` and `vibew validate` (ADR-102, #1301).
// resolveEnvOverridePath delegates to env.FileResolver which enforces the
// allowlist + EvalSymlinks containment check (#1269).
func TestResolveEnvOverridePath(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) (projectRoot string)
		envName   string
		wantEmpty bool
		wantPath  func(projectRoot string) string // only checked when wantEmpty == false
	}{
		{
			name: "override absent returns empty",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing base: %v", err)
				}
				return dir
			},
			envName:   "production",
			wantEmpty: true,
		},
		{
			name: "override present returns its path",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing base: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "vibewarden.production.yaml"), []byte("server:\n  port: 443\n"), 0o600); err != nil {
					t.Fatalf("writing prod: %v", err)
				}
				return dir
			},
			envName:   "production",
			wantEmpty: false,
			wantPath:  func(root string) string { return filepath.Join(root, "vibewarden.production.yaml") },
		},
		{
			name: "base config absent returns empty",
			setup: func(t *testing.T) string {
				// Empty dir — no vibewarden.yaml, no override.
				return t.TempDir()
			},
			envName:   "production",
			wantEmpty: true,
		},
		{
			name: "staging override returns its path",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing base: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "vibewarden.staging.yaml"), []byte("server:\n  port: 9443\n"), 0o600); err != nil {
					t.Fatalf("writing staging: %v", err)
				}
				return dir
			},
			envName:   "staging",
			wantEmpty: false,
			wantPath:  func(root string) string { return filepath.Join(root, "vibewarden.staging.yaml") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tt.setup(t)
			got := resolveEnvOverridePath(root, tt.envName)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("resolveEnvOverridePath(%q, %q) = %q, want empty", root, tt.envName, got)
				}
				return
			}
			want := tt.wantPath(root)
			if got != want {
				t.Errorf("resolveEnvOverridePath(%q, %q) = %q, want %q", root, tt.envName, got, want)
			}
		})
	}
}

// TestResolveEnvOverridePath_MaliciousEnvName verifies that resolveEnvOverridePath
// blocks path-traversal env names — same as direct env.FileResolver use — because
// it delegates to the resolver rather than building paths inline (#1301, #1269).
func TestResolveEnvOverridePath_MaliciousEnvName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}

	malicious := []string{
		"../etc/passwd",
		"../../foo",
		"foo/bar",
		".hidden",
		"foo\x00bar",
	}
	for _, name := range malicious {
		got := resolveEnvOverridePath(dir, name)
		if got != "" {
			t.Errorf("resolveEnvOverridePath(root, %q) = %q, want empty (should block traversal)", name, got)
		}
	}
}

// TestResolveEnvOverridePath_SymlinkEscape verifies that a legitimately-named
// override file that is a symlink pointing outside the project root is blocked
// by the EvalSymlinks containment check in env.FileResolver (#1269, #1301).
func TestResolveEnvOverridePath_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Write a valid base config so the resolver reaches the override check.
	if err := os.WriteFile(filepath.Join(root, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	// Create a sentinel file outside the project root.
	if err := os.WriteFile(filepath.Join(outside, "secret.yaml"), []byte("SENTINEL"), 0o600); err != nil {
		t.Fatalf("writing sentinel: %v", err)
	}
	// Symlink inside root points outside root.
	symlinkPath := filepath.Join(root, "vibewarden.prod.yaml")
	if err := os.Symlink(filepath.Join(outside, "secret.yaml"), symlinkPath); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	got := resolveEnvOverridePath(root, "prod")
	if got != "" {
		t.Errorf("resolveEnvOverridePath with symlink escape = %q, want empty", got)
	}
}

// TestResolveEnvOverridePath_ErrorIsErrInvalidEnvName verifies that the
// underlying resolver returns ErrInvalidEnvName for traversal names, and that
// resolveEnvOverridePath correctly absorbs it (returning "").
// This test validates the error class; the caller should never see the error.
func TestResolveEnvOverridePath_ErrorIsErrInvalidEnvName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}

	// Verify the sentinel error class is what the resolver produces,
	// so future maintainers know why we can silently absorb it.
	r := envapp.NewFileResolver(dir)
	_, err := r.Resolve("../etc/passwd")
	if err == nil {
		t.Fatal("expected error from resolver for traversal name, got nil")
	}
	if !errors.Is(err, envapp.ErrInvalidEnvName) {
		t.Errorf("expected ErrInvalidEnvName, got: %v", err)
	}
}

// TestDiscoverProdOverride_EmptyConfigPathResolvesAgainstCwd verifies that
// when configPath is empty, resolveEnvOverridePath uses os.Getwd() and returns
// the absolute path when vibewarden.production.yaml exists in cwd.
func TestDiscoverProdOverride_EmptyConfigPathResolvesAgainstCwd(t *testing.T) {
	dir := t.TempDir()
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte("server:\n  port: 443\n"), 0o600); err != nil {
		t.Fatalf("writing prod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// When configPath is "", validate.go resolves searchDir via os.Getwd()
	// and passes it to resolveEnvOverridePath. Simulate that here.
	cwd, _ := os.Getwd()
	got := resolveEnvOverridePath(cwd, "production")
	if got == "" {
		t.Error("resolveEnvOverridePath(cwd, production) = \"\", want non-empty path")
	}
	// On macOS, t.TempDir() returns a path under /var/... which is a symlink to
	// /private/var/... while os.Getwd() returns the symlink-resolved path. Use
	// filepath.EvalSymlinks to normalise both before comparing.
	wantResolved, err := filepath.EvalSymlinks(prodPath)
	if err != nil {
		t.Fatalf("EvalSymlinks prod: %v", err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks got: %v", err)
	}
	if gotResolved != wantResolved {
		t.Errorf("resolveEnvOverridePath(cwd, production) = %q, want %q", got, prodPath)
	}
}

// TestDiscoverProdOverride_EmptyConfigPathNoOverride verifies that
// resolveEnvOverridePath returns "" when no vibewarden.production.yaml
// exists in cwd.
func TestDiscoverProdOverride_EmptyConfigPathNoOverride(t *testing.T) {
	dir := t.TempDir()
	// Write base config but no override.
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}

	got := resolveEnvOverridePath(dir, "production")
	if got != "" {
		t.Errorf("resolveEnvOverridePath(dir, production) = %q, want empty (no override)", got)
	}
}
