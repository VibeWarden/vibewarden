package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestExplainConfig(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		contains []string
	}{
		{
			name: "minimal config shows defaults",
			yaml: `
server:
  port: 8080
upstream:
  port: 3000
tls:
  provider: self-signed
log:
  level: info
  format: json
`,
			contains: []string{
				"port 8080",
				"port 3000",
			},
		},
		{
			name: "tls enabled shows provider and domain",
			yaml: `
server:
  port: 443
upstream:
  port: 3000
tls:
  enabled: true
  provider: letsencrypt
  domain: example.com
log:
  level: info
  format: json
`,
			contains: []string{
				"letsencrypt",
				"example.com",
			},
		},
		{
			name: "rate limiting enabled shows rate",
			yaml: `
server:
  port: 8080
upstream:
  port: 3000
tls:
  provider: self-signed
log:
  level: info
  format: json
rate_limit:
  enabled: true
  per_ip:
    requests_per_second: 50
    burst: 100
`,
			contains: []string{
				"50 requests/second per IP",
				"burst up to 100",
			},
		},
		{
			name: "cors enabled shows origins",
			yaml: `
server:
  port: 8080
upstream:
  port: 3000
tls:
  provider: self-signed
log:
  level: info
  format: json
cors:
  enabled: true
  allowed_origins:
    - https://example.com
    - https://app.example.com
`,
			contains: []string{
				"CORS:",
				"https://example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "vibewarden.yaml")
			if err := os.WriteFile(cfgPath, []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("writing temp config: %v", err)
			}

			args, _ := json.Marshal(map[string]string{"path": cfgPath})
			items, err := handleExplain(context.Background(), args)
			if err != nil {
				t.Fatalf("handleExplain returned error: %v", err)
			}
			if len(items) == 0 {
				t.Fatal("expected at least one content item")
			}
			got := items[0].Text
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("output does not contain %q\ngot:\n%s", want, got)
				}
			}
		})
	}
}

func TestHandleValidate(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantContain string
	}{
		{
			name: "valid config",
			yaml: `
server:
  port: 8080
upstream:
  port: 3000
tls:
  provider: self-signed
log:
  level: info
  format: json
`,
			wantContain: "valid",
		},
		{
			name: "invalid port",
			yaml: `
server:
  port: 99999
upstream:
  port: 3000
tls:
  provider: self-signed
log:
  level: info
  format: json
`,
			wantContain: "server.port",
		},
		{
			name: "invalid tls provider",
			yaml: `
server:
  port: 8080
upstream:
  port: 3000
tls:
  provider: unknown-provider
log:
  level: info
  format: json
`,
			wantContain: "tls.provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "vibewarden.yaml")
			if err := os.WriteFile(cfgPath, []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("writing temp config: %v", err)
			}

			args, _ := json.Marshal(map[string]string{"path": cfgPath})
			items, err := handleValidate(context.Background(), args)
			if err != nil {
				t.Fatalf("handleValidate returned unexpected error: %v", err)
			}
			if len(items) == 0 {
				t.Fatal("expected at least one content item")
			}
			got := items[0].Text
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("output does not contain %q\ngot: %s", tt.wantContain, got)
			}
		})
	}
}

func TestHandleValidate_MissingFile(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "/nonexistent/path/vibewarden.yaml"})
	items, err := handleValidate(context.Background(), args)
	// A missing file is not a tool error — it returns a message item.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one content item")
	}
	if items[0].Text == "" {
		t.Error("expected a non-empty error message")
	}
}

func TestHandleExplain_MissingFile(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "/nonexistent/path/vibewarden.yaml"})
	_, err := handleExplain(context.Background(), args)
	if err == nil {
		t.Error("expected an error for a missing config file")
	}
}

func TestHandleStatus_InvalidArgs(t *testing.T) {
	_, err := handleStatus(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected an error for invalid JSON arguments")
	}
}

func TestHandleDoctor_InvalidArgs(t *testing.T) {
	_, err := handleDoctor(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected an error for invalid JSON arguments")
	}
}

func TestHandleValidate_InvalidArgs(t *testing.T) {
	_, err := handleValidate(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected an error for invalid JSON arguments")
	}
}

func TestHandleExplain_InvalidArgs(t *testing.T) {
	_, err := handleExplain(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected an error for invalid JSON arguments")
	}
}

func TestRegisterDefaultTools(t *testing.T) {
	srv := newTestServer()
	RegisterDefaultTools(srv)

	expectedTools := []string{
		"vibewarden_status",
		"vibewarden_doctor",
		"vibewarden_validate",
		"vibewarden_explain",
	}

	for _, name := range expectedTools {
		if _, ok := srv.handlers[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
	if len(srv.tools) != len(expectedTools) {
		t.Errorf("want %d tools, got %d", len(expectedTools), len(srv.tools))
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		wantErrs int
	}{
		{
			name: "valid config has no errors",
			cfg: &config.Config{
				Server:   config.ServerConfig{Port: 8080},
				Upstream: config.UpstreamConfig{Port: 3000},
				TLS:      config.TLSConfig{Provider: "self-signed"},
				Log:      config.LogConfig{Level: "info", Format: "json"},
			},
			wantErrs: 0,
		},
		{
			name: "invalid server port",
			cfg: &config.Config{
				Server:   config.ServerConfig{Port: 0},
				Upstream: config.UpstreamConfig{Port: 3000},
				TLS:      config.TLSConfig{Provider: "self-signed"},
				Log:      config.LogConfig{Level: "info", Format: "json"},
			},
			wantErrs: 1,
		},
		{
			name: "invalid upstream port",
			cfg: &config.Config{
				Server:   config.ServerConfig{Port: 8080},
				Upstream: config.UpstreamConfig{Port: 99999},
				TLS:      config.TLSConfig{Provider: "self-signed"},
				Log:      config.LogConfig{Level: "info", Format: "json"},
			},
			wantErrs: 1,
		},
		{
			name: "invalid tls provider",
			cfg: &config.Config{
				Server:   config.ServerConfig{Port: 8080},
				Upstream: config.UpstreamConfig{Port: 3000},
				TLS:      config.TLSConfig{Provider: "bad-provider"},
				Log:      config.LogConfig{Level: "info", Format: "json"},
			},
			wantErrs: 1,
		},
		{
			name: "invalid log level",
			cfg: &config.Config{
				Server:   config.ServerConfig{Port: 8080},
				Upstream: config.UpstreamConfig{Port: 3000},
				TLS:      config.TLSConfig{Provider: "self-signed"},
				Log:      config.LogConfig{Level: "verbose", Format: "json"},
			},
			wantErrs: 1,
		},
		{
			name: "invalid log format",
			cfg: &config.Config{
				Server:   config.ServerConfig{Port: 8080},
				Upstream: config.UpstreamConfig{Port: 3000},
				TLS:      config.TLSConfig{Provider: "self-signed"},
				Log:      config.LogConfig{Level: "info", Format: "yaml"},
			},
			wantErrs: 1,
		},
		{
			name: "letsencrypt without domain",
			cfg: &config.Config{
				Server:   config.ServerConfig{Port: 443},
				Upstream: config.UpstreamConfig{Port: 3000},
				TLS:      config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: ""},
				Log:      config.LogConfig{Level: "info", Format: "json"},
			},
			wantErrs: 1,
		},
		{
			name: "rate limit with zero rps",
			cfg: &config.Config{
				Server:    config.ServerConfig{Port: 8080},
				Upstream:  config.UpstreamConfig{Port: 3000},
				TLS:       config.TLSConfig{Provider: "self-signed"},
				Log:       config.LogConfig{Level: "info", Format: "json"},
				RateLimit: config.RateLimitConfig{Enabled: true, PerIP: config.RateLimitRuleConfig{RequestsPerSecond: 0, Burst: 10}},
			},
			wantErrs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateConfig(tt.cfg)
			if len(errs) != tt.wantErrs {
				t.Errorf("validateConfig() returned %d errors, want %d: %v", len(errs), tt.wantErrs, errs)
			}
		})
	}
}
