package events

import (
	"fmt"
	"time"
)

// AuthSuccessParams contains the parameters needed to construct an
// auth.success event.
type AuthSuccessParams struct {
	// Method is the HTTP method of the authenticated request (e.g. "GET").
	Method string

	// Path is the URL path of the authenticated request.
	Path string

	// SessionID is the Kratos session identifier.
	SessionID string

	// IdentityID is the Kratos identity (user) identifier.
	IdentityID string

	// Email is the email address associated with the authenticated identity.
	Email string
}

// NewAuthSuccess creates an auth.success event indicating that a request
// carried a valid session and was allowed to proceed to the upstream application.
func NewAuthSuccess(params AuthSuccessParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAuthSuccess,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Authenticated request allowed: %s %s (identity %s)",
			params.Method, params.Path, params.IdentityID,
		),
		Payload: map[string]any{
			"method":      params.Method,
			"path":        params.Path,
			"session_id":  params.SessionID,
			"identity_id": params.IdentityID,
			"email":       params.Email,
		},
	}
}

// AuthFailedParams contains the parameters needed to construct an
// auth.failed event.
type AuthFailedParams struct {
	// Method is the HTTP method of the rejected request (e.g. "GET").
	Method string

	// Path is the URL path of the rejected request.
	Path string

	// Reason is a short description of why the request was rejected (e.g.
	// "missing session cookie", "invalid or missing session").
	Reason string

	// Detail is an optional additional detail string (e.g. an error message).
	// May be empty.
	Detail string
}

// NewAuthFailed creates an auth.failed event indicating that a request was
// rejected due to a missing, invalid, or expired session.
func NewAuthFailed(params AuthFailedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAuthFailed,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Unauthenticated request rejected: %s",
			params.Reason,
		),
		Payload: map[string]any{
			"method": params.Method,
			"path":   params.Path,
			"reason": params.Reason,
			"detail": params.Detail,
		},
	}
}
