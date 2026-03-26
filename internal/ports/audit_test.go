package ports_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestAuditActionConstants(t *testing.T) {
	tests := []struct {
		name  string
		value ports.AuditAction
		want  string
	}{
		{"user created", ports.AuditActionUserCreated, "user.created"},
		{"user deactivated", ports.AuditActionUserDeactivated, "user.deactivated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("AuditAction = %q, want %q", string(tt.value), tt.want)
			}
		})
	}
}

func TestAuditEntryStruct(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	entry := ports.AuditEntry{
		UserID:    "abc-123",
		Action:    ports.AuditActionUserDeactivated,
		ActorID:   "admin-456",
		Timestamp: now,
		Metadata:  map[string]any{"reason": "policy violation"},
	}

	if entry.UserID != "abc-123" {
		t.Errorf("UserID = %q, want %q", entry.UserID, "abc-123")
	}
	if entry.Action != ports.AuditActionUserDeactivated {
		t.Errorf("Action = %q, want %q", entry.Action, ports.AuditActionUserDeactivated)
	}
	if entry.ActorID != "admin-456" {
		t.Errorf("ActorID = %q, want %q", entry.ActorID, "admin-456")
	}
	if !entry.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", entry.Timestamp, now)
	}
	if entry.Metadata["reason"] != "policy violation" {
		t.Errorf("Metadata[reason] = %v, want %q", entry.Metadata["reason"], "policy violation")
	}
}

func TestAuditEntry_ZeroTimestamp(t *testing.T) {
	// When Timestamp is zero, the adapter must set it. This test verifies that
	// the zero value is distinguishable.
	entry := ports.AuditEntry{UserID: "u-1", Action: ports.AuditActionUserCreated}
	if !entry.Timestamp.IsZero() {
		t.Errorf("expected zero Timestamp, got %v", entry.Timestamp)
	}
}
