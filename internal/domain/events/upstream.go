package events

import (
	"fmt"
	"time"
)

// UpstreamRetryParams contains the parameters needed to construct an
// upstream.retry event.
type UpstreamRetryParams struct {
	// Method is the HTTP method of the retried request (e.g. "GET").
	Method string

	// Path is the URL path of the retried request.
	Path string

	// Attempt is the retry attempt number (1 = first retry, 2 = second retry, …).
	Attempt int

	// StatusCode is the upstream HTTP status code that triggered the retry.
	StatusCode int

	// ClientIP is the remote client IP address.
	ClientIP string
}

// NewUpstreamRetry creates an upstream.retry event indicating that the retry
// middleware is re-sending a failed upstream request.
func NewUpstreamRetry(params UpstreamRetryParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeUpstreamRetry,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Upstream retry attempt %d for %s %s (previous status %d) from %s",
			params.Attempt, params.Method, params.Path, params.StatusCode, params.ClientIP,
		),
		Payload: map[string]any{
			"method":      params.Method,
			"path":        params.Path,
			"attempt":     params.Attempt,
			"status_code": params.StatusCode,
			"client_ip":   params.ClientIP,
		},
	}
}

// UpstreamTimeoutParams contains the parameters needed to construct an
// upstream.timeout event.
type UpstreamTimeoutParams struct {
	// Method is the HTTP method of the timed-out request (e.g. "GET").
	Method string

	// Path is the URL path of the timed-out request.
	Path string

	// TimeoutSeconds is the configured upstream timeout in seconds.
	TimeoutSeconds float64

	// ClientIP is the remote client IP address.
	ClientIP string
}

// NewUpstreamTimeout creates an upstream.timeout event indicating that the
// upstream application did not respond within the configured timeout.
func NewUpstreamTimeout(params UpstreamTimeoutParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeUpstreamTimeout,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Upstream timed out after %.0fs for %s %s from %s",
			params.TimeoutSeconds, params.Method, params.Path, params.ClientIP,
		),
		Payload: map[string]any{
			"method":          params.Method,
			"path":            params.Path,
			"timeout_seconds": params.TimeoutSeconds,
			"client_ip":       params.ClientIP,
		},
	}
}
