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

func TestBundle_MultiSite_AppBuildInCompose(t *testing.T) {
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
	if !strings.Contains(content, "build:") {
		t.Errorf("expected 'build:' in compose for build mode, got:\n%s", content)
	}
	if !strings.Contains(content, "context: .") {
		t.Errorf("expected 'context: .' in compose for build mode, got:\n%s", content)
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

	// Call Bundle with empty OutputDir -- should default to .vibewarden/deploy.
	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
		MultiSite:   false,
		OutputDir:   "", // empty -> defaults
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	// Verify the default directory was created.
	if _, err := os.Stat(".vibewarden/deploy/vibewarden.yaml"); os.IsNotExist(err) {
		t.Error("expected .vibewarden/deploy/vibewarden.yaml to exist with default OutputDir")
	}

	// Clean up the default directory.
	if err := os.RemoveAll(".vibewarden/deploy"); err != nil {
		t.Logf("cleanup warning: %v", err)
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
