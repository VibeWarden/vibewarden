package tls_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
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
	return tlsplugin.New(cfg, nil, discardLogger())
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
			name:    "zerossl with domain and email — valid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderZeroSSL, Domain: "example.com", Email: "admin@example.com"},
			wantErr: false,
		},
		{
			name:    "zerossl without email — invalid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderZeroSSL, Domain: "example.com"},
			wantErr: true,
		},
		{
			name:    "zerossl without domain — invalid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderZeroSSL, Email: "admin@example.com"},
			wantErr: true,
		},
		{
			name:    "buypass with domain — valid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderBuypass, Domain: "example.com"},
			wantErr: false,
		},
		{
			name:    "buypass without domain — invalid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderBuypass},
			wantErr: true,
		},
		{
			name:    "letsencrypt-staging with domain — valid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderLetsEncryptStaging, Domain: "example.com"},
			wantErr: false,
		},
		{
			name:    "letsencrypt-staging without domain — invalid",
			cfg:     ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderLetsEncryptStaging},
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

func TestPlugin_TLSApp_ZeroSSL(t *testing.T) {
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderZeroSSL,
		Domain:   "myapp.example.com",
		Email:    "admin@example.com",
	}
	p := newPlugin(cfg)
	got, err := p.TLSApp()
	if err != nil {
		t.Fatalf("TLSApp() unexpected error: %v", err)
	}

	automation, ok := got["automation"].(map[string]any)
	if !ok {
		t.Fatal("expected automation key")
	}
	policies, ok := automation["policies"].([]map[string]any)
	if !ok || len(policies) == 0 {
		t.Fatal("expected at least one policy")
	}
	issuers, ok := policies[0]["issuers"].([]map[string]any)
	if !ok || len(issuers) == 0 {
		t.Fatal("expected at least one issuer")
	}
	if len(issuers) != 1 {
		t.Errorf("expected 1 issuer for zerossl, got %d", len(issuers))
	}
	if issuers[0]["ca"] != "https://acme.zerossl.com/v2/DV90" {
		t.Errorf("ca = %q, want ZeroSSL URL", issuers[0]["ca"])
	}
	if issuers[0]["email"] != "admin@example.com" {
		t.Errorf("email = %q, want %q", issuers[0]["email"], "admin@example.com")
	}
}

func TestPlugin_TLSApp_Buypass(t *testing.T) {
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderBuypass,
		Domain:   "myapp.example.com",
	}
	p := newPlugin(cfg)
	got, err := p.TLSApp()
	if err != nil {
		t.Fatalf("TLSApp() unexpected error: %v", err)
	}

	automation, ok := got["automation"].(map[string]any)
	if !ok {
		t.Fatal("expected automation key")
	}
	policies, ok := automation["policies"].([]map[string]any)
	if !ok || len(policies) == 0 {
		t.Fatal("expected at least one policy")
	}
	issuers, ok := policies[0]["issuers"].([]map[string]any)
	if !ok || len(issuers) == 0 {
		t.Fatal("expected at least one issuer")
	}
	if len(issuers) != 1 {
		t.Errorf("expected 1 issuer for buypass, got %d", len(issuers))
	}
	if issuers[0]["ca"] != "https://api.buypass.com/acme/directory" {
		t.Errorf("ca = %q, want Buypass URL", issuers[0]["ca"])
	}
}

func TestPlugin_TLSApp_LetsEncryptStaging(t *testing.T) {
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderLetsEncryptStaging,
		Domain:   "myapp.example.com",
	}
	p := newPlugin(cfg)
	got, err := p.TLSApp()
	if err != nil {
		t.Fatalf("TLSApp() unexpected error: %v", err)
	}

	automation, ok := got["automation"].(map[string]any)
	if !ok {
		t.Fatal("expected automation key")
	}
	policies, ok := automation["policies"].([]map[string]any)
	if !ok || len(policies) == 0 {
		t.Fatal("expected at least one policy")
	}
	issuers, ok := policies[0]["issuers"].([]map[string]any)
	if !ok || len(issuers) == 0 {
		t.Fatal("expected at least one issuer")
	}
	if len(issuers) != 1 {
		t.Errorf("expected 1 issuer for letsencrypt-staging, got %d", len(issuers))
	}
	if issuers[0]["ca"] != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Errorf("ca = %q, want LE staging URL", issuers[0]["ca"])
	}
}

// TestPlugin_TLSApp_LetsEncrypt_FallbackChain verifies the default chain
// after ADR-083: Buypass is removed; ZeroSSL joins only when tls.email is set.
func TestPlugin_TLSApp_LetsEncrypt_FallbackChain(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantCAs []string
	}{
		{
			name:  "no email — single-issuer LE",
			email: "",
			wantCAs: []string{
				"https://acme-v02.api.letsencrypt.org/directory",
			},
		},
		{
			name:  "with email — LE then ZeroSSL (no Buypass)",
			email: "admin@example.com",
			wantCAs: []string{
				"https://acme-v02.api.letsencrypt.org/directory",
				"https://acme.zerossl.com/v2/DV90",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "myapp.example.com",
				Email:    tt.email,
			}
			p := newPlugin(cfg)
			got, err := p.TLSApp()
			if err != nil {
				t.Fatalf("TLSApp() unexpected error: %v", err)
			}

			automation, ok := got["automation"].(map[string]any)
			if !ok {
				t.Fatal("expected automation key")
			}
			policies, ok := automation["policies"].([]map[string]any)
			if !ok || len(policies) == 0 {
				t.Fatal("expected at least one policy")
			}
			issuers, ok := policies[0]["issuers"].([]map[string]any)
			if !ok {
				t.Fatal("expected issuers key")
			}
			if len(issuers) != len(tt.wantCAs) {
				t.Fatalf("got %d issuers, want %d", len(issuers), len(tt.wantCAs))
			}
			for i, issuer := range issuers {
				if issuer["ca"] != tt.wantCAs[i] {
					t.Errorf("issuer[%d].ca = %q, want %q", i, issuer["ca"], tt.wantCAs[i])
				}
				// Regression guard: Buypass must never appear in the default
				// chain per ADR-083.
				if issuer["ca"] == "https://api.buypass.com/acme/directory" {
					t.Errorf("issuer[%d] is Buypass — must not appear in default chain", i)
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

// TestPlugin_TLSApp_LetsEncrypt_StoragePath verifies that TLSApp() does NOT include
// a storage block even when StoragePath is set. Storage is a top-level Caddy Config
// field; placing it inside apps.tls causes Caddy to reject the config with
// "unknown field: storage". The Caddy adapter's BuildCaddyConfig is responsible
// for emitting storage at the correct top level.
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

	// Storage must NOT appear inside the TLS app map.
	if _, ok := got["storage"]; ok {
		t.Error("TLSApp() must not include 'storage' key — storage belongs at the top-level Caddy config")
	}

	// The automation policies must still be present.
	automation, ok := got["automation"].(map[string]any)
	if !ok {
		t.Fatal("expected automation key in TLS app")
	}
	if _, ok := automation["policies"]; !ok {
		t.Error("expected policies key in automation")
	}
}

// TestPlugin_TLSApp_LetsEncrypt_ACMEIssuer verifies the ACME issuer is configured
// with explicit CA URLs and without challenge settings (Caddy selects TLS-ALPN-01
// automatically).
func TestPlugin_TLSApp_LetsEncrypt_ACMEIssuer(t *testing.T) {
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
		t.Fatal("expected automation key")
	}
	policies, ok := automation["policies"].([]map[string]any)
	if !ok || len(policies) == 0 {
		t.Fatal("expected at least one policy")
	}
	issuers, ok := policies[0]["issuers"].([]map[string]any)
	if !ok || len(issuers) == 0 {
		t.Fatal("expected at least one issuer")
	}
	acmeIssuer := issuers[0]
	if acmeIssuer["module"] != "acme" {
		t.Fatalf("expected acme module, got %q", acmeIssuer["module"])
	}
	// Each issuer in the fallback chain should have an explicit "ca" field.
	if _, hasCA := acmeIssuer["ca"]; !hasCA {
		t.Error("ACME issuer should have explicit 'ca' field in fallback chain")
	}
	// No explicit challenges — Caddy uses TLS-ALPN-01 automatically.
	if _, hasChallenge := acmeIssuer["challenges"]; hasChallenge {
		t.Error("ACME issuer should not have explicit challenges config")
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
// ACME chain — Init observability events (ADR-083)
// ---------------------------------------------------------------------------

// fakeEventLogger is a minimal in-memory ports.EventLogger for tests. It
// records every event emitted by the plugin so assertions can inspect the
// event_type, severity, and payload shape.
type fakeEventLogger struct {
	mu     sync.Mutex
	events []events.Event
}

func (f *fakeEventLogger) Log(_ context.Context, e events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeEventLogger) snapshot() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]events.Event, len(f.events))
	copy(out, f.events)
	return out
}

func (f *fakeEventLogger) byType(t string) []events.Event {
	var out []events.Event
	for _, e := range f.snapshot() {
		if e.EventType == t {
			out = append(out, e)
		}
	}
	return out
}

// TestPlugin_Init_EmitsChainConfigured asserts that every ACME provider
// configuration produces exactly one tls.acme.chain_configured event at Init,
// carrying the resolved chain and primary provider.
func TestPlugin_Init_EmitsChainConfigured(t *testing.T) {
	tests := []struct {
		name            string
		cfg             ports.TLSConfig
		wantChain       []string
		wantSkipZeroSSL bool
		wantDeprecation bool
		wantPrimary     string
	}{
		{
			name: "letsencrypt default without email — LE only, zerossl skipped",
			cfg: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "app.example.com",
			},
			wantChain:       []string{"letsencrypt"},
			wantSkipZeroSSL: true,
			wantPrimary:     "letsencrypt",
		},
		{
			name: "letsencrypt default with email — LE + zerossl",
			cfg: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "app.example.com",
				Email:    "admin@example.com",
			},
			wantChain:   []string{"letsencrypt", "zerossl"},
			wantPrimary: "letsencrypt",
		},
		{
			name: "letsencrypt with acme_ca override — custom single issuer",
			cfg: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "app.example.com",
				ACMECA:   "https://internal-ca.example.com/directory",
			},
			wantChain:   []string{"custom"},
			wantPrimary: "letsencrypt",
		},
		{
			name: "zerossl explicit with email — single-issuer chain",
			cfg: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderZeroSSL,
				Domain:   "app.example.com",
				Email:    "admin@example.com",
			},
			wantChain:   []string{"zerossl"},
			wantPrimary: "zerossl",
		},
		{
			name: "buypass explicit — single-issuer chain + deprecation event",
			cfg: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderBuypass,
				Domain:   "app.example.com",
			},
			wantChain:       []string{"buypass"},
			wantDeprecation: true,
			wantPrimary:     "buypass",
		},
		{
			name: "letsencrypt-staging — single-issuer chain",
			cfg: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderLetsEncryptStaging,
				Domain:   "app.example.com",
			},
			wantChain:   []string{"letsencrypt-staging"},
			wantPrimary: "letsencrypt-staging",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeEventLogger{}
			p := tlsplugin.New(tt.cfg, fake, discardLogger())
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() unexpected error: %v", err)
			}

			// chain_configured must be emitted exactly once with the resolved chain.
			configured := fake.byType(events.EventTypeTLSACMEChainConfigured)
			if len(configured) != 1 {
				t.Fatalf("chain_configured count = %d, want 1; all events: %+v", len(configured), fake.snapshot())
			}
			primary, _ := configured[0].Payload["primary_provider"].(string)
			if primary != tt.wantPrimary {
				t.Errorf("chain_configured.primary_provider = %q, want %q", primary, tt.wantPrimary)
			}
			chain, ok := configured[0].Payload["resolved_chain"].([]string)
			if !ok {
				t.Fatalf("chain_configured.resolved_chain type = %T, want []string", configured[0].Payload["resolved_chain"])
			}
			if len(chain) != len(tt.wantChain) {
				t.Fatalf("resolved_chain len = %d, want %d (got %v)", len(chain), len(tt.wantChain), chain)
			}
			for i, got := range chain {
				if got != tt.wantChain[i] {
					t.Errorf("resolved_chain[%d] = %q, want %q", i, got, tt.wantChain[i])
				}
			}

			// chain_skipped presence matches the expectation for this row.
			skipped := fake.byType(events.EventTypeTLSACMEChainSkipped)
			if tt.wantSkipZeroSSL {
				if len(skipped) != 1 {
					t.Fatalf("chain_skipped count = %d, want 1", len(skipped))
				}
				if skipped[0].Payload["provider"] != "zerossl" {
					t.Errorf("chain_skipped.provider = %v, want %q", skipped[0].Payload["provider"], "zerossl")
				}
				if skipped[0].Payload["reason"] != "email_not_configured" {
					t.Errorf("chain_skipped.reason = %v, want %q", skipped[0].Payload["reason"], "email_not_configured")
				}
				if skipped[0].Payload["primary_provider"] != "letsencrypt" {
					t.Errorf("chain_skipped.primary_provider = %v, want %q", skipped[0].Payload["primary_provider"], "letsencrypt")
				}
			} else {
				if len(skipped) != 0 {
					t.Errorf("expected no chain_skipped events, got %+v", skipped)
				}
			}

			// provider_deprecated only emitted when buypass is explicitly chosen.
			dep := fake.byType(events.EventTypeTLSACMEProviderDeprecated)
			if tt.wantDeprecation {
				if len(dep) != 1 {
					t.Fatalf("provider_deprecated count = %d, want 1", len(dep))
				}
				if dep[0].Payload["provider"] != "buypass" {
					t.Errorf("provider_deprecated.provider = %v, want %q", dep[0].Payload["provider"], "buypass")
				}
			} else {
				if len(dep) != 0 {
					t.Errorf("expected no provider_deprecated events, got %+v", dep)
				}
			}
		})
	}
}

// TestPlugin_Init_NilEventLogger_DoesNotPanic asserts the ADR-083 §9
// requirement that Init is safe when no EventLogger is configured.
func TestPlugin_Init_NilEventLogger_DoesNotPanic(t *testing.T) {
	cfg := ports.TLSConfig{
		Enabled:  true,
		Provider: ports.TLSProviderLetsEncrypt,
		Domain:   "app.example.com",
	}
	p := tlsplugin.New(cfg, nil, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() with nil EventLogger unexpected error: %v", err)
	}
}

// TestPlugin_Init_NonACMEProvider_EmitsNoACMEEvents ensures self-signed and
// external providers do not produce chain events (the chain concept does
// not apply).
func TestPlugin_Init_NonACMEProvider_EmitsNoACMEEvents(t *testing.T) {
	tests := []struct {
		name string
		cfg  ports.TLSConfig
	}{
		{
			name: "self-signed",
			cfg:  ports.TLSConfig{Enabled: true, Provider: ports.TLSProviderSelfSigned},
		},
		{
			name: "external",
			cfg: ports.TLSConfig{
				Enabled:  true,
				Provider: ports.TLSProviderExternal,
				CertPath: "/tls/cert.pem",
				KeyPath:  "/tls/key.pem",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeEventLogger{}
			p := tlsplugin.New(tt.cfg, fake, discardLogger())
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() unexpected error: %v", err)
			}
			for _, e := range fake.snapshot() {
				switch e.EventType {
				case events.EventTypeTLSACMEChainSkipped,
					events.EventTypeTLSACMEChainConfigured,
					events.EventTypeTLSACMEProviderDeprecated,
					events.EventTypeTLSACMEChainFallback:
					t.Errorf("unexpected acme event for non-acme provider: %q", e.EventType)
				}
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
