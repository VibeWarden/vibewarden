package cors_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/cors"
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

func defaultConfig() cors.Config {
	return cors.Config{
		Enabled:          true,
		AllowedOrigins:   []string{"https://example.com"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		ExposedHeaders:   []string{"X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           3600,
	}
}

func newPlugin(cfg cors.Config) *cors.Plugin {
	return cors.New(cfg, discardLogger())
}

// responseHeaders extracts the set headers map from a Caddy headers handler.
func responseHeaders(t *testing.T, handler map[string]any) map[string][]string {
	t.Helper()
	resp, ok := handler["response"].(map[string]any)
	if !ok {
		t.Fatalf("expected response key in handler, got: %v", handler)
	}
	set, ok := resp["set"].(map[string][]string)
	if !ok {
		t.Fatalf("expected set key in response, got: %v", resp)
	}
	return set
}

// preflightHeaders extracts headers from the static_response handler nested
// inside the subroute preflight handler.
func preflightHeaders(t *testing.T, handler map[string]any) map[string][]string {
	t.Helper()
	routes, ok := handler["routes"].([]map[string]any)
	if !ok || len(routes) == 0 {
		t.Fatal("expected routes in subroute handler")
	}
	handles, ok := routes[0]["handle"].([]map[string]any)
	if !ok || len(handles) == 0 {
		t.Fatal("expected handle in preflight route")
	}
	hdrs, ok := handles[0]["headers"].(map[string][]string)
	if !ok {
		t.Fatalf("expected headers in static_response handler, got: %v", handles[0])
	}
	return hdrs
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Name(); got != "cors" {
		t.Errorf("Name() = %q, want %q", got, "cors")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Priority(); got != 10 {
		t.Errorf("Priority() = %d, want 10", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     cors.Config
		wantErr bool
	}{
		{
			name:    "disabled — skip validation",
			cfg:     cors.Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled with specific origins",
			cfg:     defaultConfig(),
			wantErr: false,
		},
		{
			name: "wildcard origin without credentials",
			cfg: cors.Config{
				Enabled:          true,
				AllowedOrigins:   []string{"*"},
				AllowCredentials: false,
			},
			wantErr: false,
		},
		{
			name: "wildcard with credentials — invalid",
			cfg: cors.Config{
				Enabled:          true,
				AllowedOrigins:   []string{"*"},
				AllowCredentials: true,
			},
			wantErr: true,
		},
		{
			name: "empty origins with credentials",
			cfg: cors.Config{
				Enabled:          true,
				AllowedOrigins:   []string{},
				AllowCredentials: true,
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
		cfg            cors.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            cors.Config{Enabled: false},
			wantHealthy:    true,
			wantMsgContain: "disabled",
		},
		{
			name:           "enabled",
			cfg:            cors.Config{Enabled: true},
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
		cfg  cors.Config
	}{
		{"disabled", cors.Config{Enabled: false}},
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
	p := newPlugin(cors.Config{Enabled: false})
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() = %v, want empty when disabled", handlers)
	}
}

func TestPlugin_ContributeCaddyHandlers_EnabledReturnsTwoHandlers(t *testing.T) {
	p := newPlugin(defaultConfig())
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 2 {
		t.Fatalf("ContributeCaddyHandlers() len = %d, want 2", len(handlers))
	}
	// First handler: preflight (subroute), priority 10.
	if handlers[0].Priority != 10 {
		t.Errorf("handlers[0].Priority = %d, want 10", handlers[0].Priority)
	}
	if handlers[0].Handler["handler"] != "subroute" {
		t.Errorf("handlers[0].Handler[\"handler\"] = %v, want \"subroute\"", handlers[0].Handler["handler"])
	}
	// Second handler: response headers, priority 11.
	if handlers[1].Priority != 11 {
		t.Errorf("handlers[1].Priority = %d, want 11", handlers[1].Priority)
	}
	if handlers[1].Handler["handler"] != "headers" {
		t.Errorf("handlers[1].Handler[\"handler\"] = %v, want \"headers\"", handlers[1].Handler["handler"])
	}
}

func TestPlugin_ContributeCaddyHandlers_WildcardOrigin(t *testing.T) {
	cfg := cors.Config{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(handlers))
	}

	// Response headers handler should have Access-Control-Allow-Origin: *
	hdrs := responseHeaders(t, handlers[1].Handler)
	origin, ok := hdrs["Access-Control-Allow-Origin"]
	if !ok || len(origin) == 0 || origin[0] != "*" {
		t.Errorf("Access-Control-Allow-Origin = %v, want [\"*\"]", origin)
	}

	// No Vary header for wildcard.
	if _, hasVary := hdrs["Vary"]; hasVary {
		t.Error("expected no Vary header for wildcard origin")
	}
}

func TestPlugin_ContributeCaddyHandlers_SpecificOrigin_AddsVary(t *testing.T) {
	cfg := cors.Config{
		Enabled:        true,
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(handlers))
	}

	// Response headers handler should have Vary: Origin for specific-origin configs.
	hdrs := responseHeaders(t, handlers[1].Handler)
	vary, ok := hdrs["Vary"]
	if !ok || len(vary) == 0 || vary[0] != "Origin" {
		t.Errorf("Vary = %v, want [\"Origin\"]", vary)
	}
}

func TestPlugin_ContributeCaddyHandlers_AllowedMethods(t *testing.T) {
	cfg := cors.Config{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	hdrs := responseHeaders(t, handlers[1].Handler)

	methods, ok := hdrs["Access-Control-Allow-Methods"]
	if !ok || len(methods) == 0 {
		t.Fatal("expected Access-Control-Allow-Methods header")
	}
	if !strings.Contains(methods[0], "GET") || !strings.Contains(methods[0], "POST") || !strings.Contains(methods[0], "PUT") {
		t.Errorf("Access-Control-Allow-Methods = %q, want GET, POST, PUT", methods[0])
	}
}

func TestPlugin_ContributeCaddyHandlers_AllowedHeaders(t *testing.T) {
	cfg := cors.Config{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	hdrs := responseHeaders(t, handlers[1].Handler)

	allowedHdrs, ok := hdrs["Access-Control-Allow-Headers"]
	if !ok || len(allowedHdrs) == 0 {
		t.Fatal("expected Access-Control-Allow-Headers header")
	}
	if !strings.Contains(allowedHdrs[0], "Content-Type") || !strings.Contains(allowedHdrs[0], "Authorization") {
		t.Errorf("Access-Control-Allow-Headers = %q, want Content-Type, Authorization", allowedHdrs[0])
	}
}

func TestPlugin_ContributeCaddyHandlers_ExposedHeaders(t *testing.T) {
	tests := []struct {
		name    string
		exposed []string
		wantSet bool
	}{
		{"exposed headers set", []string{"X-Request-Id"}, true},
		{"no exposed headers", []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := cors.Config{
				Enabled:        true,
				AllowedOrigins: []string{"*"},
				ExposedHeaders: tt.exposed,
			}
			p := newPlugin(cfg)
			handlers := p.ContributeCaddyHandlers()
			hdrs := responseHeaders(t, handlers[1].Handler)

			val, has := hdrs["Access-Control-Expose-Headers"]
			if tt.wantSet && !has {
				t.Error("expected Access-Control-Expose-Headers header")
			}
			if !tt.wantSet && has {
				t.Errorf("unexpected Access-Control-Expose-Headers header: %v", val)
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_AllowCredentials(t *testing.T) {
	tests := []struct {
		name             string
		allowCredentials bool
		wantSet          bool
	}{
		{"credentials enabled", true, true},
		{"credentials disabled", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := cors.Config{
				Enabled:          true,
				AllowedOrigins:   []string{"https://example.com"},
				AllowCredentials: tt.allowCredentials,
			}
			p := newPlugin(cfg)
			handlers := p.ContributeCaddyHandlers()
			hdrs := responseHeaders(t, handlers[1].Handler)

			val, has := hdrs["Access-Control-Allow-Credentials"]
			if tt.wantSet && !has {
				t.Error("expected Access-Control-Allow-Credentials header")
			}
			if !tt.wantSet && has {
				t.Errorf("unexpected Access-Control-Allow-Credentials header: %v", val)
			}
			if tt.wantSet {
				if len(val) == 0 || val[0] != "true" {
					t.Errorf("Access-Control-Allow-Credentials = %v, want [\"true\"]", val)
				}
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_MaxAge(t *testing.T) {
	tests := []struct {
		name    string
		maxAge  int
		wantSet bool
	}{
		{"max age set", 3600, true},
		{"max age zero — omit header", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := cors.Config{
				Enabled:        true,
				AllowedOrigins: []string{"*"},
				MaxAge:         tt.maxAge,
			}
			p := newPlugin(cfg)
			handlers := p.ContributeCaddyHandlers()
			hdrs := responseHeaders(t, handlers[1].Handler)

			val, has := hdrs["Access-Control-Max-Age"]
			if tt.wantSet && !has {
				t.Error("expected Access-Control-Max-Age header")
			}
			if !tt.wantSet && has {
				t.Errorf("unexpected Access-Control-Max-Age header: %v", val)
			}
			if tt.wantSet && (len(val) == 0 || val[0] != "3600") {
				t.Errorf("Access-Control-Max-Age = %v, want [\"3600\"]", val)
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_PreflightContainsOptionsMethod(t *testing.T) {
	p := newPlugin(defaultConfig())
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one handler")
	}

	preflightH := handlers[0].Handler
	routes, ok := preflightH["routes"].([]map[string]any)
	if !ok || len(routes) == 0 {
		t.Fatal("expected routes in subroute preflight handler")
	}
	matchers, ok := routes[0]["match"].([]map[string]any)
	if !ok || len(matchers) == 0 {
		t.Fatal("expected match in preflight route")
	}
	methods, ok := matchers[0]["method"].([]string)
	if !ok {
		t.Fatalf("expected method matcher, got %T", matchers[0]["method"])
	}
	found := false
	for _, m := range methods {
		if m == "OPTIONS" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("preflight route method matcher = %v, want to contain \"OPTIONS\"", methods)
	}
}

func TestPlugin_ContributeCaddyHandlers_PreflightStaticResponse204(t *testing.T) {
	p := newPlugin(defaultConfig())
	handlers := p.ContributeCaddyHandlers()

	routes := handlers[0].Handler["routes"].([]map[string]any)
	handle := routes[0]["handle"].([]map[string]any)
	sr := handle[0]

	if sr["handler"] != "static_response" {
		t.Errorf("preflight handler type = %v, want \"static_response\"", sr["handler"])
	}
	if sr["status_code"] != 204 {
		t.Errorf("preflight status_code = %v, want 204", sr["status_code"])
	}
}

func TestPlugin_ContributeCaddyHandlers_PreflightContainsCORSHeaders(t *testing.T) {
	cfg := cors.Config{
		Enabled:          true,
		AllowedOrigins:   []string{"https://example.com"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           600,
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	hdrs := preflightHeaders(t, handlers[0].Handler)

	if _, ok := hdrs["Access-Control-Allow-Origin"]; !ok {
		t.Error("expected Access-Control-Allow-Origin in preflight response")
	}
	if _, ok := hdrs["Access-Control-Allow-Methods"]; !ok {
		t.Error("expected Access-Control-Allow-Methods in preflight response")
	}
	if _, ok := hdrs["Access-Control-Allow-Headers"]; !ok {
		t.Error("expected Access-Control-Allow-Headers in preflight response")
	}
	if creds, ok := hdrs["Access-Control-Allow-Credentials"]; !ok || len(creds) == 0 || creds[0] != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %v, want [\"true\"]", creds)
	}
	if age, ok := hdrs["Access-Control-Max-Age"]; !ok || len(age) == 0 || age[0] != "600" {
		t.Errorf("Access-Control-Max-Age = %v, want [\"600\"]", age)
	}
}

func TestPlugin_ContributeCaddyHandlers_NoMethods_NoMethodsHeader(t *testing.T) {
	cfg := cors.Config{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{},
	}
	p := newPlugin(cfg)
	handlers := p.ContributeCaddyHandlers()
	hdrs := responseHeaders(t, handlers[1].Handler)

	if _, ok := hdrs["Access-Control-Allow-Methods"]; ok {
		t.Error("expected no Access-Control-Allow-Methods when AllowedMethods is empty")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

// TestPlugin_ImplementsPortsPlugin asserts at compile time that *Plugin
// satisfies the ports.Plugin interface.
func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*cors.Plugin)(nil)
}

// TestPlugin_ImplementsCaddyContributor asserts at compile time that *Plugin
// satisfies the ports.CaddyContributor interface.
func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*cors.Plugin)(nil)
}

// TestPlugin_ImplementsPluginMeta asserts at compile time that *Plugin
// satisfies the ports.PluginMeta interface.
func TestPlugin_ImplementsPluginMeta(t *testing.T) {
	var _ ports.PluginMeta = (*cors.Plugin)(nil)
}
