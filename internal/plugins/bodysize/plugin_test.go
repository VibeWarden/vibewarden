package bodysize_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/bodysize"
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
func defaultConfig() bodysize.Config {
	return bodysize.Config{
		Enabled:  true,
		MaxBytes: 1048576, // 1MB
	}
}

func newPlugin(cfg bodysize.Config) *bodysize.Plugin {
	return bodysize.New(cfg, discardLogger())
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Name(); got != "body-size" {
		t.Errorf("Name() = %q, want %q", got, "body-size")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Priority(); got != 45 {
		t.Errorf("Priority() = %d, want 45", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     bodysize.Config
		wantErr bool
	}{
		{
			name:    "disabled — no-op",
			cfg:     bodysize.Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled with global limit only",
			cfg:     defaultConfig(),
			wantErr: false,
		},
		{
			name: "enabled with overrides",
			cfg: bodysize.Config{
				Enabled:  true,
				MaxBytes: 1048576,
				Overrides: []bodysize.OverrideConfig{
					{Path: "/api/upload", MaxBytes: 52428800},
				},
			},
			wantErr: false,
		},
		{
			name: "enabled with zero max bytes (no global limit)",
			cfg: bodysize.Config{
				Enabled:  true,
				MaxBytes: 0,
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
// Start — no-op
// ---------------------------------------------------------------------------

func TestPlugin_Start_IsNoop(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Stop — no-op
// ---------------------------------------------------------------------------

func TestPlugin_Stop_IsNoop(t *testing.T) {
	tests := []struct {
		name string
		cfg  bodysize.Config
	}{
		{"disabled", bodysize.Config{Enabled: false}},
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
		cfg            bodysize.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            bodysize.Config{Enabled: false},
			wantHealthy:    true,
			wantMsgContain: "disabled",
		},
		{
			name:           "enabled",
			cfg:            defaultConfig(),
			wantHealthy:    true,
			wantMsgContain: "active",
		},
		{
			name: "enabled with overrides",
			cfg: bodysize.Config{
				Enabled:  true,
				MaxBytes: 1048576,
				Overrides: []bodysize.OverrideConfig{
					{Path: "/api/upload", MaxBytes: 52428800},
					{Path: "/api/avatar", MaxBytes: 5242880},
				},
			},
			wantHealthy:    true,
			wantMsgContain: "2 path overrides",
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
		cfg  bodysize.Config
	}{
		{"disabled", bodysize.Config{Enabled: false}},
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
	p := newPlugin(bodysize.Config{Enabled: false})
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
	if handlers[0].Priority != 45 {
		t.Errorf("handler Priority = %d, want 45", handlers[0].Priority)
	}
	if handlers[0].Handler["handler"] != "vibewarden_body_size" {
		t.Errorf("handler[\"handler\"] = %v, want %q", handlers[0].Handler["handler"], "vibewarden_body_size")
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

func TestPlugin_ContributeCaddyHandlers_ConfigDeserialises(t *testing.T) {
	cfg := bodysize.Config{
		Enabled:  true,
		MaxBytes: 1048576,
		Overrides: []bodysize.OverrideConfig{
			{Path: "/api/upload", MaxBytes: 52428800},
		},
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

	// Verify max_bytes round-trips.
	maxBytes, ok := decoded["max_bytes"].(float64)
	if !ok {
		t.Fatalf("decoded config max_bytes is %T, want float64", decoded["max_bytes"])
	}
	if int64(maxBytes) != 1048576 {
		t.Errorf("decoded config max_bytes = %v, want 1048576", maxBytes)
	}

	// Verify overrides round-trip.
	overrides, ok := decoded["overrides"].([]any)
	if !ok {
		t.Fatalf("decoded config overrides is %T, want []any", decoded["overrides"])
	}
	if len(overrides) != 1 {
		t.Errorf("decoded config overrides len = %d, want 1", len(overrides))
	}
}

func TestPlugin_ContributeCaddyHandlers_NoOverridesOmitsField(t *testing.T) {
	p := newPlugin(bodysize.Config{Enabled: true, MaxBytes: 1048576})
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

	// When no overrides are set, the "overrides" key should be absent (omitempty).
	if _, present := decoded["overrides"]; present {
		t.Error("expected \"overrides\" to be omitted when empty, but it was present")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*bodysize.Plugin)(nil)
}

func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*bodysize.Plugin)(nil)
}

func TestPlugin_ImplementsPluginMeta(t *testing.T) {
	var _ ports.PluginMeta = (*bodysize.Plugin)(nil)
}
