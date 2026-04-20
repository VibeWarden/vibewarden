package tls

// Description returns a short description of the TLS plugin.
func (p *Plugin) Description() string {
	return "TLS termination with ACME (Let's Encrypt, ZeroSSL, Buypass), self-signed, or external certificates"
}

// ConfigSchema returns the configuration field descriptions for the TLS plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"enabled":      "Enable TLS (default: false)",
		"provider":     "Certificate provider: letsencrypt (with LE→ZeroSSL→Buypass fallback), zerossl, buypass, letsencrypt-staging, self-signed, external",
		"domain":       "Domain for certificate (required for ACME providers)",
		"email":        "ACME account email (required for ZeroSSL, recommended for Let's Encrypt)",
		"cert_path":    "Path to certificate file (external provider)",
		"key_path":     "Path to key file (external provider)",
		"storage_path": "Directory for certificate storage",
	}
}

// Critical returns true because TLS termination is a fundamental security
// boundary. If TLS fails to initialise when enabled the sidecar must not
// serve plain-text traffic in its place.
func (p *Plugin) Critical() bool { return true }

// Example returns an example YAML configuration for the TLS plugin.
func (p *Plugin) Example() string {
	return `  tls:
    enabled: true
    provider: letsencrypt
    domain: app.example.com`
}
