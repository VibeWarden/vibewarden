package ops_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// downCompose is a minimal ComposeRunner fake used by DownService tests.
// It is separate from fakeCompose to avoid bloating that type with
// down-specific fields.
type downCompose struct {
	result     ports.DownResult
	err        error
	capturedCF string
	capturedOp ports.ComposeDownOptions
	called     int
}

func (f *downCompose) Up(_ context.Context, _ string, _ []string, _ ports.ComposeUpOptions) error {
	return nil
}
func (f *downCompose) Restart(_ context.Context, _ string, _ []string) error { return nil }
func (f *downCompose) Down(_ context.Context, composeFile string, opts ports.ComposeDownOptions) (ports.DownResult, error) {
	f.called++
	f.capturedCF = composeFile
	f.capturedOp = opts
	return f.result, f.err
}
func (f *downCompose) Version(_ context.Context) (string, error) { return "", nil }
func (f *downCompose) Info(_ context.Context) error              { return nil }
func (f *downCompose) PS(_ context.Context, _ string) ([]ports.ContainerInfo, error) {
	return nil, nil
}
func (f *downCompose) Logs(_ context.Context, _ string, _ string, _ int) (string, error) {
	return "", nil
}

func TestDownService_Run(t *testing.T) {
	wantComposeFile := filepath.Join(".vibewarden", "generated", "docker-compose.yml")

	tests := []struct {
		name       string
		opts       opsapp.DownOptions
		result     ports.DownResult
		downErr    error
		wantErr    bool
		wantOutput []string
		wantCalled bool
		wantVolOpt bool
	}{
		{
			name:       "no-op on stopped stack",
			opts:       opsapp.DownOptions{},
			result:     ports.DownResult{},
			wantOutput: []string{"No running services. Nothing to do."},
			wantCalled: true,
		},
		{
			name:       "three containers stopped, volumes preserved",
			opts:       opsapp.DownOptions{},
			result:     ports.DownResult{StoppedContainers: 3},
			wantOutput: []string{"Stopped 3 containers", "Volumes preserved", "vibew down -v"},
			wantCalled: true,
		},
		{
			name:       "--volumes --yes removes data without prompt",
			opts:       opsapp.DownOptions{Volumes: true, Yes: true, IsTTY: true},
			result:     ports.DownResult{StoppedContainers: 3, RemovedVolumes: 2},
			wantOutput: []string{"Stopped 3 containers and removed 2 volumes"},
			wantCalled: true,
			wantVolOpt: true,
		},
		{
			name:    "compose error is propagated",
			opts:    opsapp.DownOptions{},
			downErr: errors.New("daemon unreachable"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &downCompose{result: tt.result, err: tt.downErr}
			svc := opsapp.NewDownService(fake)

			var out bytes.Buffer
			err := svc.Run(context.Background(), tt.opts, &out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantCalled {
				if fake.called != 1 {
					t.Errorf("expected Down() to be called once, got %d", fake.called)
				}
				if fake.capturedCF != wantComposeFile {
					t.Errorf("Down() composeFile = %q, want %q", fake.capturedCF, wantComposeFile)
				}
				if fake.capturedOp.Volumes != tt.wantVolOpt {
					t.Errorf("Down() Volumes = %v, want %v", fake.capturedOp.Volumes, tt.wantVolOpt)
				}
			}

			for _, want := range tt.wantOutput {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q\ngot:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestDownService_Volumes_NonTTY_WithoutYes_Errors(t *testing.T) {
	// Refuse to remove volumes silently in CI/scripts: require --yes or TTY.
	fake := &downCompose{}
	svc := opsapp.NewDownService(fake)

	var out bytes.Buffer
	err := svc.Run(context.Background(),
		opsapp.DownOptions{Volumes: true, IsTTY: false, Yes: false},
		&out)
	if !errors.Is(err, opsapp.ErrNonTTYVolumesRequiresYes) {
		t.Fatalf("expected ErrNonTTYVolumesRequiresYes, got %v", err)
	}
	if fake.called != 0 {
		t.Errorf("Down() should not have been called, got %d calls", fake.called)
	}
}

func TestDownService_Volumes_TTY_PromptYes_Proceeds(t *testing.T) {
	fake := &downCompose{result: ports.DownResult{StoppedContainers: 2, RemovedVolumes: 1}}
	svc := opsapp.NewDownService(fake)

	var out bytes.Buffer
	err := svc.Run(context.Background(),
		opsapp.DownOptions{Volumes: true, IsTTY: true, In: strings.NewReader("y\n")},
		&out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fake.called != 1 {
		t.Errorf("expected Down() called once, got %d", fake.called)
	}
	if !fake.capturedOp.Volumes {
		t.Error("expected Volumes=true forwarded to adapter")
	}
	if !strings.Contains(out.String(), "Delete all volume data") {
		t.Errorf("expected prompt in output, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "removed 1 volumes") {
		t.Errorf("expected summary in output, got:\n%s", out.String())
	}
}

func TestDownService_Volumes_TTY_PromptNo_Aborts(t *testing.T) {
	fake := &downCompose{}
	svc := opsapp.NewDownService(fake)

	var out bytes.Buffer
	err := svc.Run(context.Background(),
		opsapp.DownOptions{Volumes: true, IsTTY: true, In: strings.NewReader("n\n")},
		&out)
	if err != nil {
		t.Fatalf("Run() unexpected error when aborting: %v", err)
	}
	if fake.called != 0 {
		t.Errorf("Down() should not be called after abort, got %d", fake.called)
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Errorf("expected 'Aborted' in output, got:\n%s", out.String())
	}
}

func TestDownService_Volumes_TTY_EmptyLine_Aborts(t *testing.T) {
	// An empty answer (user just hits Enter) must default to N.
	fake := &downCompose{}
	svc := opsapp.NewDownService(fake)

	var out bytes.Buffer
	err := svc.Run(context.Background(),
		opsapp.DownOptions{Volumes: true, IsTTY: true, In: strings.NewReader("\n")},
		&out)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if fake.called != 0 {
		t.Errorf("Down() should not be called on default-N, got %d", fake.called)
	}
}

func TestDownService_Idempotent_NoError_OnStoppedStack(t *testing.T) {
	// The adapter Down returns DownResult{} + nil err when nothing is
	// running. The service must pass that through as exit 0.
	fake := &downCompose{result: ports.DownResult{}, err: nil}
	svc := opsapp.NewDownService(fake)

	var out bytes.Buffer
	err := svc.Run(context.Background(), opsapp.DownOptions{}, &out)
	if err != nil {
		t.Fatalf("Run() must not error on already-stopped stack, got %v", err)
	}
	if !strings.Contains(out.String(), "Nothing to do") {
		t.Errorf("expected no-op message, got:\n%s", out.String())
	}
}

func TestDownService_RemoveOrphans_Forwarded(t *testing.T) {
	fake := &downCompose{}
	svc := opsapp.NewDownService(fake)

	var out bytes.Buffer
	if err := svc.Run(context.Background(),
		opsapp.DownOptions{RemoveOrphans: true},
		&out); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !fake.capturedOp.RemoveOrphans {
		t.Error("expected RemoveOrphans=true forwarded to adapter")
	}
}

func TestDownService_DoesNotPassServices(t *testing.T) {
	// `vibew down` tears down the whole project and must NOT restrict teardown
	// to a subset of services — only ObsService.Down targets specific services.
	// The main DownService must pass an empty Services slice so that
	// docker compose down stops the entire project.
	fake := &downCompose{}
	svc := opsapp.NewDownService(fake)

	var out bytes.Buffer
	if err := svc.Run(context.Background(), opsapp.DownOptions{}, &out); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(fake.capturedOp.Services) != 0 {
		t.Errorf("DownService.Run() must not set Services, got %v", fake.capturedOp.Services)
	}
}
