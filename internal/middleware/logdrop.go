package middleware

import (
	"context"
	"errors"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// logEvent is a fire-and-forget wrapper around ports.EventLogger.Log.
// On error it emits slog.WarnContext and increments
// vibewarden_event_log_drops_total via mc.
//
// Callers must not halt request handling on failure; this function always
// returns void. Both logger and mc may be nil — nil logger is a silent no-op,
// nil mc means the counter is skipped but the slog.Warn still fires.
func logEvent(ctx context.Context, logger ports.EventLogger, mc ports.EventLogDropCounter, middleware string, ev events.Event) {
	if logger == nil {
		return
	}
	if err := logger.Log(ctx, ev); err != nil {
		reason := errorReason(err)
		slog.WarnContext(ctx, "event log drop",
			slog.String("middleware", middleware),
			slog.String("event_type", ev.EventType),
			slog.String("reason", reason),
			slog.Any("err", err),
		)
		if mc != nil {
			mc.IncEventLogDrop(middleware, reason)
		}
	}
}

// logAudit is the AuditEventLogger counterpart of logEvent.
// On error it emits slog.WarnContext and increments
// vibewarden_event_log_drops_total via mc.
//
// Callers must not halt request handling on failure; this function always
// returns void. Both logger and mc may be nil — nil logger is a silent no-op,
// nil mc means the counter is skipped but the slog.Warn still fires.
func logAudit(ctx context.Context, logger ports.AuditEventLogger, mc ports.EventLogDropCounter, middleware string, ev audit.AuditEvent) {
	if logger == nil {
		return
	}
	if err := logger.Log(ctx, ev); err != nil {
		reason := errorReason(err)
		slog.WarnContext(ctx, "event log drop",
			slog.String("middleware", middleware),
			slog.String("event_type", string(ev.EventType)),
			slog.String("reason", reason),
			slog.Any("err", err),
		)
		if mc != nil {
			mc.IncEventLogDrop(middleware, reason)
		}
	}
}

// errorReason returns a low-cardinality label string classifying err for use
// as a Prometheus label value. It never calls err.Error() to prevent
// unbounded label cardinality.
func errorReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	default:
		return "other"
	}
}
