package inputvalidation_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/inputvalidation"
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

func defaultConfig() inputvalidation.Config {
	return inputvalidation.Config{
		Enabled:              true,
		MaxURLLength:         2048,
		MaxQueryStringLength: 2048,
		MaxHeaderCount:       100,
		MaxHeaderSize:        8192,
	}
}

func newPlugin(cfg inputvalidation.Config) *inputvalidation.Plugin {
	return inputvalidation.New(cfg, discardLogger())
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Name(); got != "input-validation" {
		t.Errorf("Name() = %q, want %q", got, "input-validation")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Priority(); got != 18 {
		t.Errorf("Priority() = %d, want 18", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     inputvalidation.Config
		wantErr bool
	}{
		{
			name:    "disabled — no validation",
			cfg:     inputvalidation.Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled with valid config",
			cfg:     defaultConfig(),
			wantErr: false,
		},
		{
			name: "enabled with valid path override",
			cfg: inputvalidation.Config{
				Enabled:      true,
				MaxURLLength: 2048,
				PathOverrides: []inputvalidation.PathOverrideConfig{
					{Path: "/api/*", MaxURLLength: 4096},
				},
			},
			wantErr: false,
		},
		{
			name: "enabled with empty path override path",
			cfg: inputvalidation.Config{
				Enabled:      true,
				MaxURLLength: 2048,
				PathOverrides: []inputvalidation.PathOverrideConfig{
					{Path: ""},
				},
			},
			wantErr: true,
		},
		{
			name: "enabled with invalid path override pattern",
			cfg: inputvalidation.Config{
				Enabled:      true,
				MaxURLLength: 2048,
				PathOverrides: []inputvalidation.PathOverrideConfig{
					{Path: "[invalid"},
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
		cfg            inputvalidation.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            inputvalidation.Config{Enabled: false},
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
		cfg  inputvalidation.Config
	}{
		{"disabled", inputvalidation.Config{Enabled: false}},
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
	p := newPlugin(inputvalidation.Config{Enabled: false})
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
	if h.Priority != 18 {
		t.Errorf("handler priority = %d, want 18", h.Priority)
	}
	if h.Handler["handler"] != "vibewarden_input_validation" {
		t.Errorf("handler name = %v, want \"vibewarden_input_validation\"", h.Handler["handler"])
	}
}

func TestPlugin_ContributeCaddyHandlers_ConfigContainsLimits(t *testing.T) {
	cfg := inputvalidation.Config{
		Enabled:              true,
		MaxURLLength:         1024,
		MaxQueryStringLength: 512,
		MaxHeaderCount:       50,
		MaxHeaderSize:        4096,
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
		MaxURLLength         int `json:"max_url_length"`
		MaxQueryStringLength int `json:"max_query_string_length"`
		MaxHeaderCount       int `json:"max_header_count"`
		MaxHeaderSize        int `json:"max_header_size"`
	}
	if err := json.Unmarshal(rawCfg, &hcfg); err != nil {
		t.Fatalf("json.Unmarshal config: %v", err)
	}

	if hcfg.MaxURLLength != 1024 {
		t.Errorf("max_url_length = %d, want 1024", hcfg.MaxURLLength)
	}
	if hcfg.MaxQueryStringLength != 512 {
		t.Errorf("max_query_string_length = %d, want 512", hcfg.MaxQueryStringLength)
	}
	if hcfg.MaxHeaderCount != 50 {
		t.Errorf("max_header_count = %d, want 50", hcfg.MaxHeaderCount)
	}
	if hcfg.MaxHeaderSize != 4096 {
		t.Errorf("max_header_size = %d, want 4096", hcfg.MaxHeaderSize)
	}
}

func TestPlugin_ContributeCaddyHandlers_PathOverridesInConfig(t *testing.T) {
	cfg := inputvalidation.Config{
		Enabled:      true,
		MaxURLLength: 2048,
		PathOverrides: []inputvalidation.PathOverrideConfig{
			{
				Path:         "/api/upload",
				MaxURLLength: 8192,
			},
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
		PathOverrides []struct {
			Path         string `json:"path"`
			MaxURLLength int    `json:"max_url_length"`
		} `json:"path_overrides"`
	}
	if err := json.Unmarshal(rawCfg, &hcfg); err != nil {
		t.Fatalf("json.Unmarshal config: %v", err)
	}
	if len(hcfg.PathOverrides) != 1 {
		t.Fatalf("path_overrides count = %d, want 1", len(hcfg.PathOverrides))
	}
	if hcfg.PathOverrides[0].Path != "/api/upload" {
		t.Errorf("path_overrides[0].path = %q, want \"/api/upload\"", hcfg.PathOverrides[0].Path)
	}
	if hcfg.PathOverrides[0].MaxURLLength != 8192 {
		t.Errorf("path_overrides[0].max_url_length = %d, want 8192", hcfg.PathOverrides[0].MaxURLLength)
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
		"enabled",
		"max_url_length",
		"max_query_string_length",
		"max_header_count",
		"max_header_size",
		"path_overrides[].path",
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
	var _ ports.Plugin = (*inputvalidation.Plugin)(nil)
}

func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*inputvalidation.Plugin)(nil)
}

func TestPlugin_ImplementsPluginMeta(t *testing.T) {
	var _ ports.PluginMeta = (*inputvalidation.Plugin)(nil)
}
