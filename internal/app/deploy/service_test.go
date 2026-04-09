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
	// transferFileErr is returned by every TransferFile call if set.
	transferFileErr error
	// runCalls records every Run invocation for assertions.
	runCalls []string
	// transferCalls records every Transfer invocation for assertions.
	transferCalls []transferCall
	// transferFileCalls records every TransferFile invocation for assertions.
	transferFileCalls []transferFileCall
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

type transferFileCall struct {
	localFile  string
	remotePath string
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

func (f *fakeExecutor) TransferFile(_ context.Context, localFile, remotePath string) error {
	f.transferFileCalls = append(f.transferFileCalls, transferFileCall{localFile: localFile, remotePath: remotePath})
	return f.transferFileErr
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
			"docker compose --project-directory ~/vibewarden/myproject/ ps": {output: "NAME   SERVICE   STATUS\nvw    vibewarden   running"},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	var buf bytes.Buffer
	err := svc.Status(context.Background(), deployapp.StatusOptions{
		ProjectName: "myproject",
		Out:         &buf,
	})
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "running") {
		t.Errorf("expected status output to contain 'running', got:\n%s", buf.String())
	}
}

func TestService_Status_DerivedFromConfigPath(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"docker compose --project-directory ~/vibewarden/myapp/ ps": {output: "NAME   SERVICE   STATUS\nvw    vibewarden   running"},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	var buf bytes.Buffer
	err := svc.Status(context.Background(), deployapp.StatusOptions{
		ConfigPath: "/home/user/myapp/vibewarden.yaml",
		Out:        &buf,
	})
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
			"docker compose --project-directory ~/vibewarden/vibewarden/ ps": {err: errors.New("connection refused")},
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
			"docker compose --project-directory ~/vibewarden/myproject/ logs --tail=50": {output: "log line 1\nlog line 2"},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	var buf bytes.Buffer
	err := svc.Logs(context.Background(), deployapp.LogsOptions{
		ProjectName: "myproject",
		Lines:       50,
		Out:         &buf,
	})
	if err != nil {
		t.Fatalf("Logs() unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "log line 1") {
		t.Errorf("expected log output to contain 'log line 1', got:\n%s", buf.String())
	}
}

func TestService_Logs_DerivedFromConfigPath(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"docker compose --project-directory ~/vibewarden/myapp/ logs --tail=20": {output: "log entry"},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	var buf bytes.Buffer
	err := svc.Logs(context.Background(), deployapp.LogsOptions{
		ConfigPath: "/home/user/myapp/vibewarden.yaml",
		Lines:      20,
		Out:        &buf,
	})
	if err != nil {
		t.Fatalf("Logs() unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "log entry") {
		t.Errorf("expected 'log entry' in output, got:\n%s", buf.String())
	}
}

func TestService_Logs_AllLines(t *testing.T) {
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"docker compose --project-directory ~/vibewarden/myproject/ logs": {output: "all logs"},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	var buf bytes.Buffer
	err := svc.Logs(context.Background(), deployapp.LogsOptions{
		ProjectName: "myproject",
		Lines:       0,
		Out:         &buf,
	})
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
			"docker compose --project-directory ~/vibewarden/myproject/ logs --tail=50": {err: errors.New("remote error")},
		},
	}

	svc := deployapp.NewService(executor, nil, nil)

	err := svc.Logs(context.Background(), deployapp.LogsOptions{
		ProjectName: "myproject",
		Lines:       50,
	})
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
	runFn             func(cmd string) (string, error)
	transferCalls     []transferCall
	transferFileCalls []transferFileCall
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

func (m *mockRunExecutor) TransferFile(_ context.Context, localFile, remotePath string) error {
	m.transferFileCalls = append(m.transferFileCalls, transferFileCall{localFile: localFile, remotePath: remotePath})
	return nil
}

// TestService_Deploy_TransferFileCalledForConfig verifies that Deploy uses
// TransferFile (not Transfer) for the config file, passing the full local path
// and the expected remote destination without a trailing slash.
func TestService_Deploy_TransferFileCalledForConfig(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	healthURL := srv.URL + "/_vibewarden/health"
	svc := deployapp.NewService(executor, generator, func(req *http.Request) (*http.Response, error) {
		req2, _ := http.NewRequestWithContext(req.Context(), req.Method, healthURL, nil)
		return http.DefaultClient.Do(req2) //nolint:gosec // test-only helper; URL is from httptest.NewServer
	})

	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath:  "/tmp/myproject/vibewarden.yaml",
		ProjectName: "myproject",
	})
	if err != nil {
		t.Fatalf("Deploy() unexpected error: %v", err)
	}

	if len(executor.transferFileCalls) == 0 {
		t.Fatal("expected TransferFile to be called for the config file")
	}

	call := executor.transferFileCalls[0]
	if call.localFile != "/tmp/myproject/vibewarden.yaml" {
		t.Errorf("TransferFile localFile = %q, want %q", call.localFile, "/tmp/myproject/vibewarden.yaml")
	}
	wantRemote := "~/vibewarden/myproject/vibewarden.yaml"
	if call.remotePath != wantRemote {
		t.Errorf("TransferFile remotePath = %q, want %q", call.remotePath, wantRemote)
	}
	if strings.HasSuffix(call.localFile, "/") {
		t.Errorf("TransferFile localFile must not end with '/', got %q", call.localFile)
	}
}

// TestService_Deploy_TransferFileFails verifies that a TransferFile error is
// surfaced with the expected context message.
func TestService_Deploy_TransferFileFails(t *testing.T) {
	executor := &fakeExecutor{
		transferFileErr: errors.New("rsync: file not found"),
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator, nil)

	err := svc.Deploy(context.Background(), defaultConfig(), deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err == nil {
		t.Fatal("expected error when TransferFile fails")
	}
	if !strings.Contains(err.Error(), "transferring") {
		t.Errorf("error should mention 'transferring', got: %v", err)
	}
}

// TestService_Deploy_ImageMode verifies that when cfg.App.Build is empty the
// deploy flow runs docker compose pull followed by docker compose up -d.
func TestService_Deploy_ImageMode(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	healthURL := srv.URL + "/_vibewarden/health"
	svc := deployapp.NewService(executor, generator, func(req *http.Request) (*http.Response, error) {
		req2, _ := http.NewRequestWithContext(req.Context(), req.Method, healthURL, nil)
		return http.DefaultClient.Do(req2) //nolint:gosec // test-only helper; URL is from httptest.NewServer
	})

	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"

	err := svc.Deploy(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err != nil {
		t.Fatalf("Deploy() unexpected error: %v", err)
	}

	// docker compose pull must be called.
	assertRunCalledContains(t, executor.runCalls, "docker compose pull")
	// docker compose up -d (without --build) must be called.
	assertRunCalledContains(t, executor.runCalls, "docker compose up -d")

	// docker compose up -d --build must NOT be called.
	for _, c := range executor.runCalls {
		if strings.Contains(c, "--build") {
			t.Errorf("did not expect --build flag in image mode, but got run call: %q", c)
		}
	}

	// App build context must NOT be transferred when app.build is empty.
	// Only the generated dir transfer is expected (transferCalls has exactly one entry).
	if len(executor.transferCalls) != 1 {
		t.Errorf("expected exactly 1 Transfer call in image mode, got %d: %v", len(executor.transferCalls), executor.transferCalls)
	}
}

// TestService_Deploy_BuildMode verifies that when cfg.App.Build is set the
// deploy flow transfers the build context and runs docker compose up -d --build
// instead of docker compose pull && docker compose up -d.
func TestService_Deploy_BuildMode(t *testing.T) {
	executor := &fakeExecutor{}
	generator := &fakeGenerator{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	healthURL := srv.URL + "/_vibewarden/health"
	svc := deployapp.NewService(executor, generator, func(req *http.Request) (*http.Response, error) {
		req2, _ := http.NewRequestWithContext(req.Context(), req.Method, healthURL, nil)
		return http.DefaultClient.Do(req2) //nolint:gosec // test-only helper; URL is from httptest.NewServer
	})

	cfg := defaultConfig()
	cfg.App.Build = "."

	err := svc.Deploy(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err != nil {
		t.Fatalf("Deploy() unexpected error: %v", err)
	}

	// docker compose up -d --build must be called.
	assertRunCalledContains(t, executor.runCalls, "docker compose up -d --build")

	// docker compose pull must NOT be called.
	for _, c := range executor.runCalls {
		if strings.Contains(c, "docker compose pull") {
			t.Errorf("did not expect 'docker compose pull' in build mode, got run call: %q", c)
		}
	}

	// Two Transfer calls expected: generated dir + build context.
	if len(executor.transferCalls) != 2 {
		t.Errorf("expected 2 Transfer calls in build mode (generated + build context), got %d: %v",
			len(executor.transferCalls), executor.transferCalls)
	}
}

// TestService_Deploy_BuildMode_TransferContextFails verifies that a failure to
// transfer the app build context is propagated correctly.
func TestService_Deploy_BuildMode_TransferContextFails(t *testing.T) {
	callCount := 0
	executor := &mockRunExecutor{
		runFn: func(_ string) (string, error) { return "", nil },
	}
	// Wrap in a custom executor that fails on the second Transfer call (build context).
	failingTransfer := &buildContextFailExecutor{
		mockRunExecutor: executor,
		failOnTransfer:  2,
		transferErr:     fmt.Errorf("rsync failed"),
		callCount:       &callCount,
	}

	generator := &fakeGenerator{}
	svc := deployapp.NewService(failingTransfer, generator, nil)

	cfg := defaultConfig()
	cfg.App.Build = "."

	err := svc.Deploy(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath: "/tmp/proj/vibewarden.yaml",
	})
	if err == nil {
		t.Fatal("expected error when build context transfer fails")
	}
	if !strings.Contains(err.Error(), "transferring app build context") {
		t.Errorf("error should mention 'transferring app build context', got: %v", err)
	}
}

// buildContextFailExecutor wraps mockRunExecutor and fails on the nth Transfer call.
type buildContextFailExecutor struct {
	*mockRunExecutor
	failOnTransfer int
	transferErr    error
	callCount      *int
}

func (b *buildContextFailExecutor) Transfer(_ context.Context, localDir, remoteDir string, deleteExtra bool) error {
	*b.callCount++
	if *b.callCount == b.failOnTransfer {
		return b.transferErr
	}
	b.transferCalls = append(b.transferCalls,
		transferCall{localDir: localDir, remoteDir: remoteDir, deleteExtra: deleteExtra})
	return nil
}

func (b *buildContextFailExecutor) TransferFile(_ context.Context, localFile, remotePath string) error {
	b.transferFileCalls = append(b.transferFileCalls,
		transferFileCall{localFile: localFile, remotePath: remotePath})
	return nil
}

// Ensure fmt is used (used in assertRunCalledContains via Errorf).
var _ = fmt.Sprintf
