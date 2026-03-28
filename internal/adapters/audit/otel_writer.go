package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

var _ ports.AuditEventLogger = (*OTelWriter)(nil)

// OTelWriter implements ports.AuditEventLogger by forwarding AuditEvents as
// slog records to the provided slog.Handler. When the handler is the OTel
// slog bridge (go.opentelemetry.io/contrib/bridges/otelslog), audit events are
// exported to the configured OTLP endpoint.
//
// OTelWriter is intentionally thin: it converts the structured AuditEvent into
// slog attributes and delegates all transport to the handler. This means the
// same adapter works with any slog.Handler — useful in tests.
type OTelWriter struct {
	logger *slog.Logger
}

// NewOTelWriter creates an OTelWriter that dispatches records to handler.
// Passing nil returns a writer that discards all events.
func NewOTelWriter(handler slog.Handler) *OTelWriter {
	if handler == nil {
		handler = noopHandler{}
	}
	return &OTelWriter{logger: slog.New(handler)}
}

// Log converts the AuditEvent into a slog record and passes it to the
// underlying slog.Handler. All event fields are emitted as slog attributes
// under a top-level group named "audit".
//
// Severity is always LevelInfo so that the OTel bridge maps it to INFO
// severity. Callers that need WARN/ERROR severity should use a custom handler
// that inspects the "audit.event_type" attribute.
func (w *OTelWriter) Log(ctx context.Context, event audit.AuditEvent) error {
	detailsBytes, err := json.Marshal(event.Details)
	if err != nil {
		return fmt.Errorf("marshalling audit event details: %w", err)
	}

	w.logger.LogAttrs(ctx, slog.LevelInfo, "audit_event",
		slog.Group("audit",
			slog.Time("timestamp", event.Timestamp),
			slog.String("event_type", string(event.EventType)),
			slog.Group("actor",
				slog.String("ip", event.Actor.IP),
				slog.String("user_id", event.Actor.UserID),
				slog.String("api_key_name", event.Actor.APIKeyName),
			),
			slog.Group("target",
				slog.String("path", event.Target.Path),
				slog.String("resource", event.Target.Resource),
			),
			slog.String("outcome", string(event.Outcome)),
			slog.String("trace_id", event.TraceID),
			slog.Any("details", json.RawMessage(detailsBytes)),
		),
	)
	return nil
}

// noopHandler is an slog.Handler that discards all records.
// Used when NewOTelWriter is called with a nil handler.
type noopHandler struct{}

func (noopHandler) Enabled(_ context.Context, _ slog.Level) bool  { return false }
func (noopHandler) Handle(_ context.Context, _ slog.Record) error { return nil }
func (n noopHandler) WithAttrs(_ []slog.Attr) slog.Handler        { return n }
func (n noopHandler) WithGroup(_ string) slog.Handler             { return n }
