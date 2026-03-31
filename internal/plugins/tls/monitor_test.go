package tls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// noopWriter discards all log output.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

// discardLogger returns an slog.Logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fakeCertReader is a certReader implementation that returns a fixed certificate.
type fakeCertReader struct {
	cert *x509.Certificate
	err  error
}

func (f *fakeCertReader) ReadCert(_, _ string) (*x509.Certificate, error) {
	return f.cert, f.err
}

// fakeEventLogger records emitted events.
type fakeEventLogger struct {
	logged []events.Event
}

func (f *fakeEventLogger) Log(_ context.Context, ev events.Event) error {
	f.logged = append(f.logged, ev)
	return nil
}

// fakeMetricsCollector records SetTLSCertExpirySeconds calls.
type fakeMonitorMetrics struct {
	domain  string
	seconds float64
	called  int
}

func (f *fakeMonitorMetrics) SetTLSCertExpirySeconds(domain string, seconds float64) {
	f.domain = domain
	f.seconds = seconds
	f.called++
}

// All other MetricsCollector methods are no-ops.
func (f *fakeMonitorMetrics) IncRequestTotal(_, _, _ string)                               {}
func (f *fakeMonitorMetrics) ObserveRequestDuration(_, _ string, _ time.Duration)          {}
func (f *fakeMonitorMetrics) IncRateLimitHit(_ string)                                     {}
func (f *fakeMonitorMetrics) IncAuthDecision(_ string)                                     {}
func (f *fakeMonitorMetrics) IncUpstreamError()                                            {}
func (f *fakeMonitorMetrics) IncUpstreamTimeout()                                          {}
func (f *fakeMonitorMetrics) IncUpstreamRetry(_ string)                                    {}
func (f *fakeMonitorMetrics) SetActiveConnections(_ int)                                   {}
func (f *fakeMonitorMetrics) SetCircuitBreakerState(_ context.Context, _ resilience.State) {}
func (f *fakeMonitorMetrics) IncWAFDetection(_, _ string)                                  {}
func (f *fakeMonitorMetrics) IncEgressRequestTotal(_, _, _ string)                         {}
func (f *fakeMonitorMetrics) ObserveEgressDuration(_, _ string, _ time.Duration)           {}
func (f *fakeMonitorMetrics) IncEgressErrorTotal(_ string)                                 {}

// Compile-time assertion: fakeMonitorMetrics implements ports.MetricsCollector.
var _ ports.MetricsCollector = (*fakeMonitorMetrics)(nil)

// makeCert creates a self-signed certificate with the given validity window.
func makeCert(t *testing.T, notBefore, notAfter time.Time, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatalf("parsing certificate: %v", err)
	}
	return cert
}

// writeCertAndKey writes a PEM cert+key pair to a temp directory and returns
// the cert path and key path.
func writeCertAndKey(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(60 * 24 * time.Hour) // 60 days
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}

	certPath = filepath.Join(dir, "cert.pem")
	cf, err := os.Create(certPath)
	if err != nil {
		t.Fatalf("creating cert file: %v", err)
	}
	if err := pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		_ = cf.Close()
		t.Fatalf("encoding cert pem: %v", err)
	}
	if err := cf.Close(); err != nil {
		t.Fatalf("closing cert file: %v", err)
	}

	keyPath = filepath.Join(dir, "key.pem")
	kf, err := os.Create(keyPath)
	if err != nil {
		t.Fatalf("creating key file: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		_ = kf.Close()
		t.Fatalf("marshalling private key: %v", err)
	}
	if err := pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		_ = kf.Close()
		t.Fatalf("encoding key pem: %v", err)
	}
	if err := kf.Close(); err != nil {
		t.Fatalf("closing key file: %v", err)
	}

	return certPath, keyPath
}

// newMonitorWithFakeReader returns a CertMonitor that uses a fake cert reader.
// eventLog may be nil; when nil the monitor's EventLogger field is left nil.
func newMonitorWithFakeReader(
	cfg ports.TLSConfig,
	reader certReader,
	eventLog *fakeEventLogger,
	mc ports.MetricsCollector,
) *CertMonitor {
	applyMonitorDefaults(&cfg.CertMonitoring)
	m := &CertMonitor{
		cfg: cfg,
		reader: &caddyCertReader{
			cfg:    cfg,
			reader: reader,
		},
		metrics: mc,
		logger:  discardLogger(),
		stopCh:  make(chan struct{}),
	}
	// Assign eventLog only when non-nil to avoid a non-nil interface wrapping a nil pointer.
	if eventLog != nil {
		m.eventLog = eventLog
	}
	return m
}

// ---------------------------------------------------------------------------
// applyMonitorDefaults
// ---------------------------------------------------------------------------

func TestApplyMonitorDefaults_ZeroValues(t *testing.T) {
	m := ports.TLSCertMonitoringConfig{}
	applyMonitorDefaults(&m)

	if m.CheckInterval != defaultCheckInterval {
		t.Errorf("CheckInterval = %v, want %v", m.CheckInterval, defaultCheckInterval)
	}
	if m.WarningThreshold != defaultWarningThreshold {
		t.Errorf("WarningThreshold = %v, want %v", m.WarningThreshold, defaultWarningThreshold)
	}
	if m.CriticalThreshold != defaultCriticalThreshold {
		t.Errorf("CriticalThreshold = %v, want %v", m.CriticalThreshold, defaultCriticalThreshold)
	}
}

func TestApplyMonitorDefaults_PreservesCustomValues(t *testing.T) {
	m := ports.TLSCertMonitoringConfig{
		CheckInterval:     1 * time.Hour,
		WarningThreshold:  10 * 24 * time.Hour,
		CriticalThreshold: 3 * 24 * time.Hour,
	}
	applyMonitorDefaults(&m)

	if m.CheckInterval != 1*time.Hour {
		t.Errorf("CheckInterval = %v, want 1h", m.CheckInterval)
	}
	if m.WarningThreshold != 10*24*time.Hour {
		t.Errorf("WarningThreshold = %v, want 240h", m.WarningThreshold)
	}
	if m.CriticalThreshold != 3*24*time.Hour {
		t.Errorf("CriticalThreshold = %v, want 72h", m.CriticalThreshold)
	}
}

// ---------------------------------------------------------------------------
// check — healthy certificate
// ---------------------------------------------------------------------------

func TestCertMonitor_Check_HealthyCert(t *testing.T) {
	cert := makeCert(t, time.Now().Add(-time.Hour), time.Now().Add(60*24*time.Hour), "example.com")
	el := &fakeEventLogger{}
	mc := &fakeMonitorMetrics{}

	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderExternal,
		Domain:   "example.com",
		CertMonitoring: ports.TLSCertMonitoringConfig{
			Enabled:           true,
			CheckInterval:     6 * time.Hour,
			WarningThreshold:  30 * 24 * time.Hour,
			CriticalThreshold: 7 * 24 * time.Hour,
		},
	}
	m := newMonitorWithFakeReader(cfg, &fakeCertReader{cert: cert}, el, mc)
	m.check(context.Background())

	// Healthy cert: no events emitted.
	if len(el.logged) != 0 {
		t.Errorf("expected no events for healthy cert, got %d: %v", len(el.logged), el.logged)
	}

	// Degraded should be false.
	if degraded, _ := m.Degraded(); degraded {
		t.Error("Degraded() = true, want false for healthy cert")
	}

	// Metric should have been updated.
	if mc.called == 0 {
		t.Error("SetTLSCertExpirySeconds was not called")
	}
	if mc.seconds <= 0 {
		t.Errorf("SetTLSCertExpirySeconds seconds = %f, want > 0", mc.seconds)
	}
}

// ---------------------------------------------------------------------------
// check — warning threshold
// ---------------------------------------------------------------------------

func TestCertMonitor_Check_WarningThreshold(t *testing.T) {
	// Cert expires in 15 days: within warning (30d) but outside critical (7d).
	cert := makeCert(t, time.Now().Add(-time.Hour), time.Now().Add(15*24*time.Hour), "example.com")
	el := &fakeEventLogger{}

	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderExternal,
		Domain:   "example.com",
		CertMonitoring: ports.TLSCertMonitoringConfig{
			Enabled:           true,
			CheckInterval:     6 * time.Hour,
			WarningThreshold:  30 * 24 * time.Hour,
			CriticalThreshold: 7 * 24 * time.Hour,
		},
	}
	m := newMonitorWithFakeReader(cfg, &fakeCertReader{cert: cert}, el, nil)
	m.check(context.Background())

	if len(el.logged) != 1 {
		t.Fatalf("expected 1 event, got %d", len(el.logged))
	}
	if el.logged[0].EventType != events.EventTypeTLSCertExpiryWarning {
		t.Errorf("event type = %q, want %q", el.logged[0].EventType, events.EventTypeTLSCertExpiryWarning)
	}

	// Warning does not set degraded.
	if degraded, _ := m.Degraded(); degraded {
		t.Error("Degraded() = true, want false for warning threshold")
	}
}

// ---------------------------------------------------------------------------
// check — critical threshold
// ---------------------------------------------------------------------------

func TestCertMonitor_Check_CriticalThreshold(t *testing.T) {
	// Cert expires in 3 days: within critical threshold (7d).
	cert := makeCert(t, time.Now().Add(-time.Hour), time.Now().Add(3*24*time.Hour), "example.com")
	el := &fakeEventLogger{}

	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderExternal,
		Domain:   "example.com",
		CertMonitoring: ports.TLSCertMonitoringConfig{
			Enabled:           true,
			CheckInterval:     6 * time.Hour,
			WarningThreshold:  30 * 24 * time.Hour,
			CriticalThreshold: 7 * 24 * time.Hour,
		},
	}
	m := newMonitorWithFakeReader(cfg, &fakeCertReader{cert: cert}, el, nil)
	m.check(context.Background())

	if len(el.logged) != 1 {
		t.Fatalf("expected 1 event, got %d", len(el.logged))
	}
	if el.logged[0].EventType != events.EventTypeTLSCertExpiryCritical {
		t.Errorf("event type = %q, want %q", el.logged[0].EventType, events.EventTypeTLSCertExpiryCritical)
	}

	// Critical sets degraded.
	if degraded, _ := m.Degraded(); !degraded {
		t.Error("Degraded() = false, want true for critical threshold")
	}
}

// ---------------------------------------------------------------------------
// check — already expired
// ---------------------------------------------------------------------------

func TestCertMonitor_Check_AlreadyExpired(t *testing.T) {
	cert := makeCert(t, time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour), "example.com")
	el := &fakeEventLogger{}
	mc := &fakeMonitorMetrics{}

	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderExternal,
		Domain:   "example.com",
		CertMonitoring: ports.TLSCertMonitoringConfig{
			Enabled:           true,
			CheckInterval:     6 * time.Hour,
			WarningThreshold:  30 * 24 * time.Hour,
			CriticalThreshold: 7 * 24 * time.Hour,
		},
	}
	m := newMonitorWithFakeReader(cfg, &fakeCertReader{cert: cert}, el, mc)
	m.check(context.Background())

	// Should emit critical event.
	if len(el.logged) != 1 {
		t.Fatalf("expected 1 event, got %d", len(el.logged))
	}
	if el.logged[0].EventType != events.EventTypeTLSCertExpiryCritical {
		t.Errorf("event type = %q, want %q", el.logged[0].EventType, events.EventTypeTLSCertExpiryCritical)
	}

	// Metric seconds should be negative.
	if mc.seconds >= 0 {
		t.Errorf("SetTLSCertExpirySeconds seconds = %f, want < 0 for expired cert", mc.seconds)
	}
}

// ---------------------------------------------------------------------------
// check — degraded cleared when cert recovers
// ---------------------------------------------------------------------------

func TestCertMonitor_Check_DegradedClearedOnRecovery(t *testing.T) {
	criticalCert := makeCert(t, time.Now().Add(-time.Hour), time.Now().Add(3*24*time.Hour), "example.com")
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderExternal,
		Domain:   "example.com",
		CertMonitoring: ports.TLSCertMonitoringConfig{
			Enabled:           true,
			WarningThreshold:  30 * 24 * time.Hour,
			CriticalThreshold: 7 * 24 * time.Hour,
		},
	}

	reader := &fakeCertReader{cert: criticalCert}
	m := newMonitorWithFakeReader(cfg, reader, nil, nil)
	m.check(context.Background())

	if degraded, _ := m.Degraded(); !degraded {
		t.Fatal("expected degraded after critical cert check")
	}

	// Now replace with a healthy cert.
	reader.cert = makeCert(t, time.Now().Add(-time.Hour), time.Now().Add(60*24*time.Hour), "example.com")
	m.check(context.Background())

	if degraded, _ := m.Degraded(); degraded {
		t.Error("Degraded() = true after recovering to healthy cert, want false")
	}
}

// ---------------------------------------------------------------------------
// check — nil eventLog does not panic
// ---------------------------------------------------------------------------

func TestCertMonitor_Check_NilEventLog(t *testing.T) {
	cert := makeCert(t, time.Now().Add(-time.Hour), time.Now().Add(3*24*time.Hour), "example.com")
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderExternal,
		Domain:   "example.com",
		CertMonitoring: ports.TLSCertMonitoringConfig{
			Enabled:           true,
			WarningThreshold:  30 * 24 * time.Hour,
			CriticalThreshold: 7 * 24 * time.Hour,
		},
	}
	m := newMonitorWithFakeReader(cfg, &fakeCertReader{cert: cert}, nil, nil)
	// Must not panic.
	m.check(context.Background())
}

// ---------------------------------------------------------------------------
// event payload fields
// ---------------------------------------------------------------------------

func TestCertMonitor_Check_EventPayloadFields(t *testing.T) {
	cert := makeCert(t, time.Now().Add(-time.Hour), time.Now().Add(15*24*time.Hour), "example.com")
	el := &fakeEventLogger{}

	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderExternal,
		Domain:   "example.com",
		CertMonitoring: ports.TLSCertMonitoringConfig{
			Enabled:           true,
			WarningThreshold:  30 * 24 * time.Hour,
			CriticalThreshold: 7 * 24 * time.Hour,
		},
	}
	m := newMonitorWithFakeReader(cfg, &fakeCertReader{cert: cert}, el, nil)
	m.check(context.Background())

	if len(el.logged) == 0 {
		t.Fatal("expected at least one event")
	}
	ev := el.logged[0]

	requiredKeys := []string{"domain", "subject", "issuer", "expires_at", "days_remaining", "serial_number"}
	for _, k := range requiredKeys {
		if _, ok := ev.Payload[k]; !ok {
			t.Errorf("event payload missing key %q", k)
		}
	}
	if ev.SchemaVersion != events.SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// fileCertReader — round-trip
// ---------------------------------------------------------------------------

func TestFileCertReader_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeCertAndKey(t, dir)

	reader := fileCertReader{}
	cert, err := reader.ReadCert(certPath, keyPath)
	if err != nil {
		t.Fatalf("ReadCert() error = %v", err)
	}
	if cert == nil {
		t.Fatal("ReadCert() returned nil certificate")
	}
	if cert.Subject.CommonName != "test.example.com" {
		t.Errorf("CommonName = %q, want %q", cert.Subject.CommonName, "test.example.com")
	}
	if cert.NotAfter.IsZero() {
		t.Error("NotAfter is zero")
	}
}

func TestFileCertReader_InvalidPath(t *testing.T) {
	reader := fileCertReader{}
	_, err := reader.ReadCert("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Error("expected error for non-existent path, got nil")
	}
}

// ---------------------------------------------------------------------------
// caddyCertReader — external provider delegates to fileCertReader
// ---------------------------------------------------------------------------

func TestCaddyCertReader_External_DelegatesToFileReader(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeCertAndKey(t, dir)

	cfg := ports.TLSConfig{
		Provider: ports.TLSProviderExternal,
		CertPath: certPath,
		KeyPath:  keyPath,
	}
	r := &caddyCertReader{cfg: cfg, reader: fileCertReader{}}
	cert, err := r.ReadCert()
	if err != nil {
		t.Fatalf("ReadCert() error = %v", err)
	}
	if cert == nil {
		t.Fatal("ReadCert() returned nil")
	}
}

// ---------------------------------------------------------------------------
// Start / Stop
// ---------------------------------------------------------------------------

func TestCertMonitor_StartStop(t *testing.T) {
	cert := makeCert(t, time.Now().Add(-time.Hour), time.Now().Add(60*24*time.Hour), "example.com")
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderExternal,
		Domain:   "example.com",
		CertMonitoring: ports.TLSCertMonitoringConfig{
			Enabled:           true,
			CheckInterval:     100 * time.Millisecond,
			WarningThreshold:  30 * 24 * time.Hour,
			CriticalThreshold: 7 * 24 * time.Hour,
		},
	}
	m := newMonitorWithFakeReader(cfg, &fakeCertReader{cert: cert}, nil, nil)
	m.Start(context.Background())

	// Give the goroutine a tick to run.
	time.Sleep(200 * time.Millisecond)

	// Stop must return without hanging.
	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2 seconds")
	}
}
