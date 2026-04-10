package events

import (
	"strings"
	"testing"
)

func TestNewUpstreamTimeout(t *testing.T) {
	tests := []struct {
		name                string
		params              UpstreamTimeoutParams
		wantEventType       string
		wantSummaryContains []string
		wantPayloadKeys     []string
	}{
		{
			name: "basic GET request",
			params: UpstreamTimeoutParams{
				Method:         "GET",
				Path:           "/api/users",
				TimeoutSeconds: 30,
				ClientIP:       "10.0.0.1",
			},
			wantEventType:       EventTypeUpstreamTimeout,
			wantSummaryContains: []string{"GET", "/api/users", "10.0.0.1", "30"},
			wantPayloadKeys:     []string{"method", "path", "timeout_seconds", "client_ip"},
		},
		{
			name: "POST request with fractional timeout",
			params: UpstreamTimeoutParams{
				Method:         "POST",
				Path:           "/api/items",
				TimeoutSeconds: 5.5,
				ClientIP:       "192.168.1.100",
			},
			wantEventType:       EventTypeUpstreamTimeout,
			wantSummaryContains: []string{"POST", "/api/items"},
			wantPayloadKeys:     []string{"method", "path", "timeout_seconds", "client_ip"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := NewUpstreamTimeout(tt.params)

			if ev.SchemaVersion != SchemaVersion {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, SchemaVersion)
			}
			if ev.EventType != tt.wantEventType {
				t.Errorf("EventType = %q, want %q", ev.EventType, tt.wantEventType)
			}
			if ev.Timestamp.IsZero() {
				t.Error("Timestamp is zero")
			}

			for _, want := range tt.wantSummaryContains {
				if !strings.Contains(ev.AISummary, want) {
					t.Errorf("AISummary = %q, want it to contain %q", ev.AISummary, want)
				}
			}

			for _, key := range tt.wantPayloadKeys {
				if _, ok := ev.Payload[key]; !ok {
					t.Errorf("Payload missing key %q", key)
				}
			}

			if ev.Payload["method"] != tt.params.Method {
				t.Errorf("Payload[method] = %v, want %q", ev.Payload["method"], tt.params.Method)
			}
			if ev.Payload["path"] != tt.params.Path {
				t.Errorf("Payload[path] = %v, want %q", ev.Payload["path"], tt.params.Path)
			}
			if ev.Payload["client_ip"] != tt.params.ClientIP {
				t.Errorf("Payload[client_ip] = %v, want %q", ev.Payload["client_ip"], tt.params.ClientIP)
			}
		})
	}
}

func TestEventTypeUpstreamTimeoutConstant(t *testing.T) {
	if EventTypeUpstreamTimeout != "upstream.timeout" {
		t.Errorf("EventTypeUpstreamTimeout = %q, want %q", EventTypeUpstreamTimeout, "upstream.timeout")
	}
}
