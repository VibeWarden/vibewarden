package events

import (
	"fmt"
	"time"
)

// RateLimitHitParams contains the parameters needed to construct a
// rate_limit.hit event.
type RateLimitHitParams struct {
	// LimitType is the kind of limit that was exceeded: "ip" or "user".
	LimitType string

	// Identifier is the value that was rate-limited: the client IP for
	// LimitType "ip", or the user ID for LimitType "user".
	Identifier string

	// RequestsPerSecond is the configured rate limit (tokens per second).
	RequestsPerSecond float64

	// Burst is the configured burst capacity.
	Burst int

	// RetryAfterSeconds is how long the caller must wait before retrying.
	RetryAfterSeconds int

	// Path is the URL path of the rate-limited request.
	Path string

	// Method is the HTTP method of the rate-limited request.
	Method string

	// ClientIP is the client IP address. Only relevant when LimitType is
	// "user" to record which IP the user was connecting from.
	// May be empty when LimitType is "ip" (identifier already is the IP).
	ClientIP string

	// RequestID is the inbound request identifier (e.g. X-Request-ID header).
	RequestID string

	// TraceID is the W3C trace-id of the active OTel span. May be empty.
	TraceID string
}

// NewRateLimitHit creates a rate_limit.hit event indicating that a request
// was rejected because the caller exceeded their rate limit.
func NewRateLimitHit(params RateLimitHitParams) Event {
	payload := map[string]any{
		"limit_type":          params.LimitType,
		"identifier":          params.Identifier,
		"requests_per_second": params.RequestsPerSecond,
		"burst":               params.Burst,
		"retry_after_seconds": params.RetryAfterSeconds,
		"path":                params.Path,
		"method":              params.Method,
	}
	if params.LimitType == "user" && params.ClientIP != "" {
		payload["client_ip"] = params.ClientIP
	}

	// Build the actor from the limit type and identifier.
	var actor Actor
	switch params.LimitType {
	case "user":
		actor = Actor{Type: ActorTypeUser, ID: params.Identifier, IP: params.ClientIP}
	default:
		actor = Actor{Type: ActorTypeIP, ID: params.Identifier, IP: params.Identifier}
	}

	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeRateLimitHit,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityMedium,
		Category:      CategoryResilience,
		AISummary: fmt.Sprintf(
			"Rate limit exceeded for %s %s: %.0f requests/second limit reached",
			params.LimitType, params.Identifier, params.RequestsPerSecond,
		),
		Payload:     payload,
		Actor:       actor,
		Resource:    Resource{Type: ResourceTypeHTTPEndpoint, Path: params.Path, Method: params.Method},
		Outcome:     OutcomeRateLimited,
		RiskSignals: []RiskSignal{{Signal: "rate_limit_exceeded", Score: 0.5, Details: fmt.Sprintf("%s %s exceeded %.0f req/s", params.LimitType, params.Identifier, params.RequestsPerSecond)}},
		RequestID:   params.RequestID,
		TraceID:     params.TraceID,
		TriggeredBy: "rate_limit_middleware",
	}
}

// RateLimitUnidentifiedParams contains the parameters needed to construct a
// rate_limit.unidentified_client event.
type RateLimitUnidentifiedParams struct {
	// Path is the URL path of the rejected request.
	Path string

	// Method is the HTTP method of the rejected request.
	Method string

	// RequestID is the inbound request identifier (e.g. X-Request-ID header).
	RequestID string

	// TraceID is the W3C trace-id of the active OTel span. May be empty.
	TraceID string
}

// NewRateLimitUnidentified creates a rate_limit.unidentified_client event
// indicating that a request was rejected because the client IP address could
// not be determined.
func NewRateLimitUnidentified(params RateLimitUnidentifiedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeRateLimitUnidentified,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityLow,
		Category:      CategoryResilience,
		AISummary:     "Request rejected because the client IP could not be determined",
		Payload: map[string]any{
			"path":   params.Path,
			"method": params.Method,
		},
		Actor:       Actor{Type: ActorTypeIP, ID: ""},
		Resource:    Resource{Type: ResourceTypeHTTPEndpoint, Path: params.Path, Method: params.Method},
		Outcome:     OutcomeBlocked,
		RequestID:   params.RequestID,
		TraceID:     params.TraceID,
		TriggeredBy: "rate_limit_middleware",
	}
}
