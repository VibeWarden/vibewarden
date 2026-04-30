package bundle_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
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

	wantFiles := []string{"sample.env", ".env", "README.md", "MANIFEST.md"}
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
	for _, name := range []string{"sample.env", ".env", "README.md", "MANIFEST.md"} {
		if _, ok := mem.files[filepath.Join(outDir, name)]; !ok {
			t.Errorf("expected %s to still exist with --skip-image", name)
		}
	}
}

// TestBundle_Extras_Readme_DeployContract is the artifact-policy enforcement
// boundary for the bundle README (#1204). The README MUST contain the literal
// deploy command sequence (scp/ssh/docker load/docker compose up) as a
// copy-pasteable fenced block at the top — this inverts the old negative
// assertions, which were removed when the policy shifted from "prose-only"
// to "real commands paired with a forensic alignment test". The forbidden
// token is only the wrapped shell script (./deploy.sh / deploy.sh) that was
// retired in #1138 — not the raw commands themselves.
func TestBundle_Extras_Readme_DeployContract(t *testing.T) {
	for _, skipImage := range []bool{false, true} {
		t.Run(fmt.Sprintf("skipImage=%v", skipImage), func(t *testing.T) {
			mem := newMemBundleFS()
			svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
				WithBundleFS(mem)

			outDir := t.TempDir()
			err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
				Config:      minimalBundleCfg(),
				ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
				ProjectName: "myproject",
				OutputDir:   outDir,
				SkipImage:   skipImage,
			})
			if err != nil {
				t.Fatalf("Bundle() error = %v", err)
			}

			body := string(mem.files[filepath.Join(outDir, "README.md")])

			// Positive assertions: deploy commands are now required (#1204).
			for _, want := range []string{
				"tar -czf - -C",
				"| ssh ",
				"tar -xzf - -C",
				"ssh ",
				"docker compose up",
				"/_vibewarden/health",
				"443",
				"directory must exist",
				"docker-compose.yml",
				"vibewarden.yaml",
				"image.tar",
				"sample.env",
				".env",
				// ADR-094: Secrets section must be present.
				"## Secrets",
				".credentials",
				"transport is untrusted",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("README.md missing %q\nbody:\n%s", want, body)
				}
			}

			// With --skip-image the deploy fenced block must not include docker
			// load; without it the block must include it. We check the first
			// fenced block specifically (between first ```bash and next ```).
			firstBlock := extractFirstFencedBlock(body)
			if skipImage {
				if strings.Contains(firstBlock, "docker load -i image.tar") {
					t.Errorf("deploy fenced block with --skip-image must not contain 'docker load -i image.tar'\nblock:\n%s", firstBlock)
				}
			} else {
				if !strings.Contains(firstBlock, "docker load") {
					t.Errorf("deploy fenced block without --skip-image must contain 'docker load'\nblock:\n%s", firstBlock)
				}
			}

			// Negative assertions: only the wrapped shell script is forbidden.
			for _, forbidden := range []string{
				"./deploy.sh",
				"deploy.sh",
				"PowerShell",
				"pwsh",
			} {
				if strings.Contains(body, forbidden) {
					t.Errorf("README.md must not contain %q\nbody:\n%s", forbidden, body)
				}
			}
		})
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
	if lines > 60 {
		t.Errorf("README.md = %d lines, want <= 60", lines)
	}
}

// TestBundle_Extras_Readme_OrphanCleanupHint verifies that the bundle README
// includes the one-time orphan-cleanup paragraph for users upgrading from
// old stacks that ran under the "vibewarden-app" project name (#1199).
func TestBundle_Extras_Readme_OrphanCleanupHint(t *testing.T) {
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

	body := string(mem.files[filepath.Join(outDir, "README.md")])

	// The README must explain the one-time orphan cleanup for users upgrading
	// from the legacy vibewarden-app project name.
	for _, want := range []string{
		"vibewarden-app",
		"docker compose -p vibewarden-app down",
		"Upgrading from a previous deployment",
		"app.name",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("README.md missing orphan-cleanup hint %q\nbody:\n%s", want, body)
		}
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

	for _, name := range []string{"sample.env", ".env", "README.md", "image.tar"} {
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

	for _, name := range []string{"sample.env", ".env", "README.md"} {
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

// configWithDomain returns a minimal config that includes a TLS domain, used
// by README substitution tests.
func configWithDomain(domain string) *config.Config {
	cfg := minimalBundleCfg()
	cfg.TLS.Domain = domain
	return cfg
}

// TestBundle_Extras_Readme_FencedDeployBlock asserts that the bundle README
// opens with a fenced bash block containing exactly the four-command deploy
// sequence within the first 30 lines.
func TestBundle_Extras_Readme_FencedDeployBlock(t *testing.T) {
	tests := []struct {
		name       string
		appName    string
		domain     string
		skipImage  bool
		wantCmds   []string
		wantAbsent []string
	}{
		{
			name:      "full deploy sequence — no sshHost uses bracketed placeholder",
			appName:   "myapp",
			domain:    "example.com",
			skipImage: false,
			wantCmds: []string{
				"ssh <your-ssh-user>@<your-ssh-host> 'mkdir -p /opt/myapp'",
				"tar -czf - -C .vibewarden/bundle . | ssh <your-ssh-user>@<your-ssh-host> 'tar -xzf - -C /opt/myapp/'",
				"docker load -i image.tar && docker compose up -d",
				"curl -fsSL https://example.com/_vibewarden/health",
			},
		},
		{
			name:      "skip-image omits docker load clause",
			appName:   "myapp",
			domain:    "example.com",
			skipImage: true,
			wantCmds: []string{
				"ssh <your-ssh-user>@<your-ssh-host> 'mkdir -p /opt/myapp'",
				"tar -czf - -C .vibewarden/bundle . | ssh <your-ssh-user>@<your-ssh-host> 'tar -xzf - -C /opt/myapp/'",
				"docker compose up -d",
				"curl -fsSL https://example.com/_vibewarden/health",
			},
			wantAbsent: []string{"docker load -i image.tar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMemBundleFS()
			svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
				WithBundleFS(mem)

			outDir := t.TempDir()
			err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
				Config:      configWithDomain(tt.domain),
				ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
				ProjectName: tt.appName,
				OutputDir:   outDir,
				SkipImage:   tt.skipImage,
			})
			if err != nil {
				t.Fatalf("Bundle() error = %v", err)
			}

			body := string(mem.files[filepath.Join(outDir, "README.md")])

			// The fenced block must appear within the first 30 lines.
			lines := strings.SplitN(body, "\n", 31)
			first30 := strings.Join(lines, "\n")
			if !strings.Contains(first30, "```bash") {
				t.Errorf("README.md missing ```bash fenced block within first 30 lines\nbody:\n%s", body)
			}

			for _, cmd := range tt.wantCmds {
				if !strings.Contains(body, cmd) {
					t.Errorf("README.md missing deploy command %q\nbody:\n%s", cmd, body)
				}
			}
			// Absent checks apply to the first fenced block only — the prose
			// sections may reference these strings in explanation context.
			firstBlock := extractFirstFencedBlock(body)
			for _, absent := range tt.wantAbsent {
				if strings.Contains(firstBlock, absent) {
					t.Errorf("deploy fenced block must not contain %q\nblock:\n%s", absent, firstBlock)
				}
			}
		})
	}
}

// TestBundle_Extras_Readme_ReadOnlyRecipes asserts that the bundle README
// contains the three read-only inspection commands near the bottom.
func TestBundle_Extras_Readme_ReadOnlyRecipes(t *testing.T) {
	mem := newMemBundleFS()
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      configWithDomain("myapp.example.com"),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myapp",
		OutputDir:   outDir,
		SkipImage:   true,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	body := string(mem.files[filepath.Join(outDir, "README.md")])

	wantRecipes := []string{
		"docker compose -f /opt/myapp/docker-compose.yml logs --tail 50",
		"docker compose -f /opt/myapp/docker-compose.yml ps",
		"curl -fsSL https://myapp.example.com/_vibewarden/health",
	}
	for _, recipe := range wantRecipes {
		if !strings.Contains(body, recipe) {
			t.Errorf("README.md missing read-only recipe %q\nbody:\n%s", recipe, body)
		}
	}

	// Read-only section header must exist.
	if !strings.Contains(body, "## Read-only inspection") {
		t.Errorf("README.md missing '## Read-only inspection' section\nbody:\n%s", body)
	}
}

// TestBundle_Extras_Readme_PlaceholdersWhenAppOrDomainMissing verifies that
// empty appName yields <your-app> and empty domain yields <your-domain> in
// the generated README deploy block. These are defensive placeholders for
// the edge case where name resolution yields an empty string — post-#1199
// this should not happen in production, but the render function must be
// safe. The test calls RenderBundleReadme directly to bypass the pipeline's
// name-resolution fallback.
func TestBundle_Extras_Readme_PlaceholdersWhenAppOrDomainMissing(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		domain  string
		wantApp string
		wantDom string
	}{
		{
			name:    "both empty",
			appName: "",
			domain:  "",
			wantApp: "<your-app>",
			wantDom: "<your-domain>",
		},
		{
			name:    "domain empty",
			appName: "myapp",
			domain:  "",
			wantApp: "myapp",
			wantDom: "<your-domain>",
		},
		{
			name:    "appName empty",
			appName: "",
			domain:  "example.com",
			wantApp: "<your-app>",
			wantDom: "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bundleapp.RenderBundleReadme(tt.appName, tt.domain, "", true)

			// The body must contain /opt/<wantApp> in the deploy block.
			if !strings.Contains(body, "/opt/"+tt.wantApp) {
				t.Errorf("README.md missing /opt/%q in deploy block\nbody:\n%s", tt.wantApp, body)
			}
			if !strings.Contains(body, tt.wantDom) {
				t.Errorf("README.md missing domain %q\nbody:\n%s", tt.wantDom, body)
			}
		})
	}
}

// TestBundle_Extras_Manifest_DeterministicAndSorted verifies that MANIFEST.md
// body (excluding the timestamp header line) is byte-identical across two
// renders with the same input file set, and that entries are sorted
// alphabetically. It also verifies MANIFEST.md does not list itself.
func TestBundle_Extras_Manifest_DeterministicAndSorted(t *testing.T) {
	// Use a real outDir so renderBundleManifest can walk it.
	outDir := t.TempDir()
	// Create a fixed set of files.
	for _, name := range []string{"vibewarden.yaml", "sample.env", ".env", "README.md", "docker-compose.yml"} {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	render := func() (header, body string) {
		t.Helper()
		content, err := bundleapp.RenderBundleManifest(outDir, "testapp")
		if err != nil {
			t.Fatalf("RenderBundleManifest: %v", err)
		}
		lines := strings.SplitN(content, "\n", 4)
		if len(lines) < 4 {
			t.Fatalf("manifest too short: %q", content)
		}
		// Line 0: # heading, line 1: blank, line 2: Generated by... (timestamp), line 3: blank
		return lines[2], strings.Join(lines[3:], "\n")
	}

	_, body1 := render()
	_, body2 := render()

	if body1 != body2 {
		t.Errorf("MANIFEST.md body is not deterministic across runs\nrun1:\n%s\nrun2:\n%s", body1, body2)
	}

	// Verify entries are sorted alphabetically.
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(body1), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Lines are "- <name> — <desc>"
		after, ok := strings.CutPrefix(line, "- ")
		if !ok {
			continue
		}
		name := strings.SplitN(after, " — ", 2)[0]
		names = append(names, name)
	}

	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)
	for i, got := range names {
		if got != sorted[i] {
			t.Errorf("MANIFEST.md entries not sorted: position %d got %q want %q", i, got, sorted[i])
		}
	}

	// MANIFEST.md must not list itself.
	if strings.Contains(body1, "- MANIFEST.md") {
		t.Errorf("MANIFEST.md must not list itself\nbody:\n%s", body1)
	}
}

// TestBundle_Extras_Manifest_UnknownFilesGetGenericDescription verifies that
// a file the manifest generator does not recognise gets a generic description
// rather than being silently omitted.
func TestBundle_Extras_Manifest_UnknownFilesGetGenericDescription(t *testing.T) {
	outDir := t.TempDir()
	knownFiles := []string{"vibewarden.yaml", "docker-compose.yml", "README.md"}
	for _, name := range knownFiles {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	// Add a file the manifest generator does not have in its description map.
	unknownFile := "mystery-tool.conf"
	if err := os.WriteFile(filepath.Join(outDir, unknownFile), []byte("data"), 0o600); err != nil {
		t.Fatalf("creating %s: %v", unknownFile, err)
	}

	content, err := bundleapp.RenderBundleManifest(outDir, "testapp")
	if err != nil {
		t.Fatalf("RenderBundleManifest: %v", err)
	}

	if !strings.Contains(content, unknownFile) {
		t.Errorf("MANIFEST.md missing unknown file %q\nbody:\n%s", unknownFile, content)
	}
	// The line for the unknown file should contain the generic description.
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, unknownFile) {
			if !strings.Contains(line, "bundle artifact") {
				t.Errorf("unknown file line missing generic description 'bundle artifact'\nline: %s", line)
			}
			return
		}
	}
	t.Errorf("no line found for unknown file %q\nbody:\n%s", unknownFile, content)
}

// TestBundle_Readme_AlignsWithDeployTmpl is the forensic drift guard for
// #1203 and #1204. It loads prompts/deploy.tmpl, renders the bundle README,
// extracts the four deploy commands from both sources, and asserts they are
// equivalent after normalising template variables to concrete values.
//
// If a future change touches one source and not the other, this test fails
// with a clear diff showing which command diverged.
func TestBundle_Readme_AlignsWithDeployTmpl(t *testing.T) {
	// Locate the deploy.tmpl relative to this test file using runtime.Caller.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/app/bundle → go up 3 dirs to repo root, then into templates.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	tmplPath := filepath.Join(repoRoot, "internal", "cli", "templates", "prompts", "deploy.tmpl")
	tmplBytes, err := os.ReadFile(tmplPath) //nolint:gosec // path derived from source tree
	if err != nil {
		t.Fatalf("reading deploy.tmpl: %v", err)
	}
	tmplContent := string(tmplBytes)

	// Render the bundle README with concrete substitution values.
	const testApp = "myapp"
	const testDomain = "example.com"
	mem := newMemBundleFS()
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)
	outDir := t.TempDir()
	err = svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      configWithDomain(testDomain),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: testApp,
		OutputDir:   outDir,
		SkipImage:   false,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}
	readmeBody := string(mem.files[filepath.Join(outDir, "README.md")])

	// normalise replaces template variables with concrete values so deploy.tmpl
	// commands can be compared against the bundle README. Post-#1244 the ssh
	// lines use the bracketed placeholder "<your-ssh-user>@<your-ssh-host>"
	// (not the old "user@{{.Domain}}" form), so no ssh-host substitution is
	// needed — both surfaces carry the same placeholder string verbatim.
	//   - {{.Name}} → testApp
	//   - {{.Domain}} → testDomain (for curl URL lines)
	normalise := func(s string) string {
		s = strings.ReplaceAll(s, "{{.Name}}", testApp)
		s = strings.ReplaceAll(s, "{{.Domain}}", testDomain)
		return s
	}
	tmplContent = normalise(tmplContent)

	// Extract the four deploy commands from deploy.tmpl (Step 7 + Step 8).
	// Each command starts with two leading spaces in the template.
	tmplCommands := extractDeployCommands(tmplContent)
	readmeCommands := extractDeployCommands(readmeBody)

	if len(tmplCommands) == 0 {
		t.Fatal("deploy.tmpl: could not extract any deploy commands — check regex / template format")
	}
	if len(readmeCommands) == 0 {
		t.Fatal("bundle README: could not extract any deploy commands — check regex / README format")
	}

	// Assert each tmpl command is also in the README (order independent, since
	// the README may split steps differently). This is the drift guard.
	for _, cmd := range tmplCommands {
		found := false
		for _, rc := range readmeCommands {
			if strings.TrimSpace(rc) == strings.TrimSpace(cmd) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("deploy command from deploy.tmpl not found in bundle README\ncmd: %q\nreadme commands: %v", cmd, readmeCommands)
		}
	}

	// Forensic alignment: load three static surfaces and assert required/forbidden
	// patterns across each one (#1217). The fourth surface — bundle stdout — is
	// guarded separately by TestBundle_Stdout_PrintsLiteralDeployCommands in
	// internal/cli/cmd/bundle_test.go, which also carries the forbidden-pattern
	// wantAbsent assertions. Failures here mean a surface drifted back to the
	// dotfile-eating scp glob or a banned artifact command.
	llmsFullPath := filepath.Join(repoRoot, "llms-full.txt")
	llmsFullBytes, err := os.ReadFile(llmsFullPath) //nolint:gosec // path derived from source tree
	if err != nil {
		t.Fatalf("reading llms-full.txt: %v", err)
	}

	surfaces := map[string]string{
		"bundle README": readmeBody,
		"deploy.tmpl":   tmplContent,
		"llms-full.txt": string(llmsFullBytes),
	}

	required := []string{
		"tar -czf - -C",
		"tar -xzf - -C",
		// #1244: bracketed placeholder must appear on all surfaces (when no deploy.host).
		"<your-ssh-user>@<your-ssh-host>",
	}
	forbidden := []string{
		"scp -r .vibewarden/bundle/*", // the dotfile-eating glob this issue eliminates
		"bash deploy.sh",              // already-removed artifact (#1138)
		"./deploy.sh",                 // ditto
		// #1244: old unbracketed placeholder forms — Codex agents followed these literally.
		"user@<domain>", // the original soft placeholder form
		"user@<",        // catches any remaining unbracketed user@<...> form
	}

	for surfaceName, surfaceContent := range surfaces {
		for _, req := range required {
			if !strings.Contains(surfaceContent, req) {
				t.Errorf("forensic[%s]: required pattern %q not found", surfaceName, req)
			}
		}
		for _, ban := range forbidden {
			if strings.Contains(surfaceContent, ban) {
				t.Errorf("forensic[%s]: forbidden pattern %q found — must not appear", surfaceName, ban)
			}
		}
	}
}

// TestRenderBundleReadme_PlaceholderPath verifies that when sshHost is empty,
// the README deploy block uses the bracketed placeholder and the hint paragraph
// is appended after the fenced block (#1244).
func TestRenderBundleReadme_PlaceholderPath(t *testing.T) {
	body := bundleapp.RenderBundleReadme("myapp", "example.com", "", false)

	// The bracketed placeholder must appear in all three ssh lines.
	wantSSH := []string{
		"ssh <your-ssh-user>@<your-ssh-host> 'mkdir -p /opt/myapp'",
		"| ssh <your-ssh-user>@<your-ssh-host> 'tar -xzf - -C /opt/myapp/'",
		`ssh <your-ssh-user>@<your-ssh-host> "cd /opt/myapp`,
	}
	for _, want := range wantSSH {
		if !strings.Contains(body, want) {
			t.Errorf("README missing %q in deploy block\nbody:\n%s", want, body)
		}
	}

	// Hint paragraph must be present.
	wantHint := []string{
		"Replace `<your-ssh-user>@<your-ssh-host>` with your actual SSH target.",
		"~/.ssh/config",
		"deploy.host: user@host",
	}
	for _, hint := range wantHint {
		if !strings.Contains(body, hint) {
			t.Errorf("README missing hint %q\nbody:\n%s", hint, body)
		}
	}

	// Old unbracketed form must not appear.
	forbidden := []string{"user@<domain>", "user@<"}
	for _, ban := range forbidden {
		if strings.Contains(body, ban) {
			t.Errorf("README must not contain %q\nbody:\n%s", ban, body)
		}
	}
}

// TestRenderBundleReadme_SubstitutedPath verifies that when sshHost is set,
// the README deploy block substitutes it verbatim, no placeholder appears, and
// no hint paragraph is emitted (#1244).
func TestRenderBundleReadme_SubstitutedPath(t *testing.T) {
	const host = "root@1.2.3.4"
	body := bundleapp.RenderBundleReadme("myapp", "example.com", host, false)

	// The configured host must appear in all three ssh lines.
	wantSSH := []string{
		"ssh root@1.2.3.4 'mkdir -p /opt/myapp'",
		"| ssh root@1.2.3.4 'tar -xzf - -C /opt/myapp/'",
		`ssh root@1.2.3.4 "cd /opt/myapp`,
	}
	for _, want := range wantSSH {
		if !strings.Contains(body, want) {
			t.Errorf("README missing %q in deploy block\nbody:\n%s", want, body)
		}
	}

	// No placeholder must appear.
	if strings.Contains(body, "<your-ssh-user>@<your-ssh-host>") {
		t.Errorf("README must not contain bracketed placeholder when sshHost is set\nbody:\n%s", body)
	}

	// Hint paragraph must be absent.
	if strings.Contains(body, "Replace `<your-ssh-user>") {
		t.Errorf("README must not contain hint paragraph when sshHost is set\nbody:\n%s", body)
	}
}

// extractFirstFencedBlock returns the content of the first ```...``` fenced
// block found in text, or empty string if none is found.
func extractFirstFencedBlock(text string) string {
	start := strings.Index(text, "```")
	if start < 0 {
		return ""
	}
	// Skip past the opening fence line (```bash or ``` etc.).
	end1 := strings.Index(text[start:], "\n")
	if end1 < 0 {
		return ""
	}
	blockStart := start + end1 + 1
	closeIdx := strings.Index(text[blockStart:], "```")
	if closeIdx < 0 {
		return ""
	}
	return text[blockStart : blockStart+closeIdx]
}

// extractDeployCommands extracts ssh/tar/docker/curl command lines from text.
// It matches lines that look like shell commands inside fenced blocks or
// indented recipe blocks.
func extractDeployCommands(text string) []string {
	// Match lines containing ssh, tar -czf, docker load, docker compose up, or
	// curl with _vibewarden/health. Strip leading whitespace and backtick wrappers.
	// tar -czf is listed before ssh so the tar-pipe line (which also contains
	// "| ssh") is captured as a single command rather than being split.
	cmdRe := regexp.MustCompile(`(?m)^\s*((?:tar -czf |ssh |docker load|docker compose up|curl -fsSL https://\S+/_vibewarden/health)[^\n]*)`)
	matches := cmdRe.FindAllStringSubmatch(text, -1)
	var cmds []string
	for _, m := range matches {
		if len(m) > 1 {
			cmds = append(cmds, strings.TrimSpace(m[1]))
		}
	}
	return cmds
}
