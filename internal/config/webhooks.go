package config

import "fmt"

// WebhooksConfig holds all webhook delivery settings.
// It maps to the webhooks section of vibewarden.yaml.
type WebhooksConfig struct {
	// Endpoints is the list of webhook endpoints to deliver events to.
	Endpoints []WebhookEndpointConfig `mapstructure:"endpoints"`

	// SignatureVerification configures inbound webhook signature verification.
	SignatureVerification WebhookSignatureVerificationConfig `mapstructure:"signature_verification"`
}

// WebhookSignatureVerificationConfig holds all settings for inbound webhook
// signature verification. It maps to the webhooks.signature_verification
// section of vibewarden.yaml.
type WebhookSignatureVerificationConfig struct {
	// Enabled toggles inbound webhook signature verification (default: false).
	Enabled bool `mapstructure:"enabled"`

	// Paths is the ordered list of per-path signature verification rules.
	Paths []WebhookSignaturePathConfig `mapstructure:"paths"`
}

// WebhookSignaturePathConfig holds the per-path webhook signature rule.
// It maps to a single entry under webhooks.signature_verification.paths.
type WebhookSignaturePathConfig struct {
	// Path is the URL path this rule applies to (exact match, required).
	Path string `mapstructure:"path"`

	// Provider selects the signature format: "stripe", "github", "slack",
	// "twilio", or "generic". Required.
	Provider string `mapstructure:"provider"`

	// SecretEnvVar is the name of the environment variable containing the
	// shared HMAC secret. Required — secrets must not be stored in the config
	// file directly. Use ${VAR_NAME} or just the variable name.
	SecretEnvVar string `mapstructure:"secret_env_var"`

	// Header is the custom HTTP header name used when Provider is "generic".
	// Ignored for all other providers.
	Header string `mapstructure:"header,omitempty"`
}

// WebhookEndpointConfig holds the settings for a single webhook endpoint.
type WebhookEndpointConfig struct {
	// URL is the HTTP(S) endpoint to POST events to. Required.
	URL string `mapstructure:"url"`

	// Events is the list of event types to deliver to this endpoint.
	// Use "*" to subscribe to all events.
	Events []string `mapstructure:"events"`

	// Format selects the payload format: "raw" (default), "slack", or "discord".
	Format string `mapstructure:"format"`

	// TimeoutSeconds is the per-request HTTP timeout in seconds (default: 10).
	TimeoutSeconds int `mapstructure:"timeout_seconds"`
}

// validateWebhooks validates webhook configuration and returns a slice of error strings.
func validateWebhooks(c *Config) []string {
	var errs []string

	// webhooks.endpoints validation.
	validFormats := map[string]bool{"": true, "raw": true, "slack": true, "discord": true}
	for i, ep := range c.Webhooks.Endpoints {
		prefix := fmt.Sprintf("webhooks.endpoints[%d]", i)
		if ep.URL == "" {
			errs = append(errs, fmt.Sprintf("%s.url is required", prefix))
		}
		if len(ep.Events) == 0 {
			errs = append(errs, fmt.Sprintf("%s.events must have at least one entry", prefix))
		}
		if !validFormats[ep.Format] {
			errs = append(errs, fmt.Sprintf("%s.format %q is invalid; accepted values: \"raw\", \"slack\", \"discord\"", prefix, ep.Format))
		}
		if ep.TimeoutSeconds < 0 {
			errs = append(errs, fmt.Sprintf("%s.timeout_seconds must be >= 0", prefix))
		}
	}

	// webhooks.signature_verification.paths validation.
	validProviders := map[string]bool{
		"stripe": true, "github": true, "slack": true, "twilio": true, "generic": true,
	}
	if c.Webhooks.SignatureVerification.Enabled {
		for i, p := range c.Webhooks.SignatureVerification.Paths {
			prefix := fmt.Sprintf("webhooks.signature_verification.paths[%d]", i)
			if p.Path == "" {
				errs = append(errs, fmt.Sprintf("%s.path is required", prefix))
			}
			if !validProviders[p.Provider] {
				errs = append(errs, fmt.Sprintf(
					"%s.provider %q is invalid; accepted values: stripe, github, slack, twilio, generic",
					prefix, p.Provider,
				))
			}
			if p.SecretEnvVar == "" {
				errs = append(errs, fmt.Sprintf("%s.secret_env_var is required", prefix))
			}
			if p.Provider == "generic" && p.Header == "" {
				errs = append(errs, fmt.Sprintf(
					"%s.header is required when provider is \"generic\"", prefix,
				))
			}
		}
	}

	return errs
}
