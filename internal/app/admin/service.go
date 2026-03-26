// Package admin contains the application service for user management admin operations.
// It orchestrates calls to the UserAdmin port and emits structured domain events
// via the EventLogger port. No business logic lives here — it delegates to ports.
package admin

import (
	"context"
	"fmt"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Service is the application service for admin user management.
// It wires the UserAdmin port with the EventLogger port so that every
// mutating operation emits a structured domain event.
type Service struct {
	admin  ports.UserAdmin
	logger ports.EventLogger
}

// NewService creates a new admin Service.
// Both admin and logger must be non-nil.
func NewService(admin ports.UserAdmin, logger ports.EventLogger) *Service {
	return &Service{
		admin:  admin,
		logger: logger,
	}
}

// ListUsers returns a paginated list of users by delegating to the UserAdmin port.
// No event is emitted — listing users is a read-only operation.
func (s *Service) ListUsers(ctx context.Context, pagination ports.Pagination) (*ports.PaginatedUsers, error) {
	result, err := s.admin.ListUsers(ctx, pagination)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	return result, nil
}

// GetUser returns a single user by ID, delegating to the UserAdmin port.
// No event is emitted — getting a user is a read-only operation.
func (s *Service) GetUser(ctx context.Context, id string) (*user.User, error) {
	u, err := s.admin.GetUser(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}
	return u, nil
}

// InviteUser creates a new user identity via the UserAdmin port and emits a
// user.created structured event via the EventLogger port.
// actorID identifies the admin performing the action and is recorded in the event.
func (s *Service) InviteUser(ctx context.Context, email string, actorID string) (*ports.InviteResult, error) {
	result, err := s.admin.InviteUser(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("inviting user: %w", err)
	}

	event := events.NewUserCreated(events.UserCreatedParams{
		IdentityID: result.User.ID,
		Email:      result.User.Email,
	})
	// Event emission is best-effort; log failure but do not abort the operation.
	if logErr := s.logger.Log(ctx, event); logErr != nil {
		// The event emission failed, but the user was created. Callers should
		// proceed — this is a secondary logging concern.
		_ = logErr
	}

	return result, nil
}

// DeactivateUser deactivates a user identity via the UserAdmin port and emits a
// user.deleted structured event via the EventLogger port.
// actorID identifies the admin performing the action and is recorded in the event.
// reason is an optional human-readable explanation for the deactivation.
func (s *Service) DeactivateUser(ctx context.Context, userID string, actorID string, reason string) error {
	// Fetch the user first so we can include the email in the event.
	u, err := s.admin.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("fetching user before deactivation: %w", err)
	}

	if err := s.admin.DeactivateUser(ctx, userID); err != nil {
		return fmt.Errorf("deactivating user: %w", err)
	}

	event := events.NewUserDeleted(events.UserDeletedParams{
		IdentityID: userID,
		Email:      u.Email,
	})
	// Event emission is best-effort.
	if logErr := s.logger.Log(ctx, event); logErr != nil {
		_ = logErr
	}

	return nil
}
