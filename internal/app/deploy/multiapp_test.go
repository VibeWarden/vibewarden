package deploy_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	"github.com/vibewarden/vibewarden/internal/config"
)

// multiappConfig returns a Config suitable for multi-app deploy tests.
func multiappConfig() *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}
}

// multiappTLSConfig returns a Config with TLS enabled for multi-app deploy tests.
func multiappTLSConfig() *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
		TLS:      config.TLSConfig{Enabled: true},
	}
}

func TestBootstrapSidecar_HappyPath(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	var buf bytes.Buffer
	err := svc.BootstrapSidecar(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/myproject/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: t.TempDir(),
		Out:          &buf,
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v\noutput:\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Bootstrap complete") {
		t.Errorf("expected 'Bootstrap complete' in output, got:\n%s", out)
	}

	// Verify directory creation was called.
	assertRunCalledContains(t, executor.runCalls, "mkdir -p ~/vibewarden/.sidecar/")

	// Verify shared Docker network creation was called.
	assertRunCalledContains(t, executor.runCalls, "docker network create vibewarden-multiapp")

	// Verify site directory was created.
	assertRunCalledContains(t, executor.runCalls, "mkdir -p ~/vibewarden/.sidecar/ ~/vibewarden/sites/myproject/")

	// Verify sidecar was started.
	assertRunCalledContains(t, executor.runCalls, "docker compose up -d")

	// Verify Transfer was called for both sidecar and site bundles.
	if len(executor.transferCalls) < 2 {
		t.Errorf("expected at least 2 Transfer calls (sidecar + site), got %d: %v",
			len(executor.transferCalls), executor.transferCalls)
	}
}

func TestBootstrapSidecar_DirectoryCreationFails(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"mkdir -p ~/vibewarden/.sidecar/ ~/vibewarden/sites/myproject/": {
				err: errors.New("permission denied"),
			},
		},
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.BootstrapSidecar(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/myproject/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when directory creation fails")
	}
	if !strings.Contains(err.Error(), "creating directory layout") {
		t.Errorf("error should mention 'creating directory layout', got: %v", err)
	}
}

func TestBootstrapSidecar_CreatesCorrectLayout(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.BootstrapSidecar(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/myproject/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// Check that the mkdir command creates both .sidecar and site directories.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, "mkdir -p") &&
			strings.Contains(call, ".sidecar") &&
			strings.Contains(call, "sites/myproject") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected mkdir to create both .sidecar and sites/<project>, got: %v", executor.runCalls)
	}
}

func TestBootstrapSidecar_WritesGlobalYAML(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.Server.Port = 8443

	bundleDir := t.TempDir()
	err := svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: bundleDir,
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// Verify that global.yaml was written locally in the bundle with the correct port.
	globalYAML, err := os.ReadFile(filepath.Join(bundleDir, ".sidecar", "global.yaml"))
	if err != nil {
		t.Fatalf("reading bundled global.yaml: %v", err)
	}
	if !strings.Contains(string(globalYAML), "listen_port: 8443") {
		t.Errorf("expected global.yaml to contain 'listen_port: 8443', got:\n%s", string(globalYAML))
	}
}

func TestDeployMultiApp_HappyPath(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	var buf bytes.Buffer
	err := svc.DeployMultiApp(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/newsite/vibewarden.yaml",
		ProjectName:  "newsite",
		GeneratedDir: t.TempDir(),
		Out:          &buf,
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v\noutput:\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Site deployed") {
		t.Errorf("expected 'Site deployed' in output, got:\n%s", out)
	}

	// Verify site directory was created.
	assertRunCalledContains(t, executor.runCalls, "mkdir -p ~/vibewarden/sites/newsite/")

	// Verify sidecar was restarted (not a full up).
	assertRunCalledContains(t, executor.runCalls, "docker compose restart vibewarden")

	// Verify Transfer was called for the site bundle.
	if len(executor.transferCalls) < 1 {
		t.Errorf("expected at least 1 Transfer call for site bundle, got %d", len(executor.transferCalls))
	}
}

func TestDeployMultiApp_DoesNotTouchExistingSites(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.DeployMultiApp(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/site2/vibewarden.yaml",
		ProjectName:  "site2",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	// No run call should reference a site other than "site2" (ignoring sidecar commands).
	for _, call := range executor.runCalls {
		if strings.Contains(call, "sites/") && !strings.Contains(call, "sites/site2") {
			t.Errorf("DeployMultiApp touched a different site directory: %q", call)
		}
	}
}

func TestDeployMultiApp_RestartFails(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"cd ~/vibewarden/.sidecar/ && docker compose restart vibewarden": {
				err: errors.New("container not found"),
			},
		},
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.DeployMultiApp(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/site/vibewarden.yaml",
		ProjectName:  "mysite",
		GeneratedDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when sidecar restart fails")
	}
	if !strings.Contains(err.Error(), "restarting sidecar") {
		t.Errorf("error should mention 'restarting sidecar', got: %v", err)
	}
}

func TestDeployMultiApp_NilOut(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	// Should not panic with nil Out.
	err := svc.DeployMultiApp(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/site/vibewarden.yaml",
		ProjectName:  "mysite",
		GeneratedDir: t.TempDir(),
		Out:          nil,
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}
}

func TestBootstrapSidecar_DefaultPort(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.Server.Port = 0 // should default to 8443

	bundleDir := t.TempDir()
	err := svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: bundleDir,
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// Verify that global.yaml uses the default port.
	globalYAML, err := os.ReadFile(filepath.Join(bundleDir, ".sidecar", "global.yaml"))
	if err != nil {
		t.Fatalf("reading bundled global.yaml: %v", err)
	}
	if !strings.Contains(string(globalYAML), "listen_port: 8443") {
		t.Errorf("expected global.yaml to contain 'listen_port: 8443' (default), got:\n%s", string(globalYAML))
	}
}

// TestDeployMultiApp_BundleWritesResolvedUpstreamHost verifies that the
// bundle's vibewarden.yaml contains the resolved upstream.host (container name)
// rather than the user's original local address. This replaces the old sed test.
func TestDeployMultiApp_BundleWritesResolvedUpstreamHost(t *testing.T) {
	tests := []struct {
		name         string
		upstreamHost string
		wantHost     string
	}{
		{
			name:         "0.0.0.0 is resolved to container name",
			upstreamHost: "0.0.0.0",
			wantHost:     "vibewarden-mysite-app",
		},
		{
			name:         "127.0.0.1 is resolved to container name",
			upstreamHost: "127.0.0.1",
			wantHost:     "vibewarden-mysite-app",
		},
		{
			name:         "localhost is resolved to container name",
			upstreamHost: "localhost",
			wantHost:     "vibewarden-mysite-app",
		},
		{
			name:         "custom host is preserved",
			upstreamHost: "my-custom-host",
			wantHost:     "my-custom-host",
		},
		{
			name:         "empty host is preserved",
			upstreamHost: "",
			wantHost:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &fakeExecutor{}
			generator := &fakeGenerator{}

			svc := deployapp.NewService(executor, generator)

			cfg := multiappConfig()
			cfg.Upstream.Host = tt.upstreamHost

			bundleDir := t.TempDir()
			err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
				ConfigPath:   "/tmp/site/vibewarden.yaml",
				ProjectName:  "mysite",
				GeneratedDir: bundleDir,
			})
			if err != nil {
				t.Fatalf("DeployMultiApp() unexpected error: %v", err)
			}

			// Read the bundled vibewarden.yaml and verify upstream.host.
			configPath := filepath.Join(bundleDir, "sites", "mysite", "vibewarden.yaml")
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("reading bundled config: %v", err)
			}

			content := string(data)
			if tt.wantHost != "" {
				if !strings.Contains(content, "host: "+tt.wantHost) {
					t.Errorf("bundled config should contain 'host: %s', got:\n%s", tt.wantHost, content)
				}
			}

			// Verify no sed was called.
			for _, call := range executor.runCalls {
				if strings.Contains(call, "sed") {
					t.Errorf("no sed should be called, but found: %q", call)
				}
			}
		})
	}
}

// TestDeployMultiApp_NoSedCalled verifies that no sed calls are made during
// multi-app deployment. Config resolution happens locally via the bundle.
func TestDeployMultiApp_NoSedCalled(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.Upstream.Host = "0.0.0.0"

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/site/vibewarden.yaml",
		ProjectName:  "mysite",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	for _, call := range executor.runCalls {
		if strings.Contains(call, "sed") {
			t.Errorf("expected no sed calls, but found: %q", call)
		}
	}
}

func TestRenderAppCompose_ImageMode(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = "myapp:v2"
	cfg.App.Build = ""

	bundleDir := t.TempDir()
	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/site/vibewarden.yaml",
		ProjectName:  "mysite",
		GeneratedDir: bundleDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the app compose was written to the bundle with the image reference.
	composePath := filepath.Join(bundleDir, "sites", "mysite", "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("reading bundled compose: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "myapp:v2") {
		t.Errorf("expected compose to contain 'myapp:v2', got:\n%s", content)
	}
	if !strings.Contains(content, "vibewarden-mysite-app") {
		t.Errorf("expected compose to contain 'vibewarden-mysite-app', got:\n%s", content)
	}
}

func TestRenderAppCompose_BuildMode(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = ""
	cfg.App.Build = "."

	bundleDir := t.TempDir()
	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/site/vibewarden.yaml",
		ProjectName:  "buildsite",
		GeneratedDir: bundleDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the app compose was written with build context.
	composePath := filepath.Join(bundleDir, "sites", "buildsite", "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("reading bundled compose: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "build:") {
		t.Errorf("expected compose to contain 'build:', got:\n%s", content)
	}
	if !strings.Contains(content, "vibewarden-buildsite-app") {
		t.Errorf("expected compose to contain 'vibewarden-buildsite-app', got:\n%s", content)
	}
}

func TestRenderAppCompose_ExternalNetwork(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	bundleDir := t.TempDir()
	err := svc.DeployMultiApp(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/site/vibewarden.yaml",
		ProjectName:  "netsite",
		GeneratedDir: bundleDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the app compose references the shared network as external.
	composePath := filepath.Join(bundleDir, "sites", "netsite", "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("reading bundled compose: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "vibewarden-multiapp") {
		t.Errorf("expected compose to reference 'vibewarden-multiapp', got:\n%s", content)
	}
	if !strings.Contains(content, "external: true") {
		t.Errorf("expected compose to declare network as 'external: true', got:\n%s", content)
	}
}

func TestRenderSidecarCompose_NetworkAndVolumes(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.Server.Port = 443

	bundleDir := t.TempDir()
	err := svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: bundleDir,
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// Verify the sidecar compose was written with the correct port.
	composePath := filepath.Join(bundleDir, ".sidecar", "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("reading bundled sidecar compose: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "443:443") {
		t.Errorf("expected sidecar compose to contain '443:443', got:\n%s", content)
	}
	if !strings.Contains(content, "vibewarden-multiapp") {
		t.Errorf("expected sidecar compose to reference 'vibewarden-multiapp', got:\n%s", content)
	}
}

func TestBootstrapSidecar_TransferFails(t *testing.T) {
	executor := &fakeExecutor{
		transferErr: errors.New("rsync failed"),
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.BootstrapSidecar(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when Transfer fails")
	}
	if !strings.Contains(err.Error(), "transferring") {
		t.Errorf("error should mention 'transferring', got: %v", err)
	}
}

func TestBootstrapSidecar_DeriveProjectName(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.BootstrapSidecar(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/home/user/my-awesome-app/vibewarden.yaml",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// Verify the project name was derived from the config path.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, "sites/my-awesome-app") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected site directory to use derived name 'my-awesome-app', got run calls: %v", executor.runCalls)
	}
}

func TestBootstrapSidecar_TLSHealthCheck(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		wantCurlCmd string
		wantScheme  string
	}{
		{
			name:        "TLS disabled uses HTTP with -sf",
			cfg:         multiappConfig(),
			wantCurlCmd: "curl -sf http://localhost:443/_vibewarden/health",
			wantScheme:  "http://",
		},
		{
			name:        "TLS enabled uses HTTPS with -sfk",
			cfg:         multiappTLSConfig(),
			wantCurlCmd: "curl -sfk https://localhost:443/_vibewarden/health",
			wantScheme:  "https://",
		},
		{
			name: "TLS enabled with custom port uses HTTPS with -sfk",
			cfg: &config.Config{
				Server:   config.ServerConfig{Port: 8443},
				Upstream: config.UpstreamConfig{Port: 3000},
				App:      config.AppConfig{Image: "myapp:latest"},
				TLS:      config.TLSConfig{Enabled: true},
			},
			wantCurlCmd: "curl -sfk https://localhost:8443/_vibewarden/health",
			wantScheme:  "https://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &fakeExecutor{}
			generator := &fakeGenerator{}

			svc := deployapp.NewService(executor, generator)

			var buf bytes.Buffer
			err := svc.BootstrapSidecar(context.Background(), tt.cfg, deployapp.RunOptions{
				ConfigPath:   "/tmp/proj/vibewarden.yaml",
				ProjectName:  "myproject",
				GeneratedDir: t.TempDir(),
				Out:          &buf,
			})
			if err != nil {
				t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
			}

			// Verify the correct curl command was executed.
			assertRunCalled(t, executor.runCalls, tt.wantCurlCmd)

			// Verify the output mentions the correct scheme in the health check URL.
			out := buf.String()
			if !strings.Contains(out, tt.wantScheme) {
				t.Errorf("expected output to contain %q, got:\n%s", tt.wantScheme, out)
			}
		})
	}
}

func TestDeployMultiApp_TLSHealthCheck(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		wantCurlCmd string
		wantScheme  string
	}{
		{
			name:        "TLS disabled uses HTTP with -sf",
			cfg:         multiappConfig(),
			wantCurlCmd: "curl -sf http://localhost:443/_vibewarden/health",
			wantScheme:  "http://",
		},
		{
			name:        "TLS enabled uses HTTPS with -sfk",
			cfg:         multiappTLSConfig(),
			wantCurlCmd: "curl -sfk https://localhost:443/_vibewarden/health",
			wantScheme:  "https://",
		},
		{
			name: "TLS enabled with custom port uses HTTPS with -sfk",
			cfg: &config.Config{
				Server:   config.ServerConfig{Port: 8443},
				Upstream: config.UpstreamConfig{Port: 3000},
				App:      config.AppConfig{Image: "myapp:latest"},
				TLS:      config.TLSConfig{Enabled: true},
			},
			wantCurlCmd: "curl -sfk https://localhost:8443/_vibewarden/health",
			wantScheme:  "https://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &fakeExecutor{}
			generator := &fakeGenerator{}

			svc := deployapp.NewService(executor, generator)

			var buf bytes.Buffer
			err := svc.DeployMultiApp(context.Background(), tt.cfg, deployapp.RunOptions{
				ConfigPath:   "/tmp/site/vibewarden.yaml",
				ProjectName:  "mysite",
				GeneratedDir: t.TempDir(),
				Out:          &buf,
			})
			if err != nil {
				t.Fatalf("DeployMultiApp() unexpected error: %v", err)
			}

			// Verify the correct curl command was executed.
			assertRunCalled(t, executor.runCalls, tt.wantCurlCmd)

			// Verify the output mentions the correct scheme in the health check URL.
			out := buf.String()
			if !strings.Contains(out, tt.wantScheme) {
				t.Errorf("expected output to contain %q, got:\n%s", tt.wantScheme, out)
			}
		})
	}
}

// TestDeploySite_BuildMode_RsyncsSource verifies that when cfg.App.Build is set,
// the app source directory is transferred to the remote site directory.
func TestDeploySite_BuildMode_RsyncsSource(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = ""
	cfg.App.Build = "."

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/myproject/vibewarden.yaml",
		ProjectName:  "buildapp",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	// Transfer must be called for the site bundle; TransferExcluding for the
	// build context.
	if len(executor.transferCalls) < 1 {
		t.Fatalf("expected at least 1 Transfer call (site bundle), got %d: %v",
			len(executor.transferCalls), executor.transferCalls)
	}
	if len(executor.transferExcludingCalls) < 1 {
		t.Fatalf("expected at least 1 TransferExcluding call (build context), got %d: %v",
			len(executor.transferExcludingCalls), executor.transferExcludingCalls)
	}

	found := false
	for _, call := range executor.transferExcludingCalls {
		if strings.Contains(call.remoteDir, "sites/buildapp/") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TransferExcluding to site directory, got: %v", executor.transferExcludingCalls)
	}
}

// TestDeploySite_BuildMode_UsesComposeUpBuild verifies that when cfg.App.Build
// is set, docker compose up is called with --build.
func TestDeploySite_BuildMode_UsesComposeUpBuild(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = ""
	cfg.App.Build = "."

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/myproject/vibewarden.yaml",
		ProjectName:  "buildapp",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	assertRunCalledContains(t, executor.runCalls, "docker compose up -d --build")
}

// TestDeploySite_ImageMode_NoBuildContext verifies that in image mode (no build),
// docker compose up is called without --build and no extra transfer is made for
// build context.
func TestDeploySite_ImageMode_NoBuildContext(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = "myapp:latest"
	cfg.App.Build = ""

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/myproject/vibewarden.yaml",
		ProjectName:  "imgapp",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	// Only the site bundle Transfer (no build context).
	if len(executor.transferCalls) != 1 {
		t.Errorf("expected 1 Transfer call in image mode (site bundle only), got %d: %v",
			len(executor.transferCalls), executor.transferCalls)
	}

	// docker compose up -d must be called without --build.
	for _, c := range executor.runCalls {
		if strings.Contains(c, "--build") {
			t.Errorf("did not expect --build flag in image mode, got: %q", c)
		}
	}
}

// TestDeploySite_BuildMode_TransferFails verifies that a failure to rsync the
// app build context is propagated as an error.
func TestDeploySite_BuildMode_TransferFails(t *testing.T) {
	callCount := 0
	mockExec := &mockRunExecutor{
		runFn: func(_ string) (string, error) { return "", nil },
	}
	// Fail on the second Transfer call (build context).
	failingExec := &buildContextFailExecutor{
		mockRunExecutor: mockExec,
		failOnTransfer:  2,
		transferErr:     errors.New("rsync failed"),
		callCount:       &callCount,
	}

	generator := &fakeGenerator{}
	svc := deployapp.NewService(failingExec, generator)

	cfg := multiappConfig()
	cfg.App.Image = ""
	cfg.App.Build = "."

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/myproject/vibewarden.yaml",
		ProjectName:  "buildapp",
		GeneratedDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when build context transfer fails")
	}
	if !strings.Contains(err.Error(), "transferring app build context") {
		t.Errorf("error should mention 'transferring app build context', got: %v", err)
	}
}

// TestRenderSidecarCompose_NetworkExternal verifies that the sidecar compose
// template declares the vibewarden-multiapp network as external: true.
func TestRenderSidecarCompose_NetworkExternal(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.Server.Port = 443

	bundleDir := t.TempDir()
	err := svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: bundleDir,
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	composePath := filepath.Join(bundleDir, ".sidecar", "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("reading bundled sidecar compose: %v", err)
	}
	content := string(data)

	// Verify the sidecar compose declares the network as external.
	if !strings.Contains(content, "vibewarden-multiapp") ||
		!strings.Contains(content, "external: true") {
		t.Errorf("expected sidecar compose to declare vibewarden-multiapp as external: true, got:\n%s", content)
	}

	// Verify it does NOT contain driver: bridge.
	if strings.Contains(content, "driver: bridge") {
		t.Errorf("sidecar compose must NOT contain 'driver: bridge', got:\n%s", content)
	}
}

// TestBootstrapSidecar_HealthCheckFails verifies that BootstrapSidecar returns
// ErrHealthCheck when the sidecar health check times out.
func TestBootstrapSidecar_HealthCheckFails(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"curl -sf http://localhost:443/_vibewarden/health": {err: errors.New("exit status 7")},
		},
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { cancel() }()

	var buf bytes.Buffer
	err := svc.BootstrapSidecar(ctx, multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: t.TempDir(),
		Out:          &buf,
	})
	if !errors.Is(err, deployapp.ErrHealthCheck) {
		t.Fatalf("BootstrapSidecar() should return ErrHealthCheck, got: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "health check failed") {
		t.Errorf("expected 'health check failed' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "vibew doctor") {
		t.Errorf("expected 'vibew doctor' suggestion in output, got:\n%s", out)
	}
	// "Bootstrap complete" should NOT appear when health check fails.
	if strings.Contains(out, "Bootstrap complete.") {
		t.Errorf("'Bootstrap complete.' should NOT appear when health check fails, got:\n%s", out)
	}
}

// TestDeployMultiApp_HealthCheckFails verifies that DeployMultiApp returns
// ErrHealthCheck when the sidecar health check times out.
func TestDeployMultiApp_HealthCheckFails(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"curl -sf http://localhost:443/_vibewarden/health": {err: errors.New("exit status 7")},
		},
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { cancel() }()

	var buf bytes.Buffer
	err := svc.DeployMultiApp(ctx, multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/site/vibewarden.yaml",
		ProjectName:  "mysite",
		GeneratedDir: t.TempDir(),
		Out:          &buf,
	})
	if !errors.Is(err, deployapp.ErrHealthCheck) {
		t.Fatalf("DeployMultiApp() should return ErrHealthCheck, got: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "health check failed") {
		t.Errorf("expected 'health check failed' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "vibew doctor") {
		t.Errorf("expected 'vibew doctor' suggestion in output, got:\n%s", out)
	}
	// "Site deployed." should NOT appear when health check fails.
	if strings.Contains(out, "Site deployed.") {
		t.Errorf("'Site deployed.' should NOT appear when health check fails, got:\n%s", out)
	}
}

// TestBootstrapSidecar_NoTransferFileOrWriteRemoteFile verifies that
// BootstrapSidecar no longer uses TransferFile or writeRemoteFile -- everything
// is bundled locally and rsynced.
func TestBootstrapSidecar_NoTransferFileOrWriteRemoteFile(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.BootstrapSidecar(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		ProjectName:  "myproject",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// TransferFile should not be called.
	if len(executor.transferFileCalls) != 0 {
		t.Errorf("expected no TransferFile calls, got %d: %v",
			len(executor.transferFileCalls), executor.transferFileCalls)
	}

	// No heredoc/cat commands for file writing.
	for _, call := range executor.runCalls {
		if strings.Contains(call, "VIBEWARDEN_EOF") || strings.Contains(call, "cat >") {
			t.Errorf("expected no writeRemoteFile calls (heredoc), but found: %q", call)
		}
	}
}
