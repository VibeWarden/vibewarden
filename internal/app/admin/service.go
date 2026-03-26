// Package admin contains the application service for user management admin operations.
// It orchestrates calls to the UserAdmin port and emits structured domain events
// via the EventLogger port. No business logic lives here — it delegates to ports.
package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Service is the application service for admin user management.
// It wires the UserAdmin port with the EventLogger and AuditLogger ports so
// that every mutating operation emits a structured domain event and persists
// an audit log entry.
type Service struct {
	admin  ports.UserAdmin
	logger ports.EventLogger
	audit  ports.AuditLogger
}

// NewService creates a new admin Service.
// admin and logger must be non-nil. audit may be nil, in which case audit
// logging is skipped (useful for tests that pre-date audit support).
func NewService(admin ports.UserAdmin, logger ports.EventLogger, audit ports.AuditLogger) *Service {
	return &Service{
		admin:  admin,
		logger: logger,
		audit:  audit,
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
// user.created structured event via the EventLogger port and persists an
// audit entry via the AuditLogger port.
// actorID identifies the admin performing the action and is recorded in both
// the event and the audit entry.
func (s *Service) InviteUser(ctx context.Context, email string, actorID string) (*ports.InviteResult, error) {
	result, err := s.admin.InviteUser(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("inviting user: %w", err)
	}

	event := events.NewUserCreated(events.UserCreatedParams{
		IdentityID: result.User.ID,
		Email:      result.User.Email,
		ActorID:    actorID,
	})
	// Event emission is best-effort; log failure but do not abort the operation.
	if logErr := s.logger.Log(ctx, event); logErr != nil {
		_ = logErr
	}

	// Audit logging is best-effort; a failure here must not abort the invite.
	if s.audit != nil {
		auditErr := s.audit.RecordEntry(ctx, ports.AuditEntry{
			UserID:    result.User.ID,
			Action:    ports.AuditActionUserCreated,
			ActorID:   actorID,
			Timestamp: time.Now().UTC(),
			Metadata: map[string]any{
				"email": result.User.Email,
			},
		})
		_ = auditErr
	}

	return result, nil
}

// DeactivateUser deactivates a user identity via the UserAdmin port and emits a
// user.deactivated structured event via the EventLogger port and persists an
// audit entry via the AuditLogger port.
// actorID identifies the admin performing the action and is recorded in both
// the event and the audit entry.
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

	event := events.NewUserDeactivated(events.UserDeactivatedParams{
		IdentityID: userID,
		Email:      u.Email,
		ActorID:    actorID,
		Reason:     reason,
	})
	// Event emission is best-effort.
	if logErr := s.logger.Log(ctx, event); logErr != nil {
		_ = logErr
	}

	// Audit logging is best-effort.
	if s.audit != nil {
		auditErr := s.audit.RecordEntry(ctx, ports.AuditEntry{
			UserID:    userID,
			Action:    ports.AuditActionUserDeactivated,
			ActorID:   actorID,
			Timestamp: time.Now().UTC(),
			Metadata: map[string]any{
				"email":  u.Email,
				"reason": reason,
			},
		})
		_ = auditErr
	}

	return nil
}
