package caddy

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestBuildCatchAllHandlers_Order pins the middleware chain order invariant.
// The strip-user-headers handler must be at index 0 and the reverse_proxy
// handler must be the last element.
func TestBuildCatchAllHandlers_Order(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}

	handlers, err := buildCatchAllHandlers(cfg)
	if err != nil {
		t.Fatalf("buildCatchAllHandlers() unexpected error: %v", err)
	}
	if len(handlers) == 0 {
		t.Fatal("handlers slice is empty")
	}

	// Index 0 must be the strip-user-headers handler.
	first := handlers[0]
	if first["handler"] != "headers" {
		t.Errorf("handlers[0].handler = %q, want %q", first["handler"], "headers")
	}
	req, ok := first["request"].(map[string]any)
	if !ok {
		t.Fatal("handlers[0].request is not a map")
	}
	del, ok := req["delete"].([]string)
	if !ok || len(del) == 0 {
		t.Fatal("handlers[0].request.delete is not a non-empty []string")
	}

	// Last must be the reverse_proxy handler.
	last := handlers[len(handlers)-1]
	if last["handler"] != "reverse_proxy" {
		t.Errorf("handlers[last].handler = %q, want %q", last["handler"], "reverse_proxy")
	}
}

// TestBuildCatchAllHandlers_FullChainOrder verifies the full chain ordering when
// all optional middleware is enabled.
func TestBuildCatchAllHandlers_FullChainOrder(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
		SecurityHeaders: ports.SecurityHeadersConfig{
			Enabled: true,
		},
		ResponseHeaders: ports.ResponseHeadersConfig{
			Enabled: true,
			Set:     map[string]string{"X-Test": "1"},
		},
		BodySize: ports.BodySizeConfig{
			Enabled:  true,
			MaxBytes: 1024,
		},
		Compression: ports.CompressionConfig{
			Enabled: true,
		},
	}

	handlers, err := buildCatchAllHandlers(cfg)
	if err != nil {
		t.Fatalf("buildCatchAllHandlers() unexpected error: %v", err)
	}

	// headers (strip) at 0, reverse_proxy last.
	if handlers[0]["handler"] != "headers" {
		t.Errorf("index 0: want headers strip handler, got %q", handlers[0]["handler"])
	}
	if handlers[len(handlers)-1]["handler"] != "reverse_proxy" {
		t.Errorf("last: want reverse_proxy, got %q", handlers[len(handlers)-1]["handler"])
	}
}

// TestBuildUserHeaderStripHandler_WildcardPattern verifies that
// buildUserHeaderStripHandler emits a single Caddy suffix-wildcard delete pattern
// ("x-user-*") so that ALL X-User-* headers are stripped, including
// X-User-Name which was previously missing from the hardcoded list.
//
// The pattern uses Caddy's glob syntax (strings.HasSuffix(fieldName, "*")),
// NOT tilde-prefix regex notation. Tilde patterns fall through to the default
// case in Caddy's header-ops loop and call hdr.Del("~^x-user-") — a no-op
// because no header has that literal name.
//
// Regression test for #1264 (Part A).
func TestBuildUserHeaderStripHandler_WildcardPattern(t *testing.T) {
	h := buildUserHeaderStripHandler()

	if h["handler"] != "headers" {
		t.Fatalf("handler = %q, want %q", h["handler"], "headers")
	}
	req, ok := h["request"].(map[string]any)
	if !ok {
		t.Fatal("request field is not a map")
	}
	del, ok := req["delete"].([]string)
	if !ok {
		t.Fatalf("delete field is not []string, got %T", req["delete"])
	}
	if len(del) != 1 {
		t.Fatalf("delete has %d entries, want 1 (wildcard pattern)", len(del))
	}
	// Must be Caddy suffix-wildcard glob, NOT a tilde-prefix regex pattern.
	// "~^x-user-" is silently a no-op in Caddy's headers module.
	const wantPattern = "x-user-*"
	if del[0] != wantPattern {
		t.Errorf("delete[0] = %q, want %q (NOTE: tilde-prefix regex patterns are not supported by Caddy headers module)", del[0], wantPattern)
	}
}

// TestApplyAutomaticHTTPS verifies the automatic_https field is set correctly
// for each TLS provider combination.
func TestApplyAutomaticHTTPS(t *testing.T) {
	tests := []struct {
		name        string
		tls         ports.TLSConfig
		wantKey     string
		wantAbsent  bool
		wantDisable bool
		wantNoRedir bool
	}{
		{
			name:        "TLS disabled — automatic_https disable=true",
			tls:         ports.TLSConfig{Enabled: false},
			wantKey:     "automatic_https",
			wantDisable: true,
		},
		{
			name: "ACME (letsencrypt) — no automatic_https key",
			tls: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
			},
			wantAbsent: true,
		},
		{
			name: "self-signed — disable_redirects=true",
			tls: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderSelfSigned,
			},
			wantKey:     "automatic_https",
			wantNoRedir: true,
		},
		{
			name: "external — disable_redirects=true",
			tls: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderExternal,
				CertPath: "/etc/cert.pem",
				KeyPath:  "/etc/key.pem",
			},
			wantKey:     "automatic_https",
			wantNoRedir: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := map[string]any{}
			applyAutomaticHTTPS(server, tt.tls)

			val, present := server["automatic_https"]
			if tt.wantAbsent {
				if present {
					t.Errorf("automatic_https should be absent, but got %v", val)
				}
				return
			}
			if !present {
				t.Fatal("automatic_https key not set")
			}
			m, ok := val.(map[string]any)
			if !ok {
				t.Fatalf("automatic_https is not a map: %T", val)
			}
			if tt.wantDisable {
				if m["disable"] != true {
					t.Errorf("automatic_https.disable = %v, want true", m["disable"])
				}
			}
			if tt.wantNoRedir {
				if m["disable_redirects"] != true {
					t.Errorf("automatic_https.disable_redirects = %v, want true", m["disable_redirects"])
				}
			}
		})
	}
}

// TestAssembleTopLevelConfig_StoragePath verifies the storage field is set at the
// top level when TLS.StoragePath is configured, and absent when it is not.
func TestAssembleTopLevelConfig_StoragePath(t *testing.T) {
	apps := map[string]any{"http": map[string]any{}}

	t.Run("without storage path", func(t *testing.T) {
		cfg := &ports.ProxyConfig{
			ListenAddr:   "127.0.0.1:8080",
			UpstreamAddr: "127.0.0.1:3000",
			TLS:          ports.TLSConfig{Enabled: false},
		}
		result, err := assembleTopLevelConfig(cfg, apps)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := result["storage"]; ok {
			t.Error("storage key should not be present when StoragePath is empty")
		}
	})

	t.Run("with storage path", func(t *testing.T) {
		cfg := &ports.ProxyConfig{
			ListenAddr:   "0.0.0.0:443",
			UpstreamAddr: "127.0.0.1:3000",
			TLS: ports.TLSConfig{
				Enabled:     true,
				Provider:    ports.TLSProviderSelfSigned,
				StoragePath: "/var/lib/vibewarden/certs",
			},
		}
		result, err := assembleTopLevelConfig(cfg, apps)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		storage, ok := result["storage"].(map[string]any)
		if !ok {
			t.Fatal("storage key not found or not a map")
		}
		if storage["module"] != "file_system" {
			t.Errorf("storage.module = %v, want file_system", storage["module"])
		}
		if storage["root"] != "/var/lib/vibewarden/certs" {
			t.Errorf("storage.root = %v, want /var/lib/vibewarden/certs", storage["root"])
		}
	})
}

// TestApplyServerTimeouts verifies that timeout fields are set when non-zero and
// absent when zero.
func TestApplyServerTimeouts(t *testing.T) {
	t.Run("zero values — no fields set", func(t *testing.T) {
		server := map[string]any{}
		applyServerTimeouts(server, ports.ServerTimeoutsConfig{})
		if _, ok := server["read_timeout"]; ok {
			t.Error("read_timeout should not be set for zero value")
		}
		if _, ok := server["write_timeout"]; ok {
			t.Error("write_timeout should not be set for zero value")
		}
		if _, ok := server["idle_timeout"]; ok {
			t.Error("idle_timeout should not be set for zero value")
		}
	})

	t.Run("non-zero values — all fields set", func(t *testing.T) {
		server := map[string]any{}
		cfg := ports.ServerTimeoutsConfig{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		applyServerTimeouts(server, cfg)
		if server["read_timeout"] != int64(5*time.Second) {
			t.Errorf("read_timeout = %v, want %v", server["read_timeout"], int64(5*time.Second))
		}
		if server["write_timeout"] != int64(10*time.Second) {
			t.Errorf("write_timeout = %v, want %v", server["write_timeout"], int64(10*time.Second))
		}
		if server["idle_timeout"] != int64(60*time.Second) {
			t.Errorf("idle_timeout = %v, want %v", server["idle_timeout"], int64(60*time.Second))
		}
	})
}
