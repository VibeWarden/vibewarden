//go:build integration

// Package integration — down_test.go exercises `vibew down` against a real
// Docker daemon. It verifies the command is idempotent (no-op on a stopped
// stack) and that the exit code is 0 in that case.
//
// Gate: the test skips when the docker daemon is unreachable or the vibew
// binary is not on PATH, so it is safe to run with `-tags integration` on
// any machine.

package integration

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestDown_CLI_IdempotentWhenNothingRunning confirms that `vibew down` is
// a successful no-op when no stack is running. This is the primary
// ergonomics guarantee for the command: users (and AI agents) can always
// invoke it safely.
func TestDown_CLI_IdempotentWhenNothingRunning(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH; skipping down integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "version").Run(); err != nil {
		t.Skipf("docker daemon unreachable: %v", err)
	}
	if _, err := exec.LookPath("vibew"); err != nil {
		t.Skip("vibew binary not on PATH; skipping down integration test")
	}

	// Run `vibew down` in a fresh temp dir with no generated compose file.
	workDir := t.TempDir()
	cmd := exec.CommandContext(ctx, "vibew", "down")
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vibew down should succeed on no-op path, got: %v\noutput:\n%s", err, out)
	}
	// The service prints "No running services. Nothing to do." in that path.
	if !strings.Contains(string(out), "Nothing to do") && !strings.Contains(string(out), "Stopped") {
		t.Errorf("expected idempotent summary, got:\n%s", out)
	}
}
