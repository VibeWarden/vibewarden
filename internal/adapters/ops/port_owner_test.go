package ops_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// hostPortFromURL splits a URL like https://127.0.0.1:12345 into host and port.
func hostPortFromURL(t *testing.T, raw string) (string, int) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	portStr := u.Port()
	if portStr == "" {
		portStr = "443"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return u.Hostname(), port
}

func TestVibeWardenHealthProbe_ProbeOwner(t *testing.T) {
	t.Parallel()

	okBody := `{"status":"ok","version":"test","components":{"sidecar":"ok"}}`
	foreignBody := `{"hello":"world"}`

	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantCode int // 0 => no server
		want     ports.PortOwner
	}{
		{
			name: "vibewarden signature matches → OwnerVibeWarden",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, okBody)
			},
			want: ports.OwnerVibeWarden,
		},
		{
			name: "200 with non-signature body → OwnerForeign",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, foreignBody)
			},
			want: ports.OwnerForeign,
		},
		{
			name: "200 with signature on wrong path still matches (static handler)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Probe path mismatch — foreign servers that return 200+signature
				// on any path still look like a VibeWarden sidecar by contract.
				// This test case asserts the path is actually hit.
				if r.URL.Path != "/_vibewarden/health" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, okBody)
			},
			want: ports.OwnerVibeWarden,
		},
		{
			name: "non-2xx takes precedence over body → OwnerForeign",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprint(w, okBody)
			},
			want: ports.OwnerForeign,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewTLSServer(tt.handler)
			defer srv.Close()

			probe := opsadapter.NewVibeWardenHealthProbe(srv.Client())
			host, port := hostPortFromURL(t, srv.URL)

			got := probe.ProbeOwner(context.Background(), host, port)
			if got != tt.want {
				t.Errorf("ProbeOwner() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVibeWardenHealthProbe_ConnectionRefused(t *testing.T) {
	t.Parallel()

	// Bind and immediately release a port so we know it's closed.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	probe := opsadapter.NewVibeWardenHealthProbe(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got := probe.ProbeOwner(ctx, "127.0.0.1", addr.Port)
	if got != ports.OwnerForeign {
		t.Errorf("ProbeOwner() on closed port = %q, want %q", got, ports.OwnerForeign)
	}
}

func TestVibeWardenHealthProbe_NonTLSListener(t *testing.T) {
	t.Parallel()

	// Plain TCP listener that accepts and closes — TLS handshake must fail.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	probe := opsadapter.NewVibeWardenHealthProbe(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got := probe.ProbeOwner(ctx, "127.0.0.1", addr.Port)
	if got != ports.OwnerForeign {
		t.Errorf("ProbeOwner() on plain-TCP listener = %q, want %q", got, ports.OwnerForeign)
	}
}

func TestVibeWardenHealthProbe_WildcardHostRewritten(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":"ok","version":"x"}`)
	}))
	defer srv.Close()

	// Use the test server's client so the TLS cert trust is set up.
	probe := opsadapter.NewVibeWardenHealthProbe(srv.Client())
	_, port := hostPortFromURL(t, srv.URL)

	// The probe should rewrite 0.0.0.0 to 127.0.0.1 internally.
	got := probe.ProbeOwner(context.Background(), "0.0.0.0", port)
	if got != ports.OwnerVibeWarden {
		t.Errorf("ProbeOwner(0.0.0.0) = %q, want %q", got, ports.OwnerVibeWarden)
	}
}

// TestVibeWardenHealthProbe_SignaturePrefixExact ensures the prefix match is
// strict: a body that merely contains the signature substring but does not
// start with it must be rejected.
func TestVibeWardenHealthProbe_SignaturePrefixExact(t *testing.T) {
	t.Parallel()

	body := `prefix-garbage{"status":"ok","version":"test"}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	defer srv.Close()

	probe := opsadapter.NewVibeWardenHealthProbe(srv.Client())
	host, port := hostPortFromURL(t, srv.URL)

	got := probe.ProbeOwner(context.Background(), host, port)
	if got != ports.OwnerForeign {
		t.Errorf("ProbeOwner() with non-prefix signature = %q, want %q", got, ports.OwnerForeign)
	}
	if strings.HasPrefix(body, `{"status":"ok"`) {
		t.Fatalf("test body unexpectedly has the signature prefix")
	}
}
