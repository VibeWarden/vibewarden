package events

import (
	"testing"
	"time"
)

func TestNewMaintenanceRequestBlocked(t *testing.T) {
	tests := []struct {
		name    string
		params  MaintenanceRequestBlockedParams
		wantAI  string
		wantKey string
		wantVal string
	}{
		{
			name: "GET request blocked",
			params: MaintenanceRequestBlockedParams{
				Path:    "/api/orders",
				Method:  "GET",
				Message: "Scheduled maintenance until 03:00 UTC",
			},
			wantAI:  "Request blocked by maintenance mode: GET /api/orders",
			wantKey: "message",
			wantVal: "Scheduled maintenance until 03:00 UTC",
		},
		{
			name: "POST request blocked with default message",
			params: MaintenanceRequestBlockedParams{
				Path:    "/api/checkout",
				Method:  "POST",
				Message: "Service is under maintenance",
			},
			wantAI:  "Request blocked by maintenance mode: POST /api/checkout",
			wantKey: "message",
			wantVal: "Service is under maintenance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now().UTC()
			ev := NewMaintenanceRequestBlocked(tt.params)
			after := time.Now().UTC()

			if ev.SchemaVersion != SchemaVersion {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, SchemaVersion)
			}
			if ev.EventType != EventTypeMaintenanceRequestBlocked {
				t.Errorf("EventType = %q, want %q", ev.EventType, EventTypeMaintenanceRequestBlocked)
			}
			if ev.Timestamp.Before(before) || ev.Timestamp.After(after) {
				t.Errorf("Timestamp %v not in [%v, %v]", ev.Timestamp, before, after)
			}
			if ev.AISummary != tt.wantAI {
				t.Errorf("AISummary = %q, want %q", ev.AISummary, tt.wantAI)
			}
			if ev.Payload["path"] != tt.params.Path {
				t.Errorf("Payload[path] = %v, want %q", ev.Payload["path"], tt.params.Path)
			}
			if ev.Payload["method"] != tt.params.Method {
				t.Errorf("Payload[method] = %v, want %q", ev.Payload["method"], tt.params.Method)
			}
			if ev.Payload[tt.wantKey] != tt.wantVal {
				t.Errorf("Payload[%s] = %v, want %q", tt.wantKey, ev.Payload[tt.wantKey], tt.wantVal)
			}
		})
	}
}
