package caddy

import (
	"log/slog"
	"net/http"
	"os"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func init() {
	gocaddy.RegisterModule(MaintenanceHandler{})
}

// MaintenanceHandlerConfig is the JSON-serialisable configuration for the
// MaintenanceHandler Caddy module. It is embedded in the Caddy JSON config
// under the "config" key of the "vibewarden_maintenance" handler entry.
type MaintenanceHandlerConfig struct {
	// Message is the human-readable message returned in the 503 response body.
	Message string `json:"message"`
}

// MaintenanceHandler is a Caddy HTTP middleware module that enforces maintenance
// mode. All requests to paths that do not start with /_vibewarden/ receive a
// 503 Service Unavailable response with a JSON body containing the configured
// message.
//
// A maintenance.request_blocked structured event is emitted for every blocked
// request.
//
// The module is registered under the name "vibewarden_maintenance" and
// referenced from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_maintenance", "config": {"message": "..."}}
type MaintenanceHandler struct {
	// Config holds the handler configuration populated by Caddy's JSON unmarshaller.
	Config MaintenanceHandlerConfig `json:"config"`

	// logger is used to emit error messages when event logging fails.
	logger *slog.Logger

	// eventLogger emits structured operational events for blocked requests.
	eventLogger ports.EventLogger

	// inner is the pre-built middleware handler. Built during Provision so it
	// is not reconstructed on every request.
	inner func(next http.Handler) http.Handler
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (MaintenanceHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_maintenance",
		New: func() gocaddy.Module { return new(MaintenanceHandler) },
	}
}

// Provision implements gocaddy.Provisioner. It builds the middleware handler
// that will be called on every request.
func (h *MaintenanceHandler) Provision(_ gocaddy.Context) error {
	h.logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	h.eventLogger = logadapter.NewSlogEventLogger(os.Stdout)

	h.inner = middleware.MaintenanceMiddleware(
		middleware.MaintenanceConfig{
			Enabled: true,
			Message: h.Config.Message,
		},
		h.eventLogger,
	)
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
// It delegates to the pre-built MaintenanceMiddleware which handles the
// /_vibewarden/* exemption and 503 response.
func (h *MaintenanceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	h.inner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = next.ServeHTTP(w, r)
	})).ServeHTTP(w, r)
	return nil
}

// Interface guards — ensure MaintenanceHandler satisfies the required Caddy
// interfaces at compile time.
var (
	_ gocaddy.Provisioner         = (*MaintenanceHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*MaintenanceHandler)(nil)
)
