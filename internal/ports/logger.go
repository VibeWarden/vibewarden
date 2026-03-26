// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// EventLogger is the outbound port for emitting structured security events.
// Implementations write the event to a sink (e.g. stdout JSON, a remote
// collector, a test buffer). Callers in the domain and application layers
// depend on this interface, never on a concrete implementation.
type EventLogger interface {
	// Log emits a structured event. Implementations must not modify the event.
	// The call is best-effort: callers should not halt request processing on error,
	// but should log the failure through a secondary channel where possible.
	Log(ctx context.Context, event events.Event) error
}
