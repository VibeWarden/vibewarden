package usermgmt

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	httpadapter "github.com/vibewarden/vibewarden/internal/adapters/http"
	kratosadapter "github.com/vibewarden/vibewarden/internal/adapters/kratos"
	postgresadapter "github.com/vibewarden/vibewarden/internal/adapters/postgres"
	adminapp "github.com/vibewarden/vibewarden/internal/app/admin"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Ensure Plugin implements ports.DependencyChecker at compile time.
var _ ports.DependencyChecker = (*Plugin)(nil)

// adminPath is the URL path wildcard that the plugin exposes through Caddy.
const adminPath = "/_vibewarden/admin/*"

// adminPathPrefix is the route prefix used when matching admin requests.
const adminPathPrefix = "/_vibewarden/admin/"

// configPath is the URL path wildcard for config hot-reload endpoints.
const configPath = "/_vibewarden/config/*"

// configPathPrefix is the route prefix for config endpoints.
const configPathPrefix = "/_vibewarden/config/"

// healthCheckTimeout is the deadline used for Kratos admin API connectivity
// probes performed during HealthCheck.
const healthCheckTimeout = 3 * time.Second

// kratosAdminHealthPath is the Kratos admin health endpoint used as a
// liveness probe.
const kratosAdminHealthPath = "/health/ready"

// AdminServerIface is the minimal interface the plugin needs from its internal
// HTTP server. Exported so that test packages can provide fakes without
// importing the concrete httpadapter package.
type AdminServerIface interface {
	// Start binds the listener and begins serving.
	Start() error
	// Addr returns the host:port the server is listening on, after Start.
	Addr() string
	// Stop gracefully shuts down the server.
	Stop(ctx context.Context) error
}

// ExportedServiceFactory is a replaceable factory that creates a
// ports.AdminService from the plugin config. The second return value is an
// optional cleanup func (e.g. to close a database connection) that is called
// by Stop. The third return value is an optional postgresProber for health
// checking (nil when no database is configured). Tests may replace this
// variable to inject fakes without dialling Kratos or PostgreSQL.
//
// Exported for testing only — production code must not reassign this.
var ExportedServiceFactory func(Config, ports.EventLogger, *slog.Logger) (ports.AdminService, func(), PostgresProber, error) = defaultServiceFactory

// ExportedServerFactory is a replaceable factory that creates an
// AdminServerIface backed by the supplied handlers. Tests may replace this
// variable to inject a fake server that does not bind a real port.
//
// Exported for testing only — production code must not reassign this.
var ExportedServerFactory func(*httpadapter.AdminHandlers, *slog.Logger) AdminServerIface = defaultServerFactory

// defaultServiceFactory builds the real admin application service, wiring the
// Kratos admin adapter and the optional PostgreSQL audit logger.
// The third return value is the postgres adapter when a DatabaseURL is
// configured, or nil otherwise. It is stored on the plugin for health checking.
func defaultServiceFactory(cfg Config, eventLogger ports.EventLogger, logger *slog.Logger) (ports.AdminService, func(), PostgresProber, error) {
	kratosAdmin := kratosadapter.NewAdminAdapter(cfg.KratosAdminURL, 0, logger)

	var auditLogger ports.AuditLogger
	var cleanup func()
	var prober postgresProber

	if cfg.DatabaseURL != "" {
		adapter, err := postgresadapter.NewAuditAdapter(cfg.DatabaseURL)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("connecting to audit database: %w", err)
		}
		auditLogger = adapter
		prober = adapter
		cleanup = func() {
			if closeErr := adapter.Close(); closeErr != nil {
				logger.Error("closing audit database", slog.String("error", closeErr.Error()))
			}
		}
	}

	svc := adminapp.NewService(kratosAdmin, eventLogger, auditLogger)
	return svc, cleanup, prober, nil
}

// defaultServerFactory builds the real internal AdminServer.
func defaultServerFactory(handlers *httpadapter.AdminHandlers, logger *slog.Logger) AdminServerIface {
	return httpadapter.NewAdminServer(handlers, logger)
}

// PostgresProber is a minimal interface for checking Postgres connectivity.
// It is satisfied by *postgresadapter.AuditAdapter and by test fakes.
// Exported so that the ExportedServiceFactory type signature is accessible
// from external test packages.
type PostgresProber interface {
	// Ping sends a connectivity probe to the database.
	Ping(ctx context.Context) error
}

// postgresProber is the internal alias used in the plugin struct and methods.
type postgresProber = PostgresProber

// Plugin is the VibeWarden user-management plugin.
//
// It implements ports.Plugin, ports.CaddyContributor, ports.InternalServerPlugin,
// and ports.DependencyChecker. Priority is 60, placing it after TLS (10),
// security-headers (20), rate-limiting (30), and auth (40).
//
// Responsibilities:
//   - Validate configuration on Init.
//   - Create a Kratos admin adapter, admin application service, and HTTP
//     handlers on Init.
//   - Start an internal HTTP server on a random localhost port on Start.
//   - Contribute a Caddy route that reverse-proxies /_vibewarden/admin/* to
//     the internal server (ContributeCaddyRoutes).
//   - Contribute the admin-auth handler that validates the X-Admin-Key bearer
//     token (ContributeCaddyHandlers).
//   - Report the internal server address (InternalAddr).
//   - Report the health of the admin server (Health).
//   - Report Postgres connectivity for the health endpoint (CheckDependency).
type Plugin struct {
	cfg          Config
	logger       *slog.Logger
	eventLogger  ports.EventLogger
	handlers     *httpadapter.AdminHandlers
	server       AdminServerIface
	dbCleanup    func()
	dbProber     postgresProber // non-nil only when DatabaseURL is set
	internalAddr string
	healthy      bool
	healthMsg    string
}

// New creates a new user-management Plugin with the given configuration,
// event logger, and structured logger.
// eventLogger must be non-nil; it is forwarded to the admin application service.
func New(cfg Config, eventLogger ports.EventLogger, logger *slog.Logger) *Plugin {
	return &Plugin{
		cfg:         cfg,
		logger:      logger,
		eventLogger: eventLogger,
	}
}

// Name returns the canonical plugin identifier "user-management".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "user-management" }

// InjectReloader sets the ConfigReloader on the admin handlers after the
// reload service has been constructed in serve.go. It must be called after
// Init and before Start so that the handlers are updated before the admin
// server begins serving requests.
//
// When the plugin is disabled this is a no-op.
func (p *Plugin) InjectReloader(r ports.ConfigReloader) {
	if p.handlers != nil {
		p.handlers = p.handlers.WithReloader(r)
	}
}

// InjectRingBuffer sets the EventRingBuffer on the admin handlers so that the
// /_vibewarden/admin/events endpoint can serve recent events. It must be called
// after Init and before Start.
//
// When the plugin is disabled or handlers have not been initialised, this is a
// no-op.
func (p *Plugin) InjectRingBuffer(rb ports.EventRingBuffer) {
	if p.handlers != nil {
		p.handlers = p.handlers.WithEventRingBuffer(rb)
	}
}

// Priority returns the plugin's initialisation priority.
// User management is assigned priority 60 so it is initialised after TLS (10),
// security-headers (20), rate-limiting (30), and auth (40).
func (p *Plugin) Priority() int { return 60 }

// Init validates the plugin configuration and creates the admin service and
// HTTP handlers. It must be called before Start. Long-running work (server
// binding) is deferred to Start.
//
// Returns an error when:
//   - Enabled is true but AdminToken is empty.
//   - Enabled is true but KratosAdminURL is empty or not a valid URL.
//   - The audit database cannot be reached (when DatabaseURL is non-empty).
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		p.healthy = true
		p.healthMsg = "user-management disabled"
		return nil
	}

	if err := validateConfig(p.cfg); err != nil {
		return fmt.Errorf("user-management plugin init: %w", err)
	}

	svc, cleanup, prober, err := ExportedServiceFactory(p.cfg, p.eventLogger, p.logger)
	if err != nil {
		return fmt.Errorf("user-management plugin init: %w", err)
	}
	p.dbCleanup = cleanup
	p.dbProber = prober
	p.handlers = httpadapter.NewAdminHandlers(svc, p.logger)

	p.healthy = true
	p.healthMsg = fmt.Sprintf("user-management configured, kratos admin: %s", p.cfg.KratosAdminURL)

	p.logger.Info("user-management plugin initialised",
		slog.String("kratos_admin_url", p.cfg.KratosAdminURL),
		slog.Bool("audit_log", p.cfg.DatabaseURL != ""),
	)

	return nil
}

// Start creates the internal HTTP server, binds it to a random localhost
// port, and begins accepting connections. It returns promptly; the server
// runs in a background goroutine managed by the AdminServer implementation.
//
// Start must be called after Init. Calling Start on a disabled plugin is a
// no-op.
func (p *Plugin) Start(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	srv := ExportedServerFactory(p.handlers, p.logger)
	if err := srv.Start(); err != nil {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("user-management: admin server failed to start: %s", err)
		return fmt.Errorf("starting user-management admin server: %w", err)
	}

	p.server = srv
	p.internalAddr = srv.Addr()

	p.logger.Info("user-management admin server started",
		slog.String("internal_addr", p.internalAddr),
	)

	return nil
}

// Stop gracefully shuts down the internal admin HTTP server and closes any
// database connections opened during Init. Calling Stop on a disabled or
// not-yet-started plugin is a no-op.
func (p *Plugin) Stop(ctx context.Context) error {
	if p.dbCleanup != nil {
		p.dbCleanup()
	}
	if !p.cfg.Enabled || p.server == nil {
		return nil
	}
	if err := p.server.Stop(ctx); err != nil {
		return fmt.Errorf("stopping user-management admin server: %w", err)
	}
	return nil
}

// Health returns the current health status of the user-management plugin.
// Returns healthy=true with a "user-management disabled" message when the
// plugin is disabled. When enabled, reflects the state set during Init/Start.
// Health is safe to call concurrently and does not block.
func (p *Plugin) Health() ports.HealthStatus {
	return ports.HealthStatus{
		Healthy: p.healthy,
		Message: p.healthMsg,
	}
}

// HealthCheck performs a live connectivity probe against the Kratos admin API
// and updates the internal health state. Unlike Health, this method makes a
// real HTTP request and is safe to call from a background goroutine.
func (p *Plugin) HealthCheck(ctx context.Context) ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{Healthy: true, Message: "user-management disabled"}
	}

	probeURL := p.cfg.KratosAdminURL + kratosAdminHealthPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("user-management: cannot build health probe: %s", err)
		return ports.HealthStatus{Healthy: false, Message: p.healthMsg}
	}

	client := &http.Client{Timeout: healthCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("user-management: kratos admin unreachable: %s", err)
		return ports.HealthStatus{Healthy: false, Message: p.healthMsg}
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode >= 500 {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("user-management: kratos admin health probe returned %d", resp.StatusCode)
		return ports.HealthStatus{Healthy: false, Message: p.healthMsg}
	}

	p.healthy = true
	p.healthMsg = fmt.Sprintf("user-management configured, kratos admin: %s", p.cfg.KratosAdminURL)
	return ports.HealthStatus{Healthy: true, Message: p.healthMsg}
}

// DependencyName returns "postgres" as the dependency identifier for health
// endpoint reporting.
// Implements ports.DependencyChecker.
func (p *Plugin) DependencyName() string { return "postgres" }

// CheckDependency performs a live connectivity probe against PostgreSQL and
// returns a DependencyStatus with latency. When no database is configured,
// it returns a healthy status immediately.
// Implements ports.DependencyChecker.
func (p *Plugin) CheckDependency(ctx context.Context) ports.DependencyStatus {
	if !p.cfg.Enabled || p.dbProber == nil {
		return ports.DependencyStatus{Status: "healthy", LatencyMS: 0}
	}

	start := time.Now()
	err := p.dbProber.Ping(ctx)
	latencyMS := time.Since(start).Milliseconds()

	if err != nil {
		return ports.DependencyStatus{
			Status:    "unhealthy",
			LatencyMS: latencyMS,
			Error:     fmt.Sprintf("postgres unreachable: %s", err),
		}
	}

	return ports.DependencyStatus{
		Status:    "healthy",
		LatencyMS: latencyMS,
	}
}

// InternalAddr returns the host:port the internal admin HTTP server is
// listening on. It must only be called after a successful Start.
// Implements ports.InternalServerPlugin.
func (p *Plugin) InternalAddr() string {
	return p.internalAddr
}

// ContributeCaddyRoutes returns the Caddy route that reverse-proxies all
// /_vibewarden/admin/* requests to the internal admin HTTP server.
//
// The route has Priority 60 and is placed before the catch-all reverse proxy
// route so that admin requests are never forwarded to the upstream application.
//
// Returns nil when the plugin is disabled.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute {
	if !p.cfg.Enabled {
		return nil
	}

	return []ports.CaddyRoute{
		{
			MatchPath: adminPath,
			Priority:  60,
			Handler: map[string]any{
				"match": []map[string]any{
					{"path": []string{adminPath}},
				},
				"handle": []map[string]any{
					{
						"handler": "reverse_proxy",
						"upstreams": []map[string]any{
							{"dial": p.internalAddr},
						},
					},
				},
			},
		},
		{
			MatchPath: configPath,
			Priority:  61,
			Handler: map[string]any{
				"match": []map[string]any{
					{"path": []string{configPath}},
				},
				"handle": []map[string]any{
					{
						"handler": "reverse_proxy",
						"upstreams": []map[string]any{
							{"dial": p.internalAddr},
						},
					},
				},
			},
		},
	}
}

// ContributeCaddyHandlers returns the Caddy handler that validates the
// X-Admin-Key bearer token for all /_vibewarden/admin/* requests.
//
// The handler has Priority 60 and is placed in the catch-all handler chain.
// Requests to other paths pass through unchanged.
//
// Returns nil when the plugin is disabled.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled {
		return nil
	}

	return []ports.CaddyHandler{
		{
			Handler: map[string]any{
				"handler":     "admin_auth",
				"admin_token": p.cfg.AdminToken,
				"admin_path":  adminPathPrefix,
				"config_path": configPathPrefix,
			},
			Priority: 60,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal helpers — pure functions, no side effects.
// ---------------------------------------------------------------------------

// validateConfig checks that the user-management configuration is
// self-consistent and returns an error describing the first violation found.
func validateConfig(cfg Config) error {
	if cfg.AdminToken == "" {
		return fmt.Errorf("admin_token is required when user-management is enabled")
	}
	if cfg.KratosAdminURL == "" {
		return fmt.Errorf("kratos_admin_url is required when user-management is enabled")
	}
	if _, err := url.ParseRequestURI(cfg.KratosAdminURL); err != nil {
		return fmt.Errorf("kratos_admin_url %q is not a valid URL: %w", cfg.KratosAdminURL, err)
	}
	return nil
}
