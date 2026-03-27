package webhook_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/webhook"
	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeEvent(eventType, summary string, payload map[string]any) events.Event {
	if payload == nil {
		payload = map[string]any{}
	}
	return events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     eventType,
		Timestamp:     time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC),
		AISummary:     summary,
		Payload:       payload,
	}
}

// ---------------------------------------------------------------------------
// RawFormatter
// ---------------------------------------------------------------------------

func TestRawFormatter_Format_ValidEvent(t *testing.T) {
	f := &webhook.RawFormatter{}
	ev := makeEvent(events.EventTypeAuthFailed, "auth failed for user@example.com", map[string]any{
		"ip": "1.2.3.4",
	})

	b, err := f.Format(ev)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if got["schema_version"] != "v1" {
		t.Errorf("schema_version = %v, want \"v1\"", got["schema_version"])
	}
	if got["event_type"] != events.EventTypeAuthFailed {
		t.Errorf("event_type = %v, want %q", got["event_type"], events.EventTypeAuthFailed)
	}
	if got["ai_summary"] != ev.AISummary {
		t.Errorf("ai_summary = %v, want %q", got["ai_summary"], ev.AISummary)
	}
	payload, ok := got["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload is %T, want map[string]any", got["payload"])
	}
	if payload["ip"] != "1.2.3.4" {
		t.Errorf("payload.ip = %v, want %q", payload["ip"], "1.2.3.4")
	}
}

func TestRawFormatter_Format_NilPayload(t *testing.T) {
	f := &webhook.RawFormatter{}
	ev := makeEvent(events.EventTypeProxyStarted, "proxy started", nil)

	b, err := f.Format(ev)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	payload, ok := got["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload is %T, want map[string]any", got["payload"])
	}
	if len(payload) != 0 {
		t.Errorf("payload len = %d, want 0", len(payload))
	}
}

func TestRawFormatter_Format_TimestampUTC(t *testing.T) {
	f := &webhook.RawFormatter{}
	ev := makeEvent(events.EventTypeProxyStarted, "started", nil)

	b, err := f.Format(ev)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(string(b), "2026-03-28T12:00:00Z") {
		t.Errorf("output does not contain expected timestamp; got: %s", string(b))
	}
}

// ---------------------------------------------------------------------------
// SlackFormatter
// ---------------------------------------------------------------------------

func TestSlackFormatter_Format_ValidEvent(t *testing.T) {
	f := &webhook.SlackFormatter{}
	ev := makeEvent(events.EventTypeAuthFailed, "authentication failed", map[string]any{
		"user": "alice",
	})

	b, err := f.Format(ev)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	attachments, ok := got["attachments"].([]any)
	if !ok || len(attachments) == 0 {
		t.Fatalf("attachments field missing or empty")
	}

	att := attachments[0].(map[string]any)
	if att["color"] != "danger" {
		t.Errorf("color = %v, want \"danger\" for auth.failed event", att["color"])
	}
}

func TestSlackFormatter_Format_Colors(t *testing.T) {
	f := &webhook.SlackFormatter{}

	tests := []struct {
		eventType string
		wantColor string
	}{
		{events.EventTypeAuthFailed, "danger"},
		{events.EventTypeRequestBlocked, "danger"},
		{events.EventTypeAuthProviderUnavailable, "danger"},
		{events.EventTypeRateLimitHit, "warning"},
		{events.EventTypeRateLimitUnidentified, "warning"},
		{events.EventTypeAuthSuccess, "good"},
		{events.EventTypeProxyStarted, "good"},
		{events.EventTypeUserCreated, "good"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			ev := makeEvent(tt.eventType, "summary", nil)
			b, err := f.Format(ev)
			if err != nil {
				t.Fatalf("Format() error = %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			attachments := got["attachments"].([]any)
			att := attachments[0].(map[string]any)
			if att["color"] != tt.wantColor {
				t.Errorf("color = %v, want %q", att["color"], tt.wantColor)
			}
		})
	}
}

func TestSlackFormatter_Format_EventTypeInTitle(t *testing.T) {
	f := &webhook.SlackFormatter{}
	ev := makeEvent(events.EventTypeRateLimitHit, "rate limit hit for 1.2.3.4", nil)

	b, err := f.Format(ev)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if !strings.Contains(string(b), events.EventTypeRateLimitHit) {
		t.Errorf("output does not contain event type %q", events.EventTypeRateLimitHit)
	}
}

func TestSlackFormatter_Format_NilPayload(t *testing.T) {
	f := &webhook.SlackFormatter{}
	ev := makeEvent(events.EventTypeProxyStarted, "started", nil)

	b, err := f.Format(ev)
	if err != nil {
		t.Fatalf("Format() unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DiscordFormatter
// ---------------------------------------------------------------------------

func TestDiscordFormatter_Format_ValidEvent(t *testing.T) {
	f := &webhook.DiscordFormatter{}
	ev := makeEvent(events.EventTypeRateLimitHit, "rate limit hit", map[string]any{
		"ip": "10.0.0.1",
	})

	b, err := f.Format(ev)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if got["username"] != "VibeWarden" {
		t.Errorf("username = %v, want \"VibeWarden\"", got["username"])
	}

	embeds, ok := got["embeds"].([]any)
	if !ok || len(embeds) == 0 {
		t.Fatalf("embeds field missing or empty")
	}

	embed := embeds[0].(map[string]any)
	if !strings.Contains(embed["title"].(string), events.EventTypeRateLimitHit) {
		t.Errorf("title %q does not contain event type", embed["title"])
	}
	if embed["description"] != "rate limit hit" {
		t.Errorf("description = %v, want %q", embed["description"], "rate limit hit")
	}
}

func TestDiscordFormatter_Format_Colors(t *testing.T) {
	f := &webhook.DiscordFormatter{}

	tests := []struct {
		eventType string
		wantColor float64 // JSON numbers decode as float64
	}{
		{events.EventTypeAuthFailed, 0xED4245},
		{events.EventTypeRequestBlocked, 0xED4245},
		{events.EventTypeAuthProviderUnavailable, 0xED4245},
		{events.EventTypeRateLimitHit, 0xFEE75C},
		{events.EventTypeRateLimitUnidentified, 0xFEE75C},
		{events.EventTypeAuthSuccess, 0x57F287},
		{events.EventTypeProxyStarted, 0x57F287},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			ev := makeEvent(tt.eventType, "summary", nil)
			b, err := f.Format(ev)
			if err != nil {
				t.Fatalf("Format() error = %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			embeds := got["embeds"].([]any)
			embed := embeds[0].(map[string]any)
			color := embed["color"].(float64)
			if color != tt.wantColor {
				t.Errorf("color = %v, want %v", color, tt.wantColor)
			}
		})
	}
}

func TestDiscordFormatter_Format_FooterHasSchema(t *testing.T) {
	f := &webhook.DiscordFormatter{}
	ev := makeEvent(events.EventTypeProxyStarted, "started", nil)

	b, err := f.Format(ev)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if !strings.Contains(string(b), "v1") {
		t.Errorf("output does not contain schema version; got: %s", string(b))
	}
}
