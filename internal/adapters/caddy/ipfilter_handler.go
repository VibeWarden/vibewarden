// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	auditadapter "github.com/vibewarden/vibewarden/internal/adapters/audit"
	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func init() {
	gocaddy.RegisterModule(IPFilterHandler{})
}

// IPFilterHandlerConfig is the JSON-serialisable configuration for the
// IPFilterHandler Caddy module. It is embedded in the Caddy JSON config
// under the "config" key of the "vibewarden_ip_filter" handler entry.
type IPFilterHandlerConfig struct {
	// Mode is the filter mode: "allowlist" or "blocklist".
	Mode string `json:"mode"`

	// Addresses is the list of IP addresses or CIDR ranges to match against.
	Addresses []string `json:"addresses"`

	// TrustProxyHeaders enables reading X-Forwarded-For for the real client IP.
	TrustProxyHeaders bool `json:"trust_proxy_headers"`
}

// IPFilterHandler is a Caddy HTTP middleware module that enforces IP-based
// access control. It supports two modes:
//
//   - allowlist: only requests from addresses in Addresses are permitted.
//   - blocklist: requests from addresses in Addresses are blocked.
//
// Blocked requests receive 403 Forbidden. A structured ip_filter.blocked event
// is emitted for every blocked request. An audit.ip_filter.blocked audit event
// is also emitted for every blocked request, regardless of log level.
//
// The module is registered under the name "vibewarden_ip_filter" and referenced
// from the Caddy JSON configuration as:
//
//	{"handler": "vibewarden_ip_filter", ...}
type IPFilterHandler struct {
	// Config holds the handler configuration populated by Caddy's JSON unmarshaller.
	Config IPFilterHandlerConfig `json:"config"`

	// nets and ips hold the parsed address entries. Built during Provision.
	nets []*net.IPNet
	ips  []net.IP

	// logger is used to emit error messages when event logging fails.
	logger *slog.Logger

	// eventLogger emits structured operational events for blocked requests.
	eventLogger ports.EventLogger

	// auditLogger emits security audit events for blocked requests.
	auditLogger ports.AuditEventLogger
}

// CaddyModule returns the module metadata used to register it with Caddy.
func (IPFilterHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.vibewarden_ip_filter",
		New: func() gocaddy.Module { return new(IPFilterHandler) },
	}
}

// Provision implements gocaddy.Provisioner. It parses all configured address
// strings into net.IP and *net.IPNet values for efficient per-request matching.
func (h *IPFilterHandler) Provision(_ gocaddy.Context) error {
	h.logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	h.eventLogger = logadapter.NewSlogEventLogger(os.Stdout)
	h.auditLogger = auditadapter.NewJSONWriter(os.Stdout)

	h.nets = h.nets[:0]
	h.ips = h.ips[:0]

	for _, addr := range h.Config.Addresses {
		if _, ipNet, err := net.ParseCIDR(addr); err == nil {
			h.nets = append(h.nets, ipNet)
			continue
		}
		if ip := net.ParseIP(addr); ip != nil {
			h.ips = append(h.ips, ip)
			continue
		}
		return fmt.Errorf("ip_filter: %q is not a valid IP address or CIDR", addr)
	}

	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
// It extracts the client IP, evaluates the filter rule, and either blocks the
// request with 403 or delegates to the next handler.
func (h *IPFilterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	clientIP := middleware.ExtractClientIP(r, h.Config.TrustProxyHeaders)

	ip := net.ParseIP(clientIP)
	matched := h.matchesAny(ip)

	blocked := h.isBlocked(matched)
	if blocked {
		h.emitBlockedEvent(r.Context(), clientIP, r.Method, r.URL.Path)
		h.emitAuditBlockedEvent(r.Context(), clientIP, r.Method, r.URL.Path)
		middleware.WriteErrorResponse(w, r, http.StatusForbidden, "forbidden", "your IP address is not permitted to access this resource")
		return nil
	}

	return next.ServeHTTP(w, r)
}

// matchesAny returns true when ip matches any configured address or CIDR.
// A nil ip never matches.
func (h *IPFilterHandler) matchesAny(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, known := range h.ips {
		if known.Equal(ip) {
			return true
		}
	}
	for _, cidr := range h.nets {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// isBlocked evaluates whether a request should be blocked given the match result.
// In allowlist mode a non-matching IP is blocked.
// In blocklist mode a matching IP is blocked.
func (h *IPFilterHandler) isBlocked(matched bool) bool {
	switch h.Config.Mode {
	case "allowlist":
		return !matched
	default: // "blocklist" and any unrecognised value
		return matched
	}
}

// emitBlockedEvent emits a structured ip_filter.blocked event. Errors are
// logged but do not affect the HTTP response.
func (h *IPFilterHandler) emitBlockedEvent(ctx context.Context, clientIP, method, path string) {
	if h.eventLogger == nil {
		return
	}
	ev := events.NewIPFilterBlocked(events.IPFilterBlockedParams{
		ClientIP: clientIP,
		Mode:     h.Config.Mode,
		Method:   method,
		Path:     path,
	})
	if err := h.eventLogger.Log(ctx, ev); err != nil {
		h.logger.Error("ip-filter: failed to emit blocked event", slog.String("error", err.Error()))
	}
}

// emitAuditBlockedEvent emits an audit.ip_filter.blocked audit event.
// Errors are logged but do not affect the HTTP response.
func (h *IPFilterHandler) emitAuditBlockedEvent(ctx context.Context, clientIP, method, path string) {
	if h.auditLogger == nil {
		return
	}
	auditEv, err := audit.NewAuditEvent(
		audit.EventTypeIPFilterBlocked,
		audit.Actor{IP: clientIP},
		audit.Target{Path: path},
		audit.OutcomeFailure,
		middleware.CorrelationID(ctx),
		map[string]any{
			"method": method,
			"mode":   h.Config.Mode,
		},
	)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("ip-filter: failed to build audit event", slog.String("error", err.Error()))
		}
		return
	}
	if err := h.auditLogger.Log(ctx, auditEv); err != nil {
		if h.logger != nil {
			h.logger.Error("ip-filter: failed to emit audit blocked event", slog.String("error", err.Error()))
		}
	}
}

// Interface guards — ensure IPFilterHandler satisfies the required Caddy
// interfaces at compile time.
var (
	_ gocaddy.Provisioner         = (*IPFilterHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*IPFilterHandler)(nil)
)
