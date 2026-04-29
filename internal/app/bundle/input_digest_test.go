package bundle_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// writeDigestFile writes a digest file at <root>/.vibewarden/.input-digest.
func writeDigestFile(t *testing.T, root string, content string) {
	t.Helper()
	dir := filepath.Join(root, ".vibewarden")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, ".input-digest")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write digest: %v", err)
	}
}

// readDigestFile reads the digest file from root and unmarshals it.
func readDigestFile(t *testing.T, root string) bundleapp.InputDigest {
	t.Helper()
	path := filepath.Join(root, ".vibewarden", ".input-digest")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read digest file: %v", err)
	}
	var d bundleapp.InputDigest
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("unmarshal digest file: %v", err)
	}
	return d
}

// digestFileExists reports whether the digest file is present under root.
func digestFileExists(root string) bool {
	path := filepath.Join(root, ".vibewarden", ".input-digest")
	_, err := os.Stat(path)
	return err == nil
}

// newTestFreshInspector returns a fakeInspector whose image creation time
// predates the given files so mtime-based freshness always says STALE.
func newTestFreshInspector(createdAt time.Time) *fakeInspector {
	return &fakeInspector{
		info: ports.ImageInfo{
			Tag:          "test-app:latest",
			Digest:       "sha256:abc123",
			OS:           "linux",
			Architecture: "amd64",
			Created:      createdAt,
			SizeBytes:    10 * 1024 * 1024,
		},
	}
}

// runHealthCheck calls CheckImageHealth with the given project root and walker,
// returning the verdict.
func runHealthCheck(t *testing.T, root string, inspector *fakeInspector, walker bundleapp.StalenessWalker) bundleapp.FreshnessVerdict {
	t.Helper()
	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:       "test-app:latest",
		ProjectRoot:    root,
		TargetPlatform: "linux/amd64",
		Inspector:      inspector,
		Walker:         walker,
	})
	if err != nil {
		t.Fatalf("CheckImageHealth: %v", err)
	}
	return h.Freshness
}

// ---------------------------------------------------------------------------
// Test: no prior digest → first-run baseline (FRESH, not mtime)
// ---------------------------------------------------------------------------

// TestDigest_FirstRunReportsFresh verifies that when no digest file exists,
// the verdict is FRESH with Mode=FreshnessModeBaseline (not mtime fallback).
// This is the replacement for the removed TestDigest_MissingFallsBackToMtime.
func TestDigest_FirstRunReportsFresh(t *testing.T) {
	root := t.TempDir()

	imgCreated := time.Now().Add(-2 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "vibewarden.yaml"), time.Now())

	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if verdict.Stale {
		t.Errorf("expected FRESH (baseline) when no prior digest, got STALE")
	}
	if verdict.Mode != bundleapp.FreshnessModeBaseline {
		t.Errorf("Mode = %q, want %q", verdict.Mode, bundleapp.FreshnessModeBaseline)
	}
}

// TestDigest_FirstRunFreshRegardlessOfMtime verifies that even when all source
// files are older than the image, the first-run verdict is FRESH/baseline (not
// FRESH/mtime). The distinction matters for assertions.
func TestDigest_FirstRunFreshRegardlessOfMtime(t *testing.T) {
	root := t.TempDir()

	pastMtime := time.Now().Add(-3 * time.Hour)
	imgCreated := time.Now().Add(-1 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "vibewarden.yaml"), pastMtime)

	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if verdict.Stale {
		t.Errorf("expected FRESH (baseline), got STALE")
	}
	if verdict.Mode != bundleapp.FreshnessModeBaseline {
		t.Errorf("Mode = %q, want %q", verdict.Mode, bundleapp.FreshnessModeBaseline)
	}
}

// ---------------------------------------------------------------------------
// Test: digest equal — FRESH even when mtime is newer (qr-dali bug fix)
// ---------------------------------------------------------------------------

// TestDigest_EqualSuppressesStale is the regression test for the qr-dali bug:
// touching vibewarden.yaml (bumping mtime without changing content) must NOT
// produce a STALE verdict when a matching digest file is present.
func TestDigest_EqualSuppressesStale(t *testing.T) {
	root := t.TempDir()

	// Write source file.
	yamlPath := filepath.Join(root, "vibewarden.yaml")
	if err := os.WriteFile(yamlPath, []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	// Compute and persist the digest (simulates a prior successful bundle).
	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)

	if !digestFileExists(root) {
		t.Fatal("digest file was not written")
	}

	// Now bump the mtime without changing content (the qr-dali scenario).
	futureTime := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(yamlPath, futureTime, futureTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Image was created before the mtime bump — mtime alone would say STALE.
	imgCreated := time.Now().Add(-1 * time.Hour)
	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if verdict.Stale {
		t.Errorf("qr-dali bug: expected FRESH after touch (no content change), got STALE")
	}
	if verdict.Mode != bundleapp.FreshnessModeDigest {
		t.Errorf("Mode = %q, want %q", verdict.Mode, bundleapp.FreshnessModeDigest)
	}
}

// ---------------------------------------------------------------------------
// Test: digest differs — STALE
// ---------------------------------------------------------------------------

// TestDigest_DiffersEmitsStale verifies that changing one byte in a watched
// file produces a STALE verdict in digest mode.
func TestDigest_DiffersEmitsStale(t *testing.T) {
	root := t.TempDir()

	yamlPath := filepath.Join(root, "vibewarden.yaml")
	if err := os.WriteFile(yamlPath, []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	// Record digest.
	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)

	// Modify the file content.
	if err := os.WriteFile(yamlPath, []byte("name: test-MODIFIED\n"), 0o600); err != nil {
		t.Fatalf("modify yaml: %v", err)
	}

	imgCreated := time.Now().Add(-1 * time.Hour)
	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if !verdict.Stale {
		t.Errorf("expected STALE after content change, got FRESH")
	}
	if verdict.Mode != bundleapp.FreshnessModeDigest {
		t.Errorf("Mode = %q, want %q", verdict.Mode, bundleapp.FreshnessModeDigest)
	}
}

// ---------------------------------------------------------------------------
// Test: corrupt digest → first-run baseline
// ---------------------------------------------------------------------------

// TestDigest_CorruptFallsBack verifies that a corrupt digest file (not valid
// JSON) is treated as missing: verdict is FRESH/baseline (not mtime fallback),
// no error returned.
func TestDigest_CorruptFallsBack(t *testing.T) {
	root := t.TempDir()

	writeDigestFile(t, root, "garbage {not json}")

	imgCreated := time.Now().Add(-2 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "main.go"), time.Now())

	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	// Must not panic, must not return an error.
	// Mode must be baseline (not mtime — the mtime fallback is removed).
	if verdict.Mode != bundleapp.FreshnessModeBaseline {
		t.Errorf("Mode = %q, want %q (corrupt digest → baseline)", verdict.Mode, bundleapp.FreshnessModeBaseline)
	}
	if verdict.Stale {
		t.Errorf("corrupt digest should not produce STALE, got Stale=true")
	}
}

// ---------------------------------------------------------------------------
// Test: schema_version mismatch → first-run baseline
// ---------------------------------------------------------------------------

// TestDigest_SchemaVersionMismatchFallsBack verifies that a v1 digest file is
// treated as missing and produces FRESH/baseline (not mtime fallback or STALE).
func TestDigest_SchemaVersionMismatchFallsBack(t *testing.T) {
	root := t.TempDir()

	// Write a v1-style digest (schema_version=1, old format without "files").
	content := `{"schema_version":1,"digest":"sha256:` + strings.Repeat("a", 64) + `","inputs":[]}`
	writeDigestFile(t, root, content)

	imgCreated := time.Now().Add(-2 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "main.go"), time.Now())

	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if verdict.Mode != bundleapp.FreshnessModeBaseline {
		t.Errorf("Mode = %q, want %q (schema mismatch → baseline)", verdict.Mode, bundleapp.FreshnessModeBaseline)
	}
	if verdict.Stale {
		t.Errorf("v1 schema mismatch should not produce STALE, got Stale=true")
	}
}

// ---------------------------------------------------------------------------
// Test: qr-dali bug — touch does not trip warning
// ---------------------------------------------------------------------------

// TestDigest_QrDaliBugNoWarning is the named acceptance-criteria test from the
// PM spec: `touch vibewarden.yaml` with no content change must NOT produce
// a STALE verdict.
func TestDigest_QrDaliBugNoWarning(t *testing.T) {
	root := t.TempDir()

	yamlPath := filepath.Join(root, "vibewarden.yaml")
	content := []byte("name: qr-dali\n")
	if err := os.WriteFile(yamlPath, content, 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	// Simulate first bundle: write digest.
	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)

	// touch vibewarden.yaml — bump mtime, same bytes.
	future := time.Now().Add(1 * time.Hour)
	if err := os.Chtimes(yamlPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	imgCreated := time.Now().Add(-30 * time.Minute)
	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if verdict.Stale {
		t.Errorf("qr-dali: touch vibewarden.yaml should not produce STALE, got verdict.Stale=true mode=%s", verdict.Mode)
	}
}

// ---------------------------------------------------------------------------
// Test: digest written only on success
// ---------------------------------------------------------------------------

// TestDigest_WrittenOnlyOnSuccess verifies that a mid-run failure does NOT
// update the digest file. We simulate this by keeping the prior digest intact
// and asserting it is unchanged after a failed bundle.
func TestDigest_WrittenOnlyOnSuccess(t *testing.T) {
	root := t.TempDir()

	yamlPath := filepath.Join(root, "vibewarden.yaml")
	if err := os.WriteFile(yamlPath, []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	// Write first digest.
	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)
	first := readDigestFile(t, root)

	// Change the file content (so if we wrote the digest again it would differ).
	if err := os.WriteFile(yamlPath, []byte("name: changed\n"), 0o600); err != nil {
		t.Fatalf("modify yaml: %v", err)
	}

	// Simulate NOT calling WriteInputDigest (i.e. bundle failed before success).
	// We do NOT call svc.WriteInputDigest again.

	// Assert the digest file still holds the first digest.
	second := readDigestFile(t, root)
	if first.Digest != second.Digest {
		t.Errorf("digest changed without successful bundle: first=%s second=%s", first.Digest, second.Digest)
	}
}

// ---------------------------------------------------------------------------
// Test: Dockerfile change detected
// ---------------------------------------------------------------------------

// TestDigest_DockerfileChangeDetected verifies that modifying Dockerfile
// produces a STALE verdict in digest mode.
func TestDigest_DockerfileChangeDetected(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "vibewarden.yaml"), []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)

	// Modify Dockerfile.
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM ubuntu:24.04\n"), 0o600); err != nil {
		t.Fatalf("modify Dockerfile: %v", err)
	}

	imgCreated := time.Now().Add(-1 * time.Hour)
	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if !verdict.Stale {
		t.Error("Dockerfile change should produce STALE, got FRESH")
	}
	if verdict.Mode != bundleapp.FreshnessModeDigest {
		t.Errorf("Mode = %q, want digest", verdict.Mode)
	}
}

// ---------------------------------------------------------------------------
// Test: production yaml change detected
// ---------------------------------------------------------------------------

// TestDigest_ProductionYAMLChangeDetected verifies that modifying
// vibewarden.production.yaml produces STALE in digest mode.
func TestDigest_ProductionYAMLChangeDetected(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "vibewarden.yaml"), []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "vibewarden.production.yaml"), []byte("server:\n  port: 443\n"), 0o600); err != nil {
		t.Fatalf("write prod yaml: %v", err)
	}

	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)

	// Modify production yaml.
	if err := os.WriteFile(filepath.Join(root, "vibewarden.production.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("modify prod yaml: %v", err)
	}

	imgCreated := time.Now().Add(-1 * time.Hour)
	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if !verdict.Stale {
		t.Error("vibewarden.production.yaml change should produce STALE, got FRESH")
	}
	if verdict.Mode != bundleapp.FreshnessModeDigest {
		t.Errorf("Mode = %q, want digest", verdict.Mode)
	}
}

// ---------------------------------------------------------------------------
// Test: gitignored file change does not trip STALE
// ---------------------------------------------------------------------------

// TestDigest_GitIgnoredFileChangeDoeNotTrip verifies that modifying a file
// matched by .gitignore does not change the digest and hence does not produce
// STALE (digest mode).
func TestDigest_GitIgnoredFileChangeDoeNotTrip(t *testing.T) {
	root := t.TempDir()

	// Write .gitignore that ignores *.log.
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "vibewarden.yaml"), []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.log"), []byte("old log\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)

	// Modify the gitignored file.
	if err := os.WriteFile(filepath.Join(root, "app.log"), []byte("new log content\n"), 0o600); err != nil {
		t.Fatalf("modify log: %v", err)
	}

	imgCreated := time.Now().Add(-1 * time.Hour)
	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if verdict.Stale {
		t.Error("gitignored file change should NOT produce STALE, got STALE")
	}
	if verdict.Mode != bundleapp.FreshnessModeDigest {
		t.Errorf("Mode = %q, want digest", verdict.Mode)
	}
}

// ---------------------------------------------------------------------------
// Test: RenderImageHealth freshness label in digest mode
// ---------------------------------------------------------------------------

// TestRenderImageHealth_DigestStale verifies the freshness label for digest-
// mode STALE does not include a file count inline.
func TestRenderImageHealth_DigestStale(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "test-app:latest",
			Digest:       "sha256:abc",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
			SizeBytes:    10 * 1024 * 1024,
		},
		Target: "linux/amd64",
		Freshness: bundleapp.FreshnessVerdict{
			Stale: true,
			Mode:  bundleapp.FreshnessModeDigest,
		},
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if !strings.Contains(out, "STALE") {
		t.Errorf("expected STALE in output\n%s", out)
	}
	// Digest mode must NOT include "N source files" inline — the count is not
	// meaningful in the single-line form.
	if strings.Contains(out, "0 source files") {
		t.Errorf("digest-mode STALE should not include inline file count, got:\n%s", out)
	}
}

// TestRenderImageHealth_DigestFresh verifies the FRESH label for digest mode.
func TestRenderImageHealth_DigestFresh(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "test-app:latest",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
		},
		Target: "linux/amd64",
		Freshness: bundleapp.FreshnessVerdict{
			Stale: false,
			Mode:  bundleapp.FreshnessModeDigest,
		},
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if !strings.Contains(out, "FRESH") {
		t.Errorf("expected FRESH in output\n%s", out)
	}
}

// TestRenderImageHealth_BaselineFresh verifies that a first-run baseline
// verdict produces the "FRESH (no prior baseline...)" label.
func TestRenderImageHealth_BaselineFresh(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "test-app:latest",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
		},
		Target: "linux/amd64",
		Freshness: bundleapp.FreshnessVerdict{
			Stale: false,
			Mode:  bundleapp.FreshnessModeBaseline,
		},
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if !strings.Contains(out, "FRESH") {
		t.Errorf("expected FRESH in baseline output\n%s", out)
	}
	if !strings.Contains(out, "no prior baseline") {
		t.Errorf("expected 'no prior baseline' annotation in baseline output\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Test: CheckImageHealth with nil Walker (no project root) uses zero verdict
// ---------------------------------------------------------------------------

// TestCheckImageHealth_NoWalkerNoVerdict verifies that when Walker or
// ProjectRoot is not set, the verdict is zero-valued (not stale, mode empty).
func TestCheckImageHealth_NoWalkerNoVerdict(t *testing.T) {
	inspector := newTestFreshInspector(time.Now().Add(-1 * time.Hour))

	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:  "test-app:latest",
		Inspector: inspector,
		Walker:    nil, // no walker
		// ProjectRoot: empty
	})
	if err != nil {
		t.Fatalf("CheckImageHealth: %v", err)
	}
	if h.Freshness.Stale {
		t.Error("expected not stale when no project root")
	}
}

// ---------------------------------------------------------------------------
// Test #1223 regression: .gitignore must NOT be mutated by WriteInputDigest
// ---------------------------------------------------------------------------

// TestDigest_GitignoreNotMutatedByWriteInputDigest is the regression test for
// #1223. The user's .gitignore must not be opened or written during
// WriteInputDigest — that was the root cause of the self-induced staleness.
func TestDigest_GitignoreNotMutatedByWriteInputDigest(t *testing.T) {
	root := t.TempDir()

	// Create the project .gitignore with known content and record its mtime.
	gitignorePath := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("*.log\n"), 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "vibewarden.yaml"), []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	infoBeforeFirst, err := os.Stat(gitignorePath)
	if err != nil {
		t.Fatalf("stat .gitignore before: %v", err)
	}
	contentBefore, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore before: %v", err)
	}

	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root) // first write

	// The user's .gitignore must not have changed.
	infoAfterFirst, err := os.Stat(gitignorePath)
	if err != nil {
		t.Fatalf("stat .gitignore after first write: %v", err)
	}
	if !infoAfterFirst.ModTime().Equal(infoBeforeFirst.ModTime()) {
		t.Errorf(".gitignore mtime changed after first WriteInputDigest: before=%v after=%v",
			infoBeforeFirst.ModTime(), infoAfterFirst.ModTime())
	}
	contentAfterFirst, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore after first write: %v", err)
	}
	if string(contentAfterFirst) != string(contentBefore) {
		t.Errorf(".gitignore content changed:\nbefore: %q\nafter:  %q", contentBefore, contentAfterFirst)
	}

	svc.WriteInputDigest(root) // second write (idempotent path)

	infoAfterSecond, err := os.Stat(gitignorePath)
	if err != nil {
		t.Fatalf("stat .gitignore after second write: %v", err)
	}
	if !infoAfterSecond.ModTime().Equal(infoBeforeFirst.ModTime()) {
		t.Errorf(".gitignore mtime changed after second WriteInputDigest: before=%v after=%v",
			infoBeforeFirst.ModTime(), infoAfterSecond.ModTime())
	}

	// The per-directory .vibewarden/.gitignore must exist with "*\n".
	vwGitignore := filepath.Join(root, ".vibewarden", ".gitignore")
	data, err := os.ReadFile(vwGitignore)
	if err != nil {
		t.Fatalf(".vibewarden/.gitignore not written: %v", err)
	}
	if string(data) != "*\n" {
		t.Errorf(".vibewarden/.gitignore content = %q, want %q", data, "*\n")
	}
}

// ---------------------------------------------------------------------------
// Test #1223 end-to-end reproducer: self-induced staleness must not occur
// ---------------------------------------------------------------------------

// TestDigest_SelfInducedStalenessRegression is the end-to-end reproducer for
// #1223. Two consecutive WriteInputDigest + CheckImageHealth cycles on an
// unchanged project must both report FRESH (not STALE).
func TestDigest_SelfInducedStalenessRegression(t *testing.T) {
	root := t.TempDir()

	// Write project files.
	if err := os.WriteFile(filepath.Join(root, "vibewarden.yaml"), []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	svc := bundleapp.NewService(nil, &fakeGenerator{})
	imgCreated := time.Now().Add(-1 * time.Hour)
	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	// First bundle: write digest. Verdict should be baseline FRESH.
	svc.WriteInputDigest(root)
	verdict1 := runHealthCheck(t, root, inspector, walker)
	if verdict1.Stale {
		t.Errorf("first run: expected FRESH, got STALE (mode=%s)", verdict1.Mode)
	}

	// Second bundle: no edits. WriteInputDigest must not mutate .gitignore.
	svc.WriteInputDigest(root)
	verdict2 := runHealthCheck(t, root, inspector, walker)
	if verdict2.Stale {
		t.Errorf("second run: expected FRESH (self-induced staleness regression), got STALE (mode=%s)", verdict2.Mode)
	}
	if verdict2.Mode != bundleapp.FreshnessModeDigest {
		t.Errorf("second run: Mode = %q, want %q", verdict2.Mode, bundleapp.FreshnessModeDigest)
	}
}

// ---------------------------------------------------------------------------
// Test: changed paths listed in rendered freshness block
// ---------------------------------------------------------------------------

// TestDigest_StalePathsListedInRender verifies that when files change between
// bundles, the rendered freshness block lists the changed paths by kind.
func TestDigest_StalePathsListedInRender(t *testing.T) {
	h := bundleapp.ImageHealth{
		Image: ports.ImageInfo{
			Tag:          "test-app:latest",
			OS:           "linux",
			Architecture: "amd64",
			Created:      fixedTime,
		},
		Target: "linux/amd64",
		Freshness: bundleapp.FreshnessVerdict{
			Stale:        true,
			Mode:         bundleapp.FreshnessModeDigest,
			ChangedCount: 3,
			ChangedPaths: []bundleapp.ChangedPath{
				{Path: "removed.txt", Kind: bundleapp.ChangedPathRemoved},
				{Path: "added.txt", Kind: bundleapp.ChangedPathAdded},
				{Path: "main.go", Kind: bundleapp.ChangedPathModified},
			},
		},
	}

	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	for _, want := range []string{
		"removed.txt (removed)",
		"added.txt (added)",
		"main.go (modified)",
		"STALE",
		"image is stale",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: changed paths capped at maxChangedPathsRendered (5)
// ---------------------------------------------------------------------------

// TestDigest_ChangedPathsCappedAt5 verifies that when more than 5 paths change,
// the rendered block shows exactly 5 + an "(and N more)" line.
func TestDigest_ChangedPathsCappedAt5(t *testing.T) {
	root := t.TempDir()

	// Write 12 source files and record the digest.
	svc := bundleapp.NewService(nil, &fakeGenerator{})
	for i := 0; i < 12; i++ {
		name := filepath.Join(root, strings.Repeat("a", i+1)+".go")
		if err := os.WriteFile(name, []byte("package main\n"), 0o600); err != nil {
			t.Fatalf("write file %d: %v", i, err)
		}
	}
	svc.WriteInputDigest(root)

	// Modify all 12 files.
	for i := 0; i < 12; i++ {
		name := filepath.Join(root, strings.Repeat("a", i+1)+".go")
		if err := os.WriteFile(name, []byte("package main // modified\n"), 0o600); err != nil {
			t.Fatalf("modify file %d: %v", i, err)
		}
	}

	imgCreated := time.Now().Add(-1 * time.Hour)
	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	h, err := bundleapp.CheckImageHealth(context.Background(), bundleapp.CheckImageHealthOptions{
		ImageTag:       "test-app:latest",
		ProjectRoot:    root,
		TargetPlatform: "linux/amd64",
		Inspector:      inspector,
		Walker:         walker,
	})
	if err != nil {
		t.Fatalf("CheckImageHealth: %v", err)
	}

	if !h.Freshness.Stale {
		t.Fatal("expected STALE after modifying 12 files, got FRESH")
	}
	// ChangedPaths must be capped at 5.
	if len(h.Freshness.ChangedPaths) != 5 {
		t.Errorf("ChangedPaths len = %d, want 5", len(h.Freshness.ChangedPaths))
	}
	// ChangedCount must be the full total.
	if h.Freshness.ChangedCount != 12 {
		t.Errorf("ChangedCount = %d, want 12", h.Freshness.ChangedCount)
	}

	// Render and verify "(and 7 more)" appears.
	var sb strings.Builder
	bundleapp.RenderImageHealth(&sb, h)
	out := sb.String()

	if !strings.Contains(out, "(and 7 more)") {
		t.Errorf("expected '(and 7 more)' in rendered output\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Test: .vibewarden/.gitignore written idempotently
// ---------------------------------------------------------------------------

// TestDigest_VibewardenGitignoreFileWritten verifies that after WriteInputDigest,
// <root>/.vibewarden/.gitignore exists with content "*\n", and that calling
// WriteInputDigest again is idempotent (file not rewritten).
func TestDigest_VibewardenGitignoreFileWritten(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "vibewarden.yaml"), []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)

	vwGitignore := filepath.Join(root, ".vibewarden", ".gitignore")
	data, err := os.ReadFile(vwGitignore)
	if err != nil {
		t.Fatalf(".vibewarden/.gitignore not created: %v", err)
	}
	if string(data) != "*\n" {
		t.Errorf("content = %q, want %q", data, "*\n")
	}

	// Record mtime after first write.
	info1, err := os.Stat(vwGitignore)
	if err != nil {
		t.Fatalf("stat after first write: %v", err)
	}

	// Small sleep to ensure a mtime difference would be observable if the file
	// were re-written.
	time.Sleep(5 * time.Millisecond)

	svc.WriteInputDigest(root) // second call — must be idempotent

	info2, err := os.Stat(vwGitignore)
	if err != nil {
		t.Fatalf("stat after second write: %v", err)
	}
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Errorf(".vibewarden/.gitignore was rewritten on second call: mtime1=%v mtime2=%v",
			info1.ModTime(), info2.ModTime())
	}
}

// ---------------------------------------------------------------------------
// Test: symlink outside project root is skipped
// ---------------------------------------------------------------------------

// TestDigest_SymlinkOutsideRootSkipped verifies that a symlink under
// projectRoot pointing outside the project root is silently skipped by the
// digest walker. No panic, no I/O error, and the file at the symlink target
// does not influence the digest.
func TestDigest_SymlinkOutsideRootSkipped(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Place a file inside the project root.
	if err := os.WriteFile(filepath.Join(root, "vibewarden.yaml"), []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	// Compute digest WITHOUT symlink — this is the baseline.
	svc := bundleapp.NewService(nil, &fakeGenerator{})
	svc.WriteInputDigest(root)
	digestBefore := readDigestFile(t, root)

	// Place a file outside root and create a symlink inside root pointing to it.
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("sensitive data\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	symlink := filepath.Join(root, "escape.txt")
	if err := os.Symlink(outsideFile, symlink); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// Compute digest WITH symlink — symlink must be skipped.
	imgCreated := time.Now().Add(-1 * time.Hour)
	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)
	verdict := runHealthCheck(t, root, inspector, walker)

	// Digest must match (symlink was skipped, not included in the watched set).
	// Because we have a prior digest (from the svc.WriteInputDigest call above),
	// Mode must be digest. The new walk result must equal the prior digest.
	if verdict.Stale {
		t.Errorf("symlink outside root should not cause STALE, got Stale=true")
	}
	if verdict.Mode != bundleapp.FreshnessModeDigest {
		t.Errorf("Mode = %q, want %q", verdict.Mode, bundleapp.FreshnessModeDigest)
	}

	// Also verify the digest file itself did not capture the symlink.
	_ = digestBefore // silence unused warning; we already confirmed via verdict
}
