package user_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/user"
)

// ------------------------------------------------------------------
// ID (value object)
// ------------------------------------------------------------------

func TestNewID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid uuid-like id", "550e8400-e29b-41d4-a716-446655440000", false},
		{"valid short id", "abc-123", false},
		{"empty string", "", true},
		{"whitespace only", "   ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := user.NewID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got.String() != tt.input {
				t.Errorf("NewID(%q).String() = %q, want %q", tt.input, got.String(), tt.input)
			}
		})
	}
}

func TestID_String(t *testing.T) {
	id, err := user.NewID("usr-42")
	if err != nil {
		t.Fatalf("NewID: unexpected error: %v", err)
	}
	if id.String() != "usr-42" {
		t.Errorf("String() = %q, want %q", id.String(), "usr-42")
	}
}

// ------------------------------------------------------------------
// EmailAddress
// ------------------------------------------------------------------

func TestNewEmailAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantAddr string
		wantErr  bool
	}{
		{"valid lowercase", "alice@example.com", "alice@example.com", false},
		{"valid uppercase normalised", "Alice@Example.COM", "alice@example.com", false},
		{"valid with plus", "alice+tag@example.com", "alice+tag@example.com", false},
		{"valid with subdomain", "user@mail.example.co.uk", "user@mail.example.co.uk", false},
		{"empty string", "", "", true},
		{"whitespace only", "   ", "", true},
		{"missing at sign", "notanemail", "", true},
		{"missing domain", "user@", "", true},
		{"missing local part", "@example.com", "", true},
		{"missing tld", "user@example", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := user.NewEmailAddress(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEmailAddress(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got.String() != tt.wantAddr {
				t.Errorf("NewEmailAddress(%q).String() = %q, want %q", tt.input, got.String(), tt.wantAddr)
			}
		})
	}
}

// ------------------------------------------------------------------
// Role
// ------------------------------------------------------------------

func TestNewRole(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantRole user.Role
		wantErr  bool
	}{
		{"admin lowercase", "admin", user.RoleAdmin, false},
		{"member lowercase", "member", user.RoleMember, false},
		{"readonly lowercase", "readonly", user.RoleReadOnly, false},
		{"admin uppercase", "ADMIN", user.RoleAdmin, false},
		{"mixed case member", "Member", user.RoleMember, false},
		{"unknown role", "superuser", user.Role{}, true},
		{"empty string", "", user.Role{}, true},
		{"whitespace only", "  ", user.Role{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := user.NewRole(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRole(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got.String() != tt.wantRole.String() {
				t.Errorf("NewRole(%q).String() = %q, want %q", tt.input, got.String(), tt.wantRole.String())
			}
		})
	}
}

func TestRole_String(t *testing.T) {
	tests := []struct {
		role user.Role
		want string
	}{
		{user.RoleAdmin, "admin"},
		{user.RoleMember, "member"},
		{user.RoleReadOnly, "readonly"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if tt.role.String() != tt.want {
				t.Errorf("Role.String() = %q, want %q", tt.role.String(), tt.want)
			}
		})
	}
}

// ------------------------------------------------------------------
// Status constants
// ------------------------------------------------------------------

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

// ------------------------------------------------------------------
// NewUser constructor
// ------------------------------------------------------------------

func TestNewUser(t *testing.T) {
	id, _ := user.NewID("550e8400-e29b-41d4-a716-446655440000")
	email, _ := user.NewEmailAddress("alice@example.com")
	now := time.Now().UTC().Truncate(time.Second)

	u := user.NewUser(id, email, user.RoleAdmin, now)

	if u.ID != id.String() {
		t.Errorf("ID = %q, want %q", u.ID, id.String())
	}
	if u.Email != email.String() {
		t.Errorf("Email = %q, want %q", u.Email, email.String())
	}
	if u.Status != user.StatusActive {
		t.Errorf("Status = %q, want %q", u.Status, user.StatusActive)
	}
	if u.Role.String() != user.RoleAdmin.String() {
		t.Errorf("Role = %q, want %q", u.Role.String(), user.RoleAdmin.String())
	}
	if !u.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", u.CreatedAt, now)
	}
}

func TestNewUser_DefaultsToActive(t *testing.T) {
	id, _ := user.NewID("id-1")
	email, _ := user.NewEmailAddress("bob@example.com")

	u := user.NewUser(id, email, user.RoleMember, time.Time{})

	if u.Status != user.StatusActive {
		t.Errorf("new user status = %q, want active", u.Status)
	}
}

// ------------------------------------------------------------------
// User.Disable / Enable
// ------------------------------------------------------------------

func TestUser_Disable(t *testing.T) {
	id, _ := user.NewID("id-1")
	email, _ := user.NewEmailAddress("charlie@example.com")
	u := user.NewUser(id, email, user.RoleMember, time.Now())

	if u.Status != user.StatusActive {
		t.Fatalf("pre-condition: expected active, got %q", u.Status)
	}

	u.Disable()

	if u.Status != user.StatusInactive {
		t.Errorf("after Disable(): Status = %q, want inactive", u.Status)
	}
}

func TestUser_Disable_Idempotent(t *testing.T) {
	id, _ := user.NewID("id-1")
	email, _ := user.NewEmailAddress("charlie@example.com")
	u := user.NewUser(id, email, user.RoleMember, time.Now())

	u.Disable()
	u.Disable() // second call must not panic or change state

	if u.Status != user.StatusInactive {
		t.Errorf("after double Disable(): Status = %q, want inactive", u.Status)
	}
}

func TestUser_Enable(t *testing.T) {
	id, _ := user.NewID("id-1")
	email, _ := user.NewEmailAddress("dave@example.com")
	u := user.NewUser(id, email, user.RoleMember, time.Now())
	u.Disable()

	u.Enable()

	if u.Status != user.StatusActive {
		t.Errorf("after Enable(): Status = %q, want active", u.Status)
	}
}

func TestUser_Enable_Idempotent(t *testing.T) {
	id, _ := user.NewID("id-1")
	email, _ := user.NewEmailAddress("dave@example.com")
	u := user.NewUser(id, email, user.RoleMember, time.Now())

	u.Enable()
	u.Enable() // second call must not panic

	if u.Status != user.StatusActive {
		t.Errorf("after double Enable(): Status = %q, want active", u.Status)
	}
}

func TestUser_DisableEnable_Roundtrip(t *testing.T) {
	id, _ := user.NewID("id-1")
	email, _ := user.NewEmailAddress("eve@example.com")
	u := user.NewUser(id, email, user.RoleMember, time.Now())

	u.Disable()
	u.Enable()

	if u.Status != user.StatusActive {
		t.Errorf("after Disable→Enable: Status = %q, want active", u.Status)
	}
}

// ------------------------------------------------------------------
// User.ChangeRole
// ------------------------------------------------------------------

func TestUser_ChangeRole(t *testing.T) {
	tests := []struct {
		name     string
		initial  user.Role
		newRole  user.Role
		expected string
	}{
		{"member to admin", user.RoleMember, user.RoleAdmin, "admin"},
		{"admin to readonly", user.RoleAdmin, user.RoleReadOnly, "readonly"},
		{"readonly to member", user.RoleReadOnly, user.RoleMember, "member"},
		{"same role is idempotent", user.RoleMember, user.RoleMember, "member"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, _ := user.NewID("id-1")
			email, _ := user.NewEmailAddress("frank@example.com")
			u := user.NewUser(id, email, tt.initial, time.Now())

			u.ChangeRole(tt.newRole)

			if u.Role.String() != tt.expected {
				t.Errorf("after ChangeRole: Role = %q, want %q", u.Role.String(), tt.expected)
			}
		})
	}
}

// ------------------------------------------------------------------
// Backward-compat: direct struct literal still compiles
// ------------------------------------------------------------------

func TestUserStruct_DirectLiteral(t *testing.T) {
	// Existing adapters and tests construct user.User directly.
	// This test verifies those call sites continue to compile and work.
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
