package events

import (
	"strings"
	"testing"
)

func TestNewUpstreamRetry(t *testing.T) {
	tests := []struct {
		name                string
		params              UpstreamRetryParams
		wantEventType       string
		wantSummaryContains []string
		wantPayloadKeys     []string
	}{
		{
			name: "first retry after 503",
			params: UpstreamRetryParams{
				Method:     "GET",
				Path:       "/api/users",
				Attempt:    1,
				StatusCode: 503,
				ClientIP:   "10.0.0.1",
			},
			wantEventType:       EventTypeUpstreamRetry,
			wantSummaryContains: []string{"GET", "/api/users", "1", "503", "10.0.0.1"},
			wantPayloadKeys:     []string{"method", "path", "attempt", "status_code", "client_ip"},
		},
		{
			name: "second retry after 502",
			params: UpstreamRetryParams{
				Method:     "PUT",
				Path:       "/api/items/42",
				Attempt:    2,
				StatusCode: 502,
				ClientIP:   "192.168.1.5",
			},
			wantEventType:       EventTypeUpstreamRetry,
			wantSummaryContains: []string{"PUT", "/api/items/42", "2", "502"},
			wantPayloadKeys:     []string{"method", "path", "attempt", "status_code", "client_ip"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := NewUpstreamRetry(tt.params)

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
			if ev.Payload["attempt"] != tt.params.Attempt {
				t.Errorf("Payload[attempt] = %v, want %d", ev.Payload["attempt"], tt.params.Attempt)
			}
			if ev.Payload["status_code"] != tt.params.StatusCode {
				t.Errorf("Payload[status_code] = %v, want %d", ev.Payload["status_code"], tt.params.StatusCode)
			}
			if ev.Payload["client_ip"] != tt.params.ClientIP {
				t.Errorf("Payload[client_ip] = %v, want %q", ev.Payload["client_ip"], tt.params.ClientIP)
			}
		})
	}
}

func TestEventTypeUpstreamRetryConstant(t *testing.T) {
	if EventTypeUpstreamRetry != "upstream.retry" {
		t.Errorf("EventTypeUpstreamRetry = %q, want %q", EventTypeUpstreamRetry, "upstream.retry")
	}
}
