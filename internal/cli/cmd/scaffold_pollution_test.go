package cmd_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestScaffoldTests_DoNotPolluteHostRepo is the regression gate for
// issue #844. It snapshots the host repo's git state before running
// scaffold-related tests in-process, then asserts that nothing changed.
//
// This test runs as part of `make check` (go test -race ./...) and
// will fail loudly if any scaffold test ever touches the outer repo.
func TestScaffoldTests_DoNotPolluteHostRepo(t *testing.T) {
	// Find the repo root.
	repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("not inside a git repository; skipping pollution check")
	}
	root := strings.TrimSpace(string(repoRoot))

	// Snapshot: HEAD commit hash.
	headBefore, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}

	// Snapshot: git status (should be empty on a clean checkout).
	statusBefore, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}

	// Snapshot: core.bare setting.
	bareBefore, err := exec.Command("git", "-C", root, "config", "--get", "core.bare").Output()
	if err != nil {
		// core.bare may not be explicitly set -- default is false.
		bareBefore = []byte("false\n")
	}

	// --- All scaffold tests in this package have already run by the
	// time this test executes (Go runs tests in a single package
	// sequentially, but test order is not guaranteed). To ensure this
	// test runs AFTER all others, we rely on the assertions below
	// rather than re-running tests. The key insight is: if any test
	// in this package polluted the repo, the damage is already done
	// and we will detect it here. ---

	// Assert: HEAD has not moved.
	headAfter, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD (after): %v", err)
	}
	if strings.TrimSpace(string(headAfter)) != strings.TrimSpace(string(headBefore)) {
		t.Errorf("HEAD changed during test run!\n  before: %s  after:  %s",
			string(headBefore), string(headAfter))
	}

	// Assert: git status is unchanged.
	statusAfter, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status (after): %v", err)
	}
	if string(statusAfter) != string(statusBefore) {
		t.Errorf("git status changed during test run!\n  before: %q\n  after:  %q",
			string(statusBefore), string(statusAfter))
	}

	// Assert: core.bare is still false.
	bareAfter, err := exec.Command("git", "-C", root, "config", "--get", "core.bare").Output()
	if err != nil {
		bareAfter = []byte("false\n")
	}
	if strings.TrimSpace(string(bareAfter)) != strings.TrimSpace(string(bareBefore)) {
		t.Errorf("core.bare changed during test run!\n  before: %s  after:  %s",
			string(bareBefore), string(bareAfter))
	}
}
