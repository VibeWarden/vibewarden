package deploy_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestBundle_SingleSite_ProducesCorrectLayout(t *testing.T) {
	generator := &fakeGenerator{}
	svc := deployapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8443},
	}

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Port: 3000},
		App:      config.AppConfig{Build: "."},
	}

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

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
	err = svc.Bundle(context.Background(), deployapp.BundleOptions{
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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

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

	// Write production override.
	prodYAML := `server:
  port: 443
tls:
  enabled: true
  provider: letsencrypt
  domain: "example.com"
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

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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
}

func TestBundle_WithEnv_OutputsToEnvSubdir(t *testing.T) {
	generator := &fakeGenerator{}
	svc := deployapp.NewService(&fakeExecutor{}, generator)

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

	err = svc.Bundle(context.Background(), deployapp.BundleOptions{
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
	svc := deployapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "my-backend-service", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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
