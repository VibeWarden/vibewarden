package usermgmt_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpadapter "github.com/vibewarden/vibewarden/internal/adapters/http"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/plugins/usermgmt"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeAdminService implements ports.AdminService for testing.
type fakeAdminService struct {
	listErr       error
	getUserErr    error
	inviteErr     error
	deactivateErr error
}

func (f *fakeAdminService) ListUsers(_ context.Context, _ ports.Pagination) (*ports.PaginatedUsers, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &ports.PaginatedUsers{Users: []user.User{}, Total: 0}, nil
}

func (f *fakeAdminService) GetUser(_ context.Context, _ string) (*user.User, error) {
	if f.getUserErr != nil {
		return nil, f.getUserErr
	}
	u := user.User{ID: "test-id", Email: "test@example.com"}
	return &u, nil
}

func (f *fakeAdminService) InviteUser(_ context.Context, _ string, _ string) (*ports.InviteResult, error) {
	if f.inviteErr != nil {
		return nil, f.inviteErr
	}
	return &ports.InviteResult{User: user.User{ID: "new-id", Email: "new@example.com"}}, nil
}

func (f *fakeAdminService) DeactivateUser(_ context.Context, _ string, _ string, _ string) error {
	return f.deactivateErr
}

// fakeAdminServer implements adminServer interface for testing without binding
// a real port.
type fakeAdminServer struct {
	started  bool
	stopped  bool
	addr     string
	startErr error
	stopErr  error
}

func (s *fakeAdminServer) Start() error {
	if s.startErr != nil {
		return s.startErr
	}
	s.started = true
	if s.addr == "" {
		s.addr = "127.0.0.1:19999"
	}
	return nil
}

func (s *fakeAdminServer) Addr() string { return s.addr }

func (s *fakeAdminServer) Stop(_ context.Context) error {
	s.stopped = true
	return s.stopErr
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// noopWriter discards all writes.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

// discardLogger returns an slog.Logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

// fakeNopEventLogger is a ports.EventLogger that always succeeds.
type fakeNopEventLogger struct{}

func (f *fakeNopEventLogger) Log(_ context.Context, _ events.Event) error { return nil }

// defaultConfig returns a valid enabled Config for testing.
func defaultConfig() usermgmt.Config {
	return usermgmt.Config{
		Enabled:        true,
		AdminToken:     "super-secret-token",
		KratosAdminURL: "http://127.0.0.1:4434",
		DatabaseURL:    "",
	}
}

// ---------------------------------------------------------------------------
// Name / Priority
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if got := p.Name(); got != "user-management" {
		t.Errorf("Name() = %q, want %q", got, "user-management")
	}
}

func TestPlugin_Priority(t *testing.T) {
	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if got := p.Priority(); got != 60 {
		t.Errorf("Priority() = %d, want 60", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     usermgmt.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "disabled — no validation performed",
			cfg:     usermgmt.Config{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled without admin token",
			cfg:     usermgmt.Config{Enabled: true, KratosAdminURL: "http://127.0.0.1:4434"},
			wantErr: true,
			errMsg:  "admin_token is required",
		},
		{
			name:    "enabled without kratos admin url",
			cfg:     usermgmt.Config{Enabled: true, AdminToken: "token"},
			wantErr: true,
			errMsg:  "kratos_admin_url is required",
		},
		{
			name:    "enabled with invalid kratos admin url",
			cfg:     usermgmt.Config{Enabled: true, AdminToken: "token", KratosAdminURL: "not-a-url"},
			wantErr: true,
			errMsg:  "not a valid URL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Install a fake service factory so Init does not dial Kratos or Postgres.
			old := usermgmt.ExportedServiceFactory
			usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
				return &fakeAdminService{}, nil, nil, nil
			}
			defer func() { usermgmt.ExportedServiceFactory = old }()

			p := usermgmt.New(tt.cfg, &fakeNopEventLogger{}, discardLogger())
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

func TestPlugin_Init_ServiceFactoryError(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return nil, nil, nil, errors.New("db connection failed")
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	err := p.Init(context.Background())
	if err == nil {
		t.Fatal("Init() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "db connection failed") {
		t.Errorf("Init() error = %q, want to contain %q", err.Error(), "db connection failed")
	}
}

// ---------------------------------------------------------------------------
// Start / Stop
// ---------------------------------------------------------------------------

func TestPlugin_Start_BindsServer(t *testing.T) {
	fake := &fakeAdminServer{addr: "127.0.0.1:19999"}

	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerAPI {
		return fake
	}
	defer func() { usermgmt.ExportedServerFactory = oldSrv }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !fake.started {
		t.Error("Start() did not call server.Start()")
	}
	if p.InternalAddr() != "127.0.0.1:19999" {
		t.Errorf("InternalAddr() = %q, want %q", p.InternalAddr(), "127.0.0.1:19999")
	}
}

func TestPlugin_Start_Disabled_IsNoop(t *testing.T) {
	p := usermgmt.New(usermgmt.Config{Enabled: false}, &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() on disabled plugin returned error: %v", err)
	}
}

func TestPlugin_Start_ServerStartError(t *testing.T) {
	fake := &fakeAdminServer{startErr: errors.New("port in use")}

	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerAPI {
		return fake
	}
	defer func() { usermgmt.ExportedServerFactory = oldSrv }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	err := p.Start(context.Background())
	if err == nil {
		t.Fatal("Start() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "port in use") {
		t.Errorf("Start() error = %q, want to contain %q", err.Error(), "port in use")
	}
}

func TestPlugin_Stop_CallsServerStop(t *testing.T) {
	fake := &fakeAdminServer{addr: "127.0.0.1:19999"}

	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerAPI {
		return fake
	}
	defer func() { usermgmt.ExportedServerFactory = oldSrv }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
	if !fake.stopped {
		t.Error("Stop() did not call server.Stop()")
	}
}

func TestPlugin_Stop_Disabled_IsNoop(t *testing.T) {
	p := usermgmt.New(usermgmt.Config{Enabled: false}, &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() on disabled plugin returned error: %v", err)
	}
}

func TestPlugin_Stop_ServerStopError(t *testing.T) {
	fake := &fakeAdminServer{addr: "127.0.0.1:19999", stopErr: errors.New("shutdown timeout")}

	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerAPI {
		return fake
	}
	defer func() { usermgmt.ExportedServerFactory = oldSrv }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	err := p.Stop(context.Background())
	if err == nil {
		t.Fatal("Stop() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "shutdown timeout") {
		t.Errorf("Stop() error = %q, want to contain %q", err.Error(), "shutdown timeout")
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestPlugin_Health_Disabled(t *testing.T) {
	p := usermgmt.New(usermgmt.Config{Enabled: false}, &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	h := p.Health()
	if !h.Healthy {
		t.Errorf("Health().Healthy = false for disabled plugin, want true")
	}
	if !strings.Contains(h.Message, "disabled") {
		t.Errorf("Health().Message = %q, want to contain %q", h.Message, "disabled")
	}
}

func TestPlugin_Health_EnabledAfterInit(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	h := p.Health()
	if !h.Healthy {
		t.Errorf("Health().Healthy = false after Init, want true")
	}
	if !strings.Contains(h.Message, "configured") {
		t.Errorf("Health().Message = %q, want to contain %q", h.Message, "configured")
	}
}

func TestPlugin_Health_Table(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	tests := []struct {
		name           string
		cfg            usermgmt.Config
		wantHealthy    bool
		wantMsgContain string
	}{
		{
			name:           "disabled",
			cfg:            usermgmt.Config{Enabled: false},
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
			p := usermgmt.New(tt.cfg, &fakeNopEventLogger{}, discardLogger())
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

func TestPlugin_HealthCheck_KratosAdminReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/ready" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := usermgmt.Config{
		Enabled:        true,
		AdminToken:     "token",
		KratosAdminURL: srv.URL,
	}
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	p := usermgmt.New(cfg, &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	h := p.HealthCheck(context.Background())
	if !h.Healthy {
		t.Errorf("HealthCheck() healthy = false, want true; message: %s", h.Message)
	}
}

func TestPlugin_HealthCheck_KratosAdminUnreachable(t *testing.T) {
	cfg := usermgmt.Config{
		Enabled:        true,
		AdminToken:     "token",
		KratosAdminURL: "http://127.0.0.1:19998", // nothing listening
	}
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	p := usermgmt.New(cfg, &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	h := p.HealthCheck(context.Background())
	if h.Healthy {
		t.Error("HealthCheck() healthy = true for unreachable Kratos admin, want false")
	}
}

func TestPlugin_HealthCheck_Disabled(t *testing.T) {
	p := usermgmt.New(usermgmt.Config{Enabled: false}, &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	h := p.HealthCheck(context.Background())
	if !h.Healthy {
		t.Error("HealthCheck() healthy = false for disabled plugin, want true")
	}
}

func TestPlugin_HealthCheck_KratosAdminServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := usermgmt.Config{
		Enabled:        true,
		AdminToken:     "token",
		KratosAdminURL: srv.URL,
	}
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	p := usermgmt.New(cfg, &fakeNopEventLogger{}, discardLogger())
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
// ContributeCaddyRoutes — no-op regression lock (#1393)
// ---------------------------------------------------------------------------

// TestPlugin_ContributeCaddyRoutes_AlwaysNil locks the post-#1393 no-op contract:
// ContributeCaddyRoutes must return nil for both enabled and disabled plugins.
// The canonical admin route is built by the caddy adapter (buildAdminRoute in
// config_routes.go). A non-nil return here would introduce a duplicate
// unauthenticated admin route — the auth-bypass described in #1393.
func TestPlugin_ContributeCaddyRoutes_AlwaysNil(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"disabled plugin", false},
		{"enabled plugin", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := usermgmt.ExportedServiceFactory
			usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
				return &fakeAdminService{}, nil, nil, nil
			}
			defer func() { usermgmt.ExportedServiceFactory = old }()

			cfg := usermgmt.Config{Enabled: tt.enabled}
			if tt.enabled {
				cfg = defaultConfig()
			}
			p := usermgmt.New(cfg, &fakeNopEventLogger{}, discardLogger())
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error: %v", err)
			}

			routes := p.ContributeCaddyRoutes()
			if routes != nil {
				t.Errorf("ContributeCaddyRoutes() = %v, want nil (no-op: admin route is owned by caddy adapter)", routes)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ContributeCaddyHandlers — no-op regression lock (#1393)
// ---------------------------------------------------------------------------

// TestPlugin_ContributeCaddyHandlers_AlwaysNil locks the post-#1393 no-op contract:
// ContributeCaddyHandlers must return nil for both enabled and disabled plugins.
// The divergent "admin_auth" handler map (wrong module name — should be
// "vibewarden_admin_auth") was the direct cause of the sidecar crash-loop in
// #1393: caddy.Load rejected the unknown handler name. The canonical gate is
// now inlined into the admin route by the caddy adapter.
func TestPlugin_ContributeCaddyHandlers_AlwaysNil(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"disabled plugin", false},
		{"enabled plugin", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := usermgmt.ExportedServiceFactory
			usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
				return &fakeAdminService{}, nil, nil, nil
			}
			defer func() { usermgmt.ExportedServiceFactory = old }()

			cfg := usermgmt.Config{Enabled: tt.enabled}
			if tt.enabled {
				cfg = defaultConfig()
			}
			p := usermgmt.New(cfg, &fakeNopEventLogger{}, discardLogger())
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error: %v", err)
			}

			handlers := p.ContributeCaddyHandlers()
			if handlers != nil {
				t.Errorf("ContributeCaddyHandlers() = %v, want nil (no-op: gate is inlined into admin route by caddy adapter)", handlers)
			}

			// Extra guard: confirm the divergent "admin_auth" map (wrong module
			// name) is NOT present. This is what caused caddy.Load to fail in #1393.
			for _, h := range handlers {
				if name, _ := h.Handler["handler"].(string); name == "admin_auth" {
					t.Errorf("ContributeCaddyHandlers() emitted divergent admin_auth map — this crashes Caddy (wrong module name; should be vibewarden_admin_auth)")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// InternalAddr
// ---------------------------------------------------------------------------

func TestPlugin_InternalAddr_Empty_BeforeStart(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), usermgmt.PostgresProber, error) {
		return &fakeAdminService{}, nil, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if got := p.InternalAddr(); got != "" {
		t.Errorf("InternalAddr() before Start = %q, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

// TestPlugin_ImplementsPortsPlugin asserts at compile time that *Plugin
// satisfies ports.Plugin.
func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*usermgmt.Plugin)(nil)
}

// TestPlugin_ImplementsCaddyContributor asserts at compile time that *Plugin
// satisfies ports.CaddyContributor.
func TestPlugin_ImplementsCaddyContributor(t *testing.T) {
	var _ ports.CaddyContributor = (*usermgmt.Plugin)(nil)
}

// TestPlugin_ImplementsInternalServerPlugin asserts at compile time that
// *Plugin satisfies ports.InternalServerPlugin.
func TestPlugin_ImplementsInternalServerPlugin(t *testing.T) {
	var _ ports.InternalServerPlugin = (*usermgmt.Plugin)(nil)
}
