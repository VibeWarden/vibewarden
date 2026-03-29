package generate_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestNeedsObservability(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "observability enabled returns true",
			cfg:  &config.Config{Observability: config.ObservabilityConfig{Enabled: true}},
			want: true,
		},
		{
			name: "observability disabled returns false",
			cfg:  &config.Config{Observability: config.ObservabilityConfig{Enabled: false}},
			want: false,
		},
		{
			name: "zero value config returns false",
			cfg:  &config.Config{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generate.NeedsObservability(tt.cfg)
			if got != tt.want {
				t.Errorf("NeedsObservability() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeedsOpenBao(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "secrets enabled returns true",
			cfg:  &config.Config{Secrets: config.SecretsConfig{Enabled: true}},
			want: true,
		},
		{
			name: "secrets disabled returns false",
			cfg:  &config.Config{Secrets: config.SecretsConfig{Enabled: false}},
			want: false,
		},
		{
			name: "zero value config returns false",
			cfg:  &config.Config{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generate.NeedsOpenBao(tt.cfg)
			if got != tt.want {
				t.Errorf("NeedsOpenBao() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeedsRedis(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "store redis returns true",
			cfg:  &config.Config{RateLimit: config.RateLimitConfig{Store: "redis"}},
			want: true,
		},
		{
			name: "store memory returns false",
			cfg:  &config.Config{RateLimit: config.RateLimitConfig{Store: "memory"}},
			want: false,
		},
		{
			name: "store empty string returns false",
			cfg:  &config.Config{RateLimit: config.RateLimitConfig{Store: ""}},
			want: false,
		},
		{
			name: "zero value config returns false",
			cfg:  &config.Config{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generate.NeedsRedis(tt.cfg)
			if got != tt.want {
				t.Errorf("NeedsRedis() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeedsLocalKratosDB(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "auth disabled returns false",
			cfg: &config.Config{
				Auth: config.AuthConfig{Enabled: false, Mode: config.AuthModeKratos},
			},
			want: false,
		},
		{
			name: "kratos mode enabled no external url returns true",
			cfg: &config.Config{
				Auth:     config.AuthConfig{Enabled: true, Mode: config.AuthModeKratos},
				Database: config.DatabaseConfig{ExternalURL: ""},
			},
			want: true,
		},
		{
			name: "kratos mode enabled with external url returns false",
			cfg: &config.Config{
				Auth:     config.AuthConfig{Enabled: true, Mode: config.AuthModeKratos},
				Database: config.DatabaseConfig{ExternalURL: "postgres://user:pass@db.example.com:5432/kratos"},
			},
			want: false,
		},
		{
			name: "kratos.external true returns false",
			cfg: &config.Config{
				Auth:   config.AuthConfig{Enabled: true, Mode: config.AuthModeKratos},
				Kratos: config.KratosConfig{External: true},
			},
			want: false,
		},
		{
			name: "jwt mode returns false",
			cfg: &config.Config{
				Auth: config.AuthConfig{Enabled: true, Mode: config.AuthModeJWT},
			},
			want: false,
		},
		{
			name: "zero value config returns false",
			cfg:  &config.Config{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generate.NeedsLocalKratosDB(tt.cfg)
			if got != tt.want {
				t.Errorf("NeedsLocalKratosDB() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeedsSeedSecrets(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "secrets disabled returns false even with inject headers",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: false,
					Inject: config.SecretsInjectConfig{
						Headers: []config.SecretsHeaderInjection{
							{SecretPath: "app/api-key", SecretKey: "value", Header: "X-API-Key"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "secrets enabled with no inject entries returns false",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Inject:  config.SecretsInjectConfig{},
				},
			},
			want: false,
		},
		{
			name: "secrets enabled with header injection returns true",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Inject: config.SecretsInjectConfig{
						Headers: []config.SecretsHeaderInjection{
							{SecretPath: "app/api-key", SecretKey: "value", Header: "X-API-Key"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "secrets enabled with env injection returns true",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Inject: config.SecretsInjectConfig{
						Env: []config.SecretsEnvInjection{
							{SecretPath: "app/db-pass", SecretKey: "password", EnvVar: "DB_PASSWORD"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "secrets enabled with both headers and env returns true",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Inject: config.SecretsInjectConfig{
						Headers: []config.SecretsHeaderInjection{
							{SecretPath: "app/api-key", SecretKey: "value", Header: "X-API-Key"},
						},
						Env: []config.SecretsEnvInjection{
							{SecretPath: "app/db-pass", SecretKey: "password", EnvVar: "DB_PASSWORD"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "zero value config returns false",
			cfg:  &config.Config{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generate.NeedsSeedSecrets(tt.cfg)
			if got != tt.want {
				t.Errorf("NeedsSeedSecrets() = %v, want %v", got, tt.want)
			}
		})
	}
}
