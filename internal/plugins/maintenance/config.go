// Package maintenance implements the VibeWarden maintenance mode plugin.
//
// When enabled, all inbound requests are rejected with 503 Service Unavailable
// and a configurable message. Requests to /_vibewarden/* paths (health, ready,
// metrics) are always allowed through so that infrastructure health checks and
// observability endpoints remain reachable during maintenance windows.
//
// A maintenance.request_blocked structured event is emitted for every blocked
// request, allowing operators to monitor traffic volume during maintenance.
package maintenance

// Config holds all settings for the maintenance mode plugin.
// It maps to the maintenance section of vibewarden.yaml.
type Config struct {
	// Enabled toggles maintenance mode (default: false).
	// When true, all non-internal requests receive a 503 response.
	Enabled bool

	// Message is the human-readable message returned to clients in the 503 body.
	// Defaults to "Service is under maintenance" when empty.
	Message string
}
