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
			usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
				return &fakeAdminService{}, nil, nil
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return nil, nil, errors.New("db connection failed")
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerIface {
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerIface {
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerIface {
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerIface {
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
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
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
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
// ContributeCaddyRoutes
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyRoutes_Disabled(t *testing.T) {
	p := usermgmt.New(usermgmt.Config{Enabled: false}, &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	routes := p.ContributeCaddyRoutes()
	if len(routes) != 0 {
		t.Errorf("ContributeCaddyRoutes() = %d routes for disabled plugin, want 0", len(routes))
	}
}

func TestPlugin_ContributeCaddyRoutes_Enabled(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerIface {
		return &fakeAdminServer{addr: "127.0.0.1:19999"}
	}
	defer func() { usermgmt.ExportedServerFactory = oldSrv }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	routes := p.ContributeCaddyRoutes()
	if len(routes) == 0 {
		t.Fatal("ContributeCaddyRoutes() returned empty slice for enabled plugin")
	}
}

func TestPlugin_ContributeCaddyRoutes_HandlerIsReverseProxy(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerIface {
		return &fakeAdminServer{addr: "127.0.0.1:19999"}
	}
	defer func() { usermgmt.ExportedServerFactory = oldSrv }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
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

func TestPlugin_ContributeCaddyRoutes_DialAddrMatchesInternalAddr(t *testing.T) {
	const wantAddr = "127.0.0.1:19999"

	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerIface {
		return &fakeAdminServer{addr: wantAddr}
	}
	defer func() { usermgmt.ExportedServerFactory = oldSrv }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
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
	if dialAddr != wantAddr {
		t.Errorf("dial = %q, want %q", dialAddr, wantAddr)
	}
}

func TestPlugin_ContributeCaddyRoutes_Priority(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	oldSrv := usermgmt.ExportedServerFactory
	usermgmt.ExportedServerFactory = func(_ *httpadapter.AdminHandlers, _ *slog.Logger) usermgmt.AdminServerIface {
		return &fakeAdminServer{addr: "127.0.0.1:19999"}
	}
	defer func() { usermgmt.ExportedServerFactory = oldSrv }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	routes := p.ContributeCaddyRoutes()
	if len(routes) == 0 {
		t.Fatal("no routes")
	}
	if routes[0].Priority != 60 {
		t.Errorf("route Priority = %d, want 60", routes[0].Priority)
	}
}

// ---------------------------------------------------------------------------
// ContributeCaddyHandlers
// ---------------------------------------------------------------------------

func TestPlugin_ContributeCaddyHandlers_Disabled(t *testing.T) {
	p := usermgmt.New(usermgmt.Config{Enabled: false}, &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() = %d handlers for disabled plugin, want 0", len(handlers))
	}
}

func TestPlugin_ContributeCaddyHandlers_Enabled(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("ContributeCaddyHandlers() = %d handlers, want 1", len(handlers))
	}
}

func TestPlugin_ContributeCaddyHandlers_AdminAuthHandler(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	p := usermgmt.New(defaultConfig(), &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	handlers := p.ContributeCaddyHandlers()
	if len(handlers) < 1 {
		t.Fatal("no handlers contributed")
	}

	h := handlers[0]
	if h.Priority != 60 {
		t.Errorf("admin_auth handler Priority = %d, want 60", h.Priority)
	}
	if h.Handler["handler"] != "admin_auth" {
		t.Errorf("handler type = %q, want %q", h.Handler["handler"], "admin_auth")
	}
	if _, ok := h.Handler["admin_token"]; !ok {
		t.Error("admin_auth handler missing admin_token field")
	}
	if _, ok := h.Handler["admin_path"]; !ok {
		t.Error("admin_auth handler missing admin_path field")
	}
}

func TestPlugin_ContributeCaddyHandlers_AdminTokenSet(t *testing.T) {
	const wantToken = "my-secret-token"

	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
	}
	defer func() { usermgmt.ExportedServiceFactory = old }()

	cfg := defaultConfig()
	cfg.AdminToken = wantToken
	p := usermgmt.New(cfg, &fakeNopEventLogger{}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	handlers := p.ContributeCaddyHandlers()
	if len(handlers) < 1 {
		t.Fatal("no handlers")
	}
	token, ok := handlers[0].Handler["admin_token"].(string)
	if !ok {
		t.Fatalf("admin_token is not string: %T", handlers[0].Handler["admin_token"])
	}
	if token != wantToken {
		t.Errorf("admin_token = %q, want %q", token, wantToken)
	}
}

// ---------------------------------------------------------------------------
// InternalAddr
// ---------------------------------------------------------------------------

func TestPlugin_InternalAddr_Empty_BeforeStart(t *testing.T) {
	old := usermgmt.ExportedServiceFactory
	usermgmt.ExportedServiceFactory = func(_ usermgmt.Config, _ ports.EventLogger, _ *slog.Logger) (ports.AdminService, func(), error) {
		return &fakeAdminService{}, nil, nil
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
