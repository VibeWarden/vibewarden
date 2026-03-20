package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

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
		{"rate_limit.requests_per_second", cfg.RateLimit.RequestsPerSecond, 100},
		{"rate_limit.burst_size", cfg.RateLimit.BurstSize, 50},
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
