package events

import (
	"fmt"
	"time"
)

// IPFilterBlockedParams contains the parameters needed to construct an
// ip_filter.blocked event.
type IPFilterBlockedParams struct {
	// ClientIP is the IP address that was blocked.
	ClientIP string

	// Mode is the filter mode in effect: "allowlist" or "blocklist".
	Mode string

	// Method is the HTTP method of the blocked request (e.g. "GET").
	Method string

	// Path is the URL path of the blocked request.
	Path string
}

// NewIPFilterBlocked creates an ip_filter.blocked event indicating that a
// request was rejected because the client IP did not pass the IP filter check.
func NewIPFilterBlocked(params IPFilterBlockedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeIPFilterBlocked,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Request from %s blocked by IP filter (%s mode): %s %s",
			params.ClientIP, params.Mode, params.Method, params.Path,
		),
		Payload: map[string]any{
			"client_ip": params.ClientIP,
			"mode":      params.Mode,
			"method":    params.Method,
			"path":      params.Path,
		},
	}
}
