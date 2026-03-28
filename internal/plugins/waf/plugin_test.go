package waf_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/waf"
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

func defaultConfig() waf.Config {
	return waf.Config{
		ContentTypeValidation: waf.ContentTypeValidationConfig{
			Enabled: true,
			Allowed: []string{
				"application/json",
				"application/x-www-form-urlencoded",
				"multipart/form-data",
			},
		},
	}
}

func newPlugin(cfg waf.Config) *waf.Plugin {
	return waf.New(cfg, discardLogger())
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Name(); got != "waf" {
		t.Errorf("Name() = %q, want %q", got, "waf")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Priority(); got != 25 {
		t.Errorf("Priority() = %d, want 25", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     waf.Config
		wantErr bool
	}{
		{
			name:    "disabled — no validation",
			cfg:     waf.Config{ContentTypeValidation: waf.ContentTypeValidationConfig{Enabled: false}},
			wantErr: false,
		},
		{
			name:    "enabled with allowed types",
			cfg:     defaultConfig(),
			wantErr: false,
		},
		{
			name: "enabled with empty allowed list — error",
			cfg: waf.Config{
				ContentTypeValidation: waf.ContentTypeValidationConfig{
					Enabled: true,
					Allowed: []string{},
				},
			},
			wantErr: true,
		},
		{
			name: "enabled with nil allowed list — error",
			cfg: waf.Config{
				ContentTypeValidation: waf.ContentTypeValidationConfig{
					Enabled: true,
					Allowed: nil,
				},
			},
			wantErr: true,
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
// Start / Stop — no-ops
// ---------------------------------------------------------------------------

func TestPlugin_Start_IsNoop(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() unexpected error: %v", err)
	}
}

func TestPlugin_Stop_IsNoop(t *testing.T) {
	p := newPlugin(defaultConfig())
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
		cfg            waf.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            waf.Config{ContentTypeValidation: waf.ContentTypeValidationConfig{Enabled: false}},
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

// ---------------------------------------------------------------------------
// ContributeCaddyRoutes
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_AlwaysEmpty(t *testing.T) {
	tests := []struct {
		name string
		cfg  waf.Config
	}{
		{"disabled", waf.Config{}},
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
	p := newPlugin(waf.Config{ContentTypeValidation: waf.ContentTypeValidationConfig{Enabled: false}})
	if handlers := p.ContributeCaddyHandlers(); len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() = %v, want empty when disabled", handlers)
	}
}

func TestPlugin_ContributeCaddyHandlers_EnabledReturnsOneHandler(t *testing.T) {
	p := newPlugin(defaultConfig())
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("ContributeCaddyHandlers() len = %d, want 1", len(handlers))
	}

	h := handlers[0]
	if h.Priority != 25 {
		t.Errorf("handler priority = %d, want 25", h.Priority)
	}
	if h.Handler["handler"] != "vibewarden_waf_content_type" {
		t.Errorf("handler name = %v, want \"vibewarden_waf_content_type\"", h.Handler["handler"])
	}
}

func TestPlugin_ContributeCaddyHandlers_ConfigContainsAllowedTypes(t *testing.T) {
	cfg := waf.Config{
		ContentTypeValidation: waf.ContentTypeValidationConfig{
			Enabled: true,
			Allowed: []string{"application/json", "text/plain"},
		},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(handlers))
	}

	rawCfg, ok := handlers[0].Handler["config"].(json.RawMessage)
	if !ok {
		t.Fatalf("expected json.RawMessage under \"config\" key, got %T", handlers[0].Handler["config"])
	}

	var hcfg struct {
		Allowed []string `json:"allowed"`
	}
	if err := json.Unmarshal(rawCfg, &hcfg); err != nil {
		t.Fatalf("json.Unmarshal config: %v", err)
	}

	if len(hcfg.Allowed) != 2 {
		t.Fatalf("allowed count = %d, want 2", len(hcfg.Allowed))
	}
	if hcfg.Allowed[0] != "application/json" {
		t.Errorf("allowed[0] = %q, want \"application/json\"", hcfg.Allowed[0])
	}
	if hcfg.Allowed[1] != "text/plain" {
		t.Errorf("allowed[1] = %q, want \"text/plain\"", hcfg.Allowed[1])
	}
}

// ---------------------------------------------------------------------------
// Meta
// ---------------------------------------------------------------------------

func TestPlugin_Description_NonEmpty(t *testing.T) {
	p := newPlugin(defaultConfig())
	if p.Description() == "" {
		t.Error("Description() must not be empty")
	}
}

func TestPlugin_ConfigSchema_HasExpectedKeys(t *testing.T) {
	p := newPlugin(defaultConfig())
	schema := p.ConfigSchema()

	required := []string{
		"content_type_validation.enabled",
		"content_type_validation.allowed",
	}
	for _, key := range required {
		if _, ok := schema[key]; !ok {
			t.Errorf("ConfigSchema() missing key %q", key)
		}
	}
}

func TestPlugin_Example_NonEmpty(t *testing.T) {
	p := newPlugin(defaultConfig())
	if p.Example() == "" {
		t.Error("Example() must not be empty")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*waf.Plugin)(nil)
}

func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*waf.Plugin)(nil)
}

func TestPlugin_ImplementsPluginMeta(t *testing.T) {
	var _ ports.PluginMeta = (*waf.Plugin)(nil)
}
