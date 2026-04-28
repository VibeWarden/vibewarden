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

func TestCheckDockerfile(t *testing.T) {
	tests := []struct {
		name           string
		dockerfileBody string
		upstreamPort   int
		writeFile      bool
		wantSkip       bool
		wantState      ops.StatusState
		wantFrag       string
	}{
		{
			name:           "mismatch: EXPOSE 3000 but upstream.port 8080",
			dockerfileBody: "FROM alpine\nEXPOSE 3000\nCMD [\"./app\"]\n",
			upstreamPort:   8080,
			writeFile:      true,
			wantSkip:       false,
			wantState:      ops.StatusFAIL,
			wantFrag:       "3000",
		},
		{
			name:           "match: EXPOSE 8080 and upstream.port 8080",
			dockerfileBody: "FROM alpine\nEXPOSE 8080\n",
			upstreamPort:   8080,
			writeFile:      true,
			wantSkip:       true,
		},
		{
			name:         "no Dockerfile: skip",
			upstreamPort: 8080,
			writeFile:    false,
			wantSkip:     true,
		},
		{
			name:           "malformed EXPOSE (non-integer): skip",
			dockerfileBody: "FROM alpine\nEXPOSE notaport\n",
			upstreamPort:   8080,
			writeFile:      true,
			wantSkip:       true,
		},
		{
			name:           "multiple EXPOSE: use last",
			dockerfileBody: "FROM alpine\nEXPOSE 3000\nEXPOSE 8080\n",
			upstreamPort:   3000,
			writeFile:      true,
			wantSkip:       false,
			wantState:      ops.StatusFAIL,
			wantFrag:       "8080",
		},
		{
			name:           "EXPOSE with protocol suffix: strip and use port",
			dockerfileBody: "FROM alpine\nEXPOSE 3000/tcp\n",
			upstreamPort:   8080,
			writeFile:      true,
			wantSkip:       false,
			wantState:      ops.StatusFAIL,
			wantFrag:       "3000",
		},
		{
			name:           "EXPOSE with protocol suffix matching upstream: skip",
			dockerfileBody: "FROM alpine\nEXPOSE 8080/tcp\n",
			upstreamPort:   8080,
			writeFile:      true,
			wantSkip:       true,
		},
		{
			name:           "no EXPOSE line at all: skip",
			dockerfileBody: "FROM alpine\nRUN echo hello\n",
			upstreamPort:   8080,
			writeFile:      true,
			wantSkip:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.writeFile {
				if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(tt.dockerfileBody), 0o600); err != nil {
					t.Fatalf("WriteFile Dockerfile: %v", err)
				}
			}

			cfg := &config.Config{}
			cfg.Upstream.Port = tt.upstreamPort
			r := validate.CheckDockerfile(context.Background(), validate.CheckInputs{ProjectRoot: dir, Cfg: cfg})

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
