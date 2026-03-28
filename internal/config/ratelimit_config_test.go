package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

// TestValidate_RateLimitStore verifies rate_limit.store validation.
func TestValidate_RateLimitStore(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty store is valid (defaults to memory)",
			cfg:     config.Config{RateLimit: config.RateLimitConfig{Store: ""}},
			wantErr: false,
		},
		{
			name:    "store memory is valid",
			cfg:     config.Config{RateLimit: config.RateLimitConfig{Store: "memory"}},
			wantErr: false,
		},
		{
			name: "store redis with address is valid",
			cfg: config.Config{
				RateLimit: config.RateLimitConfig{
					Store: "redis",
					Redis: config.RateLimitRedisConfig{Address: "localhost:6379"},
				},
			},
			wantErr: false,
		},
		{
			name: "store redis without address is invalid",
			cfg: config.Config{
				RateLimit: config.RateLimitConfig{
					Store: "redis",
					Redis: config.RateLimitRedisConfig{Address: ""},
				},
			},
			wantErr: true,
			errMsg:  "rate_limit.redis.address is required",
		},
		{
			name: "unknown store is invalid",
			cfg: config.Config{
				RateLimit: config.RateLimitConfig{Store: "kafka"},
			},
			wantErr: true,
			errMsg:  "rate_limit.store",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestLoad_RateLimitStoreDefaults verifies that Load sets sensible defaults
// for the new rate_limit.store and rate_limit.redis fields.
func TestLoad_RateLimitStoreDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "vibewarden.yaml")
	content := "server:\n  port: 8080\n"
	if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.RateLimit.Store != "memory" {
		t.Errorf("RateLimit.Store default = %q, want %q", cfg.RateLimit.Store, "memory")
	}
	if cfg.RateLimit.Redis.KeyPrefix != "vibewarden" {
		t.Errorf("RateLimit.Redis.KeyPrefix default = %q, want %q", cfg.RateLimit.Redis.KeyPrefix, "vibewarden")
	}
	if cfg.RateLimit.Redis.HealthCheckInterval != "30s" {
		t.Errorf("RateLimit.Redis.HealthCheckInterval default = %q, want %q", cfg.RateLimit.Redis.HealthCheckInterval, "30s")
	}
	if !cfg.RateLimit.Redis.Fallback {
		t.Error("RateLimit.Redis.Fallback default = false, want true")
	}
}

// TestLoad_RateLimitRedisFromEnv verifies that VIBEWARDEN_RATE_LIMIT_REDIS_*
// environment variables override the config file values.
func TestLoad_RateLimitRedisFromEnv(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "vibewarden.yaml")
	content := "rate_limit:\n  store: redis\n  redis:\n    address: localhost:6379\n"
	if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	t.Setenv("VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS", "redis-host:6380")
	t.Setenv("VIBEWARDEN_RATE_LIMIT_REDIS_PASSWORD", "s3cr3t")
	t.Setenv("VIBEWARDEN_RATE_LIMIT_REDIS_DB", "2")

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.RateLimit.Redis.Address != "redis-host:6380" {
		t.Errorf("Address = %q, want %q", cfg.RateLimit.Redis.Address, "redis-host:6380")
	}
	if cfg.RateLimit.Redis.Password != "s3cr3t" {
		t.Errorf("Password = %q, want %q", cfg.RateLimit.Redis.Password, "s3cr3t")
	}
	if cfg.RateLimit.Redis.DB != 2 {
		t.Errorf("DB = %d, want 2", cfg.RateLimit.Redis.DB)
	}
}
