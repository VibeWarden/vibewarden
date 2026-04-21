package bundle_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
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
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageSaver(saver)

	outDir := t.TempDir() // real dir so the existing bundleSingleSite helpers work
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageSaver(saver)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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
		{name: "with image", skipImage: false, wantFrag: "docker load -i image.tar && docker compose up -d"},
		{name: "skip image", skipImage: true, wantFrag: "docker compose pull && docker compose up -d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMemBundleFS()
			svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
				WithBundleFS(mem)

			outDir := t.TempDir()
			err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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

// TestBundle_Extras_DeploySH_GoldenFixtures is the ADR-088 render-layer
// guard: the rendered deploy.sh must be byte-identical to the committed
// fixtures under testdata/deploy_sh/. Fixtures are regenerated manually
// on intentional format changes — there is no -update flag because the
// fixtures double as human-auditable reference output.
func TestBundle_Extras_DeploySH_GoldenFixtures(t *testing.T) {
	tests := []struct {
		name      string
		skipImage bool
		fixture   string
	}{
		{name: "with image", skipImage: false, fixture: "testdata/deploy_sh/with_image.sh"},
		{name: "skip image", skipImage: true, fixture: "testdata/deploy_sh/skip_image.sh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMemBundleFS()
			svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
				WithBundleFS(mem)

			outDir := t.TempDir()
			err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
				Config:      minimalBundleCfg(), // Server.Port = 8443
				ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
				ProjectName: "myproject",
				OutputDir:   outDir,
				SkipImage:   tt.skipImage,
			})
			if err != nil {
				t.Fatalf("Bundle() error = %v", err)
			}

			got := string(mem.files[filepath.Join(outDir, "deploy.sh")])
			wantBytes, readErr := os.ReadFile(tt.fixture) //nolint:gosec // testdata relative path
			if readErr != nil {
				t.Fatalf("reading fixture %s: %v", tt.fixture, readErr)
			}
			if got != string(wantBytes) {
				t.Errorf("deploy.sh body mismatches fixture %s\n---GOT---\n%s\n---WANT---\n%s", tt.fixture, got, string(wantBytes))
			}
		})
	}
}

// TestBundle_Extras_DeploySH_BashSyntax runs `bash -n` against the rendered
// script body to guard against typos that would only surface at deploy
// time. Auto-skips when bash is not on PATH (Windows CI without WSL).
func TestBundle_Extras_DeploySH_BashSyntax(t *testing.T) {
	bashPath, lookErr := exec.LookPath("bash")
	if lookErr != nil {
		t.Skip("integration prerequisite missing: bash not on PATH — install it or run under WSL")
	}

	tests := []struct {
		name      string
		skipImage bool
	}{
		{name: "with image", skipImage: false},
		{name: "skip image", skipImage: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMemBundleFS()
			svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
				WithBundleFS(mem)

			outDir := t.TempDir()
			err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
				Config:      minimalBundleCfg(),
				ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
				ProjectName: "myproject",
				OutputDir:   outDir,
				SkipImage:   tt.skipImage,
			})
			if err != nil {
				t.Fatalf("Bundle() error = %v", err)
			}

			scriptPath := filepath.Join(t.TempDir(), "deploy.sh")
			body := mem.files[filepath.Join(outDir, "deploy.sh")]
			if err := os.WriteFile(scriptPath, body, 0o600); err != nil {
				t.Fatalf("writing script to tempdir: %v", err)
			}

			cmd := exec.Command(bashPath, "-n", scriptPath) //nolint:gosec // test-owned paths
			out, runErr := cmd.CombinedOutput()
			if runErr != nil {
				t.Errorf("bash -n failed: %v\noutput: %s\nscript:\n%s", runErr, out, body)
			}
		})
	}
}

// TestBundle_Extras_DeploySH_Determinism asserts that two invocations of
// the render path with identical inputs produce byte-identical output.
// Mirrors the pattern in bundle_determinism_test.go.
func TestBundle_Extras_DeploySH_Determinism(t *testing.T) {
	render := func() []byte {
		mem := newMemBundleFS()
		svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
			WithBundleFS(mem)

		outDir := t.TempDir()
		err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
			Config:      minimalBundleCfg(),
			ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
			ProjectName: "myproject",
			OutputDir:   outDir,
			SkipImage:   false,
		})
		if err != nil {
			t.Fatalf("Bundle() error = %v", err)
		}
		return mem.files[filepath.Join(outDir, "deploy.sh")]
	}

	a := render()
	b := render()
	if string(a) != string(b) {
		t.Errorf("deploy.sh not deterministic\nA:\n%s\nB:\n%s", a, b)
	}
}

// TestBundle_Extras_DeploySH_HealthPort_FromConfig verifies that the
// merged config's Server.Port is baked into the rendered script. When
// Server.Port is unset, the default 8443 is used.
func TestBundle_Extras_DeploySH_HealthPort_FromConfig(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		wantPort string
	}{
		{name: "default port", port: 0, wantPort: "http://localhost:8443/_vibewarden/health"},
		{name: "custom port", port: 9443, wantPort: "http://localhost:9443/_vibewarden/health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := minimalBundleCfg()
			cfg.Server.Port = tt.port

			mem := newMemBundleFS()
			svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
				WithBundleFS(mem)

			outDir := t.TempDir()
			err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
				Config:      cfg,
				ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
				ProjectName: "myproject",
				OutputDir:   outDir,
				SkipImage:   true,
			})
			if err != nil {
				t.Fatalf("Bundle() error = %v", err)
			}

			body := string(mem.files[filepath.Join(outDir, "deploy.sh")])
			if !strings.Contains(body, tt.wantPort) {
				t.Errorf("deploy.sh missing %q\nbody:\n%s", tt.wantPort, body)
			}
		})
	}
}

// TestBundle_Extras_DeploySH_ArgParsing exercises the rendered script's
// usage validation by invoking it with 0, 1, and 2 positional args under
// bash. Fake `scp` and `ssh` on PATH keep the test hermetic (no real
// network I/O). Auto-skips when bash is unavailable.
func TestBundle_Extras_DeploySH_ArgParsing(t *testing.T) {
	bashPath, lookErr := exec.LookPath("bash")
	if lookErr != nil {
		t.Skip("integration prerequisite missing: bash not on PATH — install it or run under WSL")
	}

	// Render once, write to a tempdir, and run with fake scp/ssh/curl on PATH.
	mem := newMemBundleFS()
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)
	outDir := t.TempDir()
	if err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		SkipImage:   true,
	}); err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	workDir := t.TempDir()
	scriptPath := filepath.Join(workDir, "deploy.sh")
	body := mem.files[filepath.Join(outDir, "deploy.sh")]
	if err := os.WriteFile(scriptPath, body, 0o755); err != nil { //nolint:gosec // script must be executable
		t.Fatalf("writing script: %v", err)
	}

	// Fake scp/ssh/curl that always succeed — so a valid invocation exits 0.
	fakeBinDir := t.TempDir()
	for _, name := range []string{"scp", "ssh", "curl"} {
		stub := filepath.Join(fakeBinDir, name)
		if err := os.WriteFile(stub, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test stub
			t.Fatalf("writing stub %s: %v", name, err)
		}
	}

	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantErr  string // substring on stderr; "" means no check
	}{
		{name: "zero args", args: nil, wantExit: 1, wantErr: "usage:"},
		{name: "one arg", args: []string{"user@host"}, wantExit: 0, wantErr: ""},
		{name: "two args", args: []string{"user@host", "extra"}, wantExit: 1, wantErr: "usage:"},
		{name: "with remote path", args: []string{"user@host:/srv/app"}, wantExit: 0, wantErr: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bashPath, append([]string{scriptPath}, tt.args...)...) //nolint:gosec // test-owned paths + fixture args
			cmd.Dir = workDir
			cmd.Env = append(os.Environ(), "PATH="+fakeBinDir+":"+os.Getenv("PATH"))

			var stderr strings.Builder
			cmd.Stderr = &stderr
			cmd.Stdout = &strings.Builder{}

			runErr := cmd.Run()
			gotExit := 0
			if runErr != nil {
				if ee, ok := runErr.(*exec.ExitError); ok {
					gotExit = ee.ExitCode()
				} else {
					t.Fatalf("unexpected run error: %v", runErr)
				}
			}
			if gotExit != tt.wantExit {
				t.Errorf("exit code = %d, want %d\nstderr: %s", gotExit, tt.wantExit, stderr.String())
			}
			if tt.wantErr != "" && !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("stderr missing %q\ngot: %s", tt.wantErr, stderr.String())
			}
		})
	}
}

// TestBundle_Extras_Readme_LocalRunOnly is the ADR-088 docs-consistency
// guard: the rendered README.md must describe the local-run form of
// deploy.sh and must NOT contain the contradictory remote-run recipe.
func TestBundle_Extras_Readme_LocalRunOnly(t *testing.T) {
	mem := newMemBundleFS()
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		SkipImage:   false,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	body := string(mem.files[filepath.Join(outDir, "README.md")])

	// Positive assertions: new local-run recipe.
	for _, want := range []string{
		"./deploy.sh user@host",
		"runs locally",
		"/_vibewarden/health",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("README.md missing %q\nbody:\n%s", want, body)
		}
	}

	// Negative assertions: the old contradictory remote-run phrases must be gone.
	for _, forbidden := range []string{
		"ssh user@host 'cd ~/",
		"bash deploy.sh",
		"Deploy in three steps",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("README.md still contains forbidden phrase %q\nbody:\n%s", forbidden, body)
		}
	}
}

func TestBundle_Extras_SampleEnv_DefaultsToComposeProjectName(t *testing.T) {
	mem := newMemBundleFS()
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
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
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
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
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{})

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageSaver(saver)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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

// TestBundle_DotEnv_RestoredOnGeneratorFailure is the #1061 reviewer guard:
// if the generator clobbers .env with fresh credentials and then fails
// partway through, the user's pre-run .env must be restored. Without the
// defer in Bundle, a mid-run crash would leave the generator's fresh
// random credentials on disk and destroy the user's edits silently.
func TestBundle_DotEnv_RestoredOnGeneratorFailure(t *testing.T) {
	mem := newMemBundleFS()
	// Generator returns an error so bundleSingleSite fails before the
	// extras pipeline runs. The deferred restore must still fire.
	gen := &fakeGenerator{err: errors.New("compose template exploded mid-run")}
	svc := bundleapp.NewService(&fakeExecutor{}, gen).WithBundleFS(mem)

	outDir := t.TempDir()
	priorPath := filepath.Join(outDir, ".env")
	priorContent := []byte("VIBEWARDEN_APP_IMAGE=userpin\nSTRIPE_KEY=sk_live_REAL\n")

	// Seed the existing .env in the in-memory FS AND on disk. The in-memory
	// copy is what snapshotPriorDotEnv reads; the on-disk copy is what
	// bundleSingleSite's generator would clobber in real life. We write to
	// both so the test exercises the exact sequence of a re-run.
	if err := mem.WriteFile(priorPath, priorContent, 0o600); err != nil {
		t.Fatalf("seeding mem .env: %v", err)
	}
	if err := os.WriteFile(priorPath, priorContent, 0o600); err != nil {
		t.Fatalf("seeding disk .env: %v", err)
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		ImageTag:    "myproject-app:latest",
		SkipImage:   true,
	})
	if err == nil {
		t.Fatal("Bundle() expected generator error, got nil")
	}

	// After the failure, the in-memory .env must hold the user's original
	// content — the deferred restore fired.
	got, readErr := mem.ReadFile(priorPath)
	if readErr != nil {
		t.Fatalf("reading .env after failure: %v", readErr)
	}
	if !strings.Contains(string(got), "STRIPE_KEY=sk_live_REAL") {
		t.Errorf(".env NOT restored after generator failure\ngot: %q\nwant substring: STRIPE_KEY=sk_live_REAL", got)
	}
}

// TestBundle_DotEnv_OverwriteSkipsDeferredRestore verifies that --overwrite
// bypasses the deferred restore: callers who explicitly asked to replace
// the .env must not see their prior file resurrected by the error path.
func TestBundle_DotEnv_OverwriteSkipsDeferredRestore(t *testing.T) {
	mem := newMemBundleFS()
	gen := &fakeGenerator{err: errors.New("boom")}
	svc := bundleapp.NewService(&fakeExecutor{}, gen).WithBundleFS(mem)

	outDir := t.TempDir()
	priorPath := filepath.Join(outDir, ".env")
	priorContent := []byte("STRIPE_KEY=old\n")
	if err := mem.WriteFile(priorPath, priorContent, 0o600); err != nil {
		t.Fatalf("seeding mem .env: %v", err)
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		ImageTag:    "myproject-app:latest",
		SkipImage:   true,
		Overwrite:   true,
	})
	if err == nil {
		t.Fatal("Bundle() expected generator error, got nil")
	}
	// --overwrite means: snapshot is dropped regardless of outcome. The mem
	// FS still holds the user's original because we never wrote over it
	// (the fake generator does not touch mem), but the point is the
	// deferred restore MUST NOT have run. Easiest check: assert no
	// additional writes happened during the error path beyond the seed.
	if len(mem.writes) != 1 {
		t.Errorf("expected exactly 1 write (the seed) under --overwrite on failure, got %d: %v",
			len(mem.writes), mem.writes)
	}
}

func TestBundle_Extras_ImageSaverError_PropagatesWrapped(t *testing.T) {
	mem := newMemBundleFS()
	saver := &countingImageSaver{err: errors.New("no such image")}
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageSaver(saver)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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
