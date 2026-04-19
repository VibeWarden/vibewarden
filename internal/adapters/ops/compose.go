// Package ops provides adapters for operational CLI commands.
package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ComposeAdapter implements ports.ComposeRunner by shelling out to the
// docker compose CLI.
type ComposeAdapter struct{}

// NewComposeAdapter creates a new ComposeAdapter.
func NewComposeAdapter() *ComposeAdapter {
	return &ComposeAdapter{}
}

// Up runs "docker compose [-f <composeFile>] [--profile <p>...] up -d".
// When composeFile is non-empty it is passed as the -f flag so that docker
// compose uses that specific file rather than the default discovery logic.
// Output from the command is streamed directly to stdout/stderr so the user
// sees progress in real time.
func (c *ComposeAdapter) Up(ctx context.Context, composeFile string, profiles []string) error {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d")

	cmd := exec.CommandContext(ctx, "docker", args...)
	// We want live output — use Run with inherited file descriptors instead.
	// exec.Cmd with nil Stdout/Stderr inherits the parent process's file
	// descriptors, which means output streams directly to the terminal.
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}
	return nil
}

// Restart runs "docker compose [-f <composeFile>] up -d --force-recreate --build [<service>...]".
// This rebuilds images if the Dockerfile has changed and recreates containers,
// making it safe to call after any project file change.
// When composeFile is non-empty it is passed as the -f flag.
// When services is non-empty each service name is appended so that only those
// services are rebuilt and recreated; when empty all services are affected.
// On failure, stderr output from the command is included in the returned error
// to give the caller actionable context for diagnosis.
func (c *ComposeAdapter) Restart(ctx context.Context, composeFile string, services []string) error {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	args = append(args, "up", "-d", "--force-recreate", "--build")
	args = append(args, services...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("docker compose up --force-recreate --build: %w\nstderr: %s", err, msg)
		}
		return fmt.Errorf("docker compose up --force-recreate --build: %w", err)
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

// composeContainer is the JSON shape produced by "docker compose ps --format json".
type composeContainer struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

// ImageCheckerAdapter implements ports.DockerImageChecker by shelling out to
// the docker CLI.
type ImageCheckerAdapter struct{}

// NewImageCheckerAdapter creates a new ImageCheckerAdapter.
func NewImageCheckerAdapter() *ImageCheckerAdapter {
	return &ImageCheckerAdapter{}
}

// ImageExists runs "docker image inspect <name>" and returns true when the
// exit code is 0 (image found). A non-zero exit code is treated as a missing
// image, not as an error. Other failures (e.g. daemon unreachable) are
// returned as errors.
func (a *ImageCheckerAdapter) ImageExists(ctx context.Context, name string) (bool, error) {
	args := []string{"image", "inspect", name}
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // args are constructed from caller-supplied image name, not user shell input
	if err := cmd.Run(); err != nil {
		// ExitError with code 1 means the image was not found.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("docker image inspect: %w", err)
	}
	return true, nil
}

// Logs runs "docker compose [-f <composeFile>] logs --tail <tailLines> <service>"
// and returns the combined stdout/stderr output as a string.
func (c *ComposeAdapter) Logs(ctx context.Context, composeFile string, service string, tailLines int) (string, error) {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	args = append(args, "logs", "--tail", fmt.Sprintf("%d", tailLines), service)

	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker compose logs: %w", err)
	}
	return string(out), nil
}

// PS runs "docker compose [-f <composeFile>] ps --format json" and returns one
// ContainerInfo per container.  An empty slice is returned when no containers
// are running (not an error).
func (c *ComposeAdapter) PS(ctx context.Context, composeFile string) ([]ports.ContainerInfo, error) {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	args = append(args, "ps", "--format", "json")

	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("docker compose ps: %w", err)
	}

	// "docker compose ps --format json" outputs one JSON object per line.
	var results []ports.ContainerInfo
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var ct composeContainer
		if err := dec.Decode(&ct); err != nil {
			// Ignore malformed lines; best-effort parsing.
			continue
		}
		results = append(results, ports.ContainerInfo{
			Name:    ct.Name,
			Service: ct.Service,
			State:   ct.State,
			Health:  ct.Health,
		})
	}
	return results, nil
}

// Tail returns the last n lines of logs from the specified service.
// It runs "docker compose [-f <composeFile>] logs --tail <n> <service>".
func (c *ComposeAdapter) Tail(ctx context.Context, composeFile string, service string, n int) (string, error) {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	args = append(args, "logs", "--tail", fmt.Sprintf("%d", n), service)

	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose logs: %w", err)
	}
	return string(out), nil
}

// ShellProberAdapter implements ports.DockerShellProber by shelling out to
// the docker CLI.
type ShellProberAdapter struct{}

// NewShellProberAdapter creates a new ShellProberAdapter.
func NewShellProberAdapter() *ShellProberAdapter {
	return &ShellProberAdapter{}
}

// HasShell probes whether the given Docker image contains a working /bin/sh by
// running "docker run --rm --entrypoint="" <image> /bin/sh -c "echo ok"".
// Returns (true, nil) if the command succeeds, (false, nil) if the command
// exits with a non-zero status (indicating no shell), or (false, err) for
// unexpected failures.
func (a *ShellProberAdapter) HasShell(ctx context.Context, image string) (bool, error) {
	args := []string{"run", "--rm", "--entrypoint=", image, "/bin/sh", "-c", "echo ok"}
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // args are constructed from caller-supplied image name
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("docker run shell probe: %w", err)
	}
	return true, nil
}
