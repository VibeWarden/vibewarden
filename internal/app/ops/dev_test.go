package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	logsResult string
	logsErr    error
	downResult ports.DownResult
	downErr    error

	capturedComposeFile string
	capturedProfiles    []string
	capturedUpOpts      ports.ComposeUpOptions
	capturedDownOpts    ports.ComposeDownOptions
	restartCalled       int
	downCalled          int
}

func (f *fakeCompose) Up(_ context.Context, composeFile string, profiles []string, opts ports.ComposeUpOptions) error {
	f.capturedComposeFile = composeFile
	f.capturedProfiles = profiles
	f.capturedUpOpts = opts
	return f.upErr
}

func (f *fakeCompose) Restart(_ context.Context, _ string, _ []string) error {
	f.restartCalled++
	return f.restartErr
}

func (f *fakeCompose) Down(_ context.Context, _ string, opts ports.ComposeDownOptions) (ports.DownResult, error) {
	f.downCalled++
	f.capturedDownOpts = opts
	return f.downResult, f.downErr
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

func (f *fakeCompose) Logs(_ context.Context, _ string, service string, _ int) (string, error) {
	if f.logsResult != "" {
		return f.logsResult, f.logsErr
	}
	return "fake log output for " + service, f.logsErr
}

// fakeGenerator is a test double for ports.ConfigGenerator.
type fakeGenerator struct {
	generateErr       error
	capturedOutputDir string
	generateCalled    bool
	generateCallCount int
}

func (f *fakeGenerator) Generate(_ context.Context, _ ports.GeneratorInput, outputDir string) error {
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
		Observability: config.ObservabilityConfig{
			GrafanaPort:    3001,
			PrometheusPort: 9090,
			LokiPort:       3100,
		},
	}
}

func TestDevService_Run(t *testing.T) {
	tests := []struct {
		name               string
		opts               ops.DevOptions
		upErr              error
		wantErr            bool
		wantOutputContains []string
	}{
		{
			name:    "baseline stack",
			opts:    ops.DevOptions{},
			wantErr: false,
			wantOutputContains: []string{
				"Started. https://localhost:8443",
				"Logs: vibew logs -f",
				"Stop: vibew down",
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

				// Baseline dev run must not pass any profiles to compose.
				if len(fc.capturedProfiles) != 0 {
					t.Errorf("expected no profiles for baseline dev, got %v", fc.capturedProfiles)
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

// TestDevService_StartupSummary_ThreeLines verifies that a successful dev
// run emits exactly the three expected hint lines (plus the "Dev environment
// started." header for continuity with historical output).
func TestDevService_StartupSummary_ThreeLines(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		port       int
		tlsEnabled bool
		wantURL    string
	}{
		{
			name:       "https tls with default port",
			host:       "127.0.0.1",
			port:       0,
			tlsEnabled: true,
			wantURL:    "https://localhost:8443",
		},
		{
			name:       "http when tls disabled",
			host:       "127.0.0.1",
			port:       0,
			tlsEnabled: false,
			wantURL:    "http://localhost:8443",
		},
		{
			name:       "0.0.0.0 renders as localhost",
			host:       "0.0.0.0",
			port:       9999,
			tlsEnabled: true,
			wantURL:    "https://localhost:9999",
		},
		{
			name:       "empty host defaults to localhost",
			host:       "",
			port:       1234,
			tlsEnabled: false,
			wantURL:    "http://localhost:1234",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &fakeCompose{}
			svc := ops.NewDevService(fc)
			cfg := defaultConfig()
			cfg.Server.Host = tt.host
			cfg.Server.Port = tt.port
			cfg.TLS.Enabled = tt.tlsEnabled
			var buf bytes.Buffer

			if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			out := buf.String()
			wantLines := []string{
				"Started. " + tt.wantURL,
				"Logs: vibew logs -f",
				"Stop: vibew down",
			}
			for _, line := range wantLines {
				if !strings.Contains(out, line) {
					t.Errorf("output missing line %q\ngot:\n%s", line, out)
				}
			}
		})
	}
}

// TestDevService_Verbose_ForwardsStderrWriter confirms that when Verbose is
// set on DevOptions, ComposeUpOptions.Stderr is populated on the call to
// compose.Up so the adapter streams compose output live.
func TestDevService_Verbose_ForwardsStderrWriter(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{Verbose: true}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fc.capturedUpOpts.Stderr == nil {
		t.Error("expected Up() to be called with a non-nil Stderr writer in verbose mode")
	}
}

func TestDevService_NotVerbose_UpStderrNil(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fc.capturedUpOpts.Stderr != nil {
		t.Error("expected Up() Stderr nil when not verbose")
	}
}

// TestDevService_UnhealthyContainer_EmitsWarningAboveSummary verifies the
// unhealthy-at-start UX: the warning line precedes the startup hints and
// the command still exits 0.
func TestDevService_UnhealthyContainer_EmitsWarningAboveSummary(t *testing.T) {
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Service: "vibewarden", State: "running"},
			{Service: "kratos", State: "running", Health: "unhealthy"},
		},
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	warnIdx := strings.Index(out, "unhealthy")
	summaryIdx := strings.Index(out, "Started.")
	if warnIdx < 0 {
		t.Fatalf("expected warning about unhealthy container, got:\n%s", out)
	}
	if summaryIdx < 0 {
		t.Fatalf("expected startup summary, got:\n%s", out)
	}
	if warnIdx > summaryIdx {
		t.Errorf("warning must appear ABOVE summary; warnIdx=%d, summaryIdx=%d\nout:\n%s",
			warnIdx, summaryIdx, out)
	}
	if !strings.Contains(out, "vibew logs -f kratos") {
		t.Errorf("expected hint for unhealthy service in warning, got:\n%s", out)
	}
}

func TestDevService_LetsencryptWarning(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "letsencrypt"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "tls.provider is 'letsencrypt'") {
		t.Errorf("expected letsencrypt warning in output, got:\n%s", out)
	}
	if !strings.Contains(out, "ACME HTTP-01") {
		t.Errorf("expected ACME HTTP-01 mention in output, got:\n%s", out)
	}
	if !strings.Contains(out, "self-signed") {
		t.Errorf("expected self-signed suggestion in output, got:\n%s", out)
	}
}

func TestDevService_SelfSigned_NoLetsencryptWarning(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "self-signed"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "tls.provider is 'letsencrypt'") {
		t.Errorf("unexpected letsencrypt warning when provider is self-signed:\n%s", out)
	}
}

func TestDevService_TLSDisabled_NoLetsencryptWarning(t *testing.T) {
	fc := &fakeCompose{}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.TLS.Enabled = false
	cfg.TLS.Provider = "letsencrypt"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "tls.provider is 'letsencrypt'") {
		t.Errorf("unexpected letsencrypt warning when TLS is disabled:\n%s", out)
	}
}

func TestDevService_VerifySidecar_Running_PrintsSuccess(t *testing.T) {

	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Name: "proj-vibewarden-1", Service: "vibewarden", State: "running", Health: "healthy"},
		},
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Dev environment started") {
		t.Errorf("expected success message when sidecar is running, got:\n%s", out)
	}
}

func TestDevService_VerifySidecar_Exited_ReturnsError(t *testing.T) {

	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Name: "proj-vibewarden-1", Service: "vibewarden", State: "exited", Health: ""},
		},
		logsResult: "Error: config validation failed: upstream.host is required",
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() expected error when sidecar exited, got nil")
	}
	if !strings.Contains(err.Error(), "sidecar failed to start") {
		t.Errorf("error should mention sidecar failure, got: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "not running") {
		t.Errorf("expected 'not running' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "config validation failed") {
		t.Errorf("expected sidecar logs in output, got:\n%s", out)
	}
	if !strings.Contains(out, "vibew logs or vibew doctor") {
		t.Errorf("expected diagnostic hint in output, got:\n%s", out)
	}
	// Success message should NOT have been printed.
	if strings.Contains(out, "Dev environment started") {
		t.Errorf("success message should not appear when sidecar failed:\n%s", out)
	}
}

func TestDevService_VerifySidecar_Restarting_ReturnsError(t *testing.T) {

	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Name: "proj-vibewarden-1", Service: "vibewarden", State: "restarting", Health: ""},
		},
		logsResult: "panic: runtime error",
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() expected error when sidecar is restarting, got nil")
	}
	if !strings.Contains(err.Error(), "restarting") {
		t.Errorf("error should mention state, got: %v", err)
	}
}

func TestDevService_VerifySidecar_NoSidecarContainer_Succeeds(t *testing.T) {
	// When the sidecar service is not in the compose project (e.g. user-managed
	// compose file), the check should not fail.

	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Name: "proj-app-1", Service: "app", State: "running", Health: "healthy"},
		},
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
}

func TestDevService_VerifySidecar_PSError_DoesNotFail(t *testing.T) {
	// When PS fails (e.g. docker daemon issue), the verification is skipped
	// gracefully — the command should not fail.

	fc := &fakeCompose{
		psErr: errors.New("docker daemon not responding"),
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error when PS fails: %v", err)
	}
}

func TestDevService_VerifySidecar_LogsError_StillReturnsError(t *testing.T) {
	// When the sidecar exited but Logs() also fails, the command should still
	// return an error about the sidecar failure (just without log output).

	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Name: "proj-vibewarden-1", Service: "vibewarden", State: "exited", Health: ""},
		},
		logsErr: errors.New("no such service"),
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() expected error when sidecar exited, got nil")
	}
	if !strings.Contains(err.Error(), "sidecar failed to start") {
		t.Errorf("error should mention sidecar failure, got: %v", err)
	}
}

// --- Container freshness detection tests ---

func TestDevService_Freshness_SkippedWhenNoAppConfigured(t *testing.T) {
	// When neither app.image nor app.build is set, the freshness check is skipped.
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Name: "proj-app-1", Service: "app", State: "running", Image: "old:v1", Project: "wrong"},
		},
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.App.Image = ""
	cfg.App.Build = ""
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	// Restart should NOT have been called.
	if fc.restartCalled != 0 {
		t.Errorf("expected Restart not to be called when no app configured, got %d calls", fc.restartCalled)
	}
}

func TestDevService_Freshness_SkippedWhenPSFails(t *testing.T) {
	// When PS fails, the freshness check is skipped gracefully (same as verifySidecar).
	fc := &fakeCompose{
		psErr: errors.New("daemon not responding"),
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	cfg.App.Build = "."
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error when PS fails: %v", err)
	}
}

func TestDevService_Freshness_FreshContainer_NoRestart(t *testing.T) {
	// A container with matching project, image, and recent creation time is fresh.
	now := time.Now()
	ops.NowFunc = func() time.Time { return now }
	t.Cleanup(func() { ops.NowFunc = time.Now })

	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{
				Name:      "myapp-app-1",
				Service:   "app",
				State:     "running",
				Image:     "myapp:latest",
				Project:   "myapp",
				CreatedAt: now.Add(-1 * time.Hour),
			},
		},
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	cfg.Name = "myapp"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fc.restartCalled != 0 {
		t.Errorf("expected no Restart for fresh container, got %d calls", fc.restartCalled)
	}
}

func TestDevService_Freshness_ProjectMismatch_TriggersRestart(t *testing.T) {
	// A container from a different project triggers a rebuild.
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{
				Name:      "oldproj-app-1",
				Service:   "app",
				State:     "running",
				Image:     "myapp:latest",
				Project:   "oldproj",
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
		},
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	cfg.Name = "myapp"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fc.restartCalled == 0 {
		t.Error("expected Restart to be called for project mismatch")
	}
	out := buf.String()
	if !strings.Contains(out, "project name mismatch") {
		t.Errorf("expected project mismatch diagnostic, got:\n%s", out)
	}
}

func TestDevService_Freshness_ImageMismatch_TriggersRestart(t *testing.T) {
	// A container with a different image triggers a rebuild.
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{
				Name:      "myapp-app-1",
				Service:   "app",
				State:     "running",
				Image:     "myapp:v1",
				Project:   "myapp",
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
		},
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:v2"
	cfg.Name = "myapp"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fc.restartCalled == 0 {
		t.Error("expected Restart to be called for image mismatch")
	}
	out := buf.String()
	if !strings.Contains(out, "image mismatch") {
		t.Errorf("expected image mismatch diagnostic, got:\n%s", out)
	}
}

func TestDevService_Freshness_AgeExceeded_TriggersRestart(t *testing.T) {
	// A container older than MaxContainerAge triggers a rebuild.
	now := time.Now()
	ops.NowFunc = func() time.Time { return now }
	t.Cleanup(func() { ops.NowFunc = time.Now })

	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{
				Name:      "myapp-app-1",
				Service:   "app",
				State:     "running",
				Image:     "myapp:latest",
				Project:   "myapp",
				CreatedAt: now.Add(-13 * time.Hour),
			},
		},
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	cfg.Name = "myapp"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fc.restartCalled == 0 {
		t.Error("expected Restart to be called for stale container")
	}
	out := buf.String()
	if !strings.Contains(out, "container age") {
		t.Errorf("expected age exceeded diagnostic, got:\n%s", out)
	}
}

func TestDevService_Freshness_RestartFails_ReturnsError(t *testing.T) {
	// When Restart fails during freshness rebuild, the error is propagated.
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{
				Name:      "oldproj-app-1",
				Service:   "app",
				State:     "running",
				Image:     "myapp:latest",
				Project:   "oldproj",
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
		},
		restartErr: errors.New("build context missing"),
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	cfg.Name = "myapp"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() expected error when Restart fails, got nil")
	}
	if !strings.Contains(err.Error(), "rebuilding stale app container") {
		t.Errorf("error should mention rebuilding, got: %v", err)
	}
}

func TestDevService_Freshness_NoAppContainer_Proceeds(t *testing.T) {
	// When no "app" service container exists in PS output, the check does not
	// block the compose up flow.
	fc := &fakeCompose{
		psResult: []ports.ContainerInfo{
			{Name: "myapp-vibewarden-1", Service: "vibewarden", State: "running", Image: "vibewarden:latest", Project: "myapp"},
		},
	}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	cfg.App.Image = "myapp:latest"
	cfg.Name = "myapp"
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fc.restartCalled != 0 {
		t.Errorf("expected no Restart when app container is absent, got %d calls", fc.restartCalled)
	}
}
