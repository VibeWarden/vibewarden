package events

// ActorType identifies the kind of entity that initiated an action.
type ActorType string

const (
	// ActorTypeIP identifies an anonymous client identified only by IP address.
	ActorTypeIP ActorType = "ip"

	// ActorTypeUser identifies an authenticated user (Kratos identity or JWT subject).
	ActorTypeUser ActorType = "user"

	// ActorTypeAPIKey identifies a request authenticated via an API key.
	ActorTypeAPIKey ActorType = "api_key"

	// ActorTypeSystem identifies an internally-initiated action (e.g. health probe,
	// certificate renewal, config reload triggered by the file watcher).
	ActorTypeSystem ActorType = "system"
)

// String returns the string representation of the ActorType.
func (a ActorType) String() string { return string(a) }

// Actor describes the entity that initiated a security-relevant action.
// All fields except Type are optional; use the zero value when unknown.
type Actor struct {
	// Type identifies the category of the actor.
	Type ActorType `json:"type"`

	// ID is the actor's unique identifier within its type namespace:
	// a user ID for ActorTypeUser, an API key name for ActorTypeAPIKey,
	// an IP address for ActorTypeIP, or an empty string for ActorTypeSystem.
	ID string `json:"id"`

	// IP is the client IP address. Present for ActorTypeIP (where it equals ID)
	// and optionally for ActorTypeUser and ActorTypeAPIKey when the source IP is
	// known. Omitted for ActorTypeSystem.
	IP string `json:"ip,omitempty"`
}

// ResourceType identifies the category of the resource being accessed or affected.
type ResourceType string

const (
	// ResourceTypeHTTPEndpoint identifies an inbound HTTP endpoint exposed by the
	// sidecar (proxy path/method).
	ResourceTypeHTTPEndpoint ResourceType = "http_endpoint"

	// ResourceTypeEgressRoute identifies a named egress route to an external service.
	ResourceTypeEgressRoute ResourceType = "egress_route"

	// ResourceTypeConfig identifies the VibeWarden configuration file.
	ResourceTypeConfig ResourceType = "config"
)

// String returns the string representation of the ResourceType.
func (r ResourceType) String() string { return string(r) }

// Resource describes the target of a security-relevant action.
// All fields except Type are optional; use the zero value when unknown.
type Resource struct {
	// Type identifies the category of the resource.
	Type ResourceType `json:"type"`

	// Path is the URL path (for HTTP endpoints) or route name (for egress routes)
	// or file path (for config). May be empty for system-level resources.
	Path string `json:"path"`

	// Method is the HTTP method (e.g. "GET", "POST"). Only meaningful when Type is
	// ResourceTypeHTTPEndpoint. Omitted otherwise.
	Method string `json:"method,omitempty"`
}

// Outcome describes the result of a security enforcement decision.
type Outcome string

const (
	// OutcomeAllowed indicates the request or action was permitted.
	OutcomeAllowed Outcome = "allowed"

	// OutcomeBlocked indicates the request or action was rejected by a policy.
	OutcomeBlocked Outcome = "blocked"

	// OutcomeRateLimited indicates the request was rejected due to rate limiting.
	OutcomeRateLimited Outcome = "rate_limited"

	// OutcomeFailed indicates the action failed due to an internal or transport error.
	OutcomeFailed Outcome = "failed"
)

// String returns the string representation of the Outcome.
func (o Outcome) String() string { return string(o) }

// Severity represents the severity level of an event for agent triage.
type Severity string

const (
	// SeverityInfo indicates an informational event with no immediate concern.
	SeverityInfo Severity = "info"

	// SeverityLow indicates a low-severity event that may warrant attention.
	SeverityLow Severity = "low"

	// SeverityMedium indicates a medium-severity event requiring investigation.
	SeverityMedium Severity = "medium"

	// SeverityHigh indicates a high-severity event requiring prompt action.
	SeverityHigh Severity = "high"

	// SeverityCritical indicates a critical event requiring immediate response.
	SeverityCritical Severity = "critical"
)

// String returns the string representation of the Severity.
func (s Severity) String() string { return string(s) }

// Category represents the event category for grouping and filtering.
type Category string

const (
	// CategoryAuth covers authentication and authorisation events.
	CategoryAuth Category = "auth"

	// CategoryNetwork covers network-level events (proxy, upstream, egress).
	CategoryNetwork Category = "network"

	// CategoryPolicy covers security policy enforcement events.
	CategoryPolicy Category = "policy"

	// CategoryResilience covers circuit breaker, retry, and failover events.
	CategoryResilience Category = "resilience"

	// CategorySecret covers secret rotation and health check events.
	CategorySecret Category = "secret"

	// CategoryUser covers user lifecycle events (create, delete, deactivate).
	CategoryUser Category = "user"

	// CategoryAudit covers audit trail and configuration events.
	CategoryAudit Category = "audit"
)

// String returns the string representation of the Category.
func (c Category) String() string { return string(c) }

// RiskSignal captures a single machine-detectable risk indicator associated with
// a security event.
type RiskSignal struct {
	// Signal is a stable identifier for the risk pattern (e.g. "brute_force",
	// "sqli_attempt", "prompt_injection").
	Signal string `json:"signal"`

	// Score is a normalised risk score in the range [0.0, 1.0]. Higher values
	// indicate higher confidence that the signal is a genuine threat.
	Score float64 `json:"score"`

	// Details is a free-form human-readable explanation of why this signal was
	// raised (e.g. the matched pattern text, the request count that triggered the
	// brute-force heuristic).
	Details string `json:"details"`
}
