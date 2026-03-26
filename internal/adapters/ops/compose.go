// Package ops provides adapters for operational CLI commands.
package ops

import (
	"context"
	"fmt"
	"os/exec"
)

// ComposeAdapter implements ports.ComposeRunner by shelling out to the
// docker compose CLI.
type ComposeAdapter struct{}

// NewComposeAdapter creates a new ComposeAdapter.
func NewComposeAdapter() *ComposeAdapter {
	return &ComposeAdapter{}
}

// Up runs "docker compose [--profile <p>...] up -d" in the working directory.
// Output from the command is streamed directly to stdout/stderr so the user
// sees progress in real time.
func (c *ComposeAdapter) Up(ctx context.Context, profiles []string) error {
	args := []string{"compose"}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d")

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = nil // let exec inherit parent stdout/stderr via Output
	// We want live output — use Run with inherited file descriptors instead.
	cmd.Stdout = nil
	cmd.Stderr = nil

	// exec.Cmd with nil Stdout/Stderr inherits the parent process's file
	// descriptors, which means output streams directly to the terminal.
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}
	return nil
}

// Version runs "docker compose version" and returns the raw output.
func (c *ComposeAdapter) Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "compose", "version").Output()
	if err != nil {
		return "", fmt.Errorf("docker compose version: %w", err)
	}
	return string(out), nil
}

// Info runs "docker info" to verify the Docker daemon is reachable.
func (c *ComposeAdapter) Info(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "info")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker info: %w", err)
	}
	return nil
}
