package tls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	defaultCheckInterval     = 6 * time.Hour
	defaultWarningThreshold  = 30 * 24 * time.Hour // 30 days
	defaultCriticalThreshold = 7 * 24 * time.Hour  // 7 days
)

// certReader is the interface used to load a TLS certificate from disk.
// It is abstracted for testability.
type certReader interface {
	// ReadCert loads the leaf certificate from the given PEM files.
	ReadCert(certPath, keyPath string) (*x509.Certificate, error)
}

// fileCertReader loads certificates from the filesystem using standard crypto/tls.
type fileCertReader struct{}

// ReadCert loads a TLS certificate pair from PEM files and returns the leaf certificate.
func (fileCertReader) ReadCert(certPath, keyPath string) (*x509.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("loading key pair: %w", err)
	}
	if len(cert.Certificate) == 0 {
		return nil, fmt.Errorf("certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parsing leaf certificate: %w", err)
	}
	return leaf, nil
}

// caddyCertReader resolves the certificate path for letsencrypt/self-signed
// providers by looking up the certificate in Caddy's storage directory.
// For the external provider, it delegates to a fileCertReader.
type caddyCertReader struct {
	cfg    ports.TLSConfig
	reader certReader
}

// ReadCert returns the leaf certificate appropriate for the provider.
// For letsencrypt/self-signed it scans the storage directory for PEM files.
// For external it reads cfg.CertPath and cfg.KeyPath directly.
func (r *caddyCertReader) ReadCert() (*x509.Certificate, error) {
	switch r.cfg.Provider {
	case ports.TLSProviderExternal:
		return r.reader.ReadCert(r.cfg.CertPath, r.cfg.KeyPath)
	case ports.TLSProviderLetsEncrypt, ports.TLSProviderSelfSigned, "":
		return r.readCaddyStoredCert()
	default:
		return nil, fmt.Errorf("unsupported provider %q for certificate monitoring", r.cfg.Provider)
	}
}

// readCaddyStoredCert searches Caddy's data directory for a certificate file
// matching the configured domain. It uses the standard Caddy file layout:
//
//	<storage_path>/certificates/<acme-dir>/<domain>/<domain>.crt
//
// For self-signed certificates the path is:
//
//	<storage_path>/pki/authorities/local/<domain>.crt (or similar)
//
// When StoragePath is empty the Caddy default is used (~/.local/share/caddy).
// Because the exact path varies by Caddy version and OS, the monitor first
// tries the ACME path, then falls back to glob-searching the storage directory.
func (r *caddyCertReader) readCaddyStoredCert() (*x509.Certificate, error) {
	storagePath := r.cfg.StoragePath
	if storagePath == "" {
		return nil, fmt.Errorf("storage_path is required for certificate monitoring with provider %q; set tls.storage_path in vibewarden.yaml", r.cfg.Provider)
	}

	domain := r.cfg.Domain
	if domain == "" {
		// For self-signed without a domain use a glob to find any cert.
		return r.findAnyCert(storagePath)
	}

	// Standard ACME path: <storage>/certificates/<acme-dir>/<domain>/<domain>.crt
	acmePath := filepath.Join(storagePath, "certificates", "*", domain, domain+".crt")
	matches, err := filepath.Glob(acmePath)
	if err == nil && len(matches) > 0 {
		return readCertFile(matches[0])
	}

	// Fallback: search anywhere under storagePath.
	return r.findAnyCert(storagePath)
}

// findAnyCert searches the storagePath tree for any .crt or .pem certificate file.
func (r *caddyCertReader) findAnyCert(storagePath string) (*x509.Certificate, error) {
	patterns := []string{
		filepath.Join(storagePath, "**", "*.crt"),
		filepath.Join(storagePath, "**", "*.pem"),
		filepath.Join(storagePath, "*.crt"),
		filepath.Join(storagePath, "*.pem"),
	}
	for _, pat := range patterns {
		matches, err := filepath.Glob(pat)
		if err != nil || len(matches) == 0 {
			continue
		}
		for _, m := range matches {
			cert, err := readCertFile(m)
			if err == nil {
				return cert, nil
			}
		}
	}
	return nil, fmt.Errorf("no certificate found under storage path %q", storagePath)
}

// readCertFile parses the first PEM-encoded certificate block in a .crt/.pem file.
func readCertFile(path string) (*x509.Certificate, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return nil, fmt.Errorf("reading certificate file %q: %w", path, err)
	}
	certs, err := x509.ParseCertificates(pemDecode(data))
	if err != nil {
		return nil, fmt.Errorf("parsing certificate file %q: %w", path, err)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates in file %q", path)
	}
	return certs[0], nil
}

// CertMonitor runs a background goroutine that periodically checks TLS
// certificate expiry, emits structured events, updates metrics, and sets
// the plugin's health status to degraded when the certificate is within the
// critical threshold.
type CertMonitor struct {
	cfg      ports.TLSConfig
	reader   *caddyCertReader
	eventLog ports.EventLogger
	metrics  ports.MetricsCollector
	logger   *slog.Logger

	// mu protects degraded and degradedMsg.
	mu          sync.RWMutex
	degraded    bool
	degradedMsg string

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewCertMonitor creates a CertMonitor for the given TLS configuration.
// eventLog and metrics may be nil; when nil those outputs are skipped.
func NewCertMonitor(
	cfg ports.TLSConfig,
	eventLog ports.EventLogger,
	metrics ports.MetricsCollector,
	logger *slog.Logger,
) *CertMonitor {
	applyMonitorDefaults(&cfg.CertMonitoring)
	return &CertMonitor{
		cfg: cfg,
		reader: &caddyCertReader{
			cfg:    cfg,
			reader: fileCertReader{},
		},
		eventLog: eventLog,
		metrics:  metrics,
		logger:   logger,
		stopCh:   make(chan struct{}),
	}
}

// applyMonitorDefaults fills zero-value fields with sensible defaults.
func applyMonitorDefaults(m *ports.TLSCertMonitoringConfig) {
	if m.CheckInterval == 0 {
		m.CheckInterval = defaultCheckInterval
	}
	if m.WarningThreshold == 0 {
		m.WarningThreshold = defaultWarningThreshold
	}
	if m.CriticalThreshold == 0 {
		m.CriticalThreshold = defaultCriticalThreshold
	}
}

// Start launches the background check loop. It must be called at most once.
func (m *CertMonitor) Start(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx)
	}()
}

// Stop signals the monitor to stop and waits for the goroutine to finish.
func (m *CertMonitor) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

// Degraded returns true when the certificate is within the critical threshold.
// It is safe for concurrent use.
func (m *CertMonitor) Degraded() (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.degraded, m.degradedMsg
}

// run executes the check loop until ctx is cancelled or Stop is called.
func (m *CertMonitor) run(ctx context.Context) {
	// Run immediately on start.
	m.check(ctx)

	ticker := time.NewTicker(m.cfg.CertMonitoring.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

// check performs a single certificate expiry check.
func (m *CertMonitor) check(ctx context.Context) {
	cert, err := m.reader.ReadCert()
	if err != nil {
		m.logger.WarnContext(ctx, "tls cert monitor: failed to read certificate",
			slog.String("error", err.Error()),
		)
		return
	}

	now := time.Now()
	remaining := cert.NotAfter.Sub(now)
	domain := m.cfg.Domain
	if domain == "" {
		domain = cert.Subject.CommonName
	}

	// Update metric gauge.
	if m.metrics != nil {
		m.metrics.SetTLSCertExpirySeconds(domain, remaining.Seconds())
	}

	daysRemaining := remaining.Hours() / 24

	m.logger.DebugContext(ctx, "tls cert monitor: checked certificate",
		slog.String("domain", domain),
		slog.Time("expires_at", cert.NotAfter),
		slog.Float64("days_remaining", daysRemaining),
	)

	switch {
	case remaining <= m.cfg.CertMonitoring.CriticalThreshold:
		m.setCritical(ctx, cert, domain, remaining, daysRemaining)
	case remaining <= m.cfg.CertMonitoring.WarningThreshold:
		m.setWarning(ctx, cert, domain, remaining, daysRemaining)
	default:
		// Certificate is healthy — clear any previous degraded state.
		m.mu.Lock()
		m.degraded = false
		m.degradedMsg = ""
		m.mu.Unlock()
	}
}

// setCritical emits a tls.cert_expiry_critical event and marks the monitor degraded.
func (m *CertMonitor) setCritical(
	ctx context.Context,
	cert *x509.Certificate,
	domain string,
	remaining time.Duration,
	daysRemaining float64,
) {
	msg := fmt.Sprintf(
		"TLS certificate for %q expires in %.1f days (%s) — CRITICAL",
		domain, daysRemaining, cert.NotAfter.UTC().Format(time.RFC3339),
	)
	m.logger.ErrorContext(ctx, "tls cert monitor: certificate expiry critical",
		slog.String("domain", domain),
		slog.Time("expires_at", cert.NotAfter),
		slog.Float64("days_remaining", daysRemaining),
	)
	m.emitEvent(ctx, events.EventTypeTLSCertExpiryCritical, msg, cert, domain, remaining)

	m.mu.Lock()
	m.degraded = true
	m.degradedMsg = fmt.Sprintf("TLS certificate expires in %.0f days", daysRemaining)
	m.mu.Unlock()
}

// setWarning emits a tls.cert_expiry_warning event. The health status is not
// changed to degraded for warnings — only critical threshold triggers degraded.
func (m *CertMonitor) setWarning(
	ctx context.Context,
	cert *x509.Certificate,
	domain string,
	remaining time.Duration,
	daysRemaining float64,
) {
	msg := fmt.Sprintf(
		"TLS certificate for %q expires in %.1f days (%s) — WARNING",
		domain, daysRemaining, cert.NotAfter.UTC().Format(time.RFC3339),
	)
	m.logger.WarnContext(ctx, "tls cert monitor: certificate expiry warning",
		slog.String("domain", domain),
		slog.Time("expires_at", cert.NotAfter),
		slog.Float64("days_remaining", daysRemaining),
	)
	m.emitEvent(ctx, events.EventTypeTLSCertExpiryWarning, msg, cert, domain, remaining)

	// Warning does not change the degraded state.
	m.mu.Lock()
	m.degraded = false
	m.degradedMsg = ""
	m.mu.Unlock()
}

// emitEvent emits a structured domain event. Errors are logged but not propagated.
func (m *CertMonitor) emitEvent(
	ctx context.Context,
	eventType string,
	aiSummary string,
	cert *x509.Certificate,
	domain string,
	remaining time.Duration,
) {
	if m.eventLog == nil {
		return
	}
	ev := events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     eventType,
		Timestamp:     time.Now().UTC(),
		AISummary:     aiSummary,
		Payload: map[string]any{
			"domain":         domain,
			"subject":        cert.Subject.String(),
			"issuer":         cert.Issuer.String(),
			"expires_at":     cert.NotAfter.UTC().Format(time.RFC3339),
			"days_remaining": remaining.Hours() / 24,
			"serial_number":  cert.SerialNumber.String(),
		},
	}
	if err := m.eventLog.Log(ctx, ev); err != nil {
		m.logger.WarnContext(ctx, "tls cert monitor: failed to emit event",
			slog.String("event_type", eventType),
			slog.String("error", err.Error()),
		)
	}
}
