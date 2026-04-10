package events

import (
	"fmt"
	"time"
)

// EventTypeUpstreamHealthChanged is emitted when the upstream application's
// health status transitions between Unknown, Healthy, and Unhealthy states.
const EventTypeUpstreamHealthChanged = "upstream.health_changed"

// UpstreamHealthChangedParams contains the parameters needed to construct an
// upstream.health_changed event.
type UpstreamHealthChangedParams struct {
	// PreviousStatus is the health status before the transition (e.g. "unknown", "healthy").
	PreviousStatus string

	// NewStatus is the health status after the transition (e.g. "healthy", "unhealthy").
	NewStatus string

	// ConsecutiveCount is the number of consecutive successes or failures that
	// triggered this transition.
	ConsecutiveCount int

	// UpstreamURL is the URL that was probed (host + path, no credentials).
	UpstreamURL string

	// LastError is the error message from the most recent probe when NewStatus is
	// "unhealthy". Empty when the transition is to "healthy".
	LastError string
}

// NewUpstreamHealthChanged creates an upstream.health_changed event indicating
// that the upstream application's health status has changed.
func NewUpstreamHealthChanged(params UpstreamHealthChangedParams) Event {
	payload := map[string]any{
		"previous_status": params.PreviousStatus,
		"new_status":      params.NewStatus,
		"consecutive":     params.ConsecutiveCount,
		"upstream_url":    params.UpstreamURL,
	}
	if params.LastError != "" {
		payload["last_error"] = params.LastError
	}

	summary := fmt.Sprintf(
		"Upstream health changed from %s to %s after %d consecutive probes (url: %s)",
		params.PreviousStatus, params.NewStatus, params.ConsecutiveCount, params.UpstreamURL,
	)

	sev := SeverityInfo
	if params.NewStatus == "unhealthy" {
		sev = SeverityHigh
	}

	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeUpstreamHealthChanged,
		Timestamp:     time.Now().UTC(),
		Severity:      sev,
		Category:      CategoryNetwork,
		AISummary:     summary,
		Payload:       payload,
	}
}
