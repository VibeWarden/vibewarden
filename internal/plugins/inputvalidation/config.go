// Package inputvalidation implements the VibeWarden input validation plugin.
//
// The plugin enforces request input size limits at the sidecar boundary before
// any other processing occurs (it runs at priority 18, before the WAF at 25).
// Oversized inputs are rejected with 400 Bad Request before regex scanning
// starts, reducing CPU load and protecting against resource-exhaustion attacks.
//
// Configurable limits (all with sensible defaults):
//   - max_url_length (default 2048)
//   - max_query_string_length (default 2048)
//   - max_header_count (default 100)
//   - max_header_size (default 8192 bytes per header value)
//
// Each limit can be overridden per URL path via the path_overrides list.
package inputvalidation

// PathOverrideConfig defines per-path limit overrides.
// Only non-zero fields override the global values.
type PathOverrideConfig struct {
	// Path is a glob pattern (path.Match syntax) matched against the request
	// URL path (e.g. "/api/upload", "/static/*").
	Path string

	// MaxURLLength overrides the global limit for matching paths.
	// Zero means inherit the global value.
	MaxURLLength int

	// MaxQueryStringLength overrides the global limit for matching paths.
	// Zero means inherit the global value.
	MaxQueryStringLength int

	// MaxHeaderCount overrides the global limit for matching paths.
	// Zero means inherit the global value.
	MaxHeaderCount int

	// MaxHeaderSize overrides the global limit for matching paths.
	// Zero means inherit the global value.
	MaxHeaderSize int
}

// Config holds all settings for the input validation plugin.
// It maps to the input_validation: section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the plugin. Default: false.
	Enabled bool

	// MaxURLLength is the maximum allowed length of the raw request URI
	// (path + query string). Default: 2048. Zero disables this check.
	MaxURLLength int

	// MaxQueryStringLength is the maximum allowed length of the query string,
	// not including the leading "?". Default: 2048. Zero disables this check.
	MaxQueryStringLength int

	// MaxHeaderCount is the maximum number of request headers allowed.
	// Default: 100. Zero disables this check.
	MaxHeaderCount int

	// MaxHeaderSize is the maximum allowed byte length of any single header
	// value. Default: 8192. Zero disables this check.
	MaxHeaderSize int

	// PathOverrides defines per-path limit overrides.
	// The first entry whose Path pattern matches the request URL path wins.
	PathOverrides []PathOverrideConfig
}
