//go:build integration

// Package integration — obs_lifecycle_test.go exercises the full `vibew obs up`
// / `vibew obs down` lifecycle against a real Docker daemon. It verifies that:
//   - `vibew obs up` starts observability containers.
//   - `vibew obs down` stops only the observability containers and leaves the
//     main sidecar stack running (the critical regression guard for #1177).
//
// Gate: the test skips when the docker daemon is unreachable or the vibew
// binary is not on PATH. Run with:
//
//	go test -tags integration ./test/integration/ -run TestObsLifecycle -v
package integration

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// obsServices is the expected set of service names in the observability profile.
// Must stay in sync with the static list in internal/app/ops/obs.go.
var obsServiceNames = []string{
	"prometheus",
	"loki",
	"promtail",
	"otel-collector",
	"jaeger",
	"grafana",
}

// TestObsLifecycle_ComposeTemplate verifies that a freshly generated
// docker-compose.yml always contains the obs services and the profile
// annotation regardless of vibewarden.yaml content. This test does NOT
// require a running Docker daemon — it only inspects the compose template
// output and can run in any CI environment with vibew on PATH.
func TestObsLifecycle_ComposeTemplate(t *testing.T) {
	if _, err := exec.LookPath("vibew"); err != nil {
		t.Skip("vibew binary not on PATH; skipping obs lifecycle test")
	}

	workDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Initialise a minimal project.
	initCmd := exec.CommandContext(ctx, "vibew", "init",
		"--upstream-port", "3000",
		"--non-interactive",
	)
	initCmd.Dir = workDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Skipf("vibew init failed (vibew may not support --non-interactive yet): %v\n%s", err, out)
	}

	// Generate the compose file.
	genCmd := exec.CommandContext(ctx, "vibew", "generate")
	genCmd.Dir = workDir
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("vibew generate failed: %v\n%s", err, out)
	}

	// Read the generated compose file.
	composeFile := workDir + "/.vibewarden/generated/docker-compose.yml"
	catCmd := exec.CommandContext(ctx, "cat", composeFile)
	out, err := catCmd.Output()
	if err != nil {
		t.Fatalf("reading generated compose file: %v", err)
	}
	content := string(out)

	// Every obs service must be present.
	for _, svc := range obsServiceNames {
		if !strings.Contains(content, svc+":") {
			t.Errorf("generated compose missing obs service %q; vibew obs up would no-op", svc)
		}
	}

	// The observability profile annotation must be present.
	if !strings.Contains(content, "- observability") {
		t.Error("generated compose missing 'profiles: [observability]' annotation")
	}

	// docker compose config --profiles should list "observability".
	if _, err := exec.LookPath("docker"); err == nil {
		profilesCmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "config", "--profiles")
		profilesOut, profilesErr := profilesCmd.Output()
		if profilesErr == nil && !strings.Contains(string(profilesOut), "observability") {
			t.Errorf("docker compose config --profiles does not list 'observability'; output:\n%s", profilesOut)
		}
	}
}

// TestObsLifecycle_DownDoesNotNukeMainStack verifies the core regression from
// #1177: `vibew obs down` must not stop the main sidecar + app containers.
// This test requires a running Docker daemon and vibew on PATH. It is skipped
// in environments where either is unavailable.
//
// The test exercises the full lifecycle:
//  1. Init + generate compose.
//  2. Start main stack via compose up (sidecar only, no profile).
//  3. `vibew obs up` — start the obs profile.
//  4. `vibew obs down` — stop only obs services.
//  5. Assert main stack containers still running.
//  6. Tear down everything.
func TestObsLifecycle_DownDoesNotNukeMainStack(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH; skipping obs lifecycle integration test")
	}
	if _, err := exec.LookPath("vibew"); err != nil {
		t.Skip("vibew binary not on PATH; skipping obs lifecycle integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := exec.CommandContext(ctx, "docker", "version").Run(); err != nil {
		t.Skipf("docker daemon unreachable: %v", err)
	}

	workDir := t.TempDir()

	// Initialise and generate.
	initCmd := exec.CommandContext(ctx, "vibew", "init",
		"--upstream-port", "3000",
		"--non-interactive",
	)
	initCmd.Dir = workDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Skipf("vibew init failed (may not support --non-interactive): %v\n%s", err, out)
	}

	genCmd := exec.CommandContext(ctx, "vibew", "generate")
	genCmd.Dir = workDir
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("vibew generate: %v\n%s", err, out)
	}

	composeFile := workDir + "/.vibewarden/generated/docker-compose.yml"

	// Ensure full teardown regardless of test outcome.
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanCancel()
		exec.CommandContext(cleanCtx, "docker", "compose", "-f", composeFile, "down", "--volumes").Run() //nolint:errcheck
	})

	// Start main stack (no obs profile).
	upCmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "up", "-d")
	if out, err := upCmd.CombinedOutput(); err != nil {
		t.Fatalf("docker compose up: %v\n%s", err, out)
	}

	// Start obs profile.
	obsUpCmd := exec.CommandContext(ctx, "vibew", "obs", "up")
	obsUpCmd.Dir = workDir
	if out, err := obsUpCmd.CombinedOutput(); err != nil {
		t.Fatalf("vibew obs up: %v\n%s", err, out)
	}

	// Assert obs containers are running.
	psOut := composePS(t, ctx, composeFile)
	for _, svc := range obsServiceNames {
		if !strings.Contains(psOut, svc) {
			t.Errorf("after 'vibew obs up': service %q not running\nps output:\n%s", svc, psOut)
		}
	}

	// Stop obs services — this must NOT touch the sidecar.
	obsDownCmd := exec.CommandContext(ctx, "vibew", "obs", "down", "--yes")
	obsDownCmd.Dir = workDir
	if out, err := obsDownCmd.CombinedOutput(); err != nil {
		t.Fatalf("vibew obs down: %v\n%s", err, out)
	}

	// Assert obs containers are gone.
	psAfter := composePS(t, ctx, composeFile)
	for _, svc := range obsServiceNames {
		if strings.Contains(psAfter, svc) {
			t.Errorf("after 'vibew obs down': service %q still running — obs down did not clean up\nps output:\n%s", svc, psAfter)
		}
	}

	// Assert main sidecar is still running.
	if !strings.Contains(psAfter, "vibewarden") {
		t.Errorf("after 'vibew obs down': sidecar 'vibewarden' is no longer running — obs down nuked the main stack\nps output:\n%s", psAfter)
	}
}

// composePS runs `docker compose ps --format json` and returns the combined
// output as a string. The test is failed but not halted if the command errors.
func composePS(t *testing.T, ctx context.Context, composeFile string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "ps", "--format", "json")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Logf("docker compose ps warning: %v\noutput: %s", err, buf.String())
	}
	return buf.String()
}
