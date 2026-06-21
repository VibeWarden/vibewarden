package caddy

import (
	"encoding/json"
	"log/slog"
	"net/http"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
	"github.com/vibewarden/vibewarden/internal/domain/healthsummary"
	"github.com/vibewarden/vibewarden/internal/domain/upstream"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func init() { gocaddy.RegisterModule(HealthHandler{}) }

// HealthHandlerConfig is the JSON configuration for the vibewarden_health
// module. SiteName is populated by the multisite config builder to scope the
// "site" field in the response body; it is empty for single-site deployments.
type HealthHandlerConfig struct {
	// SiteName, when non-empty, is rendered as the "site" field in the JSON
	// response body. Used by the multisite Caddy config builder.
	SiteName string `json:"site_name,omitempty"`
}

// HealthIdentityHeader is the response header always emitted by the
// /_vibewarden/health endpoint regardless of health.expose_version. It is the
// stable ownership marker used by port_owner.go to detect a VibeWarden sidecar
// without relying on the version string being present in the body. The value is
// always "1". See internal/adapters/ops/port_owner.go.
const HealthIdentityHeader = "X-Vibewarden"

// healthIdentityHeaderValue is the fixed value of HealthIdentityHeader.
const healthIdentityHeaderValue = "1"

// HealthHandler is a Caddy HTTP handler module registered as
// "http.handlers.vibewarden_health". It replaces the previous static_response
// on the /_vibewarden/health route and renders the cached probe result from
// the background HTTPChecker via RuntimeServices.
//
// The handler always responds with HTTP 200. The outer "status" field follows
// the worst-component-wins rule: "ok" when all components are healthy,
// "degraded" otherwise. HTTP 503 is reserved for /_vibewarden/ready and for
// sidecar-itself failures where this handler cannot run at all.
type HealthHandler struct {
	// Config is the JSON-decoded module configuration.
	Config HealthHandlerConfig `json:"config,omitempty"`

	logger          *slog.Logger
	checker         ports.UpstreamHealthChecker
	version         string
	siteName        string
	suppressVersion bool
}

// CaddyModule returns the module metadata. The module ID must match the
// "handler" field value used in the Caddy JSON config.
func (HealthHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_health",
		New: func() gocaddy.Module { return new(HealthHandler) },
	}
}

// Provision reads RuntimeServices from the composition-root registry and
// delegates to ProvisionWith. This is the production code path; tests use
// ProvisionWith directly.
func (h *HealthHandler) Provision(ctx gocaddy.Context) error {
	return h.ProvisionWith(ctx, currentServices())
}

// ProvisionWith initialises the handler with explicit services. Tests call
// this directly; production calls it via Provision.
//
// When s.UpstreamHealthChecker is nil the handler renders
// "upstream":"unknown" and degrades the outer status. This is the expected
// state during the 5–10s boot gap before the first probe completes, and when
// the probe is disabled by operator config.
func (h *HealthHandler) ProvisionWith(_ gocaddy.Context, s RuntimeServices) error {
	h.logger = s.Logger
	if h.logger == nil {
		h.logger = slog.Default()
	}
	h.checker = s.UpstreamHealthChecker
	h.version = s.SidecarVersion
	h.siteName = h.Config.SiteName
	h.suppressVersion = s.SuppressVersion
	return nil
}

// ServeHTTP renders the cached upstream probe state as a JSON health response.
// It is lock-free on the request path: CurrentStatus() and Snapshot() are
// both atomic reads inside HTTPChecker.
//
// The X-Vibewarden header is always emitted regardless of health.expose_version.
// It is the stable ownership marker that port_owner.go uses to detect a
// VibeWarden sidecar without relying on the version string in the body.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	upstreamState := h.resolveUpstreamState()

	components := map[string]healthsummary.ComponentState{
		"sidecar":  alwaysOkState{},
		"upstream": upstreamState,
	}
	outer := healthsummary.AggregateStatus(components)

	// Omit the version when suppression is enabled (health.expose_version: false).
	// The omitempty tag on the Version field ensures no "version":"" appears.
	version := h.version
	if h.suppressVersion {
		version = ""
	}

	body := struct {
		Status     string            `json:"status"`
		Version    string            `json:"version,omitempty"`
		Site       string            `json:"site,omitempty"`
		Components map[string]string `json:"components"`
	}{
		Status:  string(outer),
		Version: version,
		Site:    h.siteName,
		Components: map[string]string{
			"sidecar":  "ok",
			"upstream": upstreamState.String(),
		},
	}

	// X-Vibewarden is the stable ownership marker. It is always emitted so
	// that port_owner.go can detect a VibeWarden sidecar even when the version
	// is suppressed (health.expose_version: false). See ADR-084.
	w.Header().Set(HealthIdentityHeader, healthIdentityHeaderValue)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // always 200; outer status is informational
	return json.NewEncoder(w).Encode(body)
}

// resolveUpstreamState maps the probe's current domain status to an
// upstream.State for the response. Returns Unknown when no checker is wired.
func (h *HealthHandler) resolveUpstreamState() upstream.State {
	if h.checker == nil {
		return upstream.NewUnknown()
	}
	return mapUpstreamStatus(h.checker.CurrentStatus(), h.checker.Snapshot().LastError)
}

// mapUpstreamStatus translates the probe's domain-internal UpstreamStatus to
// the component-facing upstream.State used by the health response.
func mapUpstreamStatus(s domainheal.UpstreamStatus, lastError string) upstream.State {
	switch s {
	case domainheal.StatusHealthy:
		return upstream.NewOk()
	case domainheal.StatusUnhealthy:
		return upstream.NewFailing(lastError)
	default:
		return upstream.NewUnknown()
	}
}

// alwaysOkState is a ComponentState implementation that always reports healthy.
// The sidecar is by definition healthy when this handler runs — if the sidecar
// were down, this handler could not serve the response.
type alwaysOkState struct{}

func (alwaysOkState) Healthy() bool  { return true }
func (alwaysOkState) String() string { return "ok" }

// Compile-time assertion that HealthHandler satisfies the caddyhttp handler interface.
var _ caddyhttp.MiddlewareHandler = (*HealthHandler)(nil)
