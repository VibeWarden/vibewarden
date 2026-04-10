package maintenance

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestPlugin_Name(t *testing.T) {
	p := New(Config{}, discardLogger())
	if p.Name() != "maintenance" {
		t.Errorf("Name() = %q, want %q", p.Name(), "maintenance")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := New(Config{}, discardLogger())
	if p.Priority() != 5 {
		t.Errorf("Priority() = %d, want 5", p.Priority())
	}
}

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "disabled — no error",
			cfg:     Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled with message — no error",
			cfg:     Config{Enabled: true, Message: "Upgrading database"},
			wantErr: false,
		},
		{
			name:    "enabled with empty message — no error",
			cfg:     Config{Enabled: true, Message: ""},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.cfg, discardLogger())
			err := p.Init(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPlugin_Health(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		wantMsg string
	}{
		{
			name:    "disabled",
			enabled: false,
			wantMsg: "maintenance mode disabled",
		},
		{
			name:    "enabled",
			enabled: true,
			wantMsg: "maintenance mode active — traffic is blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(Config{Enabled: tt.enabled}, discardLogger())
			hs := p.Health()
			if !hs.Healthy {
				t.Error("Health().Healthy should always be true")
			}
			if hs.Message != tt.wantMsg {
				t.Errorf("Health().Message = %q, want %q", hs.Message, tt.wantMsg)
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_disabled(t *testing.T) {
	p := New(Config{Enabled: false}, discardLogger())
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() returned %d handlers, want 0 when disabled", len(handlers))
	}
}

func TestPlugin_ContributeCaddyHandlers_enabled(t *testing.T) {
	tests := []struct {
		name    string
		message string
		wantMsg string
	}{
		{
			name:    "with explicit message",
			message: "Upgrading database schema",
			wantMsg: "Upgrading database schema",
		},
		{
			name:    "with empty message uses default",
			message: "",
			wantMsg: "Service is under maintenance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(Config{Enabled: true, Message: tt.message}, discardLogger())
			handlers := p.ContributeCaddyHandlers()

			if len(handlers) != 1 {
				t.Fatalf("ContributeCaddyHandlers() returned %d handlers, want 1", len(handlers))
			}

			h := handlers[0]
			if h.Priority != 5 {
				t.Errorf("handler Priority = %d, want 5", h.Priority)
			}

			handlerName, _ := h.Handler["handler"].(string)
			if handlerName != "vibewarden_maintenance" {
				t.Errorf("handler[\"handler\"] = %q, want %q", handlerName, "vibewarden_maintenance")
			}

			rawCfg, ok := h.Handler["config"].(json.RawMessage)
			if !ok {
				t.Fatal("handler[\"config\"] is not json.RawMessage")
			}

			var hcfg maintenanceHandlerConfig
			if err := json.Unmarshal(rawCfg, &hcfg); err != nil {
				t.Fatalf("failed to unmarshal handler config: %v", err)
			}
			if hcfg.Message != tt.wantMsg {
				t.Errorf("handler config message = %q, want %q", hcfg.Message, tt.wantMsg)
			}
		})
	}
}

func TestPlugin_ContributeCaddyRoutes(t *testing.T) {
	p := New(Config{Enabled: true}, discardLogger())
	if routes := p.ContributeCaddyRoutes(); routes != nil {
		t.Errorf("ContributeCaddyRoutes() = %v, want nil", routes)
	}
}

func TestPlugin_StartStop(t *testing.T) {
	p := New(Config{Enabled: true}, discardLogger())
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Errorf("Start() returned error: %v", err)
	}
	if err := p.Stop(ctx); err != nil {
		t.Errorf("Stop() returned error: %v", err)
	}
}

// Compile-time interface guards.
var (
	_ ports.Plugin           = (*Plugin)(nil)
	_ ports.CaddyContributor = (*Plugin)(nil)
)

// testLogger is defined but slog.DiscardHandler requires Go 1.24 — use a null writer for older compat.
func init() {
	_ = testLogger // suppress unused warning; testLogger is available for future use
}
