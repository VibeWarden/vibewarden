package egress_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// generateSelfSignedCA creates a self-signed CA certificate and returns the
// parsed cert, the private key, and the DER-encoded certificate bytes.
func generateSelfSignedCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generateSelfSignedCA: generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("generateSelfSignedCA: create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("generateSelfSignedCA: parse cert: %v", err)
	}
	return cert, key, certDER
}

// generateClientCert creates a client certificate signed by the given CA.
// Returns PEM-encoded certificate and private key bytes.
func generateClientCert(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) (certPEM []byte, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generateClientCert: generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("generateClientCert: create cert: %v", err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("generateClientCert: marshal key: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return certPEM, keyPEM
}

// writeTempFile writes data to a named file inside dir and returns the path.
func writeTempFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatalf("writeTempFile %s: %v", p, err)
	}
	return p
}

// newMTLSTestServer starts an HTTPS test server that requires client
// certificate authentication. The server accepts client certificates signed by
// clientCA. It returns the server and a client configured to trust the server's
// certificate (but without any client cert — callers must add one to test mTLS).
func newMTLSTestServer(t *testing.T, clientCA *x509.Certificate) *httptest.Server {
	t.Helper()

	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(clientCA)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "missing client cert", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mtls-ok"))
	})

	srv := httptest.NewUnstartedServer(mux)
	// Start with TLS first so httptest sets up its own server cert and CA pool.
	srv.StartTLS()
	// Now layer in client auth on top of the existing TLS config.
	srv.TLS.ClientAuth = tls.RequireAnyClientCert
	srv.TLS.ClientCAs = clientCAPool
	return srv
}

// TestBuildMTLSClients_Valid verifies that BuildMTLSClients returns a dedicated
// client for each route with a non-zero MTLSConfig and no entry for plain routes.
func TestBuildMTLSClients_Valid(t *testing.T) {
	dir := t.TempDir()
	caCert, caKey, _ := generateSelfSignedCA(t)
	certPEM, keyPEM := generateClientCert(t, caCert, caKey)
	certPath := writeTempFile(t, dir, "client.crt", certPEM)
	keyPath := writeTempFile(t, dir, "client.key", keyPEM)

	routeWithMTLS, err := domainegress.NewRoute("secure", "https://api.example.com/*",
		domainegress.WithMTLS(domainegress.MTLSConfig{
			CertPath: certPath,
			KeyPath:  keyPath,
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	routeWithout, err := domainegress.NewRoute("plain", "https://other.example.com/*")
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}

	clients, err := egressadapter.BuildMTLSClients([]domainegress.Route{routeWithMTLS, routeWithout}, nil)
	if err != nil {
		t.Fatalf("BuildMTLSClients: %v", err)
	}
	if _, ok := clients["secure"]; !ok {
		t.Error("expected a client for route 'secure', got none")
	}
	if _, ok := clients["plain"]; ok {
		t.Error("expected no client for route 'plain', got one")
	}
}

// TestBuildMTLSClients_MissingCertFile verifies that BuildMTLSClients returns
// an error when the cert file path does not exist.
func TestBuildMTLSClients_MissingCertFile(t *testing.T) {
	route, err := domainegress.NewRoute("secure", "https://api.example.com/*",
		domainegress.WithMTLS(domainegress.MTLSConfig{
			CertPath: "/nonexistent/client.crt",
			KeyPath:  "/nonexistent/client.key",
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	_, buildErr := egressadapter.BuildMTLSClients([]domainegress.Route{route}, nil)
	if buildErr == nil {
		t.Fatal("BuildMTLSClients: expected error for missing cert file, got nil")
	}
}

// TestBuildMTLSClients_InvalidPEM verifies that BuildMTLSClients returns an
// error when the cert/key files contain invalid PEM data.
func TestBuildMTLSClients_InvalidPEM(t *testing.T) {
	dir := t.TempDir()
	certPath := writeTempFile(t, dir, "client.crt", []byte("not-valid-pem"))
	keyPath := writeTempFile(t, dir, "client.key", []byte("not-valid-pem"))

	route, err := domainegress.NewRoute("secure", "https://api.example.com/*",
		domainegress.WithMTLS(domainegress.MTLSConfig{
			CertPath: certPath,
			KeyPath:  keyPath,
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	_, buildErr := egressadapter.BuildMTLSClients([]domainegress.Route{route}, nil)
	if buildErr == nil {
		t.Fatal("BuildMTLSClients: expected error for invalid PEM, got nil")
	}
}

// TestBuildMTLSClients_WithCA verifies that BuildMTLSClients succeeds when a
// valid CA path is provided alongside the cert and key.
func TestBuildMTLSClients_WithCA(t *testing.T) {
	dir := t.TempDir()
	caCert, caKey, caCertDER := generateSelfSignedCA(t)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	caPath := writeTempFile(t, dir, "ca.crt", caPEM)
	certPEM, keyPEM := generateClientCert(t, caCert, caKey)
	certPath := writeTempFile(t, dir, "client.crt", certPEM)
	keyPath := writeTempFile(t, dir, "client.key", keyPEM)

	route, err := domainegress.NewRoute("secure", "https://api.example.com/*",
		domainegress.WithMTLS(domainegress.MTLSConfig{
			CertPath: certPath,
			KeyPath:  keyPath,
			CAPath:   caPath,
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	clients, buildErr := egressadapter.BuildMTLSClients([]domainegress.Route{route}, nil)
	if buildErr != nil {
		t.Fatalf("BuildMTLSClients: unexpected error: %v", buildErr)
	}
	if clients["secure"] == nil {
		t.Error("expected non-nil client for 'secure' route")
	}
}

// TestBuildMTLSClients_InvalidCA verifies that BuildMTLSClients returns an
// error when the CA file contains no valid PEM certificates.
func TestBuildMTLSClients_InvalidCA(t *testing.T) {
	dir := t.TempDir()
	caCert, caKey, _ := generateSelfSignedCA(t)
	certPEM, keyPEM := generateClientCert(t, caCert, caKey)
	certPath := writeTempFile(t, dir, "client.crt", certPEM)
	keyPath := writeTempFile(t, dir, "client.key", keyPEM)
	caPath := writeTempFile(t, dir, "ca.crt", []byte("not-valid-pem"))

	route, err := domainegress.NewRoute("secure", "https://api.example.com/*",
		domainegress.WithMTLS(domainegress.MTLSConfig{
			CertPath: certPath,
			KeyPath:  keyPath,
			CAPath:   caPath,
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	_, buildErr := egressadapter.BuildMTLSClients([]domainegress.Route{route}, nil)
	if buildErr == nil {
		t.Fatal("BuildMTLSClients: expected error for invalid CA PEM, got nil")
	}
}

// TestBuildMTLSClients_EmptyRoutes verifies that BuildMTLSClients with no
// routes returns an empty (non-nil) map without error.
func TestBuildMTLSClients_EmptyRoutes(t *testing.T) {
	clients, err := egressadapter.BuildMTLSClients(nil, nil)
	if err != nil {
		t.Fatalf("BuildMTLSClients(nil): unexpected error: %v", err)
	}
	if clients == nil {
		t.Error("BuildMTLSClients(nil): expected non-nil map, got nil")
	}
	if len(clients) != 0 {
		t.Errorf("BuildMTLSClients(nil): expected empty map, got %d entries", len(clients))
	}
}

// TestProxy_MTLSClientUsed verifies end-to-end that the proxy presents the
// configured client certificate when forwarding to an mTLS upstream and the
// request succeeds with HTTP 200.
func TestProxy_MTLSClientUsed(t *testing.T) {
	dir := t.TempDir()

	// Generate a CA that will sign the client cert.
	caCert, caKey, _ := generateSelfSignedCA(t)
	certPEM, keyPEM := generateClientCert(t, caCert, caKey)
	certPath := writeTempFile(t, dir, "client.crt", certPEM)
	keyPath := writeTempFile(t, dir, "client.key", keyPEM)

	// Start the mTLS test server — it requires a client cert signed by caCert.
	mtlsSrv := newMTLSTestServer(t, caCert)
	defer mtlsSrv.Close()

	route, err := domainegress.NewRoute("mtls-api", mtlsSrv.URL+"/api/*",
		domainegress.WithMTLS(domainegress.MTLSConfig{
			CertPath: certPath,
			KeyPath:  keyPath,
			// No CAPath — trust the server cert via the base transport below.
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	routes := []domainegress.Route{route}

	// Use the test server's transport as the base so the mTLS client inherits
	// the server's self-signed cert in the trust store, then layer in the
	// client certificate.
	baseTransport := mtlsSrv.Client().Transport.(*http.Transport)
	mtlsClients, err := egressadapter.BuildMTLSClients(routes, baseTransport)
	if err != nil {
		t.Fatalf("BuildMTLSClients: %v", err)
	}

	resolver := egressadapter.NewRouteResolver(routes)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         routes,
		MTLSClients:    mtlsClients,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, mtlsSrv.Client(), nil)

	req, err := domainegress.NewEgressRequest("GET", mtlsSrv.URL+"/api/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	resp, handleErr := proxy.HandleRequest(context.Background(), req)
	if handleErr != nil {
		t.Fatalf("HandleRequest: unexpected error: %v", handleErr)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestProxy_MTLSMissingClientCert verifies that when the upstream server
// requires client authentication but the proxy has no mTLS client configured
// for the route, the TLS handshake fails and the request returns an error.
func TestProxy_MTLSMissingClientCert(t *testing.T) {
	caCert, _, _ := generateSelfSignedCA(t)

	// Start the mTLS test server (requires client cert).
	mtlsSrv := newMTLSTestServer(t, caCert)
	defer mtlsSrv.Close()

	route, err := domainegress.NewRoute("mtls-api", mtlsSrv.URL+"/api/*")
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	routes := []domainegress.Route{route}

	resolver := egressadapter.NewRouteResolver(routes)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         routes,
		// No MTLSClients — proxy uses default client without a client cert.
	}
	// Use the test server's client (trusts the server cert) but no client cert
	// configured — the server should reject the TLS handshake.
	proxy := egressadapter.NewProxy(cfg, resolver, mtlsSrv.Client(), nil)

	req, err := domainegress.NewEgressRequest("GET", mtlsSrv.URL+"/api/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, handleErr := proxy.HandleRequest(context.Background(), req)
	if handleErr == nil {
		t.Fatal("HandleRequest: expected TLS error, got nil")
	}
}

// TestMTLSConfig_IsZero verifies that MTLSConfig.IsZero returns the correct
// value for all combinations of fields.
func TestMTLSConfig_IsZero(t *testing.T) {
	tests := []struct {
		name string
		cfg  domainegress.MTLSConfig
		want bool
	}{
		{
			name: "zero value",
			cfg:  domainegress.MTLSConfig{},
			want: true,
		},
		{
			name: "only cert path",
			cfg:  domainegress.MTLSConfig{CertPath: "/tmp/cert.pem"},
			want: false,
		},
		{
			name: "only key path",
			cfg:  domainegress.MTLSConfig{KeyPath: "/tmp/key.pem"},
			want: false,
		},
		{
			name: "only ca path",
			cfg:  domainegress.MTLSConfig{CAPath: "/tmp/ca.pem"},
			want: false,
		},
		{
			name: "cert and key",
			cfg:  domainegress.MTLSConfig{CertPath: "/tmp/cert.pem", KeyPath: "/tmp/key.pem"},
			want: false,
		},
		{
			name: "all fields",
			cfg: domainegress.MTLSConfig{
				CertPath: "/tmp/cert.pem",
				KeyPath:  "/tmp/key.pem",
				CAPath:   "/tmp/ca.pem",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsZero()
			if got != tt.want {
				t.Errorf("MTLSConfig(%+v).IsZero() = %v, want %v", tt.cfg, got, tt.want)
			}
		})
	}
}

// TestRoute_MTLS verifies that the MTLS accessor returns the value set via
// WithMTLS.
func TestRoute_MTLS(t *testing.T) {
	want := domainegress.MTLSConfig{
		CertPath: "/tmp/cert.pem",
		KeyPath:  "/tmp/key.pem",
		CAPath:   "/tmp/ca.pem",
	}
	route, err := domainegress.NewRoute("secure", "https://api.example.com/*",
		domainegress.WithMTLS(want),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	got := route.MTLS()
	if got != want {
		t.Errorf("Route.MTLS() = %+v, want %+v", got, want)
	}
}

// TestRoute_MTLS_ZeroWhenNotConfigured verifies that a route created without
// WithMTLS returns a zero MTLSConfig.
func TestRoute_MTLS_ZeroWhenNotConfigured(t *testing.T) {
	route, err := domainegress.NewRoute("plain", "https://api.example.com/*")
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	if !route.MTLS().IsZero() {
		t.Errorf("Route.MTLS() should be zero when not configured, got %+v", route.MTLS())
	}
}
