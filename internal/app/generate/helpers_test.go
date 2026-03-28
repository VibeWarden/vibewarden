package generate_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
)

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
