package bundle_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	"github.com/vibewarden/vibewarden/internal/config"
)

// skipOnWindows guards permission assertions: fs.FileMode.Perm() carries no
// meaningful POSIX bits on Windows.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
}

// assertDirPerm stats path and asserts it is a directory with mode 0700.
func assertDirPerm(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
	if got := info.Mode().Perm(); got != fs.FileMode(0o700) {
		t.Errorf("%s mode = %04o, want 0700", path, got)
	}
}

// TestDirPerm_IsOwnerOnly pins the constant itself. Bundle directories hold
// generated secrets, so group and other must have no bits at all.
func TestDirPerm_IsOwnerOnly(t *testing.T) {
	if bundleapp.DirPerm != fs.FileMode(0o700) {
		t.Errorf("DirPerm = %04o, want 0700", bundleapp.DirPerm)
	}
	if bundleapp.DirPerm&0o077 != 0 {
		t.Errorf("DirPerm = %04o grants group/other access", bundleapp.DirPerm)
	}
}

// TestBundleSidecar_CreatesOwnerOnlyDirectory asserts the on-disk mode of the
// directory created through the writeFile helper, not the literal passed to
// MkdirAll.
func TestBundleSidecar_CreatesOwnerOnlyDirectory(t *testing.T) {
	skipOnWindows(t)

	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{})
	outputDir := t.TempDir()

	cfg := &config.Config{Server: config.ServerConfig{Port: 443}}
	if err := svc.BundleSidecar(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("BundleSidecar() error = %v", err)
	}

	assertDirPerm(t, filepath.Join(outputDir, ".sidecar"))
}

// TestBundle_MultiSite_CreatesOwnerOnlyDirectories covers the nested
// sites/<project>/ directory, which is also created via writeFile.
func TestBundle_MultiSite_CreatesOwnerOnlyDirectories(t *testing.T) {
	skipOnWindows(t)

	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{})
	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		MultiSite:   true,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	assertDirPerm(t, filepath.Join(outputDir, "sites"))
	assertDirPerm(t, filepath.Join(outputDir, "sites", "myproject"))
}

// The <projectRoot>/.vibewarden directory created by input_digest.go is
// covered end to end in internal/cli/cmd (TestBundleCmd_CreatesOwnerOnlyDirs),
// where the real command wiring produces it.
