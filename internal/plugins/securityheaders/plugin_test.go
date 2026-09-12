package securityheaders_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/securityheaders"
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

func defaultConfig() securityheaders.Config {
	return securityheaders.Config{
		Enabled:     true,
		HSTSMaxAge:  31536000,
		FrameOption: "DENY",
	}
}

func newPlugin(cfg securityheaders.Config, tlsEnabled bool) *securityheaders.Plugin {
	return securityheaders.New(cfg, tlsEnabled, discardLogger())
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(defaultConfig(), false)
	if got := p.Name(); got != "security-headers" {
		t.Errorf("Name() = %q, want %q", got, "security-headers")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(defaultConfig(), false)
	if got := p.Priority(); got != 20 {
		t.Errorf("Priority() = %d, want 20", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     securityheaders.Config
		wantErr bool
	}{
		{
			name:    "disabled — no validation",
			cfg:     securityheaders.Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled with valid DENY frame option",
			cfg:     securityheaders.Config{Enabled: true, FrameOption: "DENY"},
			wantErr: false,
		},
		{
			name:    "enabled with valid SAMEORIGIN frame option",
			cfg:     securityheaders.Config{Enabled: true, FrameOption: "SAMEORIGIN"},
			wantErr: false,
		},
		{
			name:    "enabled with empty frame option",
			cfg:     securityheaders.Config{Enabled: true, FrameOption: ""},
			wantErr: false,
		},
		{
			name:    "enabled with invalid frame option",
			cfg:     securityheaders.Config{Enabled: true, FrameOption: "ALLOWALL"},
			wantErr: true,
		},
		{
			name:    "enabled with full valid config",
			cfg:     defaultConfig(),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg, false)
			err := p.Init(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Start / Stop — no-ops
// ---------------------------------------------------------------------------

func TestPlugin_Start_IsNoop(t *testing.T) {
	p := newPlugin(defaultConfig(), false)
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() unexpected error: %v", err)
	}
}

func TestPlugin_Stop_IsNoop(t *testing.T) {
	p := newPlugin(defaultConfig(), false)
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestPlugin_Health(t *testing.T) {
	tests := []struct {
		name           string
		cfg            securityheaders.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            securityheaders.Config{Enabled: false},
			wantHealthy:    true,
			wantMsgContain: "disabled",
		},
		{
			name:           "enabled",
			cfg:            securityheaders.Config{Enabled: true},
			wantHealthy:    true,
			wantMsgContain: "configured",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg, false)
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

// ---------------------------------------------------------------------------
// ContributeCaddyRoutes
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_AlwaysEmpty(t *testing.T) {
	tests := []struct {
		name string
		cfg  securityheaders.Config
	}{
		{"disabled", securityheaders.Config{Enabled: false}},
		{"enabled", defaultConfig()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg, false)
			if routes := p.ContributeCaddyRoutes(); len(routes) != 0 {
				t.Errorf("ContributeCaddyRoutes() = %v, want empty", routes)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ContributeCaddyHandlers
// ---------------------------------------------------------------------------

// TestPlugin_ContributeCaddyHandlers_AlwaysNil pins #1540: the plugin must not
// contribute a security-headers handler. The Caddy adapter emits the headers
// as a global first route from the same configuration; a handler contributed
// here would be appended to the catch-all chain after the operator
// response_headers handler and override it.
func TestPlugin_ContributeCaddyHandlers_AlwaysNil(t *testing.T) {
	tests := []struct {
		name       string
		cfg        securityheaders.Config
		tlsEnabled bool
	}{
		{"disabled", securityheaders.Config{Enabled: false}, false},
		{"enabled without tls", defaultConfig(), false},
		{"enabled with tls", defaultConfig(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg, tt.tlsEnabled)
			if handlers := p.ContributeCaddyHandlers(); len(handlers) != 0 {
				t.Errorf("ContributeCaddyHandlers() = %v, want none", handlers)
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
	var _ ports.Plugin = (*securityheaders.Plugin)(nil)
}

// TestPlugin_ImplementsCaddyContributor asserts at compile time that *Plugin
// satisfies the ports.CaddyContributor interface.
func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*securityheaders.Plugin)(nil)
}
