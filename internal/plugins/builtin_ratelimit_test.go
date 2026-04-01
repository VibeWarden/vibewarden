package plugins_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/plugins"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// findRateLimitPlugin returns the rate-limiting plugin from the registry.
func findRateLimitPlugin(registry *plugins.Registry) ports.Plugin {
	for _, p := range registry.Plugins() {
		if p.Name() == "rate-limiting" {
			return p
		}
	}
	return nil
}

// stubEventLogger satisfies ports.EventLogger for test wiring.
type stubEventLogger struct{}

func (stubEventLogger) Log(_ context.Context, _ events.Event) error { return nil }

// TestRegisterBuiltinPlugins_RateLimitStoreWiring verifies that
// RegisterBuiltinPlugins passes the correct Store value from config
// to the rate-limiting plugin, so that "redis" and "memory" stores
// are wired appropriately.
func TestRegisterBuiltinPlugins_RateLimitStoreWiring(t *testing.T) {
	tests := []struct {
		name        string
		store       string
		redisURL    string
		wantInitErr bool
		errContains string
	}{
		{
			name:        "empty store defaults to memory",
			store:       "",
			wantInitErr: false,
		},
		{
			name:        "explicit memory store",
			store:       "memory",
			wantInitErr: false,
		},
		{
			name:        "redis store with invalid URL scheme fails Init",
			store:       "redis",
			redisURL:    "http://localhost:6379",
			wantInitErr: true,
			errContains: "building rate limiter factory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				RateLimit: config.RateLimitConfig{
					Enabled: true,
					Store:   tt.store,
					Redis: config.RateLimitRedisConfig{
						URL:      tt.redisURL,
						Fallback: false,
					},
					PerIP:   config.RateLimitRuleConfig{RequestsPerSecond: 10, Burst: 20},
					PerUser: config.RateLimitRuleConfig{RequestsPerSecond: 100, Burst: 200},
				},
			}

			logger := discardLogger()
			registry := plugins.NewRegistry(logger)
			plugins.RegisterBuiltinPlugins(registry, cfg, stubEventLogger{}, logger)

			rlPlugin := findRateLimitPlugin(registry)
			if rlPlugin == nil {
				t.Fatal("rate-limiting plugin not found in registry")
			}

			err := rlPlugin.Init(context.Background())
			if tt.wantInitErr {
				if err == nil {
					_ = rlPlugin.Stop(context.Background())
					t.Fatal("Init() expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Init() error = %q, want it to contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Fatalf("Init() unexpected error: %v", err)
				}
				_ = rlPlugin.Stop(context.Background())
			}
		})
	}
}

// TestRegisterBuiltinPlugins_RateLimitRedisConfigPassthrough verifies that
// Redis configuration fields from config.Config are passed through to the
// rate-limiting plugin. A redis store with fallback=true and an unreachable
// address succeeds Init (falls back to memory).
func TestRegisterBuiltinPlugins_RateLimitRedisConfigPassthrough(t *testing.T) {
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{
			Enabled: true,
			Store:   "redis",
			Redis: config.RateLimitRedisConfig{
				URL:                 "redis://localhost:1/0",
				Fallback:            true,
				HealthCheckInterval: "5s",
				KeyPrefix:           "testprefix",
				PoolSize:            3,
			},
			PerIP:   config.RateLimitRuleConfig{RequestsPerSecond: 10, Burst: 20},
			PerUser: config.RateLimitRuleConfig{RequestsPerSecond: 100, Burst: 200},
		},
	}

	logger := discardLogger()
	registry := plugins.NewRegistry(logger)
	plugins.RegisterBuiltinPlugins(registry, cfg, stubEventLogger{}, logger)

	rlPlugin := findRateLimitPlugin(registry)
	if rlPlugin == nil {
		t.Fatal("rate-limiting plugin not found in registry")
	}

	if err := rlPlugin.Init(context.Background()); err != nil {
		t.Fatalf("Init() unexpected error with fallback=true: %v", err)
	}
	if err := rlPlugin.Stop(context.Background()); err != nil {
		t.Errorf("Stop() unexpected error: %v", err)
	}
}

// TestRegisterBuiltinPlugins_RateLimitMemoryStoreInit verifies that the
// memory store path still works correctly end-to-end after the redis
// wiring changes (no regression).
func TestRegisterBuiltinPlugins_RateLimitMemoryStoreInit(t *testing.T) {
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{
			Enabled: true,
			Store:   "memory",
			PerIP:   config.RateLimitRuleConfig{RequestsPerSecond: 10, Burst: 20},
			PerUser: config.RateLimitRuleConfig{RequestsPerSecond: 100, Burst: 200},
		},
	}

	logger := discardLogger()
	registry := plugins.NewRegistry(logger)
	plugins.RegisterBuiltinPlugins(registry, cfg, stubEventLogger{}, logger)

	rlPlugin := findRateLimitPlugin(registry)
	if rlPlugin == nil {
		t.Fatal("rate-limiting plugin not found in registry")
	}

	if err := rlPlugin.Init(context.Background()); err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}

	health := rlPlugin.Health()
	if !health.Healthy {
		t.Errorf("Health().Healthy = false, want true")
	}
	if !strings.Contains(health.Message, "active") {
		t.Errorf("Health().Message = %q, want it to contain %q", health.Message, "active")
	}

	if err := rlPlugin.Stop(context.Background()); err != nil {
		t.Errorf("Stop() unexpected error: %v", err)
	}
}
