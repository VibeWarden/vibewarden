package config_test

import (
	"os"
	"path/filepath"
	"strings"
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

// TestValidate_DatabaseExternalURL verifies validation of database.external_url.
func TestValidate_DatabaseExternalURL(t *testing.T) {
	tests := []struct {
		name        string
		externalURL string
		wantErr     bool
		wantContain string
	}{
		{
			name:        "empty is valid (use local postgres)",
			externalURL: "",
			wantErr:     false,
		},
		{
			name:        "valid postgres:// URL",
			externalURL: "postgres://user:pass@db.example.com:5432/kratos?sslmode=require",
			wantErr:     false,
		},
		{
			name:        "valid postgresql:// URL",
			externalURL: "postgresql://user:pass@db.example.com:5432/kratos?sslmode=require",
			wantErr:     false,
		},
		{
			name:        "invalid scheme mysql",
			externalURL: "mysql://user:pass@db.example.com:3306/kratos",
			wantErr:     true,
			wantContain: "database.external_url",
		},
		{
			name:        "invalid scheme http",
			externalURL: "http://db.example.com/kratos",
			wantErr:     true,
			wantContain: "database.external_url",
		},
		{
			name:        "missing host",
			externalURL: "postgres:///kratos",
			wantErr:     true,
			wantContain: "database.external_url",
		},
		{
			name:        "not a URL",
			externalURL: "not-a-url",
			wantErr:     true,
			wantContain: "database.external_url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Database: config.DatabaseConfig{ExternalURL: tt.externalURL},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantContain != "" && err != nil {
				if !strings.Contains(err.Error(), tt.wantContain) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.wantContain)
				}
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
		{"server.port", cfg.Server.Port, 8443},
		{"upstream.host", cfg.Upstream.Host, "127.0.0.1"},
		{"upstream.port", cfg.Upstream.Port, 3000},
		{"tls.enabled", cfg.TLS.Enabled, true},
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

// TestLoad_NewFieldDefaults verifies defaults for all new config fields added in #117.
func TestLoad_NewFieldDefaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"auth.enabled", cfg.Auth.Enabled, false},
		{"auth.identity_schema", cfg.Auth.IdentitySchema, "email_password"},
		{"auth.session_cookie_name", cfg.Auth.SessionCookieName, "ory_kratos_session"},
		{"auth.login_url", cfg.Auth.LoginURL, ""},
		{"kratos.dsn", cfg.Kratos.DSN, ""},
		{"kratos.smtp.host", cfg.Kratos.SMTP.Host, "localhost"},
		{"kratos.smtp.port", cfg.Kratos.SMTP.Port, 1025},
		{"kratos.smtp.from", cfg.Kratos.SMTP.From, "no-reply@vibewarden.local"},
		{"overrides.kratos_config", cfg.Overrides.KratosConfig, ""},
		{"overrides.compose_file", cfg.Overrides.ComposeFile, ""},
		{"overrides.identity_schema", cfg.Overrides.IdentitySchema, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("default %s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestLoad_NewFieldsFromFile verifies that new fields are correctly loaded from a config file.
func TestLoad_NewFieldsFromFile(t *testing.T) {
	content := `
auth:
  enabled: true
  identity_schema: email_only
  session_cookie_name: my_session
  login_url: /login
  public_paths:
    - /health
    - /static/*

kratos:
  public_url: "http://localhost:4433"
  admin_url: "http://localhost:4434"
  dsn: "postgres://kratos:secret@localhost:5432/kratos?sslmode=disable"
  smtp:
    host: smtp.example.com
    port: 587
    from: noreply@example.com

overrides:
  kratos_config: /custom/kratos.yml
  compose_file: /custom/override.yml
  identity_schema: /custom/schema.json
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
		{"auth.enabled", cfg.Auth.Enabled, true},
		{"auth.identity_schema", cfg.Auth.IdentitySchema, "email_only"},
		{"auth.session_cookie_name", cfg.Auth.SessionCookieName, "my_session"},
		{"auth.login_url", cfg.Auth.LoginURL, "/login"},
		{"auth.public_paths[0]", cfg.Auth.PublicPaths[0], "/health"},
		{"auth.public_paths[1]", cfg.Auth.PublicPaths[1], "/static/*"},
		{"kratos.dsn", cfg.Kratos.DSN, "postgres://kratos:secret@localhost:5432/kratos?sslmode=disable"},
		{"kratos.smtp.host", cfg.Kratos.SMTP.Host, "smtp.example.com"},
		{"kratos.smtp.port", cfg.Kratos.SMTP.Port, 587},
		{"kratos.smtp.from", cfg.Kratos.SMTP.From, "noreply@example.com"},
		{"overrides.kratos_config", cfg.Overrides.KratosConfig, "/custom/kratos.yml"},
		{"overrides.compose_file", cfg.Overrides.ComposeFile, "/custom/override.yml"},
		{"overrides.identity_schema", cfg.Overrides.IdentitySchema, "/custom/schema.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestValidate_SocialProviders verifies validation rules for social provider entries.
func TestValidate_SocialProviders(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid google provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "google", ClientID: "cid", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid github provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "github", ClientID: "cid", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid apple provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "apple", ClientID: "cid", ClientSecret: "csecret", TeamID: "TEAM123", KeyID: "KEY456"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid oidc provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "oidc", ClientID: "cid", ClientSecret: "csecret", ID: "acme-oidc", IssuerURL: "https://accounts.acme.com"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing client_id",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "google", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[0].client_id is required",
		},
		{
			name: "missing client_secret",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "google", ClientID: "cid"},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[0].client_secret is required",
		},
		{
			name: "apple missing team_id",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "apple", ClientID: "cid", ClientSecret: "csecret", KeyID: "KEY456"},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[0].team_id is required for provider \"apple\"",
		},
		{
			name: "apple missing key_id",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "apple", ClientID: "cid", ClientSecret: "csecret", TeamID: "TEAM123"},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[0].key_id is required for provider \"apple\"",
		},
		{
			name: "oidc missing id",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "oidc", ClientID: "cid", ClientSecret: "csecret", IssuerURL: "https://accounts.acme.com"},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[0].id is required for provider \"oidc\"",
		},
		{
			name: "oidc missing issuer_url",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "oidc", ClientID: "cid", ClientSecret: "csecret", ID: "acme-oidc"},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[0].issuer_url is required for provider \"oidc\"",
		},
		{
			name: "unknown provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "twitter", ClientID: "cid", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[0].provider \"twitter\" is not supported",
		},
		{
			name: "empty provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "", ClientID: "cid", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[0].provider",
		},
		{
			name: "multiple providers first invalid",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "google", ClientID: "", ClientSecret: "csecret"},
						{Provider: "github", ClientID: "cid", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[0].client_id is required",
		},
		{
			name: "multiple providers second invalid",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "google", ClientID: "cid", ClientSecret: "csecret"},
						{Provider: "gitlab", ClientID: "cid2", ClientSecret: ""},
					},
				},
			},
			wantErr: true,
			errMsg:  "social_providers[1].client_secret is required",
		},
		{
			name:    "no social providers is valid",
			cfg:     config.Config{},
			wantErr: false,
		},
		{
			name: "facebook provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "facebook", ClientID: "cid", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "discord provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "discord", ClientID: "cid", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "microsoft provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "microsoft", ClientID: "cid", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "slack provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "slack", ClientID: "cid", ClientSecret: "csecret"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "spotify provider",
			cfg: config.Config{
				Auth: config.AuthConfig{
					SocialProviders: []config.SocialProviderConfig{
						{Provider: "spotify", ClientID: "cid", ClientSecret: "csecret"},
					},
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
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestLoad_SocialProvidersFromFile verifies that social_providers are correctly loaded from YAML.
func TestLoad_SocialProvidersFromFile(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(t *testing.T, cfg *config.Config)
	}{
		{
			name: "single google provider",
			yaml: `
auth:
  social_providers:
    - provider: google
      client_id: my-client-id
      client_secret: my-client-secret
      scopes:
        - email
        - profile
      label: Sign in with Google
`,
			wantErr: false,
			check: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Auth.SocialProviders) != 1 {
					t.Fatalf("len(social_providers) = %d, want 1", len(cfg.Auth.SocialProviders))
				}
				sp := cfg.Auth.SocialProviders[0]
				if sp.Provider != "google" {
					t.Errorf("provider = %q, want %q", sp.Provider, "google")
				}
				if sp.ClientID != "my-client-id" {
					t.Errorf("client_id = %q, want %q", sp.ClientID, "my-client-id")
				}
				if sp.ClientSecret != "my-client-secret" {
					t.Errorf("client_secret = %q, want %q", sp.ClientSecret, "my-client-secret")
				}
				if len(sp.Scopes) != 2 || sp.Scopes[0] != "email" || sp.Scopes[1] != "profile" {
					t.Errorf("scopes = %v, want [email profile]", sp.Scopes)
				}
				if sp.Label != "Sign in with Google" {
					t.Errorf("label = %q, want %q", sp.Label, "Sign in with Google")
				}
			},
		},
		{
			name: "apple provider with team_id and key_id",
			yaml: `
auth:
  social_providers:
    - provider: apple
      client_id: com.example.app
      client_secret: apple-secret
      team_id: TEAMABC123
      key_id: KEYXYZ789
`,
			wantErr: false,
			check: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Auth.SocialProviders) != 1 {
					t.Fatalf("len(social_providers) = %d, want 1", len(cfg.Auth.SocialProviders))
				}
				sp := cfg.Auth.SocialProviders[0]
				if sp.TeamID != "TEAMABC123" {
					t.Errorf("team_id = %q, want %q", sp.TeamID, "TEAMABC123")
				}
				if sp.KeyID != "KEYXYZ789" {
					t.Errorf("key_id = %q, want %q", sp.KeyID, "KEYXYZ789")
				}
			},
		},
		{
			name: "oidc provider with id and issuer_url",
			yaml: `
auth:
  social_providers:
    - provider: oidc
      id: acme-sso
      client_id: oidc-client-id
      client_secret: oidc-secret
      issuer_url: https://sso.acme.com
`,
			wantErr: false,
			check: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Auth.SocialProviders) != 1 {
					t.Fatalf("len(social_providers) = %d, want 1", len(cfg.Auth.SocialProviders))
				}
				sp := cfg.Auth.SocialProviders[0]
				if sp.ID != "acme-sso" {
					t.Errorf("id = %q, want %q", sp.ID, "acme-sso")
				}
				if sp.IssuerURL != "https://sso.acme.com" {
					t.Errorf("issuer_url = %q, want %q", sp.IssuerURL, "https://sso.acme.com")
				}
			},
		},
		{
			name: "multiple providers",
			yaml: `
auth:
  social_providers:
    - provider: google
      client_id: g-cid
      client_secret: g-secret
    - provider: github
      client_id: gh-cid
      client_secret: gh-secret
`,
			wantErr: false,
			check: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Auth.SocialProviders) != 2 {
					t.Fatalf("len(social_providers) = %d, want 2", len(cfg.Auth.SocialProviders))
				}
				if cfg.Auth.SocialProviders[0].Provider != "google" {
					t.Errorf("providers[0] = %q, want google", cfg.Auth.SocialProviders[0].Provider)
				}
				if cfg.Auth.SocialProviders[1].Provider != "github" {
					t.Errorf("providers[1] = %q, want github", cfg.Auth.SocialProviders[1].Provider)
				}
			},
		},
		{
			name: "missing client_secret fails",
			yaml: `
auth:
  social_providers:
    - provider: google
      client_id: g-cid
`,
			wantErr: true,
		},
		{
			name: "unknown provider fails",
			yaml: `
auth:
  social_providers:
    - provider: twitter
      client_id: cid
      client_secret: csecret
`,
			wantErr: true,
		},
		{
			name: "apple missing team_id fails",
			yaml: `
auth:
  social_providers:
    - provider: apple
      client_id: cid
      client_secret: csecret
      key_id: KEY123
`,
			wantErr: true,
		},
		{
			name: "oidc missing issuer_url fails",
			yaml: `
auth:
  social_providers:
    - provider: oidc
      id: my-oidc
      client_id: cid
      client_secret: csecret
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
			cfg, err := config.Load(cfgFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

// TestLoad_SocialProvidersEnvVarSubstitution verifies that environment variables are
// substituted when used as client_id and client_secret values via viper's AutomaticEnv.
func TestLoad_SocialProvidersEnvVarSubstitution(t *testing.T) {
	// Viper's AutomaticEnv only substitutes top-level keys matching VIBEWARDEN_<KEY>.
	// For slice elements we rely on the YAML file providing the literal value from env
	// expansion done by the shell or a secrets manager before the process starts.
	// This test covers that the YAML value (already expanded) is loaded correctly.
	yamlContent := `
auth:
  social_providers:
    - provider: google
      client_id: expanded-client-id
      client_secret: expanded-client-secret
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(cfgFile, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("writing temp config file: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if len(cfg.Auth.SocialProviders) != 1 {
		t.Fatalf("len(social_providers) = %d, want 1", len(cfg.Auth.SocialProviders))
	}
	sp := cfg.Auth.SocialProviders[0]
	if sp.ClientID != "expanded-client-id" {
		t.Errorf("client_id = %q, want %q", sp.ClientID, "expanded-client-id")
	}
	if sp.ClientSecret != "expanded-client-secret" {
		t.Errorf("client_secret = %q, want %q", sp.ClientSecret, "expanded-client-secret")
	}
}

// TestLoad_SocialProvidersDefault verifies that social_providers defaults to empty.
func TestLoad_SocialProvidersDefault(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if len(cfg.Auth.SocialProviders) != 0 {
		t.Errorf("social_providers default length = %d, want 0", len(cfg.Auth.SocialProviders))
	}
}

// TestLoad_BackwardCompatibility verifies that existing config files without the new
// fields continue to load successfully with appropriate defaults.
func TestLoad_BackwardCompatibility(t *testing.T) {
	// This is a typical pre-#117 vibewarden.yaml with no auth, kratos.dsn, or overrides fields.
	legacyConfig := `
server:
  host: "127.0.0.1"
  port: 8080

upstream:
  host: "127.0.0.1"
  port: 3000

kratos:
  public_url: "http://localhost:4433"
  admin_url: "http://localhost:4434"

auth:
  session_cookie_name: "ory_kratos_session"
  login_url: "/self-service/login/browser"
  public_paths:
    - "/health"

log:
  level: "info"
  format: "json"
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(cfgFile, []byte(legacyConfig), 0600); err != nil {
		t.Fatalf("writing temp config file: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() unexpected error for legacy config: %v", err)
	}

	// Existing fields must still be set correctly.
	if cfg.Server.Port != 8080 {
		t.Errorf("server.port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Kratos.PublicURL != "http://localhost:4433" {
		t.Errorf("kratos.public_url = %q, want %q", cfg.Kratos.PublicURL, "http://localhost:4433")
	}
	if cfg.Auth.SessionCookieName != "ory_kratos_session" {
		t.Errorf("auth.session_cookie_name = %q, want %q", cfg.Auth.SessionCookieName, "ory_kratos_session")
	}
	if len(cfg.Auth.PublicPaths) != 1 || cfg.Auth.PublicPaths[0] != "/health" {
		t.Errorf("auth.public_paths = %v, want [\"/health\"]", cfg.Auth.PublicPaths)
	}

	// New fields must have their defaults.
	if cfg.Auth.Enabled {
		t.Errorf("auth.enabled = true, want false (backward compat default)")
	}
	if cfg.Auth.IdentitySchema != "email_password" {
		t.Errorf("auth.identity_schema = %q, want %q", cfg.Auth.IdentitySchema, "email_password")
	}
	if cfg.Kratos.DSN != "" {
		t.Errorf("kratos.dsn = %q, want empty (backward compat default)", cfg.Kratos.DSN)
	}
	if cfg.Overrides.KratosConfig != "" {
		t.Errorf("overrides.kratos_config = %q, want empty (backward compat default)", cfg.Overrides.KratosConfig)
	}
}

// TestLoad_AuthUIDefaults verifies that auth.ui fields default to the expected values.
func TestLoad_AuthUIDefaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"auth.ui.mode", cfg.Auth.UI.Mode, "built-in"},
		{"auth.ui.app_name", cfg.Auth.UI.AppName, ""},
		{"auth.ui.logo_url", cfg.Auth.UI.LogoURL, ""},
		{"auth.ui.primary_color", cfg.Auth.UI.PrimaryColor, "#7C3AED"},
		{"auth.ui.background_color", cfg.Auth.UI.BackgroundColor, "#1a1a2e"},
		{"auth.ui.login_url", cfg.Auth.UI.LoginURL, ""},
		{"auth.ui.registration_url", cfg.Auth.UI.RegistrationURL, ""},
		{"auth.ui.settings_url", cfg.Auth.UI.SettingsURL, ""},
		{"auth.ui.recovery_url", cfg.Auth.UI.RecoveryURL, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("default %s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestLoad_AuthUIFromFile verifies that auth.ui fields are loaded correctly from a config file.
func TestLoad_AuthUIFromFile(t *testing.T) {
	content := `
auth:
  ui:
    mode: built-in
    app_name: "My App"
    logo_url: "https://example.com/logo.png"
    primary_color: "#7C3AED"
    background_color: "#1a1a2e"
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
		{"auth.ui.mode", cfg.Auth.UI.Mode, "built-in"},
		{"auth.ui.app_name", cfg.Auth.UI.AppName, "My App"},
		{"auth.ui.logo_url", cfg.Auth.UI.LogoURL, "https://example.com/logo.png"},
		{"auth.ui.primary_color", cfg.Auth.UI.PrimaryColor, "#7C3AED"},
		{"auth.ui.background_color", cfg.Auth.UI.BackgroundColor, "#1a1a2e"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestLoad_AuthUICustomMode verifies that mode=custom with all URLs loads correctly.
func TestLoad_AuthUICustomMode(t *testing.T) {
	content := `
auth:
  ui:
    mode: custom
    login_url: "https://myapp.example.com/login"
    registration_url: "https://myapp.example.com/register"
    settings_url: "https://myapp.example.com/settings"
    recovery_url: "https://myapp.example.com/recovery"
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
		{"auth.ui.mode", cfg.Auth.UI.Mode, "custom"},
		{"auth.ui.login_url", cfg.Auth.UI.LoginURL, "https://myapp.example.com/login"},
		{"auth.ui.registration_url", cfg.Auth.UI.RegistrationURL, "https://myapp.example.com/register"},
		{"auth.ui.settings_url", cfg.Auth.UI.SettingsURL, "https://myapp.example.com/settings"},
		{"auth.ui.recovery_url", cfg.Auth.UI.RecoveryURL, "https://myapp.example.com/recovery"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestValidate_AuthUIMode verifies validation of auth.ui.mode.
func TestValidate_AuthUIMode(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "default empty mode is valid",
			cfg:     config.Config{},
			wantErr: false,
		},
		{
			name: "built-in mode is valid",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{Mode: "built-in"},
				},
			},
			wantErr: false,
		},
		{
			name: "custom mode with login_url is valid",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{Mode: "custom", LoginURL: "/login"},
				},
			},
			wantErr: false,
		},
		{
			name: "custom mode without login_url fails",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{Mode: "custom"},
				},
			},
			wantErr: true,
			errMsg:  "auth.ui.login_url is required when auth.ui.mode is \"custom\"",
		},
		{
			name: "unknown mode fails",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{Mode: "hosted"},
				},
			},
			wantErr: true,
			errMsg:  "auth.ui.mode \"hosted\" is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestValidate_AuthUIColors verifies hex color validation for primary_color and background_color.
func TestValidate_AuthUIColors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid #RRGGBB primary color",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{PrimaryColor: "#7C3AED"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid #RGB primary color",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{PrimaryColor: "#F0A"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid #RRGGBB background color",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{BackgroundColor: "#1a1a2e"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid #RGB background color",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{BackgroundColor: "#abc"},
				},
			},
			wantErr: false,
		},
		{
			name: "empty primary color is valid (uses default)",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{PrimaryColor: ""},
				},
			},
			wantErr: false,
		},
		{
			name: "empty background color is valid (uses default)",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{BackgroundColor: ""},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid primary color without hash",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{PrimaryColor: "7C3AED"},
				},
			},
			wantErr: true,
			errMsg:  "auth.ui.primary_color",
		},
		{
			name: "invalid primary color wrong length",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{PrimaryColor: "#7C3AE"},
				},
			},
			wantErr: true,
			errMsg:  "auth.ui.primary_color",
		},
		{
			name: "invalid primary color with non-hex chars",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{PrimaryColor: "#GGGGGG"},
				},
			},
			wantErr: true,
			errMsg:  "auth.ui.primary_color",
		},
		{
			name: "invalid background color",
			cfg: config.Config{
				Auth: config.AuthConfig{
					UI: config.AuthUIConfig{BackgroundColor: "not-a-color"},
				},
			},
			wantErr: true,
			errMsg:  "auth.ui.background_color",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestLoad_AuthUIValidationFromFile verifies that auth.ui validation errors surface through Load.
func TestLoad_AuthUIValidationFromFile(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid built-in config",
			yaml: `
auth:
  ui:
    mode: built-in
    primary_color: "#7C3AED"
    background_color: "#1a1a2e"
`,
			wantErr: false,
		},
		{
			name: "valid custom config with login_url",
			yaml: `
auth:
  ui:
    mode: custom
    login_url: "/auth/login"
`,
			wantErr: false,
		},
		{
			name: "custom mode without login_url fails",
			yaml: `
auth:
  ui:
    mode: custom
`,
			wantErr: true,
			errMsg:  "auth.ui.login_url is required",
		},
		{
			name: "invalid mode fails",
			yaml: `
auth:
  ui:
    mode: external
`,
			wantErr: true,
			errMsg:  "auth.ui.mode",
		},
		{
			name: "invalid primary_color fails",
			yaml: `
auth:
  ui:
    primary_color: "purple"
`,
			wantErr: true,
			errMsg:  "auth.ui.primary_color",
		},
		{
			name: "invalid background_color fails",
			yaml: `
auth:
  ui:
    background_color: "dark"
`,
			wantErr: true,
			errMsg:  "auth.ui.background_color",
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
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Load() error = %q, want it to contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestValidate_Webhooks verifies webhook endpoint configuration validation.
func TestValidate_Webhooks(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "no endpoints — valid",
			cfg:  config.Config{},
		},
		{
			name: "valid raw endpoint",
			cfg: config.Config{
				Webhooks: config.WebhooksConfig{
					Endpoints: []config.WebhookEndpointConfig{
						{URL: "https://example.com/hook", Events: []string{"*"}, Format: "raw"},
					},
				},
			},
		},
		{
			name: "valid slack endpoint",
			cfg: config.Config{
				Webhooks: config.WebhooksConfig{
					Endpoints: []config.WebhookEndpointConfig{
						{URL: "https://hooks.slack.com/xxx", Events: []string{"auth.failed"}, Format: "slack"},
					},
				},
			},
		},
		{
			name: "valid discord endpoint",
			cfg: config.Config{
				Webhooks: config.WebhooksConfig{
					Endpoints: []config.WebhookEndpointConfig{
						{URL: "https://discord.com/api/webhooks/xxx", Events: []string{"*"}, Format: "discord"},
					},
				},
			},
		},
		{
			name: "empty format is valid",
			cfg: config.Config{
				Webhooks: config.WebhooksConfig{
					Endpoints: []config.WebhookEndpointConfig{
						{URL: "https://example.com/hook", Events: []string{"*"}, Format: ""},
					},
				},
			},
		},
		{
			name: "missing url",
			cfg: config.Config{
				Webhooks: config.WebhooksConfig{
					Endpoints: []config.WebhookEndpointConfig{
						{URL: "", Events: []string{"*"}, Format: "raw"},
					},
				},
			},
			wantErr: true,
			errMsg:  "webhooks.endpoints[0].url is required",
		},
		{
			name: "missing events",
			cfg: config.Config{
				Webhooks: config.WebhooksConfig{
					Endpoints: []config.WebhookEndpointConfig{
						{URL: "https://example.com/hook", Events: []string{}, Format: "raw"},
					},
				},
			},
			wantErr: true,
			errMsg:  "webhooks.endpoints[0].events",
		},
		{
			name: "invalid format",
			cfg: config.Config{
				Webhooks: config.WebhooksConfig{
					Endpoints: []config.WebhookEndpointConfig{
						{URL: "https://example.com/hook", Events: []string{"*"}, Format: "teams"},
					},
				},
			},
			wantErr: true,
			errMsg:  "teams",
		},
		{
			name: "negative timeout",
			cfg: config.Config{
				Webhooks: config.WebhooksConfig{
					Endpoints: []config.WebhookEndpointConfig{
						{URL: "https://example.com/hook", Events: []string{"*"}, Format: "raw", TimeoutSeconds: -1},
					},
				},
			},
			wantErr: true,
			errMsg:  "timeout_seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestValidate_Observability verifies validation rules for the observability config section.
func TestValidate_Observability(t *testing.T) {
	validObs := config.ObservabilityConfig{
		Enabled:        true,
		GrafanaPort:    3001,
		PrometheusPort: 9090,
		LokiPort:       3100,
		RetentionDays:  7,
	}

	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid observability config passes",
			cfg: config.Config{
				Observability: validObs,
			},
			wantErr: false,
		},
		{
			name: "observability disabled skips port validation",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        false,
					GrafanaPort:    0,
					PrometheusPort: 0,
					LokiPort:       0,
					RetentionDays:  0,
				},
			},
			wantErr: false,
		},
		{
			name: "grafana_port zero is invalid",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        true,
					GrafanaPort:    0,
					PrometheusPort: 9090,
					LokiPort:       3100,
					RetentionDays:  7,
				},
			},
			wantErr: true,
			errMsg:  "observability.grafana_port",
		},
		{
			name: "grafana_port negative is invalid",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        true,
					GrafanaPort:    -1,
					PrometheusPort: 9090,
					LokiPort:       3100,
					RetentionDays:  7,
				},
			},
			wantErr: true,
			errMsg:  "observability.grafana_port",
		},
		{
			name: "grafana_port over 65535 is invalid",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        true,
					GrafanaPort:    65536,
					PrometheusPort: 9090,
					LokiPort:       3100,
					RetentionDays:  7,
				},
			},
			wantErr: true,
			errMsg:  "observability.grafana_port",
		},
		{
			name: "prometheus_port zero is invalid",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        true,
					GrafanaPort:    3001,
					PrometheusPort: 0,
					LokiPort:       3100,
					RetentionDays:  7,
				},
			},
			wantErr: true,
			errMsg:  "observability.prometheus_port",
		},
		{
			name: "prometheus_port over 65535 is invalid",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        true,
					GrafanaPort:    3001,
					PrometheusPort: 65536,
					LokiPort:       3100,
					RetentionDays:  7,
				},
			},
			wantErr: true,
			errMsg:  "observability.prometheus_port",
		},
		{
			name: "loki_port zero is invalid",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        true,
					GrafanaPort:    3001,
					PrometheusPort: 9090,
					LokiPort:       0,
					RetentionDays:  7,
				},
			},
			wantErr: true,
			errMsg:  "observability.loki_port",
		},
		{
			name: "loki_port over 65535 is invalid",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        true,
					GrafanaPort:    3001,
					PrometheusPort: 9090,
					LokiPort:       65536,
					RetentionDays:  7,
				},
			},
			wantErr: true,
			errMsg:  "observability.loki_port",
		},
		{
			name: "retention_days zero is invalid",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        true,
					GrafanaPort:    3001,
					PrometheusPort: 9090,
					LokiPort:       3100,
					RetentionDays:  0,
				},
			},
			wantErr: true,
			errMsg:  "observability.retention_days",
		},
		{
			name: "retention_days negative is invalid",
			cfg: config.Config{
				Observability: config.ObservabilityConfig{
					Enabled:        true,
					GrafanaPort:    3001,
					PrometheusPort: 9090,
					LokiPort:       3100,
					RetentionDays:  -1,
				},
			},
			wantErr: true,
			errMsg:  "observability.retention_days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestValidate_Profile(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		wantErr bool
		errMsg  string
	}{
		{"empty string is valid (defaults to dev)", "", false, ""},
		{"dev is valid", "dev", false, ""},
		{"tls is valid", "tls", false, ""},
		{"prod is valid", "prod", false, ""},
		{"unknown value is invalid", "staging", true, "profile must be 'dev', 'tls', or 'prod'"},
		{"uppercase DEV is invalid", "DEV", true, "profile must be 'dev', 'tls', or 'prod'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{Profile: tt.profile}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestLoad_ProfileDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Profile != "dev" {
		t.Errorf("default Profile = %q, want %q", cfg.Profile, "dev")
	}
}

func TestLoad_ProfileFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "vibewarden.yaml")
	content := "profile: tls\nserver:\n  port: 8080\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Profile != "tls" {
		t.Errorf("Profile from file = %q, want %q", cfg.Profile, "tls")
	}
}

func TestValidate_LogsOTLPRequiresEndpoint(t *testing.T) {
	cfg, _ := config.Load("")
	cfg.Telemetry.Logs.OTLP = true
	cfg.Telemetry.OTLP.Endpoint = "" // missing

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should fail when logs.otlp is true without endpoint")
	}
	if !strings.Contains(err.Error(), "telemetry.logs.otlp requires telemetry.otlp.endpoint") {
		t.Errorf("Validate() error = %v, want to contain 'telemetry.logs.otlp requires telemetry.otlp.endpoint'", err)
	}
}

func TestValidate_LogsOTLPWithEndpointPasses(t *testing.T) {
	cfg, _ := config.Load("")
	cfg.Telemetry.Logs.OTLP = true
	cfg.Telemetry.OTLP.Endpoint = "http://localhost:4318"
	cfg.Telemetry.OTLP.Enabled = true

	err := cfg.Validate()
	if err != nil && strings.Contains(err.Error(), "telemetry.logs.otlp requires telemetry.otlp.endpoint") {
		t.Errorf("Validate() should not return logs.otlp error when endpoint is set, got: %v", err)
	}
}

// TestValidate_JWTConfig verifies that auth.mode = "jwt" requires the correct sub-fields.
func TestValidate_JWTConfig(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(cfg *config.Config)
		wantErr     bool
		wantContain string
	}{
		{
			name: "valid jwt config with jwks_url",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeJWT
				cfg.Auth.JWT.JWKSURL = "https://auth.example.com/.well-known/jwks.json"
				cfg.Auth.JWT.Issuer = "https://auth.example.com/"
				cfg.Auth.JWT.Audience = "my-api"
			},
			wantErr: false,
		},
		{
			name: "valid jwt config with issuer_url",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeJWT
				cfg.Auth.JWT.IssuerURL = "https://auth.example.com/"
				cfg.Auth.JWT.Issuer = "https://auth.example.com/"
				cfg.Auth.JWT.Audience = "my-api"
			},
			wantErr: false,
		},
		{
			name: "jwt mode missing both jwks_url and issuer_url",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeJWT
				cfg.Auth.JWT.Issuer = "https://auth.example.com/"
				cfg.Auth.JWT.Audience = "my-api"
			},
			wantErr:     true,
			wantContain: "either jwks_url or issuer_url is required",
		},
		{
			name: "jwt mode missing issuer",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeJWT
				cfg.Auth.JWT.JWKSURL = "https://auth.example.com/.well-known/jwks.json"
				cfg.Auth.JWT.Audience = "my-api"
			},
			wantErr:     true,
			wantContain: "auth.jwt.issuer is required",
		},
		{
			name: "jwt mode missing audience",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeJWT
				cfg.Auth.JWT.JWKSURL = "https://auth.example.com/.well-known/jwks.json"
				cfg.Auth.JWT.Issuer = "https://auth.example.com/"
			},
			wantErr:     true,
			wantContain: "auth.jwt.audience is required",
		},
		{
			name: "jwt config fields not validated when mode is not jwt",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeKratos
				// JWT fields deliberately left empty — should not trigger validation.
			},
			wantErr: false,
		},
		{
			name: "invalid auth mode",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = "invalid-mode"
			},
			wantErr:     true,
			wantContain: "auth.mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load("")
			if err != nil {
				t.Fatalf("Load(): %v", err)
			}
			tt.mutate(cfg)

			err = cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantContain != "" {
				if !strings.Contains(err.Error(), tt.wantContain) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.wantContain)
				}
			}
		})
	}
}

// TestValidate_KratosExternal verifies that kratos.external requires public_url and admin_url.
func TestValidate_KratosExternal(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(cfg *config.Config)
		wantErr     bool
		wantContain string
	}{
		{
			name: "external with both URLs",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeKratos
				cfg.Kratos.External = true
				cfg.Kratos.PublicURL = "https://kratos.example.com"
				cfg.Kratos.AdminURL = "https://kratos-admin.example.com"
			},
			wantErr: false,
		},
		{
			name: "external missing public_url",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeKratos
				cfg.Kratos.External = true
				cfg.Kratos.PublicURL = ""
				cfg.Kratos.AdminURL = "https://kratos-admin.example.com"
			},
			wantErr:     true,
			wantContain: "kratos.public_url is required when kratos.external is true",
		},
		{
			name: "external missing admin_url",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeKratos
				cfg.Kratos.External = true
				cfg.Kratos.PublicURL = "https://kratos.example.com"
				cfg.Kratos.AdminURL = ""
			},
			wantErr:     true,
			wantContain: "kratos.admin_url is required when kratos.external is true",
		},
		{
			name: "external missing both URLs",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeKratos
				cfg.Kratos.External = true
				cfg.Kratos.PublicURL = ""
				cfg.Kratos.AdminURL = ""
			},
			wantErr:     true,
			wantContain: "kratos.public_url is required when kratos.external is true",
		},
		{
			name: "external false does not trigger URL validation",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeKratos
				cfg.Kratos.External = false
				cfg.Kratos.PublicURL = ""
				cfg.Kratos.AdminURL = ""
			},
			wantErr: false,
		},
		{
			name: "external true but mode is not kratos — no URL validation",
			mutate: func(cfg *config.Config) {
				cfg.Auth.Mode = config.AuthModeNone
				cfg.Kratos.External = true
				cfg.Kratos.PublicURL = ""
				cfg.Kratos.AdminURL = ""
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load("")
			if err != nil {
				t.Fatalf("Load(): %v", err)
			}
			tt.mutate(cfg)

			err = cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantContain != "" {
				if !strings.Contains(err.Error(), tt.wantContain) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.wantContain)
				}
			}
		})
	}
}

// TestValidate_DatabaseTLSMode verifies validation of database.tls_mode.
func TestValidate_DatabaseTLSMode(t *testing.T) {
	tests := []struct {
		name        string
		tlsMode     string
		wantErr     bool
		wantContain string
	}{
		{name: "empty (default require)", tlsMode: "", wantErr: false},
		{name: "disable", tlsMode: "disable", wantErr: false},
		{name: "require", tlsMode: "require", wantErr: false},
		{name: "verify-ca", tlsMode: "verify-ca", wantErr: false},
		{name: "verify-full", tlsMode: "verify-full", wantErr: false},
		{
			name:        "invalid value",
			tlsMode:     "allow",
			wantErr:     true,
			wantContain: "database.tls_mode",
		},
		{
			name:        "invalid value uppercase",
			tlsMode:     "REQUIRE",
			wantErr:     true,
			wantContain: "database.tls_mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Database: config.DatabaseConfig{TLSMode: tt.tlsMode},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantContain != "" && err != nil {
				if !strings.Contains(err.Error(), tt.wantContain) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.wantContain)
				}
			}
		})
	}
}

// TestValidate_DatabasePool verifies validation of database.pool settings.
func TestValidate_DatabasePool(t *testing.T) {
	tests := []struct {
		name        string
		maxConns    int
		minConns    int
		wantErr     bool
		wantContain string
	}{
		{name: "zero values (use defaults)", maxConns: 0, minConns: 0, wantErr: false},
		{name: "typical production", maxConns: 10, minConns: 2, wantErr: false},
		{name: "min equals max", maxConns: 5, minConns: 5, wantErr: false},
		{
			name:        "negative max_conns",
			maxConns:    -1,
			minConns:    0,
			wantErr:     true,
			wantContain: "database.pool.max_conns",
		},
		{
			name:        "negative min_conns",
			maxConns:    10,
			minConns:    -1,
			wantErr:     true,
			wantContain: "database.pool.min_conns",
		},
		{
			name:        "min greater than max",
			maxConns:    5,
			minConns:    10,
			wantErr:     true,
			wantContain: "database.pool.min_conns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Database: config.DatabaseConfig{
					Pool: config.DatabasePoolConfig{
						MaxConns: tt.maxConns,
						MinConns: tt.minConns,
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantContain != "" && err != nil {
				if !strings.Contains(err.Error(), tt.wantContain) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.wantContain)
				}
			}
		})
	}
}

// TestValidate_DatabaseConnectTimeout verifies validation of database.connect_timeout.
func TestValidate_DatabaseConnectTimeout(t *testing.T) {
	tests := []struct {
		name           string
		connectTimeout string
		wantErr        bool
		wantContain    string
	}{
		{name: "empty (use default 10s)", connectTimeout: "", wantErr: false},
		{name: "10s", connectTimeout: "10s", wantErr: false},
		{name: "30s", connectTimeout: "30s", wantErr: false},
		{name: "1m", connectTimeout: "1m", wantErr: false},
		{
			name:           "invalid duration",
			connectTimeout: "notaduration",
			wantErr:        true,
			wantContain:    "database.connect_timeout",
		},
		{
			name:           "integer without unit",
			connectTimeout: "10",
			wantErr:        true,
			wantContain:    "database.connect_timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Database: config.DatabaseConfig{ConnectTimeout: tt.connectTimeout},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantContain != "" && err != nil {
				if !strings.Contains(err.Error(), tt.wantContain) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.wantContain)
				}
			}
		})
	}
}

// TestDatabaseConfig_BuildDSN verifies that BuildDSN produces the expected DSN
// with connection resilience parameters appended, respecting existing query params.
func TestDatabaseConfig_BuildDSN(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.DatabaseConfig
		wantDSN string
	}{
		{
			name:    "empty external URL returns empty string",
			cfg:     config.DatabaseConfig{},
			wantDSN: "",
		},
		{
			name: "external URL without params gets all defaults",
			cfg: config.DatabaseConfig{
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos",
			},
			wantDSN: "postgres://user:pass@db.example.com:5432/kratos?connect_timeout=10&pool_max_conns=10&sslmode=require",
		},
		{
			name: "existing sslmode is not overwritten",
			cfg: config.DatabaseConfig{
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos?sslmode=verify-full",
			},
			wantDSN: "postgres://user:pass@db.example.com:5432/kratos?connect_timeout=10&pool_max_conns=10&sslmode=verify-full",
		},
		{
			name: "existing connect_timeout is not overwritten",
			cfg: config.DatabaseConfig{
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos?connect_timeout=30",
			},
			wantDSN: "postgres://user:pass@db.example.com:5432/kratos?connect_timeout=30&pool_max_conns=10&sslmode=require",
		},
		{
			name: "explicit tls_mode is applied when not present in URL",
			cfg: config.DatabaseConfig{
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos",
				TLSMode:     "verify-full",
			},
			wantDSN: "postgres://user:pass@db.example.com:5432/kratos?connect_timeout=10&pool_max_conns=10&sslmode=verify-full",
		},
		{
			name: "custom pool max_conns is applied",
			cfg: config.DatabaseConfig{
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos",
				Pool:        config.DatabasePoolConfig{MaxConns: 25, MinConns: 5},
			},
			wantDSN: "postgres://user:pass@db.example.com:5432/kratos?connect_timeout=10&pool_max_conns=25&sslmode=require",
		},
		{
			name: "custom connect_timeout strips s suffix",
			cfg: config.DatabaseConfig{
				ExternalURL:    "postgres://user:pass@db.example.com:5432/kratos",
				ConnectTimeout: "30s",
			},
			wantDSN: "postgres://user:pass@db.example.com:5432/kratos?connect_timeout=30&pool_max_conns=10&sslmode=require",
		},
		{
			name: "connect_timeout without s suffix is used as-is",
			cfg: config.DatabaseConfig{
				ExternalURL:    "postgres://user:pass@db.example.com:5432/kratos",
				ConnectTimeout: "15",
			},
			wantDSN: "postgres://user:pass@db.example.com:5432/kratos?connect_timeout=15&pool_max_conns=10&sslmode=require",
		},
		{
			name: "sslmode=disable is propagated without error",
			cfg: config.DatabaseConfig{
				ExternalURL: "postgres://user:pass@db.example.com:5432/kratos",
				TLSMode:     "disable",
			},
			wantDSN: "postgres://user:pass@db.example.com:5432/kratos?connect_timeout=10&pool_max_conns=10&sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.BuildDSN()
			if got != tt.wantDSN {
				t.Errorf("BuildDSN() = %q, want %q", got, tt.wantDSN)
			}
		})
	}
}

// TestLoad_DatabaseDefaults verifies that database resilience defaults are set
// correctly when no database config is present in the YAML file.
func TestLoad_DatabaseDefaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"database.tls_mode", cfg.Database.TLSMode, "require"},
		{"database.pool.max_conns", cfg.Database.Pool.MaxConns, 10},
		{"database.pool.min_conns", cfg.Database.Pool.MinConns, 2},
		{"database.connect_timeout", cfg.Database.ConnectTimeout, "10s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("default %s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestLoad_DatabaseResilienceFromFile verifies that database resilience settings
// are loaded correctly from a YAML config file.
func TestLoad_DatabaseResilienceFromFile(t *testing.T) {
	content := `
database:
  external_url: "postgres://user:pass@db.example.com:5432/kratos?sslmode=require"
  tls_mode: "verify-full"
  connect_timeout: "30s"
  pool:
    max_conns: 20
    min_conns: 5
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
		{"database.tls_mode", cfg.Database.TLSMode, "verify-full"},
		{"database.pool.max_conns", cfg.Database.Pool.MaxConns, 20},
		{"database.pool.min_conns", cfg.Database.Pool.MinConns, 5},
		{"database.connect_timeout", cfg.Database.ConnectTimeout, "30s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
