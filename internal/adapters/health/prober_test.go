package health

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// healthBody is a helper to produce a valid /_vibewarden/health JSON response.
func healthBody(status, version string, components map[string]string) []byte {
	b, _ := json.Marshal(map[string]any{
		"status":     status,
		"version":    version,
		"components": components,
	})
	return b
}

func TestHTTPProber_Probe_200_ValidBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(healthBody("ok", "0.18.4", map[string]string{
			"sidecar":  "ok",
			"upstream": "ok",
		}))
	}))
	defer srv.Close()

	// Use the test server's own client (trusts its cert).
	prober := &HTTPProber{client: srv.Client()}

	doc, err := prober.Probe(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if doc.Status != "ok" {
		t.Errorf("status = %q, want %q", doc.Status, "ok")
	}
	if doc.Version != "0.18.4" {
		t.Errorf("version = %q, want %q", doc.Version, "0.18.4")
	}
	if doc.Components["upstream"] != "ok" {
		t.Errorf("components.upstream = %q, want %q", doc.Components["upstream"], "ok")
	}
}

func TestHTTPProber_Probe_200_MalformedBody(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not json", "this is not json"},
		{"missing status", `{"version":"0.18.4","components":{"sidecar":"ok"}}`},
		{"missing components", `{"status":"ok","version":"0.18.4"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			prober := &HTTPProber{client: srv.Client()}
			_, err := prober.Probe(context.Background(), srv.URL)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ports.ErrProbeMalformed) {
				t.Errorf("expected ErrProbeMalformed in chain, got: %v", err)
			}
		})
	}
}

func TestHTTPProber_Probe_Non200(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"502 Bad Gateway", http.StatusBadGateway, "Bad Gateway"},
		{"503 Service Unavailable", http.StatusServiceUnavailable, "Service Unavailable"},
		{"404 Not Found", http.StatusNotFound, "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			prober := &HTTPProber{client: srv.Client()}
			_, err := prober.Probe(context.Background(), srv.URL)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ports.ErrProbeNon200) {
				t.Errorf("expected ErrProbeNon200 in chain, got: %v", err)
			}
			var ne *ports.ProbeNon200Error
			if !errors.As(err, &ne) {
				t.Fatalf("expected *ProbeNon200Error, got: %T", err)
			}
			if ne.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %d, want %d", ne.StatusCode, tt.statusCode)
			}
			if !strings.Contains(ne.Body, tt.body) {
				t.Errorf("body snippet %q does not contain %q", ne.Body, tt.body)
			}
		})
	}
}

func TestHTTPProber_Probe_ConnectionRefused(t *testing.T) {
	// Find a port that is definitely not listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not find free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	prober := NewLocalhostProber(2 * time.Second)
	_, err = prober.Probe(context.Background(), "https://"+addr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ports.ErrProbeRefused) {
		t.Errorf("expected ErrProbeRefused, got: %v", err)
	}
}

func TestHTTPProber_Probe_BodyTruncation(t *testing.T) {
	// Response body larger than maxBodySnippet should be truncated.
	longBody := strings.Repeat("x", maxBodySnippet*3)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(longBody))
	}))
	defer srv.Close()

	prober := &HTTPProber{client: srv.Client()}
	_, err := prober.Probe(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ne *ports.ProbeNon200Error
	if !errors.As(err, &ne) {
		t.Fatalf("expected *ProbeNon200Error, got: %T", err)
	}
	if len(ne.Body) > maxBodySnippet {
		t.Errorf("body snippet length %d exceeds maxBodySnippet %d", len(ne.Body), maxBodySnippet)
	}
}

func TestNewLocalhostProber_InsecureSkipVerify(t *testing.T) {
	// A TLS server presenting a self-signed cert should be reachable when
	// using NewLocalhostProber (InsecureSkipVerify=true).
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(healthBody("ok", "0.18.4", map[string]string{
			"sidecar":  "ok",
			"upstream": "ok",
		}))
	}))
	defer srv.Close()

	prober := NewLocalhostProber(3 * time.Second)
	doc, err := prober.Probe(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("NewLocalhostProber should succeed against self-signed TLS server: %v", err)
	}
	if doc.Status != "ok" {
		t.Errorf("status = %q, want %q", doc.Status, "ok")
	}
}

func TestNewStrictProber_RejectsSelfSignedCert(t *testing.T) {
	// httptest.NewTLSServer presents a self-signed certificate. NewStrictProber
	// uses the stdlib default TLS verification (no InsecureSkipVerify), so the
	// handshake must fail with an x509 unknown-authority error. This is the
	// symmetric counterpart to TestNewLocalhostProber_InsecureSkipVerify and
	// validates the --env safety property: a production probe against an invalid
	// cert is rejected, not silently accepted.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(healthBody("ok", "0.18.4", map[string]string{
			"sidecar":  "ok",
			"upstream": "ok",
		}))
	}))
	defer srv.Close()

	prober := NewStrictProber(3 * time.Second)
	_, err := prober.Probe(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("NewStrictProber should reject a self-signed certificate, but got nil error")
	}

	// The error must be (or wrap) an x509.UnknownAuthorityError, proving that
	// strict TLS verification is active. Accept either errors.As match or a
	// substring check to remain robust across Go stdlib versions.
	var unknownAuth x509.UnknownAuthorityError
	msg := err.Error()
	if !errors.As(err, &unknownAuth) &&
		!strings.Contains(msg, "certificate signed by unknown authority") &&
		!strings.Contains(msg, "x509") {
		t.Errorf("expected x509 TLS rejection error, got: %v", err)
	}
}

func TestHTTPProber_Probe_WithSite(t *testing.T) {
	// When the server includes a "site" field it is preserved in the HealthDocument.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		b, _ := json.Marshal(map[string]any{
			"status":     "ok",
			"version":    "0.18.4",
			"site":       "app1",
			"components": map[string]string{"sidecar": "ok", "upstream": "ok"},
		})
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	prober := &HTTPProber{client: srv.Client()}
	doc, err := prober.Probe(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Site != "app1" {
		t.Errorf("site = %q, want %q", doc.Site, "app1")
	}
}

func TestHTTPProber_Probe_DNSFailure(t *testing.T) {
	// nonexistent.invalid uses the RFC 6761 reserved .invalid TLD, which is
	// guaranteed never to resolve. We expect ErrDNSFailure from the prober.
	prober := NewStrictProber(2 * time.Second)
	_, err := prober.Probe(context.Background(), "https://nonexistent.invalid/_vibewarden/health")
	if err == nil {
		t.Fatal("expected error for unresolvable hostname, got nil")
	}
	if !errors.Is(err, ports.ErrDNSFailure) {
		t.Errorf("expected ErrDNSFailure, got: %v", err)
	}
}

func TestHTTPProber_Probe_ContextCancelled(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is cancelled.
		<-r.Context().Done()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	prober := &HTTPProber{client: srv.Client()}
	_, err := prober.Probe(ctx, srv.URL)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	// The error should not be ErrProbeRefused (it's a cancellation).
	if errors.Is(err, ports.ErrProbeRefused) {
		t.Errorf("cancelled context should not produce ErrProbeRefused")
	}
	_ = fmt.Sprintf("%v", err) // suppress unused import warning
}

// tlsAlertServer starts a raw TCP listener that completes the TCP handshake,
// reads (and discards) the TLS ClientHello, then responds with a TLS 1.2 alert
// record containing the given alert code. The server URL is returned. Callers
// must close the returned function to shut down the listener.
//
// TLS record structure (5-byte header + payload):
//
//	ContentType: 21 (alert)
//	Version:     0x03 0x03 (TLS 1.2)
//	Length:      0x00 0x02
//	Level:       0x02 (fatal)
//	Description: alertCode
func tlsAlertServer(t *testing.T, alertCode byte) (url string, close func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tlsAlertServer: listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				// Drain the ClientHello (up to 4 KiB is plenty).
				buf := make([]byte, 4096)
				_, _ = c.Read(buf)
				// Send a fatal TLS alert.
				alert := []byte{21, 0x03, 0x03, 0x00, 0x02, 0x02, alertCode}
				_, _ = c.Write(alert)
			}(conn)
		}
	}()
	return "https://" + ln.Addr().String(), func() { _ = ln.Close() }
}

// TestIsTLSHandshakeError_Substrings verifies that isTLSHandshakeError returns
// true for each of the four pinned substring patterns and false for unrelated
// error messages.
func TestIsTLSHandshakeError_Substrings(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{"tls internal error", "Get \"https://host\": tls: internal error", true},
		{"tls handshake failure", "Get \"https://host\": tls: handshake failure", true},
		{"bad certificate", "Get \"https://host\": bad certificate", true},
		{"tls protocol version not supported", "Get \"https://host\": tls: protocol version not supported", true},
		{"connection refused", "connect: connection refused", false},
		{"generic network error", "no route to host", false},
		{"empty string", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTLSHandshakeError(fmt.Errorf("%s", tt.msg))
			if got != tt.want {
				t.Errorf("isTLSHandshakeError(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

// TestHTTPProber_Probe_TLSHandshakeError verifies that each of the four
// recognised TLS handshake failure patterns causes the adapter to return
// ports.ErrTLSHandshake. The test uses two strategies:
//
//  1. A raw TCP server that sends a specific TLS alert record (alert codes 40,
//     42, 80 for handshake_failure, bad_certificate, internal_error).
//  2. A TLS version mismatch between server (min TLS 1.3) and client (max TLS
//     1.2) to produce "tls: protocol version not supported".
//
// Strategy 1 covers the exact alert-code-to-substring mapping that Go's
// crypto/tls uses for TLS 1.2 records; strategy 2 covers the version
// negotiation path.
func TestHTTPProber_Probe_TLSHandshakeError(t *testing.T) {
	// Strategy 1: raw alert servers for handshake_failure (40), bad_certificate
	// (42), and internal_error (80). These alert codes are defined in RFC 5246 §
	// 7.2 and produce the corresponding error string in Go's crypto/tls.
	alertTests := []struct {
		name       string
		alertCode  byte
		wantSubstr string
	}{
		{"tls: internal error", 80, "tls: internal error"},
		{"tls: handshake failure", 40, "tls: handshake failure"},
		{"bad certificate", 42, "bad certificate"},
	}

	for _, tt := range alertTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			srvURL, closeSrv := tlsAlertServer(t, tt.alertCode)
			defer closeSrv()

			// NewLocalhostProber uses InsecureSkipVerify, but we still expect a
			// handshake error because the server sends a fatal alert.
			prober := NewLocalhostProber(2 * time.Second)
			_, err := prober.Probe(context.Background(), srvURL)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSubstr)
			}
			if !errors.Is(err, ports.ErrTLSHandshake) {
				t.Errorf("expected ErrTLSHandshake (substring %q), got: %v", tt.wantSubstr, err)
			}
		})
	}

	// Strategy 2: TLS version mismatch — server requires TLS 1.3, client only
	// allows up to TLS 1.2 — produces "tls: protocol version not supported".
	t.Run("tls: protocol version not supported", func(t *testing.T) {
		// Build a TLS server that requires TLS 1.3 or higher.
		baseSrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		baseSrv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
		baseSrv.StartTLS()
		defer baseSrv.Close()

		// Build a client that is restricted to TLS 1.2 or lower so that the
		// version negotiation fails.
		client := &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{ //nolint:gosec // test-only client restriction
					MaxVersion: tls.VersionTLS12,
				},
			},
		}
		prober := &HTTPProber{client: client}
		_, err := prober.Probe(context.Background(), baseSrv.URL)
		if err == nil {
			t.Fatal("expected TLS version error, got nil")
		}
		if !errors.Is(err, ports.ErrTLSHandshake) {
			t.Errorf("expected ErrTLSHandshake for version mismatch, got: %v", err)
		}
	})
}
