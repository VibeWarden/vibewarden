package validate_test

import (
	"context"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/app/validate"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestCheckACME(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		domain    string
		wantSkip  bool
		wantState ops.StatusState
		wantFrag  string
	}{
		// ACME-compatible domain — no row.
		{
			name:     "letsencrypt with valid public domain: skip",
			provider: "letsencrypt",
			domain:   "example.com",
			wantSkip: true,
		},
		{
			name:     "zerossl with valid domain: skip",
			provider: "zerossl",
			domain:   "api.example.com",
			wantSkip: true,
		},
		// Non-ACME providers — always skip.
		{
			name:     "self-signed with localhost: skip (not ACME provider)",
			provider: "self-signed",
			domain:   "localhost",
			wantSkip: true,
		},
		{
			name:     "external with any domain: skip",
			provider: "external",
			domain:   "192.168.1.1",
			wantSkip: true,
		},
		// Incompatible domains with ACME providers.
		{
			name:      "letsencrypt with localhost: FAIL",
			provider:  "letsencrypt",
			domain:    "localhost",
			wantSkip:  false,
			wantState: ops.StatusFAIL,
			wantFrag:  "localhost",
		},
		{
			name:      "letsencrypt with IPv4: FAIL",
			provider:  "letsencrypt",
			domain:    "192.168.1.1",
			wantSkip:  false,
			wantState: ops.StatusFAIL,
			wantFrag:  "IP literal",
		},
		{
			name:      "letsencrypt with IPv6: FAIL",
			provider:  "letsencrypt",
			domain:    "::1",
			wantSkip:  false,
			wantState: ops.StatusFAIL,
			wantFrag:  "IP literal",
		},
		{
			name:      "letsencrypt with .local domain: FAIL",
			provider:  "letsencrypt",
			domain:    "myapp.local",
			wantSkip:  false,
			wantState: ops.StatusFAIL,
			wantFrag:  ".local",
		},
		{
			name:      "letsencrypt with .test domain: FAIL",
			provider:  "letsencrypt",
			domain:    "myapp.test",
			wantSkip:  false,
			wantState: ops.StatusFAIL,
			wantFrag:  ".test",
		},
		{
			name:      "letsencrypt with .localhost domain: FAIL",
			provider:  "letsencrypt",
			domain:    "myapp.localhost",
			wantSkip:  false,
			wantState: ops.StatusFAIL,
			wantFrag:  ".localhost",
		},
		{
			name:      "letsencrypt-staging with localhost: FAIL",
			provider:  "letsencrypt-staging",
			domain:    "localhost",
			wantSkip:  false,
			wantState: ops.StatusFAIL,
			wantFrag:  "localhost",
		},
		{
			name:      "buypass with .invalid domain: FAIL",
			provider:  "buypass",
			domain:    "dev.invalid",
			wantSkip:  false,
			wantState: ops.StatusFAIL,
			wantFrag:  ".invalid",
		},
		// Empty domain — skip (prior validation already rejects empty domains).
		{
			name:     "letsencrypt with empty domain: skip",
			provider: "letsencrypt",
			domain:   "",
			wantSkip: true,
		},
		// Hint text check.
		{
			name:      "FAIL message mentions self-signed hint",
			provider:  "letsencrypt",
			domain:    "localhost",
			wantSkip:  false,
			wantState: ops.StatusFAIL,
			wantFrag:  "self-signed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.TLS.Provider = tt.provider
			cfg.TLS.Domain = tt.domain
			r := validate.CheckACME(context.Background(), validate.CheckInputs{Cfg: cfg})

			if r.Skip != tt.wantSkip {
				t.Errorf("Skip = %v, want %v (message: %q)", r.Skip, tt.wantSkip, r.Message)
			}
			if tt.wantSkip {
				return
			}
			if r.State != tt.wantState {
				t.Errorf("State = %v, want %v", r.State, tt.wantState)
			}
			if tt.wantFrag != "" && !containsStr(r.Message, tt.wantFrag) {
				t.Errorf("Message = %q, want fragment %q", r.Message, tt.wantFrag)
			}
		})
	}
}
