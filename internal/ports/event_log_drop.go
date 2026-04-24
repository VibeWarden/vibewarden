// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

// EventLogDropCounter is the narrow outbound port used by the middleware
// log-drop helper. Adapters that emit drop metrics implement this interface;
// a nil value is a valid no-op at call sites.
//
// IncEventLogDrop increments the vibewarden_event_log_drops_total counter for
// the given middleware and reason labels.
//
// The middleware label is a fixed short string identifying the middleware
// (e.g. "auth", "waf", "ratelimit"). The reason label is a low-cardinality
// class derived from the error (e.g. "canceled", "deadline_exceeded", "other").
// Raw error strings must never be used as label values.
type EventLogDropCounter interface {
	IncEventLogDrop(middleware, reason string)
}
