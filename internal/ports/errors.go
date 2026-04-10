package ports

import "errors"

var (
	// ErrSessionInvalid is returned when a session cookie is invalid or expired.
	ErrSessionInvalid = errors.New("session invalid or expired")

	// ErrSessionNotFound is returned when no session exists for the given cookie.
	ErrSessionNotFound = errors.New("session not found")

	// ErrAuthProviderUnavailable is returned when the auth provider cannot be reached.
	ErrAuthProviderUnavailable = errors.New("auth provider unavailable")

	// ErrAdminUnavailable is returned when the Kratos admin API cannot be reached
	// or returns an unexpected server-side error.
	ErrAdminUnavailable = errors.New("admin API unavailable")

	// ErrUserNotFound is returned when the requested user identity does not exist
	// in the identity provider.
	ErrUserNotFound = errors.New("user not found")

	// ErrUserAlreadyExists is returned when an attempt is made to create a user
	// whose email address is already registered in the identity provider.
	ErrUserAlreadyExists = errors.New("user already exists")

	// ErrInvalidEmail is returned when the supplied email address fails basic
	// format validation before the request reaches the identity provider.
	ErrInvalidEmail = errors.New("invalid email address")

	// ErrInvalidUUID is returned when the supplied user ID is not a valid UUID.
	ErrInvalidUUID = errors.New("invalid user UUID")
)
