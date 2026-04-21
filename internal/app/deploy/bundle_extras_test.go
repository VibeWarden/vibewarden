package deploy_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	"github.com/vibewarden/vibewarden/internal/config"
)

// memBundleFS is an in-memory BundleFS for unit tests. It records every
// write so tests can assert the extras pipeline's ordering and idempotency
// without touching disk.
type memBundleFS struct {
	mu      sync.Mutex
	files   map[string][]byte
	modes   map[string]fs.FileMode
	mkdirs  []string
	writes  []string
	statErr error // optional: returned by Exists regardless of path
}

func newMemBundleFS() *memBundleFS {
	return &memBundleFS{
		files: make(map[string][]byte),
		modes: make(map[string]fs.FileMode),
	}
}

func (m *memBundleFS) Exists(path string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statErr != nil {
		return false, m.statErr
	}
	_, ok := m.files[path]
	return ok, nil
}

func (m *memBundleFS) ReadFile(path string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (m *memBundleFS) WriteFile(path string, data []byte, perm fs.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = append([]byte(nil), data...)
	m.modes[path] = perm
	m.writes = append(m.writes, path)
	return nil
}

func (m *memBundleFS) MkdirAll(path string, _ fs.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mkdirs = append(m.mkdirs, path)
	return nil
}

// countingImageSaver records Save calls so tests can assert skip-image
// semantics without a real docker daemon.
type countingImageSaver struct {
	calls int
	err   error
}

func (c *countingImageSaver) Save(_ context.Context, _, _ string) error {
	c.calls++
	return c.err
}

// minimalBundleCfg returns the smallest config that exercises the extras
// pipeline without tripping any generator branches. Tests that need
// richer behaviour override specific fields.
func minimalBundleCfg() *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "app", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}
}

func TestBundle_Extras_WritesExpectedFileSet(t *testing.T) {
	mem := newMemBundleFS()
	saver := &countingImageSaver{}
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageSaver(saver)

	outDir := t.TempDir() // real dir so the existing bundleSingleSite helpers work
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		ImageTag:    "myproject-app:latest",
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	wantFiles := []string{"sample.env", ".env", "deploy.sh", "README.md"}
	for _, name := range wantFiles {
		p := filepath.Join(outDir, name)
		if _, ok := mem.files[p]; !ok {
			t.Errorf("expected %s in bundle, got writes: %v", name, mem.writes)
		}
	}
	if saver.calls != 1 {
		t.Errorf("ImageSaver.Save calls = %d, want 1", saver.calls)
	}
}

func TestBundle_Extras_SkipImage_OmitsImageTar(t *testing.T) {
	mem := newMemBundleFS()
	saver := &countingImageSaver{}
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageSaver(saver)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		ImageTag:    "myproject-app:latest",
		SkipImage:   true,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	if saver.calls != 0 {
		t.Errorf("ImageSaver.Save calls = %d, want 0 with --skip-image", saver.calls)
	}
	if _, ok := mem.files[filepath.Join(outDir, "image.tar")]; ok {
		t.Errorf("expected image.tar NOT present with --skip-image")
	}
	// The other artifacts still appear.
	for _, name := range []string{"sample.env", ".env", "deploy.sh", "README.md"} {
		if _, ok := mem.files[filepath.Join(outDir, name)]; !ok {
			t.Errorf("expected %s to still exist with --skip-image", name)
		}
	}
}

func TestBundle_Extras_DeploySH_ExecutableMode(t *testing.T) {
	mem := newMemBundleFS()
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		SkipImage:   true,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	mode, ok := mem.modes[filepath.Join(outDir, "deploy.sh")]
	if !ok {
		t.Fatal("deploy.sh not written")
	}
	if mode&0o100 == 0 {
		t.Errorf("deploy.sh mode = %v, want owner-execute bit set", mode)
	}
}

func TestBundle_Extras_DeploySH_DockerLoadVsPull(t *testing.T) {
	tests := []struct {
		name      string
		skipImage bool
		wantFrag  string
	}{
		{name: "with image", skipImage: false, wantFrag: "docker load -i image.tar"},
		{name: "skip image", skipImage: true, wantFrag: "docker compose pull && docker compose up -d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMemBundleFS()
			svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
				WithBundleFS(mem)

			outDir := t.TempDir()
			err := svc.Bundle(context.Background(), deployapp.BundleOptions{
				Config:      minimalBundleCfg(),
				ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
				ProjectName: "myproject",
				OutputDir:   outDir,
				SkipImage:   tt.skipImage,
			})
			if err != nil {
				t.Fatalf("Bundle() error = %v", err)
			}

			body := string(mem.files[filepath.Join(outDir, "deploy.sh")])
			if !strings.Contains(body, tt.wantFrag) {
				t.Errorf("deploy.sh missing %q\nbody:\n%s", tt.wantFrag, body)
			}
		})
	}
}

func TestBundle_Extras_SampleEnv_DefaultsToComposeProjectName(t *testing.T) {
	mem := newMemBundleFS()
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	cfg := minimalBundleCfg()
	cfg.Name = "" // force ComposeProjectName to fall back to the directory name

	// Project dir's basename will seed ComposeProjectName when cfg.Name is empty.
	projectDir := t.TempDir()
	configPath := filepath.Join(projectDir, "vibewarden.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  configPath,
		ProjectName: "myproject",
		OutputDir:   outDir,
		SkipImage:   true,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	body := string(mem.files[filepath.Join(outDir, "sample.env")])
	if !strings.Contains(body, "VIBEWARDEN_APP_IMAGE=") {
		t.Errorf("sample.env missing VIBEWARDEN_APP_IMAGE line\nbody:\n%s", body)
	}
	// Output must not contain explicit empty values for the managed key —
	// a value is always set.
	if strings.Contains(body, "VIBEWARDEN_APP_IMAGE=\n") {
		t.Errorf("sample.env VIBEWARDEN_APP_IMAGE has empty value, want non-empty\nbody:\n%s", body)
	}
}

func TestBundle_Extras_SampleEnv_IncludesTemplateKeys(t *testing.T) {
	mem := newMemBundleFS()
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	projectDir := t.TempDir()
	configPath := filepath.Join(projectDir, "vibewarden.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	tmpl := "# comment\nDATABASE_URL=postgres://localhost/app\nAPI_KEY=\n\nFOO=bar\n"
	if err := os.WriteFile(filepath.Join(projectDir, ".env.template"), []byte(tmpl), 0o600); err != nil {
		t.Fatalf("writing .env.template: %v", err)
	}

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  configPath,
		ProjectName: "myproject",
		OutputDir:   outDir,
		ImageTag:    "myproject-app:latest",
		SkipImage:   true,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	body := string(mem.files[filepath.Join(outDir, "sample.env")])
	for _, key := range []string{"DATABASE_URL=", "API_KEY=", "FOO="} {
		if !strings.Contains(body, key) {
			t.Errorf("sample.env missing template key %q\nbody:\n%s", key, body)
		}
	}
}

func TestBundle_Extras_Readme_MentionsPlatformHint(t *testing.T) {
	mem := newMemBundleFS()
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "blog",
		OutputDir:   outDir,
		SkipImage:   true,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	body := string(mem.files[filepath.Join(outDir, "README.md")])
	if !strings.Contains(body, "vibew build --platform linux/amd64") {
		t.Errorf("README.md missing --platform hint\nbody:\n%s", body)
	}
	if !strings.Contains(body, "blog") {
		t.Errorf("README.md missing project name\nbody:\n%s", body)
	}
	lines := strings.Count(body, "\n") + 1
	if lines > 40 {
		t.Errorf("README.md = %d lines, want <= 40", lines)
	}
}

func TestBundle_Extras_NoBundleFS_NoOp(t *testing.T) {
	// When Service.bundleFS is nil, the extras pipeline must do nothing —
	// existing callers (vibew deploy --dry-run) rely on this fallback.
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{})

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		SkipImage:   true,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	for _, name := range []string{"sample.env", ".env", "deploy.sh", "README.md", "image.tar"} {
		if _, statErr := os.Stat(filepath.Join(outDir, name)); !os.IsNotExist(statErr) {
			t.Errorf("expected %s NOT present when no BundleFS is wired", name)
		}
	}
}

func TestBundle_Extras_MultiSite_SkipsExtras(t *testing.T) {
	// Multi-site bundles route through bundleMultiSiteSite which does not
	// call writeBundleExtras — the extras are single-site only per ADR-085.
	// This is the service-level contract the CLI relies on when it
	// hard-errors before setting BundleOptions.MultiSite=true.
	mem := newMemBundleFS()
	saver := &countingImageSaver{}
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageSaver(saver)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "blog",
		MultiSite:   true,
		OutputDir:   outDir,
	})
	if err != nil {
		t.Fatalf("Bundle(multi-site) error = %v", err)
	}

	for _, name := range []string{"sample.env", ".env", "deploy.sh", "README.md"} {
		if _, ok := mem.files[filepath.Join(outDir, name)]; ok {
			t.Errorf("multi-site bundle must not write %s (extras are single-site only)", name)
		}
	}
	if saver.calls != 0 {
		t.Errorf("multi-site bundle must not call ImageSaver, got calls = %d", saver.calls)
	}
}

func TestBundle_Extras_ImageSaverError_PropagatesWrapped(t *testing.T) {
	mem := newMemBundleFS()
	saver := &countingImageSaver{err: errors.New("no such image")}
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageSaver(saver)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		ImageTag:    "myproject-app:latest",
	})
	if err == nil {
		t.Fatal("Bundle() error = nil, want image save failure")
	}
	if !strings.Contains(err.Error(), "no such image") {
		t.Errorf("error should wrap docker save cause, got: %v", err)
	}
}
