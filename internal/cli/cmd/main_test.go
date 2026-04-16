package cmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestMain forces non-interactive mode for all CLI command tests.
// Tests that want to exercise interactive prompts must override cmd.IsTTY locally
// and restore it via t.Cleanup.
func TestMain(m *testing.M) {
	// In test runs stdin is never a real TTY; force non-interactive mode so that
	// tests run non-interactively without prompting for user input.
	cmd.IsTTY = func(*os.File) bool { return false }
	os.Exit(m.Run())
}

// scaffoldTestDir creates an isolated temporary directory for scaffold
// tests. It sets GIT_CEILING_DIRECTORIES to prevent git from discovering
// the host repo, clears GIT_DIR and GIT_WORK_TREE, and optionally
// chdir's into the directory. The environment and working directory are
// restored automatically when the test completes.
//
// Tests that pass chdir=true must NOT be marked t.Parallel() because
// os.Chdir is process-global.
func scaffoldTestDir(t *testing.T, chdir bool) string {
	t.Helper()
	dir := t.TempDir()

	// Prevent git from walking above the temp dir.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Setenv("GIT_DIR", "")
	t.Setenv("GIT_WORK_TREE", "")

	if chdir {
		origDir, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })
	}

	return dir
}
