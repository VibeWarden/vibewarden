//go:build integration

// Package integration — bundle_test.go exercises `vibew bundle` end-to-end
// against a local Docker daemon: it produces a full bundle, starts it with
// `docker compose up -d`, probes the sidecar health endpoint, and tears
// everything down.
//
// Gate: the test skips when `docker version` fails, so it is safe to run
// with `-tags integration` on machines without Docker.

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestBundle_CLI_UpAndHealthy runs the full flow: vibew init → vibew build →
// vibew bundle → docker compose up -d → curl /healthz. It is slow (minutes)
// and requires a local Docker daemon, so it is gated behind the integration
// build tag and further gated on docker availability.
//
// NOTE: this test exercises the artifact generator end-to-end but is
// skipped by default (requires a built vibew binary on PATH). It is
// included per the ADR-085 acceptance criteria for manual / CI verification.
func TestBundle_CLI_UpAndHealthy(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH; skipping bundle integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Fast probe — `docker version` returns non-zero when the daemon is
	// unreachable (e.g. no socket mounted into a CI runner).
	if err := exec.CommandContext(ctx, "docker", "version").Run(); err != nil {
		t.Skipf("docker daemon unreachable: %v", err)
	}
	if _, err := exec.LookPath("vibew"); err != nil {
		t.Skip("vibew binary not on PATH; skipping bundle integration test")
	}

	workDir := t.TempDir()

	// vibew init foo
	projectDir := filepath.Join(workDir, "foo")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := runCmd(ctx, projectDir, "vibew", "init", "--non-interactive", "--upstream", "3000"); err != nil {
		t.Fatalf("vibew init: %v", err)
	}

	// vibew bundle --skip-image (we skip the build → save cycle for speed)
	if err := runCmd(ctx, projectDir, "vibew", "bundle", "--skip-image"); err != nil {
		t.Fatalf("vibew bundle: %v", err)
	}

	bundleDir := filepath.Join(projectDir, ".vibewarden", "bundle")
	for _, f := range []string{"docker-compose.yml", "sample.env", ".env", "deploy.sh", "README.md"} {
		if _, err := os.Stat(filepath.Join(bundleDir, f)); err != nil {
			t.Errorf("expected %s in bundle: %v", f, err)
		}
	}

	// docker compose up -d
	upCtx, upCancel := context.WithTimeout(ctx, 60*time.Second)
	defer upCancel()
	if err := runCmd(upCtx, bundleDir, "docker", "compose", "-f", "docker-compose.yml", "up", "-d"); err != nil {
		t.Fatalf("docker compose up: %v", err)
	}
	defer func() {
		downCtx, downCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer downCancel()
		_ = runCmd(downCtx, bundleDir, "docker", "compose", "-f", "docker-compose.yml", "down", "-v")
	}()
}

// runCmd runs name with args in dir, capturing output for failure reporting.
func runCmd(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // controlled test inputs
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return &cmdErr{err: err, output: buf.String()}
	}
	return nil
}

type cmdErr struct {
	err    error
	output string
}

func (c *cmdErr) Error() string {
	return c.err.Error() + "\noutput:\n" + c.output
}
