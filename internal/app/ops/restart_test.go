package ops_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeComposeRunner is a simple fake that records the arguments passed to Restart.
type fakeComposeRunner struct {
	// restartComposeFile is the composeFile argument received by the last Restart call.
	restartComposeFile string
	// restartServices is the services argument received by the last Restart call.
	restartServices []string
	// restartErr is the error Restart should return, if any.
	restartErr error
}

func (f *fakeComposeRunner) Up(_ context.Context, _ string, _ []string) error {
	return nil
}

func (f *fakeComposeRunner) Restart(_ context.Context, composeFile string, services []string) error {
	f.restartComposeFile = composeFile
	f.restartServices = services
	return f.restartErr
}

func (f *fakeComposeRunner) Version(_ context.Context) (string, error) {
	return "Docker Compose version v2.x", nil
}

func (f *fakeComposeRunner) Info(_ context.Context) error {
	return nil
}

func (f *fakeComposeRunner) PS(_ context.Context, _ string) ([]ports.ContainerInfo, error) {
	return nil, nil
}

func TestRestartService_Run(t *testing.T) {
	wantComposeFile := filepath.Join(".vibewarden", "generated", "docker-compose.yml")

	tests := []struct {
		name       string
		services   []string
		restartErr error
		wantErr    bool
		wantOutput string
	}{
		{
			name:       "restart all services",
			services:   nil,
			wantOutput: "Restarting all services",
		},
		{
			name:       "restart single service",
			services:   []string{"app"},
			wantOutput: "app",
		},
		{
			name:       "restart multiple services",
			services:   []string{"app", "kratos"},
			wantOutput: "app",
		},
		{
			name:       "compose error is propagated",
			services:   nil,
			restartErr: errors.New("docker daemon not running"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeComposeRunner{restartErr: tt.restartErr}
			svc := opsapp.NewRestartService(fake)

			var out strings.Builder
			err := svc.Run(context.Background(), tt.services, &out)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			// Verify compose file path.
			if fake.restartComposeFile != wantComposeFile {
				t.Errorf("composeFile = %q, want %q", fake.restartComposeFile, wantComposeFile)
			}

			// Verify services slice.
			if len(tt.services) == 0 {
				if len(fake.restartServices) != 0 {
					t.Errorf("expected empty services slice, got %v", fake.restartServices)
				}
			} else {
				if len(fake.restartServices) != len(tt.services) {
					t.Fatalf("services len = %d, want %d", len(fake.restartServices), len(tt.services))
				}
				for i, s := range tt.services {
					if fake.restartServices[i] != s {
						t.Errorf("services[%d] = %q, want %q", i, fake.restartServices[i], s)
					}
				}
			}

			// Verify output mentions expected text.
			if tt.wantOutput != "" && !strings.Contains(out.String(), tt.wantOutput) {
				t.Errorf("output %q does not contain %q", out.String(), tt.wantOutput)
			}
		})
	}
}
