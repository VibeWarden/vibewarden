package ipfilter_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/ipfilter"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

func defaultConfig() ipfilter.Config {
	return ipfilter.Config{
		Enabled:   true,
		Mode:      ipfilter.FilterModeBlocklist,
		Addresses: []string{"10.0.0.0/8", "192.168.1.100"},
	}
}

func newPlugin(cfg ipfilter.Config) *ipfilter.Plugin {
	return ipfilter.New(cfg, discardLogger())
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Name(); got != "ip-filter" {
		t.Errorf("Name() = %q, want %q", got, "ip-filter")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Priority(); got != 15 {
		t.Errorf("Priority() = %d, want 15", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ipfilter.Config
		wantErr bool
	}{
		{
			name:    "disabled — no-op",
			cfg:     ipfilter.Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "blocklist mode with CIDRs",
			cfg:     defaultConfig(),
			wantErr: false,
		},
		{
			name: "allowlist mode with plain IPs",
			cfg: ipfilter.Config{
				Enabled:   true,
				Mode:      ipfilter.FilterModeAllowlist,
				Addresses: []string{"203.0.113.1", "198.51.100.0/24"},
			},
			wantErr: false,
		},
		{
			name: "empty addresses list",
			cfg: ipfilter.Config{
				Enabled:   true,
				Mode:      ipfilter.FilterModeBlocklist,
				Addresses: []string{},
			},
			wantErr: false,
		},
		{
			name: "invalid mode",
			cfg: ipfilter.Config{
				Enabled:   true,
				Mode:      "invalid",
				Addresses: []string{"10.0.0.0/8"},
			},
			wantErr: true,
		},
		{
			name: "invalid address",
			cfg: ipfilter.Config{
				Enabled:   true,
				Mode:      ipfilter.FilterModeBlocklist,
				Addresses: []string{"not-an-ip"},
			},
			wantErr: true,
		},
		{
			name: "default mode (empty string) treated as blocklist",
			cfg: ipfilter.Config{
				Enabled:   true,
				Mode:      "",
				Addresses: []string{"10.0.0.1"},
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
		})
	}
}

// ---------------------------------------------------------------------------
// Start / Stop — no-op
// ---------------------------------------------------------------------------

func TestPlugin_Start_IsNoop(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() unexpected error: %v", err)
	}
}

func TestPlugin_Stop_IsNoop(t *testing.T) {
	tests := []struct {
		name string
		cfg  ipfilter.Config
	}{
		{"disabled", ipfilter.Config{Enabled: false}},
		{"enabled", defaultConfig()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			if err := p.Stop(context.Background()); err != nil {
				t.Errorf("Stop() unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestPlugin_Health(t *testing.T) {
	tests := []struct {
		name           string
		cfg            ipfilter.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            ipfilter.Config{Enabled: false},
			wantHealthy:    true,
			wantMsgContain: "disabled",
		},
		{
			name:           "enabled blocklist",
			cfg:            defaultConfig(),
			wantHealthy:    true,
			wantMsgContain: "active",
		},
		{
			name: "enabled allowlist",
			cfg: ipfilter.Config{
				Enabled:   true,
				Mode:      ipfilter.FilterModeAllowlist,
				Addresses: []string{"10.0.0.0/8"},
			},
			wantHealthy:    true,
			wantMsgContain: "allowlist",
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

// ---------------------------------------------------------------------------
// ContributeCaddyRoutes
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_AlwaysEmpty(t *testing.T) {
	tests := []struct {
		name string
		cfg  ipfilter.Config
	}{
		{"disabled", ipfilter.Config{Enabled: false}},
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
	p := newPlugin(ipfilter.Config{Enabled: false})
	if handlers := p.ContributeCaddyHandlers(); len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() = %v, want empty when disabled", handlers)
	}
}

func TestPlugin_ContributeCaddyHandlers_EnabledReturnsOne(t *testing.T) {
	p := newPlugin(defaultConfig())
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("ContributeCaddyHandlers() len = %d, want 1", len(handlers))
	}
	if handlers[0].Priority != 15 {
		t.Errorf("handler Priority = %d, want 15", handlers[0].Priority)
	}
	if handlers[0].Handler["handler"] != "vibewarden_ip_filter" {
		t.Errorf("handler[\"handler\"] = %v, want %q", handlers[0].Handler["handler"], "vibewarden_ip_filter")
	}
}

func TestPlugin_ContributeCaddyHandlers_HandlerHasConfig(t *testing.T) {
	p := newPlugin(defaultConfig())
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler")
	}
	if _, ok := handlers[0].Handler["config"]; !ok {
		t.Error("expected \"config\" key in handler map")
	}
}

func TestPlugin_ContributeCaddyHandlers_ConfigRoundTrips(t *testing.T) {
	cfg := ipfilter.Config{
		Enabled:           true,
		Mode:              ipfilter.FilterModeBlocklist,
		Addresses:         []string{"10.0.0.0/8", "192.168.1.100"},
		TrustProxyHeaders: true,
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler")
	}

	rawConfig := handlers[0].Handler["config"]
	b, err := json.Marshal(rawConfig)
	if err != nil {
		t.Fatalf("json.Marshal(config) error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(config) error: %v", err)
	}

	if mode, ok := decoded["mode"].(string); !ok || mode != "blocklist" {
		t.Errorf("decoded config mode = %v, want %q", decoded["mode"], "blocklist")
	}

	addrs, ok := decoded["addresses"].([]any)
	if !ok {
		t.Fatalf("decoded config addresses is %T, want []any", decoded["addresses"])
	}
	if len(addrs) != 2 {
		t.Errorf("decoded config addresses len = %d, want 2", len(addrs))
	}

	if trust, ok := decoded["trust_proxy_headers"].(bool); !ok || !trust {
		t.Errorf("decoded config trust_proxy_headers = %v, want true", decoded["trust_proxy_headers"])
	}
}

func TestPlugin_ContributeCaddyHandlers_AllowlistMode(t *testing.T) {
	cfg := ipfilter.Config{
		Enabled:   true,
		Mode:      ipfilter.FilterModeAllowlist,
		Addresses: []string{"203.0.113.0/24"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler")
	}

	rawConfig := handlers[0].Handler["config"]
	b, _ := json.Marshal(rawConfig)
	var decoded map[string]any
	_ = json.Unmarshal(b, &decoded)

	if mode, ok := decoded["mode"].(string); !ok || mode != "allowlist" {
		t.Errorf("decoded config mode = %v, want %q", decoded["mode"], "allowlist")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*ipfilter.Plugin)(nil)
}

func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*ipfilter.Plugin)(nil)
}

func TestPlugin_ImplementsPluginMeta(t *testing.T) {
	var _ ports.PluginMeta = (*ipfilter.Plugin)(nil)
}
