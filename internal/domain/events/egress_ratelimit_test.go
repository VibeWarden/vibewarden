package events_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// TestNewEgressRateLimitHit verifies that NewEgressRateLimitHit produces a
// well-formed event with the correct type, schema version, and payload fields.
func TestNewEgressRateLimitHit(t *testing.T) {
	params := events.EgressRateLimitHitParams{
		Route:             "payments",
		Limit:             100.0,
		RetryAfterSeconds: 1.0,
	}
	before := time.Now()
	ev := events.NewEgressRateLimitHit(params)
	after := time.Now()

	if ev.EventType != events.EventTypeEgressRateLimitHit {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeEgressRateLimitHit)
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
	if got, ok := ev.Payload["route"].(string); !ok || got != "payments" {
		t.Errorf("Payload[route] = %v, want %q", ev.Payload["route"], "payments")
	}
	if got, ok := ev.Payload["limit"].(float64); !ok || got != 100.0 {
		t.Errorf("Payload[limit] = %v, want 100.0", ev.Payload["limit"])
	}
	if got, ok := ev.Payload["retry_after_seconds"].(float64); !ok || got != 1.0 {
		t.Errorf("Payload[retry_after_seconds] = %v, want 1.0", ev.Payload["retry_after_seconds"])
	}
}

// TestNewEgressRateLimitHit_AISummaryContainsRoute verifies that the AI summary
// includes the route name and the limit for quick at-a-glance diagnosis.
func TestNewEgressRateLimitHit_AISummaryContainsRoute(t *testing.T) {
	ev := events.NewEgressRateLimitHit(events.EgressRateLimitHitParams{
		Route:             "stripe-api",
		Limit:             50.0,
		RetryAfterSeconds: 2.0,
	})
	for _, want := range []string{"stripe-api", "50"} {
		found := false
		for i := 0; i < len(ev.AISummary)-len(want)+1; i++ {
			if ev.AISummary[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AISummary %q does not contain %q", ev.AISummary, want)
		}
	}
}
