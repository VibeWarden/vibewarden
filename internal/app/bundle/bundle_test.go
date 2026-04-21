package bundle_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestBundle_SingleSite_ProducesCorrectLayout(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
		MultiSite:   false,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	// vibewarden.yaml must exist in the output directory.
	configPath := filepath.Join(outputDir, "vibewarden.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected vibewarden.yaml in bundle output")
	}
}

func TestBundle_SingleSite_WritesResolvedUpstreamHost(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
		MultiSite:   false,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	// The bundled config should have upstream.host resolved to "app".
	data, err := os.ReadFile(filepath.Join(outputDir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading bundled config: %v", err)
	}
	if !strings.Contains(string(data), "host: app") {
		t.Errorf("expected resolved 'host: app' in bundled config, got:\n%s", string(data))
	}
}

func TestBundle_MultiSite_ProducesCorrectLayout(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "blog",
		MultiSite:   true,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	// sites/<project>/vibewarden.yaml must exist.
	configPath := filepath.Join(outputDir, "sites", "blog", "vibewarden.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected sites/blog/vibewarden.yaml in bundle output")
	}

	// sites/<project>/docker-compose.yml must exist.
	composePath := filepath.Join(outputDir, "sites", "blog", "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Error("expected sites/blog/docker-compose.yml in bundle output")
	}
}

func TestBundle_MultiSite_WritesResolvedUpstreamHost(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "api",
		MultiSite:   true,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "sites", "api", "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading bundled config: %v", err)
	}
	if !strings.Contains(string(data), "host: vibewarden-api-app") {
		t.Errorf("expected resolved 'host: vibewarden-api-app' in bundled config, got:\n%s", string(data))
	}
}

func TestBundle_SingleSite_GeneratorError(t *testing.T) {
	generator := &fakeGenerator{err: errors.New("template broken")}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8443},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
		MultiSite:   false,
		OutputDir:   outputDir,
	})
	if err == nil {
		t.Fatal("expected error when generator fails")
	}
	if !strings.Contains(err.Error(), "generating config files") {
		t.Errorf("error should mention 'generating config files', got: %v", err)
	}
}

func TestBundleSidecar_ProducesCorrectLayout(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 443},
	}

	err := svc.BundleSidecar(context.Background(), cfg, outputDir)
	if err != nil {
		t.Fatalf("BundleSidecar() error = %v", err)
	}

	// .sidecar/global.yaml must exist.
	globalPath := filepath.Join(outputDir, ".sidecar", "global.yaml")
	if _, err := os.Stat(globalPath); os.IsNotExist(err) {
		t.Error("expected .sidecar/global.yaml in bundle output")
	}

	// .sidecar/docker-compose.yml must exist.
	composePath := filepath.Join(outputDir, ".sidecar", "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Error("expected .sidecar/docker-compose.yml in bundle output")
	}

	// Verify global.yaml content.
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("reading global.yaml: %v", err)
	}
	if !strings.Contains(string(data), "listen_port: 443") {
		t.Errorf("expected 'listen_port: 443' in global.yaml, got:\n%s", string(data))
	}
}

func TestBundleSidecar_DefaultPort(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 0}, // should default to 8443
	}

	err := svc.BundleSidecar(context.Background(), cfg, outputDir)
	if err != nil {
		t.Fatalf("BundleSidecar() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, ".sidecar", "global.yaml"))
	if err != nil {
		t.Fatalf("reading global.yaml: %v", err)
	}
	if !strings.Contains(string(data), "listen_port: 8443") {
		t.Errorf("expected 'listen_port: 8443' in global.yaml, got:\n%s", string(data))
	}
}

func TestBundle_MultiSite_AppBuildUsesImage(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Port: 3000},
		App:      config.AppConfig{Build: "."},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "buildsite",
		MultiSite:   true,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "sites", "buildsite", "docker-compose.yml"))
	if err != nil {
		t.Fatalf("reading compose: %v", err)
	}
	content := string(data)
	// Deploy compose must use image: instead of build: because the image
	// is built locally and transferred via docker save/load.
	if strings.Contains(content, "build:") {
		t.Errorf("expected no 'build:' in deploy compose (image transfer mode), got:\n%s", content)
	}
	if !strings.Contains(content, "image:") {
		t.Errorf("expected 'image:' in deploy compose, got:\n%s", content)
	}
	if !strings.Contains(content, "vibewarden-app:latest") {
		t.Errorf("expected 'vibewarden-app:latest' in deploy compose, got:\n%s", content)
	}
}

func TestBundle_DefaultOutputDir(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	// Use t.TempDir() and chdir into it so the default output directory is
	// created inside the temp dir, not the current working directory.
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting cwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	// Call Bundle with empty OutputDir -- should default to .vibewarden/deploy/production/.
	err = svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
		MultiSite:   false,
		OutputDir:   "", // empty -> defaults
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	// Verify the default directory was created with the environment subdirectory.
	defaultPath := filepath.Join(".vibewarden", "deploy", "production", "vibewarden.yaml")
	if _, err := os.Stat(defaultPath); os.IsNotExist(err) {
		t.Errorf("expected %s to exist with default OutputDir", defaultPath)
	}
}

func TestBundle_WithProdOverride_MergesCorrectly(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()
	projDir := t.TempDir()

	// Write base config.
	baseYAML := `server:
  port: 8443
upstream:
  host: "0.0.0.0"
  port: 3000
tls:
  enabled: true
  provider: self-signed
rate_limit:
  enabled: true
  burst: 20
app:
  image: "myapp:latest"
`
	basePath := filepath.Join(projDir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	// Write production override. tls.email and tls.acme_ca are the ADR-082
	// regression guards — fields added by ADR-078/ADR-079 that the old
	// hand-written allow-list silently dropped (#1053).
	prodYAML := `server:
  port: 443
tls:
  enabled: true
  provider: letsencrypt
  domain: "example.com"
  email: "ops@example.com"
  acme_ca: "https://acme.zerossl.com/v2/DV90"
`
	prodPath := filepath.Join(projDir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod config: %v", err)
	}

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
		TLS:      config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: "example.com"},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:         cfg,
		ConfigPath:     basePath,
		ProdConfigPath: prodPath,
		ProjectName:    "myproject",
		MultiSite:      false,
		OutputDir:      outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading bundled config: %v", err)
	}

	s := string(data)

	// Production override values should be present.
	if !strings.Contains(s, "port: 443") {
		t.Errorf("expected production port 443 in merged config, got:\n%s", s)
	}
	if !strings.Contains(s, "provider: letsencrypt") {
		t.Errorf("expected letsencrypt provider in merged config, got:\n%s", s)
	}
	if !strings.Contains(s, "domain: example.com") {
		t.Errorf("expected domain in merged config, got:\n%s", s)
	}
	if !strings.Contains(s, "email: ops@example.com") {
		t.Errorf("expected tls.email preserved from prod override (ADR-082 regression), got:\n%s", s)
	}
	if !strings.Contains(s, "https://acme.zerossl.com/v2/DV90") {
		t.Errorf("expected tls.acme_ca preserved from prod override (ADR-082 regression), got:\n%s", s)
	}

	// Base config values not overridden should survive.
	if !strings.Contains(s, "rate_limit:") {
		t.Errorf("expected rate_limit section preserved from base, got:\n%s", s)
	}
	if !strings.Contains(s, "burst: 20") {
		t.Errorf("expected burst: 20 preserved from base, got:\n%s", s)
	}

	// upstream.host should be resolved (0.0.0.0 -> app for single-site with image).
	if !strings.Contains(s, "host: app") {
		t.Errorf("expected upstream.host resolved to 'app', got:\n%s", s)
	}

	// Runtime parity: LoadMergedConfig must return the same values in the
	// typed Config that the bundle YAML now carries. This is the core #1053
	// regression — the struct overlay was dropping these fields even though
	// the YAML overlay carried them.
	mergedCfg, err := bundleapp.LoadMergedConfig(basePath, prodPath)
	if err != nil {
		t.Fatalf("LoadMergedConfig() error = %v", err)
	}
	if mergedCfg.TLS.Email != "ops@example.com" {
		t.Errorf("merged cfg TLS.Email = %q, want %q", mergedCfg.TLS.Email, "ops@example.com")
	}
	if mergedCfg.TLS.ACMECA != "https://acme.zerossl.com/v2/DV90" {
		t.Errorf("merged cfg TLS.ACMECA = %q, want %q", mergedCfg.TLS.ACMECA, "https://acme.zerossl.com/v2/DV90")
	}
}

func TestBundle_WithEnv_OutputsToEnvSubdir(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting cwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err = svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ProjectName: "myproject",
		MultiSite:   false,
		Env:         "staging",
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	stagingPath := filepath.Join(".vibewarden", "deploy", "staging", "vibewarden.yaml")
	if _, err := os.Stat(stagingPath); os.IsNotExist(err) {
		t.Errorf("expected %s to exist for env=staging", stagingPath)
	}
}

func TestBundle_PreservesNonLocalUpstreamHost(t *testing.T) {
	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "my-backend-service", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
		MultiSite:   false,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading bundled config: %v", err)
	}
	if !strings.Contains(string(data), "host: my-backend-service") {
		t.Errorf("expected non-local host to be preserved, got:\n%s", string(data))
	}
}

// TestBundle_SingleSite_ConfigPathStatError verifies that a non-ErrNotExist
// stat failure on ConfigPath surfaces as an error instead of silently falling
// back to the in-memory cfg. This is the guard for the reviewer finding on
// PR #1056: a permission-denied base file in production used to be
// indistinguishable from a missing file.
func TestBundle_SingleSite_ConfigPathStatError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("cannot simulate permission denied when running as root")
	}

	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	projDir := t.TempDir()
	// Place a real config file inside an unreadable directory so os.Stat
	// fails with permission-denied (not IsNotExist).
	lockedDir := filepath.Join(projDir, "locked")
	if err := os.Mkdir(lockedDir, 0o700); err != nil {
		t.Fatalf("mkdir locked: %v", err)
	}
	configPath := filepath.Join(lockedDir, "vibewarden.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	if err := os.Chmod(lockedDir, 0o000); err != nil {
		t.Fatalf("chmod locked: %v", err)
	}
	// Restore directory permissions at cleanup so t.TempDir's own cleanup can
	// recursively remove files inside. 0o700 is required because directories
	// need the execute bit to be traversable.
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o700) }) //nolint:gosec // test-only dir perms

	outputDir := t.TempDir()
	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  configPath,
		ProjectName: "myproject",
		MultiSite:   false,
		OutputDir:   outputDir,
	})
	if err == nil {
		t.Fatal("Bundle() expected error for unreadable config path, got nil")
	}
	if !strings.Contains(err.Error(), "stat config") {
		t.Errorf("expected 'stat config' in error, got: %v", err)
	}
}
