// Package log provides the slog-based adapter for the ports.EventLogger interface.
package log

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// MultiEventLogger fans out every Log call to multiple ports.EventLogger
// implementations. All sinks receive every event regardless of any individual
// sink errors. Errors from individual sinks are silently swallowed so that a
// failure in one sink — e.g., a full ring buffer — does not prevent events from
// reaching the remaining sinks.
type MultiEventLogger struct {
	sinks []ports.EventLogger
}

// NewMultiEventLogger creates a MultiEventLogger that dispatches to all given
// sinks. Passing zero sinks produces a no-op logger.
func NewMultiEventLogger(sinks ...ports.EventLogger) *MultiEventLogger {
	s := make([]ports.EventLogger, len(sinks))
	copy(s, sinks)
	return &MultiEventLogger{sinks: s}
}

// Log dispatches the event to every underlying sink.
// The method always returns nil; errors from individual sinks are silently
// swallowed in line with the ports.EventLogger best-effort contract.
func (m *MultiEventLogger) Log(ctx context.Context, event events.Event) error {
	for _, s := range m.sinks {
		_ = s.Log(ctx, event) //nolint:errcheck // best-effort; callers log failures separately
	}
	return nil
}
