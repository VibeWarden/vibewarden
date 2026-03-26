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
		Enabled:               true,
		HSTSMaxAge:            31536000,
		HSTSIncludeSubDomains: true,
		HSTSPreload:           false,
		ContentTypeNosniff:    true,
		FrameOption:           "DENY",
		ContentSecurityPolicy: "default-src 'self'",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		PermissionsPolicy:     "",
	}
}

func newPlugin(cfg securityheaders.Config, tlsEnabled bool) *securityheaders.Plugin {
	return securityheaders.New(cfg, tlsEnabled, discardLogger())
}

// responseHeaders extracts the set headers map from a Caddy headers handler.
func responseHeaders(t *testing.T, handler map[string]any) map[string][]string {
	t.Helper()
	resp, ok := handler["response"].(map[string]any)
	if !ok {
		t.Fatal("expected response key in handler")
	}
	set, ok := resp["set"].(map[string][]string)
	if !ok {
		t.Fatal("expected set key in response")
	}
	return set
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

func TestPlugin_ContributeCaddyHandlers_DisabledReturnsNil(t *testing.T) {
	p := newPlugin(securityheaders.Config{Enabled: false}, false)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() = %v, want empty when disabled", handlers)
	}
}

func TestPlugin_ContributeCaddyHandlers_EnabledReturnsOne(t *testing.T) {
	p := newPlugin(defaultConfig(), false)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("ContributeCaddyHandlers() len = %d, want 1", len(handlers))
	}
	if handlers[0].Priority != 20 {
		t.Errorf("handler Priority = %d, want 20", handlers[0].Priority)
	}
	if handlers[0].Handler["handler"] != "headers" {
		t.Errorf("handler[\"handler\"] = %v, want \"headers\"", handlers[0].Handler["handler"])
	}
}

func TestPlugin_ContributeCaddyHandlers_HSTS(t *testing.T) {
	tests := []struct {
		name        string
		cfg         securityheaders.Config
		tlsEnabled  bool
		wantHSTS    bool
		wantContain string
	}{
		{
			name: "hsts included when tls enabled",
			cfg: securityheaders.Config{
				Enabled:    true,
				HSTSMaxAge: 31536000,
			},
			tlsEnabled:  true,
			wantHSTS:    true,
			wantContain: "max-age=31536000",
		},
		{
			name: "hsts excluded when tls disabled",
			cfg: securityheaders.Config{
				Enabled:    true,
				HSTSMaxAge: 31536000,
			},
			tlsEnabled: false,
			wantHSTS:   false,
		},
		{
			name: "hsts excluded when max age is zero",
			cfg: securityheaders.Config{
				Enabled:    true,
				HSTSMaxAge: 0,
			},
			tlsEnabled: true,
			wantHSTS:   false,
		},
		{
			name: "hsts with includeSubDomains",
			cfg: securityheaders.Config{
				Enabled:               true,
				HSTSMaxAge:            31536000,
				HSTSIncludeSubDomains: true,
			},
			tlsEnabled:  true,
			wantHSTS:    true,
			wantContain: "includeSubDomains",
		},
		{
			name: "hsts with preload",
			cfg: securityheaders.Config{
				Enabled:     true,
				HSTSMaxAge:  31536000,
				HSTSPreload: true,
			},
			tlsEnabled:  true,
			wantHSTS:    true,
			wantContain: "preload",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg, tt.tlsEnabled)
			handlers := p.ContributeCaddyHandlers()
			if len(handlers) == 0 {
				t.Fatal("expected at least one handler")
			}
			hdrs := responseHeaders(t, handlers[0].Handler)
			sts, hasHSTS := hdrs["Strict-Transport-Security"]
			if tt.wantHSTS && !hasHSTS {
				t.Error("expected Strict-Transport-Security header to be set")
			}
			if !tt.wantHSTS && hasHSTS {
				t.Errorf("unexpected Strict-Transport-Security header: %v", sts)
			}
			if tt.wantHSTS && tt.wantContain != "" {
				if len(sts) == 0 || !strings.Contains(sts[0], tt.wantContain) {
					t.Errorf("HSTS value = %q, want it to contain %q", sts, tt.wantContain)
				}
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_XContentTypeOptions(t *testing.T) {
	tests := []struct {
		name    string
		nosniff bool
		want    bool
	}{
		{"nosniff enabled", true, true},
		{"nosniff disabled", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := securityheaders.Config{Enabled: true, ContentTypeNosniff: tt.nosniff}
			p := newPlugin(cfg, false)
			handlers := p.ContributeCaddyHandlers()
			hdrs := responseHeaders(t, handlers[0].Handler)
			_, has := hdrs["X-Content-Type-Options"]
			if tt.want && !has {
				t.Error("expected X-Content-Type-Options header")
			}
			if !tt.want && has {
				t.Error("unexpected X-Content-Type-Options header")
			}
			if tt.want {
				if v := hdrs["X-Content-Type-Options"]; len(v) == 0 || v[0] != "nosniff" {
					t.Errorf("X-Content-Type-Options = %v, want [\"nosniff\"]", v)
				}
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_XFrameOptions(t *testing.T) {
	tests := []struct {
		name        string
		frameOption string
		wantSet     bool
	}{
		{"DENY", "DENY", true},
		{"SAMEORIGIN", "SAMEORIGIN", true},
		{"empty disables header", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := securityheaders.Config{Enabled: true, FrameOption: tt.frameOption}
			p := newPlugin(cfg, false)
			handlers := p.ContributeCaddyHandlers()
			hdrs := responseHeaders(t, handlers[0].Handler)
			val, has := hdrs["X-Frame-Options"]
			if tt.wantSet && !has {
				t.Error("expected X-Frame-Options header")
			}
			if !tt.wantSet && has {
				t.Errorf("unexpected X-Frame-Options header: %v", val)
			}
			if tt.wantSet && (len(val) == 0 || val[0] != tt.frameOption) {
				t.Errorf("X-Frame-Options = %v, want [%q]", val, tt.frameOption)
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_CSP(t *testing.T) {
	tests := []struct {
		name    string
		csp     string
		wantSet bool
	}{
		{"CSP set", "default-src 'self'", true},
		{"CSP empty disables header", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := securityheaders.Config{Enabled: true, ContentSecurityPolicy: tt.csp}
			p := newPlugin(cfg, false)
			handlers := p.ContributeCaddyHandlers()
			hdrs := responseHeaders(t, handlers[0].Handler)
			val, has := hdrs["Content-Security-Policy"]
			if tt.wantSet && !has {
				t.Error("expected Content-Security-Policy header")
			}
			if !tt.wantSet && has {
				t.Errorf("unexpected Content-Security-Policy header: %v", val)
			}
			if tt.wantSet && (len(val) == 0 || val[0] != tt.csp) {
				t.Errorf("Content-Security-Policy = %v, want [%q]", val, tt.csp)
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_ReferrerPolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		wantSet bool
	}{
		{"policy set", "strict-origin-when-cross-origin", true},
		{"policy empty disables header", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := securityheaders.Config{Enabled: true, ReferrerPolicy: tt.policy}
			p := newPlugin(cfg, false)
			handlers := p.ContributeCaddyHandlers()
			hdrs := responseHeaders(t, handlers[0].Handler)
			val, has := hdrs["Referrer-Policy"]
			if tt.wantSet && !has {
				t.Error("expected Referrer-Policy header")
			}
			if !tt.wantSet && has {
				t.Errorf("unexpected Referrer-Policy header: %v", val)
			}
			if tt.wantSet && (len(val) == 0 || val[0] != tt.policy) {
				t.Errorf("Referrer-Policy = %v, want [%q]", val, tt.policy)
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_PermissionsPolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		wantSet bool
	}{
		{"policy set", "camera=(), microphone=()", true},
		{"policy empty disables header", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := securityheaders.Config{Enabled: true, PermissionsPolicy: tt.policy}
			p := newPlugin(cfg, false)
			handlers := p.ContributeCaddyHandlers()
			hdrs := responseHeaders(t, handlers[0].Handler)
			val, has := hdrs["Permissions-Policy"]
			if tt.wantSet && !has {
				t.Error("expected Permissions-Policy header")
			}
			if !tt.wantSet && has {
				t.Errorf("unexpected Permissions-Policy header: %v", val)
			}
			if tt.wantSet && (len(val) == 0 || val[0] != tt.policy) {
				t.Errorf("Permissions-Policy = %v, want [%q]", val, tt.policy)
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_HandlerStructure(t *testing.T) {
	p := newPlugin(defaultConfig(), true)
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(handlers))
	}
	h := handlers[0].Handler

	// Must have handler type "headers".
	if h["handler"] != "headers" {
		t.Errorf("handler[\"handler\"] = %v, want \"headers\"", h["handler"])
	}

	// Must have a "response" key containing "set".
	resp, ok := h["response"].(map[string]any)
	if !ok {
		t.Fatal("expected response key to be map[string]any")
	}
	if _, ok := resp["set"]; !ok {
		t.Error("expected set key in response map")
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
