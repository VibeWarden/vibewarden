// Package log provides the slog-based adapter for the ports.EventLogger interface.
// It writes structured JSON to a configurable io.Writer (default: os.Stdout),
// following the VibeWarden v1 event schema.
package log

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// SlogEventLogger implements ports.EventLogger using log/slog with a JSON handler.
// Each call to Log emits one JSON object that conforms to the VibeWarden v1 event
// schema: schema_version, event_type, timestamp, ai_summary, and payload.
type SlogEventLogger struct {
	logger *slog.Logger
}

// NewSlogEventLogger creates a SlogEventLogger that writes JSON to w.
// Pass os.Stdout for production use. Pass a *bytes.Buffer or similar in tests.
// The logger emits every record regardless of level — it always uses LevelInfo.
//
// Additional handlers (e.g., an OTel bridge handler) can be provided via
// additionalHandlers. When present, a MultiHandler fans out records to the
// JSON handler and all additional handlers simultaneously.
func NewSlogEventLogger(w io.Writer, additionalHandlers ...slog.Handler) *SlogEventLogger {
	if w == nil {
		w = os.Stdout
	}
	jsonHandler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		// Disable slog's default level filtering so all events are written.
		Level: slog.LevelDebug,
		// Replace the default "time" key with our own timestamp from the Event
		// struct so the JSON timestamp is the event's logical time, not the
		// wall-clock time of the Log call.
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				// Drop slog's automatic timestamp; we add our own below.
				return slog.Attr{}
			}
			if len(groups) == 0 && a.Key == slog.LevelKey {
				// Drop the level key — the schema does not include it.
				return slog.Attr{}
			}
			if len(groups) == 0 && a.Key == slog.MessageKey {
				// Drop the msg key — the schema does not include it.
				return slog.Attr{}
			}
			return a
		},
	})

	var handler slog.Handler = jsonHandler
	if len(additionalHandlers) > 0 {
		all := make([]slog.Handler, 0, 1+len(additionalHandlers))
		all = append(all, jsonHandler)
		all = append(all, additionalHandlers...)
		handler = NewMultiHandler(all...)
	}

	return &SlogEventLogger{logger: slog.New(handler)}
}

// Log writes the event as a single JSON line to the configured writer.
// The JSON structure follows the VibeWarden v1 schema:
//
//	{
//	  "schema_version": "v1",
//	  "event_type":     "auth.success",
//	  "timestamp":      "2026-03-26T12:00:00Z",
//	  "ai_summary":     "...",
//	  "payload":        { ... },
//	  "trace_id":       "4bf92f3577b34da6a3ce929d0e0e4736",  // present only when tracing is active
//	  "span_id":        "00f067aa0ba902b7"                   // present only when tracing is active
//	}
//
// When the context contains a valid OTel span context (injected by TracingMiddleware),
// trace_id and span_id are appended as top-level fields for request correlation.
// When tracing is disabled or no span is present, those fields are completely absent
// (never emitted as empty strings).
func (l *SlogEventLogger) Log(ctx context.Context, event events.Event) error {
	// Serialize the payload map to a json.RawMessage so that:
	//  - An empty payload emits {} rather than being omitted (slog.Group with
	//    zero attributes is silently dropped by slog.JSONHandler).
	//  - The payload always appears as a nested JSON object in the output.
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling event payload: %w", err)
	}

	attrs := []slog.Attr{
		slog.String("schema_version", event.SchemaVersion),
		slog.String("event_type", event.EventType),
		slog.Time("timestamp", event.Timestamp),
		slog.String("ai_summary", event.AISummary),
		slog.Any("payload", json.RawMessage(payloadBytes)),
	}

	// Extract trace context if the request context carries a valid OTel span.
	// SpanContextFromContext is a cheap map lookup and returns an invalid
	// SpanContext when no span has been stored — no allocation occurs.
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		attrs = append(attrs,
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}

	l.logger.LogAttrs(
		ctx,
		slog.LevelInfo,
		"", // message field is suppressed by ReplaceAttr above
		attrs...,
	)

	// slog.JSONHandler does not surface write errors through the API.
	// Any I/O errors are silently dropped by the handler. We return nil
	// here in line with the ports.EventLogger contract (best-effort).
	return nil
}
