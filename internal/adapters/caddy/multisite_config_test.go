package caddy

import (
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/site"
)

// helperNewSite creates a healthy Site for testing. It panics on error
// because test helper failures should abort the test immediately.
func helperNewSite(t *testing.T, name string, cfg *config.Config) *site.Site {
	t.Helper()
	s, err := site.NewSite(name, "/tmp/"+name+"/vibewarden.yaml", cfg)
	if err != nil {
		t.Fatalf("helperNewSite(%q): %v", name, err)
	}
	return s
}

// helperNewErrorSite creates an error Site for testing.
func helperNewErrorSite(t *testing.T, name string) *site.Site {
	t.Helper()
	s, err := site.NewErrorSite(name, "/tmp/"+name+"/vibewarden.yaml", errors.New("broken config"))
	if err != nil {
		t.Fatalf("helperNewErrorSite(%q): %v", name, err)
	}
	return s
}

// helperMinimalConfig returns a minimal config.Config with the given domain
// and upstream port.
func helperMinimalConfig(domain string, upstreamPort int) *config.Config {
	return &config.Config{
		Upstream: config.UpstreamConfig{
			Host: "127.0.0.1",
			Port: upstreamPort,
		},
		TLS: config.TLSConfig{
			Enabled:  true,
			Provider: "letsencrypt",
			Domain:   domain,
		},
	}
}

func TestBuildMultiSiteConfig_SingleSite(t *testing.T) {
	cfg := helperMinimalConfig("app1.example.com", 3000)
	s := helperNewSite(t, "app1", cfg)

	global := site.DefaultGlobalConfig()
	result, err := BuildMultiSiteConfig([]*site.Site{s}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	// Verify the result is valid JSON.
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Parse back and verify structure.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Verify apps.http.servers.vibewarden exists.
	apps := parsed["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)
	vw := servers["vibewarden"].(map[string]any)
	routes := vw["routes"].([]any)

	// Single site: health route + catch-all route = 2 routes.
	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}

	// Verify host matcher on the catch-all route.
	catchAll := routes[1].(map[string]any)
	matches := catchAll["match"].([]any)
	match := matches[0].(map[string]any)
	hosts := match["host"].([]any)
	if hosts[0].(string) != "app1.example.com" {
		t.Errorf("expected host matcher for app1.example.com, got %v", hosts[0])
	}

	// Verify TLS app exists with the domain.
	tlsApp := apps["tls"].(map[string]any)
	automation := tlsApp["automation"].(map[string]any)
	policies := automation["policies"].([]any)
	if len(policies) != 1 {
		t.Errorf("expected 1 TLS policy, got %d", len(policies))
	}
}

func TestBuildMultiSiteConfig_TwoSitesDifferentDomains(t *testing.T) {
	cfg1 := helperMinimalConfig("app1.example.com", 3001)
	cfg2 := helperMinimalConfig("app2.example.com", 3002)

	s1 := helperNewSite(t, "app1", cfg1)
	s2 := helperNewSite(t, "app2", cfg2)

	global := site.DefaultGlobalConfig()
	result, err := BuildMultiSiteConfig([]*site.Site{s1, s2}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	data, _ := json.Marshal(result)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	apps := parsed["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)
	vw := servers["vibewarden"].(map[string]any)
	routes := vw["routes"].([]any)

	// Two sites: (health + catch-all) x 2 = 4 routes.
	if len(routes) != 4 {
		t.Errorf("expected 4 routes, got %d", len(routes))
	}

	// Verify each site's catch-all route has the correct host matcher.
	verifyHostInRoutes(t, routes, "app1.example.com")
	verifyHostInRoutes(t, routes, "app2.example.com")

	// Verify upstream addresses are different.
	verifyUpstreamInRoutes(t, routes, "127.0.0.1:3001")
	verifyUpstreamInRoutes(t, routes, "127.0.0.1:3002")

	// Verify TLS policies: one per domain.
	tlsApp := apps["tls"].(map[string]any)
	automation := tlsApp["automation"].(map[string]any)
	policies := automation["policies"].([]any)
	if len(policies) != 2 {
		t.Errorf("expected 2 TLS policies, got %d", len(policies))
	}
}

func TestBuildMultiSiteConfig_SiteWithNoDomainSkipped(t *testing.T) {
	// Site with a domain.
	cfg1 := helperMinimalConfig("app1.example.com", 3001)
	s1 := helperNewSite(t, "app1", cfg1)

	// Site without a domain (empty TLS.Domain).
	cfg2 := &config.Config{
		Upstream: config.UpstreamConfig{
			Host: "127.0.0.1",
			Port: 3002,
		},
		TLS: config.TLSConfig{
			Enabled: false,
		},
	}
	s2 := helperNewSite(t, "app2", cfg2)

	global := site.DefaultGlobalConfig()
	result, err := BuildMultiSiteConfig([]*site.Site{s1, s2}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	data, _ := json.Marshal(result)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	apps := parsed["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)
	vw := servers["vibewarden"].(map[string]any)
	routes := vw["routes"].([]any)

	// Only the site with a domain should produce routes.
	if len(routes) != 2 {
		t.Errorf("expected 2 routes (1 site), got %d", len(routes))
	}
}

func TestBuildMultiSiteConfig_ErrorSiteSkipped(t *testing.T) {
	cfg1 := helperMinimalConfig("app1.example.com", 3001)
	s1 := helperNewSite(t, "app1", cfg1)

	s2 := helperNewErrorSite(t, "app2")

	global := site.DefaultGlobalConfig()
	result, err := BuildMultiSiteConfig([]*site.Site{s1, s2}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	data, _ := json.Marshal(result)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	apps := parsed["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)
	vw := servers["vibewarden"].(map[string]any)
	routes := vw["routes"].([]any)

	// Only the healthy site should produce routes.
	if len(routes) != 2 {
		t.Errorf("expected 2 routes (1 healthy site), got %d", len(routes))
	}
}

func TestBuildMultiSiteConfig_EmptySitesList(t *testing.T) {
	global := site.DefaultGlobalConfig()
	_, err := BuildMultiSiteConfig([]*site.Site{}, global, slog.Default())
	if err == nil {
		t.Fatal("expected error for empty sites list, got nil")
	}
}

func TestBuildMultiSiteConfig_NilSitesList(t *testing.T) {
	global := site.DefaultGlobalConfig()
	_, err := BuildMultiSiteConfig(nil, global, slog.Default())
	if err == nil {
		t.Fatal("expected error for nil sites list, got nil")
	}
}

func TestBuildMultiSiteConfig_AllSitesUnhealthy(t *testing.T) {
	s1 := helperNewErrorSite(t, "app1")
	s2 := helperNewErrorSite(t, "app2")

	global := site.DefaultGlobalConfig()
	_, err := BuildMultiSiteConfig([]*site.Site{s1, s2}, global, slog.Default())
	if err == nil {
		t.Fatal("expected error when all sites are unhealthy, got nil")
	}
}

func TestBuildMultiSiteConfig_PerSiteMiddlewareIndependence(t *testing.T) {
	// Site A: security headers enabled.
	cfgA := helperMinimalConfig("a.example.com", 3001)
	cfgA.SecurityHeaders = config.SecurityHeadersConfig{
		Enabled:            true,
		ContentTypeNosniff: true,
		FrameOption:        "DENY",
		HSTSMaxAge:         31536000,
	}
	sA := helperNewSite(t, "site-a", cfgA)

	// Site B: security headers disabled.
	cfgB := helperMinimalConfig("b.example.com", 3002)
	cfgB.SecurityHeaders = config.SecurityHeadersConfig{
		Enabled: false,
	}
	sB := helperNewSite(t, "site-b", cfgB)

	global := site.DefaultGlobalConfig()
	result, err := BuildMultiSiteConfig([]*site.Site{sA, sB}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	data, _ := json.Marshal(result)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	apps := parsed["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)
	vw := servers["vibewarden"].(map[string]any)
	routes := vw["routes"].([]any)

	// 4 routes total: 2 per site.
	if len(routes) != 4 {
		t.Fatalf("expected 4 routes, got %d", len(routes))
	}

	// Catch-all routes are at indices 1 (site-a) and 3 (site-b).
	catchAllA := routes[1].(map[string]any)
	handlersA := catchAllA["handle"].([]any)

	catchAllB := routes[3].(map[string]any)
	handlersB := catchAllB["handle"].([]any)

	// Site A should have more handlers (header strip + security headers + reverse proxy).
	// Site B should have fewer handlers (header strip + reverse proxy).
	if len(handlersA) <= len(handlersB) {
		t.Errorf("site A should have more handlers (%d) than site B (%d)",
			len(handlersA), len(handlersB))
	}

	// Site B's catch-all should not contain a "headers" handler with security headers.
	for _, h := range handlersB {
		handler := h.(map[string]any)
		if handler["handler"] == "headers" {
			// The only headers handler in site B should be the user-header strip handler.
			if _, hasResponse := handler["response"]; hasResponse {
				t.Error("site B should not have security headers handler")
			}
		}
	}
}

func TestBuildMultiSiteConfig_TLSPolicyPerDomain(t *testing.T) {
	cfg1 := helperMinimalConfig("alpha.example.com", 3001)
	cfg2 := helperMinimalConfig("beta.example.com", 3002)
	cfg3 := helperMinimalConfig("gamma.example.com", 3003)

	s1 := helperNewSite(t, "alpha", cfg1)
	s2 := helperNewSite(t, "beta", cfg2)
	s3 := helperNewSite(t, "gamma", cfg3)

	global := site.DefaultGlobalConfig()
	global.ACMEEmail = "admin@example.com"
	result, err := BuildMultiSiteConfig([]*site.Site{s1, s2, s3}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	data, _ := json.Marshal(result)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	apps := parsed["apps"].(map[string]any)
	tlsApp := apps["tls"].(map[string]any)
	automation := tlsApp["automation"].(map[string]any)
	policies := automation["policies"].([]any)

	if len(policies) != 3 {
		t.Fatalf("expected 3 TLS policies, got %d", len(policies))
	}

	// Verify each policy has exactly one subject and an ACME issuer with email.
	expectedDomains := map[string]bool{
		"alpha.example.com": false,
		"beta.example.com":  false,
		"gamma.example.com": false,
	}
	for _, p := range policies {
		policy := p.(map[string]any)
		subjects := policy["subjects"].([]any)
		if len(subjects) != 1 {
			t.Errorf("expected 1 subject per policy, got %d", len(subjects))
			continue
		}
		domain := subjects[0].(string)
		if _, ok := expectedDomains[domain]; !ok {
			t.Errorf("unexpected domain in TLS policy: %s", domain)
			continue
		}
		expectedDomains[domain] = true

		issuers := policy["issuers"].([]any)
		issuer := issuers[0].(map[string]any)
		if issuer["module"] != "acme" {
			t.Errorf("expected ACME issuer module, got %v", issuer["module"])
		}
		if issuer["email"] != "admin@example.com" {
			t.Errorf("expected email admin@example.com, got %v", issuer["email"])
		}
	}

	for domain, found := range expectedDomains {
		if !found {
			t.Errorf("missing TLS policy for domain %s", domain)
		}
	}
}

func TestBuildMultiSiteConfig_ListenAddress(t *testing.T) {
	cfg := helperMinimalConfig("app.example.com", 3000)
	s := helperNewSite(t, "app", cfg)

	global := site.GlobalConfig{
		ListenHost: "10.0.0.1",
		ListenPort: 8443,
		LogLevel:   "info",
	}
	result, err := BuildMultiSiteConfig([]*site.Site{s}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	data, _ := json.Marshal(result)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	apps := parsed["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)
	vw := servers["vibewarden"].(map[string]any)
	listen := vw["listen"].([]any)

	if listen[0].(string) != "10.0.0.1:8443" {
		t.Errorf("expected listen address 10.0.0.1:8443, got %v", listen[0])
	}
}

func TestBuildMultiSiteConfig_HealthRoutePerSite(t *testing.T) {
	cfg1 := helperMinimalConfig("app1.example.com", 3001)
	cfg2 := helperMinimalConfig("app2.example.com", 3002)

	s1 := helperNewSite(t, "app1", cfg1)
	s2 := helperNewSite(t, "app2", cfg2)

	global := site.DefaultGlobalConfig()
	result, err := BuildMultiSiteConfig([]*site.Site{s1, s2}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	data, _ := json.Marshal(result)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	apps := parsed["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)
	vw := servers["vibewarden"].(map[string]any)
	routes := vw["routes"].([]any)

	// Health routes are at even indices (0, 2).
	healthRouteFound := make(map[string]bool)
	for _, r := range routes {
		route := r.(map[string]any)
		matches, ok := route["match"].([]any)
		if !ok || len(matches) == 0 {
			continue
		}
		match := matches[0].(map[string]any)
		paths, hasPaths := match["path"]
		if !hasPaths {
			continue
		}
		pathList := paths.([]any)
		for _, p := range pathList {
			if p.(string) == "/_vibewarden/health" {
				hosts := match["host"].([]any)
				healthRouteFound[hosts[0].(string)] = true
			}
		}
	}

	if !healthRouteFound["app1.example.com"] {
		t.Error("missing health route for app1.example.com")
	}
	if !healthRouteFound["app2.example.com"] {
		t.Error("missing health route for app2.example.com")
	}
}

func TestBuildMultiSiteConfig_ErrorIsolation(t *testing.T) {
	// 2 healthy sites + 1 error site.
	cfg1 := helperMinimalConfig("good1.example.com", 3001)
	cfg2 := helperMinimalConfig("good2.example.com", 3002)

	s1 := helperNewSite(t, "good1", cfg1)
	s2 := helperNewSite(t, "good2", cfg2)
	s3 := helperNewErrorSite(t, "broken")

	global := site.DefaultGlobalConfig()
	result, err := BuildMultiSiteConfig([]*site.Site{s1, s2, s3}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	data, _ := json.Marshal(result)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	apps := parsed["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)
	vw := servers["vibewarden"].(map[string]any)
	routes := vw["routes"].([]any)

	// Only 2 healthy sites should generate routes: (health + catch-all) x 2 = 4.
	if len(routes) != 4 {
		t.Errorf("expected 4 routes (2 healthy sites), got %d", len(routes))
	}

	// Verify the two TLS policies are for the healthy domains only.
	tlsApp := apps["tls"].(map[string]any)
	automation := tlsApp["automation"].(map[string]any)
	policies := automation["policies"].([]any)
	if len(policies) != 2 {
		t.Errorf("expected 2 TLS policies, got %d", len(policies))
	}
}

func TestBuildMultiSiteConfig_ValidJSON(t *testing.T) {
	cfg := helperMinimalConfig("valid.example.com", 3000)
	s := helperNewSite(t, "valid", cfg)

	global := site.DefaultGlobalConfig()
	result, err := BuildMultiSiteConfig([]*site.Site{s}, global, slog.Default())
	if err != nil {
		t.Fatalf("BuildMultiSiteConfig() error = %v", err)
	}

	// The result must be valid JSON that can round-trip through
	// marshal/unmarshal without loss.
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var roundTrip map[string]any
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Re-marshal and compare sizes (approximate equality check).
	data2, _ := json.Marshal(roundTrip)
	if len(data) != len(data2) {
		t.Errorf("JSON round-trip changed size: %d -> %d", len(data), len(data2))
	}
}

func TestBuildMultiSiteTLSApp(t *testing.T) {
	tests := []struct {
		name      string
		domains   []string
		acmeEmail string
		wantNil   bool
		wantCount int
		wantEmail bool
	}{
		{
			name:    "no domains",
			domains: nil,
			wantNil: true,
		},
		{
			name:      "single domain without email",
			domains:   []string{"example.com"},
			wantCount: 1,
			wantEmail: false,
		},
		{
			name:      "two domains with email",
			domains:   []string{"a.com", "b.com"},
			acmeEmail: "admin@example.com",
			wantCount: 2,
			wantEmail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildMultiSiteTLSApp(tt.domains, tt.acmeEmail)
			if tt.wantNil {
				if result != nil {
					t.Error("expected nil TLS app")
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil TLS app")
			}

			automation := result["automation"].(map[string]any)
			policies := automation["policies"].([]map[string]any)
			if len(policies) != tt.wantCount {
				t.Errorf("expected %d policies, got %d", tt.wantCount, len(policies))
			}

			if tt.wantEmail {
				for _, p := range policies {
					issuers := p["issuers"].([]map[string]any)
					if issuers[0]["email"] != tt.acmeEmail {
						t.Errorf("expected email %q, got %v", tt.acmeEmail, issuers[0]["email"])
					}
				}
			}
		})
	}
}

func TestBuildSiteRoutes_InvalidUpstream(t *testing.T) {
	cfg := &config.Config{
		Upstream: config.UpstreamConfig{
			Host: "",
			Port: 0,
		},
		TLS: config.TLSConfig{
			Domain: "test.example.com",
		},
	}
	s := helperNewSite(t, "broken-upstream", cfg)

	_, err := buildSiteRoutes(s, "test.example.com")
	if err == nil {
		t.Fatal("expected error for invalid upstream, got nil")
	}
}

// verifyHostInRoutes checks that at least one route has a host matcher
// for the given domain.
func verifyHostInRoutes(t *testing.T, routes []any, domain string) {
	t.Helper()
	for _, r := range routes {
		route := r.(map[string]any)
		matches, ok := route["match"].([]any)
		if !ok || len(matches) == 0 {
			continue
		}
		match := matches[0].(map[string]any)
		hosts, ok := match["host"].([]any)
		if !ok {
			continue
		}
		for _, h := range hosts {
			if h.(string) == domain {
				return
			}
		}
	}
	t.Errorf("no route found with host matcher for %q", domain)
}

// verifyUpstreamInRoutes checks that at least one route has a reverse proxy
// handler with the given upstream dial address.
func verifyUpstreamInRoutes(t *testing.T, routes []any, upstream string) {
	t.Helper()
	for _, r := range routes {
		route := r.(map[string]any)
		handlers, ok := route["handle"].([]any)
		if !ok {
			continue
		}
		for _, h := range handlers {
			handler := h.(map[string]any)
			if handler["handler"] != "reverse_proxy" {
				continue
			}
			upstreams, ok := handler["upstreams"].([]any)
			if !ok {
				continue
			}
			for _, u := range upstreams {
				us := u.(map[string]any)
				if us["dial"] == upstream {
					return
				}
			}
		}
	}
	t.Errorf("no route found with upstream %q", upstream)
}
