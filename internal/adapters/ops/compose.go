// Package ops provides adapters for operational CLI commands.
package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ComposeAdapter implements ports.ComposeRunner by shelling out to the
// docker compose CLI.
//
// stderrSink is the writer that receives captured stderr on failure paths.
// It defaults to os.Stderr and is overridable for tests.
type ComposeAdapter struct {
	// stderrSink is where captured compose stderr is flushed when Up fails
	// and the caller did not provide their own streaming writer. Tests may
	// override this to capture output without touching the real terminal.
	stderrSink io.Writer
}

// NewComposeAdapter creates a new ComposeAdapter that writes captured stderr
// on failure to os.Stderr.
func NewComposeAdapter() *ComposeAdapter {
	return &ComposeAdapter{stderrSink: os.Stderr}
}

// Up runs "docker compose [-f <composeFile>] [--profile <p>...] up -d".
// When composeFile is non-empty it is passed as the -f flag so that docker
// compose uses that specific file rather than the default discovery logic.
//
// Stderr handling:
//   - When opts.Stderr is non-nil, compose stderr is streamed live to that
//     writer AND mirrored into an internal buffer so that the buffer can be
//     included in the wrapped error on failure.
//   - When opts.Stderr is nil, compose stderr is captured silently and, on
//     failure only, flushed to the adapter's stderrSink (os.Stderr by default)
//     so the user sees docker's actual error before the wrapped exit status.
//
// Stdout is always inherited from the parent process so progress bars render
// correctly in interactive terminals.
func (c *ComposeAdapter) Up(ctx context.Context, composeFile string, profiles []string, opts ports.ComposeUpOptions) error {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d")

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = nil

	// Always capture stderr into a buffer so we can surface it on failure.
	var buf bytes.Buffer
	if opts.Stderr != nil {
		// Verbose: tee live stderr to the caller and keep a copy for errors.
		cmd.Stderr = io.MultiWriter(opts.Stderr, &buf)
	} else {
		cmd.Stderr = &buf
	}

	if err := cmd.Run(); err != nil {
		captured := buf.String()
		// Flush captured stderr to the adapter's sink so the user sees
		// docker's real error message. When opts.Stderr was non-nil the
		// stream was already written live — skip the dump to avoid
		// duplication.
		if opts.Stderr == nil {
			if msg := strings.TrimSpace(captured); msg != "" {
				fmt.Fprintln(c.stderrSink, msg)
			}
		}
		return fmt.Errorf("docker compose up: %w", ClassifyDockerError(err, captured))
	}
	return nil
}

// Down stops and removes containers for the compose project.
//
// When opts.Services is empty, it runs:
//
//	docker compose [-f <composeFile>] down [--volumes] [--remove-orphans]
//
// When opts.Services is non-empty, it performs a service-targeted teardown
// instead of a full project `down`. This is the correct way to tear down a
// subset of services: `docker compose down --profile <name>` does NOT scope
// teardown by profile — compose's --profile is an activation flag for `up`,
// not a scope limiter for `down` — and would remove all services in the
// project. Service-targeted teardown runs two commands in sequence:
//
//  1. docker compose [-f <file>] stop  <services...>
//  2. docker compose [-f <file>] rm -f <services...>
//
// When opts.Volumes is true and opts.VolumeNames is set, a best-effort
// docker volume rm is run for each named volume after rm. Errors from
// "in use" or "no such volume" are silently tolerated.
//
// Returns a DownResult with counters parsed from docker's progress output.
// When nothing is running, Down treats the invocation as a no-op and returns
// a zero-valued DownResult and a nil error.
func (c *ComposeAdapter) Down(ctx context.Context, composeFile string, opts ports.ComposeDownOptions) (ports.DownResult, error) {
	if len(opts.Services) > 0 {
		return c.downServices(ctx, composeFile, opts)
	}
	return c.downProject(ctx, composeFile, opts)
}

// downProject runs `docker compose down` for the entire project.
func (c *ComposeAdapter) downProject(ctx context.Context, composeFile string, opts ports.ComposeDownOptions) (ports.DownResult, error) {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	args = append(args, "down")
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	if opts.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &stderr

	err := cmd.Run()
	stderrText := stderr.String()

	// Treat "no configuration file" and "no such service" as no-ops. These
	// occur when Down is called in a project that never ran.
	if err != nil {
		lower := strings.ToLower(stderrText)
		if strings.Contains(lower, "no configuration file provided") ||
			strings.Contains(lower, "no such service") ||
			strings.Contains(lower, "has no containers") {
			return ports.DownResult{}, nil
		}
		classified := ClassifyDockerError(err, stderrText)
		msg := strings.TrimSpace(stderrText)
		if msg != "" {
			return ports.DownResult{}, fmt.Errorf("docker compose down: %w\nstderr: %s", classified, msg)
		}
		return ports.DownResult{}, fmt.Errorf("docker compose down: %w", classified)
	}

	result := parseDownOutput(stderrText)
	return result, nil
}

// downServices runs service-targeted `stop` + `rm -f` instead of a full
// project `down`. See Down for the rationale.
func (c *ComposeAdapter) downServices(ctx context.Context, composeFile string, opts ports.ComposeDownOptions) (ports.DownResult, error) {
	baseArgs := []string{"compose"}
	if composeFile != "" {
		baseArgs = append(baseArgs, "-f", composeFile)
	}

	// Step 1: stop named services.
	stopArgs := append(append([]string{}, baseArgs...), "stop")
	stopArgs = append(stopArgs, opts.Services...)
	stopCmd := exec.CommandContext(ctx, "docker", stopArgs...) //nolint:gosec // args are constructed from caller-supplied service names, not user shell input
	var stopStderr bytes.Buffer
	stopCmd.Stdout = nil
	stopCmd.Stderr = &stopStderr
	if err := stopCmd.Run(); err != nil {
		stopStderrText := stopStderr.String()
		lower := strings.ToLower(stopStderrText)
		if !isNoOpError(lower) {
			classified := ClassifyDockerError(err, stopStderrText)
			msg := strings.TrimSpace(stopStderrText)
			if msg != "" {
				return ports.DownResult{}, fmt.Errorf("docker compose stop: %w\nstderr: %s", classified, msg)
			}
			return ports.DownResult{}, fmt.Errorf("docker compose stop: %w", classified)
		}
		// Service not running — treat as no-op; continue to rm.
	}

	// Step 2: remove named services.
	rmArgs := append(append([]string{}, baseArgs...), "rm", "-f")
	rmArgs = append(rmArgs, opts.Services...)
	rmCmd := exec.CommandContext(ctx, "docker", rmArgs...) //nolint:gosec // args are constructed from caller-supplied service names, not user shell input
	var rmStderr bytes.Buffer
	rmCmd.Stdout = nil
	rmCmd.Stderr = &rmStderr
	if err := rmCmd.Run(); err != nil {
		rmStderrText := rmStderr.String()
		lower := strings.ToLower(rmStderrText)
		if !isNoOpError(lower) {
			classified := ClassifyDockerError(err, rmStderrText)
			msg := strings.TrimSpace(rmStderrText)
			if msg != "" {
				return ports.DownResult{}, fmt.Errorf("docker compose rm: %w\nstderr: %s", classified, msg)
			}
			return ports.DownResult{}, fmt.Errorf("docker compose rm: %w", classified)
		}
	}

	result := parseDownOutput(stopStderr.String() + rmStderr.String())

	// Step 3 (optional): remove named volumes best-effort.
	// ProjectName must be supplied by the caller via opts.ProjectName; it
	// must match the compose file's `name:` field (e.g. "myapp"). We do NOT
	// derive the project name from the file path because the generated compose
	// file lives at .vibewarden/generated/docker-compose.yml, so filepath.Dir
	// would yield "generated" — not the actual project name Docker uses.
	if opts.Volumes && len(opts.VolumeNames) > 0 && opts.ProjectName != "" {
		for _, vol := range opts.VolumeNames {
			fullName := opts.ProjectName + "_" + vol
			volCmd := exec.CommandContext(ctx, "docker", "volume", "rm", fullName) //nolint:gosec // fullName is derived from caller-supplied project+volume names, not user shell input
			if volErr := volCmd.Run(); volErr == nil {
				result.RemovedVolumes++
			}
			// Tolerate "no such volume" / "in use" — best-effort.
		}
	}

	return result, nil
}

// isNoOpError reports whether a lowercase stderr snippet represents a
// non-fatal "nothing to do" condition from docker compose.
func isNoOpError(lower string) bool {
	return strings.Contains(lower, "no configuration file provided") ||
		strings.Contains(lower, "no such service") ||
		strings.Contains(lower, "has no containers")
}

// parseDownOutput counts "Removed" lines emitted by docker compose down on
// stderr. It is exported at package level (lower-case) so tests can exercise
// the parser directly without invoking docker.
func parseDownOutput(stderrText string) ports.DownResult {
	var result ports.DownResult
	for _, line := range strings.Split(stderrText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Format: "Container <name>  Removed" / "Volume <name>  Removed" /
		// "Network <name>  Removed". We only count containers and volumes;
		// networks are infrastructure, not user-visible state.
		if !strings.HasSuffix(trimmed, "Removed") {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "Container "):
			result.StoppedContainers++
		case strings.HasPrefix(trimmed, "Volume "):
			result.RemovedVolumes++
		}
	}
	return result
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
		captured := stderr.String()
		classified := ClassifyDockerError(err, captured)
		msg := strings.TrimSpace(captured)
		if msg != "" {
			return fmt.Errorf("docker compose up --force-recreate --build: %w\nstderr: %s", classified, msg)
		}
		return fmt.Errorf("docker compose up --force-recreate --build: %w", classified)
	}
	return nil
}

// Version runs "docker compose version" and returns the raw output.
func (c *ComposeAdapter) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "version")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker compose version: %w", ClassifyDockerError(err, stderr.String()))
	}
	return string(out), nil
}

// Info runs "docker info" to verify the Docker daemon is reachable.
func (c *ComposeAdapter) Info(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "info")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker info: %w", ClassifyDockerError(err, stderr.String()))
	}
	return nil
}

// composeContainer is the JSON shape produced by "docker compose ps --format json".
type composeContainer struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
	Image   string `json:"Image"`
	Project string `json:"Project"`
	// CreatedAt is a human-readable timestamp as reported by docker compose
	// (e.g. "2026-04-20 17:21:43 +0300 EEST").
	CreatedAt string `json:"CreatedAt"`
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
		var createdAt time.Time
		if ct.CreatedAt != "" {
			// Docker Compose emits CreatedAt as "2006-01-02 15:04:05 -0700 MST".
			if parsed, err := time.Parse("2006-01-02 15:04:05 -0700 MST", ct.CreatedAt); err == nil {
				createdAt = parsed
			}
		}
		results = append(results, ports.ContainerInfo{
			Name:      ct.Name,
			Service:   ct.Service,
			State:     ct.State,
			Health:    ct.Health,
			Image:     ct.Image,
			Project:   ct.Project,
			CreatedAt: createdAt,
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
