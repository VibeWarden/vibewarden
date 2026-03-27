package ipfilter

import "github.com/vibewarden/vibewarden/internal/ports"

// Description returns a short description of the ip-filter plugin.
func (p *Plugin) Description() string {
	return "IP allowlist/blocklist filter: reject or permit requests by client IP or CIDR range"
}

// ConfigSchema returns the configuration field descriptions for the ip-filter plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":             "Enable IP filtering (default: false)",
		"mode":                "Filter mode: \"allowlist\" (only listed IPs allowed) or \"blocklist\" (listed IPs blocked, default: \"blocklist\")",
		"addresses":           "List of IP addresses or CIDR ranges to match (e.g. \"10.0.0.0/8\", \"192.168.1.100\")",
		"trust_proxy_headers": "Read X-Forwarded-For for real client IP when behind a trusted proxy (default: false)",
	}
}

// Example returns an example YAML configuration for the ip-filter plugin.
func (p *Plugin) Example() string {
	return `  ip_filter:
    enabled: true
    mode: blocklist
    addresses:
      - "10.0.0.0/8"
      - "192.168.1.100"`
}

// Interface guard — ip-filter Plugin implements PluginMeta.
var _ ports.PluginMeta = (*Plugin)(nil)
