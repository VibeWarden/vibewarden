package tls_test

import (
	"context"
	"log/slog"
	"testing"

	tlsplugin "github.com/vibewarden/vibewarden/internal/plugins/tls"
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

func newPlugin(cfg ports.TLSConfig) *tlsplugin.Plugin {
	return tlsplugin.New(cfg, discardLogger())
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(ports.TLSConfig{})
	if got := p.Name(); got != "tls" {
		t.Errorf("Name() = %q, want %q", got, "tls")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(ports.TLSConfig{})
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
		cfg     ports.TLSConfig
		wantErr bool
	}{
		{
			name:    "disabled — no validation",
			cfg:     ports.TLSConfig{Enabled: false},
			wantErr: false,
		},
		{
			name:    "self-signed — no fields required",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned},
			wantErr: false,
		},
		{
			name:    "self-signed empty provider string — treated as self-signed",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ""},
			wantErr: false,
		},
		{
			name:    "letsencrypt with domain — valid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderLetsEncrypt, Domain: "example.com"},
			wantErr: false,
		},
		{
			name:    "letsencrypt without domain — invalid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderLetsEncrypt},
			wantErr: true,
		},
		{
			name:    "external with cert and key — valid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderExternal, CertPath: "/tls/cert.pem", KeyPath: "/tls/key.pem"},
			wantErr: false,
		},
		{
			name:    "external without cert — invalid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderExternal, KeyPath: "/tls/key.pem"},
			wantErr: true,
		},
		{
			name:    "external without key — invalid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderExternal, CertPath: "/tls/cert.pem"},
			wantErr: true,
		},
		{
			name:    "unknown provider — invalid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: "cloudflare"},
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
	p := newPlugin(ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned})
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() unexpected error: %v", err)
	}
}

func TestPlugin_Stop_IsNoop(t *testing.T) {
	p := newPlugin(ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned})
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
		cfg            ports.TLSConfig
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            ports.TLSConfig{Enabled: false},
			wantHealthy:    true,
			wantMsgContain: "disabled",
		},
		{
			name:           "enabled self-signed",
			cfg:            ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned},
			wantHealthy:    true,
			wantMsgContain: "self-signed",
		},
		{
			name:           "enabled letsencrypt",
			cfg:            ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderLetsEncrypt, Domain: "example.com"},
			wantHealthy:    true,
			wantMsgContain: "letsencrypt",
		},
		{
			name:           "enabled external",
			cfg:            ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderExternal, CertPath: "/c.pem", KeyPath: "/k.pem"},
			wantHealthy:    true,
			wantMsgContain: "external",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			h := p.Health()
			if h.Healthy != tt.wantHealthy {
				t.Errorf("Health().Healthy = %v, want %v", h.Healthy, tt.wantHealthy)
			}
			if h.Message == "" {
				t.Error("Health().Message should not be empty")
			}
			if tt.wantMsgContain != "" {
				found := false
				for i := 0; i+len(tt.wantMsgContain) <= len(h.Message); i++ {
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
// CaddyContributor — routes and handlers
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_Empty(t *testing.T) {
	tests := []struct {
		name string
		cfg  ports.TLSConfig
	}{
		{"disabled", ports.TLSConfig{Enabled: false}},
		{"enabled self-signed", ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			routes := p.ContributeCaddyRoutes()
			if len(routes) != 0 {
				t.Errorf("ContributeCaddyRoutes() = %v, want empty", routes)
			}
		})
	}
}

func TestPlugin_ContributeCaddyHandlers_Empty(t *testing.T) {
	tests := []struct {
		name string
		cfg  ports.TLSConfig
	}{
		{"disabled", ports.TLSConfig{Enabled: false}},
		{"enabled self-signed", ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			handlers := p.ContributeCaddyHandlers()
			if len(handlers) != 0 {
				t.Errorf("ContributeCaddyHandlers() = %v, want empty", handlers)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TLSConnectionPolicies
// ---------------------------------------------------------------------------

func TestPlugin_TLSConnectionPolicies(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ports.TLSConfig
		wantNil bool
		wantLen int
		wantTag bool // expects certificate_selection with any_tag
	}{
		{
			name:    "disabled — nil",
			cfg:     ports.TLSConfig{Enabled: false},
			wantNil: true,
		},
		{
			name:    "self-signed — default empty policy",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned},
			wantLen: 1,
			wantTag: false,
		},
		{
			name:    "letsencrypt — default empty policy",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderLetsEncrypt, Domain: "example.com"},
			wantLen: 1,
			wantTag: false,
		},
		{
			name:    "external — tag-based policy",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderExternal, CertPath: "/c.pem", KeyPath: "/k.pem"},
			wantLen: 1,
			wantTag: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			got := p.TLSConnectionPolicies()
			if tt.wantNil {
				if got != nil {
					t.Errorf("TLSConnectionPolicies() = %v, want nil", got)
				}
				return
			}
			if len(got) != tt.wantLen {
				t.Fatalf("TLSConnectionPolicies() len = %d, want %d", len(got), tt.wantLen)
			}
			policy := got[0]
			_, hasCertSel := policy["certificate_selection"]
			if tt.wantTag && !hasCertSel {
				t.Error("expected certificate_selection key in policy for external provider")
			}
			if !tt.wantTag && hasCertSel {
				t.Error("unexpected certificate_selection key in policy")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TLSApp
// ---------------------------------------------------------------------------

func TestPlugin_TLSApp(t *testing.T) {
	tests := []struct {
		name       string
		cfg        ports.TLSConfig
		wantNil    bool
		wantErr    bool
		wantHasKey string // top-level key expected in the result map
	}{
		{
			name:    "disabled — nil",
			cfg:     ports.TLSConfig{Enabled: false},
			wantNil: true,
		},
		{
			name:       "self-signed — automation",
			cfg:        ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned},
			wantHasKey: "automation",
		},
		{
			name:       "letsencrypt — automation",
			cfg:        ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderLetsEncrypt, Domain: "example.com"},
			wantHasKey: "automation",
		},
		{
			name:       "external — certificates",
			cfg:        ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderExternal, CertPath: "/c.pem", KeyPath: "/k.pem"},
			wantHasKey: "certificates",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			got, err := p.TLSApp()
			if (err != nil) != tt.wantErr {
				t.Fatalf("TLSApp() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("TLSApp() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("TLSApp() = nil, want non-nil map")
			}
			if tt.wantHasKey != "" {
				if _, ok := got[tt.wantHasKey]; !ok {
					t.Errorf("TLSApp() map missing key %q; got keys: %v", tt.wantHasKey, mapKeys(got))
				}
			}
		})
	}
}

func TestPlugin_TLSApp_LetsEncrypt_Domain(t *testing.T) {
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderLetsEncrypt,
		Domain:   "myapp.example.com",
	}
	p := newPlugin(cfg)
	got, err := p.TLSApp()
	if err != nil {
		t.Fatalf("TLSApp() unexpected error: %v", err)
	}

	automation, ok := got["automation"].(map[string]any)
	if !ok {
		t.Fatal("expected automation key to be map[string]any")
	}
	policies, ok := automation["policies"].([]map[string]any)
	if !ok {
		t.Fatal("expected policies key to be []map[string]any")
	}
	if len(policies) == 0 {
		t.Fatal("expected at least one policy")
	}
	subjects, ok := policies[0]["subjects"].([]string)
	if !ok {
		t.Fatal("expected subjects key to be []string")
	}
	if len(subjects) == 0 || subjects[0] != "myapp.example.com" {
		t.Errorf("subjects[0] = %q, want %q", subjects[0], "myapp.example.com")
	}
}

func TestPlugin_TLSApp_LetsEncrypt_StoragePath(t *testing.T) {
	cfg := ports.TLSConfig{
		Enabled:     true,
		Provider:    ports.TLSProviderLetsEncrypt,
		Domain:      "myapp.example.com",
		StoragePath: "/data/certs",
	}
	p := newPlugin(cfg)
	got, err := p.TLSApp()
	if err != nil {
		t.Fatalf("TLSApp() unexpected error: %v", err)
	}

	storage, ok := got["storage"].(map[string]any)
	if !ok {
		t.Fatal("expected storage key to be map[string]any")
	}
	if storage["module"] != "file_system" {
		t.Errorf("storage.module = %q, want %q", storage["module"], "file_system")
	}
	if storage["root"] != "/data/certs" {
		t.Errorf("storage.root = %q, want %q", storage["root"], "/data/certs")
	}
}

func TestPlugin_TLSApp_SelfSigned_WithDomain(t *testing.T) {
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderSelfSigned,
		Domain:   "local.example.com",
	}
	p := newPlugin(cfg)
	got, err := p.TLSApp()
	if err != nil {
		t.Fatalf("TLSApp() unexpected error: %v", err)
	}

	automation := got["automation"].(map[string]any)
	policies := automation["policies"].([]map[string]any)
	subjects, ok := policies[0]["subjects"].([]string)
	if !ok {
		t.Fatal("expected subjects in policy when domain is set")
	}
	if subjects[0] != "local.example.com" {
		t.Errorf("subjects[0] = %q, want %q", subjects[0], "local.example.com")
	}
}

func TestPlugin_TLSApp_SelfSigned_WithoutDomain(t *testing.T) {
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderSelfSigned,
	}
	p := newPlugin(cfg)
	got, err := p.TLSApp()
	if err != nil {
		t.Fatalf("TLSApp() unexpected error: %v", err)
	}

	automation := got["automation"].(map[string]any)
	policies := automation["policies"].([]map[string]any)
	if _, ok := policies[0]["subjects"]; ok {
		t.Error("expected no subjects key when domain is empty")
	}
}

func TestPlugin_TLSApp_External_FilePaths(t *testing.T) {
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderExternal,
		CertPath: "/etc/tls/cert.pem",
		KeyPath:  "/etc/tls/key.pem",
	}
	p := newPlugin(cfg)
	got, err := p.TLSApp()
	if err != nil {
		t.Fatalf("TLSApp() unexpected error: %v", err)
	}

	certs, ok := got["certificates"].(map[string]any)
	if !ok {
		t.Fatal("expected certificates key")
	}
	files, ok := certs["load_files"].([]map[string]any)
	if !ok || len(files) == 0 {
		t.Fatal("expected load_files with at least one entry")
	}
	if files[0]["certificate"] != "/etc/tls/cert.pem" {
		t.Errorf("certificate = %q, want %q", files[0]["certificate"], "/etc/tls/cert.pem")
	}
	if files[0]["key"] != "/etc/tls/key.pem" {
		t.Errorf("key = %q, want %q", files[0]["key"], "/etc/tls/key.pem")
	}
	tags, ok := files[0]["tags"].([]string)
	if !ok || len(tags) == 0 || tags[0] != "vibewarden_external" {
		t.Errorf("tags = %v, want [vibewarden_external]", tags)
	}
}

// ---------------------------------------------------------------------------
// RedirectServer
// ---------------------------------------------------------------------------

func TestPlugin_RedirectServer(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ports.TLSConfig
		wantNil bool
	}{
		{
			name:    "disabled — nil",
			cfg:     ports.TLSConfig{Enabled: false},
			wantNil: true,
		},
		{
			name:    "enabled — non-nil",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned},
			wantNil: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			got := p.RedirectServer()
			if tt.wantNil {
				if got != nil {
					t.Errorf("RedirectServer() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("RedirectServer() = nil, want non-nil")
			}
		})
	}
}

func TestPlugin_RedirectServer_ListensOn80(t *testing.T) {
	cfg := ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned}
	p := newPlugin(cfg)
	got := p.RedirectServer()

	listen, ok := got["listen"].([]string)
	if !ok || len(listen) == 0 {
		t.Fatal("expected listen key with at least one address")
	}
	if listen[0] != ":80" {
		t.Errorf("listen[0] = %q, want %q", listen[0], ":80")
	}
}

func TestPlugin_RedirectServer_Returns301WithLocationHeader(t *testing.T) {
	cfg := ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned}
	p := newPlugin(cfg)
	got := p.RedirectServer()

	routes, ok := got["routes"].([]map[string]any)
	if !ok || len(routes) == 0 {
		t.Fatal("expected routes")
	}
	handle, ok := routes[0]["handle"].([]map[string]any)
	if !ok || len(handle) == 0 {
		t.Fatal("expected handle in first route")
	}
	handler := handle[0]
	if handler["handler"] != "static_response" {
		t.Errorf("handler = %q, want %q", handler["handler"], "static_response")
	}
	if handler["status_code"] != 301 {
		t.Errorf("status_code = %v, want 301", handler["status_code"])
	}
	headers, ok := handler["headers"].(map[string][]string)
	if !ok {
		t.Fatal("expected headers map")
	}
	if len(headers["Location"]) == 0 {
		t.Error("expected Location header")
	}
}

func TestPlugin_RedirectServer_AutomaticHTTPSDisabled(t *testing.T) {
	cfg := ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned}
	p := newPlugin(cfg)
	got := p.RedirectServer()

	autoHTTPS, ok := got["automatic_https"].(map[string]any)
	if !ok {
		t.Fatal("expected automatic_https key")
	}
	if autoHTTPS["disable"] != true {
		t.Errorf("automatic_https.disable = %v, want true", autoHTTPS["disable"])
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

// TestPlugin_ImplementsPortsPlugin asserts at compile time that *Plugin
// satisfies the ports.Plugin interface.
func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*tlsplugin.Plugin)(nil)
}

// TestPlugin_ImplementsCaddyContributor asserts at compile time that *Plugin
// satisfies the ports.CaddyContributor interface.
func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*tlsplugin.Plugin)(nil)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
