// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/user"
)

// AdminService is the application-level interface for the admin user management
// use case. It is satisfied by *app/admin.Service and any test fake.
// The HTTP adapter depends on this interface — never on a concrete type.
type AdminService interface {
	// ListUsers returns a paginated list of users.
	ListUsers(ctx context.Context, pagination Pagination) (*PaginatedUsers, error)

	// GetUser returns a single user by identity UUID.
	GetUser(ctx context.Context, id string) (*user.User, error)

	// InviteUser creates a new user identity and returns a recovery link.
	InviteUser(ctx context.Context, email string, actorID string) (*InviteResult, error)

	// DeactivateUser deactivates the user and emits a domain event.
	DeactivateUser(ctx context.Context, userID string, actorID string, reason string) error
}
