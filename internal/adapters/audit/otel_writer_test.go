package audit_test

import (
	"context"
	"log/slog"
	"testing"

	auditadapter "github.com/vibewarden/vibewarden/internal/adapters/audit"
	"github.com/vibewarden/vibewarden/internal/domain/audit"
)

// captureHandler is an slog.Handler that records every Handle call for
// inspection in tests.
type captureHandler struct {
	records []slog.Record
}

func (c *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (c *captureHandler) Handle(_ context.Context, r slog.Record) error {
	c.records = append(c.records, r.Clone())
	return nil
}
func (c *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return c }
func (c *captureHandler) WithGroup(_ string) slog.Handler      { return c }

// TestOTelWriter_Log_RecordReceived verifies that Log forwards exactly one slog
// record to the underlying handler.
func TestOTelWriter_Log_RecordReceived(t *testing.T) {
	h := &captureHandler{}
	w := auditadapter.NewOTelWriter(h)

	ev := fixedEvent(t, audit.EventTypeAuthSuccess, audit.OutcomeSuccess)
	if err := w.Log(context.Background(), ev); err != nil {
		t.Fatalf("OTelWriter.Log: %v", err)
	}

	if len(h.records) != 1 {
		t.Fatalf("handler received %d records, want 1", len(h.records))
	}
}

// TestOTelWriter_Log_MessageIsAuditEvent verifies that the slog message is the
// expected sentinel string so consumers can filter on it.
func TestOTelWriter_Log_MessageIsAuditEvent(t *testing.T) {
	h := &captureHandler{}
	w := auditadapter.NewOTelWriter(h)

	ev := fixedEvent(t, audit.EventTypeRateLimitHit, audit.OutcomeFailure)
	if err := w.Log(context.Background(), ev); err != nil {
		t.Fatalf("OTelWriter.Log: %v", err)
	}

	if got := h.records[0].Message; got != "audit_event" {
		t.Errorf("message = %q, want %q", got, "audit_event")
	}
}

// TestOTelWriter_Log_LevelIsInfo verifies that audit records use LevelInfo
// so the OTel bridge maps them to INFO severity.
func TestOTelWriter_Log_LevelIsInfo(t *testing.T) {
	h := &captureHandler{}
	w := auditadapter.NewOTelWriter(h)

	ev := fixedEvent(t, audit.EventTypeIPFilterBlocked, audit.OutcomeFailure)
	if err := w.Log(context.Background(), ev); err != nil {
		t.Fatalf("OTelWriter.Log: %v", err)
	}

	if got := h.records[0].Level; got != slog.LevelInfo {
		t.Errorf("level = %v, want %v", got, slog.LevelInfo)
	}
}

// TestOTelWriter_Log_ContainsEventType verifies that the audit.event_type
// attribute is present in the emitted record.
func TestOTelWriter_Log_ContainsEventType(t *testing.T) {
	h := &captureHandler{}
	w := auditadapter.NewOTelWriter(h)

	ev := fixedEvent(t, audit.EventTypeAdminAPIKeyRevoked, audit.OutcomeSuccess)
	if err := w.Log(context.Background(), ev); err != nil {
		t.Fatalf("OTelWriter.Log: %v", err)
	}

	rec := h.records[0]
	found := false
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == "audit" {
			// The "audit" group is emitted as a single grouped attr.
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("expected \"audit\" group attribute in slog record, not found")
	}
}

// TestOTelWriter_Log_MultipleEvents verifies that each Log call produces one
// record in the handler.
func TestOTelWriter_Log_MultipleEvents(t *testing.T) {
	tests := []struct {
		name      string
		eventType audit.EventType
		outcome   audit.Outcome
	}{
		{"auth success", audit.EventTypeAuthSuccess, audit.OutcomeSuccess},
		{"auth failure", audit.EventTypeAuthFailure, audit.OutcomeFailure},
		{"rate limit hit", audit.EventTypeRateLimitHit, audit.OutcomeFailure},
		{"ip filter blocked", audit.EventTypeIPFilterBlocked, audit.OutcomeFailure},
		{"circuit breaker opened", audit.EventTypeCircuitBreakerOpened, audit.OutcomeFailure},
		{"admin user created", audit.EventTypeAdminUserCreated, audit.OutcomeSuccess},
	}

	h := &captureHandler{}
	w := auditadapter.NewOTelWriter(h)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := fixedEvent(t, tt.eventType, tt.outcome)
			if err := w.Log(context.Background(), ev); err != nil {
				t.Fatalf("OTelWriter.Log: %v", err)
			}
		})
	}

	if got := len(h.records); got != len(tests) {
		t.Errorf("handler received %d records, want %d", got, len(tests))
	}
}

// TestOTelWriter_NilHandler verifies that NewOTelWriter(nil) does not panic
// and that Log returns nil (no-op, events silently discarded).
func TestOTelWriter_NilHandler(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewOTelWriter(nil) panicked: %v", r)
		}
	}()

	w := auditadapter.NewOTelWriter(nil)
	ev := fixedEvent(t, audit.EventTypeAuthSuccess, audit.OutcomeSuccess)
	if err := w.Log(context.Background(), ev); err != nil {
		t.Errorf("Log with nil handler returned unexpected error: %v", err)
	}
}
