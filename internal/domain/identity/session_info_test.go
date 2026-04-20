package identity

import "testing"

func TestNewSessionInfo(t *testing.T) {
	adminRole, _ := NewRole("admin")
	userRole := DefaultRole()

	tests := []struct {
		name     string
		id       string
		email    string
		verified bool
		role     Role
		wantErr  bool
	}{
		{
			name:     "valid with all fields",
			id:       "usr-123",
			email:    "user@example.com",
			verified: true,
			role:     adminRole,
			wantErr:  false,
		},
		{
			name:     "valid with empty email",
			id:       "usr-456",
			email:    "",
			verified: false,
			role:     userRole,
			wantErr:  false,
		},
		{
			name:     "empty id",
			id:       "",
			email:    "user@example.com",
			verified: true,
			role:     adminRole,
			wantErr:  true,
		},
		{
			name:     "zero role",
			id:       "usr-789",
			email:    "user@example.com",
			verified: true,
			role:     Role{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := NewSessionInfo(tt.id, tt.email, tt.verified, tt.role)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSessionInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if info.ID() != tt.id {
				t.Errorf("ID() = %q, want %q", info.ID(), tt.id)
			}
			if info.Email() != tt.email {
				t.Errorf("Email() = %q, want %q", info.Email(), tt.email)
			}
			if info.Verified() != tt.verified {
				t.Errorf("Verified() = %v, want %v", info.Verified(), tt.verified)
			}
			if info.Role() != tt.role {
				t.Errorf("Role() = %v, want %v", info.Role(), tt.role)
			}
		})
	}
}

func TestSessionInfo_IsZero(t *testing.T) {
	tests := []struct {
		name string
		info SessionInfo
		want bool
	}{
		{
			name: "zero value",
			info: SessionInfo{},
			want: true,
		},
		{
			name: "non-zero",
			info: func() SessionInfo {
				r := DefaultRole()
				s, _ := NewSessionInfo("usr-1", "a@b.com", true, r)
				return s
			}(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}
