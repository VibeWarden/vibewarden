package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// fakeBuilder is a test double for ports.DockerBuilder.
type fakeBuilder struct {
	err              error
	capturedTag      string
	capturedDir      string
	capturedNoCache  bool
	capturedPlatform string
}

func (f *fakeBuilder) Build(_ context.Context, tag string, contextDir string, noCache bool, platform string) error {
	f.capturedTag = tag
	f.capturedDir = contextDir
	f.capturedNoCache = noCache
	f.capturedPlatform = platform
	return f.err
}

func TestBuildService_Run_UsesComposeProjectNameFromConfig(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{}
	cfg.App.Image = "myapp:v1.2.3"

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	// ComposeProjectName() strips the tag from App.Image, giving "myapp".
	// resolveImageTag uses that to produce "myapp-app:latest" which matches
	// what docker-compose expects for the app service.
	wantTag := "myapp-app:latest"
	if fb.capturedTag != wantTag {
		t.Errorf("tag = %q, want %q", fb.capturedTag, wantTag)
	}

	if !strings.Contains(out.String(), wantTag) {
		t.Errorf("output missing image tag: %s", out.String())
	}
}

func TestBuildService_Run_FallsBackToDirectoryName(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	// Use a temp dir whose name we know.
	dir := t.TempDir()

	var out bytes.Buffer
	err := svc.Run(context.Background(), nil, ops.BuildOptions{WorkDir: dir}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if !strings.HasSuffix(fb.capturedTag, "-app:latest") {
		t.Errorf("tag %q should end with -app:latest when falling back to dir name", fb.capturedTag)
	}
}

func TestBuildService_Run_NilConfigFallsBackToDirName(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	dir := t.TempDir()

	var out bytes.Buffer
	err := svc.Run(context.Background(), nil, ops.BuildOptions{WorkDir: dir}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fb.capturedTag == "" {
		t.Error("expected a non-empty tag")
	}
}

func TestBuildService_Run_PassesNoCache(t *testing.T) {
	tests := []struct {
		name    string
		noCache bool
	}{
		{"no-cache false", false},
		{"no-cache true", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &fakeBuilder{}
			svc := ops.NewBuildService(fb)

			cfg := &config.Config{Name: "img"}

			var out bytes.Buffer
			err := svc.Run(context.Background(), cfg, ops.BuildOptions{
				NoCache: tt.noCache,
				WorkDir: ".",
			}, &out)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			if fb.capturedNoCache != tt.noCache {
				t.Errorf("noCache = %v, want %v", fb.capturedNoCache, tt.noCache)
			}
		})
	}
}

func TestBuildService_Run_NoCachePrintedInOutput(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{Name: "myapp"}

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{
		NoCache: true,
		WorkDir: ".",
	}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "--no-cache") {
		t.Errorf("output missing --no-cache flag indication: %s", out.String())
	}
}

func TestBuildService_Run_ReturnsBuilderError(t *testing.T) {
	want := errors.New("docker not found")
	fb := &fakeBuilder{err: want}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{Name: "myapp"}

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, want) {
		t.Errorf("error = %v, want to wrap %v", err, want)
	}
}

func TestBuildService_Run_SuccessOutputContainsTag(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{Name: "webapp"}

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "webapp-app:latest") {
		t.Errorf("success output missing tag: %s", output)
	}
	if !strings.Contains(output, "Successfully built") {
		t.Errorf("success output missing 'Successfully built': %s", output)
	}
}

func TestBuildService_Run_PassesWorkDirToBuilder(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{Name: "myapp"}

	dir := t.TempDir()

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: dir}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if fb.capturedDir != dir {
		t.Errorf("contextDir = %q, want %q", fb.capturedDir, dir)
	}
}

func TestBuildService_Run_PassesPlatformToBuilder(t *testing.T) {
	tests := []struct {
		name     string
		platform string
	}{
		{"empty platform", ""},
		{"linux/amd64", "linux/amd64"},
		{"linux/arm64", "linux/arm64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &fakeBuilder{}
			svc := ops.NewBuildService(fb)

			cfg := &config.Config{}
			cfg.App.Image = "img:latest"

			var out bytes.Buffer
			err := svc.Run(context.Background(), cfg, ops.BuildOptions{
				Platform: tt.platform,
				WorkDir:  ".",
			}, &out)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			if fb.capturedPlatform != tt.platform {
				t.Errorf("platform = %q, want %q", fb.capturedPlatform, tt.platform)
			}
		})
	}
}

func TestBuildService_Run_PlatformPrintedInOutput(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{}
	cfg.App.Image = "myapp:latest"

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{
		Platform: "linux/amd64",
		WorkDir:  ".",
	}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "linux/amd64") {
		t.Errorf("output missing platform indication: %s", out.String())
	}
}

// fakeShellProber is a test double for ports.DockerShellProber.
type fakeShellProber struct {
	hasShell bool
	err      error
}

func (f *fakeShellProber) HasShell(_ context.Context, _ string) (bool, error) {
	return f.hasShell, f.err
}

func TestBuildService_ShellProber_NoShellWarning(t *testing.T) {
	fb := &fakeBuilder{}
	fp := &fakeShellProber{hasShell: false}
	svc := ops.NewBuildService(fb).WithShellProber(fp)

	cfg := &config.Config{Name: "myapp"}

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Warning: your app image has no shell") {
		t.Errorf("expected shell warning in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Alpine-based") {
		t.Errorf("expected Alpine suggestion in output, got:\n%s", output)
	}
}

func TestBuildService_ShellProber_HasShell_NoWarning(t *testing.T) {
	fb := &fakeBuilder{}
	fp := &fakeShellProber{hasShell: true}
	svc := ops.NewBuildService(fb).WithShellProber(fp)

	cfg := &config.Config{Name: "myapp"}

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	output := out.String()
	if strings.Contains(output, "Warning: your app image has no shell") {
		t.Errorf("unexpected shell warning in output:\n%s", output)
	}
}

func TestBuildService_ShellProber_Error_NoWarning(t *testing.T) {
	fb := &fakeBuilder{}
	fp := &fakeShellProber{err: errors.New("docker daemon unreachable")}
	svc := ops.NewBuildService(fb).WithShellProber(fp)

	cfg := &config.Config{Name: "myapp"}

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	output := out.String()
	if strings.Contains(output, "Warning: your app image has no shell") {
		t.Errorf("unexpected shell warning when prober errors:\n%s", output)
	}
}

func TestBuildService_ShellProber_NilProber_NoWarning(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb) // no prober wired

	cfg := &config.Config{Name: "myapp"}

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	output := out.String()
	if strings.Contains(output, "Warning: your app image has no shell") {
		t.Errorf("unexpected shell warning when no prober wired:\n%s", output)
	}
}

// TestBuildService_Run_ImageNameMatchesComposeProjectName verifies that the
// image tag produced by `vibew build` matches what docker-compose expects:
// <ComposeProjectName>-app:latest.
//
// Regression test for #973.
func TestBuildService_Run_ImageNameMatchesComposeProjectName(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantTag string
	}{
		{
			name:    "explicit name in config",
			cfg:     &config.Config{Name: "myapp"},
			wantTag: "myapp-app:latest",
		},
		{
			name:    "image-derived name (tag stripped)",
			cfg:     &config.Config{App: config.AppConfig{Image: "webapp:v2.0"}},
			wantTag: "webapp-app:latest",
		},
		{
			name:    "image with registry prefix",
			cfg:     &config.Config{App: config.AppConfig{Image: "ghcr.io/org/service:latest"}},
			wantTag: "service-app:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &fakeBuilder{}
			svc := ops.NewBuildService(fb)

			var out bytes.Buffer
			err := svc.Run(context.Background(), tt.cfg, ops.BuildOptions{WorkDir: "."}, &out)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			if fb.capturedTag != tt.wantTag {
				t.Errorf("tag = %q, want %q", fb.capturedTag, tt.wantTag)
			}
		})
	}
}
