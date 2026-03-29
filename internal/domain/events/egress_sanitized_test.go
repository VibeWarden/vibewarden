package events

import (
	"strings"
	"testing"
)

func TestNewEgressSanitized(t *testing.T) {
	tests := []struct {
		name               string
		params             EgressSanitizedParams
		wantEventType      string
		wantTotalInSummary string
		wantPayloadTotal   int
	}{
		{
			name: "all categories redacted",
			params: EgressSanitizedParams{
				Route:               "stripe",
				Method:              "POST",
				URL:                 "https://api.stripe.com/v1/charges",
				RedactedHeaders:     2,
				StrippedQueryParams: 1,
				RedactedBodyFields:  3,
				TraceID:             "abc123",
			},
			wantEventType:      EventTypeEgressSanitized,
			wantTotalInSummary: "6 field(s) redacted",
			wantPayloadTotal:   6,
		},
		{
			name: "only body fields redacted",
			params: EgressSanitizedParams{
				Route:              "webhook",
				Method:             "POST",
				URL:                "https://example.com/webhook",
				RedactedBodyFields: 1,
			},
			wantEventType:      EventTypeEgressSanitized,
			wantTotalInSummary: "1 field(s) redacted",
			wantPayloadTotal:   1,
		},
		{
			name: "nothing redacted",
			params: EgressSanitizedParams{
				Route:  "noop",
				Method: "GET",
				URL:    "https://example.com/api",
			},
			wantEventType:      EventTypeEgressSanitized,
			wantTotalInSummary: "0 field(s) redacted",
			wantPayloadTotal:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := NewEgressSanitized(tt.params)

			if ev.EventType != tt.wantEventType {
				t.Errorf("EventType = %q, want %q", ev.EventType, tt.wantEventType)
			}
			if ev.SchemaVersion != SchemaVersion {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, SchemaVersion)
			}
			if !strings.Contains(ev.AISummary, tt.wantTotalInSummary) {
				t.Errorf("AISummary %q does not contain %q", ev.AISummary, tt.wantTotalInSummary)
			}
			if got := ev.Payload["total_redacted"]; got != tt.wantPayloadTotal {
				t.Errorf("Payload[total_redacted] = %v, want %d", got, tt.wantPayloadTotal)
			}
			if got := ev.Payload["route"]; got != tt.params.Route {
				t.Errorf("Payload[route] = %v, want %q", got, tt.params.Route)
			}
		})
	}
}
