package deploy_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// captureGenerator is a test double that captures the last GeneratorInput
// passed to Generate, so tests can inspect what config was fed to the template.
type captureGenerator struct {
	lastInput ports.GeneratorInput
	err       error
}

func (g *captureGenerator) Generate(_ context.Context, input ports.GeneratorInput, _ string) error {
	g.lastInput = input
	return g.err
}

// TestArtifact_DeployCompose_ContextIsLocal verifies that the generated
// docker-compose.yml in a single-site deploy bundle uses build context "."
// (relative to the bundle directory) instead of "../../." which only works
// for the local dev layout (.vibewarden/generated/ is two levels deep).
//
// Regression test for #952.
func TestArtifact_DeployCompose_ContextIsLocal(t *testing.T) {
	projDir := t.TempDir()
	outputDir := t.TempDir()

	// Write a minimal vibewarden.yaml with app.build set.
	baseYAML := `server:
  port: 8443
upstream:
  host: "0.0.0.0"
  port: 3000
app:
  build: "."
`
	basePath := filepath.Join(projDir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Build: "."},
	}

	gen := &captureGenerator{}
	svc := deployapp.NewService(&fakeExecutor{}, gen)

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  basePath,
		ProjectName: "myapp",
		MultiSite:   false,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	// The generator input's TemplateData must have DeployMode = true so the
	// template renders build context as "." instead of "../../.".
	if gen.lastInput.TemplateData == nil {
		t.Fatal("generator was not called")
	}
	inputCfg, ok := gen.lastInput.TemplateData.(*config.Config)
	if !ok {
		t.Fatalf("TemplateData is %T, want *config.Config", gen.lastInput.TemplateData)
	}
	if !inputCfg.DeployMode {
		t.Error("deploy bundle must set DeployMode = true so the template renders 'context: .' instead of 'context: ../../.'")
	}
	if inputCfg.App.Build != "." {
		t.Errorf("deploy bundle App.Build = %q, want %q", inputCfg.App.Build, ".")
	}
}

// TestArtifact_Bundle_MergesProductionOverlay verifies that Bundle() reads
// vibewarden.production.yaml and deep-merges it on top of the base config.
// The bundled vibewarden.yaml must contain production override values.
//
// Regression test for #953.
func TestArtifact_Bundle_MergesProductionOverlay(t *testing.T) {
	projDir := t.TempDir()
	outputDir := t.TempDir()

	baseYAML := `server:
  port: 8443
upstream:
  host: "0.0.0.0"
  port: 3000
tls:
  enabled: true
  provider: self-signed
app:
  image: "myapp:latest"
`
	basePath := filepath.Join(projDir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	prodYAML := `server:
  port: 443
tls:
  enabled: true
  provider: letsencrypt
  domain: test.example.com
`
	prodPath := filepath.Join(projDir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod config: %v", err)
	}

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
		TLS:      config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: "test.example.com"},
	}

	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{})

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:         cfg,
		ConfigPath:     basePath,
		ProdConfigPath: prodPath,
		ProjectName:    "myapp",
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
	if !strings.Contains(s, "provider: letsencrypt") {
		t.Errorf("expected 'provider: letsencrypt' in merged config, got:\n%s", s)
	}
	if !strings.Contains(s, "port: 443") {
		t.Errorf("expected 'port: 443' in merged config, got:\n%s", s)
	}
	if !strings.Contains(s, "domain: test.example.com") {
		t.Errorf("expected 'domain: test.example.com' in merged config, got:\n%s", s)
	}
}

// TestArtifact_Bundle_ResolvesUpstreamHost verifies that when the upstream host
// is a local address (0.0.0.0), Bundle() resolves it to the Docker container
// name for multi-site mode, not the original local address.
func TestArtifact_Bundle_ResolvesUpstreamHost(t *testing.T) {
	projDir := t.TempDir()
	outputDir := t.TempDir()

	baseYAML := `upstream:
  host: "0.0.0.0"
  port: 3000
app:
  image: "myapp:latest"
`
	basePath := filepath.Join(projDir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	cfg := &config.Config{
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{})

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  basePath,
		ProjectName: "myapp",
		MultiSite:   true,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "sites", "myapp", "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading bundled config: %v", err)
	}

	s := string(data)
	if strings.Contains(s, `host: "0.0.0.0"`) || strings.Contains(s, "host: 0.0.0.0") {
		t.Errorf("upstream.host should be resolved, not '0.0.0.0', got:\n%s", s)
	}
	if !strings.Contains(s, "host: vibewarden-myapp-app") {
		t.Errorf("expected 'host: vibewarden-myapp-app' in bundled config, got:\n%s", s)
	}
}

// TestArtifact_SidecarCompose_ContainsDNS verifies that the generated sidecar
// compose file includes explicit DNS servers (1.1.1.1, 8.8.8.8) so that
// DNS resolution works on hosts using systemd-resolved (which binds to
// 127.0.0.53 and is unreachable from inside Docker containers).
//
// Regression test for #955.
func TestArtifact_SidecarCompose_ContainsDNS(t *testing.T) {
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{})

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 443},
	}

	err := svc.BundleSidecar(context.Background(), cfg, outputDir)
	if err != nil {
		t.Fatalf("BundleSidecar() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, ".sidecar", "docker-compose.yml"))
	if err != nil {
		t.Fatalf("reading sidecar compose: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, "dns:") {
		t.Errorf("expected 'dns:' section in sidecar compose, got:\n%s", s)
	}
	if !strings.Contains(s, "1.1.1.1") {
		t.Errorf("expected '1.1.1.1' in sidecar compose DNS, got:\n%s", s)
	}
	if !strings.Contains(s, "8.8.8.8") {
		t.Errorf("expected '8.8.8.8' in sidecar compose DNS, got:\n%s", s)
	}
}

// TestArtifact_MergeYAML_PreservesFieldNames verifies that deep-merging YAML
// configs preserves underscore field names like rate_limit and security_headers
// instead of mangling them (e.g. "ratelimit").
func TestArtifact_MergeYAML_PreservesFieldNames(t *testing.T) {
	baseYAML := `rate_limit:
  enabled: true
  burst: 20
security_headers:
  enabled: true
`
	overrideYAML := `rate_limit:
  burst: 50
`
	merged, err := deployapp.MergeConfigYAML([]byte(baseYAML), []byte(overrideYAML))
	if err != nil {
		t.Fatalf("MergeConfigYAML() error = %v", err)
	}

	data, err := deployapp.MarshalYAMLMap(merged)
	if err != nil {
		t.Fatalf("MarshalYAMLMap() error = %v", err)
	}

	s := string(data)
	if !strings.Contains(s, "rate_limit:") {
		t.Errorf("expected 'rate_limit:' in output, got:\n%s", s)
	}
	if strings.Contains(s, "ratelimit:") {
		t.Errorf("found 'ratelimit:' (without underscore) in output, expected 'rate_limit:', got:\n%s", s)
	}
	if !strings.Contains(s, "security_headers:") {
		t.Errorf("expected 'security_headers:' in output, got:\n%s", s)
	}
	if strings.Contains(s, "securityheaders:") {
		t.Errorf("found 'securityheaders:' (without underscore) in output, got:\n%s", s)
	}
}
