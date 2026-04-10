// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
)

// AuditEventLogger is the outbound port for recording security audit events.
// Implementations write events to a durable sink (e.g. PostgreSQL, stdout JSON).
//
// Audit events are always emitted — they are not subject to log-level filtering.
// The port intentionally mirrors the shape of EventLogger to keep the consumer
// API consistent, but targets the domain/audit model rather than domain/events.
type AuditEventLogger interface {
	// Log persists a single audit event. Implementations must not modify the event.
	// Returns a non-nil error if the event could not be persisted or emitted.
	// Callers should not halt request processing on error; they should surface the
	// failure through a secondary channel (e.g. a structured log entry) instead.
	Log(ctx context.Context, event audit.AuditEvent) error
}
