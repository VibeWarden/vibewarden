package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewUpstreamHealthChanged(t *testing.T) {
	tests := []struct {
		name          string
		params        events.UpstreamHealthChangedParams
		wantSummary   string
		wantLastError bool
	}{
		{
			name: "transition to healthy",
			params: events.UpstreamHealthChangedParams{
				PreviousStatus:   "unknown",
				NewStatus:        "healthy",
				ConsecutiveCount: 1,
				UpstreamURL:      "http://localhost:3000/health",
			},
			wantSummary:   "Upstream health changed from unknown to healthy after 1 consecutive probes",
			wantLastError: false,
		},
		{
			name: "transition to unhealthy with error",
			params: events.UpstreamHealthChangedParams{
				PreviousStatus:   "healthy",
				NewStatus:        "unhealthy",
				ConsecutiveCount: 3,
				UpstreamURL:      "http://localhost:3000/health",
				LastError:        "connection refused",
			},
			wantSummary:   "Upstream health changed from healthy to unhealthy after 3 consecutive probes",
			wantLastError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewUpstreamHealthChanged(tt.params)

			if ev.EventType != events.EventTypeUpstreamHealthChanged {
				t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeUpstreamHealthChanged)
			}
			if ev.AISummary == "" {
				t.Error("AISummary should not be empty")
			}
			payload := ev.Payload
			if payload["new_status"] != tt.params.NewStatus {
				t.Errorf("new_status = %v, want %v", payload["new_status"], tt.params.NewStatus)
			}
			_, hasLastError := payload["last_error"]
			if hasLastError != tt.wantLastError {
				t.Errorf("last_error present = %v, want %v", hasLastError, tt.wantLastError)
			}
		})
	}
}
