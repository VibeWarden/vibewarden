package events

import (
	"fmt"
	"strings"
	"time"
)

// LLM response validation event type constants.
const (
	// EventTypeLLMResponseInvalid is emitted when an upstream LLM API response
	// body fails JSON Schema validation. The response may be blocked (502 Bad
	// Gateway) or passed through depending on the configured action.
	EventTypeLLMResponseInvalid = "llm.response_invalid"
)

// LLMResponseInvalidParams contains the parameters needed to construct an
// llm.response_invalid event.
type LLMResponseInvalidParams struct {
	// Route is the egress route name where the validation failure occurred.
	Route string

	// Method is the HTTP method of the outbound request (e.g. "POST").
	Method string

	// URL is the destination URL of the outbound LLM API request.
	URL string

	// StatusCode is the HTTP status code returned by the upstream.
	StatusCode int

	// ContentType is the Content-Type header returned by the upstream.
	ContentType string

	// Action is "block" or "warn".
	Action string

	// Violations is the list of JSON Schema violation messages describing why
	// the response failed validation.
	Violations []string

	// TraceID is the W3C trace-id of the inbound request. Empty when no trace
	// context is available.
	TraceID string
}

// NewLLMResponseInvalid creates an llm.response_invalid event indicating that
// an upstream LLM API response body did not conform to the configured JSON
// Schema.
func NewLLMResponseInvalid(params LLMResponseInvalidParams) Event {
	violationSummary := formatViolationSummary(params.Violations)
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeLLMResponseInvalid,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityMedium,
		Category:      CategoryPolicy,
		AISummary: fmt.Sprintf(
			"LLM response schema validation failed on route %q (%s %s) — action: %s — %s",
			params.Route, params.Method, params.URL, params.Action, violationSummary,
		),
		Payload: map[string]any{
			"route":        params.Route,
			"method":       params.Method,
			"url":          params.URL,
			"status_code":  params.StatusCode,
			"content_type": params.ContentType,
			"action":       params.Action,
			"violations":   params.Violations,
		},
	}
}

// formatViolationSummary formats a list of violations into a short summary
// string suitable for inclusion in AISummary.
func formatViolationSummary(violations []string) string {
	if len(violations) == 0 {
		return "no details"
	}
	if len(violations) == 1 {
		return violations[0]
	}
	n := len(violations)
	if n > 3 {
		n = 3
	}
	return fmt.Sprintf("%d violations: %s", len(violations), strings.Join(violations[:n], "; "))
}
