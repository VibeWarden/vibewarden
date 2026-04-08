package events

import (
	"fmt"
	"time"
)

// EventTypeEgressSanitized is emitted after the egress proxy applies PII
// redaction rules to an outbound request. The payload reports how many fields
// were redacted in each category (headers, query params, body fields) so
// operators can audit sanitization effectiveness.
const EventTypeEgressSanitized = "egress.sanitized"

// EgressSanitizedParams contains the parameters needed to construct an
// egress.sanitized event.
type EgressSanitizedParams struct {
	// Route is the matched egress route name.
	Route string

	// Method is the HTTP method of the outbound request (e.g. "GET", "POST").
	Method string

	// URL is the destination URL of the outbound request.
	// Must not include bearer tokens or credentials.
	URL string

	// RedactedHeaders is the count of request headers whose log values were
	// replaced with "[REDACTED]".
	RedactedHeaders int

	// StrippedQueryParams is the count of query parameters removed from the
	// request URL before forwarding.
	StrippedQueryParams int

	// RedactedBodyFields is the count of JSON body fields replaced with
	// "[REDACTED]" before the request was forwarded.
	RedactedBodyFields int

	// TraceID is the W3C trace-id of the inbound request that triggered this
	// egress call. Empty when no inbound trace context is available.
	TraceID string
}

// NewEgressSanitized creates an egress.sanitized event indicating that the
// egress proxy applied PII redaction rules to an outbound request.
func NewEgressSanitized(params EgressSanitizedParams) Event {
	total := params.RedactedHeaders + params.StrippedQueryParams + params.RedactedBodyFields
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeEgressSanitized,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityInfo,
		Category:      CategoryNetwork,
		AISummary: fmt.Sprintf(
			"Egress request sanitized: %s %s via route %q — %d field(s) redacted (%d header(s), %d query param(s), %d body field(s))",
			params.Method, params.URL, params.Route,
			total,
			params.RedactedHeaders, params.StrippedQueryParams, params.RedactedBodyFields,
		),
		Payload: map[string]any{
			"route":                 params.Route,
			"method":                params.Method,
			"url":                   params.URL,
			"redacted_headers":      params.RedactedHeaders,
			"stripped_query_params": params.StrippedQueryParams,
			"redacted_body_fields":  params.RedactedBodyFields,
			"total_redacted":        total,
		},
		Actor:       Actor{Type: ActorTypeSystem},
		Resource:    Resource{Type: ResourceTypeEgressRoute, Path: params.Route, Method: params.Method},
		TraceID:     params.TraceID,
		TriggeredBy: "egress_sanitizer",
	}
}
