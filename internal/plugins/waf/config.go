// Package waf implements the VibeWarden Web Application Firewall plugin.
//
// It bundles request-level protection features that guard upstream applications
// against common attack patterns. In v1 the only feature is Content-Type
// validation: body-bearing requests (POST, PUT, PATCH) must include a
// Content-Type header whose media type is on the configured allow-list.
package waf

// Config holds all settings for the WAF plugin.
// It maps to the waf: section of vibewarden.yaml.
type Config struct {
	// ContentTypeValidation configures the Content-Type validation feature.
	ContentTypeValidation ContentTypeValidationConfig
}

// ContentTypeValidationConfig holds settings for the Content-Type validation
// feature of the WAF plugin.
type ContentTypeValidationConfig struct {
	// Enabled toggles Content-Type validation on body-bearing requests
	// (POST, PUT, PATCH). Default: false.
	Enabled bool

	// Allowed is the list of permitted media types (e.g. "application/json").
	// Parameters such as "; charset=utf-8" are stripped before comparison, so
	// "application/json; charset=utf-8" matches "application/json".
	// Default: ["application/json", "application/x-www-form-urlencoded", "multipart/form-data"]
	Allowed []string
}
