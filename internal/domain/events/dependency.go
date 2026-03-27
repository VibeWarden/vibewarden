package events

import (
	"fmt"
	"time"
)

// AuthProviderUnavailableParams contains the parameters needed to construct an
// auth.provider_unavailable event.
type AuthProviderUnavailableParams struct {
	// ProviderURL is the URL of the unavailable auth provider.
	ProviderURL string

	// Error is a short description of the connectivity failure.
	Error string

	// AffectedPath is the URL path of the request that triggered discovery
	// of the unavailability. May be empty when emitted from a health probe.
	AffectedPath string
}

// NewAuthProviderUnavailable creates an auth.provider_unavailable event
// indicating that the auth provider (Ory Kratos) cannot be reached.
// This event is emitted at most once per transition from healthy to unhealthy
// to avoid log flooding.
func NewAuthProviderUnavailable(params AuthProviderUnavailableParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAuthProviderUnavailable,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Auth provider unavailable at %s: %s",
			params.ProviderURL, params.Error,
		),
		Payload: map[string]any{
			"provider_url":  params.ProviderURL,
			"error":         params.Error,
			"affected_path": params.AffectedPath,
		},
	}
}

// AuthProviderRecoveredParams contains the parameters needed to construct an
// auth.provider_recovered event.
type AuthProviderRecoveredParams struct {
	// ProviderURL is the URL of the recovered auth provider.
	ProviderURL string
}

// NewAuthProviderRecovered creates an auth.provider_recovered event
// indicating that the auth provider (Ory Kratos) is reachable again after
// a period of unavailability.
func NewAuthProviderRecovered(params AuthProviderRecoveredParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAuthProviderRecovered,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Auth provider recovered at %s",
			params.ProviderURL,
		),
		Payload: map[string]any{
			"provider_url": params.ProviderURL,
		},
	}
}

// AuditLogFailureParams contains the parameters needed to construct an
// audit.log_failure event.
type AuditLogFailureParams struct {
	// Action is the audit action that failed to be persisted
	// (e.g. "user.created").
	Action string

	// UserID is the user affected by the action that was not audited.
	UserID string

	// Error is a short description of the persistence failure.
	Error string
}

// NewAuditLogFailure creates an audit.log_failure event indicating that an
// audit entry could not be persisted. The originating operation is not
// rolled back — this event is informational only.
func NewAuditLogFailure(params AuditLogFailureParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAuditLogFailure,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Audit log write failed for action %q on user %s: %s",
			params.Action, params.UserID, params.Error,
		),
		Payload: map[string]any{
			"action":  params.Action,
			"user_id": params.UserID,
			"error":   params.Error,
		},
	}
}
