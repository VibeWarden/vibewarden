package deploy

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestResolveProdConfig(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		project    string
		multiSite  bool
		wantHost   string
		wantMutate bool // if true, verify original cfg is not modified
	}{
		{
			name: "multi-site: 0.0.0.0 rewritten to container name",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
			},
			project:   "blog",
			multiSite: true,
			wantHost:  "vibewarden-blog-app",
		},
		{
			name: "multi-site: 127.0.0.1 rewritten to container name",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
			},
			project:   "api",
			multiSite: true,
			wantHost:  "vibewarden-api-app",
		},
		{
			name: "multi-site: localhost rewritten to container name",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "localhost", Port: 8080},
			},
			project:   "shop",
			multiSite: true,
			wantHost:  "vibewarden-shop-app",
		},
		{
			name: "multi-site: custom host preserved",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "my-backend", Port: 3000},
			},
			project:   "blog",
			multiSite: true,
			wantHost:  "my-backend",
		},
		{
			name: "multi-site: empty host preserved",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "", Port: 3000},
			},
			project:   "blog",
			multiSite: true,
			wantHost:  "",
		},
		{
			name: "single-site with image: 0.0.0.0 rewritten to app",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
				App:      config.AppConfig{Image: "myapp:latest"},
			},
			project:   "myproject",
			multiSite: false,
			wantHost:  "app",
		},
		{
			name: "single-site with build: localhost rewritten to app",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
				App:      config.AppConfig{Build: "."},
			},
			project:   "myproject",
			multiSite: false,
			wantHost:  "app",
		},
		{
			name: "single-site without container: localhost NOT rewritten",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
			},
			project:   "myproject",
			multiSite: false,
			wantHost:  "localhost",
		},
		{
			name: "single-site: custom host preserved",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "my-custom-host", Port: 3000},
				App:      config.AppConfig{Image: "myapp:latest"},
			},
			project:   "myproject",
			multiSite: false,
			wantHost:  "my-custom-host",
		},
		{
			name: "does not mutate original config",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
			},
			project:    "blog",
			multiSite:  true,
			wantHost:   "vibewarden-blog-app",
			wantMutate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalHost := tt.cfg.Upstream.Host

			got := ResolveProdConfig(tt.cfg, tt.project, tt.multiSite)

			if got.Upstream.Host != tt.wantHost {
				t.Errorf("ResolveProdConfig().Upstream.Host = %q, want %q", got.Upstream.Host, tt.wantHost)
			}

			if tt.wantMutate && tt.cfg.Upstream.Host != originalHost {
				t.Errorf("ResolveProdConfig mutated original config: Host = %q, was %q", tt.cfg.Upstream.Host, originalHost)
			}

			// Verify port is preserved.
			if got.Upstream.Port != tt.cfg.Upstream.Port {
				t.Errorf("ResolveProdConfig().Upstream.Port = %d, want %d", got.Upstream.Port, tt.cfg.Upstream.Port)
			}
		})
	}
}

func TestMergeConfigYAML(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		override string
		wantKeys []string // substrings that must appear in the output
	}{
		{
			name:     "override wins for scalar",
			base:     "server:\n  port: 8443\n",
			override: "server:\n  port: 443\n",
			wantKeys: []string{"port: 443"},
		},
		{
			name:     "base key preserved when missing from override",
			base:     "server:\n  port: 8443\nupstream:\n  host: localhost\n",
			override: "server:\n  port: 443\n",
			wantKeys: []string{"port: 443", "host: localhost"},
		},
		{
			name:     "nested merge preserves sibling keys",
			base:     "rate_limit:\n  enabled: true\n  burst: 20\n",
			override: "rate_limit:\n  burst: 50\n",
			wantKeys: []string{"enabled: true", "burst: 50"},
		},
		{
			name:     "empty override returns base",
			base:     "server:\n  port: 8443\n",
			override: "",
			wantKeys: []string{"port: 8443"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged, err := MergeConfigYAML([]byte(tt.base), []byte(tt.override))
			if err != nil {
				t.Fatalf("MergeConfigYAML() error = %v", err)
			}

			data, err := MarshalYAMLMap(merged)
			if err != nil {
				t.Fatalf("MarshalYAMLMap() error = %v", err)
			}

			s := string(data)
			for _, want := range tt.wantKeys {
				if !strings.Contains(s, want) {
					t.Errorf("expected %q in output, got:\n%s", want, s)
				}
			}
		})
	}
}

func TestResolveProdUpstream(t *testing.T) {
	tests := []struct {
		name      string
		m         map[string]any
		project   string
		multiSite bool
		wantHost  string
	}{
		{
			name: "multi-site: localhost rewritten to container name",
			m: map[string]any{
				"upstream": map[string]any{"host": "localhost", "port": 3000},
			},
			project:   "blog",
			multiSite: true,
			wantHost:  "vibewarden-blog-app",
		},
		{
			name: "multi-site: custom host preserved",
			m: map[string]any{
				"upstream": map[string]any{"host": "my-backend", "port": 3000},
			},
			project:   "blog",
			multiSite: true,
			wantHost:  "my-backend",
		},
		{
			name: "single-site with image: 0.0.0.0 rewritten to app",
			m: map[string]any{
				"upstream": map[string]any{"host": "0.0.0.0", "port": 3000},
				"app":      map[string]any{"image": "myapp:latest"},
			},
			project:   "proj",
			multiSite: false,
			wantHost:  "app",
		},
		{
			name: "single-site without container: localhost NOT rewritten",
			m: map[string]any{
				"upstream": map[string]any{"host": "localhost", "port": 3000},
			},
			project:   "proj",
			multiSite: false,
			wantHost:  "localhost",
		},
		{
			name:      "no upstream section: no panic",
			m:         map[string]any{"server": map[string]any{"port": 8443}},
			project:   "proj",
			multiSite: true,
			wantHost:  "", // no upstream, so no host to check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResolveProdUpstream(tt.m, tt.project, tt.multiSite)

			if tt.wantHost == "" {
				return // nothing to check
			}

			upstream, ok := tt.m["upstream"].(map[string]any)
			if !ok {
				t.Fatal("upstream section missing after resolve")
			}
			got, _ := upstream["host"].(string)
			if got != tt.wantHost {
				t.Errorf("upstream.host = %q, want %q", got, tt.wantHost)
			}
		})
	}
}

func TestPatchYAMLMap(t *testing.T) {
	tests := []struct {
		name  string
		m     map[string]any
		path  []string
		value any
		want  any
	}{
		{
			name:  "set existing nested key",
			m:     map[string]any{"upstream": map[string]any{"host": "localhost"}},
			path:  []string{"upstream", "host"},
			value: "app",
			want:  "app",
		},
		{
			name:  "create intermediate maps",
			m:     map[string]any{},
			path:  []string{"tls", "domain"},
			value: "example.com",
			want:  "example.com",
		},
		{
			name:  "empty path is no-op",
			m:     map[string]any{"key": "val"},
			path:  []string{},
			value: "ignored",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			PatchYAMLMap(tt.m, tt.path, tt.value)

			if tt.want == nil {
				return
			}

			// Traverse the map to find the value.
			var current any = tt.m
			for _, key := range tt.path {
				m, ok := current.(map[string]any)
				if !ok {
					t.Fatalf("expected map at key path, got %T", current)
				}
				current = m[key]
			}
			if current != tt.want {
				t.Errorf("value at path %v = %v, want %v", tt.path, current, tt.want)
			}
		})
	}
}

func TestMarshalYAMLMap_PreservesUnderscoreKeys(t *testing.T) {
	// This is the key test: verify that underscore field names survive
	// marshalling, unlike yaml.Marshal(Config{}) which would lose them.
	m := map[string]any{
		"rate_limit":       map[string]any{"enabled": true, "burst": 20},
		"security_headers": map[string]any{"enabled": true},
		"body_size":        map[string]any{"max_bytes": 1048576},
		"ip_filter":        map[string]any{"enabled": false},
	}

	data, err := MarshalYAMLMap(m)
	if err != nil {
		t.Fatalf("MarshalYAMLMap() error = %v", err)
	}

	s := string(data)
	for _, key := range []string{"rate_limit:", "security_headers:", "body_size:", "ip_filter:"} {
		if !strings.Contains(s, key) {
			t.Errorf("expected %q in output, got:\n%s", key, s)
		}
	}
}

// Verify that the old MarshalConfig still works but note the underscore issue.
// This test documents the known limitation for backwards compatibility.
func TestMarshalYAMLMap_RoundTrip(t *testing.T) {
	baseYAML := `server:
  port: 8443
upstream:
  host: "0.0.0.0"
  port: 3000
rate_limit:
  enabled: true
  burst: 20
security_headers:
  enabled: true
`
	merged, err := MergeConfigYAML([]byte(baseYAML), nil)
	if err != nil {
		t.Fatalf("MergeConfigYAML() error = %v", err)
	}

	data, err := MarshalYAMLMap(merged)
	if err != nil {
		t.Fatalf("MarshalYAMLMap() error = %v", err)
	}

	// Round-trip: unmarshal the output and verify key values survive.
	roundTrip, err := MergeConfigYAML(data, nil)
	if err != nil {
		t.Fatalf("round-trip MergeConfigYAML() error = %v", err)
	}

	// Check that the upstream.port survives the round-trip.
	upstream, ok := roundTrip["upstream"].(map[string]any)
	if !ok {
		t.Fatal("upstream section missing after round-trip")
	}
	if port, _ := upstream["port"].(int); port != 3000 {
		t.Errorf("upstream.port = %v, want 3000", upstream["port"])
	}

	// Check that rate_limit.enabled survives the round-trip.
	rl, ok := roundTrip["rate_limit"].(map[string]any)
	if !ok {
		t.Fatal("rate_limit section missing after round-trip")
	}
	if enabled, _ := rl["enabled"].(bool); !enabled {
		t.Errorf("rate_limit.enabled = %v, want true", rl["enabled"])
	}
}

func TestOverlayProdConfig(t *testing.T) {
	tests := []struct {
		name         string
		base         *config.Config
		prod         *config.Config
		wantPort     int
		wantProvider string
		wantDomain   string
		wantTLS      bool
		wantLogLevel string
		wantWAFMode  string
	}{
		{
			name: "prod overrides all fields",
			base: &config.Config{
				Server: config.ServerConfig{Port: 8443},
				TLS:    config.TLSConfig{Enabled: true, Provider: "self-signed"},
			},
			prod: &config.Config{
				Server: config.ServerConfig{Port: 443},
				TLS:    config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: "example.com"},
				Log:    config.LogConfig{Level: "warn"},
				WAF:    config.WAFConfig{Mode: "block"},
			},
			wantPort:     443,
			wantProvider: "letsencrypt",
			wantDomain:   "example.com",
			wantTLS:      true,
			wantLogLevel: "warn",
			wantWAFMode:  "block",
		},
		{
			name: "prod zero port keeps base port",
			base: &config.Config{
				Server: config.ServerConfig{Port: 8443},
				TLS:    config.TLSConfig{Provider: "self-signed"},
			},
			prod: &config.Config{
				Server: config.ServerConfig{Port: 0},
			},
			wantPort:     8443,
			wantProvider: "self-signed",
		},
		{
			name: "prod empty provider keeps base provider",
			base: &config.Config{
				TLS: config.TLSConfig{Provider: "self-signed"},
			},
			prod: &config.Config{
				TLS: config.TLSConfig{Provider: ""},
			},
			wantProvider: "self-signed",
		},
		{
			name: "prod WAF mode block overrides base",
			base: &config.Config{
				WAF: config.WAFConfig{Mode: "detect"},
			},
			prod: &config.Config{
				WAF: config.WAFConfig{Mode: "block"},
			},
			wantWAFMode: "block",
		},
		{
			name: "all zero prod returns base unchanged",
			base: &config.Config{
				Server: config.ServerConfig{Port: 8443},
				TLS:    config.TLSConfig{Enabled: true, Provider: "self-signed", Domain: "dev.local"},
				Log:    config.LogConfig{Level: "debug"},
				WAF:    config.WAFConfig{Mode: "detect"},
			},
			prod:         &config.Config{},
			wantPort:     8443,
			wantProvider: "self-signed",
			wantDomain:   "dev.local",
			wantTLS:      true,
			wantLogLevel: "debug",
			wantWAFMode:  "detect",
		},
		{
			name: "does not mutate base",
			base: &config.Config{
				Server: config.ServerConfig{Port: 8443},
			},
			prod: &config.Config{
				Server: config.ServerConfig{Port: 443},
			},
			wantPort: 443,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalPort := tt.base.Server.Port

			got := overlayProdConfig(tt.base, tt.prod)

			if tt.wantPort != 0 && got.Server.Port != tt.wantPort {
				t.Errorf("Server.Port = %d, want %d", got.Server.Port, tt.wantPort)
			}
			if tt.wantProvider != "" && got.TLS.Provider != tt.wantProvider {
				t.Errorf("TLS.Provider = %q, want %q", got.TLS.Provider, tt.wantProvider)
			}
			if tt.wantDomain != "" && got.TLS.Domain != tt.wantDomain {
				t.Errorf("TLS.Domain = %q, want %q", got.TLS.Domain, tt.wantDomain)
			}
			if tt.wantTLS && !got.TLS.Enabled {
				t.Error("TLS.Enabled = false, want true")
			}
			if tt.wantLogLevel != "" && got.Log.Level != tt.wantLogLevel {
				t.Errorf("Log.Level = %q, want %q", got.Log.Level, tt.wantLogLevel)
			}
			if tt.wantWAFMode != "" && got.WAF.Mode != tt.wantWAFMode {
				t.Errorf("WAF.Mode = %q, want %q", got.WAF.Mode, tt.wantWAFMode)
			}

			// Verify the original base is not mutated.
			if tt.name == "does not mutate base" && tt.base.Server.Port != originalPort {
				t.Errorf("base.Server.Port was mutated: got %d, want %d", tt.base.Server.Port, originalPort)
			}

			// Verify the result is a distinct pointer from the input.
			if got == tt.base {
				t.Error("overlayProdConfig returned the same pointer as base, want a copy")
			}
		})
	}
}
