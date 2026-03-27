// Package bodysize implements the VibeWarden request body size limiting plugin.
//
// It enforces a global maximum request body size and supports per-path overrides.
// Requests that exceed the configured limit receive a 413 Payload Too Large response
// before the body is forwarded to the upstream application.
//
// Body size limiting is implemented via Caddy's native request_body handler,
// which wraps net/http.MaxBytesReader. The VibeWarden plugin contributes a custom
// Caddy module (vibewarden_body_size) that handles per-path dispatch internally.
package bodysize

// Config holds all settings for the body size limiting plugin.
// It maps to the body_size section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the body size limiting plugin.
	Enabled bool

	// MaxBytes is the global default maximum request body size in bytes.
	// A value of 0 disables the global limit.
	MaxBytes int64

	// Overrides defines per-path body size limits that take precedence over MaxBytes.
	Overrides []OverrideConfig
}

// OverrideConfig defines a per-path body size limit.
type OverrideConfig struct {
	// Path is the URL path prefix to match (e.g. "/api/upload").
	Path string

	// MaxBytes is the maximum request body size for this path in bytes.
	// A value of 0 means no limit for this path.
	MaxBytes int64
}
