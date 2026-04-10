package generate_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/generate"
)

func TestGeneratedCredentials_FieldsAccessible(t *testing.T) {
	creds := generate.GeneratedCredentials{
		PostgresPassword:     "pg-pass",
		KratosCookieSecret:   "cookie-secret",
		KratosCipherSecret:   "cipher-secret",
		GrafanaAdminPassword: "grafana-pass",
		OpenBaoDevRootToken:  "bao-token",
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"PostgresPassword", creds.PostgresPassword, "pg-pass"},
		{"KratosCookieSecret", creds.KratosCookieSecret, "cookie-secret"},
		{"KratosCipherSecret", creds.KratosCipherSecret, "cipher-secret"},
		{"GrafanaAdminPassword", creds.GrafanaAdminPassword, "grafana-pass"},
		{"OpenBaoDevRootToken", creds.OpenBaoDevRootToken, "bao-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestGeneratedCredentials_ZeroValue(t *testing.T) {
	var creds generate.GeneratedCredentials
	if creds.PostgresPassword != "" {
		t.Error("zero-value GeneratedCredentials should have empty PostgresPassword")
	}
}
