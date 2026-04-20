package identity

import "errors"

// SessionInfo is an immutable value object representing the publicly-visible
// attributes of an authenticated user session. It is returned by the
// /_vibewarden/me endpoint so that client-side code can inspect the current
// user without calling the identity provider directly.
//
// SessionInfo contains only the subset of session data that is safe to expose
// to the browser: user ID, email, verification status, and role.
type SessionInfo struct {
	id       string
	email    string
	verified bool
	role     Role
}

// NewSessionInfo creates a SessionInfo with the given attributes.
// Returns an error if required fields are invalid.
func NewSessionInfo(id, email string, verified bool, role Role) (SessionInfo, error) {
	if id == "" {
		return SessionInfo{}, errors.New("session info id cannot be empty")
	}
	if role.IsZero() {
		return SessionInfo{}, errors.New("session info role cannot be zero")
	}
	return SessionInfo{
		id:       id,
		email:    email,
		verified: verified,
		role:     role,
	}, nil
}

// ID returns the user's unique identifier.
func (s SessionInfo) ID() string { return s.id }

// Email returns the user's email address. May be empty.
func (s SessionInfo) Email() string { return s.email }

// Verified returns true if the user's email has been verified.
func (s SessionInfo) Verified() bool { return s.verified }

// Role returns the user's role.
func (s SessionInfo) Role() Role { return s.role }

// IsZero reports whether this is the zero value (no session info).
func (s SessionInfo) IsZero() bool { return s.id == "" }
