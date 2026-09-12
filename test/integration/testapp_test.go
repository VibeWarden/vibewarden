//go:build integration

// Package integration — testapp_test.go provides the minimal upstream app
// fixture shared by the integration tests.
//
// Every compose file VibeWarden generates wires the sidecar behind an `app`
// service that must become healthy (`depends_on: app: condition:
// service_healthy`) before the sidecar starts, and the generated healthcheck
// is `wget -q --spider http://127.0.0.1:<port>/health`. Tests therefore need a
// real image that serves 200 on /health and ships wget — busybox httpd does
// both in ~4 MB. Without it `docker compose up` either tries to pull a
// nonexistent `<project>-app:latest` or builds from a project directory with
// no Dockerfile (#1535).

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// upstreamPort is the port the fixture app listens on. It must match the
// `--port` value the tests pass to `vibew init`, because the generated
// healthcheck probes that port inside the app container.
const upstreamPort = 3000

// minimalAppDockerfile renders the fixture Dockerfile: a busybox httpd serving
// a static /health document on port.
func minimalAppDockerfile(port int) string {
	return fmt.Sprintf(`FROM busybox:1.37
RUN mkdir -p /www && printf 'ok\n' > /www/health
EXPOSE %d
CMD ["httpd", "-f", "-p", "%d", "-h", "/www"]
`, port, port)
}

// writeMinimalAppDockerfile writes the fixture Dockerfile into dir so that a
// compose file using `build:` (the `vibew dev` / `vibew generate` layout) can
// build the app service.
func writeMinimalAppDockerfile(t *testing.T, dir string, port int) {
	t.Helper()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(minimalAppDockerfile(port)), 0o600); err != nil {
		t.Fatalf("writing fixture Dockerfile: %v", err)
	}
}

// buildMinimalAppImage builds the fixture Dockerfile in dir and tags it as
// tag, for compose files using `image:` (the `vibew bundle` layout, which
// rewrites build mode to `<project>-app:latest`). The image is removed on
// test cleanup.
func buildMinimalAppImage(ctx context.Context, t *testing.T, dir, tag string, port int) {
	t.Helper()
	writeMinimalAppDockerfile(t, dir, port)

	buildCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := runCmd(buildCtx, dir, "docker", "build", "--tag", tag, "."); err != nil {
		t.Fatalf("building fixture app image %s: %v", tag, err)
	}

	t.Cleanup(func() {
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer rmCancel()
		cmd := exec.CommandContext(rmCtx, "docker", "image", "rm", "--force", tag) //nolint:gosec // controlled test inputs
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("removing fixture app image %s: %v\n%s", tag, err, out)
		}
	})
}
