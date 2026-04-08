package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestNewStatusHTTPClient(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		wantSkipTLS bool
	}{
		{
			name: "TLS disabled — standard transport",
			cfg: &config.Config{
				TLS: config.TLSConfig{Enabled: false, Provider: ""},
			},
			wantSkipTLS: false,
		},
		{
			name: "TLS letsencrypt — standard transport",
			cfg: &config.Config{
				TLS: config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: "example.com"},
			},
			wantSkipTLS: false,
		},
		{
			name: "TLS external — standard transport",
			cfg: &config.Config{
				TLS: config.TLSConfig{Enabled: true, Provider: "external"},
			},
			wantSkipTLS: false,
		},
		{
			name: "TLS self-signed — InsecureSkipVerify set",
			cfg: &config.Config{
				TLS: config.TLSConfig{Enabled: true, Provider: "self-signed"},
			},
			wantSkipTLS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newStatusHTTPClient(tt.cfg)
			if client == nil {
				t.Fatal("newStatusHTTPClient returned nil")
			}

			transport, ok := client.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("expected *http.Transport, got %T", client.Transport)
			}

			var gotSkipTLS bool
			if transport.TLSClientConfig != nil {
				gotSkipTLS = transport.TLSClientConfig.InsecureSkipVerify
			}

			if gotSkipTLS != tt.wantSkipTLS {
				t.Errorf("InsecureSkipVerify = %v, want %v", gotSkipTLS, tt.wantSkipTLS)
			}
		})
	}
}

func TestNewStatusHTTPClient_SelfSigned_CanReachTLSServer(t *testing.T) {
	// Confirm the self-signed client actually succeeds against a TLS test server.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{
		TLS: config.TLSConfig{Enabled: true, Provider: "self-signed"},
	}
	client := newStatusHTTPClient(cfg)

	// Override the transport TLS config with InsecureSkipVerify so the test
	// server's self-signed cert is accepted.
	resp, err := client.Get(srv.URL) //nolint:noctx // test-only
	if err != nil {
		t.Fatalf("GET against TLS server failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
