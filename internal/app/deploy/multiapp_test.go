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
