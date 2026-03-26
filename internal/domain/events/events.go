// Package events defines domain events for the VibeWarden structured log schema.
// This package has zero external dependencies — only Go stdlib (time, fmt).
//
// Every security-relevant action in VibeWarden emits one of these typed events.
// Consumers write events via the ports.EventLogger interface; the domain layer
// never performs I/O directly.
package events

import "time"

// SchemaVersion is the current version of the event schema.
// Changing this is a breaking change and must be documented in the schema
// changelog before being deployed.
const SchemaVersion = "v1"

// Event type constants identify the kind of event being emitted.
// These values are stable and form part of the public schema contract.
const (
	// EventTypeProxyStarted is emitted once when the reverse proxy starts
	// successfully and is ready to accept connections.
	EventTypeProxyStarted = "proxy.started"

	// EventTypeProxyKratosFlow is emitted for every request routed to the
	// Ory Kratos self-service flow API.
	EventTypeProxyKratosFlow = "proxy.kratos_flow"

	// EventTypeAuthSuccess is emitted when a request carries a valid session
	// and is allowed to proceed to the upstream application.
	EventTypeAuthSuccess = "auth.success"

	// EventTypeAuthFailed is emitted when a request is rejected due to a
	// missing, invalid, or expired session.
	EventTypeAuthFailed = "auth.failed"

	// EventTypeRateLimitHit is emitted when a request is rejected because the
	// caller has exceeded their per-IP or per-user rate limit.
	EventTypeRateLimitHit = "rate_limit.hit"

	// EventTypeRateLimitUnidentified is emitted when a request is rejected
	// because the client IP address could not be determined.
	EventTypeRateLimitUnidentified = "rate_limit.unidentified_client"

	// EventTypeRequestBlocked is emitted when a request is blocked by a
	// middleware layer for a reason other than auth or rate limiting (e.g. a
	// security policy violation).
	EventTypeRequestBlocked = "request.blocked"

	// EventTypeTLSCertificateIssued is emitted when a new TLS certificate is
	// successfully obtained or renewed.
	EventTypeTLSCertificateIssued = "tls.certificate_issued"

	// EventTypeUserCreated is emitted when a new user identity is created in
	// the identity provider.
	EventTypeUserCreated = "user.created"

	// EventTypeUserDeleted is emitted when a user identity is deleted from the
	// identity provider.
	EventTypeUserDeleted = "user.deleted"
)

// Event is the base structured log event.
// All VibeWarden log events follow this schema for AI-readability.
// The schema is published at vibewarden.dev/schema/v1/event.json.
type Event struct {
	// SchemaVersion identifies the schema version (always "v1" for now).
	SchemaVersion string

	// EventType identifies the event kind (e.g., "auth.success", "rate_limit.hit").
	EventType string

	// Timestamp is when the event occurred, always in UTC.
	Timestamp time.Time

	// AISummary is a human- and AI-readable sentence describing what happened.
	// It should be concise (under 200 characters) and include the most relevant
	// identifiers so an LLM can understand the event without reading the payload.
	AISummary string

	// Payload contains event-specific structured data.
	// Keys and value types are defined per event type in the JSON schema.
	Payload map[string]any
}
