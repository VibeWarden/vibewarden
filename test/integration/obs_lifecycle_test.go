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
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// setSidecarPort rewrites server.port in the project's vibewarden.yaml. Call
// it before `vibew generate` so the rendered compose publishes the new port.
// The default written by `vibew init` is 8443; the function fails the test
// when that line is absent rather than silently leaving the default in place.
func setSidecarPort(t *testing.T, projectDir string, port int) {
	t.Helper()
	path := filepath.Join(projectDir, "vibewarden.yaml")
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading vibewarden.yaml: %v", err)
	}
	const defaultPortLine = "  port: 8443"
	if !strings.Contains(string(data), defaultPortLine) {
		t.Fatalf("vibewarden.yaml does not contain %q; cannot relocate the sidecar port:\n%s", defaultPortLine, data)
	}
	updated := strings.Replace(string(data), defaultPortLine, "  port: "+strconv.Itoa(port), 1)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}
}

// newObsProjectDir creates a project directory with a stable, DNS-safe name
// under a fresh temp dir. The compose project name is derived from the
// directory basename, and t.TempDir() ends in a numeric component ("001")
// which YAML parses as an integer — docker compose then rejects the generated
// file with "name must be a string".
func newObsProjectDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "obsdemo")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}
	return dir
}

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

	workDir := newObsProjectDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Initialise a minimal project. The flag is --port; there has never been
	// an --upstream-port flag, and the unknown-flag error used to be swallowed
	// by a t.Skipf so this test never actually ran (#1535).
	initCmd := exec.CommandContext(ctx, "vibew", "init",
		"--port", strconv.Itoa(upstreamPort),
		"--non-interactive",
	)
	initCmd.Dir = workDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("vibew init: %v\n%s", err, out)
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

	workDir := newObsProjectDir(t)

	// Initialise and generate.
	initCmd := exec.CommandContext(ctx, "vibew", "init",
		"--port", strconv.Itoa(upstreamPort),
		"--non-interactive",
	)
	initCmd.Dir = workDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("vibew init: %v\n%s", err, out)
	}

	// The generated compose builds the app service from the project directory,
	// so it needs a Dockerfile to build.
	writeMinimalAppDockerfile(t, workDir, upstreamPort)

	// Move the sidecar off the default 8443 host binding: any other stack on
	// the machine (or a concurrent run) holding that port fails the whole
	// test with "port is already allocated".
	setSidecarPort(t, workDir, 20000+rand.Intn(20000)) //nolint:gosec // test port selection, not crypto

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
		// --rmi local also drops the app image compose built from the fixture
		// Dockerfile, so repeated runs do not leak images.
		exec.CommandContext(cleanCtx, "docker", "compose", "-f", composeFile, "down", "--volumes", "--rmi", "local").Run() //nolint:errcheck
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
