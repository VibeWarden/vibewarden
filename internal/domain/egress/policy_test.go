package egress_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/egress"
)

func TestPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  egress.Policy
		wantErr bool
	}{
		{
			name:    "allow is valid",
			policy:  egress.PolicyAllow,
			wantErr: false,
		},
		{
			name:    "deny is valid",
			policy:  egress.PolicyDeny,
			wantErr: false,
		},
		{
			name:    "empty is invalid",
			policy:  "",
			wantErr: true,
		},
		{
			name:    "unknown value is invalid",
			policy:  egress.Policy("block"),
			wantErr: true,
		},
		{
			name:    "mixed case is invalid",
			policy:  egress.Policy("Allow"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Policy(%q).Validate() error = %v, wantErr %v", tt.policy, err, tt.wantErr)
			}
		})
	}
}

func TestPolicy_String(t *testing.T) {
	tests := []struct {
		policy egress.Policy
		want   string
	}{
		{egress.PolicyAllow, "allow"},
		{egress.PolicyDeny, "deny"},
	}

	for _, tt := range tests {
		t.Run(string(tt.policy), func(t *testing.T) {
			if got := tt.policy.String(); got != tt.want {
				t.Errorf("Policy.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
