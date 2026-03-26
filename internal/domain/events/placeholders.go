package events

import (
	"fmt"
	"time"
)

// RequestBlockedParams contains the parameters needed to construct a
// request.blocked event.
type RequestBlockedParams struct {
	// Method is the HTTP method of the blocked request (e.g. "GET").
	Method string

	// Path is the URL path of the blocked request.
	Path string

	// Reason is a short description of why the request was blocked.
	Reason string

	// BlockedBy identifies the middleware or policy that blocked the request
	// (e.g. "security_headers", "ip_blocklist").
	BlockedBy string

	// ClientIP is the client IP address. May be empty if it could not be
	// determined.
	ClientIP string
}

// NewRequestBlocked creates a request.blocked event indicating that a request
// was blocked by a middleware layer for a reason other than auth or rate
// limiting (e.g. a security policy violation).
func NewRequestBlocked(params RequestBlockedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeRequestBlocked,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Request blocked by %s: %s %s — %s",
			params.BlockedBy, params.Method, params.Path, params.Reason,
		),
		Payload: map[string]any{
			"method":     params.Method,
			"path":       params.Path,
			"reason":     params.Reason,
			"blocked_by": params.BlockedBy,
			"client_ip":  params.ClientIP,
		},
	}
}

// TLSCertificateIssuedParams contains the parameters needed to construct a
// tls.certificate_issued event.
type TLSCertificateIssuedParams struct {
	// Domain is the domain name for which the certificate was issued.
	Domain string

	// Provider is the certificate authority or provider (e.g. "letsencrypt",
	// "self-signed").
	Provider string

	// ExpiresAt is the certificate expiry time in RFC3339 format.
	ExpiresAt string
}

// NewTLSCertificateIssued creates a tls.certificate_issued event indicating
// that a new TLS certificate was successfully obtained or renewed.
func NewTLSCertificateIssued(params TLSCertificateIssuedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeTLSCertificateIssued,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"TLS certificate issued for %s via %s",
			params.Domain, params.Provider,
		),
		Payload: map[string]any{
			"domain":     params.Domain,
			"provider":   params.Provider,
			"expires_at": params.ExpiresAt,
		},
	}
}

// UserCreatedParams contains the parameters needed to construct a
// user.created event.
type UserCreatedParams struct {
	// IdentityID is the identity provider identifier for the new user.
	IdentityID string

	// Email is the email address of the new user.
	Email string

	// ActorID identifies the admin who performed the action.
	// May be empty when the action was performed by the system.
	ActorID string
}

// NewUserCreated creates a user.created event indicating that a new user
// identity was created in the identity provider.
func NewUserCreated(params UserCreatedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeUserCreated,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"New user created: %s (identity %s)",
			params.Email, params.IdentityID,
		),
		Payload: map[string]any{
			"identity_id": params.IdentityID,
			"email":       params.Email,
			"actor_id":    params.ActorID,
		},
	}
}

// UserDeletedParams contains the parameters needed to construct a
// user.deleted event.
type UserDeletedParams struct {
	// IdentityID is the identity provider identifier of the deleted user.
	IdentityID string

	// Email is the email address of the deleted user.
	Email string

	// ActorID identifies the admin who performed the action.
	// May be empty when the action was performed by the system.
	ActorID string

	// Reason is an optional human-readable explanation for the deletion.
	Reason string
}

// NewUserDeleted creates a user.deleted event indicating that a user identity
// was deleted from the identity provider.
func NewUserDeleted(params UserDeletedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeUserDeleted,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"User deleted: %s (identity %s)",
			params.Email, params.IdentityID,
		),
		Payload: map[string]any{
			"identity_id": params.IdentityID,
			"email":       params.Email,
			"actor_id":    params.ActorID,
			"reason":      params.Reason,
		},
	}
}

// UserDeactivatedParams contains the parameters needed to construct a
// user.deactivated event.
type UserDeactivatedParams struct {
	// IdentityID is the identity provider identifier of the deactivated user.
	IdentityID string

	// Email is the email address of the deactivated user.
	Email string

	// ActorID identifies the admin who performed the action.
	// May be empty when the action was performed by the system.
	ActorID string

	// Reason is an optional human-readable explanation for the deactivation.
	Reason string
}

// NewUserDeactivated creates a user.deactivated event indicating that a user
// identity was deactivated, preventing further authentication while retaining
// the identity record.
func NewUserDeactivated(params UserDeactivatedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeUserDeactivated,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"User deactivated: %s (identity %s)",
			params.Email, params.IdentityID,
		),
		Payload: map[string]any{
			"identity_id": params.IdentityID,
			"email":       params.Email,
			"actor_id":    params.ActorID,
			"reason":      params.Reason,
		},
	}
}
