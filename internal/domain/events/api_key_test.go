package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewAPIKeySuccess(t *testing.T) {
	ev := events.NewAPIKeySuccess(events.APIKeySuccessParams{
		KeyName: "my-service",
		Method:  "GET",
		Path:    "/api/v1/data",
		Scopes:  []string{"read"},
	})

	if ev.EventType != events.EventTypeAPIKeySuccess {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeAPIKeySuccess)
	}
	if ev.Payload["key_name"] != "my-service" {
		t.Errorf("key_name = %v, want %q", ev.Payload["key_name"], "my-service")
	}
	if ev.AISummary == "" {
		t.Error("AISummary should not be empty")
	}
}

func TestNewAPIKeyFailed(t *testing.T) {
	ev := events.NewAPIKeyFailed(events.APIKeyFailedParams{
		Method: "POST",
		Path:   "/api/v1/secret",
		Reason: "missing api key",
	})

	if ev.EventType != events.EventTypeAPIKeyFailed {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeAPIKeyFailed)
	}
	if ev.Payload["reason"] != "missing api key" {
		t.Errorf("reason = %v, want %q", ev.Payload["reason"], "missing api key")
	}
}
