package audit_test

import (
	"context"
	"errors"
	"testing"

	auditadapter "github.com/vibewarden/vibewarden/internal/adapters/audit"
	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeWriter is a test double for ports.AuditEventLogger that records
// received events and optionally returns a configured error.
type fakeWriter struct {
	events []audit.AuditEvent
	err    error
}

func (f *fakeWriter) Log(_ context.Context, event audit.AuditEvent) error {
	f.events = append(f.events, event)
	return f.err
}

// Compile-time check: fakeWriter implements ports.AuditEventLogger.
var _ ports.AuditEventLogger = (*fakeWriter)(nil)

// TestMultiWriter_FansOutToAll verifies that every underlying writer receives
// the event when all writers succeed.
func TestMultiWriter_FansOutToAll(t *testing.T) {
	w1 := &fakeWriter{}
	w2 := &fakeWriter{}
	mw := auditadapter.NewMultiWriter(w1, w2)

	ev := fixedEvent(t, audit.EventTypeAuthSuccess, audit.OutcomeSuccess)
	if err := mw.Log(context.Background(), ev); err != nil {
		t.Fatalf("MultiWriter.Log: %v", err)
	}

	if len(w1.events) != 1 {
		t.Errorf("w1 received %d events, want 1", len(w1.events))
	}
	if len(w2.events) != 1 {
		t.Errorf("w2 received %d events, want 1", len(w2.events))
	}
}

// TestMultiWriter_AllWritersReceiveEvenOnError verifies that a failure in one
// writer does not prevent delivery to remaining writers.
func TestMultiWriter_AllWritersReceiveEvenOnError(t *testing.T) {
	sentinelErr := errors.New("sink unavailable")
	w1 := &fakeWriter{err: sentinelErr}
	w2 := &fakeWriter{}
	mw := auditadapter.NewMultiWriter(w1, w2)

	ev := fixedEvent(t, audit.EventTypeRateLimitHit, audit.OutcomeFailure)
	err := mw.Log(context.Background(), ev)

	// Error must be non-nil because w1 failed.
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("error = %v, want to contain %v", err, sentinelErr)
	}

	// w2 must have received the event despite w1 failing.
	if len(w2.events) != 1 {
		t.Errorf("w2 received %d events, want 1 (should receive despite w1 error)", len(w2.events))
	}
}

// TestMultiWriter_BothWritersFail verifies that errors from all writers are
// joined and returned.
func TestMultiWriter_BothWritersFail(t *testing.T) {
	err1 := errors.New("sink1 error")
	err2 := errors.New("sink2 error")
	w1 := &fakeWriter{err: err1}
	w2 := &fakeWriter{err: err2}
	mw := auditadapter.NewMultiWriter(w1, w2)

	ev := fixedEvent(t, audit.EventTypeIPFilterBlocked, audit.OutcomeFailure)
	err := mw.Log(context.Background(), ev)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, err1) {
		t.Errorf("joined error does not contain err1: %v", err)
	}
	if !errors.Is(err, err2) {
		t.Errorf("joined error does not contain err2: %v", err)
	}
}

// TestMultiWriter_ZeroWriters verifies that a MultiWriter with no underlying
// writers is a no-op that returns nil.
func TestMultiWriter_ZeroWriters(t *testing.T) {
	mw := auditadapter.NewMultiWriter()

	ev := fixedEvent(t, audit.EventTypeAdminUserCreated, audit.OutcomeSuccess)
	if err := mw.Log(context.Background(), ev); err != nil {
		t.Errorf("empty MultiWriter.Log returned unexpected error: %v", err)
	}
}

// TestMultiWriter_SingleWriter verifies correct pass-through with one writer.
func TestMultiWriter_SingleWriter(t *testing.T) {
	w := &fakeWriter{}
	mw := auditadapter.NewMultiWriter(w)

	ev := fixedEvent(t, audit.EventTypeCircuitBreakerOpened, audit.OutcomeFailure)
	if err := mw.Log(context.Background(), ev); err != nil {
		t.Fatalf("MultiWriter.Log: %v", err)
	}

	if len(w.events) != 1 {
		t.Errorf("writer received %d events, want 1", len(w.events))
	}
	if w.events[0].EventType != audit.EventTypeCircuitBreakerOpened {
		t.Errorf("event_type = %q, want %q", w.events[0].EventType, audit.EventTypeCircuitBreakerOpened)
	}
}

// TestMultiWriter_MultipleEvents verifies cumulative delivery of several events.
func TestMultiWriter_MultipleEvents(t *testing.T) {
	w1 := &fakeWriter{}
	w2 := &fakeWriter{}
	mw := auditadapter.NewMultiWriter(w1, w2)

	eventTypes := []audit.EventType{
		audit.EventTypeAuthSuccess,
		audit.EventTypeRateLimitHit,
		audit.EventTypeAdminAPIKeyCreated,
	}

	for _, et := range eventTypes {
		ev := fixedEvent(t, et, audit.OutcomeSuccess)
		if err := mw.Log(context.Background(), ev); err != nil {
			t.Fatalf("Log(%q): %v", et, err)
		}
	}

	if got := len(w1.events); got != len(eventTypes) {
		t.Errorf("w1 received %d events, want %d", got, len(eventTypes))
	}
	if got := len(w2.events); got != len(eventTypes) {
		t.Errorf("w2 received %d events, want %d", got, len(eventTypes))
	}
}
