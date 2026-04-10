// Package user defines the core domain types for VibeWarden user management.
// This package has zero external dependencies — only the Go standard library.
package user

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ------------------------------------------------------------------
// Value objects
// ------------------------------------------------------------------

// ID is an immutable value object wrapping a Kratos identity UUID.
// The zero value is invalid; always construct via NewID.
type ID struct{ id string }

// NewID constructs an ID, returning an error when id is empty.
func NewID(id string) (ID, error) {
	if strings.TrimSpace(id) == "" {
		return ID{}, errors.New("user id cannot be empty")
	}
	return ID{id: id}, nil
}

// String returns the raw string representation of the ID.
func (u ID) String() string { return u.id }

// emailRegexp is a simple RFC-5321-compatible email pattern.
// It intentionally avoids the full complexity of RFC 5321 to keep the
// domain layer dependency-free. Exotic formats (quoted strings, IP
// literals) are rejected — this is acceptable for VibeWarden's user base.
var emailRegexp = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// EmailAddress is an immutable value object wrapping a validated, lower-cased
// email address. The zero value is invalid; always construct via NewEmailAddress.
type EmailAddress struct{ address string }

// NewEmailAddress constructs an EmailAddress, normalising the address to
// lower-case and returning an error when the format is invalid.
func NewEmailAddress(raw string) (EmailAddress, error) {
	normalised := strings.ToLower(strings.TrimSpace(raw))
	if normalised == "" {
		return EmailAddress{}, errors.New("email address cannot be empty")
	}
	if !emailRegexp.MatchString(normalised) {
		return EmailAddress{}, fmt.Errorf("email address %q is not valid", normalised)
	}
	return EmailAddress{address: normalised}, nil
}

// String returns the normalised email address.
func (e EmailAddress) String() string { return e.address }

// Role represents the access level granted to a user within VibeWarden.
type Role struct{ name string }

var (
	// RoleAdmin grants full administrative access to VibeWarden management APIs.
	RoleAdmin = Role{name: "admin"}

	// RoleMember grants standard authenticated access with no admin privileges.
	RoleMember = Role{name: "member"}

	// RoleReadOnly grants read-only access to resources.
	RoleReadOnly = Role{name: "readonly"}
)

// validRoles is the set of roles that NewRole accepts.
var validRoles = map[string]Role{
	RoleAdmin.name:    RoleAdmin,
	RoleMember.name:   RoleMember,
	RoleReadOnly.name: RoleReadOnly,
}

// NewRole constructs a Role from its string name, returning an error when the
// name does not match a known role.
func NewRole(name string) (Role, error) {
	r, ok := validRoles[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Role{}, fmt.Errorf("unknown role %q: must be one of admin, member, readonly", name)
	}
	return r, nil
}

// String returns the role name.
func (r Role) String() string { return r.name }

// ------------------------------------------------------------------
// Status
// ------------------------------------------------------------------

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

// ------------------------------------------------------------------
// User entity
// ------------------------------------------------------------------

// User represents a user identity managed via the Kratos admin API.
//
// The ID, Email, and Status fields retain their string/Status types for
// compatibility with existing adapters and serialisation code. Use NewUser
// to create a validated instance. Mutations must go through the Disable,
// Enable, and ChangeRole methods — never mutate fields directly.
type User struct {
	// ID is the Kratos identity UUID.
	ID string

	// Email is the user's primary email address (lower-cased).
	Email string

	// Status is the current lifecycle state of the user.
	Status Status

	// Role is the access level granted to the user.
	Role Role

	// CreatedAt is when the identity was created in Kratos.
	CreatedAt time.Time
}

// NewUser constructs a User entity after validating all inputs.
// id must be non-empty. email must be a valid email address.
// role must be one of the known roles (admin, member, readonly).
// createdAt is the identity creation timestamp from the identity provider;
// pass time.Time{} when unknown and it will be set to the zero value.
func NewUser(id ID, email EmailAddress, role Role, createdAt time.Time) User {
	return User{
		ID:        id.String(),
		Email:     email.String(),
		Status:    StatusActive,
		Role:      role,
		CreatedAt: createdAt,
	}
}

// Disable transitions the user to StatusInactive, preventing further
// authentication. It is idempotent — calling Disable on an already-inactive
// user is a no-op.
func (u *User) Disable() {
	u.Status = StatusInactive
}

// Enable transitions the user to StatusActive, restoring their ability to
// authenticate. It is idempotent — calling Enable on an already-active user
// is a no-op.
func (u *User) Enable() {
	u.Status = StatusActive
}

// ChangeRole updates the user's role to the given value.
func (u *User) ChangeRole(r Role) {
	u.Role = r
}
