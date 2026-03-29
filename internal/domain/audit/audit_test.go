package audit_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
)

func TestNewAuditEvent_Valid(t *testing.T) {
	tests := []struct {
		name      string
		eventType audit.EventType
		actor     audit.Actor
		target    audit.Target
		outcome   audit.Outcome
		traceID   string
		details   map[string]any
	}{
		{
			name:      "auth success with all fields",
			eventType: audit.EventTypeAuthSuccess,
			actor: audit.Actor{
				IP:     "192.168.1.1",
				UserID: "user-abc123",
			},
			target: audit.Target{
				Path:     "/api/orders",
				Resource: "orders",
			},
			outcome: audit.OutcomeSuccess,
			traceID: "trace-xyz",
			details: map[string]any{"session_id": "sess-001"},
		},
		{
			name:      "rate limit hit with nil details becomes empty map",
			eventType: audit.EventTypeRateLimitHit,
			actor:     audit.Actor{IP: "10.0.0.5"},
			target:    audit.Target{Path: "/api/data"},
			outcome:   audit.OutcomeFailure,
			traceID:   "",
			details:   nil,
		},
		{
			name:      "ip filter blocked with api key actor",
			eventType: audit.EventTypeIPFilterBlocked,
			actor: audit.Actor{
				IP:         "203.0.113.42",
				APIKeyName: "ci-deploy",
			},
			target:  audit.Target{Path: "/deploy"},
			outcome: audit.OutcomeFailure,
			traceID: "trace-999",
			details: map[string]any{"mode": "blocklist"},
		},
		{
			name:      "admin user created with empty trace id",
			eventType: audit.EventTypeAdminUserCreated,
			actor:     audit.Actor{UserID: "admin-001"},
			target:    audit.Target{Resource: "user:new-user-001"},
			outcome:   audit.OutcomeSuccess,
			traceID:   "",
			details:   map[string]any{"email": "newuser@example.com"},
		},
		{
			name:      "circuit breaker opened",
			eventType: audit.EventTypeCircuitBreakerOpened,
			actor:     audit.Actor{},
			target:    audit.Target{},
			outcome:   audit.OutcomeFailure,
			traceID:   "",
			details:   map[string]any{"threshold": 5},
		},
		{
			name:      "waf detection in detect mode",
			eventType: audit.EventTypeWAFDetection,
			actor:     audit.Actor{IP: "10.0.0.1"},
			target:    audit.Target{Path: "/api/search"},
			outcome:   audit.OutcomeSuccess,
			traceID:   "trace-waf-1",
			details: map[string]any{
				"rule":     "sqli-union-select",
				"category": "sqli",
				"mode":     "detect",
			},
		},
		{
			name:      "waf blocked in block mode",
			eventType: audit.EventTypeWAFBlocked,
			actor:     audit.Actor{IP: "10.0.0.2"},
			target:    audit.Target{Path: "/api/data"},
			outcome:   audit.OutcomeFailure,
			traceID:   "trace-waf-2",
			details: map[string]any{
				"rule":     "xss-script-tag",
				"category": "xss",
				"mode":     "block",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now().UTC()
			ev, err := audit.NewAuditEvent(tt.eventType, tt.actor, tt.target, tt.outcome, tt.traceID, tt.details)
			after := time.Now().UTC()

			if err != nil {
				t.Fatalf("NewAuditEvent() unexpected error: %v", err)
			}

			if ev.EventType != tt.eventType {
				t.Errorf("EventType = %q, want %q", ev.EventType, tt.eventType)
			}
			if ev.Actor != tt.actor {
				t.Errorf("Actor = %+v, want %+v", ev.Actor, tt.actor)
			}
			if ev.Target != tt.target {
				t.Errorf("Target = %+v, want %+v", ev.Target, tt.target)
			}
			if ev.Outcome != tt.outcome {
				t.Errorf("Outcome = %q, want %q", ev.Outcome, tt.outcome)
			}
			if ev.TraceID != tt.traceID {
				t.Errorf("TraceID = %q, want %q", ev.TraceID, tt.traceID)
			}

			if ev.Timestamp.IsZero() {
				t.Error("Timestamp is zero")
			}
			if ev.Timestamp.Location() != time.UTC {
				t.Errorf("Timestamp location = %v, want UTC", ev.Timestamp.Location())
			}
			if ev.Timestamp.Before(before) || ev.Timestamp.After(after) {
				t.Errorf("Timestamp %v is outside [%v, %v]", ev.Timestamp, before, after)
			}

			if ev.Details == nil {
				t.Error("Details must not be nil")
			}
		})
	}
}

func TestNewAuditEvent_Invalid(t *testing.T) {
	tests := []struct {
		name      string
		eventType audit.EventType
		outcome   audit.Outcome
		wantErr   string
	}{
		{
			name:      "empty event type",
			eventType: "",
			outcome:   audit.OutcomeSuccess,
			wantErr:   "audit event type cannot be empty",
		},
		{
			name:      "empty outcome",
			eventType: audit.EventTypeAuthSuccess,
			outcome:   "",
			wantErr:   "audit event outcome cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := audit.NewAuditEvent(tt.eventType, audit.Actor{}, audit.Target{}, tt.outcome, "", nil)
			if err == nil {
				t.Fatal("NewAuditEvent() expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestOutcomeConstants(t *testing.T) {
	tests := []struct {
		outcome audit.Outcome
		want    string
	}{
		{audit.OutcomeSuccess, "success"},
		{audit.OutcomeFailure, "failure"},
	}

	for _, tt := range tests {
		t.Run(string(tt.outcome), func(t *testing.T) {
			if string(tt.outcome) != tt.want {
				t.Errorf("Outcome = %q, want %q", tt.outcome, tt.want)
			}
		})
	}
}

func TestEventTypeConstants(t *testing.T) {
	// Verify that every event type constant has the "audit." prefix and is non-empty.
	// This guards against accidental blank values or missing prefixes.
	types := []audit.EventType{
		audit.EventTypeAuthSuccess,
		audit.EventTypeAuthFailure,
		audit.EventTypeAuthAPIKeySuccess,
		audit.EventTypeAuthAPIKeyFailure,
		audit.EventTypeAuthAPIKeyForbidden,
		audit.EventTypeRateLimitHit,
		audit.EventTypeRateLimitUnidentified,
		audit.EventTypeIPFilterBlocked,
		audit.EventTypeCircuitBreakerOpened,
		audit.EventTypeCircuitBreakerHalfOpen,
		audit.EventTypeCircuitBreakerClosed,
		audit.EventTypeAdminUserCreated,
		audit.EventTypeAdminUserDeactivated,
		audit.EventTypeAdminUserDeleted,
		audit.EventTypeAdminAPIKeyCreated,
		audit.EventTypeAdminAPIKeyRevoked,
	}

	seen := make(map[audit.EventType]bool)
	for _, et := range types {
		t.Run(string(et), func(t *testing.T) {
			if et == "" {
				t.Error("EventType is empty")
			}
			if len(string(et)) < len("audit.") || string(et)[:len("audit.")] != "audit." {
				t.Errorf("EventType %q does not start with \"audit.\"", et)
			}
			if seen[et] {
				t.Errorf("EventType %q is duplicated", et)
			}
			seen[et] = true
		})
	}
}

func TestNilDetailsBecomesEmptyMap(t *testing.T) {
	ev, err := audit.NewAuditEvent(
		audit.EventTypeAuthFailure,
		audit.Actor{},
		audit.Target{},
		audit.OutcomeFailure,
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Details == nil {
		t.Error("Details must not be nil when nil was passed")
	}
	if len(ev.Details) != 0 {
		t.Errorf("Details len = %d, want 0", len(ev.Details))
	}
}
