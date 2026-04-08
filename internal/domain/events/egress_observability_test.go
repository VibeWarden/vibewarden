package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// --- egress.request ---

func TestNewEgressRequest(t *testing.T) {
	tests := []struct {
		name        string
		params      events.EgressRequestParams
		wantRoute   string
		wantMethod  string
		wantURL     string
		wantTraceID string
		wantSummary string
	}{
		{
			name: "named route GET",
			params: events.EgressRequestParams{
				Route:   "stripe",
				Method:  "GET",
				URL:     "https://api.stripe.com/v1/charges",
				TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
			},
			wantRoute:   "stripe",
			wantMethod:  "GET",
			wantURL:     "https://api.stripe.com/v1/charges",
			wantTraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
			wantSummary: "stripe",
		},
		{
			name: "unmatched allow-policy request no trace",
			params: events.EgressRequestParams{
				Route:   "unmatched",
				Method:  "POST",
				URL:     "https://api.example.com/webhook",
				TraceID: "",
			},
			wantRoute:   "unmatched",
			wantMethod:  "POST",
			wantURL:     "https://api.example.com/webhook",
			wantSummary: "unmatched",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewEgressRequest(tt.params)

			assertEvent(t, e, events.EventTypeEgressRequest)
			requireSummaryContains(t, e.AISummary, tt.wantSummary)
			requireSummaryContains(t, e.AISummary, tt.wantMethod)
			requirePayloadString(t, e.Payload, "route", tt.wantRoute)
			requirePayloadString(t, e.Payload, "method", tt.wantMethod)
			requirePayloadString(t, e.Payload, "url", tt.wantURL)
			if e.TraceID != tt.wantTraceID {
				t.Errorf("TraceID = %q, want %q", e.TraceID, tt.wantTraceID)
			}
		})
	}
}

// --- egress.response ---

func TestNewEgressResponse(t *testing.T) {
	tests := []struct {
		name        string
		params      events.EgressResponseParams
		wantSummary string
	}{
		{
			name: "successful response with retries",
			params: events.EgressResponseParams{
				Route:           "github",
				Method:          "GET",
				URL:             "https://api.github.com/repos/vibewarden/vibewarden",
				StatusCode:      200,
				DurationSeconds: 0.123,
				Attempts:        3,
				TraceID:         "abc123",
			},
			wantSummary: "github",
		},
		{
			name: "5xx response first attempt",
			params: events.EgressResponseParams{
				Route:           "stripe",
				Method:          "POST",
				URL:             "https://api.stripe.com/v1/charges",
				StatusCode:      503,
				DurationSeconds: 0.450,
				Attempts:        1,
				TraceID:         "",
			},
			wantSummary: "stripe",
		},
		{
			name: "unmatched allow-policy request",
			params: events.EgressResponseParams{
				Route:           "unmatched",
				Method:          "GET",
				URL:             "https://external.example.com/api",
				StatusCode:      204,
				DurationSeconds: 0.050,
				Attempts:        1,
				TraceID:         "trace-xyz",
			},
			wantSummary: "204",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewEgressResponse(tt.params)

			assertEvent(t, e, events.EventTypeEgressResponse)
			requireSummaryContains(t, e.AISummary, tt.wantSummary)
			requirePayloadString(t, e.Payload, "route", tt.params.Route)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
			requirePayloadString(t, e.Payload, "url", tt.params.URL)
			requirePayloadKey(t, e.Payload, "status_code")
			requirePayloadKey(t, e.Payload, "duration_seconds")
			requirePayloadKey(t, e.Payload, "attempts")
			if e.TraceID != tt.params.TraceID {
				t.Errorf("TraceID = %q, want %q", e.TraceID, tt.params.TraceID)
			}
		})
	}
}

// --- egress.blocked ---

func TestNewEgressBlocked(t *testing.T) {
	tests := []struct {
		name        string
		params      events.EgressBlockedParams
		wantSummary string
	}{
		{
			name: "denied by default policy",
			params: events.EgressBlockedParams{
				Route:   "unmatched",
				Method:  "GET",
				URL:     "https://api.unknown.example.com/data",
				Reason:  "no route matched default deny policy",
				TraceID: "trace-abc",
			},
			wantSummary: "no route matched",
		},
		{
			name: "plain HTTP blocked by TLS enforcement",
			params: events.EgressBlockedParams{
				Route:   "legacy-api",
				Method:  "POST",
				URL:     "http://api.insecure.example.com/submit",
				Reason:  "plain HTTP not allowed",
				TraceID: "",
			},
			wantSummary: "plain HTTP",
		},
		{
			name: "circuit breaker open",
			params: events.EgressBlockedParams{
				Route:   "payments",
				Method:  "POST",
				URL:     "https://payments.example.com/charge",
				Reason:  "circuit breaker open",
				TraceID: "trace-xyz",
			},
			wantSummary: "circuit breaker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewEgressBlocked(tt.params)

			assertEvent(t, e, events.EventTypeEgressBlocked)
			requireSummaryContains(t, e.AISummary, tt.wantSummary)
			requirePayloadString(t, e.Payload, "route", tt.params.Route)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
			requirePayloadString(t, e.Payload, "url", tt.params.URL)
			requirePayloadString(t, e.Payload, "reason", tt.params.Reason)
			if e.TraceID != tt.params.TraceID {
				t.Errorf("TraceID = %q, want %q", e.TraceID, tt.params.TraceID)
			}
		})
	}
}

// --- egress.circuit_breaker.opened ---

func TestNewEgressCircuitBreakerOpened(t *testing.T) {
	tests := []struct {
		name   string
		params events.EgressCircuitBreakerOpenedParams
	}{
		{
			name: "circuit tripped after threshold failures",
			params: events.EgressCircuitBreakerOpenedParams{
				Route:          "stripe",
				Threshold:      5,
				TimeoutSeconds: 30.0,
			},
		},
		{
			name: "circuit tripped with single failure threshold",
			params: events.EgressCircuitBreakerOpenedParams{
				Route:          "payments",
				Threshold:      1,
				TimeoutSeconds: 60.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewEgressCircuitBreakerOpened(tt.params)

			assertEvent(t, e, events.EventTypeEgressCircuitBreakerOpened)
			requireSummaryContains(t, e.AISummary, tt.params.Route)
			requirePayloadString(t, e.Payload, "route", tt.params.Route)
			requirePayloadKey(t, e.Payload, "threshold")
			requirePayloadKey(t, e.Payload, "timeout_seconds")

			if got := e.Payload["threshold"]; got != tt.params.Threshold {
				t.Errorf("Payload[threshold] = %v, want %v", got, tt.params.Threshold)
			}
			if got := e.Payload["timeout_seconds"]; got != tt.params.TimeoutSeconds {
				t.Errorf("Payload[timeout_seconds] = %v, want %v", got, tt.params.TimeoutSeconds)
			}
		})
	}
}

// --- egress.circuit_breaker.closed ---

func TestNewEgressCircuitBreakerClosed(t *testing.T) {
	tests := []struct {
		name   string
		params events.EgressCircuitBreakerClosedParams
	}{
		{
			name:   "circuit recovered for stripe route",
			params: events.EgressCircuitBreakerClosedParams{Route: "stripe"},
		},
		{
			name:   "circuit recovered for payments route",
			params: events.EgressCircuitBreakerClosedParams{Route: "payments"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewEgressCircuitBreakerClosed(tt.params)

			assertEvent(t, e, events.EventTypeEgressCircuitBreakerClosed)
			requireSummaryContains(t, e.AISummary, tt.params.Route)
			requirePayloadString(t, e.Payload, "route", tt.params.Route)
		})
	}
}

// --- egress.error ---

func TestNewEgressError(t *testing.T) {
	tests := []struct {
		name        string
		params      events.EgressErrorParams
		wantSummary string
	}{
		{
			name: "DNS resolution failure",
			params: events.EgressErrorParams{
				Route:    "stripe",
				Method:   "GET",
				URL:      "https://api.stripe.com/v1/charges",
				Error:    "dial tcp: lookup api.stripe.com: no such host",
				Attempts: 1,
				TraceID:  "trace-aaa",
			},
			wantSummary: "failed",
		},
		{
			name: "timeout after max retries",
			params: events.EgressErrorParams{
				Route:    "github",
				Method:   "GET",
				URL:      "https://api.github.com/repos/vibewarden/vibewarden",
				Error:    "context deadline exceeded",
				Attempts: 3,
				TraceID:  "",
			},
			wantSummary: "3 attempt",
		},
		{
			name: "connection refused unmatched route",
			params: events.EgressErrorParams{
				Route:    "unmatched",
				Method:   "POST",
				URL:      "https://webhook.example.com/notify",
				Error:    "connection refused",
				Attempts: 1,
				TraceID:  "trace-bbb",
			},
			wantSummary: "unmatched",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewEgressError(tt.params)

			assertEvent(t, e, events.EventTypeEgressError)
			requireSummaryContains(t, e.AISummary, tt.wantSummary)
			requirePayloadString(t, e.Payload, "route", tt.params.Route)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
			requirePayloadString(t, e.Payload, "url", tt.params.URL)
			requirePayloadString(t, e.Payload, "error", tt.params.Error)
			requirePayloadKey(t, e.Payload, "attempts")
			if e.TraceID != tt.params.TraceID {
				t.Errorf("TraceID = %q, want %q", e.TraceID, tt.params.TraceID)
			}
		})
	}
}
