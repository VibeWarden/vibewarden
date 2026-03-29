package auth_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/identity"
	"github.com/vibewarden/vibewarden/internal/plugins/auth"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeIdentityProvider is a test double for ports.IdentityProvider.
type fakeIdentityProvider struct {
	result identity.AuthResult
}

func (f *fakeIdentityProvider) Name() string { return "fake" }

func (f *fakeIdentityProvider) Authenticate(_ context.Context, _ *http.Request) identity.AuthResult {
	return f.result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

func defaultConfig() auth.Config {
	return auth.Config{
		Enabled:           true,
		Mode:              auth.ModeKratos,
		KratosPublicURL:   "http://127.0.0.1:4433",
		KratosAdminURL:    "http://127.0.0.1:4434",
		SessionCookieName: "ory_kratos_session",
		LoginURL:          "/self-service/login/browser",
		PublicPaths:       []string{"/health"},
		IdentitySchema:    "email_password",
	}
}

// newFakeProvider returns a fakeIdentityProvider that returns a successful result.
func newFakeProvider() ports.IdentityProvider {
	ident, _ := identity.NewIdentity("user-test", "test@example.com", "kratos", true, nil)
	return &fakeIdentityProvider{result: identity.Success(ident)}
}

func newPlugin(cfg auth.Config) *auth.Plugin {
	return auth.New(cfg, discardLogger(), newFakeProvider())
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Name(); got != "auth" {
		t.Errorf("Name() = %q, want %q", got, "auth")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := newPlugin(defaultConfig())
	if got := p.Priority(); got != 40 {
		t.Errorf("Priority() = %d, want 40", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     auth.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "disabled — no validation performed",
			cfg:     auth.Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled with valid config",
			cfg:     defaultConfig(),
			wantErr: false,
		},
		{
			name:    "enabled kratos mode without kratos public url",
			cfg:     auth.Config{Enabled: true, Mode: auth.ModeKratos, KratosPublicURL: ""},
			wantErr: true,
			errMsg:  "kratos_public_url is required",
		},
		{
			name:    "enabled kratos mode with invalid kratos public url",
			cfg:     auth.Config{Enabled: true, Mode: auth.ModeKratos, KratosPublicURL: "not-a-url"},
			wantErr: true,
			errMsg:  "not a valid URL",
		},
		{
			name:    "enabled none mode without kratos public url — no error",
			cfg:     auth.Config{Enabled: true, Mode: auth.ModeNone},
			wantErr: false,
		},
		{
			name:    "enabled jwt mode without kratos public url — no error",
			cfg:     auth.Config{Enabled: true, Mode: auth.ModeJWT},
			wantErr: false,
		},
		{
			name:    "enabled api-key mode without kratos public url — no error",
			cfg:     auth.Config{Enabled: true, Mode: auth.ModeAPIKey},
			wantErr: false,
		},
		{
			name: "enabled kratos mode with minimal valid config — defaults applied",
			cfg: auth.Config{
				Enabled:         true,
				Mode:            auth.ModeKratos,
				KratosPublicURL: "http://127.0.0.1:4433",
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
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Init() error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
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

func TestPlugin_Health_BeforeInit(t *testing.T) {
	// Before Init, the plugin health reflects the zero-value state.
	// A disabled plugin should still report healthy.
	p := auth.New(auth.Config{Enabled: false}, discardLogger(), nil)
	_ = p.Init(context.Background())
	h := p.Health()
	if !h.Healthy {
		t.Errorf("Health().Healthy = false for disabled plugin, want true")
	}
	if !strings.Contains(h.Message, "disabled") {
		t.Errorf("Health().Message = %q, want to contain %q", h.Message, "disabled")
	}
}

func TestPlugin_Health_EnabledAfterInit(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	h := p.Health()
	if !h.Healthy {
		t.Errorf("Health().Healthy = false after successful Init, want true")
	}
	if !strings.Contains(h.Message, "configured") {
		t.Errorf("Health().Message = %q, want to contain %q", h.Message, "configured")
	}
}

func TestPlugin_Health_Table(t *testing.T) {
	tests := []struct {
		name           string
		cfg            auth.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            auth.Config{Enabled: false},
			wantHealthy:    true,
			wantMsgContain: "disabled",
		},
		{
			name:           "enabled with valid config",
			cfg:            defaultConfig(),
			wantHealthy:    true,
			wantMsgContain: "configured",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlugin(tt.cfg)
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error: %v", err)
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
// HealthCheck — live probe
// ---------------------------------------------------------------------------

func TestPlugin_HealthCheck_KratosReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/ready" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := auth.Config{
		Enabled:         true,
		Mode:            auth.ModeKratos,
		KratosPublicURL: srv.URL,
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	h := p.HealthCheck(context.Background())
	if !h.Healthy {
		t.Errorf("HealthCheck() healthy = false, want true; message: %s", h.Message)
	}
}

func TestPlugin_HealthCheck_KratosUnreachable(t *testing.T) {
	cfg := auth.Config{
		Enabled:         true,
		Mode:            auth.ModeKratos,
		KratosPublicURL: "http://127.0.0.1:19999", // nothing listening here
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	h := p.HealthCheck(context.Background())
	if h.Healthy {
		t.Error("HealthCheck() healthy = true for unreachable Kratos, want false")
	}
}

func TestPlugin_HealthCheck_Disabled(t *testing.T) {
	p := auth.New(auth.Config{Enabled: false}, discardLogger(), nil)
	_ = p.Init(context.Background())
	h := p.HealthCheck(context.Background())
	if !h.Healthy {
		t.Error("HealthCheck() healthy = false for disabled plugin, want true")
	}
}

func TestPlugin_HealthCheck_KratosServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := auth.Config{
		Enabled:         true,
		Mode:            auth.ModeKratos,
		KratosPublicURL: srv.URL,
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	h := p.HealthCheck(context.Background())
	if h.Healthy {
		t.Error("HealthCheck() healthy = true for 500 response, want false")
	}
	if !strings.Contains(h.Message, "500") {
		t.Errorf("HealthCheck().Message = %q, want to contain status code", h.Message)
	}
}

// ---------------------------------------------------------------------------
// ContributeCaddyRoutes
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_Disabled(t *testing.T) {
	p := auth.New(auth.Config{Enabled: false}, discardLogger(), nil)
	_ = p.Init(context.Background())
	routes := p.ContributeCaddyRoutes()
	if len(routes) != 0 {
		t.Errorf("ContributeCaddyRoutes() = %d routes for disabled plugin, want 0", len(routes))
	}
}

func TestPlugin_ContributeCaddyRoutes_Enabled(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	routes := p.ContributeCaddyRoutes()
	if len(routes) == 0 {
		t.Fatal("ContributeCaddyRoutes() returned empty slice for enabled plugin")
	}
}

func TestPlugin_ContributeCaddyRoutes_HasKratosFlowPaths(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	routes := p.ContributeCaddyRoutes()
	if len(routes) == 0 {
		t.Fatal("ContributeCaddyRoutes() returned empty slice")
	}

	route := routes[0]

	// Route handler must have a "match" key with Kratos paths.
	matchSlice, ok := route.Handler["match"].([]map[string]any)
	if !ok {
		t.Fatalf("route Handler[\"match\"] is not []map[string]any: %T", route.Handler["match"])
	}
	if len(matchSlice) == 0 {
		t.Fatal("route Handler[\"match\"] is empty")
	}

	paths, ok := matchSlice[0]["path"].([]string)
	if !ok {
		t.Fatalf("route match[0][\"path\"] is not []string: %T", matchSlice[0]["path"])
	}

	wantPaths := []string{
		"/self-service/login/*",
		"/self-service/registration/*",
		"/self-service/logout/*",
		"/self-service/settings/*",
		"/self-service/recovery/*",
		"/self-service/verification/*",
		"/.ory/kratos/public/*",
	}

	if len(paths) != len(wantPaths) {
		t.Errorf("Kratos flow paths count = %d, want %d", len(paths), len(wantPaths))
	}
	for _, want := range wantPaths {
		found := false
		for _, got := range paths {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing Kratos flow path %q in route matcher", want)
		}
	}
}

func TestPlugin_ContributeCaddyRoutes_HandlerIsReverseProxy(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	routes := p.ContributeCaddyRoutes()
	if len(routes) == 0 {
		t.Fatal("no routes contributed")
	}

	handleSlice, ok := routes[0].Handler["handle"].([]map[string]any)
	if !ok {
		t.Fatalf("handle is not []map[string]any: %T", routes[0].Handler["handle"])
	}
	if len(handleSlice) == 0 {
		t.Fatal("handle slice is empty")
	}
	if got := handleSlice[0]["handler"]; got != "reverse_proxy" {
		t.Errorf("handler = %q, want %q", got, "reverse_proxy")
	}
}

func TestPlugin_ContributeCaddyRoutes_DialAddrExtractedFromURL(t *testing.T) {
	tests := []struct {
		name            string
		kratosPublicURL string
		wantDialAddr    string
	}{
		{
			name:            "standard http url",
			kratosPublicURL: "http://127.0.0.1:4433",
			wantDialAddr:    "127.0.0.1:4433",
		},
		{
			name:            "https url",
			kratosPublicURL: "https://kratos.example.com:4433",
			wantDialAddr:    "kratos.example.com:4433",
		},
		{
			name:            "http without port defaults to 80",
			kratosPublicURL: "http://kratos.internal",
			wantDialAddr:    "kratos.internal:80",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := auth.Config{
				Enabled:         true,
				Mode:            auth.ModeKratos,
				KratosPublicURL: tt.kratosPublicURL,
			}
			p := auth.New(cfg, discardLogger(), newFakeProvider())
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error: %v", err)
			}
			routes := p.ContributeCaddyRoutes()
			if len(routes) == 0 {
				t.Fatal("no routes contributed")
			}

			handleSlice, ok := routes[0].Handler["handle"].([]map[string]any)
			if !ok || len(handleSlice) == 0 {
				t.Fatal("handle slice invalid")
			}
			upstreams, ok := handleSlice[0]["upstreams"].([]map[string]any)
			if !ok || len(upstreams) == 0 {
				t.Fatal("upstreams slice invalid")
			}
			dialAddr, ok := upstreams[0]["dial"].(string)
			if !ok {
				t.Fatal("dial is not a string")
			}
			if dialAddr != tt.wantDialAddr {
				t.Errorf("dial = %q, want %q", dialAddr, tt.wantDialAddr)
			}
		})
	}
}

func TestPlugin_ContributeCaddyRoutes_Priority(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	routes := p.ContributeCaddyRoutes()
	if len(routes) == 0 {
		t.Fatal("no routes")
	}
	if routes[0].Priority != 40 {
		t.Errorf("route Priority = %d, want 40", routes[0].Priority)
	}
}

// ---------------------------------------------------------------------------
// ContributeCaddyHandlers
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyHandlers_Disabled(t *testing.T) {
	p := auth.New(auth.Config{Enabled: false}, discardLogger(), nil)
	_ = p.Init(context.Background())
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() = %d handlers for disabled plugin, want 0", len(handlers))
	}
}

func TestPlugin_ContributeCaddyHandlers_EnabledReturnsTwoHandlers(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 2 {
		t.Fatalf("ContributeCaddyHandlers() returned %d handlers, want 2", len(handlers))
	}
}

func TestPlugin_ContributeCaddyHandlers_AuthHandler(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) < 1 {
		t.Fatal("no handlers contributed")
	}

	h := handlers[0]
	if h.Priority != 40 {
		t.Errorf("auth handler Priority = %d, want 40", h.Priority)
	}
	if h.Handler["handler"] != "authentication" {
		t.Errorf("auth handler type = %q, want %q", h.Handler["handler"], "authentication")
	}
	if _, ok := h.Handler["cookie_name"]; !ok {
		t.Error("auth handler missing cookie_name field")
	}
	if _, ok := h.Handler["login_url"]; !ok {
		t.Error("auth handler missing login_url field")
	}
	if _, ok := h.Handler["public_paths"]; !ok {
		t.Error("auth handler missing public_paths field")
	}
}

func TestPlugin_ContributeCaddyHandlers_AuthHandler_PublicPathsIncludeVibewardenPrefix(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) < 1 {
		t.Fatal("no handlers")
	}

	authHandler := handlers[0].Handler
	paths, ok := authHandler["public_paths"].([]string)
	if !ok {
		t.Fatalf("public_paths is not []string: %T", authHandler["public_paths"])
	}

	// /_vibewarden/* must always be public.
	found := false
	for _, p := range paths {
		if p == "/_vibewarden/*" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("public_paths does not contain %q; got: %v", "/_vibewarden/*", paths)
	}
}

func TestPlugin_ContributeCaddyHandlers_AuthHandler_UserPublicPathsIncluded(t *testing.T) {
	cfg := defaultConfig()
	cfg.PublicPaths = []string{"/public/*", "/api/docs"}
	p := newPlugin(cfg)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) < 1 {
		t.Fatal("no handlers")
	}

	paths, ok := handlers[0].Handler["public_paths"].([]string)
	if !ok {
		t.Fatalf("public_paths not []string: %T", handlers[0].Handler["public_paths"])
	}

	for _, want := range []string{"/public/*", "/api/docs"} {
		found := false
		for _, got := range paths {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("public_paths missing user-configured path %q; got: %v", want, paths)
		}
	}
}

func TestPlugin_ContributeCaddyHandlers_IdentityHeadersHandler(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) < 2 {
		t.Fatalf("expected at least 2 handlers, got %d", len(handlers))
	}

	h := handlers[1]
	if h.Priority != 41 {
		t.Errorf("identity-headers handler Priority = %d, want 41", h.Priority)
	}
	if h.Handler["handler"] != "identity_headers" {
		t.Errorf("identity-headers handler type = %q, want %q", h.Handler["handler"], "identity_headers")
	}
	if _, ok := h.Handler["cookie_name"]; !ok {
		t.Error("identity-headers handler missing cookie_name field")
	}
}

func TestPlugin_ContributeCaddyHandlers_DefaultCookieName(t *testing.T) {
	// When SessionCookieName is empty, default must be applied.
	cfg := auth.Config{
		Enabled:         true,
		KratosPublicURL: "http://127.0.0.1:4433",
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) < 1 {
		t.Fatal("no handlers")
	}

	cookieName, ok := handlers[0].Handler["cookie_name"].(string)
	if !ok {
		t.Fatalf("cookie_name is not string: %T", handlers[0].Handler["cookie_name"])
	}
	if cookieName != "ory_kratos_session" {
		t.Errorf("cookie_name = %q, want %q", cookieName, "ory_kratos_session")
	}
}

func TestPlugin_ContributeCaddyHandlers_DefaultLoginURL(t *testing.T) {
	cfg := auth.Config{
		Enabled:         true,
		KratosPublicURL: "http://127.0.0.1:4433",
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) < 1 {
		t.Fatal("no handlers")
	}

	loginURL, ok := handlers[0].Handler["login_url"].(string)
	if !ok {
		t.Fatalf("login_url is not string: %T", handlers[0].Handler["login_url"])
	}
	if loginURL != "/self-service/login/browser" {
		t.Errorf("login_url = %q, want %q", loginURL, "/self-service/login/browser")
	}
}

// ---------------------------------------------------------------------------
// IdentityProvider injection
// ---------------------------------------------------------------------------

func TestPlugin_IdentityProvider_FakeInjected(t *testing.T) {
	ident, _ := identity.NewIdentity("user-uuid", "user@example.com", "kratos", true, nil)
	fake := &fakeIdentityProvider{result: identity.Success(ident)}
	p := auth.New(defaultConfig(), discardLogger(), fake)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// The injected provider must be usable directly.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	result := fake.Authenticate(context.Background(), req)
	if !result.Authenticated {
		t.Error("Authenticate() = not authenticated, want authenticated")
	}
	if result.Identity.ID() != ident.ID() {
		t.Errorf("identity ID = %q, want %q", result.Identity.ID(), ident.ID())
	}
}

func TestPlugin_IdentityProvider_FailureResult(t *testing.T) {
	fake := &fakeIdentityProvider{
		result: identity.Failure("session_invalid", "session is invalid"),
	}
	p := auth.New(defaultConfig(), discardLogger(), fake)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	result := fake.Authenticate(context.Background(), req)
	if result.Authenticated {
		t.Error("Authenticate() = authenticated, want not authenticated")
	}
	if result.Reason != "session_invalid" {
		t.Errorf("Reason = %q, want %q", result.Reason, "session_invalid")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

// TestPlugin_ImplementsPortsPlugin asserts at compile time that *Plugin
// satisfies the ports.Plugin interface.
func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*auth.Plugin)(nil)
}

// TestPlugin_ImplementsCaddyContributor asserts at compile time that *Plugin
// satisfies the ports.CaddyContributor interface.
func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*auth.Plugin)(nil)
}

// ---------------------------------------------------------------------------
// Auth UI integration — built-in mode
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_BuiltInUI_ContributesUIRoute(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer p.Stop(context.Background()) //nolint: errcheck

	routes := p.ContributeCaddyRoutes()
	// Expect at least 2 routes: Kratos proxy + auth UI.
	if len(routes) < 2 {
		t.Fatalf("ContributeCaddyRoutes() returned %d routes, want at least 2 (Kratos + auth UI)", len(routes))
	}

	// The auth UI route should proxy to a localhost address.
	uiRoute := routes[1]
	handleSlice, ok := uiRoute.Handler["handle"].([]map[string]any)
	if !ok || len(handleSlice) == 0 {
		t.Fatal("auth UI route handle slice invalid")
	}
	if got := handleSlice[0]["handler"]; got != "reverse_proxy" {
		t.Errorf("auth UI route handler = %q, want reverse_proxy", got)
	}
	upstreams, ok := handleSlice[0]["upstreams"].([]map[string]any)
	if !ok || len(upstreams) == 0 {
		t.Fatal("auth UI route upstreams invalid")
	}
	dial, _ := upstreams[0]["dial"].(string)
	if dial == "" {
		t.Error("auth UI route upstream dial address is empty")
	}
}

func TestPlugin_ContributeCaddyRoutes_BuiltInUI_MatchesFourPaths(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer p.Stop(context.Background()) //nolint: errcheck

	routes := p.ContributeCaddyRoutes()
	if len(routes) < 2 {
		t.Fatalf("expected at least 2 routes, got %d", len(routes))
	}

	uiRoute := routes[1]
	matchSlice, ok := uiRoute.Handler["match"].([]map[string]any)
	if !ok || len(matchSlice) == 0 {
		t.Fatal("auth UI route match slice invalid")
	}
	paths, ok := matchSlice[0]["path"].([]string)
	if !ok {
		t.Fatalf("auth UI match path is not []string: %T", matchSlice[0]["path"])
	}

	wantPaths := []string{
		"/_vibewarden/login",
		"/_vibewarden/registration",
		"/_vibewarden/recovery",
		"/_vibewarden/verification",
	}
	for _, want := range wantPaths {
		found := false
		for _, got := range paths {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("auth UI route missing path %q; got: %v", want, paths)
		}
	}
}

func TestPlugin_ContributeCaddyRoutes_CustomUI_NoUIRoute(t *testing.T) {
	cfg := defaultConfig() // already sets Mode: auth.ModeKratos via defaultConfig()
	cfg.UI = auth.UIConfig{Mode: "custom", LoginURL: "https://example.com/login"}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer p.Stop(context.Background()) //nolint: errcheck

	routes := p.ContributeCaddyRoutes()
	// Only the Kratos proxy route; no UI route.
	if len(routes) != 1 {
		t.Errorf("ContributeCaddyRoutes() returned %d routes for custom UI mode, want 1", len(routes))
	}
}

func TestPlugin_Stop_StopsUIHandler(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Stop must not error.
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

func TestPlugin_Stop_DisabledPlugin_IsNoop(t *testing.T) {
	p := auth.New(auth.Config{Enabled: false}, discardLogger(), nil)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() error for disabled plugin: %v", err)
	}
}

// ---------------------------------------------------------------------------
// UIConfig
// ---------------------------------------------------------------------------

func TestPlugin_UIConfig_DefaultsApplied(t *testing.T) {
	// With no UI config, built-in mode should be activated.
	cfg := auth.Config{
		Enabled:         true,
		Mode:            auth.ModeKratos,
		KratosPublicURL: "http://127.0.0.1:4433",
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer p.Stop(context.Background()) //nolint: errcheck

	routes := p.ContributeCaddyRoutes()
	// Built-in mode adds an auth UI route.
	if len(routes) < 2 {
		t.Errorf("ContributeCaddyRoutes() = %d routes, want >=2 (UI route expected by default)", len(routes))
	}
}

// ---------------------------------------------------------------------------
// Settings page route — built-in mode
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_BuiltInUI_MatchesFivePaths(t *testing.T) {
	p := newPlugin(defaultConfig())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer p.Stop(context.Background()) //nolint: errcheck

	routes := p.ContributeCaddyRoutes()
	if len(routes) < 2 {
		t.Fatalf("expected at least 2 routes, got %d", len(routes))
	}

	uiRoute := routes[1]
	matchSlice, ok := uiRoute.Handler["match"].([]map[string]any)
	if !ok || len(matchSlice) == 0 {
		t.Fatal("auth UI route match slice invalid")
	}
	paths, ok := matchSlice[0]["path"].([]string)
	if !ok {
		t.Fatalf("auth UI match path is not []string: %T", matchSlice[0]["path"])
	}

	wantPaths := []string{
		"/_vibewarden/login",
		"/_vibewarden/registration",
		"/_vibewarden/recovery",
		"/_vibewarden/verification",
		"/_vibewarden/settings",
	}
	for _, want := range wantPaths {
		found := false
		for _, got := range paths {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("auth UI route missing path %q; got: %v", want, paths)
		}
	}
}

// ---------------------------------------------------------------------------
// Custom mode — config validation
// ---------------------------------------------------------------------------

func TestPlugin_Init_CustomUI_RequiresLoginURL(t *testing.T) {
	cfg := auth.Config{
		Enabled:         true,
		Mode:            auth.ModeKratos,
		KratosPublicURL: "http://127.0.0.1:4433",
		UI:              auth.UIConfig{Mode: "custom"},
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	err := p.Init(context.Background())
	if err == nil {
		t.Fatal("Init() expected error for custom mode without login_url, got nil")
	}
	if !strings.Contains(err.Error(), "login_url") {
		t.Errorf("Init() error = %q, want to mention login_url", err.Error())
	}
}

func TestPlugin_Init_CustomUI_WithLoginURL_Succeeds(t *testing.T) {
	cfg := auth.Config{
		Enabled:         true,
		Mode:            auth.ModeKratos,
		KratosPublicURL: "http://127.0.0.1:4433",
		UI:              auth.UIConfig{Mode: "custom", LoginURL: "https://example.com/login"},
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Custom mode — auth handler uses custom login URL
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyHandlers_CustomUI_UsesConfiguredLoginURL(t *testing.T) {
	customLoginURL := "https://example.com/my-login"
	cfg := auth.Config{
		Enabled:         true,
		Mode:            auth.ModeKratos,
		KratosPublicURL: "http://127.0.0.1:4433",
		UI:              auth.UIConfig{Mode: "custom", LoginURL: customLoginURL},
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	handlers := p.ContributeCaddyHandlers()
	if len(handlers) < 1 {
		t.Fatal("no handlers contributed")
	}

	loginURL, ok := handlers[0].Handler["login_url"].(string)
	if !ok {
		t.Fatalf("login_url is not string: %T", handlers[0].Handler["login_url"])
	}
	if loginURL != customLoginURL {
		t.Errorf("login_url = %q, want %q", loginURL, customLoginURL)
	}
}

func TestPlugin_ContributeCaddyRoutes_CustomUI_OnlyKratosRoute(t *testing.T) {
	cfg := auth.Config{
		Enabled:         true,
		Mode:            auth.ModeKratos,
		KratosPublicURL: "http://127.0.0.1:4433",
		UI: auth.UIConfig{
			Mode:            "custom",
			LoginURL:        "https://example.com/login",
			RegistrationURL: "https://example.com/register",
			SettingsURL:     "https://example.com/settings",
			RecoveryURL:     "https://example.com/recovery",
		},
	}
	p := auth.New(cfg, discardLogger(), newFakeProvider())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	routes := p.ContributeCaddyRoutes()
	// Custom mode must not add an auth UI route — only the Kratos proxy.
	if len(routes) != 1 {
		t.Errorf("ContributeCaddyRoutes() = %d routes for custom UI, want 1", len(routes))
	}
}

// ---------------------------------------------------------------------------
// Mode-awareness tests — issue #386
// ---------------------------------------------------------------------------

// TestPlugin_Init_NonKratosModes verifies that non-kratos modes do not require
// KratosPublicURL and complete Init without error.
func TestPlugin_Init_NonKratosModes(t *testing.T) {
	tests := []struct {
		name string
		mode auth.Mode
	}{
		{"none mode", auth.ModeNone},
		{"jwt mode", auth.ModeJWT},
		{"api-key mode", auth.ModeAPIKey},
		{"empty mode treated as none", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := auth.Config{
				Enabled: true,
				Mode:    tt.mode,
				// KratosPublicURL intentionally absent.
			}
			p := auth.New(cfg, discardLogger(), nil)
			if err := p.Init(context.Background()); err != nil {
				t.Errorf("Init() error = %v, want nil for mode %q", err, tt.mode)
			}
		})
	}
}

// TestPlugin_ContributeCaddyRoutes_NonKratosModes verifies that Kratos flow
// routes are not contributed when the mode is not "kratos".
func TestPlugin_ContributeCaddyRoutes_NonKratosModes(t *testing.T) {
	tests := []struct {
		name string
		mode auth.Mode
	}{
		{"none mode", auth.ModeNone},
		{"jwt mode", auth.ModeJWT},
		{"api-key mode", auth.ModeAPIKey},
		{"empty mode treated as none", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := auth.Config{
				Enabled: true,
				Mode:    tt.mode,
			}
			p := auth.New(cfg, discardLogger(), nil)
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error: %v", err)
			}
			routes := p.ContributeCaddyRoutes()
			if len(routes) != 0 {
				t.Errorf("ContributeCaddyRoutes() returned %d routes for mode %q, want 0 (no Kratos routes)", len(routes), tt.mode)
			}
		})
	}
}

// TestPlugin_HealthCheck_NonKratosModes verifies that HealthCheck does not
// attempt a live Kratos probe for non-kratos modes and always returns healthy.
func TestPlugin_HealthCheck_NonKratosModes(t *testing.T) {
	tests := []struct {
		name string
		mode auth.Mode
	}{
		{"none mode", auth.ModeNone},
		{"jwt mode", auth.ModeJWT},
		{"api-key mode", auth.ModeAPIKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := auth.Config{
				Enabled:         true,
				Mode:            tt.mode,
				KratosPublicURL: "http://127.0.0.1:19999", // unreachable — must not be probed
			}
			p := auth.New(cfg, discardLogger(), nil)
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error: %v", err)
			}
			h := p.HealthCheck(context.Background())
			if !h.Healthy {
				t.Errorf("HealthCheck() healthy = false for mode %q, want true (no Kratos probe expected)", tt.mode)
			}
		})
	}
}

// TestPlugin_CheckDependency_NonKratosModes verifies that CheckDependency does
// not attempt a live Kratos probe for non-kratos modes.
func TestPlugin_CheckDependency_NonKratosModes(t *testing.T) {
	tests := []struct {
		name string
		mode auth.Mode
	}{
		{"none mode", auth.ModeNone},
		{"jwt mode", auth.ModeJWT},
		{"api-key mode", auth.ModeAPIKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := auth.Config{
				Enabled:         true,
				Mode:            tt.mode,
				KratosPublicURL: "http://127.0.0.1:19999", // unreachable — must not be probed
			}
			p := auth.New(cfg, discardLogger(), nil)
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error: %v", err)
			}
			status := p.CheckDependency(context.Background())
			if status.Status != "healthy" {
				t.Errorf("CheckDependency() status = %q for mode %q, want \"healthy\"", status.Status, tt.mode)
			}
		})
	}
}

// TestPlugin_Health_NonKratosModes verifies that Health() reports healthy for
// non-kratos modes after a successful Init.
func TestPlugin_Health_NonKratosModes(t *testing.T) {
	tests := []struct {
		name string
		mode auth.Mode
	}{
		{"none mode", auth.ModeNone},
		{"jwt mode", auth.ModeJWT},
		{"api-key mode", auth.ModeAPIKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := auth.Config{Enabled: true, Mode: tt.mode}
			p := auth.New(cfg, discardLogger(), nil)
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error: %v", err)
			}
			h := p.Health()
			if !h.Healthy {
				t.Errorf("Health().Healthy = false for mode %q, want true", tt.mode)
			}
			if !strings.Contains(h.Message, string(tt.mode)) {
				t.Errorf("Health().Message = %q, want to contain mode %q", h.Message, tt.mode)
			}
		})
	}
}
