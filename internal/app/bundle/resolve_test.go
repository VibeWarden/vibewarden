package bundle

import (
	"os"
	"path/filepath"
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

// TestResolveProdAppBuild covers the #1341 rule: a bundled production config
// must not carry app.build once app.image is known.
func TestResolveProdAppBuild(t *testing.T) {
	tests := []struct {
		name          string
		m             map[string]any
		wantBuildGone bool
		wantImage     string
	}{
		{
			name: "image set drops build",
			m: map[string]any{
				"app": map[string]any{
					"build": ".",
					"image": "ghcr.io/org/app:latest",
				},
			},
			wantBuildGone: true,
			wantImage:     "ghcr.io/org/app:latest",
		},
		{
			name: "image absent keeps build",
			m: map[string]any{
				"app": map[string]any{"build": "."},
			},
			wantBuildGone: false,
		},
		{
			name: "empty image keeps build",
			m: map[string]any{
				"app": map[string]any{"build": ".", "image": ""},
			},
			wantBuildGone: false,
		},
		{
			name: "whitespace-only image keeps build",
			m: map[string]any{
				"app": map[string]any{"build": ".", "image": "   "},
			},
			wantBuildGone: false,
		},
		{
			name: "image only is untouched",
			m: map[string]any{
				"app": map[string]any{"image": "ghcr.io/org/app:latest", "language": "go"},
			},
			wantBuildGone: true,
			wantImage:     "ghcr.io/org/app:latest",
		},
		{
			name:          "no app section is a no-op",
			m:             map[string]any{"server": map[string]any{"port": 443}},
			wantBuildGone: true,
		},
		{
			name:          "non-map app section is a no-op",
			m:             map[string]any{"app": "nonsense"},
			wantBuildGone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResolveProdAppBuild(tt.m)

			app, ok := tt.m["app"].(map[string]any)
			if !ok {
				return // no-op cases: nothing further to assert
			}

			if _, hasBuild := app["build"]; hasBuild == tt.wantBuildGone {
				t.Errorf("app.build present = %v, want %v", hasBuild, !tt.wantBuildGone)
			}
			if tt.wantImage != "" {
				if got, _ := app["image"].(string); got != tt.wantImage {
					t.Errorf("app.image = %q, want %q", got, tt.wantImage)
				}
			}
			// Sibling keys must survive.
			if tt.name == "image only is untouched" {
				if got, _ := app["language"].(string); got != "go" {
					t.Errorf("app.language = %q, want %q", got, "go")
				}
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

// TestLoadMergedConfig_PreservesAllProdFields is the regression guard for
// #1053 and ADR-082. The previous hand-written allow-list silently dropped
// fields such as tls.email and tls.acme_ca when they were only set in the
// production override. The YAML round-trip implementation now preserves every
// schema-valid field.
func TestLoadMergedConfig_PreservesAllProdFields(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		prod   string
		assert func(t *testing.T, cfg *config.Config)
	}{
		{
			name: "tls.email only in prod",
			base: "tls:\n  provider: self-signed\n",
			prod: "tls:\n  email: ops@example.com\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.TLS.Email != "ops@example.com" {
					t.Errorf("TLS.Email = %q, want %q", cfg.TLS.Email, "ops@example.com")
				}
				if cfg.TLS.Provider != "self-signed" {
					t.Errorf("TLS.Provider = %q, want %q", cfg.TLS.Provider, "self-signed")
				}
			},
		},
		{
			name: "tls.acme_ca only in prod",
			base: "tls:\n  provider: letsencrypt\n  domain: example.com\n",
			prod: "tls:\n  acme_ca: https://acme.zerossl.com/v2/DV90\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.TLS.ACMECA != "https://acme.zerossl.com/v2/DV90" {
					t.Errorf("TLS.ACMECA = %q, want %q", cfg.TLS.ACMECA, "https://acme.zerossl.com/v2/DV90")
				}
			},
		},
		{
			name: "tls.email + tls.acme_ca both applied",
			base: "tls:\n  provider: letsencrypt\n  domain: example.com\n",
			prod: "tls:\n  email: ops@example.com\n  acme_ca: https://acme.zerossl.com/v2/DV90\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.TLS.Email != "ops@example.com" {
					t.Errorf("TLS.Email = %q, want %q", cfg.TLS.Email, "ops@example.com")
				}
				if cfg.TLS.ACMECA != "https://acme.zerossl.com/v2/DV90" {
					t.Errorf("TLS.ACMECA = %q, want %q", cfg.TLS.ACMECA, "https://acme.zerossl.com/v2/DV90")
				}
			},
		},
		{
			name: "prod overrides base scalar",
			base: "tls:\n  provider: self-signed\n",
			prod: "tls:\n  provider: letsencrypt\n  domain: example.com\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.TLS.Provider != "letsencrypt" {
					t.Errorf("TLS.Provider = %q, want %q", cfg.TLS.Provider, "letsencrypt")
				}
			},
		},
		{
			name: "tls.cert_monitoring.enabled = false in prod",
			base: "tls:\n  provider: letsencrypt\n  domain: example.com\n",
			prod: "tls:\n  cert_monitoring:\n    enabled: false\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.TLS.CertMonitoring.Enabled {
					t.Error("TLS.CertMonitoring.Enabled = true, want false")
				}
			},
		},
		{
			name: "server.host only in prod keeps base port",
			base: "server:\n  port: 8443\n",
			prod: "server:\n  host: 0.0.0.0\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Server.Host != "0.0.0.0" {
					t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
				}
				if cfg.Server.Port != 8443 {
					t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8443)
				}
			},
		},
		{
			name: "rate_limit.enabled outside TLS section",
			base: "tls:\n  provider: self-signed\n",
			prod: "rate_limit:\n  enabled: true\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if !cfg.RateLimit.Enabled {
					t.Error("RateLimit.Enabled = false, want true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			basePath := filepath.Join(dir, "vibewarden.yaml")
			if err := os.WriteFile(basePath, []byte(tt.base), 0o600); err != nil {
				t.Fatalf("writing base: %v", err)
			}
			prodPath := filepath.Join(dir, "vibewarden.production.yaml")
			if err := os.WriteFile(prodPath, []byte(tt.prod), 0o600); err != nil {
				t.Fatalf("writing prod: %v", err)
			}

			cfg, err := LoadMergedConfig(basePath, prodPath)
			if err != nil {
				t.Fatalf("LoadMergedConfig() error = %v", err)
			}
			tt.assert(t, cfg)
		})
	}
}

// TestLoadMergedConfig_EnvVarWinsOverProd confirms the YAML round-trip path
// still honours VIBEWARDEN_* env-var precedence. Env vars must win over both
// the base file and the production override (viper applies env last).
func TestLoadMergedConfig_EnvVarWinsOverProd(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte("tls:\n  provider: letsencrypt\n  domain: example.com\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte("tls:\n  email: ops@example.com\n"), 0o600); err != nil {
		t.Fatalf("writing prod: %v", err)
	}

	t.Setenv("VIBEWARDEN_TLS_EMAIL", "env@example.com")

	cfg, err := LoadMergedConfig(basePath, prodPath)
	if err != nil {
		t.Fatalf("LoadMergedConfig() error = %v", err)
	}
	if cfg.TLS.Email != "env@example.com" {
		t.Errorf("TLS.Email = %q, want env override %q", cfg.TLS.Email, "env@example.com")
	}
}

// TestLoadMergedConfig_NoProdOverride ensures the function is a straight
// config.Load when prodConfigPath is empty (preserving the v0.15.0 contract
// for callers that pass no override).
func TestLoadMergedConfig_NoProdOverride(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	cfg, err := LoadMergedConfig(basePath, "")
	if err != nil {
		t.Fatalf("LoadMergedConfig() error = %v", err)
	}
	if cfg.Server.Port != 8443 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8443)
	}
}
