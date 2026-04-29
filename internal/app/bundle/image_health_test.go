package bundle_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

// TestCheckImageHealth_StaleImage verifies that stale detection works when
// source files change between two WriteInputDigest calls.
func TestCheckImageHealth_StaleImage(t *testing.T) {
	root := t.TempDir()

	// Write a source file and record the digest.
	mainGo := filepath.Join(root, "main.go")
	if err := os.WriteFile(mainGo, []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)

	// Modify the file so the digest will differ on the next check.
	if err := os.WriteFile(mainGo, []byte("package main // changed\n"), 0o600); err != nil {
		t.Fatalf("modify main.go: %v", err)
	}

	inspector := &fakeInspector{
		info: ports.ImageInfo{
			Digest:       "sha256:abc123",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
		},
	}

	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:    "myapp-app:latest",
		ProjectRoot: root,
		Inspector:   inspector,
		Walker:      &fakeStalenessWalker{},
	})
	if err != nil {
		t.Fatalf("CheckImageHealth() error = %v", err)
	}
	if !h.Freshness.Stale {
		t.Error("expected STALE after content change, got FRESH")
	}
	if h.Freshness.Mode != bundleapp.FreshnessModeDigest {
		t.Errorf("Mode = %q, want %q", h.Freshness.Mode, bundleapp.FreshnessModeDigest)
	}
	if h.Freshness.ChangedCount == 0 {
		t.Error("ChangedCount = 0, want > 0")
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

// TestCheckImageHealth_EmptyStringTargetPlatform verifies that an explicit
// empty-string TargetPlatform (the value config.Load returns when the yaml
// contains `deploy.target_platform: ""`) is treated as "use the default".
// This guards the path: yaml empty-string → config.Load returns "" →
// BundleOptions.TargetPlatform = "" → CheckImageHealth falls back to
// defaultTargetPlatform ("linux/amd64"). Without this guard, a future
// refactor removing CheckImageHealth's empty-check would silently accept the
// wrong platform.
func TestCheckImageHealth_EmptyStringTargetPlatform(t *testing.T) {
	inspector := &fakeInspector{
		info: ports.ImageInfo{OS: "linux", Architecture: "amd64"},
	}
	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:       "myapp:latest",
		TargetPlatform: "", // explicit empty — same as yaml `target_platform: ""`
		Inspector:      inspector,
		Walker:         &fakeStalenessWalker{},
	})
	if err != nil {
		t.Fatalf("CheckImageHealth() error = %v", err)
	}
	// Must resolve to linux/amd64, not remain as empty string.
	if h.Target != "linux/amd64" {
		t.Errorf("empty-string target resolved to %q, want %q", h.Target, "linux/amd64")
	}
	// An amd64 image against the resolved amd64 target must not be a mismatch.
	if h.ArchMismatch {
		t.Error("amd64 image vs resolved linux/amd64 target should not be an arch mismatch")
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
		Target: "linux/amd64",
		Freshness: bundleapp.FreshnessVerdict{
			Stale:        true,
			Mode:         bundleapp.FreshnessModeDigest,
			ChangedCount: 2,
			ChangedPaths: []bundleapp.ChangedPath{
				{Path: "main.go", Kind: bundleapp.ChangedPathModified},
				{Path: "go.mod", Kind: bundleapp.ChangedPathModified},
			},
		},
		ArchMismatch: false,
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if !strings.Contains(out, "STALE") {
		t.Errorf("expected STALE in freshness line\noutput:\n%s", out)
	}
	if !strings.Contains(out, "main.go (modified)") {
		t.Errorf("expected changed path in output\noutput:\n%s", out)
	}
	if !strings.Contains(out, "image is stale") {
		t.Errorf("expected stale warning\noutput:\n%s", out)
	}
}

// TestRenderImageHealth_ArchMismatch is a golden test for the arch mismatch case.
// Since #1200, arch mismatch is a hard error (ErrPlatformMismatch) that the
// caller returns before the next bundle step. The rendered health block still
// shows the Arch and Target lines so the user has the context, but there is
// no warning line for the mismatch — the error message carries that information.
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

	// Arch and Target must still appear in the block header lines.
	if !strings.Contains(out, "linux/arm64") {
		t.Errorf("expected image arch linux/arm64 in output\noutput:\n%s", out)
	}
	if !strings.Contains(out, "linux/amd64") {
		t.Errorf("expected target linux/amd64 in output\noutput:\n%s", out)
	}
	// The rebuild command is no longer in the rendered block — it is in the
	// ErrPlatformMismatch error string returned by runImageHealthCheck.
	// Verify it is NOT duplicated here (the caller's error message is the
	// single source of truth).
	if strings.Contains(out, "vibew build --platform") {
		t.Errorf("rendered block should NOT contain rebuild command (it lives in ErrPlatformMismatch)\noutput:\n%s", out)
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
	for _, name := range []string{"sample.env", ".env", "README.md", "MANIFEST.md"} {
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

// TestRunImageHealthCheck_ArchMismatch_ReturnsErrPlatformMismatch verifies
// that runImageHealthCheck (via Bundle) returns ErrPlatformMismatch when the
// image arch does not match the target platform. This is the primary regression
// guard for #1200: Apple Silicon builds landing on amd64 VPS hosts.
func TestRunImageHealthCheck_ArchMismatch_ReturnsErrPlatformMismatch(t *testing.T) {
	mem := newMemBundleFS()
	inspector := &fakeInspector{
		info: ports.ImageInfo{
			OS:           "linux",
			Architecture: "arm64", // Apple Silicon local image
			Created:      fixedTime,
		},
	}
	walker := &fakeStalenessWalker{}

	var out strings.Builder
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageInspector(inspector).
		WithStalenessWalker(walker)

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:         minimalBundleCfg(),
		ConfigPath:     t.TempDir() + "/vibewarden.yaml",
		ProjectName:    "myapp",
		OutputDir:      t.TempDir(),
		ImageTag:       "myapp-app:latest",
		TargetPlatform: "linux/amd64", // amd64 VPS target
		Out:            &out,
	})

	if err == nil {
		t.Fatal("Bundle() expected error on arch mismatch, got nil")
	}
	if !errors.Is(err, bundleapp.ErrPlatformMismatch) {
		t.Errorf("expected ErrPlatformMismatch, got: %v", err)
	}

	// Error message must contain arch and target.
	msg := err.Error()
	if !strings.Contains(msg, "linux/arm64") {
		t.Errorf("error message missing image arch: %s", msg)
	}
	if !strings.Contains(msg, "linux/amd64") {
		t.Errorf("error message missing target: %s", msg)
	}
	if !strings.Contains(msg, "vibew build --platform linux/amd64") {
		t.Errorf("error message missing rebuild command: %s", msg)
	}
	if !strings.Contains(msg, "Then re-run: vibew bundle") {
		t.Errorf("error message missing re-run instruction: %s", msg)
	}

	// Health block must be rendered to opts.Out BEFORE the error is returned.
	rendered := out.String()
	if !strings.Contains(rendered, "Image health") {
		t.Errorf("health block not rendered before mismatch error: %s", rendered)
	}
	if !strings.Contains(rendered, "linux/arm64") {
		t.Errorf("health block missing image arch: %s", rendered)
	}
}

// TestBundle_ArchMismatch_NoFilesWritten verifies that no bundle files are
// written when the image arch does not match the target platform.
func TestBundle_ArchMismatch_NoFilesWritten(t *testing.T) {
	mem := newMemBundleFS()
	inspector := &fakeInspector{
		info: ports.ImageInfo{
			OS:           "linux",
			Architecture: "arm64",
			Created:      fixedTime,
		},
	}
	walker := &fakeStalenessWalker{}

	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageInspector(inspector).
		WithStalenessWalker(walker)

	outDir := t.TempDir()
	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:         minimalBundleCfg(),
		ConfigPath:     t.TempDir() + "/vibewarden.yaml",
		ProjectName:    "myapp",
		OutputDir:      outDir,
		ImageTag:       "myapp-app:latest",
		TargetPlatform: "linux/amd64",
	})

	if err == nil {
		t.Fatal("Bundle() expected error on arch mismatch")
	}
	if !errors.Is(err, bundleapp.ErrPlatformMismatch) {
		t.Errorf("expected ErrPlatformMismatch, got: %v", err)
	}

	// The in-memory FS should have no files (health check aborts before generator).
	if len(mem.files) != 0 {
		t.Errorf("expected no files written on mismatch, got: %v", mem.files)
	}
}

// TestBundle_ArchMatch_Succeeds verifies the happy path: matching arch does
// not trigger ErrPlatformMismatch.
func TestBundle_ArchMatch_Succeeds(t *testing.T) {
	mem := newMemBundleFS()
	inspector := &fakeInspector{
		info: ports.ImageInfo{
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
		},
	}
	walker := &fakeStalenessWalker{}

	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem).
		WithImageInspector(inspector).
		WithStalenessWalker(walker)

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:         minimalBundleCfg(),
		ConfigPath:     t.TempDir() + "/vibewarden.yaml",
		ProjectName:    "myapp",
		OutputDir:      t.TempDir(),
		ImageTag:       "myapp-app:latest",
		TargetPlatform: "linux/amd64",
		SkipImage:      true,
	})

	if err != nil {
		t.Fatalf("Bundle() unexpected error on matching arch: %v", err)
	}
}

// TestBundle_NoInspector_SkipsArchCheck preserves the existing nil-inspector
// path used by tests that predate ADR-089.
func TestBundle_NoInspector_SkipsArchCheck(t *testing.T) {
	// No WithImageInspector — health check is skipped entirely.
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{})

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  t.TempDir() + "/vibewarden.yaml",
		ProjectName: "myapp",
		OutputDir:   t.TempDir(),
		ImageTag:    "myapp-app:latest",
	})

	if err != nil {
		t.Fatalf("Bundle() unexpected error when inspector is nil: %v", err)
	}
}

// TestPlatformMismatchMessage_ExactWording pins the exact error copy for
// #1200. Any change to this message must be reflected in all agent docs.
func TestPlatformMismatchMessage_ExactWording(t *testing.T) {
	inspector := &fakeInspector{
		info: ports.ImageInfo{
			OS:           "linux",
			Architecture: "arm64",
			Created:      fixedTime,
		},
	}
	walker := &fakeStalenessWalker{}

	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(newMemBundleFS()).
		WithImageInspector(inspector).
		WithStalenessWalker(walker)

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		Config:         minimalBundleCfg(),
		ConfigPath:     t.TempDir() + "/vibewarden.yaml",
		ProjectName:    "myapp",
		OutputDir:      t.TempDir(),
		ImageTag:       "myapp-app:latest",
		TargetPlatform: "linux/amd64",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// Pin exact wording substrings that are load-bearing for agents.
	wantSubstrings := []string{
		"image arch is linux/arm64",
		"target is linux/amd64",
		"Rebuild with: vibew build --platform linux/amd64",
		"Then re-run: vibew bundle",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q\nfull message: %s", want, err.Error())
		}
	}
}
