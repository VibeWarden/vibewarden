package ratelimit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/ratelimit"
)

// TestPlugin_Init_RedisURL_InvalidScheme verifies that buildRedisClient rejects
// an http:// URL (not a valid Redis scheme) and surfaces the error through Init.
func TestPlugin_Init_RedisURL_InvalidScheme(t *testing.T) {
	cfg := ratelimit.Config{
		Enabled: true,
		Store:   "redis",
		Redis: ratelimit.RedisConfig{
			URL: "http://localhost:6379",
		},
	}
	p := ratelimit.New(cfg, nil, discardLogger())

	err := p.Init(context.Background())
	if err == nil {
		_ = p.Stop(context.Background())
		t.Fatal("Init() expected error for invalid Redis URL scheme, got nil")
	}
	if !strings.Contains(err.Error(), "building rate limiter factory") {
		t.Errorf("Init() error = %q, want it to contain %q", err.Error(), "building rate limiter factory")
	}
}

// TestPlugin_Init_RedisURL_WithPoolSize verifies that a redis:// URL combined
// with a PoolSize override does not panic or produce a construction error.
// (Connectivity failures are expected and handled gracefully via fallback.)
func TestPlugin_Init_RedisURL_WithPoolSize(t *testing.T) {
	cfg := ratelimit.Config{
		Enabled: true,
		Store:   "redis",
		Redis: ratelimit.RedisConfig{
			// Port 1 always refuses — tests URL parsing without real Redis.
			URL:      "redis://localhost:1/0",
			PoolSize: 5,
			// With Fallback=true the plugin falls back to memory on
			// the health-check failure that happens when a limiter is
			// first used, so Init itself succeeds.
			Fallback: true,
		},
		PerIP:   ratelimit.RuleConfig{RequestsPerSecond: 10, Burst: 20},
		PerUser: ratelimit.RuleConfig{RequestsPerSecond: 100, Burst: 200},
	}
	p := ratelimit.New(cfg, nil, discardLogger())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() unexpected error: %v", err)
	}
}

// TestPlugin_Init_RedisURL_TLSScheme verifies that a rediss:// URL is accepted
// by the URL parser (TLS scheme).
func TestPlugin_Init_RedisURL_TLSScheme(t *testing.T) {
	cfg := ratelimit.Config{
		Enabled: true,
		Store:   "redis",
		Redis: ratelimit.RedisConfig{
			// rediss:// is the TLS scheme; port 1 ensures the connection
			// will fail fast but URL parsing must succeed.
			URL:      "rediss://localhost:1/0",
			Fallback: true,
		},
		PerIP:   ratelimit.RuleConfig{RequestsPerSecond: 10, Burst: 20},
		PerUser: ratelimit.RuleConfig{RequestsPerSecond: 100, Burst: 200},
	}
	p := ratelimit.New(cfg, nil, discardLogger())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() unexpected error for rediss:// URL: %v", err)
	}
	_ = p.Stop(context.Background())
}
