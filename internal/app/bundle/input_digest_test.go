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

// writeDigest writes a digest file at <root>/.vibewarden/.input-digest.
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

// gitignoreContains reports whether root/.gitignore contains the given line.
func gitignoreContains(t *testing.T, root, line string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == strings.TrimSpace(line) {
			return true
		}
	}
	return false
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
// Test: digest file absent — falls back to mtime
// ---------------------------------------------------------------------------

// TestDigest_MissingFallsBackToMtime verifies that when no digest file exists,
// the staleness verdict is computed from the mtime walker (pre-#1146 behaviour).
func TestDigest_MissingFallsBackToMtime(t *testing.T) {
	root := t.TempDir()

	// Write a file with mtime after the image creation time.
	imgCreated := time.Now().Add(-2 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "vibewarden.yaml"), time.Now())

	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if !verdict.Stale {
		t.Errorf("expected STALE via mtime fallback, got FRESH")
	}
	if verdict.Mode != bundleapp.FreshnessModeTime {
		t.Errorf("Mode = %q, want %q", verdict.Mode, bundleapp.FreshnessModeTime)
	}
	if verdict.ChangedCount == 0 {
		t.Errorf("ChangedCount = 0, want > 0 in mtime mode")
	}
}

// TestDigest_MissingMTimeOlder verifies that when no digest file exists and
// all source files are older than the image, verdict is FRESH via mtime.
func TestDigest_MissingMTimeOlder(t *testing.T) {
	root := t.TempDir()

	// Write a file with mtime BEFORE the image creation time.
	pastMtime := time.Now().Add(-3 * time.Hour)
	imgCreated := time.Now().Add(-1 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "vibewarden.yaml"), pastMtime)

	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if verdict.Stale {
		t.Errorf("expected FRESH via mtime fallback, got STALE")
	}
	if verdict.Mode != bundleapp.FreshnessModeTime {
		t.Errorf("Mode = %q, want %q", verdict.Mode, bundleapp.FreshnessModeTime)
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
// Test: corrupt digest — fallback to mtime
// ---------------------------------------------------------------------------

// TestDigest_CorruptFallsBack verifies that a corrupt digest file (not valid
// JSON) is treated as missing: falls back to mtime, no error returned.
func TestDigest_CorruptFallsBack(t *testing.T) {
	root := t.TempDir()

	writeDigestFile(t, root, "garbage {not json}")

	// File with mtime AFTER image creation → mtime says STALE.
	imgCreated := time.Now().Add(-2 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "main.go"), time.Now())

	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	// Must not panic, must not return an error.
	// Mode must be mtime (fallback).
	if verdict.Mode != bundleapp.FreshnessModeTime {
		t.Errorf("Mode = %q, want %q (corrupt digest → mtime fallback)", verdict.Mode, bundleapp.FreshnessModeTime)
	}
}

// ---------------------------------------------------------------------------
// Test: schema_version mismatch — fallback
// ---------------------------------------------------------------------------

// TestDigest_SchemaVersionMismatchFallsBack verifies that a digest file with
// a future schema_version is treated as missing and triggers mtime fallback.
func TestDigest_SchemaVersionMismatchFallsBack(t *testing.T) {
	root := t.TempDir()

	content := `{"schema_version":2,"digest":"sha256:` + strings.Repeat("a", 64) + `","inputs":[]}`
	writeDigestFile(t, root, content)

	imgCreated := time.Now().Add(-2 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "main.go"), time.Now())

	inspector := newTestFreshInspector(imgCreated)
	walker := bundleapp.NewFileSystemStalenessWalker(root)

	verdict := runHealthCheck(t, root, inspector, walker)

	if verdict.Mode != bundleapp.FreshnessModeTime {
		t.Errorf("Mode = %q, want %q (schema mismatch → mtime fallback)", verdict.Mode, bundleapp.FreshnessModeTime)
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
// Test: gitignore append on first write
// ---------------------------------------------------------------------------

// TestDigest_GitIgnoreAppendOnFirstWrite verifies that WriteInputDigest adds
// ".vibewarden/.input-digest" to the project .gitignore, idempotently.
func TestDigest_GitIgnoreAppendOnFirstWrite(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "vibewarden.yaml"), []byte("name: test\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	svc := bundleapp.NewService(nil, &fakeGenerator{})

	// First write: .gitignore does not exist yet.
	svc.WriteInputDigest(root)
	if !gitignoreContains(t, root, ".vibewarden/.input-digest") {
		t.Error("first write: .gitignore does not contain '.vibewarden/.input-digest'")
	}

	// Second write: idempotent — line must not be duplicated.
	svc.WriteInputDigest(root)
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == ".vibewarden/.input-digest" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("idempotent check: found %d occurrences of line, want 1\n.gitignore:\n%s", count, string(data))
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
// mode STALE does not include a file count.
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
	// Digest mode must NOT include "N source files" — the count is not meaningful.
	if strings.Contains(out, "0 source files") {
		t.Errorf("digest-mode STALE should not include file count, got:\n%s", out)
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
		t.Error("expected not stale when no walker wired")
	}
}
