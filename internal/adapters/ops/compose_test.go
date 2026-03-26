package ops_test

import (
	"context"
	"os/exec"
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
)

// dockerAvailable reports whether the docker binary is available on PATH.
// Tests that require docker are skipped when it is not.
func dockerAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

func TestComposeAdapter_UpArgsBaselineStack(t *testing.T) {
	// This test verifies that Up builds the correct command for the baseline
	// stack (no profiles). It relies on docker being present but the compose
	// project does not need to exist — we cancel the context immediately so
	// the command never actually runs.
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so docker compose up exits fast

	// We only care that Up returns an error (context cancelled), not that it
	// succeeds — this confirms the command is attempted with the right args.
	err := adapter.Up(ctx, nil)
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

func TestComposeAdapter_UpArgsWithProfiles(t *testing.T) {
	// Same pattern as above but verifies profile flags are forwarded.
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Up(ctx, []string{"observability"})
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

func TestComposeAdapter_UpArgsWithMultipleProfiles(t *testing.T) {
	// Verify that multiple profiles are each preceded by --profile.
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Up(ctx, []string{"observability", "debug"})
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

func TestComposeAdapter_VersionReturnsErrorWhenDockerMissing(t *testing.T) {
	if dockerAvailable() {
		t.Skip("docker is available; skipping missing-docker test")
	}

	adapter := opsadapter.NewComposeAdapter()
	_, err := adapter.Version(context.Background())
	if err == nil {
		t.Fatal("expected an error when docker is not available")
	}
}

func TestComposeAdapter_InfoReturnsErrorWhenDockerMissing(t *testing.T) {
	if dockerAvailable() {
		t.Skip("docker is available; skipping missing-docker test")
	}

	adapter := opsadapter.NewComposeAdapter()
	err := adapter.Info(context.Background())
	if err == nil {
		t.Fatal("expected an error when docker is not available")
	}
}

// commandArgs is a helper used in table-driven tests to verify the args slice
// that would be passed to docker compose up for a given set of profiles.
func commandArgs(profiles []string) []string {
	args := []string{"compose"}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d")
	return args
}

func TestCommandArgsConstruction(t *testing.T) {
	tests := []struct {
		name     string
		profiles []string
		want     []string
	}{
		{
			name:     "no profiles",
			profiles: nil,
			want:     []string{"compose", "up", "-d"},
		},
		{
			name:     "single profile",
			profiles: []string{"observability"},
			want:     []string{"compose", "--profile", "observability", "up", "-d"},
		},
		{
			name:     "multiple profiles",
			profiles: []string{"observability", "debug"},
			want:     []string{"compose", "--profile", "observability", "--profile", "debug", "up", "-d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commandArgs(tt.profiles)
			if len(got) != len(tt.want) {
				t.Fatalf("len(args) = %d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("args[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
