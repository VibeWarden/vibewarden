// Package webhooksig implements the VibeWarden inbound webhook signature
// verification plugin.
//
// When enabled, the plugin registers a Caddy middleware handler that verifies
// HMAC signatures on inbound webhook requests. Supported providers are Stripe,
// GitHub, Slack, Twilio, and a configurable generic HMAC-SHA256 format.
//
// Secrets are loaded from environment variables at startup and never embedded
// in the configuration file.
package webhooksig

// RuleConfig holds the per-path webhook signature rule configuration.
// It maps to a single entry under webhooks.signature_verification.paths in
// vibewarden.yaml.
type RuleConfig struct {
	// Path is the URL path this rule applies to (exact match).
	Path string

	// Provider selects the signature format: "stripe", "github", "slack",
	// "twilio", or "generic".
	Provider string

	// SecretEnvVar is the name of the environment variable containing the
	// shared HMAC secret. The value is read at plugin Init time so that the
	// YAML config file never contains secrets.
	SecretEnvVar string

	// Header is the custom HTTP header name used when Provider is "generic".
	// Ignored for all other providers.
	Header string
}

// Config holds all settings for the webhook signature verification plugin.
// It maps to the webhooks.signature_verification section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the webhook signature verification plugin.
	Enabled bool

	// Paths is the ordered list of per-path signature verification rules.
	Paths []RuleConfig
}
