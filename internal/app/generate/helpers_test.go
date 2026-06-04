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
			name: "store redis with external URL returns false",
			cfg: &config.Config{
				RateLimit: config.RateLimitConfig{
					Store: "redis",
					Redis: config.RateLimitRedisConfig{URL: "redis://external.example.com:6379/0"},
				},
			},
			want: false,
		},
		{
			name: "store redis with rediss TLS URL returns false",
			cfg: &config.Config{
				RateLimit: config.RateLimitConfig{
					Store: "redis",
					Redis: config.RateLimitRedisConfig{URL: "rediss://user:pass@redis.example.com:6380/1"},
				},
			},
			want: false,
		},
		{
			name: "store redis with address only (no URL) returns true",
			cfg: &config.Config{
				RateLimit: config.RateLimitConfig{
					Store: "redis",
					Redis: config.RateLimitRedisConfig{Address: "localhost:6379"},
				},
			},
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

func TestNeedsOpenBaoConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "openbao store with prod profile returns true",
			cfg:  &config.Config{Profile: "prod", Secrets: config.SecretsConfig{Enabled: true, Store: "openbao"}},
			want: true,
		},
		{
			name: "openbao store with dev profile returns false",
			cfg:  &config.Config{Profile: "dev", Secrets: config.SecretsConfig{Enabled: true, Store: "openbao"}},
			want: false,
		},
		{
			name: "openbao store with empty profile returns false",
			cfg:  &config.Config{Profile: "", Secrets: config.SecretsConfig{Enabled: true, Store: "openbao"}},
			want: false,
		},
		{
			name: "builtin store with prod profile returns false — no openbao/config.hcl needed",
			cfg:  &config.Config{Profile: "prod", Secrets: config.SecretsConfig{Enabled: true, Store: "builtin"}},
			want: false,
		},
		{
			name: "empty store (defaults to builtin) with prod profile returns false",
			cfg:  &config.Config{Profile: "prod", Secrets: config.SecretsConfig{Enabled: true, Store: ""}},
			want: false,
		},
		{
			name: "secrets disabled with prod profile returns false",
			cfg:  &config.Config{Profile: "prod", Secrets: config.SecretsConfig{Enabled: false}},
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
			got := generate.NeedsOpenBaoConfig(tt.cfg)
			if got != tt.want {
				t.Errorf("NeedsOpenBaoConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeedsSeedUsers(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "kratos mode local with seed_demo_users true returns true",
			cfg: &config.Config{
				Auth:   config.AuthConfig{Mode: config.AuthModeKratos, SeedDemoUsers: true},
				Kratos: config.KratosConfig{External: false},
			},
			want: true,
		},
		{
			name: "kratos mode local without seed_demo_users (default false) returns false",
			cfg: &config.Config{
				Auth:   config.AuthConfig{Mode: config.AuthModeKratos},
				Kratos: config.KratosConfig{External: false},
			},
			want: false,
		},
		{
			name: "kratos mode external with seed_demo_users true returns false",
			cfg: &config.Config{
				Auth:   config.AuthConfig{Mode: config.AuthModeKratos, SeedDemoUsers: true},
				Kratos: config.KratosConfig{External: true},
			},
			want: false,
		},
		{
			name: "kratos mode external returns false",
			cfg: &config.Config{
				Auth:   config.AuthConfig{Mode: config.AuthModeKratos},
				Kratos: config.KratosConfig{External: true},
			},
			want: false,
		},
		{
			name: "jwt mode returns false",
			cfg: &config.Config{
				Auth: config.AuthConfig{Mode: config.AuthModeJWT},
			},
			want: false,
		},
		{
			name: "api-key mode returns false",
			cfg: &config.Config{
				Auth: config.AuthConfig{Mode: config.AuthModeAPIKey},
			},
			want: false,
		},
		{
			name: "none mode returns false",
			cfg: &config.Config{
				Auth: config.AuthConfig{Mode: config.AuthModeNone},
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
			got := generate.NeedsSeedUsers(tt.cfg)
			if got != tt.want {
				t.Errorf("NeedsSeedUsers() = %v, want %v", got, tt.want)
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
			name: "openbao store with no inject entries returns false",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Store:   "openbao",
					Inject:  config.SecretsInjectConfig{},
				},
			},
			want: false,
		},
		{
			name: "openbao store with header injection returns true",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Store:   "openbao",
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
			name: "openbao store with env injection returns true",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Store:   "openbao",
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
			name: "openbao store with both headers and env returns true",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Store:   "openbao",
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
			name: "builtin store returns false even with inject headers — seed-secrets.sh is openbao-only",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Store:   "builtin",
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
			name: "empty store (defaults to builtin) returns false even with inject entries",
			cfg: &config.Config{
				Secrets: config.SecretsConfig{
					Enabled: true,
					Store:   "",
					Inject: config.SecretsInjectConfig{
						Env: []config.SecretsEnvInjection{
							{SecretPath: "app/db-pass", SecretKey: "password", EnvVar: "DB_PASSWORD"},
						},
					},
				},
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
			got := generate.NeedsSeedSecrets(tt.cfg)
			if got != tt.want {
				t.Errorf("NeedsSeedSecrets() = %v, want %v", got, tt.want)
			}
		})
	}
}
