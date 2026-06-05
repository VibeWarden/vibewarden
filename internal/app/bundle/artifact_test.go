package bundle_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
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

// TestArtifact_DeployCompose_UsesImageNotBuild verifies that the generated
// docker-compose.yml in a single-site bundle uses image: instead of build:
// when app.build is set. The image is built locally by `vibew build` and
// shipped via image.tar in the bundle — no source code on the server.
//
// Regression test for #952.
func TestArtifact_DeployCompose_UsesImageNotBuild(t *testing.T) {
	projDir := t.TempDir()
	outputDir := t.TempDir()

	// Write a minimal vibewarden.yaml with app.build and name: set.
	// Since v0.18.2 (#1199) vibew init always writes name:, so bundles use
	// cfg.Name (via ComposeProjectName) rather than the last-resort "vibewarden"
	// fallback. The expected App.Image is therefore <name>-app:latest.
	baseYAML := `name: myapp
server:
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
		Name:     "myapp",
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Build: "."},
	}

	gen := &captureGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, gen)

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  basePath,
		ProjectName: "myapp",
		MultiSite:   false,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	// The generator input's TemplateData must have DeployMode = true.
	if gen.lastInput.TemplateData == nil {
		t.Fatal("generator was not called")
	}
	inputCfg, ok := gen.lastInput.TemplateData.(*config.Config)
	if !ok {
		t.Fatalf("TemplateData is %T, want *config.Config", gen.lastInput.TemplateData)
	}
	if !inputCfg.DeployMode {
		t.Error("deploy bundle must set DeployMode = true")
	}
	// App.Build must be cleared and App.Image set for deploy compose.
	// Image is derived from cfg.Name via ComposeProjectName().
	if inputCfg.App.Build != "" {
		t.Errorf("deploy bundle App.Build = %q, want empty (image mode)", inputCfg.App.Build)
	}
	if inputCfg.App.Image != "myapp-app:latest" {
		t.Errorf("deploy bundle App.Image = %q, want %q", inputCfg.App.Image, "myapp-app:latest")
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

	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{})

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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

	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{})

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{})

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
	merged, err := bundleapp.MergeConfigYAML([]byte(baseYAML), []byte(overrideYAML))
	if err != nil {
		t.Fatalf("MergeConfigYAML() error = %v", err)
	}

	data, err := bundleapp.MarshalYAMLMap(merged)
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

// TestArtifact_Bundle_GeneratorWriteOverwrittenByMerge is a regression test
// for #953. It verifies that the merged vibewarden.yaml is written AFTER the
// generator runs, so a generator that copies the unmerged base config into the
// output directory does not clobber the production overlay.
//
// The test uses a fake generator that writes a sentinel value to
// vibewarden.yaml, then verifies the final file has the merged (production)
// content, not the sentinel.
func TestArtifact_Bundle_GeneratorWriteOverwrittenByMerge(t *testing.T) {
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
  domain: app.example.com
`
	prodPath := filepath.Join(projDir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod config: %v", err)
	}

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
		TLS:      config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: "app.example.com"},
	}

	// sentinelGenerator writes a sentinel value to vibewarden.yaml in the output
	// directory, simulating the real generator which copies the original config.
	gen := &sentinelGenerator{sentinel: "provider: SENTINEL_NOT_MERGED"}
	svc := bundleapp.NewService(&fakeExecutor{}, gen)

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
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

	// The sentinel must NOT appear -- the merged write must overwrite it.
	if strings.Contains(s, "SENTINEL_NOT_MERGED") {
		t.Errorf("bundled vibewarden.yaml contains sentinel from generator; merged write did not overwrite it:\n%s", s)
	}

	// The production values must be present.
	if !strings.Contains(s, "provider: letsencrypt") {
		t.Errorf("expected 'provider: letsencrypt' in merged config, got:\n%s", s)
	}
	if !strings.Contains(s, "port: 443") {
		t.Errorf("expected 'port: 443' in merged config, got:\n%s", s)
	}
	if !strings.Contains(s, "domain: app.example.com") {
		t.Errorf("expected 'domain: app.example.com' in merged config, got:\n%s", s)
	}
}

// TestArtifact_AppEnvironment_RendersInCompose verifies that app.environment
// variables are rendered in the generated docker-compose.yml for single-site mode.
func TestArtifact_AppEnvironment_RendersInCompose(t *testing.T) {
	gen := &captureGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, gen)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
		App: config.AppConfig{
			Image: "myapp:latest",
			Environment: map[string]string{
				"DATABASE_URL": "postgres://user:pass@db:5432/myapp",
				"LOG_LEVEL":    "info",
			},
		},
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

	// Verify the captured generator input has environment variables.
	if gen.lastInput.TemplateData == nil {
		t.Fatal("generator was not called")
	}
	inputCfg, ok := gen.lastInput.TemplateData.(*config.Config)
	if !ok {
		t.Fatalf("TemplateData is %T, want *config.Config", gen.lastInput.TemplateData)
	}
	if len(inputCfg.App.Environment) != 2 {
		t.Errorf("expected 2 environment variables, got %d", len(inputCfg.App.Environment))
	}
	if inputCfg.App.Environment["DATABASE_URL"] != "postgres://user:pass@db:5432/myapp" {
		t.Errorf("expected DATABASE_URL, got: %v", inputCfg.App.Environment)
	}
}

// TestArtifact_AppEnvironment_RendersInAppCompose verifies that app.environment
// variables are rendered in the per-app compose file for multi-site mode.
func TestArtifact_AppEnvironment_RendersInAppCompose(t *testing.T) {
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{})

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
		App: config.AppConfig{
			Image: "myapp:latest",
			Environment: map[string]string{
				"DATABASE_URL": "postgres://user:pass@db:5432/myapp",
				"REDIS_URL":    "redis://redis:6379",
			},
		},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "envsite",
		MultiSite:   true,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "sites", "envsite", "docker-compose.yml"))
	if err != nil {
		t.Fatalf("reading bundled compose: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "environment:") {
		t.Errorf("expected 'environment:' in compose, got:\n%s", content)
	}
	if !strings.Contains(content, "DATABASE_URL=postgres://user:pass@db:5432/myapp") {
		t.Errorf("expected DATABASE_URL in compose, got:\n%s", content)
	}
	if !strings.Contains(content, "REDIS_URL=redis://redis:6379") {
		t.Errorf("expected REDIS_URL in compose, got:\n%s", content)
	}
}

// TestArtifact_DeployCompose_HasImageNotBuild verifies that when app.build is
// set, the bundled compose file uses image: instead of build:. This ensures no
// source code needs to exist on the production server.
func TestArtifact_DeployCompose_HasImageNotBuild(t *testing.T) {
	gen := &captureGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, gen)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Name:     "myapp",
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
		App:      config.AppConfig{Build: "."},
	}

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      cfg,
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myapp",
		MultiSite:   false,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	if gen.lastInput.TemplateData == nil {
		t.Fatal("generator was not called")
	}
	inputCfg, ok := gen.lastInput.TemplateData.(*config.Config)
	if !ok {
		t.Fatalf("TemplateData is %T, want *config.Config", gen.lastInput.TemplateData)
	}

	// App.Build must be cleared for bundle output.
	if inputCfg.App.Build != "" {
		t.Errorf("bundle App.Build = %q, want empty", inputCfg.App.Build)
	}
	// App.Image must be set to the derived name.
	if inputCfg.App.Image != "myapp-app:latest" {
		t.Errorf("bundle App.Image = %q, want %q", inputCfg.App.Image, "myapp-app:latest")
	}
}

// TestArtifact_SidecarCompose_ReleaseVersion_PinsImage verifies that when the
// Service is configured with a release version, the sidecar docker-compose.yml
// contains a pinned image tag and no pull_policy (ADR-106).
func TestArtifact_SidecarCompose_ReleaseVersion_PinsImage(t *testing.T) {
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).WithVersion("0.20.0")

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 443},
	}

	if err := svc.BundleSidecar(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("BundleSidecar() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, ".sidecar", "docker-compose.yml"))
	if err != nil {
		t.Fatalf("reading sidecar compose: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, "image: ghcr.io/vibewarden/vibewarden:0.20.0") {
		t.Errorf("release sidecar compose must pin image to :0.20.0; got:\n%s", s)
	}
	if strings.Contains(s, "pull_policy:") {
		t.Errorf("release sidecar compose must NOT contain pull_policy (pinned tag is immutable); got:\n%s", s)
	}
}

// TestArtifact_SidecarCompose_DevVersion_UsesLatestWithAlways verifies that
// when the Service is configured with a dev version, the sidecar
// docker-compose.yml uses :latest and pull_policy: always (ADR-106).
func TestArtifact_SidecarCompose_DevVersion_UsesLatestWithAlways(t *testing.T) {
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).WithVersion("dev")

	outputDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 443},
	}

	if err := svc.BundleSidecar(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("BundleSidecar() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, ".sidecar", "docker-compose.yml"))
	if err != nil {
		t.Fatalf("reading sidecar compose: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, "image: ghcr.io/vibewarden/vibewarden:latest") {
		t.Errorf("dev sidecar compose must use :latest; got:\n%s", s)
	}
	if !strings.Contains(s, "pull_policy: always") {
		t.Errorf("dev sidecar compose must contain pull_policy: always; got:\n%s", s)
	}
}

// sentinelGenerator is a test double for ports.ConfigGenerator that writes a
// sentinel value to vibewarden.yaml in the output directory, simulating the
// real generator's behaviour of copying the base config into the output.
type sentinelGenerator struct {
	sentinel string
}

func (g *sentinelGenerator) Generate(_ context.Context, _ ports.GeneratorInput, outputDir string) error {
	dir := outputDir
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(g.sentinel), 0o600)
}
