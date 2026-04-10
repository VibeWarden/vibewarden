package metrics_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/metrics"
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

func enabledConfig() metrics.Config {
	return metrics.Config{
		Enabled:           true,
		PathPatterns:      []string{"/users/:id", "/api/v1/*"},
		PrometheusEnabled: true,
	}
}

func disabledConfig() metrics.Config {
	return metrics.Config{Enabled: false}
}

func newPlugin(cfg metrics.Config) *metrics.Plugin {
	return metrics.New(cfg, discardLogger())
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(enabledConfig())
	if got := p.Name(); got != "metrics" {
		t.Errorf("Name() = %q, want %q", got, "metrics")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(enabledConfig())
	if got := p.Priority(); got != 30 {
		t.Errorf("Priority() = %d, want 30", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     metrics.Config
		wantErr bool
	}{
		{"enabled", enabledConfig(), false},
		{"disabled", disabledConfig(), false},
		{"enabled no patterns", metrics.Config{Enabled: true, PrometheusEnabled: true}, false},
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
// Start / Stop lifecycle
// ---------------------------------------------------------------------------

func TestPlugin_Start_Disabled_IsNoop(t *testing.T) {
	p := newPlugin(disabledConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() unexpected error for disabled plugin: %v", err)
	}
	if p.InternalAddr() != "" {
		t.Errorf("InternalAddr() = %q, want empty for disabled plugin", p.InternalAddr())
	}
}

func TestPlugin_Stop_BeforeStart_IsNoop(t *testing.T) {
	p := newPlugin(enabledConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	// Stop without Start — must not panic or return error.
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() before Start returned unexpected error: %v", err)
	}
}

func TestPlugin_StartStop_Enabled(t *testing.T) {
	p := newPlugin(enabledConfig())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() {
		_ = p.Stop(context.Background())
	})

	addr := p.InternalAddr()
	if addr == "" {
		t.Error("InternalAddr() returned empty string after Start")
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("InternalAddr() = %q, want 127.0.0.1:<port>", addr)
	}

	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestPlugin_Health(t *testing.T) {
	tests := []struct {
		name           string
		cfg            metrics.Config
		initAndStart   bool
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            disabledConfig(),
			wantHealthy:    true,
			wantMsgContain: "disabled",
		},
		{
			name:           "enabled not started",
			cfg:            enabledConfig(),
			wantHealthy:    true,
			wantMsgContain: "not started",
		},
		{
			name:           "enabled and running",
			cfg:            enabledConfig(),
			initAndStart:   true,
			wantHealthy:    true,
			wantMsgContain: "running",
		},
		{
			name:           "OTLP-only running",
			cfg:            otlpOnlyConfig(),
			initAndStart:   true,
			wantHealthy:    true,
			wantMsgContain: "running",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			if tt.initAndStart {
				if err := p.Init(context.Background()); err != nil {
					t.Fatalf("Init() error: %v", err)
				}
				if err := p.Start(context.Background()); err != nil {
					t.Fatalf("Start() error: %v", err)
				}
				t.Cleanup(func() { _ = p.Stop(context.Background()) })
			}
			h := p.Health()
			if h.Healthy != tt.wantHealthy {
				t.Errorf("Health().Healthy = %v, want %v", h.Healthy, tt.wantHealthy)
			}
			if !strings.Contains(h.Message, tt.wantMsgContain) {
				t.Errorf("Health().Message = %q, want to contain %q", h.Message, tt.wantMsgContain)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ContributeCaddyRoutes
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_Disabled(t *testing.T) {
	p := newPlugin(disabledConfig())
	if routes := p.ContributeCaddyRoutes(); len(routes) != 0 {
		t.Errorf("ContributeCaddyRoutes() disabled = %v, want empty", routes)
	}
}

func TestPlugin_ContributeCaddyRoutes_EnabledNotStarted(t *testing.T) {
	p := newPlugin(enabledConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	// Not started yet — InternalAddr is empty, should return nil.
	if routes := p.ContributeCaddyRoutes(); len(routes) != 0 {
		t.Errorf("ContributeCaddyRoutes() before Start = %v, want empty", routes)
	}
}

func TestPlugin_ContributeCaddyRoutes_AfterStart(t *testing.T) {
	p := newPlugin(enabledConfig())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	routes := p.ContributeCaddyRoutes()
	if len(routes) != 1 {
		t.Fatalf("ContributeCaddyRoutes() len = %d, want 1", len(routes))
	}

	route := routes[0]
	if route.MatchPath != "/_vibewarden/metrics" {
		t.Errorf("route.MatchPath = %q, want %q", route.MatchPath, "/_vibewarden/metrics")
	}
	if route.Priority != 30 {
		t.Errorf("route.Priority = %d, want 30", route.Priority)
	}
}

func TestPlugin_ContributeCaddyRoutes_RouteStructure(t *testing.T) {
	p := newPlugin(enabledConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	routes := p.ContributeCaddyRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}

	h := routes[0].Handler

	// Must have a "match" with path /_vibewarden/metrics.
	match, ok := h["match"].([]map[string]any)
	if !ok || len(match) == 0 {
		t.Fatal("route handler missing 'match' key")
	}
	paths, ok := match[0]["path"].([]string)
	if !ok || len(paths) == 0 {
		t.Fatal("match missing 'path' key")
	}
	if paths[0] != "/_vibewarden/metrics" {
		t.Errorf("match path = %q, want %q", paths[0], "/_vibewarden/metrics")
	}

	// Must have a "handle" slice with at least rewrite + reverse_proxy.
	handle, ok := h["handle"].([]map[string]any)
	if !ok || len(handle) < 2 {
		t.Fatalf("expected at least 2 handlers (rewrite + reverse_proxy) in route, got %v", h["handle"])
	}

	// First handler must be a rewrite to /metrics.
	if handle[0]["handler"] != "rewrite" {
		t.Errorf("handle[0].handler = %v, want %q", handle[0]["handler"], "rewrite")
	}
	if handle[0]["uri"] != "/metrics" {
		t.Errorf("handle[0].uri = %v, want %q", handle[0]["uri"], "/metrics")
	}

	// Second handler must be a reverse_proxy with the correct upstream dial.
	if handle[1]["handler"] != "reverse_proxy" {
		t.Errorf("handle[1].handler = %v, want %q", handle[1]["handler"], "reverse_proxy")
	}
	upstreams, ok := handle[1]["upstreams"].([]map[string]any)
	if !ok || len(upstreams) == 0 {
		t.Fatal("reverse_proxy handler missing 'upstreams'")
	}
	dial, _ := upstreams[0]["dial"].(string)
	if dial != p.InternalAddr() {
		t.Errorf("upstream dial = %q, want %q", dial, p.InternalAddr())
	}
}

// ---------------------------------------------------------------------------
// ContributeCaddyHandlers
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyHandlers_AlwaysNil(t *testing.T) {
	tests := []struct {
		name string
		cfg  metrics.Config
	}{
		{"disabled", disabledConfig()},
		{"enabled", enabledConfig()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			if h := p.ContributeCaddyHandlers(); len(h) != 0 {
				t.Errorf("ContributeCaddyHandlers() = %v, want empty", h)
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
	var _ ports.Plugin = (*metrics.Plugin)(nil)
}

// TestPlugin_ImplementsCaddyContributor asserts at compile time that *Plugin
// satisfies the ports.CaddyContributor interface.
func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*metrics.Plugin)(nil)
}

// TestPlugin_ImplementsInternalServerPlugin asserts at compile time that *Plugin
// satisfies the ports.InternalServerPlugin interface.
func TestPlugin_ImplementsInternalServerPlugin(t *testing.T) {
	var _ ports.InternalServerPlugin = (*metrics.Plugin)(nil)
}

func TestPlugin_Collector_DisabledReturnsNoOp(t *testing.T) {
	p := metrics.New(metrics.Config{Enabled: false}, slog.New(slog.NewTextHandler(&noopWriter{}, nil)))
	c := p.Collector()
	if c == nil {
		t.Fatal("Collector() returned nil, want NoOpMetricsCollector")
	}
	// NoOp should not panic when called.
	c.IncRequestTotal("GET", "200", "/test")
}

func TestPlugin_Collector_EnabledReturnsAdapter(t *testing.T) {
	p := metrics.New(metrics.Config{Enabled: true, PrometheusEnabled: true}, slog.New(slog.NewTextHandler(&noopWriter{}, nil)))
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer p.Stop(context.Background()) //nolint:errcheck

	c := p.Collector()
	if c == nil {
		t.Fatal("Collector() returned nil after Start")
	}
	// Should not panic when recording.
	c.IncRequestTotal("GET", "200", "/test")
}

// ---------------------------------------------------------------------------
// OTLP-only mode
// ---------------------------------------------------------------------------

func otlpOnlyConfig() metrics.Config {
	return metrics.Config{
		Enabled:           true,
		PrometheusEnabled: false,
		OTLPEnabled:       true,
		OTLPEndpoint:      "http://localhost:4318",
		OTLPProtocol:      "http",
		OTLPInterval:      "30s",
	}
}

func TestPlugin_OTLPOnly_Init(t *testing.T) {
	p := newPlugin(otlpOnlyConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() with OTLP-only config: %v", err)
	}
}

func TestPlugin_OTLPOnly_StartIsNoOp(t *testing.T) {
	p := newPlugin(otlpOnlyConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	// Start should succeed without starting an internal HTTP server.
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error in OTLP-only mode: %v", err)
	}
	defer p.Stop(context.Background()) //nolint:errcheck

	// InternalAddr should be empty — no Prometheus server running.
	if addr := p.InternalAddr(); addr != "" {
		t.Errorf("InternalAddr() = %q, want empty in OTLP-only mode", addr)
	}
}

func TestPlugin_OTLPOnly_ContributeCaddyRoutes_ReturnsNil(t *testing.T) {
	p := newPlugin(otlpOnlyConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer p.Stop(context.Background()) //nolint:errcheck

	routes := p.ContributeCaddyRoutes()
	if len(routes) != 0 {
		t.Errorf("ContributeCaddyRoutes() returned %d routes in OTLP-only mode, want 0", len(routes))
	}
}

// ---------------------------------------------------------------------------
// Invalid duration parsing
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// LogHandler
// ---------------------------------------------------------------------------

func TestPlugin_LogHandler_DisabledReturnsNil(t *testing.T) {
	p := newPlugin(disabledConfig())
	if h := p.LogHandler(); h != nil {
		t.Errorf("LogHandler() = %v, want nil when disabled", h)
	}
}

func TestPlugin_LogHandler_NoLogsConfigReturnsNil(t *testing.T) {
	cfg := enabledConfig()
	cfg.LogsOTLPEnabled = false
	p := newPlugin(cfg)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if h := p.LogHandler(); h != nil {
		t.Errorf("LogHandler() = %v, want nil when logs not enabled", h)
	}
}

func TestPlugin_Init_InvalidOTLPInterval(t *testing.T) {
	cfg := metrics.Config{
		Enabled:           true,
		PrometheusEnabled: true,
		OTLPEnabled:       true,
		OTLPEndpoint:      "http://localhost:4318",
		OTLPInterval:      "not-a-duration",
	}
	p := newPlugin(cfg)
	err := p.Init(context.Background())
	if err == nil {
		t.Fatal("Init() should fail with invalid OTLP interval")
	}
	if !strings.Contains(err.Error(), "invalid interval duration") {
		t.Errorf("error = %v, want to contain 'invalid interval duration'", err)
	}
}
