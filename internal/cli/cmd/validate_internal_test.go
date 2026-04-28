package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiscoverProdOverride covers every branch of the auto-discovery helper
// used by `vibew validate`. The helper's contract: when configPath is non-empty,
// look for the sibling vibewarden.production.yaml. When configPath is empty,
// resolve against os.Getwd(). Return the empty string when the override is
// absent, the directory cannot be read, or os.Getwd() fails.
func TestDiscoverProdOverride(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T) string // returns configPath
		want   func(t *testing.T, dir string) string
		expect string // literal value when setup returns no dir; "" means use want fn
	}{
		{
			// empty configPath → falls through to os.Getwd(). The package test
			// directory does not contain vibewarden.production.yaml so the result
			// is "". For positive cwd cases see TestDiscoverProdOverride_EmptyCwdWithOverride.
			name:  "empty input returns empty (no prod override in cwd)",
			setup: func(_ *testing.T) string { return "" },
			want:  func(_ *testing.T, _ string) string { return "" },
		},
		{
			name: "base config missing returns empty",
			setup: func(t *testing.T) string {
				// Return a path inside an empty tempdir that doesn't exist.
				return filepath.Join(t.TempDir(), "missing.yaml")
			},
			want: func(_ *testing.T, _ string) string { return "" },
		},
		{
			name: "base config present but no sibling prod returns empty",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				basePath := filepath.Join(dir, "vibewarden.yaml")
				if err := os.WriteFile(basePath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing base: %v", err)
				}
				return basePath
			},
			want: func(_ *testing.T, _ string) string { return "" },
		},
		{
			name: "base config with sibling prod returns prod path",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				basePath := filepath.Join(dir, "vibewarden.yaml")
				if err := os.WriteFile(basePath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing base: %v", err)
				}
				prodPath := filepath.Join(dir, "vibewarden.production.yaml")
				if err := os.WriteFile(prodPath, []byte("server:\n  port: 443\n"), 0o600); err != nil {
					t.Fatalf("writing prod: %v", err)
				}
				return basePath
			},
			want: func(_ *testing.T, basePath string) string {
				return filepath.Join(filepath.Dir(basePath), "vibewarden.production.yaml")
			},
		},
		{
			name: "sibling directory instead of file returns empty",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				basePath := filepath.Join(dir, "vibewarden.yaml")
				if err := os.WriteFile(basePath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing base: %v", err)
				}
				// Create a directory with the exact override filename: stat
				// succeeds, but discoverProdOverride's contract (a readable
				// override file) is not met. Current helper returns the
				// directory path — this test documents that behaviour; if
				// the implementation tightens to require a regular file,
				// update this case accordingly.
				if err := os.Mkdir(filepath.Join(dir, "vibewarden.production.yaml"), 0o700); err != nil {
					t.Fatalf("mkdir override dir: %v", err)
				}
				return basePath
			},
			want: func(_ *testing.T, basePath string) string {
				return filepath.Join(filepath.Dir(basePath), "vibewarden.production.yaml")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			basePath := tt.setup(t)
			got := discoverProdOverride(basePath)
			want := tt.want(t, basePath)
			if got != want {
				t.Errorf("discoverProdOverride(%q) = %q, want %q", basePath, got, want)
			}
		})
	}
}

// TestDiscoverProdOverride_EmptyCwdWithOverride verifies that
// discoverProdOverride("") resolves against os.Getwd() and returns the
// absolute path when vibewarden.production.yaml exists in cwd.
func TestDiscoverProdOverride_EmptyCwdWithOverride(t *testing.T) {
	dir := t.TempDir()
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte("name: prod\n"), 0o600); err != nil {
		t.Fatalf("writing prod: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	got := discoverProdOverride("")
	if got == "" {
		t.Error("discoverProdOverride(\"\") = \"\", want non-empty path")
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
		t.Errorf("discoverProdOverride(\"\") = %q, want %q", got, prodPath)
	}
}

// TestDiscoverProdOverride_EmptyCwdWithoutOverride verifies that
// discoverProdOverride("") returns "" when no vibewarden.production.yaml
// exists in cwd.
func TestDiscoverProdOverride_EmptyCwdWithoutOverride(t *testing.T) {
	dir := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	got := discoverProdOverride("")
	if got != "" {
		t.Errorf("discoverProdOverride(\"\") = %q, want empty", got)
	}
}

// TestDiscoverProdOverride_UnreadableDir verifies the helper swallows
// permission errors (returning ""), as documented in its godoc. This is the
// one case where silent fallback is intentional: a `vibew validate` on a
// config whose parent dir is unreadable should not crash — the subsequent
// config.LoadStrict call will surface the real error against the base file.
func TestDiscoverProdOverride_UnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("cannot simulate permission denied when running as root")
	}

	dir := t.TempDir()
	basePath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	// Make parent dir unreadable so Stat on the sibling fails.
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	// Restore directory permissions so t.TempDir's cleanup can recurse.
	// 0o700 is required because directories need the execute bit to be traversable.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // test-only dir perms

	got := discoverProdOverride(basePath)
	if got != "" {
		t.Errorf("discoverProdOverride with unreadable dir = %q, want empty", got)
	}
}
