// Package user defines the core domain types for VibeWarden user management.
// This package has zero external dependencies — only the Go standard library.
package user

import "time"

// Status represents the lifecycle state of a user identity.
// Kratos identities can be active or inactive (deactivated).
type Status string

const (
	// StatusActive indicates the user can authenticate and use the system.
	StatusActive Status = "active"

	// StatusInactive indicates the user has been deactivated and cannot
	// authenticate. The identity record is retained for audit purposes.
	StatusInactive Status = "inactive"
)

// AuditAction is the type of administrative action recorded in an AuditEntry.
type AuditAction string

const (
	// AuditActionCreated is recorded when a new user is invited/created by an admin.
	AuditActionCreated AuditAction = "created"

	// AuditActionDeactivated is recorded when a user is deactivated by an admin.
	AuditActionDeactivated AuditAction = "deactivated"

	// AuditActionReactivated is recorded when a previously inactive user is
	// reactivated by an admin.
	AuditActionReactivated AuditAction = "reactivated"
)

// User represents a user identity managed via the Kratos admin API.
// It is a read-only projection — mutations are performed through the
// UserAdmin port, not by modifying this struct directly.
type User struct {
	// ID is the Kratos identity UUID.
	ID string

	// Email is the user's primary email address.
	Email string

	// Status is the current lifecycle state of the user.
	Status Status

	// CreatedAt is when the identity was created in Kratos.
	CreatedAt time.Time
}

// AuditEntry records an administrative action taken on a user identity.
// These entries form an append-only audit trail that VibeWarden can expose
// to the fleet dashboard (Pro tier) for compliance purposes.
type AuditEntry struct {
	// UserID is the Kratos identity UUID of the affected user.
	UserID string

	// Action is the type of administrative action performed.
	Action AuditAction

	// PerformedAt is when the action was recorded.
	PerformedAt time.Time
}
