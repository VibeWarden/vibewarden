package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

// TestValidate_TLSExternal verifies that provider=external requires cert_path and key_path.
func TestValidate_TLSExternal(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
	}{
		{
			name: "external with both paths",
			cfg: config.Config{
				TLS: config.TLSConfig{
					Enabled:  true,
					Provider: "external",
					CertPath: "/etc/tls/cert.pem",
					KeyPath:  "/etc/tls/key.pem",
				},
			},
			wantErr: false,
		},
		{
			name: "external missing cert_path",
			cfg: config.Config{
				TLS: config.TLSConfig{
					Enabled:  true,
					Provider: "external",
					KeyPath:  "/etc/tls/key.pem",
				},
			},
			wantErr: true,
		},
		{
			name: "external missing key_path",
			cfg: config.Config{
				TLS: config.TLSConfig{
					Enabled:  true,
					Provider: "external",
					CertPath: "/etc/tls/cert.pem",
				},
			},
			wantErr: true,
		},
		{
			name: "external missing both paths",
			cfg: config.Config{
				TLS: config.TLSConfig{
					Enabled:  true,
					Provider: "external",
				},
			},
			wantErr: true,
		},
		{
			name: "letsencrypt does not require cert_path or key_path",
			cfg: config.Config{
				TLS: config.TLSConfig{
					Enabled:  true,
					Provider: "letsencrypt",
					Domain:   "example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "self-signed does not require cert_path or key_path",
			cfg: config.Config{
				TLS: config.TLSConfig{
					Enabled:  true,
					Provider: "self-signed",
				},
			},
			wantErr: false,
		},
		{
			name: "TLS disabled external without paths is valid",
			cfg: config.Config{
				TLS: config.TLSConfig{
					Enabled:  false,
					Provider: "external",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestLoad_TLSExternalValidation verifies that Load returns an error when
// provider=external is configured without cert_path and/or key_path.
func TestLoad_TLSExternalValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "external with both paths passes",
			yaml: `
tls:
  enabled: true
  provider: external
  cert_path: /etc/tls/cert.pem
  key_path: /etc/tls/key.pem
`,
			wantErr: false,
		},
		{
			name: "external missing cert_path fails",
			yaml: `
tls:
  enabled: true
  provider: external
  key_path: /etc/tls/key.pem
`,
			wantErr: true,
		},
		{
			name: "external missing key_path fails",
			yaml: `
tls:
  enabled: true
  provider: external
  cert_path: /etc/tls/cert.pem
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "vibewarden.yaml")
			if err := os.WriteFile(cfgFile, []byte(tt.yaml), 0600); err != nil {
				t.Fatalf("writing temp config file: %v", err)
			}
			_, err := config.Load(cfgFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"server.host", cfg.Server.Host, "127.0.0.1"},
		{"server.port", cfg.Server.Port, 8080},
		{"upstream.host", cfg.Upstream.Host, "127.0.0.1"},
		{"upstream.port", cfg.Upstream.Port, 3000},
		{"tls.enabled", cfg.TLS.Enabled, false},
		{"tls.provider", cfg.TLS.Provider, "self-signed"},
		{"kratos.public_url", cfg.Kratos.PublicURL, "http://127.0.0.1:4433"},
		{"kratos.admin_url", cfg.Kratos.AdminURL, "http://127.0.0.1:4434"},
		{"rate_limit.enabled", cfg.RateLimit.Enabled, true},
		{"rate_limit.per_ip.requests_per_second", cfg.RateLimit.PerIP.RequestsPerSecond, float64(10)},
		{"rate_limit.per_ip.burst", cfg.RateLimit.PerIP.Burst, 20},
		{"rate_limit.per_user.requests_per_second", cfg.RateLimit.PerUser.RequestsPerSecond, float64(100)},
		{"rate_limit.per_user.burst", cfg.RateLimit.PerUser.Burst, 200},
		{"rate_limit.trust_proxy_headers", cfg.RateLimit.TrustProxyHeaders, false},
		{"log.level", cfg.Log.Level, "info"},
		{"log.format", cfg.Log.Format, "json"},
		{"admin.enabled", cfg.Admin.Enabled, false},
		{"admin.token", cfg.Admin.Token, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("default %s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLoad_FromFile(t *testing.T) {
	content := `
server:
  host: "0.0.0.0"
  port: 9090
upstream:
  host: "localhost"
  port: 4000
log:
  level: "debug"
  format: "text"
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
		t.Fatalf("writing temp config file: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"server.host", cfg.Server.Host, "0.0.0.0"},
		{"server.port", cfg.Server.Port, 9090},
		{"upstream.host", cfg.Upstream.Host, "localhost"},
		{"upstream.port", cfg.Upstream.Port, 4000},
		{"log.level", cfg.Log.Level, "debug"},
		{"log.format", cfg.Log.Format, "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLoad_EnvVarOverride(t *testing.T) {
	t.Setenv("VIBEWARDEN_SERVER_PORT", "7777")
	t.Setenv("VIBEWARDEN_LOG_LEVEL", "warn")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Server.Port != 7777 {
		t.Errorf("server.port = %d, want 7777", cfg.Server.Port)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("log.level = %s, want warn", cfg.Log.Level)
	}
}

func TestLoad_InvalidFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/vibewarden.yaml")
	if err == nil {
		t.Error("Load() expected error for nonexistent explicit config path, got nil")
	}
}

func TestLoad_MetricsDefaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"metrics.enabled", cfg.Metrics.Enabled, true},
		{"metrics.path_patterns length", len(cfg.Metrics.PathPatterns), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("default %s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLoad_MetricsFromFile(t *testing.T) {
	content := `
metrics:
  enabled: false
  path_patterns:
    - "/users/:id"
    - "/api/v1/items/:item_id"
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
		t.Fatalf("writing temp config file: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Metrics.Enabled {
		t.Errorf("metrics.enabled = true, want false")
	}
	if len(cfg.Metrics.PathPatterns) != 2 {
		t.Fatalf("len(metrics.path_patterns) = %d, want 2", len(cfg.Metrics.PathPatterns))
	}
	if cfg.Metrics.PathPatterns[0] != "/users/:id" {
		t.Errorf("metrics.path_patterns[0] = %q, want %q", cfg.Metrics.PathPatterns[0], "/users/:id")
	}
	if cfg.Metrics.PathPatterns[1] != "/api/v1/items/:item_id" {
		t.Errorf("metrics.path_patterns[1] = %q, want %q", cfg.Metrics.PathPatterns[1], "/api/v1/items/:item_id")
	}
}

func TestLoad_MetricsEnvVarOverride(t *testing.T) {
	t.Setenv("VIBEWARDEN_METRICS_ENABLED", "false")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Metrics.Enabled {
		t.Errorf("metrics.enabled = true, want false after VIBEWARDEN_METRICS_ENABLED=false")
	}
}
