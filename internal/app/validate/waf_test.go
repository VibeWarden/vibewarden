package validate_test

import (
	"context"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/app/validate"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestCheckWAF(t *testing.T) {
	tests := []struct {
		name               string
		wafEnabled         bool
		wafMode            string
		acknowledgeLogMode bool
		prodOverrideExists bool
		wantSkip           bool
		wantState          ops.StatusState
		wantFrag           string
	}{
		{
			name:               "no prod override: skip",
			wafEnabled:         true,
			wafMode:            "log",
			prodOverrideExists: false,
			wantSkip:           true,
		},
		{
			name:               "WAF disabled: skip",
			wafEnabled:         false,
			wafMode:            "log",
			prodOverrideExists: true,
			wantSkip:           true,
		},
		{
			name:               "WAF enabled with mode block: skip",
			wafEnabled:         true,
			wafMode:            "block",
			prodOverrideExists: true,
			wantSkip:           true,
		},
		{
			name:               "WAF enabled with mode detect: skip",
			wafEnabled:         true,
			wafMode:            "detect",
			prodOverrideExists: true,
			wantSkip:           true,
		},
		{
			name:               "WAF log mode without ack: FAIL",
			wafEnabled:         true,
			wafMode:            "log",
			acknowledgeLogMode: false,
			prodOverrideExists: true,
			wantSkip:           false,
			wantState:          ops.StatusFAIL,
			wantFrag:           "waf.acknowledge_log_mode: true",
		},
		{
			name:               "WAF log mode with ack: OK",
			wafEnabled:         true,
			wafMode:            "log",
			acknowledgeLogMode: true,
			prodOverrideExists: true,
			wantSkip:           false,
			wantState:          ops.StatusOK,
			wantFrag:           "WAF log-mode acknowledged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.WAF.Enabled = tt.wafEnabled
			cfg.WAF.Mode = tt.wafMode
			cfg.WAF.AcknowledgeLogMode = tt.acknowledgeLogMode

			r := validate.CheckWAF(context.Background(), "", cfg, tt.prodOverrideExists)

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
