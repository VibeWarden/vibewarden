package caddy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

// makeTLSServer boots an httptest.NewUnstartedServer configured with a
// self-signed leaf whose issuer CN and NotAfter are test-controlled.
// Returns the host, port and cleanup function.
func makeTLSServer(t *testing.T, issuerCN string, notAfter time.Time) (host string, port int, cleanup func()) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	// x509.CreateCertificate derives the Issuer from the parent's Subject,
	// so we build a parent cert with Subject = <issuerCN> and sign the leaf
	// with it. This matches how Caddy's internal CA produces leaves with
	// Issuer.CommonName = "Caddy Local Authority".
	parentTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: issuerCN},
		NotBefore:    notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:     notAfter.Add(365 * 24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    notAfter.Add(-24 * time.Hour),
		NotAfter:     notAfter,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, parentTemplate, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	tlsCert := tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: key}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	srv.StartTLS()

	u := srv.Listener.Addr().(*net.TCPAddr)
	return u.IP.String(), u.Port, srv.Close
}

func TestHandshakeResolver_Resolve(t *testing.T) {
	fixedNow := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	t.Run("self-signed local issuer → SelfSignedLocal", func(t *testing.T) {
		// Short-lived leaf (8 hours). Would have tripped the old day-count
		// warning — new resolver ignores NotAfter when issuer CN matches.
		host, port, cleanup := makeTLSServer(t, caddyLocalIssuerCN, fixedNow.Add(8*time.Hour))
		defer cleanup()

		cfg := &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "self-signed"}}
		r := NewHandshakeResolver(cfg, host, port)
		r.now = func() time.Time { return fixedNow }

		state, err := r.Resolve(context.Background())
		if err != nil {
			t.Fatalf("Resolve() unexpected error: %v", err)
		}
		if state.Kind() != tlsdomain.KindSelfSignedLocal {
			t.Errorf("Kind() = %v, want SelfSignedLocal", state.Kind())
		}
	})

	t.Run("external issuer with long expiry → Obtained", func(t *testing.T) {
		host, port, cleanup := makeTLSServer(t, "ExampleCA", fixedNow.Add(90*24*time.Hour))
		defer cleanup()

		cfg := &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "letsencrypt"}}
		r := NewHandshakeResolver(cfg, host, port)
		r.now = func() time.Time { return fixedNow }

		state, err := r.Resolve(context.Background())
		if err != nil {
			t.Fatalf("Resolve() unexpected error: %v", err)
		}
		if state.Kind() != tlsdomain.KindObtained {
			t.Errorf("Kind() = %v, want Obtained", state.Kind())
		}
	})

	t.Run("external issuer near expiry → ExpiringSoon", func(t *testing.T) {
		host, port, cleanup := makeTLSServer(t, "ExampleCA", fixedNow.Add(3*24*time.Hour))
		defer cleanup()

		cfg := &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "letsencrypt"}}
		r := NewHandshakeResolver(cfg, host, port)
		r.now = func() time.Time { return fixedNow }

		state, err := r.Resolve(context.Background())
		if err != nil {
			t.Fatalf("Resolve() unexpected error: %v", err)
		}
		if state.Kind() != tlsdomain.KindExpiringSoon {
			t.Errorf("Kind() = %v, want ExpiringSoon", state.Kind())
		}
		if state.DaysLeft() < 0 || state.DaysLeft() > 7 {
			t.Errorf("DaysLeft() = %d, want 0..7", state.DaysLeft())
		}
	})

	t.Run("sidecar unreachable → Unknown", func(t *testing.T) {
		// Bind and release to get a closed port.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		addr := ln.Addr().(*net.TCPAddr)
		_ = ln.Close()

		cfg := &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "self-signed"}}
		r := NewHandshakeResolver(cfg, "127.0.0.1", addr.Port)
		r.timeout = 500 * time.Millisecond

		state, err := r.Resolve(context.Background())
		if err != nil {
			t.Fatalf("Resolve() unexpected error: %v", err)
		}
		if state.Kind() != tlsdomain.KindUnknown {
			t.Errorf("Kind() = %v, want Unknown", state.Kind())
		}
	})

	t.Run("tls disabled → Disabled", func(t *testing.T) {
		cfg := &config.Config{TLS: config.TLSConfig{Enabled: false}}
		r := NewHandshakeResolver(cfg, "127.0.0.1", 0)

		state, err := r.Resolve(context.Background())
		if err != nil {
			t.Fatalf("Resolve() unexpected error: %v", err)
		}
		if state.Kind() != tlsdomain.KindDisabled {
			t.Errorf("Kind() = %v, want Disabled", state.Kind())
		}
	})
}
