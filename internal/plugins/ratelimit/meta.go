package ratelimit

// Description returns a short description of the rate-limiting plugin.
func (p *Plugin) Description() string {
	return "Per-IP and per-user token-bucket rate limiting on every proxied request"
}

// ConfigSchema returns the configuration field descriptions for the rate-limiting plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":                      "Enable rate limiting (default: true)",
		"store":                        "Backing store for limiter state: memory (default)",
		"per_ip.requests_per_second":   "Sustained per-IP request rate (default: 10)",
		"per_ip.burst":                 "Per-IP burst size above the sustained rate (default: 20)",
		"per_user.requests_per_second": "Sustained per-user request rate (default: 100)",
		"per_user.burst":               "Per-user burst size above the sustained rate (default: 200)",
		"trust_proxy_headers":          "Read X-Forwarded-For to determine real client IP (default: false)",
		"exempt_paths":                 "URL path glob patterns that bypass rate limiting",
	}
}

// Example returns an example YAML configuration for the rate-limiting plugin.
func (p *Plugin) Example() string {
	return `  rate-limiting:
    enabled: true
    per_ip:
      requests_per_second: 10
      burst: 20`
}
