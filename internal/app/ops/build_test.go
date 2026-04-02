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
	err             error
	capturedTag     string
	capturedDir     string
	capturedNoCache bool
}

func (f *fakeBuilder) Build(_ context.Context, tag string, contextDir string, noCache bool) error {
	f.capturedTag = tag
	f.capturedDir = contextDir
	f.capturedNoCache = noCache
	return f.err
}

func TestBuildService_Run_UsesAppImageFromConfig(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{}
	cfg.App.Image = "myapp:v1.2.3"

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if fb.capturedTag != "myapp:v1.2.3" {
		t.Errorf("tag = %q, want %q", fb.capturedTag, "myapp:v1.2.3")
	}

	if !strings.Contains(out.String(), "myapp:v1.2.3") {
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

	if !strings.HasSuffix(fb.capturedTag, ":latest") {
		t.Errorf("tag %q should end with :latest when falling back to dir name", fb.capturedTag)
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

			cfg := &config.Config{}
			cfg.App.Image = "img:latest"

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

	cfg := &config.Config{}
	cfg.App.Image = "myapp:latest"

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

	cfg := &config.Config{}
	cfg.App.Image = "myapp:latest"

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

	cfg := &config.Config{}
	cfg.App.Image = "webapp:1.0"

	var out bytes.Buffer
	err := svc.Run(context.Background(), cfg, ops.BuildOptions{WorkDir: "."}, &out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "webapp:1.0") {
		t.Errorf("success output missing tag: %s", output)
	}
	if !strings.Contains(output, "Successfully built") {
		t.Errorf("success output missing 'Successfully built': %s", output)
	}
}

func TestBuildService_Run_PassesWorkDirToBuilder(t *testing.T) {
	fb := &fakeBuilder{}
	svc := ops.NewBuildService(fb)

	cfg := &config.Config{}
	cfg.App.Image = "myapp:latest"

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
