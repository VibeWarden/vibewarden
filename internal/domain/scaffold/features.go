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
}
