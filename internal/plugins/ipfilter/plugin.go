package ipfilter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Plugin is the IP filter plugin for VibeWarden.
// It implements ports.Plugin and ports.CaddyContributor.
//
// When enabled, it injects a vibewarden_ip_filter Caddy handler at priority 15
// (before security headers at 20) so that disallowed clients are rejected
// before any further middleware — including authentication — runs.
type Plugin struct {
	cfg    Config
	nets   []*net.IPNet
	ips    []net.IP
	logger *slog.Logger
}

// New creates a new IP filter Plugin.
func New(cfg Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		cfg:    cfg,
		logger: logger,
	}
}

// Name returns the canonical plugin identifier "ip-filter".
// This must match the key used under plugins: in vibewarden.yaml.
func (p *Plugin) Name() string { return "ip-filter" }

// Priority returns 15 — ip-filter runs before all other middleware so that
// blocked clients are rejected as early as possible.
func (p *Plugin) Priority() int { return 15 }

// Init validates the configuration and parses IP/CIDR address entries.
// Returns an error if any address cannot be parsed, or if Mode is invalid.
func (p *Plugin) Init(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	mode := p.cfg.Mode
	if mode == "" {
		mode = FilterModeBlocklist
	}
	if mode != FilterModeAllowlist && mode != FilterModeBlocklist {
		return fmt.Errorf("ip_filter.mode %q is invalid; accepted values: %q, %q",
			mode, FilterModeAllowlist, FilterModeBlocklist)
	}

	// Parse all configured addresses, distinguishing CIDRs from plain IPs.
	p.nets = p.nets[:0]
	p.ips = p.ips[:0]

	for _, addr := range p.cfg.Addresses {
		if _, ipNet, err := net.ParseCIDR(addr); err == nil {
			p.nets = append(p.nets, ipNet)
			continue
		}
		if ip := net.ParseIP(addr); ip != nil {
			p.ips = append(p.ips, ip)
			continue
		}
		return fmt.Errorf("ip_filter.addresses: %q is not a valid IP address or CIDR", addr)
	}

	p.logger.Info("ip-filter plugin initialised",
		slog.String("mode", string(mode)),
		slog.Int("address_count", len(p.cfg.Addresses)),
	)
	return nil
}

// Start is a no-op for the ip-filter plugin. No background work is needed.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op for the ip-filter plugin. No resources need releasing.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Health returns the current health status of the ip-filter plugin.
func (p *Plugin) Health() ports.HealthStatus {
	if !p.cfg.Enabled {
		return ports.HealthStatus{
			Healthy: true,
			Message: "ip-filter disabled",
		}
	}

	mode := p.cfg.Mode
	if mode == "" {
		mode = FilterModeBlocklist
	}

	return ports.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf(
			"ip-filter active (%s mode, %d addresses)",
			mode, len(p.cfg.Addresses),
		),
	}
}

// ContributeCaddyRoutes returns nil.
// The ip-filter plugin does not add named routes.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns the vibewarden_ip_filter Caddy handler
// fragment at priority 15. Returns an empty slice when the plugin is disabled.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled {
		return nil
	}

	handler, err := buildIPFilterHandlerJSON(p.cfg)
	if err != nil {
		// JSON marshalling of a known struct cannot fail in practice.
		p.logger.Error("ip-filter plugin: building handler JSON", slog.String("err", err.Error()))
		return nil
	}

	return []ports.CaddyHandler{
		{
			Handler:  handler,
			Priority: 15,
		},
	}
}

// ---------------------------------------------------------------------------
// Internal builder — pure function, no side effects.
// ---------------------------------------------------------------------------

// ipFilterHandlerConfig is the JSON-serialisable config sent to the
// vibewarden_ip_filter Caddy module.
type ipFilterHandlerConfig struct {
	Mode              string   `json:"mode"`
	Addresses         []string `json:"addresses"`
	TrustProxyHeaders bool     `json:"trust_proxy_headers"`
}

// buildIPFilterHandlerJSON serialises cfg into the Caddy handler JSON map
// expected by the vibewarden_ip_filter module.
func buildIPFilterHandlerJSON(cfg Config) (map[string]any, error) {
	mode := cfg.Mode
	if mode == "" {
		mode = FilterModeBlocklist
	}

	hcfg := ipFilterHandlerConfig{
		Mode:              string(mode),
		Addresses:         cfg.Addresses,
		TrustProxyHeaders: cfg.TrustProxyHeaders,
	}

	cfgBytes, err := json.Marshal(hcfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling ip filter handler config: %w", err)
	}

	return map[string]any{
		"handler": "vibewarden_ip_filter",
		"config":  json.RawMessage(cfgBytes),
	}, nil
}

// ---------------------------------------------------------------------------
// Interface guards.
// ---------------------------------------------------------------------------

var (
	_ ports.Plugin           = (*Plugin)(nil)
	_ ports.CaddyContributor = (*Plugin)(nil)
)
