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

