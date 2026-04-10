// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/user"
)

// Pagination holds the parameters for a paginated list request.
type Pagination struct {
	// Page is the 1-based page number. Zero and negative values are treated as 1.
	Page int

	// PerPage is the number of items per page. Zero is treated as the
	// implementation's default (typically 25). Implementations may cap this
	// at a maximum value.
	PerPage int
}

// PaginatedUsers is the result of a paginated ListUsers request.
type PaginatedUsers struct {
	// Users is the slice of users for the requested page.
	Users []user.User

	// Total is the total number of users across all pages.
	// Implementations that cannot determine the total may set this to -1.
	Total int
}

// InviteResult holds the data returned after a successful InviteUser call.
type InviteResult struct {
	// User is the newly created user identity.
	User user.User

	// RecoveryLink is a one-time link the admin can send to the invited user
	// so they can set their password without needing an email from Kratos.
	// It is empty when the identity provider does not support recovery links.
	RecoveryLink string
}

// UserAdmin is the port for administrative user management operations.
// Implementations communicate with the identity provider's admin API.
//
// Error semantics shared by all methods:
//   - Returns ErrAdminUnavailable when the identity provider cannot be reached
//     or returns a server-side (5xx) error.
//   - All methods accept a context.Context as the first argument; callers should
//     supply a context with an appropriate deadline.
type UserAdmin interface {
	// ListUsers returns a paginated list of all user identities.
	// Returns ErrAdminUnavailable when the admin API cannot be reached.
	ListUsers(ctx context.Context, pagination Pagination) (*PaginatedUsers, error)

	// GetUser returns a single user identity by its UUID.
	// Returns ErrUserNotFound when no identity with the given ID exists.
	// Returns ErrInvalidUUID when the supplied id is not a valid UUID.
	GetUser(ctx context.Context, id string) (*user.User, error)

	// InviteUser creates a new user identity for the given email address and
	// returns a one-time recovery link the admin can forward to the new user.
	// Returns ErrUserAlreadyExists when the email is already registered.
	// Returns ErrInvalidEmail when the email format is invalid.
	InviteUser(ctx context.Context, email string) (*InviteResult, error)

	// DeactivateUser sets the user's state to inactive, preventing further
	// authentication. The identity record is retained.
	// Returns ErrUserNotFound when no identity with the given ID exists.
	// Returns ErrInvalidUUID when the supplied id is not a valid UUID.
	DeactivateUser(ctx context.Context, id string) error
}
