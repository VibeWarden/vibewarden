// Package log provides the slog-based adapter for the ports.EventLogger interface.
package log

import (
	"context"
	"log/slog"
)

// MultiHandler is an slog.Handler that dispatches every log record to multiple
// underlying handlers. All handlers receive every record regardless of level.
// Errors returned by individual handlers are silently ignored (best-effort
// logging) so that a failure in one sink — e.g., a network-backed OTel exporter
// — does not block or discard events destined for the remaining sinks.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler creates a MultiHandler that dispatches to all given handlers.
// Passing zero handlers produces a no-op handler that discards all records.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	h := make([]slog.Handler, len(handlers))
	copy(h, handlers)
	return &MultiHandler{handlers: h}
}

// Enabled returns true if at least one underlying handler is enabled for the
// given level. This mirrors how the slog package evaluates whether a Logger is
// enabled before constructing a record.
func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle dispatches the record to every underlying handler.
// Errors are silently swallowed; all handlers receive the record.
func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			// Clone so each handler receives an independent copy; cloning is
			// cheap (only the Attrs slice is duplicated).
			_ = h.Handle(ctx, r.Clone())
		}
	}
	return nil
}

// WithAttrs returns a new MultiHandler with each underlying handler replaced by
// the result of calling WithAttrs on it with the given attrs.
func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: next}
}

// WithGroup returns a new MultiHandler with each underlying handler replaced by
// the result of calling WithGroup on it with the given name.
func (m *MultiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: next}
}
