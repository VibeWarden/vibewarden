package deploy_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestService_Deploy_ArchMismatch_BlocksLocalImageTransfer(t *testing.T) {
	// Remote reports x86_64 (amd64) but local is arm64.
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"uname -m": {output: "x86_64"},
		},
	}
	generator := &fakeGenerator{}
	exporter := &fakeImageExporter{}

	svc := deployapp.NewService(executor, generator).
		WithImageExporter(exporter).
		WithLocalArch("arm64")

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8443},
		App:    config.AppConfig{Build: "."},
	}

	err := svc.Deploy(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		GeneratedDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when arch mismatches during local image transfer")
	}

	if !errors.Is(err, deployapp.ErrArchMismatch) {
		t.Errorf("expected ErrArchMismatch, got: %v", err)
	}

	var archErr *deployapp.ArchMismatchError
	if !errors.As(err, &archErr) {
		t.Fatalf("expected *ArchMismatchError, got %T: %v", err, archErr)
	}
	if archErr.LocalArch != "arm64" {
		t.Errorf("ArchMismatchError.LocalArch = %q, want %q", archErr.LocalArch, "arm64")
	}
	if archErr.RemoteArch != "amd64" {
		t.Errorf("ArchMismatchError.RemoteArch = %q, want %q", archErr.RemoteArch, "amd64")
	}

	// Verify the error message contains the fix-it suggestion.
	msg := archErr.Error()
	if got := "vibew build --platform linux/amd64"; !strings.Contains(msg, got) {
		t.Errorf("error message should contain %q, got: %s", got, msg)
	}
}

func TestService_Deploy_ArchMatch_Proceeds(t *testing.T) {
	// Both local and remote are amd64 -- deploy should succeed.
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"uname -m": {output: "x86_64"},
		},
	}
	generator := &fakeGenerator{}
	exporter := &fakeImageExporter{}

	svc := deployapp.NewService(executor, generator).
		WithImageExporter(exporter).
		WithLocalArch("amd64")

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8443},
		App:    config.AppConfig{Build: "."},
	}

	err := svc.Deploy(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Deploy() should succeed when archs match, got: %v", err)
	}
}

func TestService_Deploy_ArchCheck_SkippedForRegistryImage(t *testing.T) {
	// Registry image (contains "/") -- no local image transfer, no arch check.
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			// If the arch check ran, it would detect a mismatch.
			"uname -m": {output: "x86_64"},
		},
	}
	generator := &fakeGenerator{}

	svc := deployapp.NewService(executor, generator).
		WithLocalArch("arm64")

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8443},
		App:    config.AppConfig{Image: "ghcr.io/org/myapp:latest"},
	}

	err := svc.Deploy(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Deploy() with registry image should not check arch, got: %v", err)
	}
}

func TestService_Deploy_ArchCheck_SkippedWithoutExporter(t *testing.T) {
	// Local image name but no exporter -- no transfer, no arch check.
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"uname -m": {output: "x86_64"},
		},
	}
	generator := &fakeGenerator{}

	// No WithImageExporter -- exporter is nil.
	svc := deployapp.NewService(executor, generator).
		WithLocalArch("arm64")

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8443},
		App:    config.AppConfig{Build: "."},
	}

	err := svc.Deploy(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Deploy() without exporter should not check arch, got: %v", err)
	}
}

func TestService_Deploy_ArchCheck_SkippedWhenUnameFails(t *testing.T) {
	// uname -m fails on the remote -- skip the check, don't block deploy.
	executor := &fakeExecutor{
		runResponses: map[string]runResponse{
			"uname -m": {err: errors.New("command not found")},
		},
	}
	generator := &fakeGenerator{}
	exporter := &fakeImageExporter{}

	svc := deployapp.NewService(executor, generator).
		WithImageExporter(exporter).
		WithLocalArch("arm64")

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8443},
		App:    config.AppConfig{Build: "."},
	}

	err := svc.Deploy(context.Background(), cfg, deployapp.RunOptions{
		ConfigPath:   "/tmp/proj/vibewarden.yaml",
		GeneratedDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Deploy() should proceed when uname fails, got: %v", err)
	}
}

func TestArchMismatchError_ErrorMessage(t *testing.T) {
	err := &deployapp.ArchMismatchError{
		LocalArch:  "arm64",
		RemoteArch: "amd64",
	}

	msg := err.Error()

	checks := []string{
		"architecture mismatch",
		"arm64",
		"amd64",
		"vibew build --platform linux/amd64",
	}
	for _, want := range checks {
		if !strings.Contains(msg, want) {
			t.Errorf("ArchMismatchError.Error() should contain %q, got:\n%s", want, msg)
		}
	}
}

func TestArchMismatchError_Unwrap(t *testing.T) {
	err := &deployapp.ArchMismatchError{
		LocalArch:  "arm64",
		RemoteArch: "amd64",
	}

	if !errors.Is(err, deployapp.ErrArchMismatch) {
		t.Error("ArchMismatchError should unwrap to ErrArchMismatch")
	}
}

func TestErrArchMismatch_Sentinel(t *testing.T) {
	// Verify the sentinel error exists and has a reasonable message.
	if deployapp.ErrArchMismatch.Error() != "architecture mismatch" {
		t.Errorf("ErrArchMismatch.Error() = %q, want %q",
			deployapp.ErrArchMismatch.Error(), "architecture mismatch")
	}
}
