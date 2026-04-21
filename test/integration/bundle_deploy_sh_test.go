//go:build integration

// Package integration — bundle_deploy_sh_test.go exercises the rendered
// deploy.sh end-to-end against a local sshd + stubbed remote `docker`.
//
// Per ADR-088 §Test strategy, harness option (c): shell out to a local
// sshd when present, auto-skip otherwise. Rationale: testcontainers-go
// sshd+DinD is heavy and flaky on macOS runners; a mock SSH server
// would re-implement enough of openssh to be a second bug farm.
//
// Run:
//
//	go test -race -tags integration ./test/integration/ -run TestBundleDeploySH -v

package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBundleDeploySH exercises the rendered deploy.sh against a disposable
// remote target composed of a local sshd plus a stubbed `docker` binary.
//
// Assertions:
//
//  1. `./deploy.sh testuser@127.0.0.1:$tmpdir` exits 0.
//  2. Bundle dir was scp'ed to the remote tempdir (marker file present).
//  3. `docker compose up -d` was invoked (stub logs the invocation).
//  4. The healthcheck HTTP probe reached /_vibewarden/health on the baked port.
func TestBundleDeploySH(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Skip sentinels — mirror TestMultiSite's pattern.
	if _, err := exec.LookPath("sshd"); err != nil {
		t.Skip("integration prerequisite missing: sshd not on PATH — install openssh-server or run under make integration on a Linux host with docker")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("integration prerequisite missing: ssh-keygen not on PATH")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("integration prerequisite missing: bash not on PATH")
	}

	// This test only makes sense against a real sshd on a Linux-like
	// runner. The harness below assumes openssh conventions. Bail out
	// cheaply on any failure with a descriptive skip message — per
	// CLAUDE.md guidance, integration prerequisites auto-skip rather
	// than fail.
	harness := newSSHDHarness(t)
	defer harness.Close()

	// Start a tiny in-process HTTP server that stands in for the sidecar's
	// healthcheck endpoint. We can't actually run the sidecar in this
	// test, so we point the script at the stub and assert it was probed.
	healthProbed := make(chan struct{}, 4)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_vibewarden/health" {
			select {
			case healthProbed <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer stub.Close()

	_, stubPort, err := net.SplitHostPort(strings.TrimPrefix(stub.URL, "http://"))
	if err != nil {
		t.Fatalf("parsing stub URL %q: %v", stub.URL, err)
	}

	// Render a deploy.sh with healthPort pointing at our stub.
	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "image.tar"), []byte("fake-image"), 0o600); err != nil {
		t.Fatalf("writing fake image.tar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "docker-compose.yml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("writing fake compose: %v", err)
	}

	// Deploy.sh body — we render it manually here (we don't need the full
	// bundle pipeline; we just need a script pointing at the stub port).
	// The canonical render is unit-tested in bundle_extras_test.go.
	deployBody := renderDeploySHForIntegration(stubPort)
	deployPath := filepath.Join(bundleDir, "deploy.sh")
	if err := os.WriteFile(deployPath, []byte(deployBody), 0o750); err != nil { //nolint:gosec // executable script
		t.Fatalf("writing deploy.sh: %v", err)
	}

	// Stub `docker` on the remote side: the harness writes a logging
	// shell script into a per-session directory on the server's PATH.
	dockerLog := filepath.Join(harness.sessionDir, "docker.log")
	dockerStub := "#!/usr/bin/env bash\necho \"$@\" >> " + dockerLog + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(harness.binDir, "docker"), []byte(dockerStub), 0o755); err != nil { //nolint:gosec // test stub
		t.Fatalf("writing docker stub: %v", err)
	}
	// Stub remote `curl` to hit the stub server regardless of the script's
	// hardcoded localhost:PORT — the script ssh's to the remote then runs
	// curl there, and our harness is running on localhost, so the real
	// localhost:$stubPort will work as-is.

	// Run the script.
	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	remotePath := filepath.Join(harness.sessionDir, "deployed")
	target := fmt.Sprintf("%s@127.0.0.1:%s", harness.user, remotePath)

	cmd := exec.CommandContext(runCtx, "bash", deployPath, target)
	cmd.Dir = bundleDir
	cmd.Env = append(os.Environ(), harness.envVars()...)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("deploy.sh failed: %v\noutput:\n%s", runErr, out)
	}

	// Assertion 2: bundle was scp'ed to the remote path.
	if _, err := os.Stat(filepath.Join(remotePath, "deploy.sh")); err != nil {
		t.Errorf("expected deploy.sh at remote path %s: %v", remotePath, err)
	}

	// Assertion 3: docker compose up -d was invoked (via the stub log).
	logBytes, err := os.ReadFile(dockerLog) //nolint:gosec // test-owned path
	if err != nil {
		t.Fatalf("reading docker stub log: %v", err)
	}
	if !strings.Contains(string(logBytes), "compose up -d") {
		t.Errorf("docker stub log missing 'compose up -d':\n%s", logBytes)
	}

	// Assertion 4: healthcheck was probed.
	select {
	case <-healthProbed:
		// ok
	case <-time.After(1 * time.Second):
		t.Errorf("/_vibewarden/health was not probed within 1s of script completion")
	}
}

// renderDeploySHForIntegration produces a deploy.sh body with the given
// health port. Duplicates the production render minimally so the
// integration test stays self-contained — the real renderer is covered
// by the unit tests.
func renderDeploySHForIntegration(healthPort string) string {
	return `#!/usr/bin/env bash
# Reference deploy script generated by ` + "`vibew bundle`" + ` for myproject.
# Runs LOCALLY.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <user@host[:/remote/path]>" >&2
  exit 1
fi

TARGET="$1"
USER_HOST="${TARGET%%:*}"
REMOTE_PATH="${TARGET#*:}"
if [[ "$REMOTE_PATH" == "$TARGET" ]]; then REMOTE_PATH="~/vibewarden-bundle"; fi

scp -r . "$USER_HOST:$REMOTE_PATH/"
ssh "$USER_HOST" "cd $REMOTE_PATH && docker load -i image.tar && docker compose up -d"
sleep 1
if ! ssh "$USER_HOST" "curl -fsSL -m 10 'http://localhost:` + healthPort + `/_vibewarden/health'" >/dev/null; then
  ssh "$USER_HOST" "cd $REMOTE_PATH && docker compose logs --tail 50" >&2
  echo "deploy.sh: healthcheck failed — dumped last 50 log lines above" >&2
  exit 1
fi
echo "deploy.sh: healthy ($USER_HOST:$REMOTE_PATH)"
`
}

// sshdHarness starts a per-test sshd on a loopback port and provisions an
// ED25519 keypair so scp/ssh invocations from the test process
// authenticate without touching the user's ~/.ssh/.
type sshdHarness struct {
	user       string
	port       string
	sessionDir string
	binDir     string
	keyPath    string
	cmd        *exec.Cmd
	cancel     context.CancelFunc
}

func newSSHDHarness(t *testing.T) *sshdHarness {
	t.Helper()

	sessionDir, err := os.MkdirTemp("", "vw-sshd-*")
	if err != nil {
		t.Fatalf("mkdir sessionDir: %v", err)
	}

	binDir := filepath.Join(sessionDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil { //nolint:gosec // test-owned path
		t.Fatalf("mkdir binDir: %v", err)
	}

	// Generate host key + user key.
	hostKey := filepath.Join(sessionDir, "host_ed25519")
	if err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", hostKey, "-q").Run(); err != nil {
		t.Skipf("integration prerequisite missing: ssh-keygen failed: %v", err)
	}
	userKey := filepath.Join(sessionDir, "id_ed25519")
	if err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", userKey, "-q").Run(); err != nil {
		t.Skipf("integration prerequisite missing: ssh-keygen failed: %v", err)
	}
	pubBytes, err := os.ReadFile(userKey + ".pub") //nolint:gosec // test-owned path
	if err != nil {
		t.Fatalf("reading pubkey: %v", err)
	}

	// Minimal sshd_config.
	authKeys := filepath.Join(sessionDir, "authorized_keys")
	if err := os.WriteFile(authKeys, pubBytes, 0o600); err != nil {
		t.Fatalf("writing authorized_keys: %v", err)
	}

	// Pick a free loopback port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("picking loopback port: %v", err)
	}
	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()

	sshdConfig := filepath.Join(sessionDir, "sshd_config")
	cfgBody := fmt.Sprintf(`Port %s
ListenAddress 127.0.0.1
HostKey %s
PidFile %s/sshd.pid
AuthorizedKeysFile %s
PasswordAuthentication no
PubkeyAuthentication yes
UsePAM no
StrictModes no
LogLevel QUIET
`, port, hostKey, sessionDir, authKeys)
	if err := os.WriteFile(sshdConfig, []byte(cfgBody), 0o600); err != nil {
		t.Fatalf("writing sshd_config: %v", err)
	}

	// Start sshd in the foreground so we can kill it cleanly.
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sshd", "-D", "-e", "-f", sshdConfig)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Skipf("integration prerequisite missing: cannot start sshd: %v", err)
	}

	// Wait briefly for sshd to bind. Give up and skip on failure.
	ready := false
	for i := 0; i < 20; i++ {
		conn, dialErr := net.DialTimeout("tcp", "127.0.0.1:"+port, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		cancel()
		_ = cmd.Process.Kill()
		t.Skipf("integration prerequisite missing: sshd did not bind on 127.0.0.1:%s within 2s", port)
	}

	// Current OS user — sshd authenticates against the process's login
	// shell by default. This harness uses the invoking user's account,
	// which is why it's gated behind //go:build integration: CI-only.
	currentUser := os.Getenv("USER")
	if currentUser == "" {
		currentUser = "root"
	}

	return &sshdHarness{
		user:       currentUser,
		port:       port,
		sessionDir: sessionDir,
		binDir:     binDir,
		keyPath:    userKey,
		cmd:        cmd,
		cancel:     cancel,
	}
}

// envVars returns env overrides that point ssh/scp at the harness keypair
// and loopback port, so the script does not depend on the user's ~/.ssh/.
func (h *sshdHarness) envVars() []string {
	// Wrap ssh/scp via $SSH variables isn't universal; instead we override
	// PATH with wrappers that inject -i and -p flags transparently.
	sshWrap := filepath.Join(h.sessionDir, "ssh")
	scpWrap := filepath.Join(h.sessionDir, "scp")

	sshScript := fmt.Sprintf("#!/usr/bin/env bash\nexec /usr/bin/ssh -i %s -p %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o BatchMode=yes \"$@\"\n", h.keyPath, h.port)
	scpScript := fmt.Sprintf("#!/usr/bin/env bash\nexec /usr/bin/scp -i %s -P %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o BatchMode=yes \"$@\"\n", h.keyPath, h.port)

	_ = os.WriteFile(sshWrap, []byte(sshScript), 0o755) //nolint:gosec // test wrapper
	_ = os.WriteFile(scpWrap, []byte(scpScript), 0o755) //nolint:gosec // test wrapper

	// Prepend the harness bin dir and session dir so our stubs/wrappers
	// shadow the real binaries for this test only.
	path := h.sessionDir + ":" + h.binDir + ":" + os.Getenv("PATH")
	return []string{"PATH=" + path}
}

// Close stops the sshd and removes all harness state.
func (h *sshdHarness) Close() {
	if h.cancel != nil {
		h.cancel()
	}
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
		_ = h.cmd.Wait()
	}
	_ = os.RemoveAll(h.sessionDir)
}
