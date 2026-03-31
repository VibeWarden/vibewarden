package responseheaders_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/responseheaders"
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

func newPlugin(cfg responseheaders.Config) *responseheaders.Plugin {
	return responseheaders.New(cfg, discardLogger())
}

// responseSection extracts the "response" sub-map from a Caddy headers handler.
func responseSection(t *testing.T, handler map[string]any) map[string]any {
	t.Helper()
	resp, ok := handler["response"].(map[string]any)
	if !ok {
		t.Fatalf("expected response key to be map[string]any, got %T", handler["response"])
	}
	return resp
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(responseheaders.Config{})
	if got := p.Name(); got != "response-headers" {
		t.Errorf("Name() = %q, want %q", got, "response-headers")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(responseheaders.Config{})
	if got := p.Priority(); got != 25 {
		t.Errorf("Priority() = %d, want 25", got)
	}
}

// ---------------------------------------------------------------------------
// Init — no-op, always succeeds
// ---------------------------------------------------------------------------

func TestPlugin_Init_AlwaysSucceeds(t *testing.T) {
	tests := []struct {
		name string
		cfg  responseheaders.Config
	}{
		{"empty config", responseheaders.Config{}},
		{
			"full config",
			responseheaders.Config{
				Set:    map[string]string{"X-Foo": "bar"},
				Add:    map[string]string{"Cache-Control": "no-store"},
				Remove: []string{"Server"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			if err := p.Init(context.Background()); err != nil {
				t.Errorf("Init() unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Start / Stop — no-ops
// ---------------------------------------------------------------------------

func TestPlugin_Start_IsNoop(t *testing.T) {
	p := newPlugin(responseheaders.Config{Set: map[string]string{"X-A": "1"}})
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() unexpected error: %v", err)
	}
}

func TestPlugin_Stop_IsNoop(t *testing.T) {
	p := newPlugin(responseheaders.Config{Remove: []string{"Server"}})
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
		cfg            responseheaders.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "inactive — empty config",
			cfg:            responseheaders.Config{},
			wantHealthy:    true,
			wantMsgContain: "inactive",
		},
		{
			name:           "active — has set rules",
			cfg:            responseheaders.Config{Set: map[string]string{"X-A": "1"}},
			wantHealthy:    true,
			wantMsgContain: "configured",
		},
		{
			name:           "active — has add rules",
			cfg:            responseheaders.Config{Add: map[string]string{"X-B": "2"}},
			wantHealthy:    true,
			wantMsgContain: "configured",
		},
		{
			name:           "active — has remove rules",
			cfg:            responseheaders.Config{Remove: []string{"Server"}},
			wantHealthy:    true,
			wantMsgContain: "configured",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			h := p.Health()
			if h.Healthy != tt.wantHealthy {
				t.Errorf("Health().Healthy = %v, want %v", h.Healthy, tt.wantHealthy)
			}
			if tt.wantMsgContain != "" {
				found := false
				for i := 0; i < len(h.Message)-len(tt.wantMsgContain)+1; i++ {
					if h.Message[i:i+len(tt.wantMsgContain)] == tt.wantMsgContain {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Health().Message = %q, want it to contain %q", h.Message, tt.wantMsgContain)
				}
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
		cfg  responseheaders.Config
	}{
		{"empty config", responseheaders.Config{}},
		{"active config", responseheaders.Config{Remove: []string{"Server"}}},
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

func TestPlugin_ContributeCaddyHandlers_InactiveReturnsNil(t *testing.T) {
	p := newPlugin(responseheaders.Config{})
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() = %v, want empty when no rules configured", handlers)
	}
}

func TestPlugin_ContributeCaddyHandlers_ActiveReturnsOne(t *testing.T) {
	p := newPlugin(responseheaders.Config{Remove: []string{"Server"}})
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("ContributeCaddyHandlers() len = %d, want 1", len(handlers))
	}
	if handlers[0].Priority != 25 {
		t.Errorf("handler Priority = %d, want 25", handlers[0].Priority)
	}
	if handlers[0].Handler["handler"] != "headers" {
		t.Errorf("handler[\"handler\"] = %v, want \"headers\"", handlers[0].Handler["handler"])
	}
}

func TestPlugin_ContributeCaddyHandlers_Remove(t *testing.T) {
	cfg := responseheaders.Config{
		Remove: []string{"Server", "X-Powered-By"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler")
	}

	resp := responseSection(t, handlers[0].Handler)
	del, ok := resp["delete"].([]string)
	if !ok {
		t.Fatalf("response.delete = %T, want []string", resp["delete"])
	}
	if len(del) != 2 {
		t.Errorf("response.delete len = %d, want 2", len(del))
	}
	for _, want := range []string{"Server", "X-Powered-By"} {
		found := false
		for _, got := range del {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("response.delete does not contain %q; got %v", want, del)
		}
	}
}

func TestPlugin_ContributeCaddyHandlers_Set(t *testing.T) {
	cfg := responseheaders.Config{
		Set: map[string]string{
			"X-Service-Version": "1.2.3",
			"X-Frame-Options":   "SAMEORIGIN",
		},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler")
	}

	resp := responseSection(t, handlers[0].Handler)
	set, ok := resp["set"].(map[string][]string)
	if !ok {
		t.Fatalf("response.set = %T, want map[string][]string", resp["set"])
	}

	tests := []struct {
		header string
		want   string
	}{
		{"X-Service-Version", "1.2.3"},
		{"X-Frame-Options", "SAMEORIGIN"},
	}
	for _, tt := range tests {
		vals, exists := set[tt.header]
		if !exists {
			t.Errorf("response.set missing header %q", tt.header)
			continue
		}
		if len(vals) != 1 || vals[0] != tt.want {
			t.Errorf("response.set[%q] = %v, want [%q]", tt.header, vals, tt.want)
		}
	}
}

func TestPlugin_ContributeCaddyHandlers_Add(t *testing.T) {
	cfg := responseheaders.Config{
		Add: map[string]string{
			"Cache-Control": "no-store",
		},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler")
	}

	resp := responseSection(t, handlers[0].Handler)
	add, ok := resp["add"].(map[string][]string)
	if !ok {
		t.Fatalf("response.add = %T, want map[string][]string", resp["add"])
	}

	vals, exists := add["Cache-Control"]
	if !exists {
		t.Fatal("response.add missing Cache-Control header")
	}
	if len(vals) != 1 || vals[0] != "no-store" {
		t.Errorf("response.add[Cache-Control] = %v, want [\"no-store\"]", vals)
	}
}

func TestPlugin_ContributeCaddyHandlers_NoDeleteKeyWhenRemoveEmpty(t *testing.T) {
	cfg := responseheaders.Config{
		Set: map[string]string{"X-Foo": "bar"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	resp := responseSection(t, handlers[0].Handler)
	if _, hasDelete := resp["delete"]; hasDelete {
		t.Error("response.delete should not be set when Remove is empty")
	}
}

func TestPlugin_ContributeCaddyHandlers_NoSetKeyWhenSetEmpty(t *testing.T) {
	cfg := responseheaders.Config{
		Remove: []string{"Server"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	resp := responseSection(t, handlers[0].Handler)
	if _, hasSet := resp["set"]; hasSet {
		t.Error("response.set should not be set when Set map is empty")
	}
}

func TestPlugin_ContributeCaddyHandlers_NoAddKeyWhenAddEmpty(t *testing.T) {
	cfg := responseheaders.Config{
		Remove: []string{"Server"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	resp := responseSection(t, handlers[0].Handler)
	if _, hasAdd := resp["add"]; hasAdd {
		t.Error("response.add should not be set when Add map is empty")
	}
}

func TestPlugin_ContributeCaddyHandlers_AllOperationsTogether(t *testing.T) {
	cfg := responseheaders.Config{
		Set:    map[string]string{"X-Version": "2"},
		Add:    map[string]string{"X-Extra": "yes"},
		Remove: []string{"Server"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(handlers))
	}

	resp := responseSection(t, handlers[0].Handler)

	if _, ok := resp["delete"]; !ok {
		t.Error("expected response.delete to be set")
	}
	if _, ok := resp["set"]; !ok {
		t.Error("expected response.set to be set")
	}
	if _, ok := resp["add"]; !ok {
		t.Error("expected response.add to be set")
	}
}

func TestPlugin_ContributeCaddyHandlers_EnvVarValuePassedThrough(t *testing.T) {
	// Caddy resolves ${VAR} placeholders at request time; the plugin must pass
	// the raw string through unchanged so Caddy can perform substitution.
	cfg := responseheaders.Config{
		Set: map[string]string{
			"X-Service-Version": "${APP_VERSION}",
		},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	resp := responseSection(t, handlers[0].Handler)
	set, ok := resp["set"].(map[string][]string)
	if !ok {
		t.Fatalf("response.set = %T, want map[string][]string", resp["set"])
	}
	vals := set["X-Service-Version"]
	if len(vals) != 1 || vals[0] != "${APP_VERSION}" {
		t.Errorf("X-Service-Version = %v, want [\"${APP_VERSION}\"]", vals)
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*responseheaders.Plugin)(nil)
}

func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*responseheaders.Plugin)(nil)
}

func TestPlugin_ImplementsPluginMeta(t *testing.T) {
	var _ ports.PluginMeta = (*responseheaders.Plugin)(nil)
}
