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

	// EventTypeUserDeactivated is emitted when a user identity is deactivated,
	// preventing further authentication while retaining the identity record.
	EventTypeUserDeactivated = "user.deactivated"

	// EventTypeAuthProviderUnavailable is emitted when the auth provider
	// (Ory Kratos) becomes unreachable and requests are being affected.
	EventTypeAuthProviderUnavailable = "auth.provider_unavailable"

	// EventTypeAuthProviderRecovered is emitted when the auth provider
	// (Ory Kratos) becomes reachable again after a period of unavailability.
	EventTypeAuthProviderRecovered = "auth.provider_recovered"

	// EventTypeAuditLogFailure is emitted when an audit log entry cannot be
	// persisted to the backing store (e.g. PostgreSQL is unavailable).
	// The operation that triggered the audit entry is not rolled back.
	EventTypeAuditLogFailure = "audit.log_failure"

	// EventTypeIPFilterBlocked is emitted when a request is rejected by the
	// IP filter plugin because the client IP is not in the allowlist or is
	// in the blocklist.
	EventTypeIPFilterBlocked = "ip_filter.blocked"

	// EventTypeSecretRotated is emitted when a dynamic secret (e.g. dynamic
	// Postgres credentials) is successfully rotated before its TTL expires.
	EventTypeSecretRotated = "secret.rotated"

	// EventTypeSecretRotationFailed is emitted when a dynamic secret rotation
	// attempt fails. The old credentials remain in use until the next retry.
	EventTypeSecretRotationFailed = "secret.rotation_failed"

	// EventTypeSecretHealthCheck is emitted on each scheduled secret health
	// check run. The payload contains the list of findings (if any).
	EventTypeSecretHealthCheck = "secret.health_check"

	// EventTypeRateLimitStoreFallback is emitted when the Redis rate limit
	// store becomes unavailable and the rate limiter falls back to the
	// in-memory store to preserve availability.
	EventTypeRateLimitStoreFallback = "rate_limit.store_fallback"

	// EventTypeRateLimitStoreRecovered is emitted when the Redis rate limit
	// store becomes reachable again after a period of unavailability and the
	// rate limiter switches back from the in-memory fallback.
	EventTypeRateLimitStoreRecovered = "rate_limit.store_recovered"

	// EventTypeUpstreamTimeout is emitted when the upstream application does
	// not respond within the configured resilience.timeout duration. The
	// request is terminated and a 504 Gateway Timeout is returned to the
	// client.
	EventTypeUpstreamTimeout = "upstream.timeout"

	// EventTypeUpstreamRetry is emitted each time the retry middleware retries
	// a failed upstream request. One event is emitted per retry attempt; the
	// initial request does not produce a retry event.
	EventTypeUpstreamRetry = "upstream.retry"

	// EventTypeAPIKeySuccess is emitted when a request is authenticated
	// successfully via an API key.
	EventTypeAPIKeySuccess = "auth.api_key.success"

	// EventTypeAPIKeyFailed is emitted when a request is rejected because the
	// presented API key is missing, invalid, or belongs to an inactive key.
	EventTypeAPIKeyFailed = "auth.api_key.failed"

	// EventTypeAPIKeyForbidden is emitted when a valid API key is presented but
	// lacks the required scopes to access the requested path+method combination.
	EventTypeAPIKeyForbidden = "auth.api_key.forbidden"

	// EventTypeMaintenanceRequestBlocked is emitted when a request is rejected
	// because maintenance mode is enabled. The path and method of the blocked
	// request are included in the payload.
	EventTypeMaintenanceRequestBlocked = "maintenance.request_blocked"

	// EventTypeTLSCertExpiryWarning is emitted by the certificate expiry monitor
	// when a TLS certificate expires within 30 days. The payload includes the
	// certificate subject, issuer, expiry time, and days remaining.
	EventTypeTLSCertExpiryWarning = "tls.cert_expiry_warning"

	// EventTypeTLSCertExpiryCritical is emitted by the certificate expiry monitor
	// when a TLS certificate expires within 7 days. The payload includes the
	// certificate subject, issuer, expiry time, and days remaining.
	EventTypeTLSCertExpiryCritical = "tls.cert_expiry_critical"
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
