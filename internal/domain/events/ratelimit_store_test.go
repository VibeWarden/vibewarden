package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewRateLimitStoreFallback(t *testing.T) {
	tests := []struct {
		name   string
		reason string
	}{
		{"redis connection refused", "dial tcp: connection refused"},
		{"empty reason", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewRateLimitStoreFallback(events.RateLimitStoreFallbackParams{
				Reason: tt.reason,
			})

			if e.SchemaVersion != events.SchemaVersion {
				t.Errorf("SchemaVersion = %q, want %q", e.SchemaVersion, events.SchemaVersion)
			}
			if e.EventType != events.EventTypeRateLimitStoreFallback {
				t.Errorf("EventType = %q, want %q", e.EventType, events.EventTypeRateLimitStoreFallback)
			}
			if e.AISummary == "" {
				t.Error("AISummary must not be empty")
			}
			if e.Timestamp.IsZero() {
				t.Error("Timestamp must not be zero")
			}
			if store, ok := e.Payload["store"].(string); !ok || store != "memory" {
				t.Errorf("Payload store = %v, want %q", e.Payload["store"], "memory")
			}
		})
	}
}

func TestNewRateLimitStoreRecovered(t *testing.T) {
	e := events.NewRateLimitStoreRecovered()

	if e.SchemaVersion != events.SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", e.SchemaVersion, events.SchemaVersion)
	}
	if e.EventType != events.EventTypeRateLimitStoreRecovered {
		t.Errorf("EventType = %q, want %q", e.EventType, events.EventTypeRateLimitStoreRecovered)
	}
	if e.AISummary == "" {
		t.Error("AISummary must not be empty")
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp must not be zero")
	}
	if store, ok := e.Payload["store"].(string); !ok || store != "redis" {
		t.Errorf("Payload store = %v, want %q", e.Payload["store"], "redis")
	}
}
