package log_test

import (
	"context"
	"errors"
	"testing"
	"time"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeSink is a simple ports.EventLogger that records every event it receives.
type fakeSink struct {
	logged []events.Event
	err    error
}

func (f *fakeSink) Log(_ context.Context, event events.Event) error {
	f.logged = append(f.logged, event)
	return f.err
}

func TestMultiEventLogger_AllSinksReceiveEvent(t *testing.T) {
	s1 := &fakeSink{}
	s2 := &fakeSink{}

	ml := logadapter.NewMultiEventLogger(s1, s2)

	ev := events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     events.EventTypeAuthSuccess,
		Timestamp:     time.Now(),
		AISummary:     "test",
	}

	if err := ml.Log(context.Background(), ev); err != nil {
		t.Fatalf("Log returned unexpected error: %v", err)
	}

	if len(s1.logged) != 1 {
		t.Errorf("s1 logged %d events, want 1", len(s1.logged))
	}
	if len(s2.logged) != 1 {
		t.Errorf("s2 logged %d events, want 1", len(s2.logged))
	}
}

// TestMultiEventLogger_SinkErrorSwallowed verifies that an error from one sink
// does not prevent the other sinks from receiving the event, and that Log
// always returns nil.
func TestMultiEventLogger_SinkErrorSwallowed(t *testing.T) {
	s1 := &fakeSink{err: errors.New("disk full")}
	s2 := &fakeSink{}

	ml := logadapter.NewMultiEventLogger(s1, s2)

	if err := ml.Log(context.Background(), makeEvent(events.EventTypeAuthFailed)); err != nil {
		t.Fatalf("Log returned error %v, want nil", err)
	}

	// s1 still recorded the event even though it returned an error.
	if len(s1.logged) != 1 {
		t.Errorf("s1 logged %d events, want 1", len(s1.logged))
	}
	// s2 still received the event despite s1 failing.
	if len(s2.logged) != 1 {
		t.Errorf("s2 logged %d events, want 1", len(s2.logged))
	}
}

// TestMultiEventLogger_ZeroSinks verifies that a MultiEventLogger with no sinks
// is a no-op and does not panic.
func TestMultiEventLogger_ZeroSinks(t *testing.T) {
	ml := logadapter.NewMultiEventLogger()

	if err := ml.Log(context.Background(), makeEvent(events.EventTypeProxyStarted)); err != nil {
		t.Fatalf("Log returned error %v, want nil", err)
	}
}

// TestMultiEventLogger_ImplementsPort verifies the compile-time assertion.
func TestMultiEventLogger_ImplementsPort(t *testing.T) {
	var _ ports.EventLogger = logadapter.NewMultiEventLogger()
}
