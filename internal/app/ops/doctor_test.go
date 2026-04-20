package ops_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakePortChecker is a test double for ports.PortChecker.
type fakePortChecker struct {
	// available maps port -> available
	available map[int]bool
}

func (f *fakePortChecker) IsPortAvailable(_ context.Context, _ string, port int) (bool, error) {
	if v, ok := f.available[port]; ok {
		return v, nil
	}
	return true, nil
}

// fakePortOwnerProbe is a test double for ports.PortOwnerProbe that returns
// a fixed owner regardless of input.
type fakePortOwnerProbe struct {
	owner ports.PortOwner
}

func (f *fakePortOwnerProbe) ProbeOwner(_ context.Context, _ string, _ int) ports.PortOwner {
	return f.owner
}

// fakeRemoteExecutor is a test double for ports.RemoteExecutor.
type fakeRemoteExecutor struct {
	runResponses map[string]runResult
	runErr       error
}

type runResult struct {
	output string
	err    error
}

func (f *fakeRemoteExecutor) Run(_ context.Context, cmd string) (string, error) {
	if f.runErr != nil {
		return "", f.runErr
	}
	if r, ok := f.runResponses[cmd]; ok {
		return r.output, r.err
	}
	// Default: echo commands succeed
	if strings.HasPrefix(cmd, "echo ") {
		return "ok", nil
	}
	return "", errors.New("unknown command")
}

func (f *fakeRemoteExecutor) RunStream(_ context.Context, _ string, _, _ io.Writer) error {
	return nil
}

func (f *fakeRemoteExecutor) Transfer(_ context.Context, _, _ string, _ bool) error {
	return nil
}

func (f *fakeRemoteExecutor) TransferExcluding(_ context.Context, _, _ string, _ bool, _ []string) error {
	return nil
}

func (f *fakeRemoteExecutor) TransferFile(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeRemoteExecutor) DryRunTransfer(_ context.Context, _, _ string) ([]string, error) {
	return []string{}, nil
}

// reachableHealthChecker is a fakeHealthChecker that reports upstream as reachable.
func reachableHealthChecker() *fakeHealthChecker {
	return &fakeHealthChecker{
		responses: map[string]healthResponse{
			"http://127.0.0.1:3000": {ok: true, statusCode: 200},
		},
	}
}

// unreachableHealthChecker returns a fakeHealthChecker where upstream is unreachable.
func unreachableHealthChecker() *fakeHealthChecker {
	return &fakeHealthChecker{
		responses: map[string]healthResponse{},
	}
}

// noContainersCompose returns a fakeCompose whose PS returns an empty slice.
func noContainersCompose() *fakeCompose {
	return &fakeCompose{
		versionStr: "Docker Compose version v2.35.1",
		psResult:   nil,
	}
}

// healthyContainersCompose returns a fakeCompose with one healthy running container.
func healthyContainersCompose() *fakeCompose {
	return &fakeCompose{
		versionStr: "Docker Compose version v2.35.1",
		psResult: []ports.ContainerInfo{
			{Name: "vibewarden-proxy-1", Service: "proxy", State: "running", Health: "healthy"},
		},
	}
}

// defaultOpts returns a DoctorOptions with a temp workDir so generated-file checks
// do not depend on the actual filesystem.
func defaultOpts(t *testing.T) ops.DoctorOptions {
	t.Helper()
	return ops.DoctorOptions{
		ConfigPath: "vibewarden.yaml",
		WorkDir:    t.TempDir(), // no generated files present by default
	}
}

// optsWithGeneratedFile creates a DoctorOptions whose workDir contains the
// expected generated docker-compose.yml so that check passes.
func optsWithGeneratedFile(t *testing.T) ops.DoctorOptions {
	t.Helper()
	dir := t.TempDir()
	genDir := filepath.Join(dir, ".vibewarden", "generated")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatalf("create generated dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "docker-compose.yml"), []byte("version: '3'"), 0o644); err != nil {
		t.Fatalf("create docker-compose.yml: %v", err)
	}
	return ops.DoctorOptions{
		ConfigPath: "vibewarden.yaml",
		WorkDir:    dir,
	}
}

// doctorConfig returns a defaultConfig() override with TLS.Provider set to
// "external" so the live-TLS cert check is skipped for doctor tests that
// do not specifically exercise it. Tests that do exercise the TLS check
// set Provider to "self-signed" and point Server.Host/Port at a local
// httptest.NewTLSServer (see startTLSTestSidecar).
func doctorConfig() *config.Config {
	cfg := defaultConfig()
	cfg.TLS.Provider = "external"
	return cfg
}

// startTLSTestSidecar boots a local TLS server that mimics the VibeWarden
// sidecar on /_vibewarden/health. The server uses a short-lived self-signed
// certificate whose NotAfter is controlled by notAfter. It returns the host
// and port the test should point cfg.Server.Host/Port at, plus the port
// already registered as "available=false" on the supplied portChecker so
// the proxy-port check mirrors a real sidecar-in-use state.
func startTLSTestSidecar(t *testing.T, notBefore, notAfter time.Time) (host string, port int, cleanup func()) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	tlsCert := tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: key}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","version":"test"}`))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	srv.StartTLS()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return u.Hostname(), p, srv.Close
}

func TestDoctorService_Run_AllPassing(t *testing.T) {
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()

	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	opts := optsWithGeneratedFile(t)
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !allOK {
		t.Error("expected allOK = true when all checks pass")
	}

	out := buf.String()
	for _, want := range []string{
		"VibeWarden Doctor",
		"Docker daemon",
		"Docker Compose",
		"Config file",
		"Proxy port",
		"Generated files",
		"Container health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestDoctorService_Run_DockerNotRunning(t *testing.T) {
	fc := &fakeCompose{
		infoErr:    errors.New("docker daemon not running"),
		versionStr: "Docker Compose version v2.35.1",
	}
	pc := &fakePortChecker{}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when docker daemon is not running")
	}

	out := buf.String()
	if !strings.Contains(out, "not running") {
		t.Errorf("expected 'not running' in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_DockerComposeNotAvailable(t *testing.T) {
	fc := &fakeCompose{
		infoErr:    nil,
		versionErr: errors.New("docker compose: command not found"),
	}
	pc := &fakePortChecker{}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when docker compose is not available")
	}

	out := buf.String()
	if !strings.Contains(out, "not available") {
		t.Errorf("expected 'not available' in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_PortInUse(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: false}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when proxy port is in use")
	}

	out := buf.String()
	if !strings.Contains(out, "already in use") {
		t.Errorf("expected 'already in use' in output, got:\n%s", out)
	}
}

// TestDoctorService_CheckPort_OwnershipMatrix exercises the port ownership
// decision table from ADR-084.
func TestDoctorService_CheckPort_OwnershipMatrix(t *testing.T) {
	tests := []struct {
		name         string
		available    bool
		probe        ports.PortOwnerProbe
		wantSeverity ops.Severity
		wantDetail   string
	}{
		{
			name:         "port free (probe not consulted)",
			available:    true,
			probe:        &fakePortOwnerProbe{owner: ports.OwnerForeign},
			wantSeverity: ops.SeverityOK,
			wantDetail:   "is available",
		},
		{
			name:         "port bound + probe returns OwnerVibeWarden → OK",
			available:    false,
			probe:        &fakePortOwnerProbe{owner: ports.OwnerVibeWarden},
			wantSeverity: ops.SeverityOK,
			wantDetail:   "in use by local vibew dev (expected)",
		},
		{
			name:         "port bound + probe returns OwnerForeign → FAIL",
			available:    false,
			probe:        &fakePortOwnerProbe{owner: ports.OwnerForeign},
			wantSeverity: ops.SeverityFail,
			wantDetail:   "already in use",
		},
		{
			name:         "port bound + no probe wired → FAIL (back-compat)",
			available:    false,
			probe:        nil,
			wantSeverity: ops.SeverityFail,
			wantDetail:   "already in use",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			fc := noContainersCompose()
			pc := &fakePortChecker{available: map[int]bool{8443: tt.available}}
			hc := reachableHealthChecker()
			svc := ops.NewDoctorService(fc, pc, hc)
			if tt.probe != nil {
				svc = svc.WithPortOwnerProbe(tt.probe)
			}
			cfg := doctorConfig()

			var buf bytes.Buffer
			opts := defaultOpts(t)
			opts.JSON = true
			if _, err := svc.Run(context.Background(), cfg, opts, &buf); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var results []ops.CheckResult
			if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
				t.Fatalf("parse json output: %v\nbody:\n%s", err, buf.String())
			}
			var got ops.CheckResult
			for _, r := range results {
				if r.Name == "Proxy port" {
					got = r
					break
				}
			}
			if got.Name == "" {
				t.Fatalf("Proxy port check missing from results")
			}
			if got.Severity != tt.wantSeverity {
				t.Errorf("severity = %q, want %q (detail=%q)", got.Severity, tt.wantSeverity, got.Detail)
			}
			if !strings.Contains(got.Detail, tt.wantDetail) {
				t.Errorf("detail %q missing substring %q", got.Detail, tt.wantDetail)
			}
		})
	}
}

func TestDoctorService_Run_ConfigPathInOutput(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	opts := defaultOpts(t)
	opts.ConfigPath = "custom.yaml"
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "custom.yaml") {
		t.Errorf("expected config path in output, got:\n%s", out)
	}
}

func TestDoctorService_ChecksAreIndependent(t *testing.T) {
	// All checks fail — report should still contain all check names.
	fc := &fakeCompose{
		infoErr:    errors.New("docker not running"),
		versionErr: errors.New("compose not found"),
		psErr:      errors.New("ps failed"),
	}
	pc := &fakePortChecker{available: map[int]bool{8443: false}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"Docker daemon",
		"Docker Compose",
		"Config file",
		"Proxy port",
		"Generated files",
		"Container health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check %q missing from output\ngot:\n%s", want, out)
		}
	}
}

func TestDoctorService_Run_GeneratedFileMissing_IsWarn(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	// defaultOpts uses an empty temp dir — no generated file present.
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WARN does not cause allOK=false.
	if !allOK {
		t.Error("expected allOK = true because generated-file absence is WARN, not FAIL")
	}

	out := buf.String()
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected [WARN] badge in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Generated files") {
		t.Errorf("expected 'Generated files' check name in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_UnhealthyContainer_IsFail(t *testing.T) {
	fc := &fakeCompose{
		versionStr: "Docker Compose version v2.35.1",
		psResult: []ports.ContainerInfo{
			{Name: "vibewarden-proxy-1", Service: "proxy", State: "running", Health: "unhealthy"},
		},
	}
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, optsWithGeneratedFile(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when a container is unhealthy")
	}

	out := buf.String()
	if !strings.Contains(out, "unhealthy") {
		t.Errorf("expected 'unhealthy' in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_JSONOutput(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	opts := optsWithGeneratedFile(t)
	opts.JSON = true
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []ops.CheckResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("output is not valid JSON: %v\ngot:\n%s", err, buf.String())
	}
	if len(results) == 0 {
		t.Error("expected at least one check result in JSON output")
	}
	for _, r := range results {
		if r.Name == "" {
			t.Errorf("check result has empty name: %+v", r)
		}
		if r.Severity == "" {
			t.Errorf("check result has empty severity: %+v", r)
		}
	}
}

func TestDoctorService_Run_OKFAILBadgesInOutput(t *testing.T) {
	// Docker daemon failure should produce a [FAIL] badge in human output.
	fc := &fakeCompose{
		infoErr:    errors.New("docker not running"),
		versionStr: "Docker Compose version v2.35.1",
	}
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[FAIL]") {
		t.Errorf("expected [FAIL] badge in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[OK]") {
		t.Errorf("expected [OK] badge in output, got:\n%s", out)
	}
}

func TestDoctorService_Run_ContainersHealthy_AllOK(t *testing.T) {
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	opts := optsWithGeneratedFile(t)
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true when containers are healthy\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Container health") {
		t.Errorf("expected 'Container health' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("expected 'running' in container health detail, got:\n%s", out)
	}
}

// --- New tests for Layer 2: Local Runtime checks ---

func TestDoctorService_Run_UpstreamReachable(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true when upstream is reachable\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Upstream reachable") {
		t.Errorf("expected 'Upstream reachable' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "HTTP 200") {
		t.Errorf("expected 'HTTP 200' in upstream detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_UpstreamUnreachable(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := unreachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	// Upstream unreachable is WARN, not FAIL, so allOK should still be true.
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true because upstream unreachable is WARN\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Upstream reachable") {
		t.Errorf("expected 'Upstream reachable' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "unreachable") {
		t.Errorf("expected 'unreachable' in upstream detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_TLSCertValid(t *testing.T) {
	host, port, cleanup := startTLSTestSidecar(t, time.Now().Add(-24*time.Hour), time.Now().Add(90*24*time.Hour))
	defer cleanup()

	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{port: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	cfg.TLS.Provider = "self-signed"
	cfg.Server.Host = host
	cfg.Server.Port = port

	opts := defaultOpts(t)

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true with valid cert\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "TLS certificate") {
		t.Errorf("expected 'TLS certificate' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "valid until") {
		t.Errorf("expected 'valid until' in cert detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_TLSCertExpired(t *testing.T) {
	host, port, cleanup := startTLSTestSidecar(t, time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	defer cleanup()

	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{port: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	cfg.TLS.Provider = "self-signed"
	cfg.Server.Host = host
	cfg.Server.Port = port

	opts := defaultOpts(t)

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when TLS cert is expired")
	}

	out := buf.String()
	if !strings.Contains(out, "expired") {
		t.Errorf("expected 'expired' in cert detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_TLSCertExpiringSoon(t *testing.T) {
	host, port, cleanup := startTLSTestSidecar(t, time.Now().Add(-24*time.Hour), time.Now().Add(3*24*time.Hour))
	defer cleanup()

	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{port: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	cfg.TLS.Provider = "self-signed"
	cfg.Server.Host = host
	cfg.Server.Port = port

	opts := defaultOpts(t)

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WARN does not cause allOK = false.
	if !allOK {
		t.Errorf("expected allOK = true because cert-expiring-soon is WARN\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "expires in") {
		t.Errorf("expected 'expires in' in cert detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_TLSCertSidecarUnreachable(t *testing.T) {
	// Bind and release a port so we know it is closed for the duration of the test.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{addr.Port: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := defaultConfig()
	cfg.TLS.Provider = "self-signed"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = addr.Port

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unreachable sidecar is WARN, not FAIL.
	if !allOK {
		t.Errorf("expected allOK = true because sidecar-unreachable is WARN\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "TLS certificate") {
		t.Errorf("expected 'TLS certificate' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "start 'vibew dev'") {
		t.Errorf("expected sidecar-unreachable hint in detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_TLSCertNonSelfSigned_Skipped(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	cfg.TLS.Provider = "letsencrypt"
	cfg.TLS.Domain = "example.com"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true when TLS provider is letsencrypt\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "skipping local cert check") {
		t.Errorf("expected 'skipping local cert check' in output, got:\n%s", out)
	}
}

// --- New tests for Layer 3: Production checks ---

func TestDoctorService_Run_SSHConnectivity_Success(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	executor := &fakeRemoteExecutor{
		runResponses: map[string]runResult{
			"echo ok": {output: "ok", err: nil},
			"docker compose ps --format json": {
				output: "", err: nil,
			},
		},
	}
	svc := ops.NewDoctorService(fc, pc, hc).WithRemoteExecutor(executor)
	cfg := doctorConfig()

	opts := defaultOpts(t)
	opts.Target = "ssh://user@192.0.2.1"

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "SSH connectivity") {
		t.Errorf("expected 'SSH connectivity' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "connected successfully") {
		t.Errorf("expected 'connected successfully' in SSH detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_SSHConnectivity_Failure(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	executor := &fakeRemoteExecutor{
		runErr: errors.New("connection refused"),
	}
	svc := ops.NewDoctorService(fc, pc, hc).WithRemoteExecutor(executor)
	cfg := doctorConfig()

	opts := defaultOpts(t)
	opts.Target = "ssh://user@192.0.2.1"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when SSH connectivity fails")
	}

	out := buf.String()
	if !strings.Contains(out, "SSH connectivity") {
		t.Errorf("expected 'SSH connectivity' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "could not connect") {
		t.Errorf("expected 'could not connect' in SSH detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_NoTarget_NoRemoteChecks(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()

	// No --target flag, no remote executor.
	opts := defaultOpts(t)

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Production checks should not appear.
	for _, absent := range []string{"SSH connectivity", "Remote containers", "Domain DNS", "Remote TLS cert"} {
		if strings.Contains(out, absent) {
			t.Errorf("expected %q to be absent when no target is set, but found in output:\n%s", absent, out)
		}
	}
}

// TestDoctorService_Run_RemoteContainerHealth_ErrorFormattedNoRawShell ensures
// that when the remote docker compose command fails the user-facing detail
// never leaks shell fragments (`2>/dev/null`, `||`, raw ssh command, or the
// adapter's "ssh exit:" wrapper). This is the end-to-end guarantee described
// in ADR-084.
func TestDoctorService_Run_RemoteContainerHealth_ErrorFormattedNoRawShell(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()

	// Simulate the historical leak: stderr containing every fragment.
	leakErr := errors.New("ssh exit: docker compose ps --format json 2>/dev/null || docker-compose ps 2>/dev/null: exit status 127")
	executor := &fakeRemoteExecutor{
		runResponses: map[string]runResult{
			"echo ok":                         {output: "ok", err: nil},
			"docker compose ps --format json": {output: "", err: leakErr},
			"uname -m":                        {output: "x86_64", err: nil},
		},
	}
	svc := ops.NewDoctorService(fc, pc, hc).WithRemoteExecutor(executor)
	cfg := doctorConfig()

	opts := defaultOpts(t)
	opts.Target = "ssh://user@192.0.2.1"

	var buf bytes.Buffer
	if _, err := svc.Run(context.Background(), cfg, opts, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Find the Remote containers line.
	var remoteLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Remote containers") {
			remoteLine = line
			break
		}
	}
	if remoteLine == "" {
		t.Fatalf("Remote containers line missing from output:\n%s", out)
	}
	for _, leak := range []string{"2>/dev/null", "||", "ssh exit"} {
		if strings.Contains(remoteLine, leak) {
			t.Errorf("remote-containers detail leaks %q:\n%s", leak, remoteLine)
		}
	}
}

func TestDoctorService_Run_RemoteContainerHealth_Unhealthy(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()

	// Remote containers with one unhealthy.
	unhealthyJSON := `{"Service":"proxy","State":"running","Health":"unhealthy"}
{"Service":"app","State":"running","Health":"healthy"}`

	executor := &fakeRemoteExecutor{
		runResponses: map[string]runResult{
			"echo ok": {output: "ok", err: nil},
			"docker compose ps --format json": {
				output: unhealthyJSON, err: nil,
			},
		},
	}
	svc := ops.NewDoctorService(fc, pc, hc).WithRemoteExecutor(executor)
	cfg := doctorConfig()

	opts := defaultOpts(t)
	opts.Target = "ssh://user@192.0.2.1"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when remote container is unhealthy")
	}

	out := buf.String()
	if !strings.Contains(out, "Remote containers") {
		t.Errorf("expected 'Remote containers' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "unhealthy") {
		t.Errorf("expected 'unhealthy' in remote containers detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_RemoteContainerHealth_AllHealthy(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()

	healthyJSON := `{"Service":"proxy","State":"running","Health":"healthy"}
{"Service":"app","State":"running","Health":"healthy"}`

	executor := &fakeRemoteExecutor{
		runResponses: map[string]runResult{
			"echo ok": {output: "ok", err: nil},
			"docker compose ps --format json": {
				output: healthyJSON, err: nil,
			},
		},
	}
	svc := ops.NewDoctorService(fc, pc, hc).WithRemoteExecutor(executor)
	cfg := doctorConfig()

	opts := defaultOpts(t)
	opts.Target = "ssh://user@192.0.2.1"

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "2 container(s) running") {
		t.Errorf("expected '2 container(s) running' in remote containers detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_SectionHeaders(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Config & Docker") {
		t.Errorf("expected 'Config & Docker' section header, got:\n%s", out)
	}
	if !strings.Contains(out, "Local Runtime") {
		t.Errorf("expected 'Local Runtime' section header, got:\n%s", out)
	}
}

func TestDoctorService_Run_ProductionSectionHeaders(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	executor := &fakeRemoteExecutor{
		runResponses: map[string]runResult{
			"echo ok": {output: "ok", err: nil},
			"docker compose ps --format json": {
				output: "", err: nil,
			},
		},
	}
	svc := ops.NewDoctorService(fc, pc, hc).WithRemoteExecutor(executor)
	cfg := doctorConfig()

	opts := defaultOpts(t)
	opts.Target = "ssh://user@192.0.2.1"

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Production") {
		t.Errorf("expected 'Production' section header, got:\n%s", out)
	}
}

func TestDoctorService_Run_JSONOutput_IncludesSection(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	var buf bytes.Buffer

	opts := defaultOpts(t)
	opts.JSON = true
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []ops.CheckResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("output is not valid JSON: %v\ngot:\n%s", err, buf.String())
	}

	// All results should have a section.
	for _, r := range results {
		if r.Section == "" {
			t.Errorf("check %q has empty section in JSON output", r.Name)
		}
	}
}

// --- Tests for ACME email check ---

func TestDoctorService_Run_ACMEEmail_ZeroSSLWithoutEmail(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	cfg.TLS.Provider = "letsencrypt"
	cfg.TLS.ACMECA = "https://acme.zerossl.com/v2/DV90"
	cfg.TLS.Email = ""

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when ZeroSSL is configured without email")
	}

	out := buf.String()
	if !strings.Contains(out, "ACME email") {
		t.Errorf("expected 'ACME email' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "ZeroSSL requires") {
		t.Errorf("expected 'ZeroSSL requires' in detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_ACMEEmail_ZeroSSLWithEmail(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	cfg.TLS.Provider = "letsencrypt"
	cfg.TLS.ACMECA = "https://acme.zerossl.com/v2/DV90"
	cfg.TLS.Email = "admin@example.com"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true when ZeroSSL has email set\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "ACME email") {
		t.Errorf("expected 'ACME email' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "admin@example.com") {
		t.Errorf("expected email in detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_ACMEEmail_NonZeroSSL(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	svc := ops.NewDoctorService(fc, pc, hc)
	cfg := doctorConfig()
	cfg.TLS.Provider = "letsencrypt"
	cfg.TLS.ACMECA = "" // default Let's Encrypt
	cfg.TLS.Email = ""

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true when not using ZeroSSL\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "ACME email") {
		t.Errorf("expected 'ACME email' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "not using ZeroSSL") {
		t.Errorf("expected 'not using ZeroSSL' in detail, got:\n%s", out)
	}
}

// --- Tests for image tag consistency check ---

func TestDoctorService_Run_ImageTag_Exists(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	ic := &fakeImageChecker{exists: true}
	svc := ops.NewDoctorService(fc, pc, hc).WithImageChecker(ic)
	cfg := doctorConfig()
	cfg.App.Image = "myapp:latest"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true when image exists\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Image tag") {
		t.Errorf("expected 'Image tag' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "exists locally") {
		t.Errorf("expected 'exists locally' in detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_ImageTag_Missing(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	ic := &fakeImageChecker{exists: false}
	svc := ops.NewDoctorService(fc, pc, hc).WithImageChecker(ic)
	cfg := doctorConfig()
	cfg.App.Image = "myapp:latest"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when image does not exist locally")
	}

	out := buf.String()
	if !strings.Contains(out, "Image tag") {
		t.Errorf("expected 'Image tag' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "not found locally") {
		t.Errorf("expected 'not found locally' in detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_ImageTag_CheckerError(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	ic := &fakeImageChecker{err: errors.New("docker daemon unavailable")}
	svc := ops.NewDoctorService(fc, pc, hc).WithImageChecker(ic)
	cfg := doctorConfig()
	cfg.App.Image = "myapp:latest"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WARN does not cause allOK = false.
	if !allOK {
		t.Errorf("expected allOK = true because image checker error is WARN\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Image tag") {
		t.Errorf("expected 'Image tag' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "could not check image") {
		t.Errorf("expected 'could not check image' in detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_ImageTag_NoImage_Skipped(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	ic := &fakeImageChecker{exists: false} // Should not be called.
	svc := ops.NewDoctorService(fc, pc, hc).WithImageChecker(ic)
	cfg := doctorConfig()
	cfg.App.Image = "" // No image configured.

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "Image tag") {
		t.Errorf("expected 'Image tag' check to be skipped when no image is configured, got:\n%s", out)
	}
}

// --- Tests for arch compatibility check ---

func TestDoctorService_Run_ArchCompatibility_Match(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()

	// Simulate remote returning the same arch as local (mapped via normalizeArch).
	var remoteUname string
	switch runtime.GOARCH {
	case "amd64":
		remoteUname = "x86_64"
	case "arm64":
		remoteUname = "aarch64"
	default:
		remoteUname = runtime.GOARCH
	}

	executor := &fakeRemoteExecutor{
		runResponses: map[string]runResult{
			"echo ok":  {output: "ok", err: nil},
			"uname -m": {output: remoteUname, err: nil},
			"docker compose ps --format json": {
				output: "", err: nil,
			},
		},
	}
	svc := ops.NewDoctorService(fc, pc, hc).WithRemoteExecutor(executor)
	cfg := doctorConfig()

	opts := defaultOpts(t)
	opts.Target = "ssh://user@192.0.2.1"

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Arch compatibility") {
		t.Errorf("expected 'Arch compatibility' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[OK]") || !strings.Contains(out, "local=") {
		t.Errorf("expected arch match to show OK with local= detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_ArchCompatibility_Mismatch(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()

	// Simulate remote arch different from local. Pick an arch that won't
	// match the CI runner.
	remoteUname := "aarch64"
	if runtime.GOARCH == "arm64" {
		remoteUname = "x86_64"
	}

	executor := &fakeRemoteExecutor{
		runResponses: map[string]runResult{
			"echo ok":  {output: "ok", err: nil},
			"uname -m": {output: remoteUname, err: nil},
			"docker compose ps --format json": {
				output: "", err: nil,
			},
		},
	}
	svc := ops.NewDoctorService(fc, pc, hc).WithRemoteExecutor(executor)
	cfg := doctorConfig()

	opts := defaultOpts(t)
	opts.Target = "ssh://user@192.0.2.1"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Arch mismatch is WARN, so allOK should still be true.
	if !allOK {
		t.Errorf("expected allOK = true because arch mismatch is WARN\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Arch compatibility") {
		t.Errorf("expected 'Arch compatibility' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "--platform") {
		t.Errorf("expected '--platform' hint in arch mismatch detail, got:\n%s", out)
	}
}

func TestDoctorService_Run_ArchCompatibility_RemoteError(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	executor := &fakeRemoteExecutor{
		runResponses: map[string]runResult{
			"echo ok":  {output: "ok", err: nil},
			"uname -m": {output: "", err: errors.New("command not found")},
			"docker compose ps --format json": {
				output: "", err: nil,
			},
		},
	}
	svc := ops.NewDoctorService(fc, pc, hc).WithRemoteExecutor(executor)
	cfg := doctorConfig()

	opts := defaultOpts(t)
	opts.Target = "ssh://user@192.0.2.1"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WARN does not cause allOK = false.
	if !allOK {
		t.Errorf("expected allOK = true because arch check error is WARN\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Arch compatibility") {
		t.Errorf("expected 'Arch compatibility' check in output, got:\n%s", out)
	}
	if !strings.Contains(out, "could not determine") {
		t.Errorf("expected 'could not determine' in arch detail, got:\n%s", out)
	}
}
