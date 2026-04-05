package deploy_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	"github.com/vibewarden/vibewarden/internal/config"
)

// fakeExecutor is a test double for ports.RemoteExecutor.
type fakeExecutor struct {
	// runResponses maps the command string to (output, error).
	runResponses map[string]runResponse
	// transferErr is returned by every Transfer call if set.
	transferErr error
	// runCalls records every Run invocation for assertions.
	runCalls []string
	// transferCalls records every Transfer invocation for assertions.
	transferCalls []transferCall
}

type runResponse struct {
	output string
	err    error
}

type transferCall struct {
	localDir    string
	remoteDir   string
	deleteExtra bool
}

func (f *fakeExecutor) Run(_ context.Context, cmd string) (string, error) {
	f.runCalls = append(f.runCalls, cmd)
	if r, ok := f.runResponses[cmd]; ok {
		return r.output, r.err
	}
	// Default: success with empty output.
	return "", nil
}

func (f *fakeExecutor) Transfer(_ context.Context, localDir, remoteDir string, deleteExtra bool) error {
	f.transferCalls = append(f.transferCalls, transferCall{localDir: localDir, remoteDir: remoteDir, deleteExtra: deleteExtra})
	return f.transferErr
}

// fakeGenerator is a test double for ports.ConfigGenerator.
type fakeGenerator struct {
	err error
}

func (f *fakeGenerator) Generate(_ context.Context, _ *config.Config, _ string) error {
	return f.err
}

// defaultConfig returns a minimal Config for testing.
func defaultConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Port: 8443},
	}
}

func TestService_Deploy_HappyPath(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Patch the health URL to point at our test server.
	healthURL := srv.URL + "/_vibewarden/health"

	svc := deployapp.NewService(executor, generator, func(req *http.Request) (*http.Response, error) {
		// Redirect health check to our test server.
		req2, _ := http.NewRequestWithContext(req.Context(), req.Method, healthURL, nil)
		return http.DefaultClient.Do(req2) //nolint:gosec // G704: test-only helper; URL is from httptest.NewServer
	})

	var buf bytes.Buffer
	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath: "/tmp/myproject/vibewarden.yaml",
		Out:        &buf,
	})
	if err != nil {
		t.Fatalf("Deploy() unexpected error: %v\noutput:\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Deploy complete") {
		t.Errorf("expected 'Deploy complete' in output, got:\n%s", out)
	}

	// Verify that prerequisite checks ran.
	assertRunCalled(t, executor.runCalls, "which docker")
	assertRunCalled(t, executor.runCalls, "docker compose version")

	// Verify transfer was called.
	if len(executor.transferCalls) == 0 {
		t.Error("expected Transfer to be called at least once")
	}

	// Verify docker compose up was called.
	assertRunCalledContains(t, executor.runCalls, "docker compose up -d")
}

func TestService_Deploy_GenerateFails(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{err: errors.New("template error")}

	svc := deployapp.NewService(executor, generator, nil)

	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err == nil {
		t.Fatal("expected error when generator fails")
	}
	if !strings.Contains(err.Error(), "generating config files") {
		t.Errorf("error message should mention 'generating config files', got: %v", err)
	}
}

func TestService_Deploy_MissingDocker(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"which docker": {err: errors.New("not found")},
		},
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator, nil)

	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err == nil {
		t.Fatal("expected error when docker is missing")
	}
	if !strings.Contains(err.Error(), "remote prerequisites") {
		t.Errorf("error should mention 'remote prerequisites', got: %v", err)
	}
}

func TestService_Deploy_TransferFails(t *testing.T) {
	executor := &fakeExecutor{
		transferErr: errors.New("rsync failed"),
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator, nil)

	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err == nil {
		t.Fatal("expected error when transfer fails")
	}
	if !strings.Contains(err.Error(), "transferring") {
		t.Errorf("error should mention 'transferring', got: %v", err)
	}
}

func TestService_Deploy_HealthCheckTimeout(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	// Health check always returns 503.
	svc := deployapp.NewService(executor, generator, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       http.NoBody,
		}, nil
	})

	// Use a cancelled context to avoid the full 60 s timeout.
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after the first health poll attempt completes.
	go func() {
		cancel()
	}()

	err := svc.Deploy(ctx, defaultConfig(), deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err == nil {
		t.Fatal("expected error when health check fails")
	}
}

func TestService_Status(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"docker compose --project-directory ~/vibewarden/ ps": {output: "NAME   SERVICE   STATUS\nvw    vibewarden   running"},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	var buf bytes.Buffer
	err := svc.Status(context.Background(), deployapp.StatusOptions{Out: &buf})
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "running") {
		t.Errorf("expected status output to contain 'running', got:\n%s", buf.String())
	}
}

func TestService_Status_Error(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"docker compose --project-directory ~/vibewarden/ ps": {err: errors.New("connection refused")},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	err := svc.Status(context.Background(), deployapp.StatusOptions{})
	if err == nil {
		t.Fatal("expected error when executor fails")
	}
	if !strings.Contains(err.Error(), "fetching remote status") {
		t.Errorf("error should mention 'fetching remote status', got: %v", err)
	}
}

func TestService_Logs(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"docker compose --project-directory ~/vibewarden/ logs --tail=50": {output: "log line 1\nlog line 2"},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	var buf bytes.Buffer
	err := svc.Logs(context.Background(), deployapp.LogsOptions{Lines: 50, Out: &buf})
	if err != nil {
		t.Fatalf("Logs() unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "log line 1") {
		t.Errorf("expected log output to contain 'log line 1', got:\n%s", buf.String())
	}
}

func TestService_Logs_AllLines(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"docker compose --project-directory ~/vibewarden/ logs": {output: "all logs"},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	var buf bytes.Buffer
	err := svc.Logs(context.Background(), deployapp.LogsOptions{Lines: 0, Out: &buf})
	if err != nil {
		t.Fatalf("Logs() unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "all logs") {
		t.Errorf("expected 'all logs' in output, got:\n%s", buf.String())
	}
}

func TestService_Logs_Error(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"docker compose --project-directory ~/vibewarden/ logs --tail=50": {err: errors.New("remote error")},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	err := svc.Logs(context.Background(), deployapp.LogsOptions{Lines: 50})
	if err == nil {
		t.Fatal("expected error when executor fails")
	}
	if !strings.Contains(err.Error(), "fetching remote logs") {
		t.Errorf("error should mention 'fetching remote logs', got: %v", err)
	}
}

func TestService_Deploy_RemoteDir(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	healthURL := srv.URL + "/_vibewarden/health"

	svc := deployapp.NewService(executor, generator, func(req *http.Request) (*http.Response, error) {
		req2, _ := http.NewRequestWithContext(req.Context(), req.Method, healthURL, nil)
		return http.DefaultClient.Do(req2) //nolint:gosec // G704: test-only helper; URL is from httptest.NewServer
	})

	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath:  "/home/user/myproject/vibewarden.prod.yaml",
		ProjectName: "myproject",
	})
	if err != nil {
		t.Fatalf("Deploy() unexpected error: %v", err)
	}

	// Verify the remote directory includes the project name.
	found := false
	for _, call := range executor.runCalls {
		if strings.Contains(call, "~/vibewarden/myproject/") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected remote dir ~/vibewarden/myproject/ to appear in run calls, got: %v", executor.runCalls)
	}
}

func TestService_Deploy_DockerComposeMissing(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"which docker":           {output: "/usr/bin/docker"},
			"docker compose version": {err: errors.New("docker compose not found")},
		},
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator, nil)

	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err == nil {
		t.Fatal("expected error when docker compose is missing")
	}
	if !strings.Contains(err.Error(), "remote prerequisites") {
		t.Errorf("error should mention 'remote prerequisites', got: %v", err)
	}
}

// assertRunCalled checks that cmd appears in calls.
func assertRunCalled(t *testing.T, calls []string, cmd string) {
	t.Helper()
	for _, c := range calls {
		if c == cmd {
			return
		}
	}
	t.Errorf("expected Run(%q) to be called, actual calls: %v", cmd, calls)
}

// assertRunCalledContains checks that at least one call contains the substring.
func assertRunCalledContains(t *testing.T, calls []string, substr string) {
	t.Helper()
	for _, c := range calls {
		if strings.Contains(c, substr) {
			return
		}
	}
	t.Errorf("expected a Run call containing %q, actual calls: %v", substr, calls)
}

func TestProjectNameFromConfig(t *testing.T) {
	// We test the exported behaviour indirectly through Deploy's remote dir.
	tests := []struct {
		name        string
		configPath  string
		projectName string // explicit override
		wantDirSub  string // expected substring in remote dir run calls
	}{
		{
			name:       "derive from config dir",
			configPath: "/home/user/myapp/vibewarden.yaml",
			wantDirSub: "~/vibewarden/myapp/",
		},
		{
			name:        "explicit project name overrides config dir",
			configPath:  "/home/user/myapp/vibewarden.yaml",
			projectName: "production",
			wantDirSub:  "~/vibewarden/production/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &fakeExecutor{}
			generator := &fakeGenerator{}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			healthURL := srv.URL + "/_vibewarden/health"

			svc := deployapp.NewService(executor, generator, func(req *http.Request) (*http.Response, error) {
				req2, _ := http.NewRequestWithContext(req.Context(), req.Method, healthURL, nil)
				return http.DefaultClient.Do(req2) //nolint:gosec // G704: test-only helper; URL is from httptest.NewServer
			})

			_ = svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
				ConfigPath:  tt.configPath,
				ProjectName: tt.projectName,
			})

			found := false
			for _, c := range executor.runCalls {
				if strings.Contains(c, tt.wantDirSub) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected remote dir %q in run calls, got: %v", tt.wantDirSub, executor.runCalls)
			}
		})
	}
}

// TestService_Deploy_NilOut ensures a nil Out does not panic.
func TestService_Deploy_NilOut(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{err: errors.New("stop early")}

	svc := deployapp.NewService(executor, generator, nil)

	// Should not panic even with nil Out.
	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
		Out:        nil,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestService_Deploy_ComposeUpFails verifies that a compose up failure is surfaced.
func TestService_Deploy_ComposeUpFails(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{},
	}

	// Set compose up to fail.
	calls := 0
	origRun := executor.runResponses
	_ = origRun

	executor2 := &mockRunExecutor{
		runFn: func(cmd string) (string, error) {
			if strings.Contains(cmd, "docker compose up -d") {
				return "", errors.New("compose up failed")
			}
			return "", nil
		},
	}

	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor2, generator, nil)
	_ = calls

	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err == nil {
		t.Fatal("expected error when compose up fails")
	}
	if !strings.Contains(err.Error(), "docker compose up") {
		t.Errorf("error should mention 'docker compose up', got: %v", err)
	}
}

// mockRunExecutor is a flexible test double with a custom Run function.
type mockRunExecutor struct {
	runFn         func(cmd string) (string, error)
	transferCalls []transferCall
}

func (m *mockRunExecutor) Run(_ context.Context, cmd string) (string, error) {
	if m.runFn != nil {
		return m.runFn(cmd)
	}
	return "", nil
}

func (m *mockRunExecutor) Transfer(_ context.Context, localDir, remoteDir string, deleteExtra bool) error {
	m.transferCalls = append(m.transferCalls, transferCall{localDir: localDir, remoteDir: remoteDir, deleteExtra: deleteExtra})
	return nil
}

// Ensure fmt is used (used in assertRunCalledContains via Errorf).
var _ = fmt.Sprintf
