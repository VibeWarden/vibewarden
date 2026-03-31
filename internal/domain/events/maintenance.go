package events

import (
	"fmt"
	"time"
)

// MaintenanceRequestBlockedParams contains the parameters needed to construct a
// maintenance.request_blocked event.
type MaintenanceRequestBlockedParams struct {
	// Path is the URL path of the blocked request.
	Path string

	// Method is the HTTP method of the blocked request.
	Method string

	// Message is the operator-configured maintenance message returned to the client.
	Message string
}

// NewMaintenanceRequestBlocked creates a maintenance.request_blocked event
// indicating that a request was rejected because maintenance mode is enabled.
func NewMaintenanceRequestBlocked(params MaintenanceRequestBlockedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeMaintenanceRequestBlocked,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Request blocked by maintenance mode: %s %s",
			params.Method, params.Path,
		),
		Payload: map[string]any{
			"path":    params.Path,
			"method":  params.Method,
			"message": params.Message,
		},
	}
}
