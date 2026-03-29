package egress

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// MTLSClientMap is a map from route name to a dedicated *http.Client that
// carries the route's mTLS client certificate. Routes without an MTLSConfig
// are not present in the map; the proxy falls back to its default client.
type MTLSClientMap map[string]*http.Client

// BuildMTLSClients reads the cert/key/CA files for every route that has a
// non-zero MTLSConfig, validates them, and constructs a dedicated *http.Client
// per route. The returned map is keyed by route name.
//
// The base transport is cloned from baseTransport for each route so that SSRF
// guards and other per-proxy dial hooks are inherited. Pass nil to clone from
// http.DefaultTransport.
//
// An error is returned if any cert, key, or CA file for any route cannot be
// read or parsed. The error message identifies the offending route name.
func BuildMTLSClients(routes []domainegress.Route, baseTransport *http.Transport) (MTLSClientMap, error) {
	m := make(MTLSClientMap)
	for _, route := range routes {
		cfg := route.MTLS()
		if cfg.IsZero() {
			continue
		}
		client, err := buildMTLSClient(cfg, baseTransport)
		if err != nil {
			return nil, fmt.Errorf("route %q mTLS config: %w", route.Name(), err)
		}
		m[route.Name()] = client
	}
	return m, nil
}

// buildMTLSClient constructs an *http.Client for a single MTLSConfig.
func buildMTLSClient(cfg domainegress.MTLSConfig, baseTransport *http.Transport) (*http.Client, error) {
	tlsCert, err := loadClientCert(cfg.CertPath, cfg.KeyPath)
	if err != nil {
		return nil, err
	}

	var rootCAs *x509.CertPool
	if cfg.CAPath != "" {
		rootCAs, err = loadCACert(cfg.CAPath)
		if err != nil {
			return nil, err
		}
	}

	var t *http.Transport
	if baseTransport != nil {
		t = baseTransport.Clone()
	} else {
		t = http.DefaultTransport.(*http.Transport).Clone()
	}

	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		t.TLSClientConfig = t.TLSClientConfig.Clone()
	}
	t.TLSClientConfig.MinVersion = tls.VersionTLS12
	t.TLSClientConfig.Certificates = []tls.Certificate{tlsCert}
	if rootCAs != nil {
		t.TLSClientConfig.RootCAs = rootCAs
	}

	return &http.Client{
		Transport: t,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// loadClientCert reads and parses a PEM-encoded certificate and private key
// pair from the given file paths. Returns an error when either path is empty,
// either file cannot be read, or the cert/key pair is not valid PEM.
func loadClientCert(certPath, keyPath string) (tls.Certificate, error) {
	if certPath == "" {
		return tls.Certificate{}, fmt.Errorf("cert_path is required for mTLS")
	}
	if keyPath == "" {
		return tls.Certificate{}, fmt.Errorf("key_path is required for mTLS")
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("reading client cert %q: %w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("reading client key %q: %w", keyPath, err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parsing client cert/key pair (%q, %q): %w", certPath, keyPath, err)
	}
	return cert, nil
}

// loadCACert reads and parses a PEM-encoded CA certificate bundle from the
// given file path and returns a populated *x509.CertPool. Returns an error
// when the file cannot be read or contains no valid PEM certificates.
func loadCACert(caPath string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("reading CA cert %q: %w", caPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("CA cert file %q contains no valid PEM certificates", caPath)
	}
	return pool, nil
}

// isMTLSError reports whether err looks like a TLS handshake failure.
// net/http wraps TLS errors in *url.Error; we inspect the error message for
// known TLS alert strings since the underlying tls package errors are not
// exported in a way that is stable across Go versions.
func isMTLSError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "tls:") ||
		strings.Contains(msg, "TLS handshake") ||
		strings.Contains(msg, "certificate") ||
		strings.Contains(msg, "handshake failure")
}
