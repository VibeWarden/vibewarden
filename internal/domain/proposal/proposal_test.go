package proposal_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/proposal"
)

func TestProposalIsExpired(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresAt time.Time
		checkAt   time.Time
		want      bool
	}{
		{
			name:      "not yet expired",
			expiresAt: now.Add(time.Hour),
			checkAt:   now,
			want:      false,
		},
		{
			name:      "expired one second ago",
			expiresAt: now.Add(-time.Second),
			checkAt:   now,
			want:      true,
		},
		{
			name:      "expires exactly now (not expired)",
			expiresAt: now,
			checkAt:   now,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &proposal.Proposal{
				ExpiresAt: tt.expiresAt,
			}
			got := p.IsExpired(tt.checkAt)
			if got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestActionTypeConstants(t *testing.T) {
	tests := []struct {
		name  string
		value proposal.ActionType
		want  string
	}{
		{"block_ip", proposal.ActionBlockIP, "block_ip"},
		{"adjust_rate_limit", proposal.ActionAdjustRateLimit, "adjust_rate_limit"},
		{"update_config", proposal.ActionUpdateConfig, "update_config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("ActionType = %q, want %q", tt.value, tt.want)
			}
		})
	}
}

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name  string
		value proposal.Status
		want  string
	}{
		{"pending", proposal.StatusPending, "pending"},
		{"approved", proposal.StatusApproved, "approved"},
		{"dismissed", proposal.StatusDismissed, "dismissed"},
		{"expired", proposal.StatusExpired, "expired"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("Status = %q, want %q", tt.value, tt.want)
			}
		})
	}
}

func TestDefaultTTL(t *testing.T) {
	if proposal.DefaultTTL != time.Hour {
		t.Errorf("DefaultTTL = %v, want 1h", proposal.DefaultTTL)
	}
}
