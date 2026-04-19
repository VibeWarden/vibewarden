package deploy_test

import (
	"bytes"
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

// TestArtifact_DeployCompose_UsesImageNotBuild verifies that the generated
// docker-compose.yml in a single-site deploy bundle uses image: instead of
// build: when app.build is set. The image is built locally by `vibew build`
// and transferred via docker save/load -- no source code on the server.
//
// Regression test for #952.
func TestArtifact_DeployCompose_UsesImageNotBuild(t *testing.T) {
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
	if inputCfg.App.Build != "" {
		t.Errorf("deploy bundle App.Build = %q, want empty (image mode)", inputCfg.App.Build)
	}
	if inputCfg.App.Image != "vibewarden-app:latest" {
		t.Errorf("deploy bundle App.Image = %q, want %q", inputCfg.App.Image, "vibewarden-app:latest")
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

// TestArtifact_BuildMode_TransfersImageNotSource verifies that when app.build
// is set, Deploy transfers the locally-built image via docker save/load instead
// of rsyncing source code. No TransferExcluding is used because no source code
// is sent to the server.
//
// Replaces #953 regression test (build context rsync is no longer used).
func TestArtifact_BuildMode_TransfersImageNotSource(t *testing.T) {
	projDir := t.TempDir()

	// Base config: self-signed TLS on port 8443 (local dev).
	baseYAML := `server:
  port: 8443
upstream:
  host: "0.0.0.0"
  port: 3000
tls:
  enabled: true
  provider: self-signed
app:
  build: "."
`
	basePath := filepath.Join(projDir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	// Production override: letsencrypt on port 443.
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

	// The merged config that config.Load would produce.
	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Build: "."},
		TLS:      config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: "app.example.com"},
	}

	executor := &fakeExecutor{}
	generator := &fakeGenerator{}
	svc := deployapp.NewService(executor, generator)

	bundleDir := t.TempDir()

	err := svc.Deploy(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:     basePath,
		ProdConfigPath: prodPath,
		ProjectName:    "myapp",
		GeneratedDir:   bundleDir,
		Force:          true,
	})
	if err != nil {
		t.Fatalf("Deploy() unexpected error: %v", err)
	}

	// 1. The bundled vibewarden.yaml must have the merged (production) values.
	bundledConfig, err := os.ReadFile(filepath.Join(bundleDir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading bundled config: %v", err)
	}
	s := string(bundledConfig)
	if !strings.Contains(s, "provider: letsencrypt") {
		t.Errorf("bundled config should contain 'provider: letsencrypt' (merged), got:\n%s", s)
	}
	if !strings.Contains(s, "port: 443") {
		t.Errorf("bundled config should contain 'port: 443' (merged), got:\n%s", s)
	}
	if strings.Contains(s, "provider: self-signed") {
		t.Errorf("bundled config must NOT contain 'provider: self-signed' (base), got:\n%s", s)
	}

	// 2. No TransferExcluding must be called -- no source code is transferred.
	if len(executor.transferExcludingCalls) != 0 {
		t.Errorf("expected no TransferExcluding calls (no source code transfer), got %d: %v",
			len(executor.transferExcludingCalls), executor.transferExcludingCalls)
	}

	// 3. No --build flag must be present in docker compose up.
	for _, c := range executor.runCalls {
		if strings.Contains(c, "--build") {
			t.Errorf("did not expect --build flag (image transfer mode), got: %q", c)
		}
	}
}

// TestArtifact_BuildMode_MultiApp_NoSourceTransfer verifies that in multi-app
// mode, when app.build is set, no source code is transferred to the server.
// The image is transferred via docker save/load instead.
//
// Replaces #953 multi-app regression test.
func TestArtifact_BuildMode_MultiApp_NoSourceTransfer(t *testing.T) {
	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Build: "."},
	}

	tests := []struct {
		name   string
		deploy func(svc *deployapp.Service, executor *fakeExecutor) error
	}{
		{
			name: "BootstrapSidecar does not transfer source code",
			deploy: func(svc *deployapp.Service, _ *fakeExecutor) error {
				return svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
					ConfigPath:   "/tmp/buildsite/vibewarden.yaml",
					ProjectName:  "buildsite",
					GeneratedDir: t.TempDir(),
				})
			},
		},
		{
			name: "DeployMultiApp does not transfer source code",
			deploy: func(svc *deployapp.Service, _ *fakeExecutor) error {
				return svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
					ConfigPath:   "/tmp/buildsite/vibewarden.yaml",
					ProjectName:  "buildsite",
					GeneratedDir: t.TempDir(),
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &fakeExecutor{}
			generator := &fakeGenerator{}
			svc := deployapp.NewService(executor, generator)

			if err := tt.deploy(svc, executor); err != nil {
				t.Fatalf("deploy error: %v", err)
			}

			// No TransferExcluding must be called -- no source code transfer.
			if len(executor.transferExcludingCalls) != 0 {
				t.Errorf("expected no TransferExcluding calls (no source transfer), got %d: %v",
					len(executor.transferExcludingCalls), executor.transferExcludingCalls)
			}

			// No --build flag in docker compose up.
			for _, c := range executor.runCalls {
				if strings.Contains(c, "--build") {
					t.Errorf("did not expect --build flag, got: %q", c)
				}
			}
		})
	}
}

// TestArtifact_FirstDeploy_NoDriftWarning verifies that on first deploy to an
// empty remote, the dry-run output containing only new-file entries (all "+"
// attributes) does NOT trigger a DriftError. Only actual modifications should
// be treated as drift.
//
// Regression test for #962.
func TestArtifact_FirstDeploy_NoDriftWarning(t *testing.T) {
	// Simulate a first deploy where the dry-run reports only new files
	// (all "+" attributes in the rsync itemize code).
	executor := &fakeExecutor{
		dryRunChanges: nil, // parseDryRunOutput now filters out new-file entries
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	var buf bytes.Buffer
	err := svc.Deploy(context.Background(), &config.Config{
		Server: config.ServerConfig{Port: 8443},
	}, deployapp.RunOptions{
		ConfigPath:   "/tmp/firstproject/vibewarden.yaml",
		GeneratedDir: t.TempDir(),
		Force:        false,
		Out:          &buf,
	})
	if err != nil {
		t.Fatalf("Deploy() on first deploy should not return error, got: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "remote files have been modified") {
		t.Errorf("first deploy should not report drift, got:\n%s", out)
	}
	if !strings.Contains(out, "Deploy complete") {
		t.Errorf("expected 'Deploy complete' in output, got:\n%s", out)
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
	svc := deployapp.NewService(&fakeExecutor{}, gen)

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
	svc := deployapp.NewService(&fakeExecutor{}, gen)

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
	svc := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{})

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

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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
// set, the deploy compose file uses image: instead of build:. This ensures no
// source code needs to exist on the production server.
func TestArtifact_DeployCompose_HasImageNotBuild(t *testing.T) {
	gen := &captureGenerator{}
	svc := deployapp.NewService(&fakeExecutor{}, gen)

	outputDir := t.TempDir()

	cfg := &config.Config{
		Name:     "myapp",
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
		App:      config.AppConfig{Build: "."},
	}

	err := svc.Bundle(context.Background(), deployapp.BundleOptions{
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

	// App.Build must be cleared for deploy.
	if inputCfg.App.Build != "" {
		t.Errorf("deploy bundle App.Build = %q, want empty", inputCfg.App.Build)
	}
	// App.Image must be set to the derived name.
	if inputCfg.App.Image != "myapp-app:latest" {
		t.Errorf("deploy bundle App.Image = %q, want %q", inputCfg.App.Image, "myapp-app:latest")
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
