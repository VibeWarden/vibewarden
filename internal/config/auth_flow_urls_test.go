package config_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

// TestPublicBaseURL verifies the browser-facing base URL derived from the
// tls and server blocks.
func TestPublicBaseURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{
			name: "no tls falls back to http localhost",
			cfg: config.Config{
				Server: config.ServerConfig{Port: 8080},
			},
			want: "http://localhost:8080",
		},
		{
			name: "tls enabled without domain uses https localhost",
			cfg: config.Config{
				Server: config.ServerConfig{Port: 8443},
				TLS:    config.TLSConfig{Enabled: true},
			},
			want: "https://localhost:8443",
		},
		{
			name: "domain wins over port",
			cfg: config.Config{
				Server: config.ServerConfig{Port: 8080},
				TLS:    config.TLSConfig{Enabled: true, Domain: "app.example.com"},
			},
			want: "https://app.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.PublicBaseURL(); got != tt.want {
				t.Errorf("PublicBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAuthFlowURLs_BuiltInMode verifies that the default (and explicit)
// built-in UI mode points every flow at a /_vibewarden/ path the sidecar
// actually serves.
//
// Regression test for #1516: the flows previously pointed at /auth/*, which
// the sidecar does not serve, so any Kratos-initiated redirect fell through
// to the upstream app and the flow broke silently.
func TestAuthFlowURLs_BuiltInMode(t *testing.T) {
	modes := []struct {
		name string
		mode string
	}{
		{"empty mode defaults to built-in", ""},
		{"explicit built-in", config.AuthUIModeBuiltIn},
	}

	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			cfg := config.Config{
				Server: config.ServerConfig{Port: 8080},
				Auth:   config.AuthConfig{UI: config.AuthUIConfig{Mode: m.mode}},
			}

			got := cfg.AuthFlowURLs()
			want := config.AuthFlowURLs{
				Login:        "http://localhost:8080/_vibewarden/login",
				Registration: "http://localhost:8080/_vibewarden/registration",
				Recovery:     "http://localhost:8080/_vibewarden/recovery",
				Verification: "http://localhost:8080/_vibewarden/verification",
				Settings:     "http://localhost:8080/_vibewarden/settings",
				// No error page exists; login is the recoverable landing.
				Error:        "http://localhost:8080/_vibewarden/login",
				LogoutReturn: "http://localhost:8080/_vibewarden/login",
			}
			if got != want {
				t.Errorf("AuthFlowURLs() = %+v, want %+v", got, want)
			}
		})
	}
}

// TestAuthFlowURLs_BuiltInModeWithDomain verifies the built-in URLs are built
// on the public domain when TLS is configured.
func TestAuthFlowURLs_BuiltInModeWithDomain(t *testing.T) {
	cfg := config.Config{
		Server: config.ServerConfig{Port: 8080},
		TLS:    config.TLSConfig{Enabled: true, Domain: "app.example.com"},
	}

	if got, want := cfg.AuthFlowURLs().Login, "https://app.example.com/_vibewarden/login"; got != want {
		t.Errorf("AuthFlowURLs().Login = %q, want %q", got, want)
	}
}

// TestAuthFlowURLs_CustomMode verifies that custom mode uses the operator's
// auth.ui URLs, resolving relative paths against the public base URL and
// falling back to login_url for every flow that has no URL of its own.
func TestAuthFlowURLs_CustomMode(t *testing.T) {
	tests := []struct {
		name string
		ui   config.AuthUIConfig
		want config.AuthFlowURLs
	}{
		{
			name: "all urls absolute",
			ui: config.AuthUIConfig{
				Mode:            config.AuthUIModeCustom,
				LoginURL:        "https://app.example.com/login",
				RegistrationURL: "https://app.example.com/register",
				RecoveryURL:     "https://app.example.com/recover",
				SettingsURL:     "https://app.example.com/account",
			},
			want: config.AuthFlowURLs{
				Login:        "https://app.example.com/login",
				Registration: "https://app.example.com/register",
				Recovery:     "https://app.example.com/recover",
				Verification: "https://app.example.com/login",
				Settings:     "https://app.example.com/account",
				Error:        "https://app.example.com/login",
				LogoutReturn: "https://app.example.com/login",
			},
		},
		{
			name: "relative paths resolve against the public base url",
			ui: config.AuthUIConfig{
				Mode:            config.AuthUIModeCustom,
				LoginURL:        "/auth/login",
				RegistrationURL: "auth/register",
			},
			want: config.AuthFlowURLs{
				Login:        "http://localhost:8080/auth/login",
				Registration: "http://localhost:8080/auth/register",
				Recovery:     "http://localhost:8080/auth/login",
				Verification: "http://localhost:8080/auth/login",
				Settings:     "http://localhost:8080/auth/login",
				Error:        "http://localhost:8080/auth/login",
				LogoutReturn: "http://localhost:8080/auth/login",
			},
		},
		{
			name: "only login_url set falls back everywhere",
			ui: config.AuthUIConfig{
				Mode:     config.AuthUIModeCustom,
				LoginURL: "https://app.example.com/signin",
			},
			want: config.AuthFlowURLs{
				Login:        "https://app.example.com/signin",
				Registration: "https://app.example.com/signin",
				Recovery:     "https://app.example.com/signin",
				Verification: "https://app.example.com/signin",
				Settings:     "https://app.example.com/signin",
				Error:        "https://app.example.com/signin",
				LogoutReturn: "https://app.example.com/signin",
			},
		},
		{
			name: "whitespace-only url is treated as unset",
			ui: config.AuthUIConfig{
				Mode:            config.AuthUIModeCustom,
				LoginURL:        "/signin",
				RegistrationURL: "   ",
			},
			want: config.AuthFlowURLs{
				Login:        "http://localhost:8080/signin",
				Registration: "http://localhost:8080/signin",
				Recovery:     "http://localhost:8080/signin",
				Verification: "http://localhost:8080/signin",
				Settings:     "http://localhost:8080/signin",
				Error:        "http://localhost:8080/signin",
				LogoutReturn: "http://localhost:8080/signin",
			},
		},
		{
			name: "uppercase scheme is still absolute",
			ui: config.AuthUIConfig{
				Mode:     config.AuthUIModeCustom,
				LoginURL: "HTTPS://app.example.com/signin",
			},
			want: config.AuthFlowURLs{
				Login:        "HTTPS://app.example.com/signin",
				Registration: "HTTPS://app.example.com/signin",
				Recovery:     "HTTPS://app.example.com/signin",
				Verification: "HTTPS://app.example.com/signin",
				Settings:     "HTTPS://app.example.com/signin",
				Error:        "HTTPS://app.example.com/signin",
				LogoutReturn: "HTTPS://app.example.com/signin",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Server: config.ServerConfig{Port: 8080},
				Auth:   config.AuthConfig{UI: tt.ui},
			}
			if got := cfg.AuthFlowURLs(); got != tt.want {
				t.Errorf("AuthFlowURLs() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
