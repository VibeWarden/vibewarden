// Package waf implements the VibeWarden Web Application Firewall plugin.
//
// It bundles request-level protection features that guard upstream applications
// against common attack patterns:
//   - Content-Type validation: body-bearing requests (POST, PUT, PATCH) must
//     include a Content-Type header whose media type is on the configured allow-list.
//   - Rule-based WAF engine: scans URL parameters, selected headers, and the first
//     8 KB of request bodies against built-in attack patterns (SQLi, XSS, path
//     traversal, command injection). Runs in "block" mode (403 Forbidden) or
//     "detect" mode (log-only, pass through).
package waf

// Mode controls how the WAF rule engine responds to detections.
type Mode string

const (
	// ModeBlock causes the WAF to reject matching requests with 403 Forbidden.
	ModeBlock Mode = "block"

	// ModeDetect causes the WAF to log detections and pass matching requests
	// through to the upstream application unchanged.
	ModeDetect Mode = "detect"
)

// RulesConfig toggles individual built-in rule categories.
// All categories are enabled by default when the WAF engine is active.
type RulesConfig struct {
	// SQLInjection toggles SQLi detection rules (default: true).
	SQLInjection bool

	// XSS toggles cross-site scripting detection rules (default: true).
	XSS bool

	// PathTraversal toggles path traversal detection rules (default: true).
	PathTraversal bool

	// CommandInjection toggles command injection detection rules (default: true).
	CommandInjection bool
}

// WAFEngineConfig holds settings for the built-in WAF rule engine.
//
//nolint:revive // WAFEngineConfig distinguishes the engine config from the WAF plugin's top-level Config type
type WAFEngineConfig struct {
	// Enabled toggles the WAF rule engine. Default: false.
	Enabled bool

	// Mode controls the response to a detection: "block" (default) or "detect".
	Mode Mode

	// Rules toggles individual rule categories.
	Rules RulesConfig

	// ExemptPaths is a list of URL path glob patterns (path.Match syntax) that
	// bypass WAF scanning. The /_vibewarden/* prefix is always exempt.
	ExemptPaths []string
}

// Config holds all settings for the WAF plugin.
// It maps to the waf: section of vibewarden.yaml.
type Config struct {
	// ContentTypeValidation configures the Content-Type validation feature.
	ContentTypeValidation ContentTypeValidationConfig

	// Engine configures the built-in WAF rule engine.
	Engine WAFEngineConfig
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
