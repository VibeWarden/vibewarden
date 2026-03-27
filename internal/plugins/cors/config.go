// Package cors implements the VibeWarden CORS plugin.
//
// It sets Cross-Origin Resource Sharing response headers on every response,
// and responds to OPTIONS preflight requests with a 204 No Content status.
// A wildcard "*" in AllowedOrigins enables all origins (development use only).
package cors

// Config holds all settings for the CORS plugin.
// It maps to the plugins.cors section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the CORS plugin.
	Enabled bool

	// AllowedOrigins is the list of origins that are allowed to make
	// cross-origin requests. Use ["*"] to allow all origins (development only).
	// Example: ["https://example.com", "https://app.example.com"]
	AllowedOrigins []string

	// AllowedMethods is the list of HTTP methods that are allowed in
	// cross-origin requests.
	// Default: ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
	AllowedMethods []string

	// AllowedHeaders is the list of HTTP request headers that browsers may
	// send in cross-origin requests.
	// Default: ["Content-Type", "Authorization"]
	AllowedHeaders []string

	// ExposedHeaders is the list of response headers that are safe to expose
	// to the browser. These headers appear in Access-Control-Expose-Headers.
	// Default: []
	ExposedHeaders []string

	// AllowCredentials, when true, sets Access-Control-Allow-Credentials: true.
	// Must not be combined with AllowedOrigins: ["*"].
	// Default: false
	AllowCredentials bool

	// MaxAge is the number of seconds browsers may cache the preflight response.
	// Sets Access-Control-Max-Age. Zero means the header is omitted.
	// Default: 0
	MaxAge int
}
