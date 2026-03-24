package ports

import "errors"

var (
	// ErrSessionInvalid is returned when a session cookie is invalid or expired.
	ErrSessionInvalid = errors.New("session invalid or expired")

	// ErrSessionNotFound is returned when no session exists for the given cookie.
	ErrSessionNotFound = errors.New("session not found")

	// ErrAuthProviderUnavailable is returned when the auth provider cannot be reached.
	ErrAuthProviderUnavailable = errors.New("auth provider unavailable")
)
