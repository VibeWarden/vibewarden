// Package audit provides adapters that implement the ports.AuditEventLogger
// interface. Each adapter writes AuditEvents to a specific sink.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

var _ ports.AuditEventLogger = (*JSONWriter)(nil)

// jsonRecord is the wire format written by JSONWriter.
// All fields use JSON snake_case to match the VibeWarden v1 event schema style.
type jsonRecord struct {
	Timestamp string         `json:"timestamp"`
	EventType string         `json:"event_type"`
	Actor     jsonActor      `json:"actor"`
	Target    jsonTarget     `json:"target"`
	Outcome   string         `json:"outcome"`
	TraceID   string         `json:"trace_id,omitempty"`
	Details   map[string]any `json:"details"`
}

// jsonActor mirrors audit.Actor for JSON serialisation.
type jsonActor struct {
	IP         string `json:"ip,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	APIKeyName string `json:"api_key_name,omitempty"`
}

// jsonTarget mirrors audit.Target for JSON serialisation.
type jsonTarget struct {
	Path     string `json:"path,omitempty"`
	Resource string `json:"resource,omitempty"`
}

// JSONWriter implements ports.AuditEventLogger by writing each AuditEvent as a
// single JSON line (JSONL) to a configurable io.Writer.
//
// The zero value is not usable — create instances via NewJSONWriter or
// NewJSONWriterToFile.
type JSONWriter struct {
	enc *json.Encoder
}

// NewJSONWriter creates a JSONWriter that writes to w.
// Passing nil falls back to os.Stdout.
// The encoder is configured to write each record on a single line with no
// indentation, producing a valid JSONL stream.
func NewJSONWriter(w io.Writer) *JSONWriter {
	if w == nil {
		w = os.Stdout
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &JSONWriter{enc: enc}
}

// NewJSONWriterToFile creates a JSONWriter that appends to the file at path.
// The file is created if it does not exist, with permission 0644.
// The returned closer must be called when the writer is no longer needed.
// Returns an error if the file cannot be opened or created.
func NewJSONWriterToFile(path string) (*JSONWriter, io.Closer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening audit log file %q: %w", path, err)
	}
	return NewJSONWriter(f), f, nil
}

// Log writes the AuditEvent as a single JSON line to the configured writer.
// The record format includes timestamp (RFC3339Nano, UTC), event_type, actor,
// target, outcome, optional trace_id, and a details object.
// Returns an error if JSON serialisation or the underlying write fails.
func (w *JSONWriter) Log(_ context.Context, event audit.AuditEvent) error {
	rec := jsonRecord{
		Timestamp: event.Timestamp.UTC().Format("2006-01-02T15:04:05.999999999Z"),
		EventType: string(event.EventType),
		Actor: jsonActor{
			IP:         event.Actor.IP,
			UserID:     event.Actor.UserID,
			APIKeyName: event.Actor.APIKeyName,
		},
		Target: jsonTarget{
			Path:     event.Target.Path,
			Resource: event.Target.Resource,
		},
		Outcome: string(event.Outcome),
		TraceID: event.TraceID,
		Details: event.Details,
	}
	if rec.Details == nil {
		rec.Details = map[string]any{}
	}

	if err := w.enc.Encode(rec); err != nil {
		return fmt.Errorf("encoding audit event: %w", err)
	}
	return nil
}
