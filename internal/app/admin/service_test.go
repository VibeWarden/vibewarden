package admin_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/admin"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeAuditLogger is a hand-written fake that implements ports.AuditLogger.
type fakeAuditLogger struct {
	entries   []ports.AuditEntry
	recordErr error
}

func (f *fakeAuditLogger) RecordEntry(_ context.Context, e ports.AuditEntry) error {
	f.entries = append(f.entries, e)
	return f.recordErr
}

// fakeUserAdmin is a hand-written fake that implements ports.UserAdmin.
type fakeUserAdmin struct {
	listUsersResult *ports.PaginatedUsers
	listUsersErr    error

	getUserResult *user.User
	getUserErr    error

	inviteUserResult *ports.InviteResult
	inviteUserErr    error

	deactivateUserErr error

	// Recorded calls for assertion.
	lastListPagination ports.Pagination
	lastGetUserID      string
	lastInviteEmail    string
	lastDeactivateID   string
}

func (f *fakeUserAdmin) ListUsers(_ context.Context, p ports.Pagination) (*ports.PaginatedUsers, error) {
	f.lastListPagination = p
	return f.listUsersResult, f.listUsersErr
}

func (f *fakeUserAdmin) GetUser(_ context.Context, id string) (*user.User, error) {
	f.lastGetUserID = id
	return f.getUserResult, f.getUserErr
}

func (f *fakeUserAdmin) InviteUser(_ context.Context, email string) (*ports.InviteResult, error) {
	f.lastInviteEmail = email
	return f.inviteUserResult, f.inviteUserErr
}

func (f *fakeUserAdmin) DeactivateUser(_ context.Context, id string) error {
	f.lastDeactivateID = id
	return f.deactivateUserErr
}

// fakeEventLogger is a hand-written fake that implements ports.EventLogger.
type fakeEventLogger struct {
	logged   []events.Event
	logError error
}

func (f *fakeEventLogger) Log(_ context.Context, e events.Event) error {
	f.logged = append(f.logged, e)
	return f.logError
}

func makeUser(id, email string, status user.Status) *user.User {
	return &user.User{
		ID:        id,
		Email:     email,
		Status:    status,
		CreatedAt: time.Now(),
	}
}

func TestService_ListUsers(t *testing.T) {
	tests := []struct {
		name       string
		setupAdmin func() *fakeUserAdmin
		pagination ports.Pagination
		wantErr    bool
		wantLen    int
	}{
		{
			name: "success returns paginated users",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{
					listUsersResult: &ports.PaginatedUsers{
						Users: []user.User{
							*makeUser("id-1", "alice@example.com", user.StatusActive),
							*makeUser("id-2", "bob@example.com", user.StatusActive),
						},
						Total: 2,
					},
				}
			},
			pagination: ports.Pagination{Page: 1, PerPage: 20},
			wantLen:    2,
		},
		{
			name: "empty result",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{
					listUsersResult: &ports.PaginatedUsers{Users: []user.User{}, Total: 0},
				}
			},
			pagination: ports.Pagination{Page: 1, PerPage: 20},
			wantLen:    0,
		},
		{
			name: "port error is wrapped",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{listUsersErr: ports.ErrAdminUnavailable}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := admin.NewService(tt.setupAdmin(), &fakeEventLogger{}, nil)
			result, err := svc.ListUsers(context.Background(), tt.pagination)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ListUsers() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(result.Users) != tt.wantLen {
				t.Errorf("ListUsers() returned %d users, want %d", len(result.Users), tt.wantLen)
			}
		})
	}
}

func TestService_GetUser(t *testing.T) {
	tests := []struct {
		name       string
		setupAdmin func() *fakeUserAdmin
		id         string
		wantErr    bool
		wantEmail  string
	}{
		{
			name: "found returns user",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{getUserResult: makeUser("id-1", "alice@example.com", user.StatusActive)}
			},
			id:        "id-1",
			wantEmail: "alice@example.com",
		},
		{
			name: "not found error is wrapped",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{getUserErr: ports.ErrUserNotFound}
			},
			id:      "id-999",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := admin.NewService(tt.setupAdmin(), &fakeEventLogger{}, nil)
			u, err := svc.GetUser(context.Background(), tt.id)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetUser() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && u.Email != tt.wantEmail {
				t.Errorf("GetUser() email = %q, want %q", u.Email, tt.wantEmail)
			}
		})
	}
}

func TestService_InviteUser(t *testing.T) {
	tests := []struct {
		name        string
		setupAdmin  func() *fakeUserAdmin
		email       string
		logError    error
		wantErr     bool
		wantLogged  int
		wantEventTy string
	}{
		{
			name: "success emits user.created event",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{
					inviteUserResult: &ports.InviteResult{
						User:         *makeUser("id-1", "alice@example.com", user.StatusActive),
						RecoveryLink: "https://kratos.example.com/recovery/link",
					},
				}
			},
			email:       "alice@example.com",
			wantLogged:  1,
			wantEventTy: events.EventTypeUserCreated,
		},
		{
			name: "port error is returned without emitting event",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{inviteUserErr: ports.ErrUserAlreadyExists}
			},
			email:      "existing@example.com",
			wantErr:    true,
			wantLogged: 0,
		},
		{
			name: "log failure does not abort invite",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{
					inviteUserResult: &ports.InviteResult{
						User: *makeUser("id-2", "bob@example.com", user.StatusActive),
					},
				}
			},
			email:    "bob@example.com",
			logError: errors.New("log sink unavailable"),
			// The invite still succeeds despite log failure.
			wantLogged:  1,
			wantEventTy: events.EventTypeUserCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeAdmin := tt.setupAdmin()
			fakeLog := &fakeEventLogger{logError: tt.logError}
			svc := admin.NewService(fakeAdmin, fakeLog, nil)

			result, err := svc.InviteUser(context.Background(), tt.email, "admin-actor-id")
			if (err != nil) != tt.wantErr {
				t.Fatalf("InviteUser() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && result == nil {
				t.Fatal("InviteUser() returned nil result without error")
			}
			if len(fakeLog.logged) != tt.wantLogged {
				t.Errorf("InviteUser() emitted %d events, want %d", len(fakeLog.logged), tt.wantLogged)
			}
			if tt.wantLogged > 0 && fakeLog.logged[0].EventType != tt.wantEventTy {
				t.Errorf("InviteUser() event type = %q, want %q", fakeLog.logged[0].EventType, tt.wantEventTy)
			}
		})
	}
}

func TestService_DeactivateUser(t *testing.T) {
	tests := []struct {
		name              string
		setupAdmin        func() *fakeUserAdmin
		userID            string
		logError          error
		wantErr           bool
		wantLogged        int
		wantEventTy       string
		wantDeactivatedID string
	}{
		{
			name: "success emits user.deactivated event",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{
					getUserResult: makeUser("id-1", "alice@example.com", user.StatusActive),
				}
			},
			userID:            "id-1",
			wantLogged:        1,
			wantEventTy:       events.EventTypeUserDeactivated,
			wantDeactivatedID: "id-1",
		},
		{
			name: "get user failure returns error without deactivating",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{getUserErr: ports.ErrUserNotFound}
			},
			userID:     "id-999",
			wantErr:    true,
			wantLogged: 0,
		},
		{
			name: "deactivate port failure is returned",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{
					getUserResult:     makeUser("id-1", "alice@example.com", user.StatusActive),
					deactivateUserErr: ports.ErrAdminUnavailable,
				}
			},
			userID:     "id-1",
			wantErr:    true,
			wantLogged: 0,
		},
		{
			name: "log failure does not abort deactivate",
			setupAdmin: func() *fakeUserAdmin {
				return &fakeUserAdmin{
					getUserResult: makeUser("id-2", "bob@example.com", user.StatusActive),
				}
			},
			userID:            "id-2",
			logError:          errors.New("log sink unavailable"),
			wantLogged:        1,
			wantEventTy:       events.EventTypeUserDeactivated,
			wantDeactivatedID: "id-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeAdmin := tt.setupAdmin()
			fakeLog := &fakeEventLogger{logError: tt.logError}
			svc := admin.NewService(fakeAdmin, fakeLog, nil)

			err := svc.DeactivateUser(context.Background(), tt.userID, "admin-actor-id", "policy violation")
			if (err != nil) != tt.wantErr {
				t.Fatalf("DeactivateUser() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(fakeLog.logged) != tt.wantLogged {
				t.Errorf("DeactivateUser() emitted %d events, want %d", len(fakeLog.logged), tt.wantLogged)
			}
			if tt.wantLogged > 0 && fakeLog.logged[0].EventType != tt.wantEventTy {
				t.Errorf("DeactivateUser() event type = %q, want %q", fakeLog.logged[0].EventType, tt.wantEventTy)
			}
			if tt.wantDeactivatedID != "" && fakeAdmin.lastDeactivateID != tt.wantDeactivatedID {
				t.Errorf("DeactivateUser() called Deactivate with ID %q, want %q", fakeAdmin.lastDeactivateID, tt.wantDeactivatedID)
			}
		})
	}
}

func TestService_InviteUser_AuditLogging(t *testing.T) {
	tests := []struct {
		name            string
		auditErr        error
		wantAuditCalls  int
		wantAuditAction ports.AuditAction
		wantActorID     string
		wantResultNil   bool
	}{
		{
			name:            "records audit entry on success",
			wantAuditCalls:  1,
			wantAuditAction: ports.AuditActionUserCreated,
			wantActorID:     "admin-xyz",
		},
		{
			name:           "audit failure does not abort invite",
			auditErr:       errors.New("db unavailable"),
			wantAuditCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeAdmin := &fakeUserAdmin{
				inviteUserResult: &ports.InviteResult{
					User: *makeUser("id-1", "alice@example.com", user.StatusActive),
				},
			}
			fakeLog := &fakeEventLogger{}
			fakeAudit := &fakeAuditLogger{recordErr: tt.auditErr}
			svc := admin.NewService(fakeAdmin, fakeLog, fakeAudit)

			result, err := svc.InviteUser(context.Background(), "alice@example.com", "admin-xyz")
			if err != nil {
				t.Fatalf("InviteUser() unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("InviteUser() returned nil result")
			}
			if len(fakeAudit.entries) != tt.wantAuditCalls {
				t.Errorf("audit entries = %d, want %d", len(fakeAudit.entries), tt.wantAuditCalls)
			}
			if tt.wantAuditCalls > 0 {
				entry := fakeAudit.entries[0]
				if entry.Action != ports.AuditActionUserCreated {
					t.Errorf("audit action = %q, want %q", entry.Action, ports.AuditActionUserCreated)
				}
				if entry.ActorID != "admin-xyz" {
					t.Errorf("audit actor_id = %q, want %q", entry.ActorID, "admin-xyz")
				}
				if entry.UserID != "id-1" {
					t.Errorf("audit user_id = %q, want %q", entry.UserID, "id-1")
				}
			}
		})
	}
}

func TestService_DeactivateUser_AuditLogging(t *testing.T) {
	tests := []struct {
		name            string
		auditErr        error
		wantAuditCalls  int
		wantAuditAction ports.AuditAction
	}{
		{
			name:            "records audit entry on success",
			wantAuditCalls:  1,
			wantAuditAction: ports.AuditActionUserDeactivated,
		},
		{
			name:           "audit failure does not abort deactivation",
			auditErr:       errors.New("db unavailable"),
			wantAuditCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeAdmin := &fakeUserAdmin{
				getUserResult: makeUser("id-1", "alice@example.com", user.StatusActive),
			}
			fakeLog := &fakeEventLogger{}
			fakeAudit := &fakeAuditLogger{recordErr: tt.auditErr}
			svc := admin.NewService(fakeAdmin, fakeLog, fakeAudit)

			err := svc.DeactivateUser(context.Background(), "id-1", "admin-xyz", "policy violation")
			if err != nil {
				t.Fatalf("DeactivateUser() unexpected error: %v", err)
			}
			if len(fakeAudit.entries) != tt.wantAuditCalls {
				t.Errorf("audit entries = %d, want %d", len(fakeAudit.entries), tt.wantAuditCalls)
			}
			if tt.wantAuditCalls > 0 {
				entry := fakeAudit.entries[0]
				if entry.Action != ports.AuditActionUserDeactivated {
					t.Errorf("audit action = %q, want %q", entry.Action, ports.AuditActionUserDeactivated)
				}
				if entry.ActorID != "admin-xyz" {
					t.Errorf("audit actor_id = %q, want %q", entry.ActorID, "admin-xyz")
				}
				if entry.UserID != "id-1" {
					t.Errorf("audit user_id = %q, want %q", entry.UserID, "id-1")
				}
			}
		})
	}
}
