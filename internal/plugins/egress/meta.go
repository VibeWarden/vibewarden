package egress

// Description returns a short description of the egress plugin.
func (p *Plugin) Description() string {
	return "Egress proxy: allowlist outbound API calls with secret injection, rate limiting, and circuit breaking"
}

// ConfigSchema returns the configuration field descriptions for the egress plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":                              "Enable the egress proxy plugin (default: false)",
		"listen":                               "TCP address the egress proxy binds to (default: \"127.0.0.1:8081\")",
		"default_policy":                       "Disposition for unmatched routes: \"allow\" or \"deny\" (default: \"deny\")",
		"allow_insecure":                       "Permit plain HTTP egress requests globally (default: false)",
		"default_timeout":                      "Global request timeout as a Go duration string (default: \"30s\")",
		"default_body_size_limit":              "Global max request body in bytes; 0 means no limit",
		"default_response_size_limit":          "Global max response body in bytes; 0 means no limit",
		"dns.block_private":                    "Block requests to private/loopback IPs (SSRF protection, default: true)",
		"dns.allowed_private":                  "CIDR ranges exempt from block_private (e.g. [\"10.0.0.0/8\"])",
		"routes[].name":                        "Unique identifier for the route (required)",
		"routes[].pattern":                     "URL glob pattern, must start with http:// or https:// (required)",
		"routes[].methods":                     "HTTP methods this route applies to; empty means all methods",
		"routes[].timeout":                     "Per-route request timeout as a Go duration string",
		"routes[].secret":                      "OpenBao secret name to fetch and inject",
		"routes[].secret_header":               "HTTP request header to inject the secret value into",
		"routes[].secret_format":               "Value template; \"{value}\" is replaced with the secret value",
		"routes[].rate_limit":                  "Rate limit expression (e.g. \"100/s\", \"1000/m\")",
		"routes[].circuit_breaker.threshold":   "Consecutive failures required to open the circuit",
		"routes[].circuit_breaker.reset_after": "How long the circuit stays open before a probe (Go duration)",
		"routes[].retries.max":                 "Maximum number of retry attempts",
		"routes[].retries.methods":             "HTTP methods eligible for retry; empty means idempotent only",
		"routes[].retries.backoff":             "Backoff strategy: \"exponential\" (default) or \"fixed\"",
		"routes[].retries.initial_backoff":     "Base wait before first retry (Go duration, default: \"100ms\")",
		"routes[].body_size_limit":             "Max request body in bytes; 0 means use global default",
		"routes[].response_size_limit":         "Max response body in bytes; 0 means use global default",
		"routes[].allow_insecure":              "Permit plain HTTP for this route only (default: false)",
	}
}

// Example returns an example YAML configuration for the egress plugin.
func (p *Plugin) Example() string {
	return `  egress:
    enabled: true
    listen: "127.0.0.1:8081"
    default_policy: deny
    default_timeout: "30s"
    dns:
      block_private: true
    routes:
      - name: stripe-api
        pattern: "https://api.stripe.com/**"
        methods: ["POST"]
        timeout: "10s"
        secret: app/stripe
        secret_header: Authorization
        secret_format: "Bearer {value}"
        rate_limit: "100/s"
        circuit_breaker:
          threshold: 5
          reset_after: "30s"
        retries:
          max: 3
          backoff: exponential
      - name: github-api
        pattern: "https://api.github.com/**"
        methods: ["GET", "POST"]
        timeout: "15s"`
}
