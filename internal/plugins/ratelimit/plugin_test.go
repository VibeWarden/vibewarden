package ratelimit_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	ratelimitadapter "github.com/vibewarden/vibewarden/internal/adapters/ratelimit"
	"github.com/vibewarden/vibewarden/internal/plugins/ratelimit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// discardLogger returns an slog.Logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

// defaultConfig returns a fully-enabled Config for tests.
func defaultConfig() ratelimit.Config {
	return ratelimit.Config{
		Enabled: true,
		Store:   "memory",
		PerIP: ratelimit.RuleConfig{
			RequestsPerSecond: 10,
			Burst:             20,
		},
		PerUser: ratelimit.RuleConfig{
			RequestsPerSecond: 100,
			Burst:             200,
		},
		TrustProxyHeaders: false,
		ExemptPaths:       []string{"/health"},
	}
}

// newPlugin creates a Plugin using the real MemoryFactory.
func newPlugin(cfg ratelimit.Config) *ratelimit.Plugin {
	return ratelimit.New(cfg, ratelimitadapter.NewDefaultMemoryFactory(), discardLogger())
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Name(); got != "rate-limiting" {
		t.Errorf("Name() = %q, want %q", got, "rate-limiting")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Priority(); got != 50 {
		t.Errorf("Priority() = %d, want 50", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ratelimit.Config
		wantErr bool
	}{
		{
			name:    "disabled — no-op",
			cfg:     ratelimit.Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled with valid config",
			cfg:     defaultConfig(),
			wantErr: false,
		},
		{
			name: "enabled with zero rates",
			cfg: ratelimit.Config{
				Enabled: true,
				PerIP:   ratelimit.RuleConfig{RequestsPerSecond: 0, Burst: 0},
				PerUser: ratelimit.RuleConfig{RequestsPerSecond: 0, Burst: 0},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			err := p.Init(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
			// Always stop to release GC goroutines started during Init.
			_ = p.Stop(context.Background())
		})
	}
}

// ---------------------------------------------------------------------------
// Start — no-op
// ---------------------------------------------------------------------------

func TestPlugin_Start_IsNoop(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}
	defer p.Stop(context.Background()) //nolint:errcheck
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Stop
// ---------------------------------------------------------------------------

func TestPlugin_Stop_WhenDisabled(t *testing.T) {
	p := newPlugin(ratelimit.Config{Enabled: false})
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() on disabled plugin unexpected error: %v", err)
	}
}

func TestPlugin_Stop_ClosesLimiters(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() unexpected error: %v", err)
	}
	// Calling Stop a second time must not panic or error (idempotent).
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() second call unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestPlugin_Health(t *testing.T) {
	tests := []struct {
		name           string
		cfg            ratelimit.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            ratelimit.Config{Enabled: false},
			wantHealthy:    true,
			wantMsgContain: "disabled",
		},
		{
			name:           "enabled",
			cfg:            defaultConfig(),
			wantHealthy:    true,
			wantMsgContain: "active",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			h := p.Health()
			if h.Healthy != tt.wantHealthy {
				t.Errorf("Health().Healthy = %v, want %v", h.Healthy, tt.wantHealthy)
			}
			if !strings.Contains(h.Message, tt.wantMsgContain) {
				t.Errorf("Health().Message = %q, want it to contain %q", h.Message, tt.wantMsgContain)
			}
		})
	}
}

func TestPlugin_Health_ContainsRates(t *testing.T) {
	cfg := ratelimit.Config{
		Enabled: true,
		PerIP:   ratelimit.RuleConfig{RequestsPerSecond: 10, Burst: 20},
		PerUser: ratelimit.RuleConfig{RequestsPerSecond: 100, Burst: 200},
	}
	p := newPlugin(cfg)
	h := p.Health()
	for _, want := range []string{"10.0", "20", "100.0", "200"} {
		if !strings.Contains(h.Message, want) {
			t.Errorf("Health().Message = %q, want it to contain %q", h.Message, want)
		}
	}
}

// ---------------------------------------------------------------------------
// ContributeCaddyRoutes
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_AlwaysEmpty(t *testing.T) {
	tests := []struct {
		name string
		cfg  ratelimit.Config
	}{
		{"disabled", ratelimit.Config{Enabled: false}},
		{"enabled", defaultConfig()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			if routes := p.ContributeCaddyRoutes(); len(routes) != 0 {
				t.Errorf("ContributeCaddyRoutes() = %v, want empty", routes)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ContributeCaddyHandlers
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyHandlers_DisabledReturnsNil(t *testing.T) {
	p := newPlugin(ratelimit.Config{Enabled: false})
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() = %v, want empty when disabled", handlers)
	}
}

func TestPlugin_ContributeCaddyHandlers_EnabledReturnsOne(t *testing.T) {
	p := newPlugin(defaultConfig())
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("ContributeCaddyHandlers() len = %d, want 1", len(handlers))
	}
	if handlers[0].Priority != 50 {
		t.Errorf("handler Priority = %d, want 50", handlers[0].Priority)
	}
	if handlers[0].Handler["handler"] != "vibewarden_rate_limit" {
		t.Errorf("handler[\"handler\"] = %v, want %q", handlers[0].Handler["handler"], "vibewarden_rate_limit")
	}
}

func TestPlugin_ContributeCaddyHandlers_HandlerHasConfig(t *testing.T) {
	p := newPlugin(defaultConfig())
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler")
	}
	h := handlers[0].Handler
	if _, ok := h["config"]; !ok {
		t.Error("expected \"config\" key in handler map")
	}
}

func TestPlugin_ContributeCaddyHandlers_ConfigDeserialises(t *testing.T) {
	cfg := ratelimit.Config{
		Enabled:           true,
		PerIP:             ratelimit.RuleConfig{RequestsPerSecond: 5, Burst: 10},
		PerUser:           ratelimit.RuleConfig{RequestsPerSecond: 50, Burst: 100},
		TrustProxyHeaders: true,
		ExemptPaths:       []string{"/health", "/_vibewarden/*"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler")
	}

	rawConfig := handlers[0].Handler["config"]

	// The config value must be JSON-marshalable back to a map.
	b, err := json.Marshal(rawConfig)
	if err != nil {
		t.Fatalf("json.Marshal(config) error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(config) error: %v", err)
	}

	// Verify enabled flag round-trips.
	if enabled, ok := decoded["enabled"].(bool); !ok || !enabled {
		t.Errorf("decoded config enabled = %v, want true", decoded["enabled"])
	}

	// Verify trust_proxy_headers round-trips.
	if tph, ok := decoded["trust_proxy_headers"].(bool); !ok || !tph {
		t.Errorf("decoded config trust_proxy_headers = %v, want true", decoded["trust_proxy_headers"])
	}
}

func TestPlugin_ContributeCaddyHandlers_ExemptPaths(t *testing.T) {
	tests := []struct {
		name        string
		exemptPaths []string
	}{
		{"no exempt paths", nil},
		{"with exempt paths", []string{"/health", "/_vibewarden/*"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ratelimit.Config{
				Enabled:     true,
				PerIP:       ratelimit.RuleConfig{RequestsPerSecond: 10, Burst: 20},
				ExemptPaths: tt.exemptPaths,
			}
			p := newPlugin(cfg)
			handlers := p.ContributeCaddyHandlers()
			if len(handlers) == 0 {
				t.Fatal("expected at least one handler")
			}
			// Just assert the handler is present with correct type.
			if handlers[0].Handler["handler"] != "vibewarden_rate_limit" {
				t.Errorf("unexpected handler type: %v", handlers[0].Handler["handler"])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

// TestPlugin_ImplementsPortsPlugin asserts at compile time that *Plugin
// satisfies the ports.Plugin interface.
func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*ratelimit.Plugin)(nil)
}

// TestPlugin_ImplementsCaddyContributor asserts at compile time that *Plugin
// satisfies the ports.CaddyContributor interface.
func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*ratelimit.Plugin)(nil)
}
