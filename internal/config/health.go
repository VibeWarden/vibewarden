package config

// HealthConfig holds configuration for the /_vibewarden/health endpoint
// served by the VibeWarden sidecar.
type HealthConfig struct {
	// ExposeVersion controls whether the running sidecar version string is
	// included in the /_vibewarden/health JSON response.
	//
	// Default: true (backward-compatible — existing deployments are unaffected).
	//
	// Set to false to suppress the version field (OWASP A05 hardening). The
	// sidecar is still identifiable as VibeWarden via the stable
	// X-Vibewarden response header, which is always present regardless of
	// this setting and is used by `vibew doctor` for port-ownership detection.
	ExposeVersion bool `mapstructure:"expose_version"`
}
