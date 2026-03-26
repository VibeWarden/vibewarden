package user_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/user"
)

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name  string
		value user.Status
		want  string
	}{
		{"active", user.StatusActive, "active"},
		{"inactive", user.StatusInactive, "inactive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("Status = %q, want %q", string(tt.value), tt.want)
			}
		})
	}
}

func TestAuditActionConstants(t *testing.T) {
	tests := []struct {
		name  string
		value user.AuditAction
		want  string
	}{
		{"created", user.AuditActionCreated, "created"},
		{"deactivated", user.AuditActionDeactivated, "deactivated"},
		{"reactivated", user.AuditActionReactivated, "reactivated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("AuditAction = %q, want %q", string(tt.value), tt.want)
			}
		})
	}
}

func TestUserStruct(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	u := user.User{
		ID:        "abc-123",
		Email:     "admin@example.com",
		Status:    user.StatusActive,
		CreatedAt: now,
	}

	if u.ID != "abc-123" {
		t.Errorf("ID = %q, want %q", u.ID, "abc-123")
	}
	if u.Email != "admin@example.com" {
		t.Errorf("Email = %q, want %q", u.Email, "admin@example.com")
	}
	if u.Status != user.StatusActive {
		t.Errorf("Status = %q, want %q", u.Status, user.StatusActive)
	}
	if !u.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", u.CreatedAt, now)
	}
}

func TestAuditEntryStruct(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	entry := user.AuditEntry{
		UserID:      "abc-123",
		Action:      user.AuditActionDeactivated,
		PerformedAt: now,
	}

	if entry.UserID != "abc-123" {
		t.Errorf("UserID = %q, want %q", entry.UserID, "abc-123")
	}
	if entry.Action != user.AuditActionDeactivated {
		t.Errorf("Action = %q, want %q", entry.Action, user.AuditActionDeactivated)
	}
	if !entry.PerformedAt.Equal(now) {
		t.Errorf("PerformedAt = %v, want %v", entry.PerformedAt, now)
	}
}
