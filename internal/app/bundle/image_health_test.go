package bundle_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fixedTime is a stable timestamp used in all golden tests so rendered output
// stays deterministic regardless of when the test runs.
var fixedTime = time.Date(2026, 4, 19, 14, 2, 11, 0, time.UTC)

// fakeInspector is a test double for ports.ImageInspector.
type fakeInspector struct {
	info ports.ImageInfo
	err  error
}

func (f *fakeInspector) Inspect(_ context.Context, tag string) (ports.ImageInfo, error) {
	if f.err != nil {
		return ports.ImageInfo{}, f.err
	}
	info := f.info
	info.Tag = tag
	return info, nil
}

// fakeStalenessWalker is a test double for bundle.StalenessWalker.
type fakeStalenessWalker struct {
	newest       time.Time
	changedCount int
	err          error
}

func (f *fakeStalenessWalker) NewestMTime(_ string, _ time.Time) (time.Time, int, error) {
	return f.newest, f.changedCount, f.err
}

// TestCheckImageHealth_ErrImageNotFound verifies that ErrImageNotFound from
// the inspector propagates unwrapped so the CLI can map it to exit code 2.
func TestCheckImageHealth_ErrImageNotFound(t *testing.T) {
	inspector := &fakeInspector{err: ports.ErrImageNotFound}
	_, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:  "missing:latest",
		Inspector: inspector,
		Walker:    &fakeStalenessWalker{},
	})
	if !errors.Is(err, ports.ErrImageNotFound) {
		t.Errorf("expected ErrImageNotFound, got %v", err)
	}
}

// TestCheckImageHealth_ErrDockerUnavailable verifies that ErrDockerUnavailable
// propagates unwrapped so the CLI can map it to exit code 3.
func TestCheckImageHealth_ErrDockerUnavailable(t *testing.T) {
	inspector := &fakeInspector{err: ports.ErrDockerUnavailable}
	_, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:  "myapp:latest",
		Inspector: inspector,
		Walker:    &fakeStalenessWalker{},
	})
	if !errors.Is(err, ports.ErrDockerUnavailable) {
		t.Errorf("expected ErrDockerUnavailable, got %v", err)
	}
}

// TestCheckImageHealth_FreshNoWarnings verifies a happy-path health result
// with no warnings.
func TestCheckImageHealth_FreshNoWarnings(t *testing.T) {
	inspector := &fakeInspector{
		info: ports.ImageInfo{
			Digest:       "sha256:abc123",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
			SizeBytes:    160 * 1024 * 1024,
		},
	}
	walker := &fakeStalenessWalker{changedCount: 0}

	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:       "myapp-app:latest",
		TargetPlatform: "linux/amd64",
		Inspector:      inspector,
		Walker:         walker,
	})
	if err != nil {
		t.Fatalf("CheckImageHealth() error = %v", err)
	}
	if h.Freshness.Stale {
		t.Error("expected FRESH, got STALE")
	}
	if h.ArchMismatch {
		t.Error("expected no arch mismatch")
	}
	if h.LegacyTag {
		t.Error("expected LegacyTag=false for project-scoped tag")
	}
	if h.Target != "linux/amd64" {
		t.Errorf("Target = %q, want %q", h.Target, "linux/amd64")
	}
}

// TestCheckImageHealth_StaleImage verifies that stale detection works.
func TestCheckImageHealth_StaleImage(t *testing.T) {
	inspector := &fakeInspector{
		info: ports.ImageInfo{
			Digest:       "sha256:abc123",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
		},
	}
	walker := &fakeStalenessWalker{changedCount: 12, newest: fixedTime.Add(time.Hour)}

	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:    "myapp-app:latest",
		ProjectRoot: "/fake/project/root", // non-empty so walker is invoked
		Inspector:   inspector,
		Walker:      walker,
	})
	if err != nil {
		t.Fatalf("CheckImageHealth() error = %v", err)
	}
	if !h.Freshness.Stale {
		t.Error("expected STALE, got FRESH")
	}
	if h.Freshness.ChangedCount != 12 {
		t.Errorf("ChangedCount = %d, want 12", h.Freshness.ChangedCount)
	}
}

// TestCheckImageHealth_ArchMismatch verifies arch mismatch detection.
func TestCheckImageHealth_ArchMismatch(t *testing.T) {
	inspector := &fakeInspector{
		info: ports.ImageInfo{
			OS:           "linux",
			Architecture: "arm64", // arm64 image
		},
	}
	walker := &fakeStalenessWalker{}

	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:       "myapp-app:latest",
		TargetPlatform: "linux/amd64", // amd64 target
		Inspector:      inspector,
		Walker:         walker,
	})
	if err != nil {
		t.Fatalf("CheckImageHealth() error = %v", err)
	}
	if !h.ArchMismatch {
		t.Error("expected ArchMismatch=true for arm64 image vs amd64 target")
	}
}

// TestCheckImageHealth_LegacyTag verifies legacy tag detection.
func TestCheckImageHealth_LegacyTag(t *testing.T) {
	inspector := &fakeInspector{
		info: ports.ImageInfo{OS: "linux", Architecture: "amd64"},
	}
	walker := &fakeStalenessWalker{}

	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:       "vibewarden-app:latest", // legacy generic tag
		TargetPlatform: "linux/amd64",
		Inspector:      inspector,
		Walker:         walker,
	})
	if err != nil {
		t.Fatalf("CheckImageHealth() error = %v", err)
	}
	if !h.LegacyTag {
		t.Error("expected LegacyTag=true for vibewarden-app:latest")
	}
}

// TestCheckImageHealth_DefaultTargetPlatform verifies the default target is linux/amd64.
func TestCheckImageHealth_DefaultTargetPlatform(t *testing.T) {
	inspector := &fakeInspector{
		info: ports.ImageInfo{OS: "linux", Architecture: "amd64"},
	}
	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:  "myapp:latest",
		Inspector: inspector,
		Walker:    &fakeStalenessWalker{},
		// TargetPlatform intentionally empty — should default to linux/amd64
	})
	if err != nil {
		t.Fatalf("CheckImageHealth() error = %v", err)
	}
	if h.Target != "linux/amd64" {
		t.Errorf("default target = %q, want %q", h.Target, "linux/amd64")
	}
}

// TestRenderImageHealth_FreshNoWarnings is a golden test for the all-good case.
func TestRenderImageHealth_FreshNoWarnings(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "myapp-app:latest",
			Digest:       "sha256:abc123def456",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
			SizeBytes:    162 * 1024 * 1024,
		},
		Target:       "linux/amd64",
		Freshness:    bundleapp.FreshnessVerdict{Stale: false},
		ArchMismatch: false,
		LegacyTag:    false,
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	for _, want := range []string{
		"Image health",
		"Tag:          myapp-app:latest",
		"Digest:       sha256:abc123def456",
		"Arch:         linux/amd64",
		"Target:       linux/amd64",
		"Freshness:    FRESH",
		"Warnings:     none",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q\noutput:\n%s", want, out)
		}
	}
}

// TestRenderImageHealth_StaleWithWarning is a golden test for the stale case.
func TestRenderImageHealth_StaleWithWarning(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "myapp-app:latest",
			Digest:       "sha256:abc123",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
			SizeBytes:    50 * 1024 * 1024,
		},
		Target:       "linux/amd64",
		Freshness:    bundleapp.FreshnessVerdict{Stale: true, ChangedCount: 7},
		ArchMismatch: false,
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if !strings.Contains(out, "STALE") {
		t.Errorf("expected STALE in freshness line\noutput:\n%s", out)
	}
	if !strings.Contains(out, "7 source files") {
		t.Errorf("expected changed count in output\noutput:\n%s", out)
	}
	if !strings.Contains(out, "image is stale") {
		t.Errorf("expected stale warning\noutput:\n%s", out)
	}
}

// TestRenderImageHealth_ArchMismatch is a golden test for the arch mismatch case.
func TestRenderImageHealth_ArchMismatch(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "myapp-app:latest",
			Digest:       "sha256:abc123",
			OS:           "linux",
			Architecture: "arm64",
			Created:      fixedTime,
			SizeBytes:    100 * 1024 * 1024,
		},
		Target:       "linux/amd64",
		ArchMismatch: true,
		Freshness:    bundleapp.FreshnessVerdict{Stale: false},
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if !strings.Contains(out, "linux/arm64") {
		t.Errorf("expected image arch linux/arm64 in output\noutput:\n%s", out)
	}
	if !strings.Contains(out, "linux/amd64") {
		t.Errorf("expected target linux/amd64 in output\noutput:\n%s", out)
	}
	if !strings.Contains(out, "vibew build --platform linux/amd64") {
		t.Errorf("expected rebuild command in warning\noutput:\n%s", out)
	}
}

// TestRenderImageHealth_AllowStale verifies --allow-stale suppresses the STALE label.
func TestRenderImageHealth_AllowStale(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "myapp-app:latest",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
		},
		Target:     "linux/amd64",
		Freshness:  bundleapp.FreshnessVerdict{Stale: true, ChangedCount: 3},
		AllowStale: true,
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if strings.Contains(out, "STALE — ") {
		t.Errorf("STALE label should be suppressed with AllowStale=true\noutput:\n%s", out)
	}
	if strings.Contains(out, "image is stale") {
		t.Errorf("stale warning should be suppressed with AllowStale=true\noutput:\n%s", out)
	}
	if !strings.Contains(out, "stale suppressed") {
		t.Errorf("expected suppression note in freshness label\noutput:\n%s", out)
	}
}

// TestRenderImageHealth_LegacyTagWarning verifies the legacy tag warning.
func TestRenderImageHealth_LegacyTagWarning(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "vibewarden-app:latest",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
		},
		Target:    "linux/amd64",
		LegacyTag: true,
		Freshness: bundleapp.FreshnessVerdict{Stale: false},
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if !strings.Contains(out, "legacy generic tag") {
		t.Errorf("expected legacy tag warning\noutput:\n%s", out)
	}
	if !strings.Contains(out, "vibewarden-app:latest") {
		t.Errorf("expected legacy tag name in warning\noutput:\n%s", out)
	}
}

// TestRenderImageHealth_FormatStable verifies that the block always starts
// with "Image health" and always ends with a warnings line (either "none"
// or a list entry).
func TestRenderImageHealth_FormatStable(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "test-app:latest",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
		},
		Target:    "linux/amd64",
		Freshness: bundleapp.FreshnessVerdict{},
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if !strings.HasPrefix(out, "Image health\n") {
		t.Errorf("block must start with 'Image health\\n'\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Warnings:") {
		t.Errorf("block must always contain 'Warnings:' line\noutput:\n%s", out)
	}
}

// TestBundle_ImageMissing_NoFilesWritten verifies that when the inspector
// returns ErrImageNotFound, no bundle files are written and ErrImageMissing
// is returned.
func TestBundle_ImageMissing_NoFilesWritten(t *testing.T) {
	mem := newMemBundleFS()
	inspector := &fakeInspector{err: ports.ErrImageNotFound}
	walker := &fakeStalenessWalker{}

	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageInspector(inspector).
		WithStalenessWalker(walker)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  t.TempDir() + "/vibewarden.yaml",
		ProjectName: "myapp",
		OutputDir:   outDir,
		ImageTag:    "myapp-app:latest",
		SkipImage:   false, // inspector is wired, so health check runs
	})

	if err == nil {
		t.Fatal("expected error when image is missing")
	}
	if !errors.Is(err, bundleapp.ErrImageMissing) {
		t.Errorf("expected ErrImageMissing, got: %v", err)
	}

	// No extra files should have been written (only potentially sample.env etc
	// are written by extras after the health check — but health check aborts first).
	for _, name := range []string{"sample.env", ".env", "deploy.sh", "README.md"} {
		if _, ok := mem.files[t.TempDir()+"/"+name]; ok {
			t.Errorf("file %s should not be written when image is missing", name)
		}
	}
}

// TestBundle_HealthBlockEmittedOnce verifies the health block is written to
// opts.Out exactly once per Bundle() call.
func TestBundle_HealthBlockEmittedOnce(t *testing.T) {
	mem := newMemBundleFS()
	inspector := &fakeInspector{
		info: ports.ImageInfo{
			OS: "linux", Architecture: "amd64",
			Created: fixedTime,
		},
	}
	walker := &fakeStalenessWalker{}

	var out strings.Builder
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageInspector(inspector).
		WithStalenessWalker(walker)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  t.TempDir() + "/vibewarden.yaml",
		ProjectName: "myapp",
		OutputDir:   outDir,
		ImageTag:    "myapp-app:latest",
		SkipImage:   true,
		Out:         &out,
	})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}

	rendered := out.String()
	count := strings.Count(rendered, "Image health")
	if count != 1 {
		t.Errorf("'Image health' block emitted %d times, want exactly 1\noutput:\n%s", count, rendered)
	}
}
