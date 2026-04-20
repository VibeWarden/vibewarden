package identity

import "fmt"

// Role is a value object representing a user's role within the application.
// It is immutable and validated at construction time.
//
// Accepted values are "user", "admin", and "moderator". The zero value is
// not a valid Role; use NewRole or DefaultRole to create instances.
type Role struct{ value string }

const (
	roleUser      = "user"
	roleAdmin     = "admin"
	roleModerator = "moderator"
)

// validRoles is the set of accepted role strings.
var validRoles = map[string]bool{
	roleUser:      true,
	roleAdmin:     true,
	roleModerator: true,
}

// NewRole creates a Role from the given string. Returns an error if the
// value is not one of the accepted roles: "user", "admin", "moderator".
func NewRole(s string) (Role, error) {
	if !validRoles[s] {
		return Role{}, fmt.Errorf("invalid role %q: accepted values are user, admin, moderator", s)
	}
	return Role{value: s}, nil
}

// DefaultRole returns the default role ("user").
func DefaultRole() Role {
	return Role{value: roleUser}
}

// String returns the string representation of the role.
func (r Role) String() string { return r.value }

// IsZero reports whether this is the zero-value Role (uninitialised).
func (r Role) IsZero() bool { return r.value == "" }
