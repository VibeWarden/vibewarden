package ops

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// DownService orchestrates the "vibew down" use case.
// It stops and optionally removes the dev environment started by "vibew dev".
type DownService struct {
	compose ports.ComposeRunner
}

// NewDownService creates a new DownService.
func NewDownService(compose ports.ComposeRunner) *DownService {
	return &DownService{compose: compose}
}

// DownOptions holds options for the "vibew down" command.
type DownOptions struct {
	// Volumes controls whether named volumes (Let's Encrypt certs, Postgres
	// data, etc.) are also removed. Destructive — callers should confirm
	// with the user before setting this to true.
	Volumes bool
	// RemoveOrphans passes --remove-orphans to docker compose down so that
	// containers for services no longer defined in the compose file are
	// also removed.
	RemoveOrphans bool
	// Yes skips the interactive confirmation prompt when Volumes is true.
	// Callers should set this from a --yes flag and/or when stdout is not
	// attached to a terminal.
	Yes bool
	// In is the reader used to read the y/N answer when a confirmation
	// prompt is shown. When nil, os.Stdin should be wired by the caller.
	In io.Reader
	// IsTTY indicates whether the process is attached to an interactive
	// terminal. When false, prompting is not possible: the caller must
	// have set Yes=true for destructive operations or a non-TTY error
	// will be returned.
	IsTTY bool
}

// ErrNonTTYVolumesRequiresYes is returned when Run is called with
// Volumes=true, IsTTY=false, and Yes=false. Removing named volumes silently
// in CI/scripts is too destructive to allow without an explicit opt-in.
var ErrNonTTYVolumesRequiresYes = errors.New("--yes required when not running in a TTY and --volumes is set")

// Run executes the "vibew down" flow:
//  1. Resolve the generated docker-compose.yml path.
//  2. When opts.Volumes is true: require confirmation (interactive prompt in
//     a TTY, or Yes=true in non-TTY environments).
//  3. Invoke compose.Down with the resolved options.
//  4. Print a short summary to out.
//
// Run is idempotent: invoking it on a stack that is already stopped prints a
// no-op summary and returns a nil error.
func (s *DownService) Run(ctx context.Context, opts DownOptions, out io.Writer) error {
	composeFile := filepath.Join(generatedOutputDir, "docker-compose.yml")

	if opts.Volumes && !opts.Yes {
		if !opts.IsTTY {
			return ErrNonTTYVolumesRequiresYes
		}
		confirmed, err := confirmVolumeDeletion(opts.In, out)
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		if !confirmed {
			fmt.Fprintln(out, "Aborted. Volumes preserved.")
			return nil
		}
	}

	result, err := s.compose.Down(ctx, composeFile, ports.ComposeDownOptions{
		Volumes:       opts.Volumes,
		RemoveOrphans: opts.RemoveOrphans,
	})
	if err != nil {
		return fmt.Errorf("stopping dev environment: %w", err)
	}

	printDownSummary(result, opts, out)
	return nil
}

// confirmVolumeDeletion prompts the user for confirmation before removing
// named volumes. It reads a single line from in and returns true only when
// the user enters "y" or "yes" (case-insensitive). Any other input (empty,
// "n", "no", or unrecognised) returns false.
func confirmVolumeDeletion(in io.Reader, out io.Writer) (bool, error) {
	if in == nil {
		// Safety: refuse to delete volumes when we have no way to prompt.
		return false, nil
	}
	fmt.Fprint(out, "Delete all volume data? [y/N] ")
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// printDownSummary writes the one-line status summary for the user.
// Formats:
//   - `No running services. Nothing to do.` (no-op path).
//   - `Stopped N containers. Volumes preserved — run 'vibew down -v' to also remove data.`
//   - `Stopped N containers and removed M volumes.` (--volumes path).
func printDownSummary(result ports.DownResult, opts DownOptions, out io.Writer) {
	if result.StoppedContainers == 0 && result.RemovedVolumes == 0 {
		fmt.Fprintln(out, "No running services. Nothing to do.")
		return
	}
	if opts.Volumes {
		fmt.Fprintf(out, "Stopped %d containers and removed %d volumes.\n",
			result.StoppedContainers, result.RemovedVolumes)
		return
	}
	fmt.Fprintf(out, "Stopped %d containers. Volumes preserved — run 'vibew down -v' to also remove data.\n",
		result.StoppedContainers)
}
