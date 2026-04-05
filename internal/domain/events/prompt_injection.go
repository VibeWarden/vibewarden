package events

import (
	"fmt"
	"time"
)

// Prompt injection event type constants.
const (
	// EventTypeLLMPromptInjectionBlocked is emitted when the prompt injection
	// detector finds a matching pattern in an outbound LLM API request body and
	// the route is configured with action "block". The request is rejected with
	// 400 Bad Request and is not forwarded to the upstream.
	EventTypeLLMPromptInjectionBlocked = "llm.prompt_injection_blocked"

	// EventTypeLLMPromptInjectionDetected is emitted when the prompt injection
	// detector finds a matching pattern in an outbound LLM API request body and
	// the route is configured with action "detect" (log-only). The request is
	// forwarded to the upstream unchanged.
	EventTypeLLMPromptInjectionDetected = "llm.prompt_injection_detected"
)

// LLMPromptInjectionParams contains the parameters needed to construct a
// prompt injection event.
type LLMPromptInjectionParams struct {
	// Route is the egress route name where the detection occurred.
	Route string

	// Method is the HTTP method of the outbound request (e.g. "POST").
	Method string

	// URL is the destination URL of the outbound request.
	URL string

	// Pattern is the name of the detection pattern that matched.
	Pattern string

	// ContentPath is the JSON path expression that yielded the matched text
	// (e.g. ".messages[0].content", ".prompt").
	ContentPath string

	// Action is either "block" or "detect".
	Action string

	// TraceID is the W3C trace-id of the inbound request. Empty when no trace
	// context is available.
	TraceID string
}

// NewLLMPromptInjectionBlocked creates an llm.prompt_injection_blocked event
// indicating that an outbound LLM request was rejected due to a detected
// prompt injection payload.
func NewLLMPromptInjectionBlocked(params LLMPromptInjectionParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeLLMPromptInjectionBlocked,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Prompt injection blocked on route %q: pattern %q matched in %s",
			params.Route, params.Pattern, params.ContentPath,
		),
		Payload: map[string]any{
			"route":        params.Route,
			"method":       params.Method,
			"url":          params.URL,
			"pattern":      params.Pattern,
			"content_path": params.ContentPath,
			"action":       params.Action,
			"trace_id":     params.TraceID,
		},
	}
}

// NewLLMPromptInjectionDetected creates an llm.prompt_injection_detected event
// indicating that an outbound LLM request contained a prompt injection payload
// but was forwarded because the route action is "detect" (log-only).
func NewLLMPromptInjectionDetected(params LLMPromptInjectionParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeLLMPromptInjectionDetected,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Prompt injection detected (allowed) on route %q: pattern %q matched in %s",
			params.Route, params.Pattern, params.ContentPath,
		),
		Payload: map[string]any{
			"route":        params.Route,
			"method":       params.Method,
			"url":          params.URL,
			"pattern":      params.Pattern,
			"content_path": params.ContentPath,
			"action":       params.Action,
			"trace_id":     params.TraceID,
		},
	}
}
