package mcp

// EventTypeInfo describes a single VibeWarden event type and its fields.
// It is used by the vibewarden_schema_describe MCP tool to return structured
// schema information to AI agents without requiring a live sidecar connection.
type EventTypeInfo struct {
	// Description is a one-line human-readable summary of when this event is emitted.
	Description string

	// Fields lists every field that appears on this event type's Event struct or
	// Payload map. Common top-level fields (schema_version, event_type, timestamp,
	// ai_summary) are omitted here — they appear on every event.
	Fields []FieldInfo
}

// FieldInfo describes a single field of an event type.
type FieldInfo struct {
	// Name is the field name as it appears in the JSON log output.
	Name string

	// Type is the Go-level type name of the field (e.g. "string", "int", "float64").
	Type string

	// Description explains what the field represents and any notable constraints.
	Description string
}

// eventTypeRegistry is the authoritative static map of every event type
// supported by VibeWarden's structured log schema.
// Every constant defined in internal/domain/events/events.go and the
// per-subsystem event files must have an entry here.
var eventTypeRegistry = map[string]EventTypeInfo{

	// -----------------------------------------------------------------------
	// proxy
	// -----------------------------------------------------------------------

	"proxy.started": {
		Description: "Emitted once when the reverse proxy starts successfully and is ready to accept connections.",
		Fields: []FieldInfo{
			{Name: "listen", Type: "string", Description: "Address the proxy is listening on (e.g. \":8443\")."},
			{Name: "upstream", Type: "string", Description: "Address requests are forwarded to (e.g. \"localhost:3000\")."},
			{Name: "tls_enabled", Type: "bool", Description: "Whether TLS termination is active."},
			{Name: "tls_provider", Type: "string", Description: "TLS certificate provider (e.g. \"letsencrypt\", \"self-signed\"). Empty when TLS is disabled."},
			{Name: "security_headers_enabled", Type: "bool", Description: "Whether the security-headers middleware is active."},
			{Name: "version", Type: "string", Description: "VibeWarden binary version string."},
		},
	},

	"proxy.kratos_flow": {
		Description: "Emitted for every request routed to the Ory Kratos self-service flow API.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "Client identity — empty for unauthenticated flow requests."},
			{Name: "resource", Type: "Resource", Description: "HTTP endpoint (method + Kratos flow path)."},
			{Name: "method", Type: "string", Description: "HTTP method of the proxied request (e.g. \"GET\", \"POST\")."},
			{Name: "path", Type: "string", Description: "URL path of the proxied request."},
		},
	},

	// -----------------------------------------------------------------------
	// auth
	// -----------------------------------------------------------------------

	"auth.success": {
		Description: "Emitted when a request carries a valid Kratos session and is allowed through to the upstream application.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "Authenticated user (type: user, id: identity_id, ip: client IP)."},
			{Name: "resource", Type: "Resource", Description: "HTTP endpoint (method + path)."},
			{Name: "outcome", Type: "string", Description: "Always \"allowed\"."},
			{Name: "request_id", Type: "string", Description: "Value of the X-Request-ID header. May be empty."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id of the active OTel span. May be empty."},
			{Name: "triggered_by", Type: "string", Description: "Always \"auth_middleware\"."},
			{Name: "method", Type: "string", Description: "HTTP method of the authenticated request."},
			{Name: "path", Type: "string", Description: "URL path of the authenticated request."},
			{Name: "session_id", Type: "string", Description: "Kratos session identifier."},
			{Name: "identity_id", Type: "string", Description: "Kratos identity (user) identifier."},
			{Name: "email", Type: "string", Description: "Email address associated with the authenticated identity."},
		},
	},

	"auth.failed": {
		Description: "Emitted when a request is rejected due to a missing, invalid, or expired Kratos session.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "Client identified by IP address only (type: ip)."},
			{Name: "resource", Type: "Resource", Description: "HTTP endpoint (method + path)."},
			{Name: "outcome", Type: "string", Description: "Always \"blocked\"."},
			{Name: "request_id", Type: "string", Description: "Value of the X-Request-ID header. May be empty."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id of the active OTel span. May be empty."},
			{Name: "triggered_by", Type: "string", Description: "Always \"auth_middleware\"."},
			{Name: "method", Type: "string", Description: "HTTP method of the rejected request."},
			{Name: "path", Type: "string", Description: "URL path of the rejected request."},
			{Name: "reason", Type: "string", Description: "Short description of why the request was rejected (e.g. \"missing session cookie\")."},
			{Name: "detail", Type: "string", Description: "Optional additional detail string (e.g. an error message). May be empty."},
		},
	},

	"auth.provider_unavailable": {
		Description: "Emitted when the Ory Kratos auth provider becomes unreachable; emitted at most once per transition to avoid log flooding.",
		Fields: []FieldInfo{
			{Name: "provider_url", Type: "string", Description: "URL of the unavailable auth provider."},
			{Name: "error", Type: "string", Description: "Short description of the connectivity failure."},
			{Name: "affected_path", Type: "string", Description: "URL path of the request that triggered discovery of the unavailability. May be empty."},
		},
	},

	"auth.provider_recovered": {
		Description: "Emitted when the Ory Kratos auth provider becomes reachable again after a period of unavailability.",
		Fields: []FieldInfo{
			{Name: "provider_url", Type: "string", Description: "URL of the recovered auth provider."},
		},
	},

	// -----------------------------------------------------------------------
	// auth — API key
	// -----------------------------------------------------------------------

	"auth.api_key.success": {
		Description: "Emitted when a request is authenticated successfully via a valid API key.",
		Fields: []FieldInfo{
			{Name: "method", Type: "string", Description: "HTTP method of the authenticated request."},
			{Name: "path", Type: "string", Description: "URL path of the authenticated request."},
			{Name: "key_name", Type: "string", Description: "Human-readable name of the API key that was accepted."},
			{Name: "scopes", Type: "[]string", Description: "List of scopes granted by the key."},
		},
	},

	"auth.api_key.failed": {
		Description: "Emitted when a request is rejected because the presented API key is missing, invalid, or belongs to an inactive key.",
		Fields: []FieldInfo{
			{Name: "method", Type: "string", Description: "HTTP method of the rejected request."},
			{Name: "path", Type: "string", Description: "URL path of the rejected request."},
			{Name: "reason", Type: "string", Description: "Short description of why the request was rejected (e.g. \"missing api key\", \"invalid or inactive api key\")."},
		},
	},

	"auth.api_key.forbidden": {
		Description: "Emitted when a valid API key is presented but lacks the required scopes to access the requested path and method.",
		Fields: []FieldInfo{
			{Name: "method", Type: "string", Description: "HTTP method of the forbidden request."},
			{Name: "path", Type: "string", Description: "URL path of the forbidden request."},
			{Name: "key_name", Type: "string", Description: "Human-readable name of the API key that was presented."},
			{Name: "key_scopes", Type: "[]string", Description: "List of scopes held by the key."},
			{Name: "required_scopes", Type: "[]string", Description: "List of scopes required by the matching scope rule."},
		},
	},

	// -----------------------------------------------------------------------
	// auth — JWT
	// -----------------------------------------------------------------------

	"auth.jwt_valid": {
		Description: "Emitted when a JWT token passes all validation checks (signature, issuer, audience, expiry).",
		Fields: []FieldInfo{
			{Name: "method", Type: "string", Description: "HTTP method of the authenticated request."},
			{Name: "path", Type: "string", Description: "URL path of the authenticated request."},
			{Name: "subject", Type: "string", Description: "Value of the \"sub\" claim from the validated token."},
			{Name: "issuer", Type: "string", Description: "Value of the \"iss\" claim from the validated token."},
			{Name: "audience", Type: "string", Description: "Value of the \"aud\" claim that was validated against."},
		},
	},

	"auth.jwt_invalid": {
		Description: "Emitted when a JWT token fails validation for any reason other than expiry (bad signature, wrong issuer or audience, parse error).",
		Fields: []FieldInfo{
			{Name: "method", Type: "string", Description: "HTTP method of the rejected request."},
			{Name: "path", Type: "string", Description: "URL path of the rejected request."},
			{Name: "reason", Type: "string", Description: "Machine-readable failure code (e.g. \"invalid_signature\", \"invalid_issuer\", \"invalid_audience\")."},
			{Name: "detail", Type: "string", Description: "Optional additional detail string. May be empty."},
		},
	},

	"auth.jwt_expired": {
		Description: "Emitted when a JWT token is structurally valid but has passed its expiry time.",
		Fields: []FieldInfo{
			{Name: "method", Type: "string", Description: "HTTP method of the rejected request."},
			{Name: "path", Type: "string", Description: "URL path of the rejected request."},
			{Name: "subject", Type: "string", Description: "Value of the \"sub\" claim from the expired token."},
			{Name: "expired_at", Type: "string", Description: "Time at which the token expired (RFC3339 UTC)."},
		},
	},

	"auth.jwks_refresh": {
		Description: "Emitted each time the JWKS cache is successfully refreshed from the remote JWKS endpoint.",
		Fields: []FieldInfo{
			{Name: "jwks_url", Type: "string", Description: "URL from which the key set was fetched."},
			{Name: "key_count", Type: "int", Description: "Number of keys in the refreshed key set."},
		},
	},

	"auth.jwks_error": {
		Description: "Emitted when fetching or parsing the JWKS fails, making JWT validation impossible until the next successful refresh.",
		Fields: []FieldInfo{
			{Name: "jwks_url", Type: "string", Description: "URL that could not be reached or parsed."},
			{Name: "detail", Type: "string", Description: "Error message describing what went wrong."},
		},
	},

	// -----------------------------------------------------------------------
	// rate_limit
	// -----------------------------------------------------------------------

	"rate_limit.hit": {
		Description: "Emitted when a request is rejected because the caller has exceeded their per-IP or per-user rate limit.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "Rate-limited entity: type=ip when limit_type is \"ip\", type=user when limit_type is \"user\"."},
			{Name: "resource", Type: "Resource", Description: "HTTP endpoint (method + path)."},
			{Name: "outcome", Type: "string", Description: "Always \"rate_limited\"."},
			{Name: "risk_signals", Type: "[]RiskSignal", Description: "Signal: \"rate_limit_exceeded\", score: 0.5."},
			{Name: "request_id", Type: "string", Description: "Value of the X-Request-ID header. May be empty."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id of the active OTel span. May be empty."},
			{Name: "triggered_by", Type: "string", Description: "Always \"rate_limit_middleware\"."},
			{Name: "limit_type", Type: "string", Description: "Kind of limit exceeded: \"ip\" or \"user\"."},
			{Name: "identifier", Type: "string", Description: "Value that was rate-limited: client IP for \"ip\" limit, user ID for \"user\" limit."},
			{Name: "requests_per_second", Type: "float64", Description: "Configured rate limit in requests per second."},
			{Name: "burst", Type: "int", Description: "Configured burst capacity."},
			{Name: "retry_after_seconds", Type: "int", Description: "How long the caller must wait before retrying."},
			{Name: "path", Type: "string", Description: "URL path of the rate-limited request."},
			{Name: "method", Type: "string", Description: "HTTP method of the rate-limited request."},
			{Name: "client_ip", Type: "string", Description: "Client IP address. Only present when limit_type is \"user\"."},
		},
	},

	"rate_limit.unidentified_client": {
		Description: "Emitted when a request is rejected because the client IP address could not be determined.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "Empty actor — client IP unknown."},
			{Name: "resource", Type: "Resource", Description: "HTTP endpoint (method + path)."},
			{Name: "outcome", Type: "string", Description: "Always \"blocked\"."},
			{Name: "request_id", Type: "string", Description: "Value of the X-Request-ID header. May be empty."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id. May be empty."},
			{Name: "triggered_by", Type: "string", Description: "Always \"rate_limit_middleware\"."},
			{Name: "path", Type: "string", Description: "URL path of the rejected request."},
			{Name: "method", Type: "string", Description: "HTTP method of the rejected request."},
		},
	},

	"rate_limit.store_fallback": {
		Description: "Emitted when the Redis rate limit store becomes unavailable and the rate limiter falls back to the in-memory store.",
		Fields: []FieldInfo{
			{Name: "reason", Type: "string", Description: "Short description of why the fallback was triggered."},
			{Name: "store", Type: "string", Description: "Always \"memory\"."},
		},
	},

	"rate_limit.store_recovered": {
		Description: "Emitted when the Redis rate limit store becomes reachable again and the rate limiter switches back from the in-memory fallback.",
		Fields: []FieldInfo{
			{Name: "store", Type: "string", Description: "Always \"redis\"."},
		},
	},

	// -----------------------------------------------------------------------
	// request
	// -----------------------------------------------------------------------

	"request.blocked": {
		Description: "Emitted when a request is blocked by a middleware layer for a reason other than auth or rate limiting (e.g. a security policy violation).",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "Client identified by IP address (type: ip)."},
			{Name: "resource", Type: "Resource", Description: "HTTP endpoint (method + path)."},
			{Name: "outcome", Type: "string", Description: "Always \"blocked\"."},
			{Name: "request_id", Type: "string", Description: "Value of the X-Request-ID header. May be empty."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id. May be empty."},
			{Name: "triggered_by", Type: "string", Description: "The middleware or policy that blocked the request (e.g. \"ip_blocklist\")."},
			{Name: "method", Type: "string", Description: "HTTP method of the blocked request."},
			{Name: "path", Type: "string", Description: "URL path of the blocked request."},
			{Name: "reason", Type: "string", Description: "Short description of why the request was blocked."},
			{Name: "blocked_by", Type: "string", Description: "Middleware or policy component that blocked the request."},
			{Name: "client_ip", Type: "string", Description: "Client IP address. May be empty."},
		},
	},

	// -----------------------------------------------------------------------
	// tls
	// -----------------------------------------------------------------------

	"tls.certificate_issued": {
		Description: "Emitted when a new TLS certificate is successfully obtained or renewed.",
		Fields: []FieldInfo{
			{Name: "domain", Type: "string", Description: "Domain name for which the certificate was issued."},
			{Name: "provider", Type: "string", Description: "Certificate authority or provider (e.g. \"letsencrypt\", \"self-signed\")."},
			{Name: "expires_at", Type: "string", Description: "Certificate expiry time in RFC3339 format."},
		},
	},

	"tls.cert_expiry_warning": {
		Description: "Emitted by the certificate expiry monitor when a TLS certificate expires within 30 days.",
		Fields: []FieldInfo{
			{Name: "subject", Type: "string", Description: "Certificate subject (domain name)."},
			{Name: "issuer", Type: "string", Description: "Certificate issuer."},
			{Name: "expires_at", Type: "string", Description: "Certificate expiry time in RFC3339 format."},
			{Name: "days_remaining", Type: "int", Description: "Number of days until the certificate expires."},
		},
	},

	"tls.cert_expiry_critical": {
		Description: "Emitted by the certificate expiry monitor when a TLS certificate expires within 7 days.",
		Fields: []FieldInfo{
			{Name: "subject", Type: "string", Description: "Certificate subject (domain name)."},
			{Name: "issuer", Type: "string", Description: "Certificate issuer."},
			{Name: "expires_at", Type: "string", Description: "Certificate expiry time in RFC3339 format."},
			{Name: "days_remaining", Type: "int", Description: "Number of days until the certificate expires."},
		},
	},

	// -----------------------------------------------------------------------
	// user
	// -----------------------------------------------------------------------

	"user.created": {
		Description: "Emitted when a new user identity is created in the identity provider.",
		Fields: []FieldInfo{
			{Name: "identity_id", Type: "string", Description: "Identity provider identifier for the new user."},
			{Name: "email", Type: "string", Description: "Email address of the new user."},
			{Name: "actor_id", Type: "string", Description: "ID of the admin who performed the action. May be empty for system-initiated creation."},
		},
	},

	"user.deleted": {
		Description: "Emitted when a user identity is deleted from the identity provider.",
		Fields: []FieldInfo{
			{Name: "identity_id", Type: "string", Description: "Identity provider identifier of the deleted user."},
			{Name: "email", Type: "string", Description: "Email address of the deleted user."},
			{Name: "actor_id", Type: "string", Description: "ID of the admin who performed the action. May be empty."},
			{Name: "reason", Type: "string", Description: "Optional human-readable explanation for the deletion. May be empty."},
		},
	},

	"user.deactivated": {
		Description: "Emitted when a user identity is deactivated, preventing further authentication while retaining the identity record.",
		Fields: []FieldInfo{
			{Name: "identity_id", Type: "string", Description: "Identity provider identifier of the deactivated user."},
			{Name: "email", Type: "string", Description: "Email address of the deactivated user."},
			{Name: "actor_id", Type: "string", Description: "ID of the admin who performed the action. May be empty."},
			{Name: "reason", Type: "string", Description: "Optional human-readable explanation for the deactivation. May be empty."},
		},
	},

	// -----------------------------------------------------------------------
	// audit
	// -----------------------------------------------------------------------

	"audit.log_failure": {
		Description: "Emitted when an audit log entry cannot be persisted to the backing store (e.g. PostgreSQL is unavailable). The originating operation is not rolled back.",
		Fields: []FieldInfo{
			{Name: "action", Type: "string", Description: "Audit action that failed to be persisted (e.g. \"user.created\")."},
			{Name: "user_id", Type: "string", Description: "User affected by the action that was not audited."},
			{Name: "error", Type: "string", Description: "Short description of the persistence failure."},
		},
	},

	// -----------------------------------------------------------------------
	// ip_filter
	// -----------------------------------------------------------------------

	"ip_filter.blocked": {
		Description: "Emitted when a request is rejected by the IP filter plugin because the client IP is not in the allowlist or is in the blocklist.",
		Fields: []FieldInfo{
			{Name: "client_ip", Type: "string", Description: "IP address that was blocked."},
			{Name: "mode", Type: "string", Description: "Filter mode in effect: \"allowlist\" or \"blocklist\"."},
			{Name: "method", Type: "string", Description: "HTTP method of the blocked request."},
			{Name: "path", Type: "string", Description: "URL path of the blocked request."},
		},
	},

	// -----------------------------------------------------------------------
	// secret
	// -----------------------------------------------------------------------

	"secret.rotated": {
		Description: "Emitted when a dynamic secret (e.g. dynamic Postgres credentials) is successfully rotated before its TTL expires.",
		Fields: []FieldInfo{
			{Name: "secret_path", Type: "string", Description: "Vault/OpenBao path of the rotated secret."},
			{Name: "lease_id", Type: "string", Description: "Lease identifier of the new credentials."},
			{Name: "ttl_seconds", Type: "int", Description: "TTL of the newly issued secret in seconds."},
		},
	},

	"secret.rotation_failed": {
		Description: "Emitted when a dynamic secret rotation attempt fails. The old credentials remain in use until the next retry.",
		Fields: []FieldInfo{
			{Name: "secret_path", Type: "string", Description: "Vault/OpenBao path of the secret that failed to rotate."},
			{Name: "error", Type: "string", Description: "Error message describing the rotation failure."},
		},
	},

	"secret.health_check": {
		Description: "Emitted on each scheduled secret health check run. The payload contains any findings.",
		Fields: []FieldInfo{
			{Name: "secrets_checked", Type: "int", Description: "Total number of secrets included in this health check run."},
			{Name: "findings", Type: "[]string", Description: "List of human-readable findings (e.g. secrets near expiry). Empty when all secrets are healthy."},
		},
	},

	// -----------------------------------------------------------------------
	// upstream
	// -----------------------------------------------------------------------

	"upstream.timeout": {
		Description: "Emitted when the upstream application does not respond within the configured resilience.timeout duration; a 504 Gateway Timeout is returned to the client.",
		Fields: []FieldInfo{
			{Name: "method", Type: "string", Description: "HTTP method of the timed-out request."},
			{Name: "path", Type: "string", Description: "URL path of the timed-out request."},
			{Name: "timeout_seconds", Type: "float64", Description: "Configured upstream timeout in seconds."},
			{Name: "client_ip", Type: "string", Description: "Remote client IP address."},
		},
	},

	"upstream.retry": {
		Description: "Emitted each time the retry middleware retries a failed upstream request. One event per retry attempt; the initial request does not produce a retry event.",
		Fields: []FieldInfo{
			{Name: "method", Type: "string", Description: "HTTP method of the retried request."},
			{Name: "path", Type: "string", Description: "URL path of the retried request."},
			{Name: "attempt", Type: "int", Description: "Retry attempt number (1 = first retry, 2 = second retry, …)."},
			{Name: "status_code", Type: "int", Description: "Upstream HTTP status code that triggered the retry."},
			{Name: "client_ip", Type: "string", Description: "Remote client IP address."},
		},
	},

	"upstream.health_changed": {
		Description: "Emitted when the upstream application's health status transitions between Unknown, Healthy, and Unhealthy states.",
		Fields: []FieldInfo{
			{Name: "previous_status", Type: "string", Description: "Health status before the transition (e.g. \"unknown\", \"healthy\")."},
			{Name: "new_status", Type: "string", Description: "Health status after the transition (e.g. \"healthy\", \"unhealthy\")."},
			{Name: "consecutive", Type: "int", Description: "Number of consecutive successes or failures that triggered this transition."},
			{Name: "upstream_url", Type: "string", Description: "URL that was probed (host + path, no credentials)."},
			{Name: "last_error", Type: "string", Description: "Error message from the most recent probe when new_status is \"unhealthy\". Omitted when transitioning to \"healthy\"."},
		},
	},

	// -----------------------------------------------------------------------
	// circuit_breaker (inbound upstream circuit breaker)
	// -----------------------------------------------------------------------

	"circuit_breaker.opened": {
		Description: "Emitted when the inbound upstream circuit breaker trips from Closed to Open because consecutive failures reached the threshold.",
		Fields: []FieldInfo{
			{Name: "threshold", Type: "int", Description: "Consecutive failure count that tripped the circuit."},
			{Name: "timeout_seconds", Type: "float64", Description: "Duration the circuit will remain open before allowing a probe, in seconds."},
		},
	},

	"circuit_breaker.half_open": {
		Description: "Emitted when the inbound upstream circuit breaker transitions from Open to HalfOpen because the open timeout expired and a probe request is allowed through.",
		Fields: []FieldInfo{
			{Name: "timeout_seconds", Type: "float64", Description: "Open timeout that elapsed before the probe, in seconds."},
		},
	},

	"circuit_breaker.closed": {
		Description: "Emitted when the inbound upstream circuit breaker returns to Closed because the upstream probe succeeded.",
		Fields:      []FieldInfo{},
	},

	// -----------------------------------------------------------------------
	// config
	// -----------------------------------------------------------------------

	"config.reloaded": {
		Description: "Emitted when configuration is successfully reloaded from disk and applied to all components.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Config file resource (type: config, path: config file path)."},
			{Name: "outcome", Type: "string", Description: "Always \"allowed\"."},
			{Name: "triggered_by", Type: "string", Description: "What initiated the reload: \"file_watcher\" or \"admin_api\"."},
			{Name: "config_path", Type: "string", Description: "Path to the configuration file that was reloaded."},
			{Name: "trigger_source", Type: "string", Description: "What initiated the reload: \"file_watcher\" or \"admin_api\"."},
			{Name: "duration_ms", Type: "int64", Description: "How long the reload took in milliseconds."},
		},
	},

	"config.reload_failed": {
		Description: "Emitted when configuration reload fails due to validation errors or other issues. The old configuration remains active.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Config file resource (type: config, path: config file path)."},
			{Name: "outcome", Type: "string", Description: "Always \"failed\"."},
			{Name: "triggered_by", Type: "string", Description: "What initiated the reload attempt."},
			{Name: "config_path", Type: "string", Description: "Path to the configuration file."},
			{Name: "trigger_source", Type: "string", Description: "What initiated the reload attempt."},
			{Name: "reason", Type: "string", Description: "Human-readable description of why the reload failed."},
			{Name: "validation_errors", Type: "[]string", Description: "List of specific validation errors, if applicable. May be empty."},
		},
	},

	// -----------------------------------------------------------------------
	// maintenance
	// -----------------------------------------------------------------------

	"maintenance.request_blocked": {
		Description: "Emitted when a request is rejected because maintenance mode is enabled.",
		Fields: []FieldInfo{
			{Name: "path", Type: "string", Description: "URL path of the blocked request."},
			{Name: "method", Type: "string", Description: "HTTP method of the blocked request."},
			{Name: "message", Type: "string", Description: "Operator-configured maintenance message returned to the client."},
		},
	},

	// -----------------------------------------------------------------------
	// webhook
	// -----------------------------------------------------------------------

	"webhook.signature_valid": {
		Description: "Emitted when an inbound webhook request carries a valid signature that matches the configured secret.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "Webhook sender identified by IP address (type: ip)."},
			{Name: "resource", Type: "Resource", Description: "HTTP endpoint (method + path)."},
			{Name: "outcome", Type: "string", Description: "Always \"allowed\"."},
			{Name: "triggered_by", Type: "string", Description: "Always \"webhook_middleware\"."},
			{Name: "path", Type: "string", Description: "URL path of the webhook request."},
			{Name: "method", Type: "string", Description: "HTTP method of the webhook request."},
			{Name: "provider", Type: "string", Description: "Signature format used: \"stripe\", \"github\", \"slack\", \"twilio\", or \"generic\"."},
			{Name: "client_ip", Type: "string", Description: "Source IP address of the webhook sender."},
		},
	},

	"webhook.signature_invalid": {
		Description: "Emitted when an inbound webhook request carries an invalid or missing signature. The request is rejected with 401.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "Webhook sender identified by IP address (type: ip)."},
			{Name: "resource", Type: "Resource", Description: "HTTP endpoint (method + path)."},
			{Name: "outcome", Type: "string", Description: "Always \"blocked\"."},
			{Name: "triggered_by", Type: "string", Description: "Always \"webhook_middleware\"."},
			{Name: "path", Type: "string", Description: "URL path of the webhook request."},
			{Name: "method", Type: "string", Description: "HTTP method of the webhook request."},
			{Name: "provider", Type: "string", Description: "Signature format used: \"stripe\", \"github\", \"slack\", \"twilio\", or \"generic\"."},
			{Name: "reason", Type: "string", Description: "Brief description of why the signature was rejected."},
			{Name: "client_ip", Type: "string", Description: "Source IP address of the webhook sender."},
		},
	},

	// -----------------------------------------------------------------------
	// egress
	// -----------------------------------------------------------------------

	"egress.request": {
		Description: "Emitted when the egress proxy begins forwarding an outbound request to an external service.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system) — egress requests are initiated internally."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route, path: route name)."},
			{Name: "triggered_by", Type: "string", Description: "Always \"egress_proxy\"."},
			{Name: "route", Type: "string", Description: "Matched egress route name, or empty string for unmatched routes with allow policy."},
			{Name: "method", Type: "string", Description: "HTTP method of the outbound request."},
			{Name: "url", Type: "string", Description: "Destination URL (sanitised — no auth tokens)."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id of the inbound request that triggered this egress call."},
		},
	},

	"egress.response": {
		Description: "Emitted when the egress proxy receives a complete response from the external service.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "triggered_by", Type: "string", Description: "Always \"egress_proxy\"."},
			{Name: "route", Type: "string", Description: "Matched egress route name."},
			{Name: "method", Type: "string", Description: "HTTP method of the outbound request."},
			{Name: "url", Type: "string", Description: "Destination URL."},
			{Name: "status_code", Type: "int", Description: "HTTP status code returned by the external service."},
			{Name: "duration_seconds", Type: "float64", Description: "Total round-trip duration in seconds."},
			{Name: "attempts", Type: "int", Description: "Total number of upstream attempts (1 = no retries)."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id of the originating inbound request."},
		},
	},

	"egress.blocked": {
		Description: "Emitted when the egress proxy refuses to forward a request because the default policy is deny and no route matched, or a security rule (SSRF, TLS enforcement) blocked it.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "outcome", Type: "string", Description: "Always \"blocked\"."},
			{Name: "triggered_by", Type: "string", Description: "Always \"egress_proxy\"."},
			{Name: "route", Type: "string", Description: "Matched route name, or empty string when no route matched."},
			{Name: "method", Type: "string", Description: "HTTP method of the blocked outbound request."},
			{Name: "url", Type: "string", Description: "Destination URL of the blocked outbound request."},
			{Name: "reason", Type: "string", Description: "Short human-readable description of why the request was blocked."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id."},
		},
	},

	"egress.error": {
		Description: "Emitted when the egress proxy encounters a transport-level error (timeout, DNS failure, connection refused) and cannot return a response.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "outcome", Type: "string", Description: "Always \"failed\"."},
			{Name: "triggered_by", Type: "string", Description: "Always \"egress_proxy\"."},
			{Name: "route", Type: "string", Description: "Matched egress route name."},
			{Name: "method", Type: "string", Description: "HTTP method of the failed outbound request."},
			{Name: "url", Type: "string", Description: "Destination URL of the failed outbound request."},
			{Name: "error", Type: "string", Description: "Human-readable error message. Does not include credentials."},
			{Name: "attempts", Type: "int", Description: "Total number of upstream attempts made before failing."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id."},
		},
	},

	"egress.circuit_breaker.opened": {
		Description: "Emitted when a per-route egress circuit breaker trips from Closed to Open because consecutive upstream failures reached the configured threshold.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "triggered_by", Type: "string", Description: "Always \"egress_circuit_breaker\"."},
			{Name: "route", Type: "string", Description: "Egress route name whose circuit tripped."},
			{Name: "threshold", Type: "int", Description: "Consecutive failure count that tripped the circuit."},
			{Name: "timeout_seconds", Type: "float64", Description: "Duration the circuit will remain open before allowing a probe, in seconds."},
		},
	},

	"egress.circuit_breaker.closed": {
		Description: "Emitted when a per-route egress circuit breaker returns to Closed after a successful probe request confirms that the upstream has recovered.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "triggered_by", Type: "string", Description: "Always \"egress_circuit_breaker\"."},
			{Name: "route", Type: "string", Description: "Egress route name whose circuit closed."},
		},
	},

	"egress.response_invalid": {
		Description: "Emitted when an upstream egress response fails per-route validate_response rules (disallowed status code or content type). The egress proxy returns 502 Bad Gateway.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "outcome", Type: "string", Description: "Always \"failed\"."},
			{Name: "triggered_by", Type: "string", Description: "Always \"egress_proxy\"."},
			{Name: "route", Type: "string", Description: "Matched egress route name."},
			{Name: "method", Type: "string", Description: "HTTP method of the outbound request."},
			{Name: "url", Type: "string", Description: "Destination URL of the outbound request."},
			{Name: "status_code", Type: "int", Description: "HTTP status code returned by the upstream."},
			{Name: "content_type", Type: "string", Description: "Content-Type header value returned by the upstream."},
			{Name: "reason", Type: "string", Description: "Short description of why the response was invalid."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id."},
		},
	},

	"egress.rate_limit_hit": {
		Description: "Emitted when an outbound request to a named egress route is rejected because the per-route rate limit has been exceeded.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "outcome", Type: "string", Description: "Always \"rate_limited\"."},
			{Name: "risk_signals", Type: "[]RiskSignal", Description: "Signal: \"rate_limit_exceeded\", score: 0.5."},
			{Name: "triggered_by", Type: "string", Description: "Always \"egress_rate_limit_middleware\"."},
			{Name: "route", Type: "string", Description: "Egress route name whose rate limit was exceeded."},
			{Name: "limit", Type: "float64", Description: "Configured rate limit in requests per second."},
			{Name: "retry_after_seconds", Type: "float64", Description: "How many seconds the caller should wait before retrying."},
		},
	},

	"egress.sanitized": {
		Description: "Emitted after the egress proxy applies PII redaction rules to an outbound request, reporting how many fields were redacted.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "triggered_by", Type: "string", Description: "Always \"egress_sanitizer\"."},
			{Name: "route", Type: "string", Description: "Matched egress route name."},
			{Name: "method", Type: "string", Description: "HTTP method of the outbound request."},
			{Name: "url", Type: "string", Description: "Destination URL (sanitised — no credentials)."},
			{Name: "redacted_headers", Type: "int", Description: "Count of request headers whose log values were replaced with \"[REDACTED]\"."},
			{Name: "stripped_query_params", Type: "int", Description: "Count of query parameters removed from the request URL before forwarding."},
			{Name: "redacted_body_fields", Type: "int", Description: "Count of JSON body fields replaced with \"[REDACTED]\" before forwarding."},
			{Name: "total_redacted", Type: "int", Description: "Sum of all redacted/stripped field counts."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id."},
		},
	},

	// -----------------------------------------------------------------------
	// llm
	// -----------------------------------------------------------------------

	"llm.prompt_injection_blocked": {
		Description: "Emitted when the prompt injection detector finds a matching pattern in an outbound LLM API request and the route action is \"block\". The request is rejected with 400 Bad Request.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "outcome", Type: "string", Description: "Always \"blocked\"."},
			{Name: "risk_signals", Type: "[]RiskSignal", Description: "Signal: \"prompt_injection\", score: 1.0."},
			{Name: "triggered_by", Type: "string", Description: "Always \"prompt_injection_middleware\"."},
			{Name: "route", Type: "string", Description: "Egress route name where the detection occurred."},
			{Name: "method", Type: "string", Description: "HTTP method of the outbound request."},
			{Name: "url", Type: "string", Description: "Destination URL of the outbound LLM API request."},
			{Name: "pattern", Type: "string", Description: "Name of the detection pattern that matched."},
			{Name: "content_path", Type: "string", Description: "JSON path expression that yielded the matched text (e.g. \".messages[0].content\")."},
			{Name: "action", Type: "string", Description: "Always \"block\" for this event type."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id."},
		},
	},

	"llm.prompt_injection_detected": {
		Description: "Emitted when the prompt injection detector finds a matching pattern and the route action is \"detect\" (log-only). The request is forwarded unchanged.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Egress route (type: egress_route)."},
			{Name: "outcome", Type: "string", Description: "Always \"allowed\" — the request was forwarded despite the detection."},
			{Name: "risk_signals", Type: "[]RiskSignal", Description: "Signal: \"prompt_injection\", score: 0.9."},
			{Name: "triggered_by", Type: "string", Description: "Always \"prompt_injection_middleware\"."},
			{Name: "route", Type: "string", Description: "Egress route name where the detection occurred."},
			{Name: "method", Type: "string", Description: "HTTP method of the outbound request."},
			{Name: "url", Type: "string", Description: "Destination URL of the outbound LLM API request."},
			{Name: "pattern", Type: "string", Description: "Name of the detection pattern that matched."},
			{Name: "content_path", Type: "string", Description: "JSON path expression that yielded the matched text."},
			{Name: "action", Type: "string", Description: "Always \"detect\" for this event type."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id."},
		},
	},

	"llm.response_invalid": {
		Description: "Emitted when an upstream LLM API response body fails JSON Schema validation. May be blocked (502) or passed through depending on the configured action.",
		Fields: []FieldInfo{
			{Name: "route", Type: "string", Description: "Egress route name where the validation failure occurred."},
			{Name: "method", Type: "string", Description: "HTTP method of the outbound request."},
			{Name: "url", Type: "string", Description: "Destination URL of the outbound LLM API request."},
			{Name: "status_code", Type: "int", Description: "HTTP status code returned by the upstream."},
			{Name: "content_type", Type: "string", Description: "Content-Type header returned by the upstream."},
			{Name: "action", Type: "string", Description: "\"block\" or \"warn\"."},
			{Name: "violations", Type: "[]string", Description: "List of JSON Schema violation messages describing why the response failed validation."},
			{Name: "trace_id", Type: "string", Description: "W3C trace-id."},
		},
	},

	// -----------------------------------------------------------------------
	// agent
	// -----------------------------------------------------------------------

	"agent.proposal_created": {
		Description: "Emitted when an MCP agent creates a new configuration-change proposal that is pending human review.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system, id: source component)."},
			{Name: "resource", Type: "Resource", Description: "Config resource (type: config)."},
			{Name: "outcome", Type: "string", Description: "Always \"allowed\" (proposal was accepted into the pending queue)."},
			{Name: "triggered_by", Type: "string", Description: "Source component that created the proposal (e.g. \"mcp_agent\")."},
			{Name: "proposal_id", Type: "string", Description: "UUID of the newly created proposal."},
			{Name: "action_type", Type: "string", Description: "Kind of configuration change (e.g. \"block_ip\")."},
			{Name: "reason", Type: "string", Description: "Agent's justification for the proposal."},
			{Name: "source", Type: "string", Description: "Component that created the proposal."},
		},
	},

	"agent.proposal_approved": {
		Description: "Emitted when a human admin approves a pending proposal and the configuration change is applied.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Config resource (type: config)."},
			{Name: "outcome", Type: "string", Description: "Always \"allowed\"."},
			{Name: "triggered_by", Type: "string", Description: "Always \"admin_api\"."},
			{Name: "proposal_id", Type: "string", Description: "UUID of the approved proposal."},
			{Name: "action_type", Type: "string", Description: "Kind of configuration change that was applied."},
		},
	},

	"agent.proposal_dismissed": {
		Description: "Emitted when a human admin dismisses a pending proposal without applying the change.",
		Fields: []FieldInfo{
			{Name: "actor", Type: "Actor", Description: "System actor (type: system)."},
			{Name: "resource", Type: "Resource", Description: "Config resource (type: config)."},
			{Name: "outcome", Type: "string", Description: "Always \"blocked\" (change was not applied)."},
			{Name: "triggered_by", Type: "string", Description: "Always \"admin_api\"."},
			{Name: "proposal_id", Type: "string", Description: "UUID of the dismissed proposal."},
			{Name: "action_type", Type: "string", Description: "Kind of configuration change that was dismissed."},
		},
	},
}

// AllEventTypes returns a sorted slice of all registered event type strings.
// It is used by the vibewarden_schema_describe tool to list available types.
func AllEventTypes() []string {
	types := make([]string, 0, len(eventTypeRegistry))
	for k := range eventTypeRegistry {
		types = append(types, k)
	}
	// Sort for deterministic output.
	sortStrings(types)
	return types
}

// LookupEventType returns the EventTypeInfo for the given event type string,
// and a boolean indicating whether the type was found in the registry.
func LookupEventType(eventType string) (EventTypeInfo, bool) {
	info, ok := eventTypeRegistry[eventType]
	return info, ok
}

// sortStrings sorts a slice of strings in-place using insertion sort.
// It avoids importing "sort" to keep the dependency footprint minimal,
// and the slice is never large enough for this to matter.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
