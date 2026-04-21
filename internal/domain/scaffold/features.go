// Package scaffold contains value objects for the VibeWarden project
// scaffolding subsystem. This file defines feature-toggle domain types.
// This package has zero external dependencies.
package scaffold

import "errors"

// Feature identifies a VibeWarden feature that can be enabled via
// `vibewarden add <feature>`.
type Feature string

const (
	// FeatureAuth enables Ory Kratos authentication.
	FeatureAuth Feature = "auth"

	// FeatureRateLimit enables per-IP/per-user rate limiting.
	FeatureRateLimit Feature = "rate-limiting"

	// FeatureTLS enables TLS termination.
	FeatureTLS Feature = "tls"

	// FeatureAdmin enables the admin API.
	FeatureAdmin Feature = "admin"

	// FeatureMetrics enables Prometheus metrics.
	FeatureMetrics Feature = "metrics"

	// FeatureWAF enables the Web Application Firewall.
	FeatureWAF Feature = "waf"
)

// ErrFeatureAlreadyEnabled is returned by FeatureToggler.EnableFeature when
// the requested feature is already enabled in vibewarden.yaml.
var ErrFeatureAlreadyEnabled = errors.New("feature already enabled")

// ErrConfigNotFound is returned when vibewarden.yaml does not exist in the
// target directory.
var ErrConfigNotFound = errors.New("vibewarden.yaml not found")

// FeatureState holds the current enable/disable state of all known features
// as read from vibewarden.yaml. It is a value object — equality by value.
type FeatureState struct {
	// UpstreamPort is the configured upstream application port.
	UpstreamPort int

	// AuthEnabled is true when the auth/kratos section is present and enabled.
	AuthEnabled bool

	// RateLimitEnabled is true when the rate_limit section is enabled.
	RateLimitEnabled bool

	// TLSEnabled is true when the tls section is enabled.
	TLSEnabled bool

	// AdminEnabled is true when the admin section is enabled.
	AdminEnabled bool

	// MetricsEnabled is true when the metrics section is enabled.
	MetricsEnabled bool

	// WAFEnabled is true when the waf section is enabled.
	WAFEnabled bool
}

// FieldChange describes a single key added, changed, or removed by a YAML
// edit. Before is empty for additions; After is empty for removals. Path is
// the dotted key path (e.g. "tls.domain").
type FieldChange struct {
	// Path is the dotted key path within the edited YAML document.
	Path string

	// Before is the rendered value prior to the edit, or "" for additions.
	Before string

	// After is the rendered value after the edit, or "" for removals.
	After string
}

// Diff captures the set of fields added, changed, or removed by a single
// YAML edit. It is used to render a summary to the user after a
// `vibewarden add <feature>` call.
type Diff struct {
	// File is the absolute path of the edited file.
	File string

	// Added lists fields that did not exist before the edit.
	Added []FieldChange

	// Changed lists fields whose scalar value was replaced.
	Changed []FieldChange

	// Removed lists fields that were removed by the edit.
	Removed []FieldChange
}

// IsEmpty reports whether the diff captures no changes.
func (d Diff) IsEmpty() bool {
	return len(d.Added) == 0 && len(d.Changed) == 0 && len(d.Removed) == 0
}

// FeatureOptions carries feature-specific options supplied by the user when
// running `vibewarden add <feature>`. Fields that do not apply to a
// particular feature are ignored.
type FeatureOptions struct {
	// TLSDomain is the domain for TLS certificate provisioning.
	// Required when enabling FeatureTLS.
	TLSDomain string

	// TLSProvider is the TLS provider: "letsencrypt", "self-signed", or "external".
	// Defaults to "letsencrypt" when TLSDomain is set.
	TLSProvider string

	// WAFMode is the WAF operating mode: "detect" or "block".
	// Defaults to "detect" when enabling FeatureWAF.
	WAFMode string
}
