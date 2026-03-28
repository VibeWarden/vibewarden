package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// fixedEvent returns an Event with a fixed timestamp for deterministic tests.
func fixedEvent(eventType, aiSummary string, payload map[string]any) events.Event {
	return events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     eventType,
		Timestamp:     time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC),
		AISummary:     aiSummary,
		Payload:       payload,
	}
}

// logAndDecode writes a single event using a SlogEventLogger and decodes the
// resulting JSON line into a map. It fails the test if the output is not valid
// JSON or if more than one line was written.
func logAndDecode(t *testing.T, event events.Event) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := log.NewSlogEventLogger(&buf)
	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log() returned unexpected error: %v", err)
	}

	out := buf.Bytes()
	if len(out) == 0 {
		t.Fatal("Log() produced no output")
	}

	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	return result
}

// TestSlogEventLogger_TopLevelFields verifies that all required schema fields
// are present at the top level of the emitted JSON object.
func TestSlogEventLogger_TopLevelFields(t *testing.T) {
	event := fixedEvent(
		events.EventTypeAuthSuccess,
		"Authenticated request allowed: GET /api (identity abc123)",
		map[string]any{
			"method":      "GET",
			"path":        "/api",
			"session_id":  "sess_1",
			"identity_id": "abc123",
			"email":       "user@example.com",
		},
	)

	result := logAndDecode(t, event)

	requiredKeys := []string{"schema_version", "event_type", "timestamp", "ai_summary", "payload"}
	for _, key := range requiredKeys {
		if _, ok := result[key]; !ok {
			t.Errorf("missing required key %q in JSON output", key)
		}
	}

	// Ensure slog's internal keys are not present.
	unwantedKeys := []string{"level", "msg", "time"}
	for _, key := range unwantedKeys {
		if _, ok := result[key]; ok {
			t.Errorf("unexpected key %q found in JSON output", key)
		}
	}
}

// TestSlogEventLogger_FieldValues verifies that schema fields carry the correct
// values from the domain Event.
func TestSlogEventLogger_FieldValues(t *testing.T) {
	ts := time.Date(2026, 3, 26, 15, 30, 0, 0, time.UTC)
	event := events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     events.EventTypeAuthFailed,
		Timestamp:     ts,
		AISummary:     "Unauthenticated request rejected: missing session cookie",
		Payload: map[string]any{
			"method": "POST",
			"path":   "/login",
			"reason": "missing session cookie",
			"detail": "",
		},
	}

	result := logAndDecode(t, event)

	if got := result["schema_version"]; got != "v1" {
		t.Errorf("schema_version = %v, want %q", got, "v1")
	}
	if got := result["event_type"]; got != events.EventTypeAuthFailed {
		t.Errorf("event_type = %v, want %q", got, events.EventTypeAuthFailed)
	}
	if got := result["ai_summary"]; got != event.AISummary {
		t.Errorf("ai_summary = %v, want %q", got, event.AISummary)
	}
	// timestamp is RFC3339Nano in slog's JSON output.
	if got, ok := result["timestamp"].(string); !ok || got == "" {
		t.Errorf("timestamp is missing or not a string: %v", result["timestamp"])
	}
}

// TestSlogEventLogger_PayloadStructure verifies that the payload is nested as
// a JSON object under the "payload" key.
func TestSlogEventLogger_PayloadStructure(t *testing.T) {
	event := fixedEvent(
		events.EventTypeRateLimitHit,
		"Rate limit exceeded",
		map[string]any{
			"limit_type":          "ip",
			"identifier":          "1.2.3.4",
			"requests_per_second": 10.0,
			"burst":               20,
			"retry_after_seconds": 5,
			"path":                "/api",
			"method":              "GET",
		},
	)

	result := logAndDecode(t, event)

	payload, ok := result["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload is not a JSON object, got: %T %v", result["payload"], result["payload"])
	}

	wantKeys := []string{"limit_type", "identifier", "requests_per_second", "burst", "retry_after_seconds", "path", "method"}
	for _, key := range wantKeys {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload missing key %q", key)
		}
	}
}

// TestSlogEventLogger_MultipleEventTypes exercises several different event
// types to ensure each produces valid JSON with correct schema_version and
// event_type values.
func TestSlogEventLogger_MultipleEventTypes(t *testing.T) {
	tests := []struct {
		name     string
		event    events.Event
		wantType string
	}{
		{
			name: "proxy started",
			event: fixedEvent(
				events.EventTypeProxyStarted,
				"Reverse proxy listening on :8080, forwarding to localhost:3000",
				map[string]any{
					"listen":                   ":8080",
					"upstream":                 "localhost:3000",
					"tls_enabled":              false,
					"tls_provider":             "",
					"security_headers_enabled": true,
					"version":                  "dev",
				},
			),
			wantType: events.EventTypeProxyStarted,
		},
		{
			name: "auth success",
			event: fixedEvent(
				events.EventTypeAuthSuccess,
				"Authenticated request allowed: GET /dashboard (identity u1)",
				map[string]any{
					"method":      "GET",
					"path":        "/dashboard",
					"session_id":  "sess_abc",
					"identity_id": "u1",
					"email":       "alice@example.com",
				},
			),
			wantType: events.EventTypeAuthSuccess,
		},
		{
			name: "auth failed",
			event: fixedEvent(
				events.EventTypeAuthFailed,
				"Unauthenticated request rejected: missing session cookie",
				map[string]any{
					"method": "GET",
					"path":   "/secret",
					"reason": "missing session cookie",
					"detail": "",
				},
			),
			wantType: events.EventTypeAuthFailed,
		},
		{
			name: "rate limit hit",
			event: fixedEvent(
				events.EventTypeRateLimitHit,
				"Rate limit exceeded for ip 10.0.0.1: 5 requests/second limit reached",
				map[string]any{
					"limit_type":          "ip",
					"identifier":          "10.0.0.1",
					"requests_per_second": 5.0,
					"burst":               10,
					"retry_after_seconds": 2,
					"path":                "/api/data",
					"method":              "POST",
				},
			),
			wantType: events.EventTypeRateLimitHit,
		},
		{
			name: "rate limit unidentified",
			event: fixedEvent(
				events.EventTypeRateLimitUnidentified,
				"Request rejected because the client IP could not be determined",
				map[string]any{
					"path":   "/api",
					"method": "GET",
				},
			),
			wantType: events.EventTypeRateLimitUnidentified,
		},
		{
			name: "request blocked",
			event: fixedEvent(
				events.EventTypeRequestBlocked,
				"Request blocked by ip_blocklist: GET /admin — blocked IP",
				map[string]any{
					"method":     "GET",
					"path":       "/admin",
					"reason":     "blocked IP",
					"blocked_by": "ip_blocklist",
					"client_ip":  "1.2.3.4",
				},
			),
			wantType: events.EventTypeRequestBlocked,
		},
		{
			name: "proxy kratos flow",
			event: fixedEvent(
				events.EventTypeProxyKratosFlow,
				"Request proxied to Kratos self-service API",
				map[string]any{
					"method": "POST",
					"path":   "/self-service/login",
				},
			),
			wantType: events.EventTypeProxyKratosFlow,
		},
		{
			name: "tls certificate issued",
			event: fixedEvent(
				events.EventTypeTLSCertificateIssued,
				"TLS certificate issued for example.com via letsencrypt",
				map[string]any{
					"domain":     "example.com",
					"provider":   "letsencrypt",
					"expires_at": "2026-06-26T00:00:00Z",
				},
			),
			wantType: events.EventTypeTLSCertificateIssued,
		},
		{
			name: "user created",
			event: fixedEvent(
				events.EventTypeUserCreated,
				"New user created: bob@example.com (identity id_bob)",
				map[string]any{
					"identity_id": "id_bob",
					"email":       "bob@example.com",
				},
			),
			wantType: events.EventTypeUserCreated,
		},
		{
			name: "user deleted",
			event: fixedEvent(
				events.EventTypeUserDeleted,
				"User deleted: bob@example.com (identity id_bob)",
				map[string]any{
					"identity_id": "id_bob",
					"email":       "bob@example.com",
				},
			),
			wantType: events.EventTypeUserDeleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := logAndDecode(t, tt.event)

			if got := result["schema_version"]; got != "v1" {
				t.Errorf("schema_version = %v, want %q", got, "v1")
			}
			if got := result["event_type"]; got != tt.wantType {
				t.Errorf("event_type = %v, want %q", got, tt.wantType)
			}
			if _, ok := result["payload"].(map[string]any); !ok {
				t.Errorf("payload is not a JSON object: %T", result["payload"])
			}
			if got, ok := result["ai_summary"].(string); !ok || got == "" {
				t.Errorf("ai_summary is missing or empty")
			}
		})
	}
}

// TestSlogEventLogger_NilWriter verifies that passing nil as the writer falls
// back gracefully (uses os.Stdout) and does not panic.
func TestSlogEventLogger_NilWriter(t *testing.T) {
	// We can't easily capture stdout, so we just verify no panic occurs.
	// The real writes go to os.Stdout during this test, which is acceptable.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewSlogEventLogger(nil) panicked: %v", r)
		}
	}()
	logger := log.NewSlogEventLogger(nil)
	if logger == nil {
		t.Error("NewSlogEventLogger(nil) returned nil")
	}
}

// TestSlogEventLogger_TimestampFromEvent verifies that the JSON timestamp
// matches the Timestamp field of the Event, not the current wall-clock time.
func TestSlogEventLogger_TimestampFromEvent(t *testing.T) {
	fixedTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	event := events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     events.EventTypeProxyStarted,
		Timestamp:     fixedTime,
		AISummary:     "test",
		Payload:       map[string]any{},
	}

	result := logAndDecode(t, event)

	tsStr, ok := result["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp is not a string: %v", result["timestamp"])
	}

	parsed, err := time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		t.Fatalf("timestamp %q is not RFC3339: %v", tsStr, err)
	}

	if !parsed.Equal(fixedTime) {
		t.Errorf("timestamp = %v, want %v", parsed, fixedTime)
	}
}

// TestSlogEventLogger_EmptyPayload verifies that an empty payload is emitted
// as an empty JSON object, not null or omitted.
func TestSlogEventLogger_EmptyPayload(t *testing.T) {
	event := fixedEvent(events.EventTypeProxyStarted, "test", map[string]any{})

	result := logAndDecode(t, event)

	payload, ok := result["payload"]
	if !ok {
		t.Fatal("payload key is absent when payload is empty")
	}
	if payload == nil {
		t.Fatal("payload is null, want empty object")
	}
	if _, ok := payload.(map[string]any); !ok {
		t.Errorf("payload is not a JSON object: %T", payload)
	}
}

// captureHandler is an slog.Handler used in tests to record Handle calls.
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

// TestSlogEventLogger_AdditionalHandler verifies that when an extra handler is
// provided to NewSlogEventLogger, it receives every log record in addition to
// the JSON writer.
func TestSlogEventLogger_AdditionalHandler(t *testing.T) {
	var buf bytes.Buffer
	extra := &captureHandler{}

	logger := log.NewSlogEventLogger(&buf, extra)

	event := fixedEvent(
		events.EventTypeAuthSuccess,
		"Authenticated request allowed",
		map[string]any{"method": "GET", "path": "/api"},
	)

	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	// JSON writer must have produced output.
	if buf.Len() == 0 {
		t.Error("JSON writer produced no output")
	}

	// Additional handler must have received the record.
	if len(extra.records) != 1 {
		t.Errorf("extra handler received %d records, want 1", len(extra.records))
	}
}

// TestSlogEventLogger_MultipleAdditionalHandlers verifies fan-out to more than
// one extra handler.
func TestSlogEventLogger_MultipleAdditionalHandlers(t *testing.T) {
	var buf bytes.Buffer
	h1 := &captureHandler{}
	h2 := &captureHandler{}

	logger := log.NewSlogEventLogger(&buf, h1, h2)

	event := fixedEvent(events.EventTypeProxyStarted, "Proxy started", map[string]any{})
	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	if len(h1.records) != 1 {
		t.Errorf("handler1 received %d records, want 1", len(h1.records))
	}
	if len(h2.records) != 1 {
		t.Errorf("handler2 received %d records, want 1", len(h2.records))
	}
}

// TestSlogEventLogger_NoAdditionalHandlers verifies backward compatibility:
// passing no extra handlers keeps the original JSON-only behaviour.
func TestSlogEventLogger_NoAdditionalHandlers(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewSlogEventLogger(&buf)

	event := fixedEvent(events.EventTypeAuthSuccess, "test", map[string]any{})
	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("JSON writer produced no output")
	}
}
