package validate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/app/validate"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestCheckImageTag(t *testing.T) {
	tests := []struct {
		name       string
		cfgName    string
		envContent string
		writeFile  bool
		wantSkip   bool
		wantState  ops.StatusState
		wantFrag   string
	}{
		{
			name:       "drift: .env has wrong image tag",
			cfgName:    "myapp",
			envContent: "VIBEWARDEN_APP_IMAGE=otherapp:v1\n",
			writeFile:  true,
			wantSkip:   false,
			wantState:  ops.StatusFAIL,
			wantFrag:   "otherapp:v1",
		},
		{
			name:       "match: .env has correct image tag",
			cfgName:    "myapp",
			envContent: "VIBEWARDEN_APP_IMAGE=myapp-app:latest\n",
			writeFile:  true,
			wantSkip:   true,
		},
		{
			name:      "no .env: skip",
			cfgName:   "myapp",
			writeFile: false,
			wantSkip:  true,
		},
		{
			name:       "no VIBEWARDEN_APP_IMAGE line: skip",
			cfgName:    "myapp",
			envContent: "SOME_OTHER_VAR=value\nANOTHER=value2\n",
			writeFile:  true,
			wantSkip:   true,
		},
		{
			name:       "commented-out line: skip",
			cfgName:    "myapp",
			envContent: "# VIBEWARDEN_APP_IMAGE=otherapp:v1\n",
			writeFile:  true,
			wantSkip:   true,
		},
		{
			name:       "hint includes vibew bundle --overwrite",
			cfgName:    "myapp",
			envContent: "VIBEWARDEN_APP_IMAGE=oldimage:v2\n",
			writeFile:  true,
			wantSkip:   false,
			wantState:  ops.StatusFAIL,
			wantFrag:   "vibew bundle --overwrite",
		},
		{
			name:       "expected tag shown in message",
			cfgName:    "myapp",
			envContent: "VIBEWARDEN_APP_IMAGE=oldimage:v2\n",
			writeFile:  true,
			wantSkip:   false,
			wantState:  ops.StatusFAIL,
			wantFrag:   "myapp-app:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			// Write a vibewarden.yaml so DeriveProjectName can resolve the
			// project root basename (though cfgName takes precedence when set).
			if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
				t.Fatalf("WriteFile vibewarden.yaml: %v", err)
			}

			if tt.writeFile {
				if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(tt.envContent), 0o600); err != nil {
					t.Fatalf("WriteFile .env: %v", err)
				}
			}

			cfg := &config.Config{Name: tt.cfgName}
			r := validate.CheckImageTag(context.Background(), dir, cfg, false)

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
