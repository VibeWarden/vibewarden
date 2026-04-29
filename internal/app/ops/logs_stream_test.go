package ops_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// --- fakes ---

// fakeComposeRunner is a minimal ComposeRunner fake that returns configurable
// PS results. All other methods are no-ops.
type fakeComposeRunner struct {
	psContainers []ports.ContainerInfo
	psErr        error
}

func (f *fakeComposeRunner) Up(_ context.Context, _ string, _ []string, _ ports.ComposeUpOptions) error {
	return nil
}
func (f *fakeComposeRunner) Restart(_ context.Context, _ string, _ []string) error { return nil }
func (f *fakeComposeRunner) Down(_ context.Context, _ string, _ ports.ComposeDownOptions) (ports.DownResult, error) {
	return ports.DownResult{}, nil
}
func (f *fakeComposeRunner) Version(_ context.Context) (string, error) { return "", nil }
func (f *fakeComposeRunner) Info(_ context.Context) error              { return nil }
func (f *fakeComposeRunner) PS(_ context.Context, _ string) ([]ports.ContainerInfo, error) {
	return f.psContainers, f.psErr
}
func (f *fakeComposeRunner) Logs(_ context.Context, _ string, _ string, _ int) (string, error) {
	return "", nil
}

// fakeStreamer is a ComposeLogsStreamer fake that records the options it was
// called with and returns a configurable error.
type fakeStreamer struct {
	called bool
	opts   ports.ComposeLogsStreamOptions
	err    error
}

func (f *fakeStreamer) Stream(_ context.Context, opts ports.ComposeLogsStreamOptions) error {
	f.called = true
	f.opts = opts
	return f.err
}

// writeComposeFile writes a minimal docker-compose.yml with the given services
// to path.
func writeComposeFile(t *testing.T, path string, services []string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("name: testproject\nservices:\n")
	for _, svc := range services {
		sb.WriteString("  " + svc + ":\n    image: " + svc + ":latest\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatalf("writing compose file: %v", err)
	}
}

// patchGeneratedDir temporarily patches the generatedOutputDir used by the
// app service by symlink or by writing the compose file to the expected path.
// Since generatedOutputDir is a package constant we can't override it
// directly, so tests that need a custom path use the approach of creating the
// directory structure the service expects.
//
// This helper returns a function that restores the original working directory
// if it was changed.
func runInTempProjectDir(t *testing.T, services []string) string {
	t.Helper()
	dir := t.TempDir()

	genDir := filepath.Join(dir, ".vibewarden", "generated")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatalf("creating generated dir: %v", err)
	}

	if services != nil {
		writeComposeFile(t, filepath.Join(genDir, "docker-compose.yml"), services)
	}

	// Change to the temp dir so the service finds the correct relative path.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Logf("restoring wd: %v", err)
		}
	})
	return dir
}

// --- tests ---

// TestLogsStreamService_AllServices verifies that when no services are
// requested the streamer is called with an empty Services slice.
func TestLogsStreamService_AllServices(t *testing.T) {
	runInTempProjectDir(t, []string{"vibewarden", "app"})

	runner := &fakeComposeRunner{
		psContainers: []ports.ContainerInfo{{Name: "c1", Service: "vibewarden"}},
	}
	streamer := &fakeStreamer{}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err := svc.Run(context.Background(), cfg, ops.LogsStreamOptions{Tail: 100}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !streamer.called {
		t.Fatal("streamer was not called")
	}
	if len(streamer.opts.Services) != 0 {
		t.Errorf("expected empty Services, got %v", streamer.opts.Services)
	}
}

// TestLogsStreamService_SingleService verifies that a single requested service
// is passed through to the streamer.
func TestLogsStreamService_SingleService(t *testing.T) {
	runInTempProjectDir(t, []string{"vibewarden", "app"})

	runner := &fakeComposeRunner{
		psContainers: []ports.ContainerInfo{{Name: "c1", Service: "vibewarden"}},
	}
	streamer := &fakeStreamer{}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err := svc.Run(context.Background(), cfg, ops.LogsStreamOptions{
		Services: []string{"vibewarden"},
		Tail:     100,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !streamer.called {
		t.Fatal("streamer was not called")
	}
	if len(streamer.opts.Services) != 1 || streamer.opts.Services[0] != "vibewarden" {
		t.Errorf("expected Services=[vibewarden], got %v", streamer.opts.Services)
	}
}

// TestLogsStreamService_MultiService verifies that multiple services are
// passed through to the streamer.
func TestLogsStreamService_MultiService(t *testing.T) {
	runInTempProjectDir(t, []string{"vibewarden", "app"})

	runner := &fakeComposeRunner{
		psContainers: []ports.ContainerInfo{{Name: "c1", Service: "vibewarden"}},
	}
	streamer := &fakeStreamer{}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err := svc.Run(context.Background(), cfg, ops.LogsStreamOptions{
		Services: []string{"vibewarden", "app"},
		Tail:     100,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !streamer.called {
		t.Fatal("streamer was not called")
	}
	if len(streamer.opts.Services) != 2 {
		t.Errorf("expected 2 services, got %v", streamer.opts.Services)
	}
}

// TestLogsStreamService_UnknownService verifies that an unrecognised service
// name returns an ErrUnknownService whose message lists known services.
func TestLogsStreamService_UnknownService(t *testing.T) {
	runInTempProjectDir(t, []string{"vibewarden", "app"})

	runner := &fakeComposeRunner{
		psContainers: []ports.ContainerInfo{{Name: "c1", Service: "vibewarden"}},
	}
	streamer := &fakeStreamer{}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err := svc.Run(context.Background(), cfg, ops.LogsStreamOptions{
		Services: []string{"nope"},
		Tail:     100,
	}, &bytes.Buffer{}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var unknownErr *ops.ErrUnknownService
	if !errors.As(err, &unknownErr) {
		t.Fatalf("expected *ops.ErrUnknownService, got %T: %v", err, err)
	}
	if unknownErr.Service != "nope" {
		t.Errorf("Service = %q, want %q", unknownErr.Service, "nope")
	}
	if !strings.Contains(err.Error(), "vibewarden") || !strings.Contains(err.Error(), "app") {
		t.Errorf("error message should list known services, got: %v", err)
	}
	if streamer.called {
		t.Error("streamer should not be called when service is unknown")
	}
}

// TestLogsStreamService_StackNotRunning_PSEmpty verifies that an empty PS
// result returns ErrStackNotRunning without calling the streamer.
func TestLogsStreamService_StackNotRunning_PSEmpty(t *testing.T) {
	runInTempProjectDir(t, []string{"vibewarden", "app"})

	runner := &fakeComposeRunner{psContainers: nil}
	streamer := &fakeStreamer{}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err := svc.Run(context.Background(), cfg, ops.LogsStreamOptions{Tail: 100}, &bytes.Buffer{}, &bytes.Buffer{})

	if !errors.Is(err, ops.ErrStackNotRunning) {
		t.Errorf("expected ErrStackNotRunning, got %v", err)
	}
	if streamer.called {
		t.Error("streamer should not be called when stack is not running")
	}
}

// TestLogsStreamService_StackNotRunning_MissingFile verifies that a missing
// compose file returns ErrStackNotRunning.
func TestLogsStreamService_StackNotRunning_MissingFile(t *testing.T) {
	// Set up a project dir with NO compose file.
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Logf("restoring wd: %v", err)
		}
	})

	runner := &fakeComposeRunner{}
	streamer := &fakeStreamer{}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err = svc.Run(context.Background(), cfg, ops.LogsStreamOptions{Tail: 100}, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, ops.ErrStackNotRunning) {
		t.Errorf("expected ErrStackNotRunning, got %v", err)
	}
	if streamer.called {
		t.Error("streamer should not be called when compose file is missing")
	}
}

// TestLogsStreamService_DockerUnavailable verifies that ports.ErrDockerUnavailable
// from the streamer is propagated to the caller.
func TestLogsStreamService_DockerUnavailable(t *testing.T) {
	runInTempProjectDir(t, []string{"vibewarden", "app"})

	runner := &fakeComposeRunner{
		psContainers: []ports.ContainerInfo{{Name: "c1", Service: "vibewarden"}},
	}
	streamer := &fakeStreamer{err: ports.ErrDockerUnavailable}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err := svc.Run(context.Background(), cfg, ops.LogsStreamOptions{Tail: 100}, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, ports.ErrDockerUnavailable) {
		t.Errorf("expected ErrDockerUnavailable, got %v", err)
	}
}

// TestLogsStreamService_TailOption verifies that the Tail option is forwarded
// to the streamer.
func TestLogsStreamService_TailOption(t *testing.T) {
	runInTempProjectDir(t, []string{"vibewarden", "app"})

	runner := &fakeComposeRunner{
		psContainers: []ports.ContainerInfo{{Name: "c1", Service: "vibewarden"}},
	}
	streamer := &fakeStreamer{}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err := svc.Run(context.Background(), cfg, ops.LogsStreamOptions{Tail: 50}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if streamer.opts.Tail != 50 {
		t.Errorf("Tail = %d, want 50", streamer.opts.Tail)
	}
}

// TestLogsStreamService_FollowOption verifies that the Follow option is
// forwarded to the streamer.
func TestLogsStreamService_FollowOption(t *testing.T) {
	runInTempProjectDir(t, []string{"vibewarden", "app"})

	runner := &fakeComposeRunner{
		psContainers: []ports.ContainerInfo{{Name: "c1", Service: "vibewarden"}},
	}
	streamer := &fakeStreamer{}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err := svc.Run(context.Background(), cfg, ops.LogsStreamOptions{Tail: 100, Follow: true}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !streamer.opts.Follow {
		t.Error("Follow = false, want true")
	}
}

// TestLogsStreamService_SinceOption verifies that the Since option is
// forwarded verbatim to the streamer.
func TestLogsStreamService_SinceOption(t *testing.T) {
	runInTempProjectDir(t, []string{"vibewarden", "app"})

	runner := &fakeComposeRunner{
		psContainers: []ports.ContainerInfo{{Name: "c1", Service: "vibewarden"}},
	}
	streamer := &fakeStreamer{}
	svc := ops.NewLogsStreamService(runner, streamer)

	cfg := &config.Config{}
	cfg.Name = "testproj"

	err := svc.Run(context.Background(), cfg, ops.LogsStreamOptions{Tail: 100, Since: "5m"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if streamer.opts.Since != "5m" {
		t.Errorf("Since = %q, want %q", streamer.opts.Since, "5m")
	}
}
