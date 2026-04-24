package eject_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/eject"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeBuilder is a test double for ports.ConfigBuilder.
type fakeBuilder struct {
	called bool
	got    *ports.ProxyConfig
	result map[string]any
	err    error
}

func (f *fakeBuilder) Build(cfg *ports.ProxyConfig) (map[string]any, error) {
	f.called = true
	f.got = cfg
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return map[string]any{"apps": map[string]any{}}, nil
}

func minimalConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
		Upstream: config.UpstreamConfig{
			Host: "127.0.0.1",
			Port: 3000,
		},
		TLS: config.TLSConfig{
			Provider: "self-signed",
		},
	}
}

func TestService_Eject_NilConfig(t *testing.T) {
	b := &fakeBuilder{}
	svc := eject.NewService(b)

	_, err := svc.Eject(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
}

func TestService_Eject_BuilderError(t *testing.T) {
	b := &fakeBuilder{err: errors.New("build failed")}
	svc := eject.NewService(b)

	_, err := svc.Eject(minimalConfig(), nil)
	if err == nil {
		t.Fatal("expected error when builder fails, got nil")
	}
}

func TestService_Eject_ReturnsBuilderResult(t *testing.T) {
	want := map[string]any{"apps": map[string]any{"http": "ok"}}
	b := &fakeBuilder{result: want}
	svc := eject.NewService(b)

	got, err := svc.Eject(minimalConfig(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestService_Eject_CallsBuilder(t *testing.T) {
	b := &fakeBuilder{}
	svc := eject.NewService(b)

	cfg := minimalConfig()
	_, err := svc.Eject(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !b.called {
		t.Error("expected builder.Build to be called, was not")
	}
}

func TestService_Eject_ProxyConfigListenAddr(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		wantAddr string
	}{
		{"localhost default", "127.0.0.1", 8080, "127.0.0.1:8080"},
		{"all interfaces", "0.0.0.0", 443, "0.0.0.0:443"},
		{"custom host", "10.0.0.1", 9000, "10.0.0.1:9000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBuilder{}
			svc := eject.NewService(b)

			cfg := minimalConfig()
			cfg.Server.Host = tt.host
			cfg.Server.Port = tt.port

			_, err := svc.Eject(cfg, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if b.got == nil {
				t.Fatal("expected builder.Build to be called with a ProxyConfig")
			}

			if b.got.ListenAddr != tt.wantAddr {
				t.Errorf("ListenAddr = %q, want %q", b.got.ListenAddr, tt.wantAddr)
			}
		})
	}
}

func TestService_Eject_ProxyConfigUpstreamAddr(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		wantAddr string
	}{
		{"default upstream", "127.0.0.1", 3000, "127.0.0.1:3000"},
		{"external upstream", "app.internal", 8000, "app.internal:8000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBuilder{}
			svc := eject.NewService(b)

			cfg := minimalConfig()
			cfg.Upstream.Host = tt.host
			cfg.Upstream.Port = tt.port

			_, err := svc.Eject(cfg, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if b.got.UpstreamAddr != tt.wantAddr {
				t.Errorf("UpstreamAddr = %q, want %q", b.got.UpstreamAddr, tt.wantAddr)
			}
		})
	}
}

func TestService_Eject_TLSConfig(t *testing.T) {
	tests := []struct {
		name         string
		tlsCfg       config.TLSConfig
		wantEnabled  bool
		wantProvider ports.TLSProvider
		wantDomain   string
	}{
		{
			name:        "tls disabled",
			tlsCfg:      config.TLSConfig{Enabled: false, Provider: "self-signed"},
			wantEnabled: false,
		},
		{
			name:         "self-signed",
			tlsCfg:       config.TLSConfig{Enabled: true, Provider: "self-signed", Domain: "localhost"},
			wantEnabled:  true,
			wantProvider: ports.TLSProviderSelfSigned,
			wantDomain:   "localhost",
		},
		{
			name:         "letsencrypt",
			tlsCfg:       config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: "example.com"},
			wantEnabled:  true,
			wantProvider: ports.TLSProviderLetsEncrypt,
			wantDomain:   "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBuilder{}
			svc := eject.NewService(b)

			cfg := minimalConfig()
			cfg.TLS = tt.tlsCfg

			_, err := svc.Eject(cfg, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := b.got.TLS
			if got.Enabled != tt.wantEnabled {
				t.Errorf("TLS.Enabled = %v, want %v", got.Enabled, tt.wantEnabled)
			}
			if tt.wantEnabled {
				if got.Provider != tt.wantProvider {
					t.Errorf("TLS.Provider = %q, want %q", got.Provider, tt.wantProvider)
				}
				if got.Domain != tt.wantDomain {
					t.Errorf("TLS.Domain = %q, want %q", got.Domain, tt.wantDomain)
				}
			}
		})
	}
}

func TestService_Eject_VersionIsEjected(t *testing.T) {
	b := &fakeBuilder{}
	svc := eject.NewService(b)

	_, err := svc.Eject(minimalConfig(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if b.got.Version != "ejected" {
		t.Errorf("Version = %q, want %q", b.got.Version, "ejected")
	}
}

func TestService_Eject_InternalAddrsOmitted(t *testing.T) {
	b := &fakeBuilder{}
	svc := eject.NewService(b)

	_, err := svc.Eject(minimalConfig(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if b.got.Metrics.InternalAddr != "" {
		t.Errorf("Metrics.InternalAddr should be empty, got %q", b.got.Metrics.InternalAddr)
	}
	if b.got.Admin.InternalAddr != "" {
		t.Errorf("Admin.InternalAddr should be empty, got %q", b.got.Admin.InternalAddr)
	}
	if b.got.Readiness.InternalAddr != "" {
		t.Errorf("Readiness.InternalAddr should be empty, got %q", b.got.Readiness.InternalAddr)
	}
}

func TestService_Eject_ExtraHandlersPassedThrough(t *testing.T) {
	tests := []struct {
		name          string
		handlers      []ports.CaddyHandler
		wantCount     int
		wantFirstName string
	}{
		{
			name:      "nil handlers",
			handlers:  nil,
			wantCount: 0,
		},
		{
			name:      "empty handlers",
			handlers:  []ports.CaddyHandler{},
			wantCount: 0,
		},
		{
			name: "single handler",
			handlers: []ports.CaddyHandler{
				{
					Handler:  map[string]any{"handler": "vibewarden_waf_content_type"},
					Priority: 25,
				},
			},
			wantCount:     1,
			wantFirstName: "vibewarden_waf_content_type",
		},
		{
			name: "two handlers",
			handlers: []ports.CaddyHandler{
				{
					Handler:  map[string]any{"handler": "vibewarden_waf_content_type"},
					Priority: 25,
				},
				{
					Handler:  map[string]any{"handler": "vibewarden_waf_engine"},
					Priority: 25,
				},
			},
			wantCount:     2,
			wantFirstName: "vibewarden_waf_content_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBuilder{}
			svc := eject.NewService(b)

			_, err := svc.Eject(minimalConfig(), tt.handlers)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := b.got.ExtraHandlers
			if len(got) != tt.wantCount {
				t.Fatalf("ExtraHandlers count = %d, want %d", len(got), tt.wantCount)
			}

			if tt.wantCount > 0 {
				gotName, ok := got[0].Handler["handler"].(string)
				if !ok {
					t.Errorf("ExtraHandlers[0].Handler[\"handler\"] is not a string")
				} else if gotName != tt.wantFirstName {
					t.Errorf("ExtraHandlers[0] handler = %q, want %q", gotName, tt.wantFirstName)
				}
			}
		})
	}
}

func TestErrUnsupportedFormat_Error(t *testing.T) {
	err := eject.ErrUnsupportedFormat{Format: eject.Format("nginx")}
	msg := err.Error()

	if msg == "" {
		t.Error("expected non-empty error message")
	}

	want := "nginx"
	if len(msg) < len(want) {
		t.Errorf("error message %q does not mention format", msg)
	}
}

// TestService_Eject_BodySizeConfig exercises buildBodySizeConfig through
// Service.Eject, verifying correct translation of human-readable size strings
// and per-path overrides into ports.BodySizeConfig.
func TestService_Eject_BodySizeConfig(t *testing.T) {
	tests := []struct {
		name          string
		bodySizeCfg   config.BodySizeConfig
		wantEnabled   bool
		wantMaxBytes  int64
		wantOverrides []ports.BodySizeOverride
	}{
		{
			name:         "empty Max returns zero config",
			bodySizeCfg:  config.BodySizeConfig{Max: ""},
			wantEnabled:  false,
			wantMaxBytes: 0,
		},
		{
			name:         "unparseable Max returns zero config",
			bodySizeCfg:  config.BodySizeConfig{Max: "notasize"},
			wantEnabled:  false,
			wantMaxBytes: 0,
		},
		{
			name:         "valid Max with no overrides",
			bodySizeCfg:  config.BodySizeConfig{Max: "10MB"},
			wantEnabled:  true,
			wantMaxBytes: 10 * 1024 * 1024,
		},
		{
			name: "valid Max with one valid override",
			bodySizeCfg: config.BodySizeConfig{
				Max: "10MB",
				Overrides: []config.BodySizeOverrideConfig{
					{Path: "/upload", Max: "50MB"},
				},
			},
			wantEnabled:  true,
			wantMaxBytes: 10 * 1024 * 1024,
			wantOverrides: []ports.BodySizeOverride{
				{Path: "/upload", MaxBytes: 50 * 1024 * 1024},
			},
		},
		{
			name: "valid Max with one unparseable override — override silently skipped",
			bodySizeCfg: config.BodySizeConfig{
				Max: "10MB",
				Overrides: []config.BodySizeOverrideConfig{
					{Path: "/bad", Max: "badsize"},
				},
			},
			wantEnabled:   true,
			wantMaxBytes:  10 * 1024 * 1024,
			wantOverrides: nil,
		},
		{
			name: "valid Max with mixed overrides — unparseable skipped, valid retained",
			bodySizeCfg: config.BodySizeConfig{
				Max: "10MB",
				Overrides: []config.BodySizeOverrideConfig{
					{Path: "/bad", Max: "badsize"},
					{Path: "/upload", Max: "50MB"},
				},
			},
			wantEnabled:  true,
			wantMaxBytes: 10 * 1024 * 1024,
			wantOverrides: []ports.BodySizeOverride{
				{Path: "/upload", MaxBytes: 50 * 1024 * 1024},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBuilder{}
			svc := eject.NewService(b)

			cfg := minimalConfig()
			cfg.BodySize = tt.bodySizeCfg

			_, err := svc.Eject(cfg, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := b.got.BodySize
			if got.Enabled != tt.wantEnabled {
				t.Errorf("BodySize.Enabled = %v, want %v", got.Enabled, tt.wantEnabled)
			}
			if got.MaxBytes != tt.wantMaxBytes {
				t.Errorf("BodySize.MaxBytes = %d, want %d", got.MaxBytes, tt.wantMaxBytes)
			}
			if len(got.Overrides) != len(tt.wantOverrides) {
				t.Fatalf("BodySize.Overrides len = %d, want %d", len(got.Overrides), len(tt.wantOverrides))
			}
			for i, ov := range tt.wantOverrides {
				if got.Overrides[i].Path != ov.Path {
					t.Errorf("Overrides[%d].Path = %q, want %q", i, got.Overrides[i].Path, ov.Path)
				}
				if got.Overrides[i].MaxBytes != ov.MaxBytes {
					t.Errorf("Overrides[%d].MaxBytes = %d, want %d", i, got.Overrides[i].MaxBytes, ov.MaxBytes)
				}
			}
		})
	}
}

// TestService_Eject_ResilienceConfig exercises buildResilienceConfig through
// Service.Eject, verifying timeout parsing, circuit breaker, and retry
// translation into ports.ResilienceConfig.
func TestService_Eject_ResilienceConfig(t *testing.T) {
	tests := []struct {
		name             string
		resilienceCfg    config.ResilienceConfig
		wantTimeout      time.Duration
		wantCBEnabled    bool
		wantCBThreshold  int
		wantCBTimeout    time.Duration
		wantRetryEnabled bool
		wantMaxAttempts  int
		wantInitBackoff  time.Duration
		wantMaxBackoff   time.Duration
		wantRetryOn      []int
	}{
		{
			name:          "empty timeout — no timeout set",
			resilienceCfg: config.ResilienceConfig{Timeout: ""},
			wantTimeout:   0,
		},
		{
			name:          "zero string timeout — no timeout set",
			resilienceCfg: config.ResilienceConfig{Timeout: "0"},
			wantTimeout:   0,
		},
		{
			name:          "valid timeout parsed correctly",
			resilienceCfg: config.ResilienceConfig{Timeout: "30s"},
			wantTimeout:   30 * time.Second,
		},
		{
			name:          "invalid timeout falls back to 30s",
			resilienceCfg: config.ResilienceConfig{Timeout: "notaduration"},
			wantTimeout:   30 * time.Second,
		},
		{
			name: "circuit breaker disabled",
			resilienceCfg: config.ResilienceConfig{
				CircuitBreaker: config.CircuitBreakerConfig{Enabled: false},
			},
			wantCBEnabled: false,
		},
		{
			name: "circuit breaker enabled with valid threshold and timeout",
			resilienceCfg: config.ResilienceConfig{
				CircuitBreaker: config.CircuitBreakerConfig{
					Enabled:   true,
					Threshold: 10,
					Timeout:   "2m",
				},
			},
			wantCBEnabled:   true,
			wantCBThreshold: 10,
			wantCBTimeout:   2 * time.Minute,
		},
		{
			name: "circuit breaker enabled with zero threshold clamped to 5",
			resilienceCfg: config.ResilienceConfig{
				CircuitBreaker: config.CircuitBreakerConfig{
					Enabled:   true,
					Threshold: 0,
					Timeout:   "60s",
				},
			},
			wantCBEnabled:   true,
			wantCBThreshold: 5,
			wantCBTimeout:   60 * time.Second,
		},
		{
			name: "circuit breaker enabled with negative threshold clamped to 5",
			resilienceCfg: config.ResilienceConfig{
				CircuitBreaker: config.CircuitBreakerConfig{
					Enabled:   true,
					Threshold: -3,
					Timeout:   "60s",
				},
			},
			wantCBEnabled:   true,
			wantCBThreshold: 5,
			wantCBTimeout:   60 * time.Second,
		},
		{
			name: "circuit breaker enabled with invalid timeout falls back to 60s",
			resilienceCfg: config.ResilienceConfig{
				CircuitBreaker: config.CircuitBreakerConfig{
					Enabled:   true,
					Threshold: 5,
					Timeout:   "notaduration",
				},
			},
			wantCBEnabled:   true,
			wantCBThreshold: 5,
			wantCBTimeout:   60 * time.Second,
		},
		{
			name: "circuit breaker enabled with empty timeout uses 60s default",
			resilienceCfg: config.ResilienceConfig{
				CircuitBreaker: config.CircuitBreakerConfig{
					Enabled:   true,
					Threshold: 5,
					Timeout:   "",
				},
			},
			wantCBEnabled:   true,
			wantCBThreshold: 5,
			wantCBTimeout:   60 * time.Second,
		},
		{
			name: "retry disabled",
			resilienceCfg: config.ResilienceConfig{
				Retry: config.RetryConfig{Enabled: false},
			},
			wantRetryEnabled: false,
		},
		{
			name: "retry enabled with valid settings",
			resilienceCfg: config.ResilienceConfig{
				Retry: config.RetryConfig{
					Enabled:        true,
					MaxAttempts:    5,
					InitialBackoff: "200ms",
					MaxBackoff:     "5s",
					RetryOn:        []int{500, 502},
				},
			},
			wantRetryEnabled: true,
			wantMaxAttempts:  5,
			wantInitBackoff:  200 * time.Millisecond,
			wantMaxBackoff:   5 * time.Second,
			wantRetryOn:      []int{500, 502},
		},
		{
			name: "retry enabled with MaxAttempts less than 2 clamped to 3",
			resilienceCfg: config.ResilienceConfig{
				Retry: config.RetryConfig{
					Enabled:     true,
					MaxAttempts: 1,
					RetryOn:     []int{502},
				},
			},
			wantRetryEnabled: true,
			wantMaxAttempts:  3,
			wantInitBackoff:  100 * time.Millisecond,
			wantMaxBackoff:   10 * time.Second,
			wantRetryOn:      []int{502},
		},
		{
			name: "retry enabled with zero MaxAttempts clamped to 3",
			resilienceCfg: config.ResilienceConfig{
				Retry: config.RetryConfig{
					Enabled:     true,
					MaxAttempts: 0,
					RetryOn:     []int{502},
				},
			},
			wantRetryEnabled: true,
			wantMaxAttempts:  3,
			wantInitBackoff:  100 * time.Millisecond,
			wantMaxBackoff:   10 * time.Second,
			wantRetryOn:      []int{502},
		},
		{
			name: "retry enabled with empty RetryOn defaults to 502 503 504",
			resilienceCfg: config.ResilienceConfig{
				Retry: config.RetryConfig{
					Enabled:     true,
					MaxAttempts: 3,
				},
			},
			wantRetryEnabled: true,
			wantMaxAttempts:  3,
			wantInitBackoff:  100 * time.Millisecond,
			wantMaxBackoff:   10 * time.Second,
			wantRetryOn:      []int{502, 503, 504},
		},
		{
			name: "retry enabled with invalid InitialBackoff falls back to 100ms",
			resilienceCfg: config.ResilienceConfig{
				Retry: config.RetryConfig{
					Enabled:        true,
					MaxAttempts:    3,
					InitialBackoff: "badbackoff",
					MaxBackoff:     "10s",
					RetryOn:        []int{502},
				},
			},
			wantRetryEnabled: true,
			wantMaxAttempts:  3,
			wantInitBackoff:  100 * time.Millisecond,
			wantMaxBackoff:   10 * time.Second,
			wantRetryOn:      []int{502},
		},
		{
			name: "retry enabled with invalid MaxBackoff falls back to 10s",
			resilienceCfg: config.ResilienceConfig{
				Retry: config.RetryConfig{
					Enabled:        true,
					MaxAttempts:    3,
					InitialBackoff: "100ms",
					MaxBackoff:     "badmax",
					RetryOn:        []int{502},
				},
			},
			wantRetryEnabled: true,
			wantMaxAttempts:  3,
			wantInitBackoff:  100 * time.Millisecond,
			wantMaxBackoff:   10 * time.Second,
			wantRetryOn:      []int{502},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBuilder{}
			svc := eject.NewService(b)

			cfg := minimalConfig()
			cfg.Resilience = tt.resilienceCfg

			_, err := svc.Eject(cfg, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := b.got.Resilience

			if got.Timeout != tt.wantTimeout {
				t.Errorf("Resilience.Timeout = %v, want %v", got.Timeout, tt.wantTimeout)
			}

			if got.CircuitBreaker.Enabled != tt.wantCBEnabled {
				t.Errorf("CircuitBreaker.Enabled = %v, want %v", got.CircuitBreaker.Enabled, tt.wantCBEnabled)
			}
			if tt.wantCBEnabled {
				if got.CircuitBreaker.Threshold != tt.wantCBThreshold {
					t.Errorf("CircuitBreaker.Threshold = %d, want %d", got.CircuitBreaker.Threshold, tt.wantCBThreshold)
				}
				if got.CircuitBreaker.Timeout != tt.wantCBTimeout {
					t.Errorf("CircuitBreaker.Timeout = %v, want %v", got.CircuitBreaker.Timeout, tt.wantCBTimeout)
				}
			}

			if got.Retry.Enabled != tt.wantRetryEnabled {
				t.Errorf("Retry.Enabled = %v, want %v", got.Retry.Enabled, tt.wantRetryEnabled)
			}
			if tt.wantRetryEnabled {
				if got.Retry.MaxAttempts != tt.wantMaxAttempts {
					t.Errorf("Retry.MaxAttempts = %d, want %d", got.Retry.MaxAttempts, tt.wantMaxAttempts)
				}
				if got.Retry.InitialBackoff != tt.wantInitBackoff {
					t.Errorf("Retry.InitialBackoff = %v, want %v", got.Retry.InitialBackoff, tt.wantInitBackoff)
				}
				if got.Retry.MaxBackoff != tt.wantMaxBackoff {
					t.Errorf("Retry.MaxBackoff = %v, want %v", got.Retry.MaxBackoff, tt.wantMaxBackoff)
				}
				if len(got.Retry.RetryOn) != len(tt.wantRetryOn) {
					t.Fatalf("Retry.RetryOn len = %d, want %d", len(got.Retry.RetryOn), len(tt.wantRetryOn))
				}
				for i, code := range tt.wantRetryOn {
					if got.Retry.RetryOn[i] != code {
						t.Errorf("Retry.RetryOn[%d] = %d, want %d", i, got.Retry.RetryOn[i], code)
					}
				}
			}
		})
	}
}
