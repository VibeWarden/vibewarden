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

// APIKeyForbiddenParams contains the parameters needed to construct an
// auth.api_key.forbidden event.
type APIKeyForbiddenParams struct {
	// Method is the HTTP method of the forbidden request (e.g. "POST").
	Method string

	// Path is the URL path of the forbidden request.
	Path string

	// KeyName is the human-readable name of the API key that was presented.
	KeyName string

	// KeyScopes is the list of scopes held by the key.
	KeyScopes []string

	// RequiredScopes is the list of scopes required by the matching scope rule.
	RequiredScopes []string
}

// NewAPIKeyForbidden creates an auth.api_key.forbidden event indicating that a
// valid API key was presented but lacked the required scopes for the requested
// path and HTTP method.
func NewAPIKeyForbidden(params APIKeyForbiddenParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAPIKeyForbidden,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"API key %q forbidden: %s %s — key scopes %v do not satisfy required %v",
			params.KeyName, params.Method, params.Path, params.KeyScopes, params.RequiredScopes,
		),
		Payload: map[string]any{
			"method":          params.Method,
			"path":            params.Path,
			"key_name":        params.KeyName,
			"key_scopes":      params.KeyScopes,
			"required_scopes": params.RequiredScopes,
		},
	}
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
