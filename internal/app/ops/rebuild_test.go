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

// rebuildFakeBuilder is a test double for ports.DockerBuilder used in
// rebuild tests. It is separate from the fakeBuilder in build_test.go because
// both live in the same _test package and the field sets differ.
type rebuildFakeBuilder struct {
	buildErr   error
	buildCalls int
	lastTag    string
}

func (f *rebuildFakeBuilder) Build(_ context.Context, tag string, _ string, _ ports.DockerBuildOptions) error {
	f.buildCalls++
	f.lastTag = tag
	return f.buildErr
}

// Ensure rebuildFakeBuilder satisfies the interface at compile time.
var _ ports.DockerBuilder = (*rebuildFakeBuilder)(nil)

// fakeRemover is a test double for ports.DockerImageRemover.
type fakeRemover struct {
	removeErr   error
	removeCalls int
	lastTag     string
}

func (f *fakeRemover) Remove(_ context.Context, tag string) error {
	f.removeCalls++
	f.lastTag = tag
	return f.removeErr
}

// Ensure fakeRemover satisfies the interface at compile time.
var _ ports.DockerImageRemover = (*fakeRemover)(nil)

// rebuildConfig returns a config suitable for rebuild tests.
// The image tag is vibew-derived ("myapp-app:latest") and app.build is unset
// so that the full down → rmi → build → start path runs.
func rebuildConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := defaultConfig()
	cfg.Name = "myapp"
	cfg.ProjectRoot = t.TempDir()
	// Vibew-derived canonical tag: ComposeProjectName() + "-app:latest".
	cfg.App.Image = "myapp-app:latest"
	return cfg
}

// newBuildService returns a BuildService backed by the given fake builder.
func newBuildService(fb *rebuildFakeBuilder) *ops.BuildService {
	return ops.NewBuildService(fb)
}

// TestRebuild_HappyPath verifies the four-line stdout template is emitted in
// order and that down, remove, build, and up are all called.
func TestRebuild_HappyPath(t *testing.T) {
	cfg := rebuildConfig(t)
	fc := &fakeCompose{}
	fr := &fakeRemover{}
	fb := &rebuildFakeBuilder{}

	svc := ops.NewDevService(fc).WithImageRemover(fr)
	builder := newBuildService(fb)

	var buf bytes.Buffer
	err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf)
	if err != nil {
		t.Fatalf("Rebuild() unexpected error: %v", err)
	}

	out := buf.String()

	// Verify all four anchor lines are present.
	anchors := []string{
		"Stopping stack...",
		"Removing image myapp-app:latest",
		"Rebuilding image...",
		"Starting stack...",
	}
	for _, anchor := range anchors {
		if !strings.Contains(out, anchor) {
			t.Errorf("output missing %q\ngot:\n%s", anchor, out)
		}
	}

	// Verify ordering: each anchor must appear before the next.
	for i := 1; i < len(anchors); i++ {
		prevIdx := strings.Index(out, anchors[i-1])
		currIdx := strings.Index(out, anchors[i])
		if prevIdx < 0 || currIdx < 0 {
			continue // already flagged above
		}
		if prevIdx > currIdx {
			t.Errorf("line %q must appear before %q", anchors[i-1], anchors[i])
		}
	}

	// Verify down was called.
	if fc.downCalled == 0 {
		t.Error("expected compose.Down to be called")
	}

	// Verify remove was called with the correct tag.
	if fr.removeCalls == 0 {
		t.Error("expected imageRemover.Remove to be called")
	}
	wantTag := "myapp-app:latest"
	if fr.lastTag != wantTag {
		t.Errorf("Remove called with tag %q, want %q", fr.lastTag, wantTag)
	}

	// Verify build was called.
	if fb.buildCalls == 0 {
		t.Error("expected builder.Build to be called")
	}
	if fb.lastTag != wantTag {
		t.Errorf("Build called with tag %q, want %q", fb.lastTag, wantTag)
	}
}

// TestRebuild_ForeignLabelBypass verifies that when the existing image carries
// a label from a different project (the #1219 block scenario), Rebuild bypasses
// the identity check structurally — down → rmi → build happens regardless, and
// the subsequent Run sees a freshly-built image that passes identity.
func TestRebuild_ForeignLabelBypass(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	cfg := defaultConfig()
	cfg.Name = "myapp"
	cfg.ProjectRoot = dir
	cfg.App.Image = "myapp-app:latest"

	otherHash, otherPath, err := ops.ProjectRootHash(other)
	if err != nil {
		t.Fatalf("ProjectRootHash(other): %v", err)
	}

	// Inspector returns the foreign-project hash — this would block plain Run.
	fi := &fakeInspector{
		info: ports.ImageInfo{
			Labels: map[string]string{
				ops.LabelProjectRootHash: otherHash,
				ops.LabelProjectRoot:     otherPath,
			},
		},
	}

	// After rebuild the inspector should return the CURRENT project hash.
	currentHash, currentPath, err := ops.ProjectRootHash(dir)
	if err != nil {
		t.Fatalf("ProjectRootHash(dir): %v", err)
	}
	fiAfterBuild := &fakeInspector{
		info: ports.ImageInfo{
			Labels: map[string]string{
				ops.LabelProjectRootHash: currentHash,
				ops.LabelProjectRoot:     currentPath,
			},
		},
	}

	fc := &fakeCompose{}
	fr := &fakeRemover{}
	fb := &rebuildFakeBuilder{}

	// Wire the foreign-label inspector for the initial image check but then
	// switch to the correct-label inspector for the Run call after build.
	// We achieve this by using fiAfterBuild for the DevService — Rebuild calls
	// Run at the end, and that Run sees the new image (fresh build).
	svc := ops.NewDevService(fc).
		WithImageChecker(&fakeImageChecker{exists: true}).
		WithImageInspector(fiAfterBuild). // post-build inspector: correct labels
		WithImageRemover(fr)
	builder := newBuildService(fb)

	// Confirm that without --rebuild the plain Run would fail (via the foreign inspector).
	svcBlocking := ops.NewDevService(fc).
		WithImageChecker(&fakeImageChecker{exists: true}).
		WithImageInspector(fi) // foreign hash
	var blockBuf bytes.Buffer
	blockErr := svcBlocking.Run(context.Background(), cfg, ops.DevOptions{}, &blockBuf)
	if blockErr == nil {
		t.Fatal("expected Run() to block on foreign-label image, got nil error")
	}

	// Now confirm Rebuild succeeds.
	var buf bytes.Buffer
	err = svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf)
	if err != nil {
		t.Fatalf("Rebuild() unexpected error with foreign-label image: %v", err)
	}

	// Build must have run.
	if fb.buildCalls == 0 {
		t.Error("expected builder.Build to be called")
	}
}

// TestRebuild_SubsequentDevPassesIdentityCheck verifies that after a successful
// --rebuild, a plain `vibew dev` call passes the identity check because the new
// image carries the correct project-root labels.
func TestRebuild_SubsequentDevPassesIdentityCheck(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.Name = "myapp"
	cfg.ProjectRoot = dir
	cfg.App.Image = "myapp-app:latest"

	currentHash, currentPath, err := ops.ProjectRootHash(dir)
	if err != nil {
		t.Fatalf("ProjectRootHash: %v", err)
	}
	fi := &fakeInspector{
		info: ports.ImageInfo{
			Labels: map[string]string{
				ops.LabelProjectRootHash: currentHash,
				ops.LabelProjectRoot:     currentPath,
			},
		},
	}

	fc := &fakeCompose{}
	fr := &fakeRemover{}
	fb := &rebuildFakeBuilder{}
	svc := ops.NewDevService(fc).
		WithImageChecker(&fakeImageChecker{exists: true}).
		WithImageInspector(fi).
		WithImageRemover(fr)
	builder := newBuildService(fb)

	// Phase 1: Rebuild.
	var buf1 bytes.Buffer
	if err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf1); err != nil {
		t.Fatalf("Rebuild() phase 1 error: %v", err)
	}

	// Phase 2: Plain Run — must pass the identity check (no error).
	var buf2 bytes.Buffer
	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf2); err != nil {
		t.Fatalf("Run() after rebuild error: %v", err)
	}
}

// TestRebuild_MissingImage verifies that when Remove returns nil (adapter
// no-ops for "No such image"), the build still runs and the stack starts.
func TestRebuild_MissingImage(t *testing.T) {
	cfg := rebuildConfig(t)
	fc := &fakeCompose{}
	fr := &fakeRemover{} // returns nil — idempotent no-op
	fb := &rebuildFakeBuilder{}
	svc := ops.NewDevService(fc).WithImageRemover(fr)
	builder := newBuildService(fb)

	var buf bytes.Buffer
	err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf)
	if err != nil {
		t.Fatalf("Rebuild() unexpected error when image already absent: %v", err)
	}

	// The "Removing image" line is always printed (documents intent).
	if !strings.Contains(buf.String(), "Removing image") {
		t.Errorf("expected 'Removing image' line even when image absent\ngot:\n%s", buf.String())
	}

	// Build and Up must have run regardless.
	if fb.buildCalls == 0 {
		t.Error("expected build to run after no-op remove")
	}
	if fc.downCalled == 0 {
		t.Error("expected compose.Down to be called")
	}
}

// TestRebuild_BuildFailure verifies that when the build step fails, Up is NOT
// called and the error is propagated.
func TestRebuild_BuildFailure(t *testing.T) {
	cfg := rebuildConfig(t)
	fc := &fakeCompose{}
	fr := &fakeRemover{}
	fb := &rebuildFakeBuilder{buildErr: errors.New("Dockerfile not found")}
	svc := ops.NewDevService(fc).WithImageRemover(fr)
	builder := newBuildService(fb)

	var buf bytes.Buffer
	err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf)
	if err == nil {
		t.Fatal("Rebuild() expected error on build failure, got nil")
	}
	if !strings.Contains(err.Error(), "rebuilding image") {
		t.Errorf("error should mention 'rebuilding image': %v", err)
	}

	// compose.Up must NOT have been called.
	if fc.upCalled != 0 {
		t.Errorf("compose.Up should not have been called when build fails, got %d calls", fc.upCalled)
	}
}

// TestRebuild_WithVolumes verifies that --rebuild --volumes passes Volumes:true
// to compose.Down.
func TestRebuild_WithVolumes(t *testing.T) {
	cfg := rebuildConfig(t)
	fc := &fakeCompose{}
	fr := &fakeRemover{}
	fb := &rebuildFakeBuilder{}
	svc := ops.NewDevService(fc).WithImageRemover(fr)
	builder := newBuildService(fb)

	var buf bytes.Buffer
	err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{RebuildVolumes: true}, builder, &buf)
	if err != nil {
		t.Fatalf("Rebuild() unexpected error: %v", err)
	}

	if !fc.capturedDownOpts.Volumes {
		t.Error("expected compose.Down called with Volumes: true")
	}
}

// TestRebuild_UserSetImage verifies that when cfg.App.Image is user-managed
// (non-canonical tag), the skip path fires: no Down/Remove/Build, just Run.
func TestRebuild_UserSetImage(t *testing.T) {
	cfg := defaultConfig()
	cfg.Name = "myapp"
	cfg.ProjectRoot = t.TempDir()
	// User-managed image — not the canonical "myapp-app:latest" tag.
	cfg.App.Image = "ghcr.io/someorg/myapp:v1"

	fc := &fakeCompose{}
	fr := &fakeRemover{}
	fb := &rebuildFakeBuilder{}
	svc := ops.NewDevService(fc).WithImageRemover(fr)
	builder := newBuildService(fb)

	var buf bytes.Buffer
	err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf)
	if err != nil {
		t.Fatalf("Rebuild() unexpected error: %v", err)
	}

	out := buf.String()

	// Skip line must be printed.
	if !strings.Contains(out, "user-managed") {
		t.Errorf("expected skip line with 'user-managed'\ngot:\n%s", out)
	}

	// Down must have been called (stack is stopped even on the skip path).
	if fc.downCalled == 0 {
		t.Error("expected compose.Down to be called even on skip path")
	}

	// Remove and Build must NOT be called.
	if fr.removeCalls != 0 {
		t.Errorf("expected Remove not to be called on user-set image, got %d calls", fr.removeCalls)
	}
	if fb.buildCalls != 0 {
		t.Errorf("expected Build not to be called on user-set image, got %d calls", fb.buildCalls)
	}
}

// TestRebuild_AppBuildSet verifies that when cfg.App.Build is non-empty
// (compose builds the image itself), the skip path fires identically to the
// user-set-image case.
func TestRebuild_AppBuildSet(t *testing.T) {
	cfg := defaultConfig()
	cfg.Name = "myapp"
	cfg.ProjectRoot = t.TempDir()
	cfg.App.Build = "."
	// app.image can be set or empty — the check is on App.Build.
	cfg.App.Image = ""

	fc := &fakeCompose{}
	fr := &fakeRemover{}
	fb := &rebuildFakeBuilder{}
	svc := ops.NewDevService(fc).WithImageRemover(fr)
	builder := newBuildService(fb)

	var buf bytes.Buffer
	err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf)
	if err != nil {
		t.Fatalf("Rebuild() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "user-managed") {
		t.Errorf("expected skip line with 'user-managed'\ngot:\n%s", out)
	}
	if fr.removeCalls != 0 {
		t.Errorf("expected Remove not called when app.build set, got %d", fr.removeCalls)
	}
	if fb.buildCalls != 0 {
		t.Errorf("expected Build not called when app.build set, got %d", fb.buildCalls)
	}
}

// TestRebuild_DownFailure verifies that a compose.Down failure aborts before
// any remove/build/up step.
func TestRebuild_DownFailure(t *testing.T) {
	cfg := rebuildConfig(t)
	fc := &fakeCompose{downErr: errors.New("daemon not responding")}
	fr := &fakeRemover{}
	fb := &rebuildFakeBuilder{}
	svc := ops.NewDevService(fc).WithImageRemover(fr)
	builder := newBuildService(fb)

	var buf bytes.Buffer
	err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf)
	if err == nil {
		t.Fatal("Rebuild() expected error on Down failure, got nil")
	}
	if !strings.Contains(err.Error(), "stopping stack") {
		t.Errorf("error should mention 'stopping stack': %v", err)
	}

	// Remove and Build must NOT have been called.
	if fr.removeCalls != 0 {
		t.Errorf("expected Remove not called after Down failure, got %d", fr.removeCalls)
	}
	if fb.buildCalls != 0 {
		t.Errorf("expected Build not called after Down failure, got %d", fb.buildCalls)
	}
}

// TestRebuild_RemoveNonFatalFailure verifies that a non-"not-found" Remove
// error is logged at WARN and execution continues: build runs and stack starts.
func TestRebuild_RemoveNonFatalFailure(t *testing.T) {
	cfg := rebuildConfig(t)
	fc := &fakeCompose{}
	// Simulate a genuine docker error that is not "No such image".
	fr := &fakeRemover{removeErr: errors.New("image is being used by a stopped container")}
	fb := &rebuildFakeBuilder{}
	svc := ops.NewDevService(fc).WithImageRemover(fr)
	builder := newBuildService(fb)

	var buf bytes.Buffer
	err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf)
	if err != nil {
		t.Fatalf("Rebuild() should continue past non-fatal Remove error, got: %v", err)
	}

	// Build must still have run.
	if fb.buildCalls == 0 {
		t.Error("expected Build to run after non-fatal Remove error")
	}

	// Stack must have been started (Up was called).
	if fc.upCalled == 0 {
		t.Error("expected compose.Up to be called after non-fatal Remove error")
	}
}

// TestRebuild_NoImageRemover verifies that Rebuild succeeds even when no
// imageRemover is wired (nil-safe skip of the rmi step).
func TestRebuild_NoImageRemover(t *testing.T) {
	cfg := rebuildConfig(t)
	fc := &fakeCompose{}
	fb := &rebuildFakeBuilder{}
	// No WithImageRemover call.
	svc := ops.NewDevService(fc)
	builder := newBuildService(fb)

	var buf bytes.Buffer
	err := svc.Rebuild(context.Background(), cfg, ops.DevOptions{}, builder, &buf)
	if err != nil {
		t.Fatalf("Rebuild() unexpected error without imageRemover: %v", err)
	}
	// Build must still run even with no remover.
	if fb.buildCalls == 0 {
		t.Error("expected Build to run even with no imageRemover")
	}
}
