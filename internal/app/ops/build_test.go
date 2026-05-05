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

// fakeBuilder is a test double for ports.DockerBuilder.
type fakeBuilder struct {
	err          error
	capturedTag  string
	capturedDir  string
	capturedOpts ports.DockerBuildOptions
}

func (f *fakeBuilder) Build(_ context.Context, tag string, contextDir string, opts ports.DockerBuildOptions) error {
	f.capturedTag = tag
	f.capturedDir = contextDir
	f.capturedOpts = opts
	return f.err
}

func TestBuildService_Run_UsesComposeProjectNameFromConfig(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	// Use cfg.Name — the canonical resolver since v0.18.2 (#1199).
	// App.Image is no longer used to derive the project name.
	cfg := &config.Config{Name: "myapp"}

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	// ComposeProjectName() uses cfg.Name, giving "myapp".
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

			if fb.capturedOpts.NoCache != tt.noCache {
				t.Errorf("noCache = %v, want %v", fb.capturedOpts.NoCache, tt.noCache)
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

			if fb.capturedOpts.Platform != tt.platform {
				t.Errorf("platform = %q, want %q", fb.capturedOpts.Platform, tt.platform)
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

// TestBuildService_Run_UsesPreResolvedImageTag verifies that when
// BuildOptions.ImageTag is set the BuildService uses it verbatim and does NOT
// run its own resolveImageTag chain. This short-circuit lets vibew bundle --build
// pass the already-resolved tag so the build step and the bundle lookup always agree.
func TestBuildService_Run_UsesPreResolvedImageTag(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		preResolved string
		wantTag     string
	}{
		{
			name:        "pre-resolved tag wins over cfg.Name",
			cfg:         &config.Config{Name: "other-name"},
			preResolved: "qr-dali-app:latest",
			wantTag:     "qr-dali-app:latest",
		},
		{
			name:        "pre-resolved tag wins over cfg.App.Image",
			cfg:         &config.Config{App: config.AppConfig{Image: "ghcr.io/org/myapp:v1"}},
			preResolved: "qr-dali-app:latest",
			wantTag:     "qr-dali-app:latest",
		},
		{
			name:        "pre-resolved tag wins when cfg is nil",
			cfg:         nil,
			preResolved: "qr-dali-app:latest",
			wantTag:     "qr-dali-app:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &fakeBuilder{}
			svc := ops.NewBuildService(fb)

			var out bytes.Buffer
			err := svc.Run(context.Background(), tt.cfg, ops.BuildOptions{
				WorkDir:  ".",
				ImageTag: tt.preResolved,
			}, &out)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			if fb.capturedTag != tt.wantTag {
				t.Errorf("tag = %q, want %q", fb.capturedTag, tt.wantTag)
			}
		})
	}
}

// TestBuildService_Run_ImageNameMatchesComposeProjectName verifies that the
// image tag produced by `vibew build` matches what docker-compose expects:
// <ComposeProjectName>-app:latest.
//
// Since v0.18.2 (#1199), App.Image is no longer part of the derivation chain.
// The canonical resolver is: cfg.Name → dirname(cfg.ProjectRoot or workDir) →
// dirname(workDir) fallback.
//
// Regression test for #973.
func TestBuildService_Run_ImageNameMatchesComposeProjectName(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		cfg     *config.Config
		workDir string
		wantTag string
	}{
		{
			name:    "explicit name in config",
			cfg:     &config.Config{Name: "myapp"},
			workDir: dir,
			wantTag: "myapp-app:latest",
		},
		{
			// App.Image is no longer used for project-name derivation.
			// dirname(workDir) is used as the fallback.
			name:    "image-derived name (tag stripped) — now uses dirname fallback",
			cfg:     &config.Config{App: config.AppConfig{Image: "webapp:v2.0"}},
			workDir: filepath.Join(dir, "webapp-project"),
			wantTag: "webapp-project-app:latest",
		},
		{
			// App.Image with registry prefix — same: dirname is now authoritative.
			name:    "image with registry prefix — now uses dirname fallback",
			cfg:     &config.Config{App: config.AppConfig{Image: "ghcr.io/org/service:latest"}},
			workDir: filepath.Join(dir, "my-service"),
			wantTag: "my-service-app:latest",
		},
	}

	// Pre-create subdirectories used as workDir.
	for _, d := range []string{
		filepath.Join(dir, "webapp-project"),
		filepath.Join(dir, "my-service"),
	} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &fakeBuilder{}
			svc := ops.NewBuildService(fb)

			var out bytes.Buffer
			err := svc.Run(context.Background(), tt.cfg, ops.BuildOptions{WorkDir: tt.workDir}, &out)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			if fb.capturedTag != tt.wantTag {
				t.Errorf("tag = %q, want %q", fb.capturedTag, tt.wantTag)
			}
		})
	}
}

// --- Label stamping tests (ADR-100) ---

func TestBuildService_Run_StampsProjectRootLabels(t *testing.T) {
	dir := t.TempDir()
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{Name: "myapp", ProjectRoot: dir}

	var out bytes.Buffer
	if err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: dir}, &out); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	labels := fb.capturedOpts.Labels
	if labels == nil {
		t.Fatal("expected non-nil labels in BuildOptions")
	}

	hashVal, ok := labels[ops.LabelProjectRootHash]
	if !ok {
		t.Errorf("labels missing key %q; got: %v", ops.LabelProjectRootHash, labels)
	}
	if !strings.HasPrefix(hashVal, "sha256:") {
		t.Errorf("hash label %q must start with 'sha256:'", hashVal)
	}

	if _, ok := labels[ops.LabelProjectRoot]; !ok {
		t.Errorf("labels missing key %q; got: %v", ops.LabelProjectRoot, labels)
	}
}

func TestBuildService_Run_LabelHashMatchesProjectRoot(t *testing.T) {
	dir := t.TempDir()
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{Name: "myapp", ProjectRoot: dir}

	var out bytes.Buffer
	if err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: dir}, &out); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	wantHash, _, err := ops.ProjectRootHash(dir)
	if err != nil {
		t.Fatalf("ProjectRootHash() error: %v", err)
	}

	got := fb.capturedOpts.Labels[ops.LabelProjectRootHash]
	if got != wantHash {
		t.Errorf("label hash = %q, want %q", got, wantHash)
	}
}

func TestBuildService_Run_NilCfg_StampsLabelsFromWorkDir(t *testing.T) {
	dir := t.TempDir()
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	var out bytes.Buffer
	if err := svc.Run(context.Background(), nil, ops.BuildOptions{WorkDir: dir}, &out); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	labels := fb.capturedOpts.Labels
	if labels == nil {
		t.Fatal("expected non-nil labels when cfg is nil (uses WorkDir as projectRoot)")
	}
	if _, ok := labels[ops.LabelProjectRootHash]; !ok {
		t.Errorf("labels missing key %q when cfg is nil", ops.LabelProjectRootHash)
	}
}

func TestBuildService_Run_PreResolvedTag_StillStampsLabels(t *testing.T) {
	dir := t.TempDir()
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{ProjectRoot: dir}

	var out bytes.Buffer
	if err := svc.Run(context.Background(), cfg, ops.BuildOptions{
		WorkDir:  dir,
		ImageTag: "pre-resolved-tag:latest",
	}, &out); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if fb.capturedTag != "pre-resolved-tag:latest" {
		t.Errorf("tag = %q, want pre-resolved-tag:latest", fb.capturedTag)
	}
	if fb.capturedOpts.Labels == nil {
		t.Fatal("expected labels to be stamped even with pre-resolved tag")
	}
}
