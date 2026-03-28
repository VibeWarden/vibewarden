package events

import (
	"fmt"
	"time"
)

// APIKeySuccessParams contains the parameters needed to construct an
// auth.api_key.success event.
type APIKeySuccessParams struct {
	// Method is the HTTP method of the authenticated request (e.g. "GET").
	Method string

	// Path is the URL path of the authenticated request.
	Path string

	// KeyName is the human-readable name of the API key that was accepted.
	KeyName string

	// Scopes is the list of scopes granted by the key.
	Scopes []string
}

// NewAPIKeySuccess creates an auth.api_key.success event indicating that a
// request was authenticated via a valid API key.
func NewAPIKeySuccess(params APIKeySuccessParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAPIKeySuccess,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"API key authenticated: %s %s (key %q)",
			params.Method, params.Path, params.KeyName,
		),
		Payload: map[string]any{
			"method":   params.Method,
			"path":     params.Path,
			"key_name": params.KeyName,
			"scopes":   params.Scopes,
		},
	}
}

// APIKeyFailedParams contains the parameters needed to construct an
// auth.api_key.failed event.
type APIKeyFailedParams struct {
	// Method is the HTTP method of the rejected request (e.g. "GET").
	Method string

	// Path is the URL path of the rejected request.
	Path string

	// Reason is a short description of why the request was rejected (e.g.
	// "missing api key", "invalid or inactive api key").
	Reason string
}

// NewAPIKeyFailed creates an auth.api_key.failed event indicating that a
// request was rejected due to a missing or invalid API key.
func NewAPIKeyFailed(params APIKeyFailedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAPIKeyFailed,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"API key authentication rejected: %s",
			params.Reason,
		),
		Payload: map[string]any{
			"method": params.Method,
			"path":   params.Path,
			"reason": params.Reason,
		},
	}
}
