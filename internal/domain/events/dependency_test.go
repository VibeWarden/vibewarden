package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewAuthProviderUnavailable(t *testing.T) {
	tests := []struct {
		name           string
		params         events.AuthProviderUnavailableParams
		wantEventType  string
		wantSchemaVer  string
		wantSummaryMin string
	}{
		{
			name: "basic unavailable event",
			params: events.AuthProviderUnavailableParams{
				ProviderURL:  "http://127.0.0.1:4433",
				Error:        "connection refused",
				AffectedPath: "/dashboard",
			},
			wantEventType:  events.EventTypeAuthProviderUnavailable,
			wantSchemaVer:  events.SchemaVersion,
			wantSummaryMin: "http://127.0.0.1:4433",
		},
		{
			name: "empty affected path",
			params: events.AuthProviderUnavailableParams{
				ProviderURL: "http://127.0.0.1:4433",
				Error:       "timeout",
			},
			wantEventType: events.EventTypeAuthProviderUnavailable,
			wantSchemaVer: events.SchemaVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewAuthProviderUnavailable(tt.params)

			if ev.EventType != tt.wantEventType {
				t.Errorf("EventType = %q, want %q", ev.EventType, tt.wantEventType)
			}
			if ev.SchemaVersion != tt.wantSchemaVer {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, tt.wantSchemaVer)
			}
			if ev.Timestamp.IsZero() {
				t.Error("Timestamp is zero")
			}
			if ev.AISummary == "" {
				t.Error("AISummary is empty")
			}
			if tt.wantSummaryMin != "" && !contains(ev.AISummary, tt.wantSummaryMin) {
				t.Errorf("AISummary = %q, want to contain %q", ev.AISummary, tt.wantSummaryMin)
			}

			// Payload checks.
			if got, _ := ev.Payload["provider_url"].(string); got != tt.params.ProviderURL {
				t.Errorf("payload.provider_url = %q, want %q", got, tt.params.ProviderURL)
			}
			if got, _ := ev.Payload["error"].(string); got != tt.params.Error {
				t.Errorf("payload.error = %q, want %q", got, tt.params.Error)
			}
			if got, _ := ev.Payload["affected_path"].(string); got != tt.params.AffectedPath {
				t.Errorf("payload.affected_path = %q, want %q", got, tt.params.AffectedPath)
			}
		})
	}
}

func TestNewAuthProviderRecovered(t *testing.T) {
	params := events.AuthProviderRecoveredParams{
		ProviderURL: "http://127.0.0.1:4433",
	}

	ev := events.NewAuthProviderRecovered(params)

	if ev.EventType != events.EventTypeAuthProviderRecovered {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeAuthProviderRecovered)
	}
	if ev.SchemaVersion != events.SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
	}
	if ev.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if ev.AISummary == "" {
		t.Error("AISummary is empty")
	}
	if got, _ := ev.Payload["provider_url"].(string); got != params.ProviderURL {
		t.Errorf("payload.provider_url = %q, want %q", got, params.ProviderURL)
	}
}

func TestNewAuditLogFailure(t *testing.T) {
	tests := []struct {
		name   string
		params events.AuditLogFailureParams
	}{
		{
			name: "user created audit failure",
			params: events.AuditLogFailureParams{
				Action: "user.created",
				UserID: "user-123",
				Error:  "postgres: connection refused",
			},
		},
		{
			name: "user deactivated audit failure",
			params: events.AuditLogFailureParams{
				Action: "user.deactivated",
				UserID: "user-456",
				Error:  "context deadline exceeded",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewAuditLogFailure(tt.params)

			if ev.EventType != events.EventTypeAuditLogFailure {
				t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeAuditLogFailure)
			}
			if ev.SchemaVersion != events.SchemaVersion {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
			}
			if ev.Timestamp.IsZero() {
				t.Error("Timestamp is zero")
			}
			if ev.AISummary == "" {
				t.Error("AISummary is empty")
			}
			if got, _ := ev.Payload["action"].(string); got != tt.params.Action {
				t.Errorf("payload.action = %q, want %q", got, tt.params.Action)
			}
			if got, _ := ev.Payload["user_id"].(string); got != tt.params.UserID {
				t.Errorf("payload.user_id = %q, want %q", got, tt.params.UserID)
			}
			if got, _ := ev.Payload["error"].(string); got != tt.params.Error {
				t.Errorf("payload.error = %q, want %q", got, tt.params.Error)
			}
		})
	}
}

// TestEventTypeConstants verifies that the new event type constants are
// distinct strings and non-empty.
func TestEventTypeConstants_NewTypes(t *testing.T) {
	types := []string{
		events.EventTypeAuthProviderUnavailable,
		events.EventTypeAuthProviderRecovered,
		events.EventTypeAuditLogFailure,
	}

	seen := make(map[string]bool, len(types))
	for _, et := range types {
		if et == "" {
			t.Errorf("event type constant is empty string")
		}
		if seen[et] {
			t.Errorf("duplicate event type constant: %q", et)
		}
		seen[et] = true
	}
}

// contains is a simple substring check for test assertions.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && containsRune(s, sub)))
}

func containsRune(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
