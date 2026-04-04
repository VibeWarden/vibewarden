package ports

import (
	"context"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
)

// RedactedConfig is a JSON-serializable representation of the running
// configuration with sensitive fields masked. It is safe to return to
// external callers such as the admin API.
type RedactedConfig = config.RedactedConfig

// ReloadResult represents the outcome of a config reload operation returned
// by the admin API.
type ReloadResult struct {
	// Success is true when the reload completed successfully.
	Success bool `json:"success"`

	// Message is a human-readable status message.
	Message string `json:"message"`

	// Errors contains validation error details when Success is false.
	// Omitted from the JSON response when empty.
	Errors []string `json:"errors,omitempty"`
}

// ConfigReloader orchestrates configuration hot reload across all components.
// It validates the new configuration, rebuilds internal state, and applies
// changes to the proxy server without dropping active connections.
type ConfigReloader interface {
	// Reload reads configuration from disk, validates it, and applies changes.
	// Returns an error if validation fails or if the reload cannot be applied.
	// On error, the previous configuration remains active.
	//
	// source identifies the reload trigger ("file_watcher" or "admin_api") and
	// is included in structured log events.
	Reload(ctx context.Context, source string) error

	// CurrentConfig returns the currently active configuration with sensitive
	// fields redacted, safe for external exposure via the admin API.
	CurrentConfig() RedactedConfig
}

// ConfigWatcherOption configures the file watcher behaviour.
type ConfigWatcherOption func(*ConfigWatcherOptions)

// ConfigWatcherOptions holds watcher configuration. It is exported so that
// adapter constructors can accept and apply options from callers.
type ConfigWatcherOptions struct {
	Debounce time.Duration
}

// WithDebounce sets the debounce duration for file change events.
// Default: 500ms.
func WithDebounce(d time.Duration) ConfigWatcherOption {
	return func(o *ConfigWatcherOptions) { o.Debounce = d }
}
