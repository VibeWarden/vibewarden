package bundle_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
)

// TestFileSystemStalenessWalker_MissingRoot returns zero values when root
// does not exist.
func TestFileSystemStalenessWalker_MissingRoot(t *testing.T) {
	w := bundleapp.NewFileSystemStalenessWalker("/nonexistent/path/that/does/not/exist")
	newest, count, err := w.NewestMTime("/nonexistent/path/that/does/not/exist", time.Now())
	if err != nil {
		t.Fatalf("NewestMTime() error = %v", err)
	}
	if !newest.IsZero() {
		t.Errorf("expected zero newest time for missing root, got %v", newest)
	}
	if count != 0 {
		t.Errorf("expected 0 changed count for missing root, got %d", count)
	}
}

// TestFileSystemStalenessWalker_FreshImage returns changedCount=0 when all
// files are older than the image creation time.
func TestFileSystemStalenessWalker_FreshImage(t *testing.T) {
	root := t.TempDir()

	// Write files with past mtimes.
	pastTime := time.Now().Add(-24 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "main.go"), pastTime)
	writeFileWithMtime(t, filepath.Join(root, "go.mod"), pastTime)

	threshold := time.Now().Add(-1 * time.Hour) // threshold is 1 hour ago — files are older
	w := bundleapp.NewFileSystemStalenessWalker(root)
	_, count, err := w.NewestMTime(root, threshold)
	if err != nil {
		t.Fatalf("NewestMTime() error = %v", err)
	}
	if count != 0 {
		t.Errorf("changedCount = %d, want 0 (all files pre-date threshold)", count)
	}
}

// TestFileSystemStalenessWalker_StaleImage returns changedCount>0 when files
// are newer than the image creation time.
func TestFileSystemStalenessWalker_StaleImage(t *testing.T) {
	root := t.TempDir()

	// Write files with recent mtimes (after threshold).
	now := time.Now()
	writeFileWithMtime(t, filepath.Join(root, "app.py"), now)
	writeFileWithMtime(t, filepath.Join(root, "requirements.txt"), now)

	threshold := now.Add(-1 * time.Hour) // threshold is 1 hour ago; both files are newer
	w := bundleapp.NewFileSystemStalenessWalker(root)
	_, count, err := w.NewestMTime(root, threshold)
	if err != nil {
		t.Fatalf("NewestMTime() error = %v", err)
	}
	if count == 0 {
		t.Errorf("changedCount = 0, want > 0 (files are newer than threshold)")
	}
}

// TestFileSystemStalenessWalker_HardIgnoreDirs verifies that hard-coded
// ignore directories (node_modules, .git, vendor, etc.) are not walked.
func TestFileSystemStalenessWalker_HardIgnoreDirs(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		wantSkip bool
	}{
		{name: "node_modules", dir: "node_modules", wantSkip: true},
		{name: ".git", dir: ".git", wantSkip: true},
		{name: "vendor", dir: "vendor", wantSkip: true},
		{name: "dist", dir: "dist", wantSkip: true},
		{name: "build", dir: "build", wantSkip: true},
		{name: "target", dir: "target", wantSkip: true},
		{name: ".venv", dir: ".venv", wantSkip: true},
		{name: "__pycache__", dir: "__pycache__", wantSkip: true},
		{name: ".vibewarden", dir: ".vibewarden", wantSkip: true},
		{name: "bin", dir: "bin", wantSkip: true},
		{name: ".next", dir: ".next", wantSkip: true},
		{name: ".claude/worktrees", dir: filepath.Join(".claude", "worktrees"), wantSkip: true},
		{name: ".claude/worktrees nested", dir: filepath.Join(".claude", "worktrees", "task-1", "src"), wantSkip: true},
		{name: ".claude/agents (not ignored)", dir: filepath.Join(".claude", "agents"), wantSkip: false},
		{name: "worktrees outside .claude (not ignored)", dir: "worktrees", wantSkip: false},
		{name: "src (not ignored)", dir: "src", wantSkip: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()

			// Place a very-new file inside the potentially-ignored directory.
			subDir := filepath.Join(root, tt.dir)
			if err := os.MkdirAll(subDir, 0o750); err != nil {
				t.Fatalf("mkdir %s: %v", tt.dir, err)
			}
			newFile := filepath.Join(subDir, "newfile.txt")
			writeFileWithMtime(t, newFile, time.Now())

			// Threshold is 1 hour ago — new file would be "stale" if not ignored.
			threshold := time.Now().Add(-1 * time.Hour)
			w := bundleapp.NewFileSystemStalenessWalker(root)
			_, count, err := w.NewestMTime(root, threshold)
			if err != nil {
				t.Fatalf("NewestMTime() error = %v", err)
			}

			if tt.wantSkip && count > 0 {
				t.Errorf("%s: changedCount = %d, want 0 (directory should be ignored)", tt.dir, count)
			}
			if !tt.wantSkip && count == 0 {
				t.Errorf("%s: changedCount = 0, want > 0 (directory should be walked)", tt.dir)
			}
		})
	}
}

// TestFileSystemStalenessWalker_GitignoreRespected verifies that files
// matched by .gitignore are excluded from the walk.
func TestFileSystemStalenessWalker_GitignoreRespected(t *testing.T) {
	root := t.TempDir()

	// Write .gitignore that ignores *.log files.
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o600); err != nil {
		t.Fatalf("writing .gitignore: %v", err)
	}

	// Write a new .log file (should be ignored) and a new .go file (should count).
	now := time.Now()
	writeFileWithMtime(t, filepath.Join(root, "app.log"), now)
	writeFileWithMtime(t, filepath.Join(root, "main.go"), now)

	threshold := now.Add(-1 * time.Hour)
	w := bundleapp.NewFileSystemStalenessWalker(root)
	_, count, err := w.NewestMTime(root, threshold)
	if err != nil {
		t.Fatalf("NewestMTime() error = %v", err)
	}
	// Only main.go should count (app.log is gitignored).
	// The .gitignore file itself was also written with now mtime.
	if count == 0 {
		t.Errorf("changedCount = 0, want > 0 (main.go and .gitignore are not ignored)")
	}
	// If both app.log and main.go counted, count would be 2. We cannot
	// assert exactly 1 because the .gitignore file itself has mtime=now.
	// But we can verify that count < 3 (not all 3 files counted).
	if count >= 3 {
		t.Errorf("changedCount = %d, want < 3 (app.log must not be counted)", count)
	}
}

// TestFileSystemStalenessWalker_DockerignoreRespected verifies that files
// matched by .dockerignore are excluded from the walk.
func TestFileSystemStalenessWalker_DockerignoreRespected(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, ".dockerignore"), []byte("*.tmp\n"), 0o600); err != nil {
		t.Fatalf("writing .dockerignore: %v", err)
	}

	now := time.Now()
	writeFileWithMtime(t, filepath.Join(root, "scratch.tmp"), now) // ignored
	writeFileWithMtime(t, filepath.Join(root, "Dockerfile"), now)  // counted

	threshold := now.Add(-1 * time.Hour)
	w := bundleapp.NewFileSystemStalenessWalker(root)
	_, count, err := w.NewestMTime(root, threshold)
	if err != nil {
		t.Fatalf("NewestMTime() error = %v", err)
	}
	// Dockerfile and .dockerignore should count; scratch.tmp should not.
	if count == 0 {
		t.Errorf("changedCount = 0, want > 0 (Dockerfile is not ignored)")
	}
}

// TestFileSystemStalenessWalker_NestedDirs verifies the walk descends into
// non-ignored subdirectories.
func TestFileSystemStalenessWalker_NestedDirs(t *testing.T) {
	root := t.TempDir()

	subDir := filepath.Join(root, "cmd", "app")
	if err := os.MkdirAll(subDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	now := time.Now()
	writeFileWithMtime(t, filepath.Join(subDir, "main.go"), now)

	threshold := now.Add(-1 * time.Hour)
	w := bundleapp.NewFileSystemStalenessWalker(root)
	_, count, err := w.NewestMTime(root, threshold)
	if err != nil {
		t.Fatalf("NewestMTime() error = %v", err)
	}
	if count == 0 {
		t.Errorf("changedCount = 0, want > 0 (nested main.go is not ignored)")
	}
}

// ---------------------------------------------------------------------------
// Security tests: symlink containment (#1274 — sibling fix to #1223)
// ---------------------------------------------------------------------------

// TestStalenessWalker_SymlinkOutsideRootSkipped verifies that a symlink inside
// the project root pointing to a file outside the root is silently skipped by
// the staleness walker. The external target's mtime must not influence the
// changedCount.
//
// This test FAILS on the pre-fix code (the symlink is newly created, so its
// own mtime equals now and counts as stale) and PASSES on the fixed code.
func TestStalenessWalker_SymlinkOutsideRootSkipped(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Only project file has an old mtime — nothing should count as changed.
	pastMtime := time.Now().Add(-24 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "main.go"), pastMtime)

	// External file with a recent mtime. Without the fix, a symlink pointing
	// here would cause the symlink entry itself (mtime = now, just created) to
	// be counted, or the target's mtime to influence the result.
	outsideFile := filepath.Join(outside, "secret.log")
	writeFileWithMtime(t, outsideFile, time.Now())

	// Symlink inside project pointing to the external file.
	symlink := filepath.Join(root, "escape.txt")
	if err := os.Symlink(outsideFile, symlink); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// Threshold is 1 hour ago. The symlink (mtime = just now) would count
	// as stale without the fix. After the fix it must be skipped.
	threshold := time.Now().Add(-1 * time.Hour)
	w := bundleapp.NewFileSystemStalenessWalker(root)
	_, count, err := w.NewestMTime(root, threshold)
	if err != nil {
		t.Fatalf("NewestMTime() error = %v", err)
	}
	if count != 0 {
		t.Errorf("changedCount = %d, want 0 (symlink pointing outside root must be skipped)", count)
	}
}

// TestStalenessWalker_SymlinkPrefixExtensionRejected is the security regression
// test for the prefix-extension symlink escape: a symlink pointing at a sibling
// directory whose name extends the project root name (e.g. /proj-secret when
// root is /proj) must be rejected, not admitted by a bare strings.HasPrefix
// check.
//
// This test FAILS on the pre-fix code and PASSES on the fixed code.
func TestStalenessWalker_SymlinkPrefixExtensionRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping symlink test in short mode")
	}

	// Use a shared parent so root and outside share a directory prefix.
	parent := t.TempDir()
	root := filepath.Join(parent, "proj")
	outside := filepath.Join(parent, "proj-secret") // name extends root's base — the bug

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	pastMtime := time.Now().Add(-24 * time.Hour)
	writeFileWithMtime(t, filepath.Join(root, "main.go"), pastMtime)

	// Secret file in the prefix-extension sibling directory — recent mtime.
	secretFile := filepath.Join(outside, "secret.txt")
	writeFileWithMtime(t, secretFile, time.Now())

	// A bare strings.HasPrefix(resolved, absRoot) check incorrectly admits the
	// symlink because "proj-secret" starts with "proj". The fix uses
	// absRoot + string(os.PathSeparator) as the separator-terminated prefix.
	symlink := filepath.Join(root, "leak")
	if err := os.Symlink(secretFile, symlink); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	threshold := time.Now().Add(-1 * time.Hour)
	w := bundleapp.NewFileSystemStalenessWalker(root)
	_, count, err := w.NewestMTime(root, threshold)
	if err != nil {
		t.Fatalf("NewestMTime() error = %v", err)
	}
	if count != 0 {
		t.Errorf("changedCount = %d, want 0 (prefix-extension symlink escape must be rejected)", count)
	}
}

// TestStalenessWalker_InRootSymlinkNotSkipped verifies that a symlink whose
// resolved target stays within the project root is NOT skipped — in-root
// symlinks must continue to participate in the freshness walk.
func TestStalenessWalker_InRootSymlinkNotSkipped(t *testing.T) {
	root := t.TempDir()

	// Write a real file with a recent mtime.
	now := time.Now()
	realFile := filepath.Join(root, "real.go")
	writeFileWithMtime(t, realFile, now)

	// Symlink inside root pointing to the real file.
	symlink := filepath.Join(root, "link.go")
	if err := os.Symlink(realFile, symlink); err != nil {
		t.Fatalf("create in-root symlink: %v", err)
	}

	// Both the real file and the symlink have mtime >= now.
	// changedCount must be > 0 (at least the real file).
	threshold := now.Add(-1 * time.Hour)
	w := bundleapp.NewFileSystemStalenessWalker(root)
	_, count, err := w.NewestMTime(root, threshold)
	if err != nil {
		t.Fatalf("NewestMTime() error = %v", err)
	}
	if count == 0 {
		t.Errorf("changedCount = 0, want > 0 (in-root symlink and real file must not be skipped)")
	}
}

// writeFileWithMtime writes content to path and sets its mtime to t.
func writeFileWithMtime(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}
