package health

import (
	"context"
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
