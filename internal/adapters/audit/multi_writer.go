package audit

import (
	"context"
	"errors"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/ports"
)

var _ ports.AuditEventLogger = (*MultiWriter)(nil)

// MultiWriter implements ports.AuditEventLogger by fanning out every
// AuditEvent to a list of underlying loggers. All loggers receive every event.
// If one or more loggers return an error, MultiWriter joins all errors and
// returns the combined result. A failure in one sink does not prevent delivery
// to the remaining sinks.
type MultiWriter struct {
	writers []ports.AuditEventLogger
}

// NewMultiWriter creates a MultiWriter that dispatches to all given writers.
// Passing zero writers produces a no-op writer that discards all events.
func NewMultiWriter(writers ...ports.AuditEventLogger) *MultiWriter {
	w := make([]ports.AuditEventLogger, len(writers))
	copy(w, writers)
	return &MultiWriter{writers: w}
}

// Log delivers the event to every underlying logger.
// Errors from individual loggers are collected and returned as a joined error.
// A nil return means all loggers accepted the event without error.
func (m *MultiWriter) Log(ctx context.Context, event audit.AuditEvent) error {
	var errs []error
	for _, w := range m.writers {
		if err := w.Log(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
