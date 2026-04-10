package ports_test

import (
	"context"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestRateLimitRule_ZeroValue(t *testing.T) {
	var r ports.RateLimitRule
	if r.RequestsPerSecond != 0 {
		t.Errorf("expected zero RequestsPerSecond, got %v", r.RequestsPerSecond)
	}
	if r.Burst != 0 {
		t.Errorf("expected zero Burst, got %v", r.Burst)
	}
}

func TestRateLimitResult_Fields(t *testing.T) {
	tests := []struct {
		name   string
		result ports.RateLimitResult
		want   ports.RateLimitResult
	}{
		{
			name: "allowed result",
			result: ports.RateLimitResult{
				Allowed:    true,
				Remaining:  9,
				RetryAfter: 0,
				Limit:      10.0,
				Burst:      20,
			},
			want: ports.RateLimitResult{
				Allowed:    true,
				Remaining:  9,
				RetryAfter: 0,
				Limit:      10.0,
				Burst:      20,
			},
		},
		{
			name: "denied result with retry-after",
			result: ports.RateLimitResult{
				Allowed:    false,
				Remaining:  0,
				RetryAfter: 500 * time.Millisecond,
				Limit:      5.0,
				Burst:      10,
			},
			want: ports.RateLimitResult{
				Allowed:    false,
				Remaining:  0,
				RetryAfter: 500 * time.Millisecond,
				Limit:      5.0,
				Burst:      10,
			},
		},
		{
			name:   "zero value",
			result: ports.RateLimitResult{},
			want:   ports.RateLimitResult{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result.Allowed != tt.want.Allowed {
				t.Errorf("Allowed: got %v, want %v", tt.result.Allowed, tt.want.Allowed)
			}
			if tt.result.Remaining != tt.want.Remaining {
				t.Errorf("Remaining: got %v, want %v", tt.result.Remaining, tt.want.Remaining)
			}
			if tt.result.RetryAfter != tt.want.RetryAfter {
				t.Errorf("RetryAfter: got %v, want %v", tt.result.RetryAfter, tt.want.RetryAfter)
			}
			if tt.result.Limit != tt.want.Limit {
				t.Errorf("Limit: got %v, want %v", tt.result.Limit, tt.want.Limit)
			}
			if tt.result.Burst != tt.want.Burst {
				t.Errorf("Burst: got %v, want %v", tt.result.Burst, tt.want.Burst)
			}
		})
	}
}

func TestRateLimitConfig_Fields(t *testing.T) {
	tests := []struct {
		name   string
		config ports.RateLimitConfig
		want   ports.RateLimitConfig
	}{
		{
			name: "full configuration",
			config: ports.RateLimitConfig{
				Enabled: true,
				PerIP: ports.RateLimitRule{
					RequestsPerSecond: 10.0,
					Burst:             20,
				},
				PerUser: ports.RateLimitRule{
					RequestsPerSecond: 50.0,
					Burst:             100,
				},
				TrustProxyHeaders: true,
				ExemptPaths:       []string{"/healthz", "/readyz"},
			},
			want: ports.RateLimitConfig{
				Enabled: true,
				PerIP: ports.RateLimitRule{
					RequestsPerSecond: 10.0,
					Burst:             20,
				},
				PerUser: ports.RateLimitRule{
					RequestsPerSecond: 50.0,
					Burst:             100,
				},
				TrustProxyHeaders: true,
				ExemptPaths:       []string{"/healthz", "/readyz"},
			},
		},
		{
			name:   "disabled configuration",
			config: ports.RateLimitConfig{Enabled: false},
			want:   ports.RateLimitConfig{Enabled: false},
		},
		{
			name:   "zero value",
			config: ports.RateLimitConfig{},
			want:   ports.RateLimitConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.Enabled != tt.want.Enabled {
				t.Errorf("Enabled: got %v, want %v", tt.config.Enabled, tt.want.Enabled)
			}
			if tt.config.PerIP.RequestsPerSecond != tt.want.PerIP.RequestsPerSecond {
				t.Errorf("PerIP.RequestsPerSecond: got %v, want %v", tt.config.PerIP.RequestsPerSecond, tt.want.PerIP.RequestsPerSecond)
			}
			if tt.config.PerIP.Burst != tt.want.PerIP.Burst {
				t.Errorf("PerIP.Burst: got %v, want %v", tt.config.PerIP.Burst, tt.want.PerIP.Burst)
			}
			if tt.config.PerUser.RequestsPerSecond != tt.want.PerUser.RequestsPerSecond {
				t.Errorf("PerUser.RequestsPerSecond: got %v, want %v", tt.config.PerUser.RequestsPerSecond, tt.want.PerUser.RequestsPerSecond)
			}
			if tt.config.PerUser.Burst != tt.want.PerUser.Burst {
				t.Errorf("PerUser.Burst: got %v, want %v", tt.config.PerUser.Burst, tt.want.PerUser.Burst)
			}
			if tt.config.TrustProxyHeaders != tt.want.TrustProxyHeaders {
				t.Errorf("TrustProxyHeaders: got %v, want %v", tt.config.TrustProxyHeaders, tt.want.TrustProxyHeaders)
			}
			if len(tt.config.ExemptPaths) != len(tt.want.ExemptPaths) {
				t.Errorf("ExemptPaths length: got %v, want %v", len(tt.config.ExemptPaths), len(tt.want.ExemptPaths))
				return
			}
			for i, p := range tt.config.ExemptPaths {
				if p != tt.want.ExemptPaths[i] {
					t.Errorf("ExemptPaths[%d]: got %q, want %q", i, p, tt.want.ExemptPaths[i])
				}
			}
		})
	}
}

// TestRateLimiterInterface is a compile-time check that verifies the RateLimiter
// interface can be satisfied by a concrete type. If the interface signature changes
// in a breaking way, this test will fail to compile.
func TestRateLimiterInterface(t *testing.T) {
	var _ ports.RateLimiter = (*stubLimiter)(nil)
}

// TestRateLimiterFactoryInterface is a compile-time check for the RateLimiterFactory interface.
func TestRateLimiterFactoryInterface(t *testing.T) {
	var _ ports.RateLimiterFactory = (*stubFactory)(nil)
}

// stubLimiter is a minimal fake implementing ports.RateLimiter for interface verification.
type stubLimiter struct{}

func (s *stubLimiter) Allow(_ context.Context, _ string) ports.RateLimitResult {
	return ports.RateLimitResult{Allowed: true}
}

func (s *stubLimiter) Close() error { return nil }

// stubFactory is a minimal fake implementing ports.RateLimiterFactory for interface verification.
type stubFactory struct{}

func (f *stubFactory) NewLimiter(_ ports.RateLimitRule) ports.RateLimiter {
	return &stubLimiter{}
}
