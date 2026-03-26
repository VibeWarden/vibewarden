package caddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeRateLimiter is a test double for ports.RateLimiter.
type fakeRateLimiter struct {
	allowResult ports.RateLimitResult
	closeErr    error
	closeCalled bool
}

func (f *fakeRateLimiter) Allow(_ context.Context, _ string) ports.RateLimitResult {
	return f.allowResult
}

func (f *fakeRateLimiter) Close() error {
	f.closeCalled = true
	return f.closeErr
}

// TestRateLimitHandler_CaddyModule verifies the Caddy module metadata.
func TestRateLimitHandler_CaddyModule(t *testing.T) {
	info := RateLimitHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_rate_limit" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_rate_limit")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}

	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*RateLimitHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *RateLimitHandler", mod)
	}
}

// TestRateLimitHandler_InterfaceGuards verifies the handler satisfies required Caddy interfaces.
func TestRateLimitHandler_InterfaceGuards(t *testing.T) {
	// These compile-time assertions are already in the production file, but
	// we repeat them in the test package to make breakage visible in test output.
	var _ gocaddy.Provisioner = (*RateLimitHandler)(nil)
	var _ gocaddy.CleanerUpper = (*RateLimitHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*RateLimitHandler)(nil)
}

// TestRateLimitHandler_Provision verifies that Provision initialises the handler.
func TestRateLimitHandler_Provision(t *testing.T) {
	tests := []struct {
		name    string
		config  RateLimitHandlerConfig
		wantErr bool
	}{
		{
			name: "provision with enabled config",
			config: RateLimitHandlerConfig{
				Enabled: true,
				PerIP: RateLimitRuleHandlerConfig{
					RequestsPerSecond: 10,
					Burst:             20,
				},
				PerUser: RateLimitRuleHandlerConfig{
					RequestsPerSecond: 5,
					Burst:             10,
				},
				TrustProxyHeaders: false,
			},
			wantErr: false,
		},
		{
			name: "provision with disabled config still creates limiters",
			config: RateLimitHandlerConfig{
				Enabled: false,
				PerIP: RateLimitRuleHandlerConfig{
					RequestsPerSecond: 1,
					Burst:             1,
				},
				PerUser: RateLimitRuleHandlerConfig{
					RequestsPerSecond: 1,
					Burst:             1,
				},
			},
			wantErr: false,
		},
		{
			name: "provision with exempt paths",
			config: RateLimitHandlerConfig{
				Enabled: true,
				PerIP: RateLimitRuleHandlerConfig{
					RequestsPerSecond: 10,
					Burst:             20,
				},
				PerUser: RateLimitRuleHandlerConfig{
					RequestsPerSecond: 5,
					Burst:             10,
				},
				ExemptPaths: []string{"/health", "/metrics"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &RateLimitHandler{Config: tt.config}

			err := h.Provision(gocaddy.Context{})
			if (err != nil) != tt.wantErr {
				t.Errorf("Provision() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if h.ipLimiter == nil {
					t.Error("Provision() did not create ipLimiter")
				}
				if h.userLimiter == nil {
					t.Error("Provision() did not create userLimiter")
				}
				if h.handler == nil {
					t.Error("Provision() did not create handler")
				}
				// Clean up background goroutines.
				if err := h.Cleanup(); err != nil {
					t.Errorf("Cleanup() after Provision() error = %v", err)
				}
			}
		})
	}
}

// TestRateLimitHandler_Cleanup verifies Cleanup closes limiters correctly.
func TestRateLimitHandler_Cleanup(t *testing.T) {
	tests := []struct {
		name        string
		ipLimiter   *fakeRateLimiter
		userLimiter *fakeRateLimiter
		wantErr     bool
		wantErrMsg  string
	}{
		{
			name:        "both limiters nil — no-op",
			ipLimiter:   nil,
			userLimiter: nil,
			wantErr:     false,
		},
		{
			name:        "both limiters present — both closed",
			ipLimiter:   &fakeRateLimiter{},
			userLimiter: &fakeRateLimiter{},
			wantErr:     false,
		},
		{
			name:        "only ipLimiter present",
			ipLimiter:   &fakeRateLimiter{},
			userLimiter: nil,
			wantErr:     false,
		},
		{
			name:        "only userLimiter present",
			ipLimiter:   nil,
			userLimiter: &fakeRateLimiter{},
			wantErr:     false,
		},
		{
			name:        "ipLimiter.Close() returns error — wrapped",
			ipLimiter:   &fakeRateLimiter{closeErr: errFake("store closed")},
			userLimiter: &fakeRateLimiter{},
			wantErr:     true,
			wantErrMsg:  "closing IP rate limiter",
		},
		{
			name:        "userLimiter.Close() returns error — wrapped",
			ipLimiter:   &fakeRateLimiter{},
			userLimiter: &fakeRateLimiter{closeErr: errFake("store closed")},
			wantErr:     true,
			wantErrMsg:  "closing user rate limiter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &RateLimitHandler{}

			if tt.ipLimiter != nil {
				h.ipLimiter = tt.ipLimiter
			}
			if tt.userLimiter != nil {
				h.userLimiter = tt.userLimiter
			}

			err := h.Cleanup()
			if (err != nil) != tt.wantErr {
				t.Errorf("Cleanup() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.wantErrMsg != "" {
				if err == nil || !contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("Cleanup() error = %v, want it to contain %q", err, tt.wantErrMsg)
				}
			}

			// When no error from ip limiter, verify closeCalled.
			if tt.ipLimiter != nil && tt.ipLimiter.closeErr == nil && !tt.ipLimiter.closeCalled {
				t.Error("Cleanup() did not call Close() on ipLimiter")
			}
			// When ip limiter had no error, user limiter should also have been closed.
			if tt.userLimiter != nil && tt.ipLimiter != nil && tt.ipLimiter.closeErr == nil {
				if !tt.userLimiter.closeCalled {
					t.Error("Cleanup() did not call Close() on userLimiter")
				}
			}
		})
	}
}

// TestRateLimitHandler_ServeHTTP verifies the handler bridges to the middleware correctly.
func TestRateLimitHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		handlerEnabled bool
		nextCalled     bool
	}{
		{
			name:           "enabled handler calls next",
			handlerEnabled: true,
			nextCalled:     true,
		},
		{
			name:           "disabled handler passes through",
			handlerEnabled: false,
			nextCalled:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false

			h := &RateLimitHandler{
				Config: RateLimitHandlerConfig{
					Enabled: tt.handlerEnabled,
					PerIP: RateLimitRuleHandlerConfig{
						RequestsPerSecond: 100,
						Burst:             200,
					},
					PerUser: RateLimitRuleHandlerConfig{
						RequestsPerSecond: 100,
						Burst:             200,
					},
				},
			}
			if err := h.Provision(gocaddy.Context{}); err != nil {
				t.Fatalf("Provision() error = %v", err)
			}
			defer func() {
				if err := h.Cleanup(); err != nil {
					t.Errorf("Cleanup() error = %v", err)
				}
			}()

			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				nextCalled = true
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			w := httptest.NewRecorder()

			err := h.ServeHTTP(w, req, next)
			if err != nil {
				t.Errorf("ServeHTTP() error = %v", err)
			}

			if nextCalled != tt.nextCalled {
				t.Errorf("nextCalled = %v, want %v", nextCalled, tt.nextCalled)
			}
		})
	}
}

// TestBuildRateLimitHandlerJSON verifies the JSON serialisation of the handler config.
func TestBuildRateLimitHandlerJSON(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ports.RateLimitConfig
		wantErr bool
		checks  func(t *testing.T, result map[string]any)
	}{
		{
			name: "produces correct handler name",
			cfg: ports.RateLimitConfig{
				Enabled: true,
				PerIP: ports.RateLimitRule{
					RequestsPerSecond: 10,
					Burst:             20,
				},
				PerUser: ports.RateLimitRule{
					RequestsPerSecond: 5,
					Burst:             10,
				},
			},
			wantErr: false,
			checks: func(t *testing.T, result map[string]any) {
				t.Helper()
				handler, ok := result["handler"]
				if !ok {
					t.Fatal("result missing 'handler' key")
				}
				if handler != "vibewarden_rate_limit" {
					t.Errorf("handler = %q, want %q", handler, "vibewarden_rate_limit")
				}
			},
		},
		{
			name: "config key is present and valid JSON",
			cfg: ports.RateLimitConfig{
				Enabled: true,
				PerIP: ports.RateLimitRule{
					RequestsPerSecond: 10,
					Burst:             20,
				},
				PerUser: ports.RateLimitRule{
					RequestsPerSecond: 5,
					Burst:             10,
				},
				TrustProxyHeaders: true,
				ExemptPaths:       []string{"/healthz", "/ready"},
			},
			wantErr: false,
			checks: func(t *testing.T, result map[string]any) {
				t.Helper()
				raw, ok := result["config"]
				if !ok {
					t.Fatal("result missing 'config' key")
				}
				// config should be a json.RawMessage ([]byte), re-parseable.
				rawBytes, err := json.Marshal(raw)
				if err != nil {
					t.Fatalf("config value is not JSON-serialisable: %v", err)
				}
				var parsed map[string]any
				if err := json.Unmarshal(rawBytes, &parsed); err != nil {
					t.Fatalf("config value is not valid JSON: %v", err)
				}
				if enabled, _ := parsed["enabled"].(bool); !enabled {
					t.Error("config.enabled should be true")
				}
			},
		},
		{
			name: "exempt paths round-trip",
			cfg: ports.RateLimitConfig{
				Enabled:     true,
				ExemptPaths: []string{"/a", "/b"},
				PerIP:       ports.RateLimitRule{RequestsPerSecond: 1, Burst: 1},
				PerUser:     ports.RateLimitRule{RequestsPerSecond: 1, Burst: 1},
			},
			wantErr: false,
			checks: func(t *testing.T, result map[string]any) {
				t.Helper()
				rawBytes, _ := json.Marshal(result["config"])
				var parsed map[string]any
				_ = json.Unmarshal(rawBytes, &parsed)
				paths, ok := parsed["exempt_paths"].([]any)
				if !ok {
					t.Fatal("config.exempt_paths missing or wrong type")
				}
				if len(paths) != 2 {
					t.Errorf("len(exempt_paths) = %d, want 2", len(paths))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildRateLimitHandlerJSON(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildRateLimitHandlerJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checks != nil {
				tt.checks(t, result)
			}
		})
	}
}

// errFake is a simple error type used in tests.
type errFake string

func (e errFake) Error() string { return string(e) }

// contains reports whether s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
