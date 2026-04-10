package events_test

import (
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewIPFilterBlocked(t *testing.T) {
	tests := []struct {
		name           string
		params         events.IPFilterBlockedParams
		wantEventType  string
		wantSchema     string
		wantSummary    string
		wantPayloadKey string
	}{
		{
			name: "blocklist mode",
			params: events.IPFilterBlockedParams{
				ClientIP: "10.1.2.3",
				Mode:     "blocklist",
				Method:   "GET",
				Path:     "/api/data",
			},
			wantEventType:  events.EventTypeIPFilterBlocked,
			wantSchema:     events.SchemaVersion,
			wantSummary:    "10.1.2.3",
			wantPayloadKey: "client_ip",
		},
		{
			name: "allowlist mode",
			params: events.IPFilterBlockedParams{
				ClientIP: "203.0.113.5",
				Mode:     "allowlist",
				Method:   "POST",
				Path:     "/submit",
			},
			wantEventType:  events.EventTypeIPFilterBlocked,
			wantSchema:     events.SchemaVersion,
			wantSummary:    "allowlist",
			wantPayloadKey: "mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now().UTC()
			ev := events.NewIPFilterBlocked(tt.params)
			after := time.Now().UTC()

			if ev.EventType != tt.wantEventType {
				t.Errorf("EventType = %q, want %q", ev.EventType, tt.wantEventType)
			}
			if ev.SchemaVersion != tt.wantSchema {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, tt.wantSchema)
			}
			if !strings.Contains(ev.AISummary, tt.wantSummary) {
				t.Errorf("AISummary = %q, want it to contain %q", ev.AISummary, tt.wantSummary)
			}
			if ev.Timestamp.Before(before) || ev.Timestamp.After(after) {
				t.Errorf("Timestamp %v not in expected range [%v, %v]", ev.Timestamp, before, after)
			}
			if _, ok := ev.Payload[tt.wantPayloadKey]; !ok {
				t.Errorf("Payload missing key %q; got %v", tt.wantPayloadKey, ev.Payload)
			}

			// Verify all core payload fields are present.
			for _, key := range []string{"client_ip", "mode", "method", "path"} {
				if _, ok := ev.Payload[key]; !ok {
					t.Errorf("Payload missing key %q", key)
				}
			}

			if ev.Payload["client_ip"] != tt.params.ClientIP {
				t.Errorf("Payload[client_ip] = %v, want %q", ev.Payload["client_ip"], tt.params.ClientIP)
			}
			if ev.Payload["mode"] != tt.params.Mode {
				t.Errorf("Payload[mode] = %v, want %q", ev.Payload["mode"], tt.params.Mode)
			}
			if ev.Payload["method"] != tt.params.Method {
				t.Errorf("Payload[method] = %v, want %q", ev.Payload["method"], tt.params.Method)
			}
			if ev.Payload["path"] != tt.params.Path {
				t.Errorf("Payload[path] = %v, want %q", ev.Payload["path"], tt.params.Path)
			}
		})
	}
}

func TestEventTypeIPFilterBlocked_Value(t *testing.T) {
	if events.EventTypeIPFilterBlocked != "ip_filter.blocked" {
		t.Errorf("EventTypeIPFilterBlocked = %q, want %q", events.EventTypeIPFilterBlocked, "ip_filter.blocked")
	}
}
