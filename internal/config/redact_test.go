package config_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestRedact_SensitiveFieldsAreRedacted(t *testing.T) {
	cfg := &config.Config{
		Admin: config.AdminConfig{
			Enabled: true,
			Token:   "super-secret-token",
		},
		Database: config.DatabaseConfig{
			URL:         "postgres://user:pass@localhost/db",
			ExternalURL: "postgres://user:pass@external/db",
		},
		Kratos: config.KratosConfig{
			DSN: "postgres://kratos:pass@localhost/kratos",
		},
		RateLimit: config.RateLimitConfig{
			Redis: config.RateLimitRedisConfig{
				Password: "redis-password",
			},
		},
		Secrets: config.SecretsConfig{
			OpenBao: config.SecretsOpenBaoConfig{
				Auth: config.SecretsOpenBaoAuthConfig{
					Token:    "openbao-token",
					SecretID: "secret-id",
				},
			},
		},
	}

	redacted := config.Redact(cfg)

	// JSON marshalling of config struct uses Go field names (capitalised) since
	// there are no json struct tags. So we use capital-letter keys here.
	tests := []struct {
		path  string
		value any
	}{
		{"Admin.Token", "[REDACTED]"},
		{"Database.URL", "[REDACTED]"},
		{"Database.ExternalURL", "[REDACTED]"},
		{"Kratos.DSN", "[REDACTED]"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := deepGet(redacted, tt.path)
			if got != tt.value {
				t.Errorf("path %q = %v, want %v", tt.path, got, tt.value)
			}
		})
	}

	// Verify Admin.Enabled is NOT redacted (boolean, not sensitive name).
	adminMap, ok := redacted["Admin"].(map[string]any)
	if !ok {
		t.Fatal("Admin field is missing from redacted config")
	}
	if adminMap["Enabled"] != true {
		t.Errorf("Admin.Enabled = %v, want true (non-sensitive boolean fields should not be redacted)", adminMap["Enabled"])
	}
}

func TestRedact_EmptyStringsNotRedacted(t *testing.T) {
	cfg := &config.Config{
		Admin: config.AdminConfig{
			Enabled: false,
			Token:   "", // empty — should not become "[REDACTED]"
		},
	}

	redacted := config.Redact(cfg)

	adminMap, ok := redacted["Admin"].(map[string]any)
	if !ok {
		t.Fatal("Admin field is missing from redacted config")
	}
	// Empty string token should remain as empty string, not "[REDACTED]".
	if adminMap["Token"] == "[REDACTED]" {
		t.Error("empty token was redacted; only non-empty sensitive strings should be redacted")
	}
}

func TestRedact_NonSensitiveFieldsPreserved(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "127.0.0.1",
			Port: 8443,
		},
		Upstream: config.UpstreamConfig{
			Host: "localhost",
			Port: 3000,
		},
	}

	redacted := config.Redact(cfg)

	serverMap, ok := redacted["Server"].(map[string]any)
	if !ok {
		t.Fatal("Server field is missing from redacted config")
	}
	if serverMap["Host"] != "127.0.0.1" {
		t.Errorf("Server.Host = %v, want %q", serverMap["Host"], "127.0.0.1")
	}
}

func TestRedact_NilConfigPanicsNotExpected(t *testing.T) {
	// Calling Redact with a zero-value Config should not panic.
	cfg := &config.Config{}
	redacted := config.Redact(cfg)
	if redacted == nil {
		t.Error("Redact returned nil for zero-value Config")
	}
}

// deepGet navigates a dot-separated path through nested map[string]any values.
// Returns nil if any level is missing or not a map.
func deepGet(m map[string]any, path string) any {
	parts := splitPath(path)
	var cur any = map[string]any(m)
	for _, p := range parts {
		cm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = cm[p]
	}
	return cur
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i, c := range path {
		if c == '.' {
			parts = append(parts, path[start:i])
			start = i + 1
		}
	}
	parts = append(parts, path[start:])
	return parts
}
