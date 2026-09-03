//go:build integration

// Package integration — compose_limits_test.go proves that the container
// resource limits VibeWarden renders into docker-compose.yml (ADR-111) are
// actually enforced by the Docker daemon under plain `docker compose up`,
// non-swarm.
//
// This is the load-bearing test for issue #1306. The render-shape assertions in
// internal/app/generate and internal/app/bundle check that the right strings
// appear in the right service block; only this test checks that the daemon
// honours them. A compose key that Docker silently ignores would pass every
// unit test in the repo and ship a security control that does nothing — the
// exact failure mode CLAUDE.md's "behavioural test for silent no-ops" rule
// exists to prevent.
//
// Prerequisites:
//   - Docker daemon running
//   - vibewarden:local-test image built (`make integration` does this)
//
// Run:
//
//	go test -tags integration ./test/integration/ -run TestComposeResourceLimits -v
package integration

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
)

// Expected HostConfig values for the documented defaults: 512MB memory,
// 1.0 CPU, 200 PIDs. These are the byte/nano-core forms Docker stores.
const (
	wantMemoryBytes = "536870912"
	wantNanoCPUs    = "1000000000"
	wantPidsLimit   = "200"
)

// TestComposeResourceLimits_Enforced renders a compose file through the
// production generate path and verifies, via `docker inspect`, that the
// resulting sidecar container really carries the configured caps.
func TestComposeResourceLimits_Enforced(t *testing.T) {
	requireDockerAndImage(t)

	tests := []struct {
		name       string
		memLimit   string
		cpuLimit   float64
		pidsLimit  int
		wantMemory string
		wantCPUs   string
		wantPids   []string // any of these is acceptable
	}{
		{
			name:       "documented defaults are enforced",
			memLimit:   "512MB",
			cpuLimit:   1.0,
			pidsLimit:  200,
			wantMemory: wantMemoryBytes,
			wantCPUs:   wantNanoCPUs,
			wantPids:   []string{wantPidsLimit},
		},
		{
			name:       "zero disables every cap",
			memLimit:   "0",
			cpuLimit:   0,
			pidsLimit:  0,
			wantMemory: "0",
			wantCPUs:   "0",
			// Docker reports an unset PID limit as either 0 or <nil>
			// depending on API version.
			wantPids: []string{"0", "<nil>", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := renderComposeProject(t, tt.memLimit, tt.cpuLimit, tt.pidsLimit)
			id := composeUpSidecar(t, dir)

			memory := dockerInspect(t, id, "{{.HostConfig.Memory}}")
			if memory != tt.wantMemory {
				t.Errorf("HostConfig.Memory = %q, want %q", memory, tt.wantMemory)
			}
			cpus := dockerInspect(t, id, "{{.HostConfig.NanoCpus}}")
			if cpus != tt.wantCPUs {
				t.Errorf("HostConfig.NanoCpus = %q, want %q", cpus, tt.wantCPUs)
			}
			pids := dockerInspect(t, id, "{{.HostConfig.PidsLimit}}")
			if !anyEquals(tt.wantPids, pids) {
				t.Errorf("HostConfig.PidsLimit = %q, want one of %v", pids, tt.wantPids)
			}
		})
	}
}

// TestComposeResourceLimits_GoMemLimitInContainer verifies the derived
// GOMEMLIMIT actually reaches the sidecar process environment. Without it a
// memory cap makes the sidecar *more* likely to be OOM-killed, because the Go
// GC never sees the cgroup ceiling (ADR-111).
func TestComposeResourceLimits_GoMemLimitInContainer(t *testing.T) {
	requireDockerAndImage(t)

	dir := renderComposeProject(t, "512MB", 1.0, 200)
	id := composeUpSidecar(t, dir)

	env := dockerInspect(t, id, "{{range .Config.Env}}{{println .}}{{end}}")
	if !strings.Contains(env, "GOMEMLIMIT=483183820B") {
		t.Errorf("container env missing GOMEMLIMIT=483183820B, got:\n%s", env)
	}
}

// requireDockerAndImage skips the test unless a reachable Docker daemon and the
// locally built sidecar image are both available. Mirrors bundle_test.go.
func requireDockerAndImage(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH; skipping compose resource limits test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "version").Run(); err != nil {
		t.Skipf("docker daemon unreachable: %v", err)
	}
	if err := exec.CommandContext(ctx, "docker", "image", "inspect", sidecarImage).Run(); err != nil {
		t.Skipf("image %s not built locally; run `make integration`", sidecarImage)
	}
}

// renderComposeProject writes a runnable compose project into a temp dir using
// the production generate path, and returns the directory.
func renderComposeProject(t *testing.T, memLimit string, cpuLimit float64, pidsLimit int) string {
	t.Helper()

	// A high random port keeps concurrent runs from colliding on the host.
	port := 20000 + rand.Intn(20000) //nolint:gosec // test port selection, not crypto

	cfg := &config.Config{
		// A unique compose project name so each subtest is isolated and
		// teardown cannot touch another run's containers.
		Name:    "vwlimits-" + randSuffix(),
		Profile: "dev",
		Server: config.ServerConfig{
			Host: "0.0.0.0", Port: port,
			MemLimit: memLimit, CPULimit: cpuLimit, PidsLimit: pidsLimit,
		},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		TLS:      config.TLSConfig{Enabled: false},
		// Use the locally built image so the test never pulls from ghcr.io
		// and never depends on a release tag existing.
		SidecarImage: sidecarImage,
	}

	dir := t.TempDir()
	svc := generate.NewService(templateadapter.NewRenderer(configtemplates.FS))
	if err := svc.Generate(context.Background(), cfg.ToGeneratorInput(), dir); err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The sidecar service bind-mounts ./vibewarden.yaml. Write a minimal one so
	// Docker mounts a file rather than silently creating a directory.
	sidecarYAML := "server:\n  host: \"0.0.0.0\"\n  port: " + strconv.Itoa(port) +
		"\nupstream:\n  host: \"127.0.0.1\"\n  port: 3000\ntls:\n  enabled: false\n"
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(sidecarYAML), 0o600); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}

	return dir
}

// composeUpSidecar starts only the vibewarden service from the rendered compose
// file and returns its container ID. Teardown is registered with t.Cleanup.
//
// The sidecar's upstream is deliberately unreachable: the container only has to
// exist for `docker inspect` to report its HostConfig.
func composeUpSidecar(t *testing.T, dir string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := runCmd(ctx, dir, "docker", "compose", "-f", "docker-compose.yml", "up", "-d", "vibewarden"); err != nil {
		t.Fatalf("docker compose up: %v", err)
	}
	t.Cleanup(func() {
		downCtx, downCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer downCancel()
		_ = runCmd(downCtx, dir, "docker", "compose", "-f", "docker-compose.yml", "down", "-t", "0", "-v")
	})

	out, err := outputOf(ctx, dir, "docker", "compose", "-f", "docker-compose.yml", "ps", "-aq", "vibewarden")
	if err != nil {
		t.Fatalf("docker compose ps: %v", err)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		t.Fatal("docker compose ps returned no container id for the vibewarden service")
	}
	// ps may return several lines if the service was recreated; take the first.
	return strings.Fields(id)[0]
}

// dockerInspect returns the trimmed result of `docker inspect --format`.
func dockerInspect(t *testing.T, containerID, format string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := outputOf(ctx, "", "docker", "inspect", containerID, "--format", format)
	if err != nil {
		t.Fatalf("docker inspect %s: %v", containerID, err)
	}
	return strings.TrimSpace(out)
}

// outputOf runs a command and returns its stdout. Compose writes warnings to
// stderr, so stdout must be captured separately or a container ID read back
// from `docker compose ps -q` arrives with a log line glued to the front.
func outputOf(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // controlled test inputs
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// anyEquals reports whether needle matches one of the accepted values.
func anyEquals(accepted []string, needle string) bool {
	for _, s := range accepted {
		if s == needle {
			return true
		}
	}
	return false
}

// randSuffix returns a short lowercase suffix for compose project isolation.
func randSuffix() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))] //nolint:gosec // test naming, not crypto
	}
	return string(b)
}
