// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"
	"time"
)

// AuditAction is the type of action recorded in the audit log.
// Values are stable strings that form part of the public schema contract.
type AuditAction string

const (
	// AuditActionUserCreated records that a user identity was created.
	AuditActionUserCreated AuditAction = "user.created"

	// AuditActionUserDeactivated records that a user identity was deactivated.
	AuditActionUserDeactivated AuditAction = "user.deactivated"
)

// AuditEntry is a single immutable record written to the audit log.
// The log is append-only: entries are never updated or deleted.
type AuditEntry struct {
	// UserID is the identity provider UUID of the user affected by the action.
	UserID string

	// Action is the type of action performed (e.g. "user.created").
	Action AuditAction

	// ActorID identifies the admin who performed the action.
	// May be empty when the action was performed by the system.
	ActorID string

	// Timestamp is when the action occurred. If zero, the adapter sets it to now.
	Timestamp time.Time

	// Metadata holds arbitrary structured data specific to the action.
	// Values must be JSON-serialisable.
	Metadata map[string]any
}

// AuditLogger is the outbound port for persisting audit log entries.
// Implementations write entries to a durable store (e.g. PostgreSQL).
// All writes are append-only — callers must never request deletion.
type AuditLogger interface {
	// RecordEntry persists a single audit entry. Implementations must set
	// Timestamp to the current UTC time when entry.Timestamp is zero.
	// Returns a non-nil error if the entry could not be persisted.
	RecordEntry(ctx context.Context, entry AuditEntry) error
}
