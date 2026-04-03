package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeCompose is a test double for ports.ComposeRunner.
type fakeCompose struct {
	upErr      error
	restartErr error
	versionStr string
	versionErr error
	infoErr    error
	psResult   []ports.ContainerInfo
	psErr      error

	capturedComposeFile string
	capturedProfiles    []string
	restartCalled       int
}

func (f *fakeCompose) Up(_ context.Context, composeFile string, profiles []string) error {
	f.capturedComposeFile = composeFile
	f.capturedProfiles = profiles
	return f.upErr
}

func (f *fakeCompose) Restart(_ context.Context, _ string, _ []string) error {
	f.restartCalled++
	return f.restartErr
}

func (f *fakeCompose) Version(_ context.Context) (string, error) {
	return f.versionStr, f.versionErr
}

func (f *fakeCompose) Info(_ context.Context) error {
	return f.infoErr
}

func (f *fakeCompose) PS(_ context.Context, _ string) ([]ports.ContainerInfo, error) {
	return f.psResult, f.psErr
}

// fakeGenerator is a test double for ports.ConfigGenerator.
type fakeGenerator struct {
	generateErr       error
	capturedOutputDir string
	generateCalled    bool
	generateCallCount int
}

func (f *fakeGenerator) Generate(_ context.Context, _ *config.Config, outputDir string) error {
	f.generateCalled = true
	f.generateCallCount++
	f.capturedOutputDir = outputDir
	return f.generateErr
}

func defaultConfig() *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: 8443},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		TLS:      config.TLSConfig{Enabled: true, Provider: "self-signed"},
		RateLimit: config.RateLimitConfig{
			Enabled: true,
			PerIP:   config.RateLimitRuleConfig{RequestsPerSecond: 10, Burst: 20},
		},
		Telemetry: config.TelemetryConfig{Prometheus: config.PrometheusExporterConfig{Enabled: true}},
		Kratos:    config.KratosConfig{PublicURL: "http://127.0.0.1:4433", AdminURL: "http://127.0.0.1:4434"},
	}
}

func TestDevService_Run(t *testing.T) {
	tests := []struct {
		name               string
		opts               ops.DevOptions
		upErr              error
		wantErr            bool
		wantProfiles       []string
		wantOutputContains []string
	}{
		{
			name:         "baseline stack — no observability",
			opts:         ops.DevOptions{Observability: false},
			wantErr:      false,
			wantProfiles: nil,
			wantOutputContains: []string{
				"Proxy (VibeWarden):",
				"https://localhost:8443",
				"vibew status",
			},
		},
		{
			name:         "observability profile enabled",
			opts:         ops.DevOptions{Observability: true},
			wantErr:      false,
			wantProfiles: []string{"observability"},
			wantOutputContains: []string{
				"Prometheus:",
				"Grafana:",
				"Observability profile enabled",
			},
		},
		{
			name:    "docker compose up returns error",
			opts:    ops.DevOptions{},
			upErr:   errors.New("docker not running"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &fakeCompose{upErr: tt.upErr}
			svc := ops.NewDevService(fc)
			cfg := defaultConfig()
			var buf bytes.Buffer

			err := svc.Run(context.Background(), cfg, tt.opts, &buf)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				out := buf.String()
				for _, want := range tt.wantOutputContains {
					if !strings.Contains(out, want) {
						t.Errorf("output missing %q\ngot:\n%s", want, out)
					}
				}

				// Check profiles forwarded to compose
				if len(tt.wantProfiles) == 0 && len(fc.capturedProfiles) != 0 {
					t.Errorf("expected no profiles, got %v", fc.capturedProfiles)
				}
				for i, p := range tt.wantProfiles {
					if i >= len(fc.capturedProfiles) || fc.capturedProfiles[i] != p {
						t.Errorf("profile[%d] = %q, want %q", i, fc.capturedProfiles[i], p)
					}
				}
			}
		})
	}
}

func TestDevService_WithGenerator_CallsGenerateBeforeUp(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{}
	svc := ops.NewDevServiceWithGenerator(fc, fg)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if !fg.generateCalled {
		t.Error("expected Generate() to be called, but it was not")
	}
	if fg.capturedOutputDir != ".vibewarden/generated" {
		t.Errorf("Generate() called with outputDir=%q, want %q", fg.capturedOutputDir, ".vibewarden/generated")
	}
}

func TestDevService_WithGenerator_PassesGeneratedComposeFilePath(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{}
	svc := ops.NewDevServiceWithGenerator(fc, fg)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	wantComposeFile := ".vibewarden/generated/docker-compose.yml"
	if fc.capturedComposeFile != wantComposeFile {
		t.Errorf("Up() called with composeFile=%q, want %q", fc.capturedComposeFile, wantComposeFile)
	}
}

func TestDevService_WithGenerator_GenerateError_ReturnsError(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{generateErr: errors.New("template render failed")}
	svc := ops.NewDevServiceWithGenerator(fc, fg)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() expected error when Generate() fails, got nil")
	}
}

func TestDevService_WithoutGenerator_UsesEmptyComposeFile(t *testing.T) {
	// Without a generator, Up should be called with an empty composeFile so
	// that docker compose uses its default discovery behaviour.
	fc := &fakeCompose{}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if fc.capturedComposeFile != "" {
		t.Errorf("Up() called with composeFile=%q, want empty string for backward compat", fc.capturedComposeFile)
	}
}

func TestDevService_WithGenerator_PrintsGeneratedOutputMessage(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{}
	svc := ops.NewDevServiceWithGenerator(fc, fg)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, ".vibewarden/generated") {
		t.Errorf("expected output to mention generated dir, got:\n%s", out)
	}
}

// fakeWatcher is a test double for ports.ConfigWatcher.
type fakeWatcher struct {
	// ch is the channel returned by Watch. Tests send on this channel to
	// simulate a file-change event.
	ch       chan struct{}
	watchErr error
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{ch: make(chan struct{}, 1)}
}

func (f *fakeWatcher) Watch(_ context.Context, _ string) (<-chan struct{}, error) {
	if f.watchErr != nil {
		return nil, f.watchErr
	}
	return f.ch, nil
}

// Ensure fakeWatcher satisfies the interface at compile time.
var _ ports.ConfigWatcher = (*fakeWatcher)(nil)

func TestDevService_Watch_PrintsWatchingMessage(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{}
	fw := newFakeWatcher()
	svc := ops.NewDevServiceWithWatcher(fc, fg, fw)
	cfg := defaultConfig()
	var buf bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so watchLoop exits right away.
	cancel()

	if err := svc.Run(ctx, cfg, ops.DevOptions{Watch: true, ConfigPath: "vibewarden.yaml"}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Watching") {
		t.Errorf("expected output to contain 'Watching', got:\n%s", out)
	}
}

func TestDevService_Watch_RegeneratesAndRestartsOnChange(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{}
	fw := newFakeWatcher()
	svc := ops.NewDevServiceWithWatcher(fc, fg, fw)
	cfg := defaultConfig()
	var buf bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- svc.Run(ctx, cfg, ops.DevOptions{Watch: true, ConfigPath: "vibewarden.yaml"}, &buf)
	}()

	// Simulate one config-change event and then close the watcher channel so
	// watchLoop exits naturally (simulates the watcher being shut down).
	fw.ch <- struct{}{}
	close(fw.ch)

	if err := <-done; err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if fc.restartCalled == 0 {
		t.Error("expected Restart() to be called after a file-change event")
	}
	// Generate should have been called at least twice: once on startup, once on change.
	if fg.generateCallCount < 2 {
		t.Errorf("expected Generate() called at least 2 times, got %d", fg.generateCallCount)
	}
	out := buf.String()
	if !strings.Contains(out, "config changed, regenerating") {
		t.Errorf("expected output to contain regenerating message, got:\n%s", out)
	}
}

func TestDevService_Watch_WatcherSetupError_ReturnsError(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{}
	fw := &fakeWatcher{watchErr: errors.New("inotify limit reached")}
	svc := ops.NewDevServiceWithWatcher(fc, fg, fw)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{Watch: true, ConfigPath: "vibewarden.yaml"}, &buf)
	if err == nil {
		t.Fatal("Run() expected error when watcher setup fails, got nil")
	}
}

func TestDevService_Watch_WatcherNil_DoesNotBlock(t *testing.T) {
	// When watch=true but no watcher is wired, Run should return without blocking.
	fc := &fakeCompose{}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{Watch: true}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
}

// fakeImageChecker is a test double for ports.DockerImageChecker.
type fakeImageChecker struct {
	exists bool
	err    error
}

func (f *fakeImageChecker) ImageExists(_ context.Context, _ string) (bool, error) {
	return f.exists, f.err
}

// Ensure fakeImageChecker satisfies the interface at compile time.
var _ ports.DockerImageChecker = (*fakeImageChecker)(nil)

func TestDevService_ImageCheck_SkippedWhenNoChecker(t *testing.T) {
	// Without an image checker wired, Run should succeed even when app.image is set.
	fc := &fakeCompose{}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
}

func TestDevService_ImageCheck_SkippedWhenBuildSet(t *testing.T) {
	// When app.build is set, compose builds the image — skip the check.
	fc := &fakeCompose{}
	fi := &fakeImageChecker{exists: false} // would fail if called
	svc := ops.NewDevService(fc).WithImageChecker(fi)
	cfg := defaultConfig()
	cfg.App.Build = "."
	cfg.App.Image = "myapp:latest"
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error when build is set: %v", err)
	}
}

func TestDevService_ImageCheck_SkippedWhenNoImage(t *testing.T) {
	// When app.image is empty, no check is performed.
	fc := &fakeCompose{}
	fi := &fakeImageChecker{exists: false} // would fail if called
	svc := ops.NewDevService(fc).WithImageChecker(fi)
	cfg := defaultConfig()
	cfg.App.Image = ""
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error when no image configured: %v", err)
	}
}

func TestDevService_ImageCheck_ImageExists_Proceeds(t *testing.T) {
	// When the image exists, Run proceeds normally.
	fc := &fakeCompose{}
	fi := &fakeImageChecker{exists: true}
	svc := ops.NewDevService(fc).WithImageChecker(fi)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "myapp:latest") {
		t.Errorf("expected output to mention image name, got:\n%s", out)
	}
}

func TestDevService_ImageCheck_ImageMissing_ReturnsError(t *testing.T) {
	// When the image is absent, Run must return an error before calling compose.
	fc := &fakeCompose{}
	fi := &fakeImageChecker{exists: false}
	svc := ops.NewDevService(fc).WithImageChecker(fi)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() expected error for missing image, got nil")
	}
	if !strings.Contains(err.Error(), "myapp:latest") {
		t.Errorf("error should mention image name, got: %v", err)
	}
	// compose.Up must NOT have been called
	if fc.capturedComposeFile != "" || fc.capturedProfiles != nil {
		t.Errorf("compose.Up should not have been called when image is missing")
	}
}

func TestDevService_ImageCheck_ImageMissing_WithGoLang_ContainsBuildHint(t *testing.T) {
	// Error message for Go projects should include go build instructions.
	fc := &fakeCompose{}
	fi := &fakeImageChecker{exists: false}
	svc := ops.NewDevService(fc).WithImageChecker(fi)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{DetectedLang: "go"}, &buf)
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "go build") {
		t.Errorf("error should contain 'go build' hint for Go projects, got: %v", err)
	}
}

func TestDevService_ImageCheck_ImageMissing_WithKotlinLang_ContainsBuildHint(t *testing.T) {
	fc := &fakeCompose{}
	fi := &fakeImageChecker{exists: false}
	svc := ops.NewDevService(fc).WithImageChecker(fi)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{DetectedLang: "kotlin"}, &buf)
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "gradlew") {
		t.Errorf("error should contain 'gradlew' hint for Kotlin projects, got: %v", err)
	}
}

func TestDevService_ImageCheck_ImageMissing_WithTypeScriptLang_ContainsBuildHint(t *testing.T) {
	fc := &fakeCompose{}
	fi := &fakeImageChecker{exists: false}
	svc := ops.NewDevService(fc).WithImageChecker(fi)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{DetectedLang: "typescript"}, &buf)
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "npm run build") {
		t.Errorf("error should contain 'npm run build' hint for TypeScript projects, got: %v", err)
	}
}

func TestDevService_ImageCheck_ImageMissing_UnknownLang_ContainsGenericHint(t *testing.T) {
	fc := &fakeCompose{}
	fi := &fakeImageChecker{exists: false}
	svc := ops.NewDevService(fc).WithImageChecker(fi)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{DetectedLang: ""}, &buf)
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "vibew build") {
		t.Errorf("error should mention 'vibew build', got: %v", err)
	}
}

func TestDevService_ImageCheck_CheckerError_ReturnsError(t *testing.T) {
	// When the image checker itself fails (e.g. docker daemon down), Run returns
	// the wrapped error.
	fc := &fakeCompose{}
	fi := &fakeImageChecker{err: errors.New("docker daemon not running")}
	svc := ops.NewDevService(fc).WithImageChecker(fi)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() expected error when checker fails, got nil")
	}
	if !strings.Contains(err.Error(), "docker daemon not running") {
		t.Errorf("error should wrap checker error, got: %v", err)
	}
}
