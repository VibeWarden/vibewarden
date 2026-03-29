package events_test

import (
	"fmt"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// requirePayloadInt asserts that the payload map contains key with the
// expected int value.
func requirePayloadInt(t *testing.T, payload map[string]any, key string, want int) {
	t.Helper()
	v, ok := payload[key]
	if !ok {
		t.Errorf("Payload missing key %q", key)
		return
	}
	got, ok := v.(int)
	if !ok {
		t.Errorf("Payload[%q] type = %T (%v), want int", key, v, v)
		return
	}
	if got != want {
		t.Errorf("Payload[%q] = %d, want %d", key, got, want)
	}
}

// requireSummaryContainsInt is a helper that asserts the summary contains the
// string representation of n.
func requireSummaryContainsInt(t *testing.T, summary string, n int) {
	t.Helper()
	requireSummaryContains(t, summary, fmt.Sprintf("%d", n))
}

// TestNewEgressResponseInvalid verifies the egress.response_invalid event
// constructor produces a well-formed event with all required payload fields.
func TestNewEgressResponseInvalid(t *testing.T) {
	tests := []struct {
		name        string
		params      events.EgressResponseInvalidParams
		wantRoute   string
		wantMethod  string
		wantURL     string
		wantStatus  int
		wantCT      string
		wantReason  string
		wantTraceID string
	}{
		{
			name: "status code not allowed",
			params: events.EgressResponseInvalidParams{
				Route:       "stripe",
				Method:      "GET",
				URL:         "https://api.stripe.com/v1/charges",
				StatusCode:  500,
				ContentType: "application/json",
				Reason:      "status code 500 not in allowed list",
				TraceID:     "abc123",
			},
			wantRoute:   "stripe",
			wantMethod:  "GET",
			wantURL:     "https://api.stripe.com/v1/charges",
			wantStatus:  500,
			wantCT:      "application/json",
			wantReason:  "status code 500 not in allowed list",
			wantTraceID: "abc123",
		},
		{
			name: "content type not allowed",
			params: events.EgressResponseInvalidParams{
				Route:       "github",
				Method:      "POST",
				URL:         "https://api.github.com/repos",
				StatusCode:  200,
				ContentType: "text/html",
				Reason:      `content type "text/html" not in allowed list`,
				TraceID:     "",
			},
			wantRoute:  "github",
			wantMethod: "POST",
			wantURL:    "https://api.github.com/repos",
			wantStatus: 200,
			wantCT:     "text/html",
			wantReason: `content type "text/html" not in allowed list`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewEgressResponseInvalid(tt.params)

			assertEvent(t, e, events.EventTypeEgressResponseInvalid)
			requireSummaryContains(t, e.AISummary, tt.wantRoute)
			requireSummaryContains(t, e.AISummary, tt.wantMethod)

			requirePayloadString(t, e.Payload, "route", tt.wantRoute)
			requirePayloadString(t, e.Payload, "method", tt.wantMethod)
			requirePayloadString(t, e.Payload, "url", tt.wantURL)
			requirePayloadInt(t, e.Payload, "status_code", tt.wantStatus)
			requirePayloadString(t, e.Payload, "content_type", tt.wantCT)
			requirePayloadString(t, e.Payload, "reason", tt.wantReason)
			requirePayloadString(t, e.Payload, "trace_id", tt.wantTraceID)
		})
	}
}
