package audit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	auditadapter "github.com/vibewarden/vibewarden/internal/adapters/audit"
	"github.com/vibewarden/vibewarden/internal/domain/audit"
)

// fixedEvent returns an AuditEvent with a fixed timestamp and deterministic fields.
func fixedEvent(t *testing.T, eventType audit.EventType, outcome audit.Outcome) audit.AuditEvent {
	t.Helper()
	ev, err := audit.NewAuditEvent(
		eventType,
		audit.Actor{IP: "10.0.0.1", UserID: "user-abc"},
		audit.Target{Path: "/api/test", Resource: "test"},
		outcome,
		"trace-xyz",
		map[string]any{"key": "value"},
	)
	if err != nil {
		t.Fatalf("NewAuditEvent: %v", err)
	}
	return ev
}

// logAndDecode writes one event to a buffer via JSONWriter and returns the
// decoded JSON map. The test fails if writing or decoding fails.
func logAndDecode(t *testing.T, event audit.AuditEvent) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	w := auditadapter.NewJSONWriter(&buf)
	if err := w.Log(context.Background(), event); err != nil {
		t.Fatalf("JSONWriter.Log: %v", err)
	}
	out := buf.Bytes()
	if len(out) == 0 {
		t.Fatal("JSONWriter produced no output")
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	return result
}

// TestJSONWriter_TopLevelFields verifies that all required fields are present.
func TestJSONWriter_TopLevelFields(t *testing.T) {
	event := fixedEvent(t, audit.EventTypeAuthSuccess, audit.OutcomeSuccess)
	result := logAndDecode(t, event)

	required := []string{"timestamp", "event_type", "actor", "target", "outcome", "details"}
	for _, key := range required {
		if _, ok := result[key]; !ok {
			t.Errorf("missing required key %q in JSON output", key)
		}
	}
}

// TestJSONWriter_FieldValues verifies that fields carry the correct values.
func TestJSONWriter_FieldValues(t *testing.T) {
	tests := []struct {
		name      string
		eventType audit.EventType
		outcome   audit.Outcome
		actor     audit.Actor
		target    audit.Target
		traceID   string
	}{
		{
			name:      "auth success",
			eventType: audit.EventTypeAuthSuccess,
			outcome:   audit.OutcomeSuccess,
			actor:     audit.Actor{IP: "1.2.3.4", UserID: "u-1"},
			target:    audit.Target{Path: "/api", Resource: "api"},
			traceID:   "trace-001",
		},
		{
			name:      "rate limit hit",
			eventType: audit.EventTypeRateLimitHit,
			outcome:   audit.OutcomeFailure,
			actor:     audit.Actor{IP: "5.6.7.8"},
			target:    audit.Target{Path: "/heavy"},
			traceID:   "",
		},
		{
			name:      "ip filter blocked",
			eventType: audit.EventTypeIPFilterBlocked,
			outcome:   audit.OutcomeFailure,
			actor:     audit.Actor{IP: "9.9.9.9", APIKeyName: "ci-key"},
			target:    audit.Target{Path: "/admin"},
			traceID:   "trace-block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := audit.NewAuditEvent(tt.eventType, tt.actor, tt.target, tt.outcome, tt.traceID, nil)
			if err != nil {
				t.Fatalf("NewAuditEvent: %v", err)
			}

			result := logAndDecode(t, ev)

			if got := result["event_type"]; got != string(tt.eventType) {
				t.Errorf("event_type = %v, want %q", got, tt.eventType)
			}
			if got := result["outcome"]; got != string(tt.outcome) {
				t.Errorf("outcome = %v, want %q", got, tt.outcome)
			}

			actor, ok := result["actor"].(map[string]any)
			if !ok {
				t.Fatalf("actor is not a JSON object: %T", result["actor"])
			}
			if tt.actor.IP != "" {
				if got := actor["ip"]; got != tt.actor.IP {
					t.Errorf("actor.ip = %v, want %q", got, tt.actor.IP)
				}
			}
			if tt.actor.UserID != "" {
				if got := actor["user_id"]; got != tt.actor.UserID {
					t.Errorf("actor.user_id = %v, want %q", got, tt.actor.UserID)
				}
			}
			if tt.actor.APIKeyName != "" {
				if got := actor["api_key_name"]; got != tt.actor.APIKeyName {
					t.Errorf("actor.api_key_name = %v, want %q", got, tt.actor.APIKeyName)
				}
			}

			if tt.traceID != "" {
				if got := result["trace_id"]; got != tt.traceID {
					t.Errorf("trace_id = %v, want %q", got, tt.traceID)
				}
			} else {
				if _, ok := result["trace_id"]; ok {
					t.Error("trace_id should be absent when empty, but it was present")
				}
			}
		})
	}
}

// TestJSONWriter_DetailsObject verifies that the details field is always a JSON object.
func TestJSONWriter_DetailsObject(t *testing.T) {
	tests := []struct {
		name    string
		details map[string]any
	}{
		{"non-nil details", map[string]any{"foo": "bar", "n": 42}},
		{"nil details normalised", nil},
		{"empty details", map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := audit.NewAuditEvent(
				audit.EventTypeAuthFailure,
				audit.Actor{},
				audit.Target{},
				audit.OutcomeFailure,
				"",
				tt.details,
			)
			if err != nil {
				t.Fatalf("NewAuditEvent: %v", err)
			}
			result := logAndDecode(t, ev)
			if _, ok := result["details"].(map[string]any); !ok {
				t.Errorf("details is not a JSON object: %T", result["details"])
			}
		})
	}
}

// TestJSONWriter_TimestampFormat verifies that the timestamp is RFC3339Nano in UTC.
func TestJSONWriter_TimestampFormat(t *testing.T) {
	ev := fixedEvent(t, audit.EventTypeAuthSuccess, audit.OutcomeSuccess)
	result := logAndDecode(t, ev)

	ts, ok := result["timestamp"].(string)
	if !ok || ts == "" {
		t.Fatalf("timestamp is missing or not a string: %v", result["timestamp"])
	}
	// Must be parseable as RFC3339Nano and end in Z (UTC).
	parsed, err := time.Parse("2006-01-02T15:04:05.999999999Z", ts)
	if err != nil {
		// Try without sub-second precision (no fractional part)
		parsed, err = time.Parse("2006-01-02T15:04:05Z", ts)
		if err != nil {
			t.Fatalf("timestamp %q is not in expected UTC format: %v", ts, err)
		}
	}
	if parsed.Location() != time.UTC {
		t.Errorf("timestamp location = %v, want UTC", parsed.Location())
	}
}

// TestJSONWriter_OneLinePerEvent verifies that each call to Log writes exactly
// one newline-terminated JSON object (JSONL semantics).
func TestJSONWriter_OneLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	w := auditadapter.NewJSONWriter(&buf)

	for i := 0; i < 3; i++ {
		ev := fixedEvent(t, audit.EventTypeAuthSuccess, audit.OutcomeSuccess)
		if err := w.Log(context.Background(), ev); err != nil {
			t.Fatalf("Log iteration %d: %v", i, err)
		}
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

// TestJSONWriter_NilWriterFallsBackToStdout verifies no panic when w is nil.
func TestJSONWriter_NilWriterFallsBackToStdout(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewJSONWriter(nil) panicked: %v", r)
		}
	}()
	w := auditadapter.NewJSONWriter(nil)
	if w == nil {
		t.Error("NewJSONWriter(nil) returned nil")
	}
}

// TestNewJSONWriterToFile verifies that NewJSONWriterToFile creates the file
// and writes valid JSONL to it.
func TestNewJSONWriterToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, closer, err := auditadapter.NewJSONWriterToFile(path)
	if err != nil {
		t.Fatalf("NewJSONWriterToFile: %v", err)
	}

	ev := fixedEvent(t, audit.EventTypeAdminUserCreated, audit.OutcomeSuccess)
	if err := w.Log(context.Background(), ev); err != nil {
		_ = closer.Close()
		t.Fatalf("Log: %v", err)
	}
	// Close before reading so the OS flushes the file.
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("file content is not valid JSON: %v\ncontent: %s", err, data)
	}
	if got := result["event_type"]; got != string(audit.EventTypeAdminUserCreated) {
		t.Errorf("event_type = %v, want %q", got, audit.EventTypeAdminUserCreated)
	}
}

// TestNewJSONWriterToFile_InvalidPath verifies that an error is returned for
// an unwritable path.
func TestNewJSONWriterToFile_InvalidPath(t *testing.T) {
	_, _, err := auditadapter.NewJSONWriterToFile("/nonexistent/deep/path/audit.jsonl")
	if err == nil {
		t.Error("expected error for invalid path, got nil")
	}
}
