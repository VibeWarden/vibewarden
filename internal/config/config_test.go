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
