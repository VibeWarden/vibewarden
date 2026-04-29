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
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	apptlspreflight "github.com/vibewarden/vibewarden/internal/app/tlspreflight"
	"github.com/vibewarden/vibewarden/internal/config"
	domaintlspreflight "github.com/vibewarden/vibewarden/internal/domain/tlspreflight"
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

	svc := ops.NewDoctorService(fc, pc)
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
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
	// Container health was deleted in #1222 — regression guard.
	if strings.Contains(out, "Container health") {
		t.Errorf("output must NOT contain 'Container health' (deleted check)\ngot:\n%s", out)
	}
}

func TestDoctorService_Run_DockerNotRunning(t *testing.T) {
	fc := &fakeCompose{
		infoErr:    errors.New("docker daemon not running"),
		versionStr: "Docker Compose version v2.35.1",
	}
	pc := &fakePortChecker{}
	svc := ops.NewDoctorService(fc, pc)
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
	svc := ops.NewDoctorService(fc, pc)
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
	svc := ops.NewDoctorService(fc, pc)
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
			svc := ops.NewDoctorService(fc, pc)
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
	svc := ops.NewDoctorService(fc, pc)
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
	// All static checks fail — report should still contain all static check names.
	// PS fails (psErr) so stack is down → Generated files and TLS are silently skipped.
	fc := &fakeCompose{
		infoErr:    errors.New("docker not running"),
		versionErr: errors.New("compose not found"),
		psErr:      errors.New("ps failed"),
	}
	pc := &fakePortChecker{available: map[int]bool{8443: false}}
	svc := ops.NewDoctorService(fc, pc)
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
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check %q missing from output\ngot:\n%s", want, out)
		}
	}
	// Container health is deleted (#1222) — regression guard.
	if strings.Contains(out, "Container health") {
		t.Errorf("output must NOT contain 'Container health' (deleted check)\ngot:\n%s", out)
	}
	// Generated files is gated on stack-up; with psErr stack is down → absent.
	if strings.Contains(out, "Generated files") {
		t.Errorf("output must NOT contain 'Generated files' when stack is down\ngot:\n%s", out)
	}
}

func TestDoctorService_Run_GeneratedFileMissing_IsWarn(t *testing.T) {
	// Stack is up (healthyContainersCompose) but generated file is absent →
	// the gated check runs and emits [WARN].
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	svc := ops.NewDoctorService(fc, pc)
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

// TestDoctorService_Run_UnhealthyContainer_IsFail is intentionally removed.
// The Container health check was deleted in #1222 — runtime container liveness
// is covered by /_vibewarden/health (since #1197).

func TestDoctorService_Run_JSONOutput(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	svc := ops.NewDoctorService(fc, pc)
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
	svc := ops.NewDoctorService(fc, pc)
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

// TestDoctorService_Run_ContainersHealthy_AllOK is intentionally removed.
// The Container health check was deleted in #1222. See TestDoctor_StackUp_AllOK
// for the stack-up regression guard.

// --- Tests for Layer 2: Local Runtime checks ---
// All TLS tests use healthyContainersCompose() to simulate a running stack,
// because the TLS check is gated on isStackRunning (#1222).

func TestDoctorService_Run_TLSCertValid(t *testing.T) {
	host, port, cleanup := startTLSTestSidecar(t, time.Now().Add(-24*time.Hour), time.Now().Add(90*24*time.Hour))
	defer cleanup()

	// Stack is up so TLS check runs.
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{port: true}}
	svc := ops.NewDoctorService(fc, pc)
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

	// Stack is up so TLS check runs.
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{port: true}}
	svc := ops.NewDoctorService(fc, pc)
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

	// Stack is up so TLS check runs.
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{port: true}}
	svc := ops.NewDoctorService(fc, pc)
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

	// Stack is up so TLS check runs even though the sidecar port is closed.
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{addr.Port: true}}
	svc := ops.NewDoctorService(fc, pc)
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
	// Stack is up so TLS check runs (and hits the non-self-signed skip branch).
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	svc := ops.NewDoctorService(fc, pc)
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

func TestDoctorService_Run_SectionHeaders(t *testing.T) {
	// Stack is up so both sections are populated.
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	svc := ops.NewDoctorService(fc, pc)
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
	// "Local Runtime" renders when the stack is up and checkTLSCertValid emits a
	// row. With Provider="external" and no TLSStateResolver wired, checkTLSCertValid
	// hits the legacy short-circuit and returns an OK result tagged sectionLocalRuntime
	// — so the header IS present even with an external provider.
	if !strings.Contains(out, "Local Runtime") {
		t.Errorf("expected 'Local Runtime' section header when stack is up, got:\n%s", out)
	}
}

func TestDoctorService_Run_JSONOutput_IncludesSection(t *testing.T) {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	svc := ops.NewDoctorService(fc, pc)
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
	svc := ops.NewDoctorService(fc, pc)
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
	svc := ops.NewDoctorService(fc, pc)
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
	svc := ops.NewDoctorService(fc, pc)
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
	ic := &fakeImageChecker{exists: true}
	svc := ops.NewDoctorService(fc, pc).WithImageChecker(ic)
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
	ic := &fakeImageChecker{exists: false}
	svc := ops.NewDoctorService(fc, pc).WithImageChecker(ic)
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
	ic := &fakeImageChecker{err: errors.New("docker daemon unavailable")}
	svc := ops.NewDoctorService(fc, pc).WithImageChecker(ic)
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
	ic := &fakeImageChecker{exists: false} // Should not be called.
	svc := ops.NewDoctorService(fc, pc).WithImageChecker(ic)
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

// fakeCTQuerier is a test double for ports.CertTransparencyQuerier.
type fakeCTQuerier struct {
	records   []ports.CrtShRecord
	err       error
	callCount int
}

func (f *fakeCTQuerier) Query(_ context.Context, _ string) ([]ports.CrtShRecord, error) {
	f.callCount++
	return f.records, f.err
}

// leCfg returns a doctorConfig() with TLS configured for letsencrypt.
func leCfg() *config.Config {
	cfg := doctorConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "letsencrypt"
	cfg.TLS.Domain = "app.example.com"
	cfg.TLS.ACMECA = ""
	return cfg
}

// leRecords builds n ports.CrtShRecord values, all LE-issued within the 168h window.
func leRecords(n int) []ports.CrtShRecord {
	now := time.Now()
	recs := make([]ports.CrtShRecord, n)
	for i := range recs {
		recs[i] = ports.CrtShRecord{
			NotBefore:  now.Add(-time.Duration(i+1) * time.Hour),
			IssuerName: "C=US, O=Let's Encrypt, CN=R3",
		}
	}
	return recs
}

func TestDoctorService_Run_LERateLimit_WARN(t *testing.T) {
	q := &fakeCTQuerier{records: leRecords(4)} // 4/5 → WARN, 1 remaining
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := leCfg()
	opts := defaultOpts(t)
	opts.LERegisteredDomains = []string{"example.com"}

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WARN does not cause allOK = false.
	if !allOK {
		t.Error("expected allOK = true when only WARN checks exist")
	}
	out := buf.String()
	if !strings.Contains(out, "LE rate-limit: example.com") {
		t.Errorf("expected LE rate-limit check name in output\ngot:\n%s", out)
	}
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected [WARN] badge in output\ngot:\n%s", out)
	}
}

func TestDoctorService_Run_LERateLimit_FAIL(t *testing.T) {
	q := &fakeCTQuerier{records: leRecords(5)} // 5/5 → FAIL
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := leCfg()
	opts := defaultOpts(t)
	opts.LERegisteredDomains = []string{"example.com"}

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allOK {
		t.Error("expected allOK = false when LE rate limit is exhausted")
	}
	out := buf.String()
	if !strings.Contains(out, "--skip-le-preflight") {
		t.Errorf("expected --skip-le-preflight verbatim in FAIL detail\ngot:\n%s", out)
	}
}

func TestDoctorService_Run_LERateLimit_SkipRateLimitCheck_Config(t *testing.T) {
	q := &fakeCTQuerier{records: leRecords(5)} // Would be FAIL if checked.
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := leCfg()
	cfg.TLS.SkipRateLimitCheck = true // config opt-out
	opts := defaultOpts(t)
	opts.LERegisteredDomains = []string{"example.com"}

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Error("expected allOK = true when SkipRateLimitCheck is true")
	}
	out := buf.String()
	if strings.Contains(out, "LE rate-limit") {
		t.Errorf("expected LE rate-limit check to be skipped\ngot:\n%s", out)
	}
}

func TestDoctorService_Run_LERateLimit_SkipFlag(t *testing.T) {
	q := &fakeCTQuerier{records: leRecords(5)} // Would be FAIL if checked.
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := leCfg()
	opts := defaultOpts(t)
	opts.SkipLEPreflight = true // flag opt-out
	opts.LERegisteredDomains = []string{"example.com"}

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Error("expected allOK = true when --skip-le-preflight flag is set")
	}
	out := buf.String()
	if strings.Contains(out, "LE rate-limit") {
		t.Errorf("expected LE rate-limit check to be skipped\ngot:\n%s", out)
	}
}

func TestDoctorService_Run_LERateLimit_NonLEProvider_Skipped(t *testing.T) {
	q := &fakeCTQuerier{records: leRecords(5)} // Would be FAIL if checked.
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := doctorConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "self-signed" // not letsencrypt
	cfg.TLS.Domain = "app.example.com"
	opts := defaultOpts(t)
	opts.LERegisteredDomains = []string{"example.com"}

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Error("expected allOK = true for non-LE provider")
	}
	out := buf.String()
	if strings.Contains(out, "LE rate-limit") {
		t.Errorf("expected LE rate-limit check to be skipped for non-LE provider\ngot:\n%s", out)
	}
}

func TestDoctorService_Run_LERateLimit_ACMECASet_Skipped(t *testing.T) {
	q := &fakeCTQuerier{records: leRecords(5)} // Would be FAIL if checked.
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := leCfg()
	cfg.TLS.ACMECA = "https://acme-staging-v02.api.letsencrypt.org/directory"
	opts := defaultOpts(t)
	opts.LERegisteredDomains = []string{"example.com"}

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Error("expected allOK = true when ACME CA override is set")
	}
	out := buf.String()
	if strings.Contains(out, "LE rate-limit") {
		t.Errorf("expected LE rate-limit check to be skipped when ACMECA is set\ngot:\n%s", out)
	}
}

func TestDoctorService_Run_LERateLimit_NoDomainsInOpts_Skipped(t *testing.T) {
	q := &fakeCTQuerier{records: leRecords(5)} // Would be FAIL if checked.
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := leCfg()
	opts := defaultOpts(t)
	// LERegisteredDomains and LESkippedDomains deliberately not set.

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No FAIL because no domains were enumerated.
	if !allOK {
		t.Error("expected allOK = true when no registered domains are provided")
	}
	out := buf.String()
	if strings.Contains(out, "LE rate-limit") {
		t.Errorf("expected LE rate-limit check to be skipped when no domains set\ngot:\n%s", out)
	}
}

// TestDoctorService_Run_LERateLimit_SingleLabelDomain_WARN verifies that a
// single-label hostname (e.g. "localhost") that cannot be normalised to eTLD+1
// produces a SeverityWarn CheckResult instead of being silently omitted. This
// satisfies the ADR-090 error-cases contract for un-normalisable domains.
func TestDoctorService_Run_LERateLimit_SingleLabelDomain_WARN(t *testing.T) {
	q := &fakeCTQuerier{records: leRecords(0)} // should never be called
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := leCfg()
	opts := defaultOpts(t)
	// Simulate deriveRegisteredDomains returning "localhost" as a skipped domain.
	opts.LESkippedDomains = []string{"localhost"}

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WARN does not flip allOK to false.
	if !allOK {
		t.Error("expected allOK = true for WARN result (single-label domain)")
	}
	out := buf.String()
	if !strings.Contains(out, "LE rate-limit") {
		t.Errorf("expected LE rate-limit check in output for skipped domain\ngot:\n%s", out)
	}
	if !strings.Contains(out, "cannot derive registered domain") {
		t.Errorf("expected 'cannot derive registered domain' in WARN detail\ngot:\n%s", out)
	}
	// Must not have called the CT querier (q.records would be consumed if queried).
	if q.callCount > 0 {
		t.Errorf("expected CT querier not to be called for a skipped domain, got %d calls", q.callCount)
	}
}

func TestDoctorService_Run_LERateLimit_NetworkError_WARN(t *testing.T) {
	q := &fakeCTQuerier{err: domaintlspreflight.ErrCTUnavailable}
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := leCfg()
	opts := defaultOpts(t)
	opts.LERegisteredDomains = []string{"example.com"}

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Network error → WARN → allOK stays true.
	if !allOK {
		t.Error("expected allOK = true when crt.sh is unreachable (WARN)")
	}
	out := buf.String()
	if !strings.Contains(out, "crt.sh unreachable") {
		t.Errorf("expected 'crt.sh unreachable' in WARN detail\ngot:\n%s", out)
	}
}

func TestDoctorService_Run_LERateLimit_JSONOutput(t *testing.T) {
	q := &fakeCTQuerier{records: leRecords(4)} // 4/5 → WARN
	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithLERateLimitService(apptlspreflight.NewService(q))

	cfg := leCfg()
	opts := defaultOpts(t)
	opts.JSON = true
	opts.LERegisteredDomains = []string{"example.com"}

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []ops.CheckResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("JSON unmarshal failed: %v\nbody: %s", err, buf.String())
	}
	var found bool
	for _, r := range results {
		if strings.Contains(r.Name, "LE rate-limit") {
			found = true
			if r.Severity != ops.SeverityWarn {
				t.Errorf("LE rate-limit check severity = %v, want WARN", r.Severity)
			}
			if r.Section == "" {
				t.Error("LE rate-limit check has empty Section")
			}
		}
	}
	if !found {
		t.Errorf("LE rate-limit check not found in JSON results: %s", buf.String())
	}
}

// --- Stack-state awareness tests (#1222) ---

// TestDoctor_StackDown_SkipsGenAndTLS asserts the silent-skip contract:
// when no compose containers are detected, Generated files and TLS certificate
// rows must be absent from the output (no misleading pre-stack WARNs).
// Container health must also be absent (it was deleted entirely in #1222).
func TestDoctor_StackDown_SkipsGenAndTLS(t *testing.T) {
	fc := noContainersCompose() // PS returns nil → stack is down
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	svc := ops.NewDoctorService(fc, pc)
	cfg := defaultConfig()
	cfg.TLS.Provider = "self-signed"

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No FAIL rows exist → allOK must be true.
	if !allOK {
		t.Errorf("expected allOK = true with stack down (no FAIL-level static checks)\noutput:\n%s", buf.String())
	}

	out := buf.String()
	for _, absent := range []string{"Generated files", "Container health", "TLS certificate"} {
		if strings.Contains(out, absent) {
			t.Errorf("output must NOT contain %q when stack is down (silent skip)\ngot:\n%s", absent, out)
		}
	}
	// Static checks must still be present.
	for _, present := range []string{"Config file", "Docker daemon", "Docker Compose", "Proxy port"} {
		if !strings.Contains(out, present) {
			t.Errorf("expected static check %q in output when stack is down\ngot:\n%s", present, out)
		}
	}
}

// TestDoctor_StackUp_AllOK asserts that with a healthy stack the static and
// runtime checks all pass, and that Container health is absent (deleted in #1222).
func TestDoctor_StackUp_AllOK(t *testing.T) {
	fc := healthyContainersCompose() // PS returns one healthy container → stack is up
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	svc := ops.NewDoctorService(fc, pc)
	cfg := doctorConfig() // TLS.Provider = "external" → TLS check skips local handshake

	opts := optsWithGeneratedFile(t)
	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allOK {
		t.Errorf("expected allOK = true when stack is up and all checks pass\noutput:\n%s", buf.String())
	}

	out := buf.String()
	// Generated files must appear when stack is up.
	if !strings.Contains(out, "Generated files") {
		t.Errorf("expected 'Generated files' check in output when stack is up\ngot:\n%s", out)
	}
	// Container health must be absent — regression guard against re-introducing the deleted check.
	if strings.Contains(out, "Container health") {
		t.Errorf("output must NOT contain 'Container health' (deleted in #1222)\ngot:\n%s", out)
	}
}

// TestDoctor_StackUp_GeneratedFileMissing_StillWarns confirms that the
// stack-state gate does not suppress the real signal: when the stack is up
// but the generated file is missing, a [WARN] row must still appear.
func TestDoctor_StackUp_GeneratedFileMissing_StillWarns(t *testing.T) {
	fc := healthyContainersCompose() // stack is up
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	svc := ops.NewDoctorService(fc, pc)
	cfg := doctorConfig()

	// defaultOpts has an empty tempdir — no generated file present.
	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WARN does not cause allOK = false.
	if !allOK {
		t.Errorf("expected allOK = true because missing generated file is WARN\noutput:\n%s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Generated files") {
		t.Errorf("expected 'Generated files' WARN row when stack is up but file missing\ngot:\n%s", out)
	}
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected [WARN] badge for missing generated file\ngot:\n%s", out)
	}
}

// --- Tests for macOS LibreSSL advisory (#1224) ---

// TestDoctorService_Run_DarwinAdvisory is a table-driven test that verifies
// the darwin-only LibreSSL curl advisory is emitted precisely when expected:
// darwin + human mode → present; non-darwin → absent; JSON mode → absent.
func TestDoctorService_Run_DarwinAdvisory(t *testing.T) {
	const advisorySubstring = "Note (macOS): system curl uses LibreSSL"
	const troubleshootingRef = "docs/troubleshooting.md"

	tests := []struct {
		name        string
		goos        string
		jsonMode    bool
		wantPresent bool
	}{
		{
			name:        "darwin human mode emits advisory",
			goos:        "darwin",
			jsonMode:    false,
			wantPresent: true,
		},
		{
			name:        "linux omits advisory",
			goos:        "linux",
			jsonMode:    false,
			wantPresent: false,
		},
		{
			name:        "windows omits advisory",
			goos:        "windows",
			jsonMode:    false,
			wantPresent: false,
		},
		{
			name:        "darwin JSON mode omits advisory",
			goos:        "darwin",
			jsonMode:    true,
			wantPresent: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			restore := ops.SetGOOSForTest(tt.goos)
			t.Cleanup(restore)

			fc := noContainersCompose()
			pc := &fakePortChecker{available: map[int]bool{8443: true}}
			svc := ops.NewDoctorService(fc, pc)
			cfg := doctorConfig()

			opts := defaultOpts(t)
			opts.JSON = tt.jsonMode

			var buf bytes.Buffer
			if _, err := svc.Run(context.Background(), cfg, opts, &buf); err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			out := buf.String()
			if tt.wantPresent {
				if !strings.Contains(out, advisorySubstring) {
					t.Errorf("expected advisory substring %q in output\ngot:\n%s", advisorySubstring, out)
				}
				if !strings.Contains(out, troubleshootingRef) {
					t.Errorf("expected troubleshooting ref %q in output\ngot:\n%s", troubleshootingRef, out)
				}
			} else {
				if strings.Contains(out, "Note (macOS):") {
					t.Errorf("expected advisory to be absent in output\ngot:\n%s", out)
				}
			}

			// JSON mode: output must be valid JSON regardless of OS.
			if tt.jsonMode {
				var results []ops.CheckResult
				if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
					t.Errorf("JSON mode output is not valid JSON: %v\nbody:\n%s", err, out)
				}
			}
		})
	}
}

// TestDoctor_ContainerHealth_AbsentInJSON is a regression guard for JSON mode:
// no result in the JSON array may have Name == "Container health", regardless
// of stack state. The check was deleted in #1222.
func TestDoctor_ContainerHealth_AbsentInJSON(t *testing.T) {
	fc := healthyContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	svc := ops.NewDoctorService(fc, pc)
	cfg := doctorConfig()

	opts := optsWithGeneratedFile(t)
	opts.JSON = true
	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []ops.CheckResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("JSON unmarshal failed: %v\nbody: %s", err, buf.String())
	}
	for _, r := range results {
		if strings.EqualFold(r.Name, "Container health") {
			t.Errorf("JSON output must not contain 'Container health' result (deleted in #1222): %+v", r)
		}
	}
}
