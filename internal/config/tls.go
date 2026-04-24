package config

import "fmt"

// TLSCertMonitoringConfig holds configuration for the certificate expiry monitor.
type TLSCertMonitoringConfig struct {
	// Enabled toggles the certificate expiry monitor (default: true when TLS is enabled).
	Enabled bool `mapstructure:"enabled"`
	// CheckInterval is how often the monitor checks certificate expiry (default: 6h).
	CheckInterval string `mapstructure:"check_interval"`
	// WarningThreshold is the time-before-expiry that triggers a warning event (default: 720h = 30 days).
	WarningThreshold string `mapstructure:"warning_threshold"`
	// CriticalThreshold is the time-before-expiry that triggers a critical event and degraded health (default: 168h = 7 days).
	CriticalThreshold string `mapstructure:"critical_threshold"`
}

// TLSConfig holds TLS-related settings.
type TLSConfig struct {
	// Enabled toggles TLS (default: false for local dev)
	Enabled bool `mapstructure:"enabled"`
	// Domain for TLS certificate (required if enabled with provider "letsencrypt")
	Domain string `mapstructure:"domain"`
	// Provider: "letsencrypt" (or alias "acme"), "self-signed", or "external"
	Provider string `mapstructure:"provider"`
	// CertPath is the path to a PEM-encoded certificate file.
	// Required when Provider is "external".
	CertPath string `mapstructure:"cert_path"`
	// KeyPath is the path to a PEM-encoded private key file.
	// Required when Provider is "external".
	KeyPath string `mapstructure:"key_path"`
	// StoragePath is the directory where Caddy stores ACME certificates.
	// Only applies when Provider is "letsencrypt".
	StoragePath string `mapstructure:"storage_path"`
	// Email is the ACME account email address used for certificate expiry
	// notifications and automatic EAB registration with CAs that require it
	// (e.g. ZeroSSL). Optional for Let's Encrypt.
	Email string `mapstructure:"email"`
	// ACMECA is the ACME directory URL to use instead of Let's Encrypt production.
	// Only applies when Provider is "letsencrypt".
	// Example: "https://acme-staging-v02.api.letsencrypt.org/directory"
	ACMECA string `mapstructure:"acme_ca"`
	// CertMonitoring holds configuration for the background certificate expiry monitor.
	CertMonitoring TLSCertMonitoringConfig `mapstructure:"cert_monitoring"`
	// SkipRateLimitCheck disables the Let's Encrypt rate-limit preflight check
	// run by "vibew doctor" when provider is "letsencrypt". Set to true when
	// you are intentionally re-issuing within a 168-hour window (e.g. after
	// certificate revocation) or when crt.sh is unavailable and you want to
	// suppress the WARN. The --skip-le-preflight flag on "vibew doctor" is the
	// single-invocation equivalent. Both flags are frozen by ADR-090.
	SkipRateLimitCheck bool `mapstructure:"skip_rate_limit_check"`
}

// validateTLS validates TLS configuration and returns a slice of error strings.
func validateTLS(c *Config) []string {
	var errs []string

	// tls.provider validation: must be one of the accepted values.
	// "acme" is accepted as an alias for "letsencrypt"; Load() normalises it
	// before Validate() runs, but direct callers of Validate() may still pass
	// it, so we permit it here as well.
	switch c.TLS.Provider {
	case "", "self-signed", "letsencrypt", "acme", "external",
		"zerossl", "buypass", "letsencrypt-staging":
		// valid — empty string is accepted (defaults to "self-signed" via Load)
	default:
		errs = append(errs, fmt.Sprintf(
			"tls.provider %q is invalid; accepted values: \"self-signed\", \"letsencrypt\" (or alias \"acme\"), "+
				"\"zerossl\", \"buypass\", \"letsencrypt-staging\", \"external\" — "+
				"set tls.provider to one of those values",
			c.TLS.Provider,
		))
	}

	// ACME providers require a domain for certificate issuance.
	// Also checked for "acme" — the alias — in case Validate() is called
	// before Load() has had a chance to normalise the value.
	acmeProviders := map[string]bool{
		"letsencrypt": true, "acme": true,
		"zerossl": true, "buypass": true, "letsencrypt-staging": true,
	}
	if c.TLS.Enabled && acmeProviders[c.TLS.Provider] && c.TLS.Domain == "" {
		errs = append(errs, fmt.Sprintf(
			"tls.domain is required when tls.provider is %q — "+
				"set tls.domain to a domain you control and have pointed at this server "+
				"(Let's Encrypt rejects reserved names like example.com)",
			c.TLS.Provider,
		))
	}

	// ZeroSSL requires email for automatic EAB registration.
	if c.TLS.Enabled && c.TLS.Provider == "zerossl" && c.TLS.Email == "" {
		errs = append(errs, "tls.email is required when tls.provider is \"zerossl\" — "+
			"ZeroSSL needs an email for automatic EAB registration")
	}

	// TLS external provider requires cert_path and key_path.
	if c.TLS.Enabled && c.TLS.Provider == "external" {
		if c.TLS.CertPath == "" {
			errs = append(errs, "tls.cert_path is required when tls.provider is \"external\" — "+
				"set tls.cert_path to the path of your PEM-encoded certificate file")
		}
		if c.TLS.KeyPath == "" {
			errs = append(errs, "tls.key_path is required when tls.provider is \"external\" — "+
				"set tls.key_path to the path of your PEM-encoded private key file")
		}
	}

	return errs
}
