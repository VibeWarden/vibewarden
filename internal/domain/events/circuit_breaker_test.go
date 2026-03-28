package events_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewCircuitBreakerOpened(t *testing.T) {
	params := events.CircuitBreakerOpenedParams{
		Threshold:      5,
		TimeoutSeconds: 60,
	}
	before := time.Now()
	ev := events.NewCircuitBreakerOpened(params)
	after := time.Now()

	if ev.EventType != events.EventTypeCircuitBreakerOpened {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeCircuitBreakerOpened)
	}
	if ev.SchemaVersion != events.SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
	}
	if ev.Timestamp.Before(before) || ev.Timestamp.After(after) {
		t.Errorf("Timestamp %v not in expected range [%v, %v]", ev.Timestamp, before, after)
	}
	if ev.AISummary == "" {
		t.Error("AISummary must not be empty")
	}
	if ev.Payload["threshold"] != 5 {
		t.Errorf("Payload[threshold] = %v, want 5", ev.Payload["threshold"])
	}
	if ev.Payload["timeout_seconds"] != float64(60) {
		t.Errorf("Payload[timeout_seconds] = %v, want 60", ev.Payload["timeout_seconds"])
	}
}

func TestNewCircuitBreakerHalfOpen(t *testing.T) {
	params := events.CircuitBreakerHalfOpenParams{
		TimeoutSeconds: 30,
	}
	ev := events.NewCircuitBreakerHalfOpen(params)

	if ev.EventType != events.EventTypeCircuitBreakerHalfOpen {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeCircuitBreakerHalfOpen)
	}
	if ev.AISummary == "" {
		t.Error("AISummary must not be empty")
	}
	if ev.Payload["timeout_seconds"] != float64(30) {
		t.Errorf("Payload[timeout_seconds] = %v, want 30", ev.Payload["timeout_seconds"])
	}
}

func TestNewCircuitBreakerClosed(t *testing.T) {
	ev := events.NewCircuitBreakerClosed()

	if ev.EventType != events.EventTypeCircuitBreakerClosed {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeCircuitBreakerClosed)
	}
	if ev.AISummary == "" {
		t.Error("AISummary must not be empty")
	}
}
