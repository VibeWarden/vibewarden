package admin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/admin"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestService_InviteUser_AuditFailure_EmitsAuditLogFailureEvent verifies that
// when the audit store is unavailable during InviteUser, an audit.log_failure
// structured event is emitted so the failure is observable in the event stream.
// The invite result must still be returned successfully.
func TestService_InviteUser_AuditFailure_EmitsAuditLogFailureEvent(t *testing.T) {
	fakeAdmin := &fakeUserAdmin{
		inviteUserResult: &ports.InviteResult{
			User: user.User{ID: "user-1", Email: "alice@example.com"},
		},
	}
	fakeLog := &fakeEventLogger{}
	fakeAudit := &fakeAuditLogger{recordErr: errors.New("postgres: connection refused")}

	svc := admin.NewService(fakeAdmin, fakeLog, fakeAudit)
	result, err := svc.InviteUser(context.Background(), "alice@example.com", "admin-1")
	if err != nil {
		t.Fatalf("InviteUser() returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("InviteUser() returned nil result, want non-nil")
	}

	// Check that an audit.log_failure event was emitted.
	var found bool
	for _, ev := range fakeLog.logged {
		if ev.EventType == events.EventTypeAuditLogFailure {
			found = true
			// Verify payload.
			if action, ok := ev.Payload["action"].(string); !ok || action == "" {
				t.Error("audit.log_failure payload missing non-empty action")
			}
			if userID, ok := ev.Payload["user_id"].(string); !ok || userID == "" {
				t.Error("audit.log_failure payload missing non-empty user_id")
			}
			if errMsg, ok := ev.Payload["error"].(string); !ok || errMsg == "" {
				t.Error("audit.log_failure payload missing non-empty error")
			}
		}
	}
	if !found {
		t.Error("expected audit.log_failure event when audit store is unavailable, got none")
	}
}

// TestService_DeactivateUser_AuditFailure_EmitsAuditLogFailureEvent verifies
// that when the audit store is unavailable during DeactivateUser, an
// audit.log_failure event is emitted. The deactivation must still succeed.
func TestService_DeactivateUser_AuditFailure_EmitsAuditLogFailureEvent(t *testing.T) {
	fakeAdmin := &fakeUserAdmin{
		getUserResult: &user.User{ID: "user-1", Email: "bob@example.com"},
	}
	fakeLog := &fakeEventLogger{}
	fakeAudit := &fakeAuditLogger{recordErr: errors.New("postgres: connection refused")}

	svc := admin.NewService(fakeAdmin, fakeLog, fakeAudit)
	err := svc.DeactivateUser(context.Background(), "user-1", "admin-1", "spam")
	if err != nil {
		t.Fatalf("DeactivateUser() returned unexpected error: %v", err)
	}

	var found bool
	for _, ev := range fakeLog.logged {
		if ev.EventType == events.EventTypeAuditLogFailure {
			found = true
			if action, ok := ev.Payload["action"].(string); !ok || action == "" {
				t.Error("audit.log_failure payload missing non-empty action")
			}
		}
	}
	if !found {
		t.Error("expected audit.log_failure event when audit store is unavailable, got none")
	}
}

// TestService_InviteUser_NoAuditFailureEvent_WhenAuditSucceeds verifies that
// no audit.log_failure event is emitted when audit logging succeeds.
func TestService_InviteUser_NoAuditFailureEvent_WhenAuditSucceeds(t *testing.T) {
	fakeAdmin := &fakeUserAdmin{
		inviteUserResult: &ports.InviteResult{
			User: user.User{ID: "user-2", Email: "carol@example.com"},
		},
	}
	fakeLog := &fakeEventLogger{}
	fakeAudit := &fakeAuditLogger{} // no error

	svc := admin.NewService(fakeAdmin, fakeLog, fakeAudit)
	if _, err := svc.InviteUser(context.Background(), "carol@example.com", "admin-1"); err != nil {
		t.Fatalf("InviteUser() error: %v", err)
	}

	for _, ev := range fakeLog.logged {
		if ev.EventType == events.EventTypeAuditLogFailure {
			t.Errorf("unexpected audit.log_failure event when audit succeeded: %v", ev)
		}
	}
}

// TestService_InviteUser_NilAudit_NoAuditFailureEvent verifies that when no
// audit logger is configured, no audit.log_failure event is emitted.
func TestService_InviteUser_NilAudit_NoAuditFailureEvent(t *testing.T) {
	fakeAdmin := &fakeUserAdmin{
		inviteUserResult: &ports.InviteResult{
			User: user.User{ID: "user-3", Email: "dave@example.com"},
		},
	}
	fakeLog := &fakeEventLogger{}

	svc := admin.NewService(fakeAdmin, fakeLog, nil)
	if _, err := svc.InviteUser(context.Background(), "dave@example.com", "admin-1"); err != nil {
		t.Fatalf("InviteUser() error: %v", err)
	}

	for _, ev := range fakeLog.logged {
		if ev.EventType == events.EventTypeAuditLogFailure {
			t.Errorf("unexpected audit.log_failure event when audit is nil: %v", ev)
		}
	}
}
