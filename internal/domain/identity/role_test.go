package identity

import "testing"

func TestNewRole(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"valid user", "user", "user", false},
		{"valid admin", "admin", "admin", false},
		{"valid moderator", "moderator", "moderator", false},
		{"empty string", "", "", true},
		{"unknown role", "superadmin", "", true},
		{"uppercase", "Admin", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRole(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRole(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && r.String() != tt.want {
				t.Errorf("NewRole(%q).String() = %q, want %q", tt.input, r.String(), tt.want)
			}
		})
	}
}

func TestDefaultRole(t *testing.T) {
	r := DefaultRole()
	if r.String() != "user" {
		t.Errorf("DefaultRole().String() = %q, want %q", r.String(), "user")
	}
	if r.IsZero() {
		t.Error("DefaultRole().IsZero() = true, want false")
	}
}

func TestRole_IsZero(t *testing.T) {
	var r Role
	if !r.IsZero() {
		t.Error("zero-value Role.IsZero() = false, want true")
	}
}
