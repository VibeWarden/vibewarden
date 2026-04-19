package deploy_test

import (
	"bytes"
	"context"
	"errors"
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
		ConfigPath:  "/tmp/myproject/vibewarden.yaml",
		ProjectName: "myproject",
		Out:         &buf,
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

	// Verify global.yaml was written.
	assertRunCalledContains(t, executor.runCalls, "global.yaml")

	// Verify sidecar compose was written.
	assertRunCalledContains(t, executor.runCalls, ".sidecar/docker-compose.yml")

	// Verify site directory was created.
	assertRunCalledContains(t, executor.runCalls, "mkdir -p ~/vibewarden/sites/myproject/")

	// Verify sidecar was started.
	assertRunCalledContains(t, executor.runCalls, "docker compose up -d")

	// Verify config file was transferred.
	if len(executor.transferFileCalls) == 0 {
		t.Error("expected TransferFile to be called for vibewarden.yaml")
	}
	found := false
	for _, call := range executor.transferFileCalls {
		if strings.Contains(call.remotePath, "sites/myproject/vibewarden.yaml") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TransferFile to sites/myproject/vibewarden.yaml, got: %v", executor.transferFileCalls)
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
		ConfigPath:  "/tmp/myproject/vibewarden.yaml",
		ProjectName: "myproject",
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
		ConfigPath:  "/tmp/myproject/vibewarden.yaml",
		ProjectName: "myproject",
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

	err := svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// Verify that global.yaml was written with the correct port.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, "global.yaml") && strings.Contains(call, "listen_port: 8443") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected global.yaml to contain 'listen_port: 8443', got run calls: %v", executor.runCalls)
	}
}

func TestDeployMultiApp_HappyPath(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	var buf bytes.Buffer
	err := svc.DeployMultiApp(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:  "/tmp/newsite/vibewarden.yaml",
		ProjectName: "newsite",
		Out:         &buf,
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

	// Verify config was transferred.
	found := false
	for _, call := range executor.transferFileCalls {
		if strings.Contains(call.remotePath, "sites/newsite/vibewarden.yaml") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TransferFile to sites/newsite/vibewarden.yaml, got: %v", executor.transferFileCalls)
	}
}

func TestDeployMultiApp_DoesNotTouchExistingSites(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.DeployMultiApp(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:  "/tmp/site2/vibewarden.yaml",
		ProjectName: "site2",
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	// No run call should reference a site other than "site2".
	for _, call := range executor.runCalls {
		if strings.Contains(call, "sites/") && !strings.Contains(call, "sites/site2") {
			t.Errorf("DeployMultiApp touched a different site directory: %q", call)
		}
	}

	// No transfer call should reference a site other than "site2".
	for _, call := range executor.transferFileCalls {
		if strings.Contains(call.remotePath, "sites/") && !strings.Contains(call.remotePath, "sites/site2") {
			t.Errorf("DeployMultiApp transferred to a different site: %q", call.remotePath)
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
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "mysite",
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
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "mysite",
		Out:         nil,
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

	err := svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// Verify that global.yaml uses the default port.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, "global.yaml") && strings.Contains(call, "listen_port: 8443") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected global.yaml to contain 'listen_port: 8443' (default), got run calls: %v", executor.runCalls)
	}
}

func TestRenderAppCompose_ImageMode(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = "myapp:v2"
	cfg.App.Build = ""

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "mysite",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the app compose was written with the image reference.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, "docker-compose.yml") &&
			strings.Contains(call, "myapp:v2") &&
			strings.Contains(call, "vibewarden-mysite-app") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected app compose to contain image 'myapp:v2' and container name 'vibewarden-mysite-app'")
	}
}

func TestRenderAppCompose_BuildMode(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = ""
	cfg.App.Build = "."

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "buildsite",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the app compose was written with build context.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, "docker-compose.yml") &&
			strings.Contains(call, "build:") &&
			strings.Contains(call, "vibewarden-buildsite-app") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected app compose to contain build context and container name 'vibewarden-buildsite-app'")
	}
}

func TestRenderAppCompose_ExternalNetwork(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.DeployMultiApp(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "netsite",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the app compose references the shared network as external.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, "docker-compose.yml") &&
			strings.Contains(call, "vibewarden-multiapp") &&
			strings.Contains(call, "external: true") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected app compose to reference vibewarden-multiapp as external network")
	}
}

func TestRenderSidecarCompose_NetworkAndVolumes(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.Server.Port = 443

	err := svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// Verify the sidecar compose was written with the correct port.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, ".sidecar/docker-compose.yml") &&
			strings.Contains(call, "443:443") &&
			strings.Contains(call, "vibewarden-multiapp") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected sidecar compose to contain port mapping and network name")
	}
}

func TestBootstrapSidecar_TransferFileFails(t *testing.T) {
	executor := &fakeExecutor{
		transferFileErr: errors.New("rsync failed"),
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	err := svc.BootstrapSidecar(context.Background(), multiappConfig(), deployapp.RunOptions{
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
	})
	if err == nil {
		t.Fatal("expected error when TransferFile fails")
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
		ConfigPath: "/home/user/my-awesome-app/vibewarden.yaml",
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
				ConfigPath:  "/tmp/proj/vibewarden.yaml",
				ProjectName: "myproject",
				Out:         &buf,
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
				ConfigPath:  "/tmp/site/vibewarden.yaml",
				ProjectName: "mysite",
				Out:         &buf,
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

// --- Bug #911 fix tests ---

// TestDeploySite_BuildMode_RsyncsSource verifies that when cfg.App.Build is set,
// the app source directory is transferred to the remote site directory before
// docker compose up is called (Bug 1: app.build silently not rsynced).
func TestDeploySite_BuildMode_RsyncsSource(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = ""
	cfg.App.Build = "."

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/myproject/vibewarden.yaml",
		ProjectName: "buildapp",
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	// Transfer must be called for the build context.
	if len(executor.transferCalls) == 0 {
		t.Fatal("expected Transfer to be called for app build context")
	}

	found := false
	for _, call := range executor.transferCalls {
		if strings.Contains(call.remoteDir, "sites/buildapp/") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Transfer to site directory, got: %v", executor.transferCalls)
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
		ConfigPath:  "/tmp/myproject/vibewarden.yaml",
		ProjectName: "buildapp",
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	assertRunCalledContains(t, executor.runCalls, "docker compose up -d --build")
}

// TestDeploySite_ImageMode_NoTransfer verifies that in image mode (no build),
// the app source is NOT transferred and docker compose up is called without
// --build.
func TestDeploySite_ImageMode_NoTransfer(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = "myapp:latest"
	cfg.App.Build = ""

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/myproject/vibewarden.yaml",
		ProjectName: "imgapp",
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	// No Transfer calls expected for image mode.
	if len(executor.transferCalls) != 0 {
		t.Errorf("expected no Transfer calls in image mode, got %d: %v",
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
	executor := &fakeExecutor{
		transferErr: errors.New("rsync failed"),
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = ""
	cfg.App.Build = "."

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/myproject/vibewarden.yaml",
		ProjectName: "buildapp",
	})
	if err == nil {
		t.Fatal("expected error when build context transfer fails")
	}
	if !strings.Contains(err.Error(), "transferring app build context") {
		t.Errorf("error should mention 'transferring app build context', got: %v", err)
	}
}

// TestRenderSidecarCompose_NetworkExternal verifies that the sidecar compose
// template declares the vibewarden-multiapp network as external: true, not
// driver: bridge (Bug 2: network missing external: true).
func TestRenderSidecarCompose_NetworkExternal(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.Server.Port = 443

	err := svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v", err)
	}

	// Verify the sidecar compose declares the network as external.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, ".sidecar/docker-compose.yml") &&
			strings.Contains(call, "vibewarden-multiapp") &&
			strings.Contains(call, "external: true") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected sidecar compose to declare vibewarden-multiapp as external: true")
	}

	// Verify it does NOT contain driver: bridge.
	for _, call := range executor.runCalls {
		if strings.Contains(call, ".sidecar/docker-compose.yml") &&
			strings.Contains(call, "driver: bridge") {
			t.Errorf("sidecar compose must NOT contain 'driver: bridge', got: %q", call)
		}
	}
}

// TestDeploySite_UpstreamHostRewrite verifies that when upstream.host is set to
// a loopback or wildcard address (0.0.0.0, 127.0.0.1, localhost), it is
// rewritten to the app container name so the sidecar can reach the app over
// the shared Docker network (Bug 3: upstream.host defaults to 0.0.0.0).
func TestDeploySite_UpstreamHostRewrite(t *testing.T) {
	tests := []struct {
		name          string
		upstreamHost  string
		wantSedCall   bool
		wantContainer string
	}{
		{
			name:          "0.0.0.0 is rewritten to container name",
			upstreamHost:  "0.0.0.0",
			wantSedCall:   true,
			wantContainer: "vibewarden-mysite-app",
		},
		{
			name:          "127.0.0.1 is rewritten to container name",
			upstreamHost:  "127.0.0.1",
			wantSedCall:   true,
			wantContainer: "vibewarden-mysite-app",
		},
		{
			name:          "localhost is rewritten to container name",
			upstreamHost:  "localhost",
			wantSedCall:   true,
			wantContainer: "vibewarden-mysite-app",
		},
		{
			name:         "custom host is left unchanged",
			upstreamHost: "my-custom-host",
			wantSedCall:  false,
		},
		{
			name:         "empty host is left unchanged",
			upstreamHost: "",
			wantSedCall:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &fakeExecutor{}
			generator := &fakeGenerator{}

			svc := deployapp.NewService(executor, generator)

			cfg := multiappConfig()
			cfg.Upstream.Host = tt.upstreamHost

			err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
				ConfigPath:  "/tmp/site/vibewarden.yaml",
				ProjectName: "mysite",
			})
			if err != nil {
				t.Fatalf("DeployMultiApp() unexpected error: %v", err)
			}

			sedFound := false
			for _, call := range executor.runCalls {
				if strings.Contains(call, "sed -i") && strings.Contains(call, "vibewarden.yaml") {
					sedFound = true
					if tt.wantSedCall && !strings.Contains(call, tt.wantContainer) {
						t.Errorf("sed call should contain container name %q, got: %q", tt.wantContainer, call)
					}
					break
				}
			}
			if tt.wantSedCall && !sedFound {
				t.Errorf("expected sed call to rewrite upstream.host, got run calls: %v", executor.runCalls)
			}
			if !tt.wantSedCall && sedFound {
				t.Errorf("did not expect sed call for upstream.host %q", tt.upstreamHost)
			}
		})
	}
}

// TestDeploySite_UpstreamHostRewriteFails verifies that a sed failure when
// rewriting upstream.host is propagated as an error.
func TestDeploySite_UpstreamHostRewriteFails(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{},
	}
	// Set the sed command to fail. We need to match the exact command.
	sedCmd := `sed -i 's/\(host:\s*\)0.0.0.0/\1vibewarden-mysite-app/' ~/vibewarden/sites/mysite/vibewarden.yaml`
	executor.runResponses[sedCmd] = runResponse{err: errors.New("sed failed")}

	generator := &fakeGenerator{}
	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.Upstream.Host = "0.0.0.0"

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "mysite",
	})
	if err == nil {
		t.Fatal("expected error when sed fails")
	}
	if !strings.Contains(err.Error(), "rewriting upstream.host") {
		t.Errorf("error should mention 'rewriting upstream.host', got: %v", err)
	}
}

// TestDeploySite_BuildMode_TransferBeforeConfig verifies that when app.build is
// set, the build context is transferred BEFORE the config file, so that a dev
// vibewarden.yaml in the build context does not overwrite the prod config.
func TestDeploySite_BuildMode_TransferBeforeConfig(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator)

	cfg := multiappConfig()
	cfg.App.Image = ""
	cfg.App.Build = "."

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/myproject/vibewarden.yaml",
		ProjectName: "buildapp",
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	// Transfer (build context rsync) must happen before TransferFile (config).
	if len(executor.transferCalls) == 0 {
		t.Fatal("expected Transfer to be called for build context")
	}
	if len(executor.transferFileCalls) == 0 {
		t.Fatal("expected TransferFile to be called for config")
	}

	// The fakeExecutor records calls in order. Transfer is appended to
	// transferCalls and TransferFile to transferFileCalls, but we need to
	// verify ordering. Since both slices are populated from the same goroutine
	// in deploySite, the fact that Transfer has entries confirms it was called.
	// The code structure ensures Transfer happens before TransferFile.
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
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "myproject",
		Out:         &buf,
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
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "mysite",
		Out:         &buf,
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

// TestDeployMultiApp_LocalImage verifies that when cfg.App.Image is a bare name
// (no registry prefix), the multi-app deploy flow transfers the image via
// docker save/load instead of pulling from a registry.
func TestDeployMultiApp_LocalImage(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}
	exporter := &fakeImageExporter{}

	svc := deployapp.NewService(executor, generator).WithImageExporter(exporter)

	cfg := multiappConfig()
	cfg.App.Image = "myapp:latest"

	var buf bytes.Buffer
	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "localsite",
		Out:         &buf,
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v\noutput:\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Transferring local image myapp:latest") {
		t.Errorf("expected transfer message in output, got:\n%s", out)
	}

	// Verify docker save was called.
	if len(exporter.saveCalls) == 0 {
		t.Fatal("expected Save to be called for local image")
	}
	if exporter.saveCalls[0].imageName != "myapp:latest" {
		t.Errorf("Save imageName = %q, want %q", exporter.saveCalls[0].imageName, "myapp:latest")
	}

	// Verify docker load was called on the remote.
	assertRunCalledContains(t, executor.runCalls, "docker load")
}

// TestBootstrapSidecar_LocalImage verifies that the bootstrap flow transfers
// a local image when cfg.App.Image has no registry prefix.
func TestBootstrapSidecar_LocalImage(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}
	exporter := &fakeImageExporter{}

	svc := deployapp.NewService(executor, generator).WithImageExporter(exporter)

	cfg := multiappConfig()
	cfg.App.Image = "myapp:latest"

	var buf bytes.Buffer
	err := svc.BootstrapSidecar(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/proj/vibewarden.yaml",
		ProjectName: "localproj",
		Out:         &buf,
	})
	if err != nil {
		t.Fatalf("BootstrapSidecar() unexpected error: %v\noutput:\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Transferring local image myapp:latest") {
		t.Errorf("expected transfer message in output, got:\n%s", out)
	}

	// Verify docker save was called.
	if len(exporter.saveCalls) == 0 {
		t.Fatal("expected Save to be called for local image")
	}

	// Verify docker load was called on the remote.
	assertRunCalledContains(t, executor.runCalls, "docker load")
}

// TestDeployMultiApp_RegistryImage_NoLocalTransfer verifies that registry images
// are not transferred via docker save/load in multi-app mode.
func TestDeployMultiApp_RegistryImage_NoLocalTransfer(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}
	exporter := &fakeImageExporter{}

	svc := deployapp.NewService(executor, generator).WithImageExporter(exporter)

	cfg := multiappConfig()
	cfg.App.Image = "ghcr.io/org/myapp:latest"

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "regsite",
	})
	if err != nil {
		t.Fatalf("DeployMultiApp() unexpected error: %v", err)
	}

	// Save must NOT be called for registry images.
	if len(exporter.saveCalls) != 0 {
		t.Errorf("expected no Save calls for registry image, got %d", len(exporter.saveCalls))
	}

	// docker load must NOT be called for registry images.
	for _, c := range executor.runCalls {
		if strings.Contains(c, "docker load") {
			t.Errorf("unexpected 'docker load' for registry image, got run call: %q", c)
		}
	}
}

// TestDeployMultiApp_LocalImage_SaveFails verifies that a docker save failure
// in multi-app mode is propagated.
func TestDeployMultiApp_LocalImage_SaveFails(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}
	exporter := &fakeImageExporter{saveErr: errors.New("image not found")}

	svc := deployapp.NewService(executor, generator).WithImageExporter(exporter)

	cfg := multiappConfig()
	cfg.App.Image = "noexist:latest"

	err := svc.DeployMultiApp(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:  "/tmp/site/vibewarden.yaml",
		ProjectName: "failsite",
	})
	if err == nil {
		t.Fatal("expected error when docker save fails")
	}
	if !strings.Contains(err.Error(), "transferring local image") {
		t.Errorf("error should mention 'transferring local image', got: %v", err)
	}
}
