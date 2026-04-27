package validate_test

import (
	"context"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/app/validate"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestCheckName(t *testing.T) {
	tests := []struct {
		name        string
		cfgName     string
		projectRoot string
		wantSkip    bool
		wantState   ops.StatusState
		wantFrag    string
	}{
		{
			name:        "collision: name empty and dir is vibewarden",
			cfgName:     "",
			projectRoot: "/home/user/vibewarden",
			wantSkip:    false,
			wantState:   ops.StatusFAIL,
			wantFrag:    "vibewarden",
		},
		{
			name:        "no collision: name empty but dir is myapp",
			cfgName:     "",
			projectRoot: "/home/user/myapp",
			wantSkip:    true,
		},
		{
			name:        "no collision: name set, dir is vibewarden",
			cfgName:     "myproject",
			projectRoot: "/home/user/vibewarden",
			wantSkip:    true,
		},
		{
			name:        "no collision: name set, dir is also myapp",
			cfgName:     "myapp",
			projectRoot: "/home/user/myapp",
			wantSkip:    true,
		},
		{
			name:        "skip when projectRoot is empty",
			cfgName:     "",
			projectRoot: "",
			wantSkip:    true,
		},
		{
			name:        "no collision: vibewarden-app dir (not exact match)",
			cfgName:     "",
			projectRoot: "/home/user/vibewarden-app",
			wantSkip:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Name: tt.cfgName}
			r := validate.CheckName(context.Background(), tt.projectRoot, cfg, false)

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
