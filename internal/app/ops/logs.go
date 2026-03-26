package ops

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// LogsOptions holds the options passed to the "vibewarden logs" command.
type LogsOptions struct {
	// Follow tails the log stream in real time (docker compose logs -f).
	Follow bool
	// Stdin reads log lines from an explicit reader rather than shelling out
	// to docker compose. When non-nil the Follow and ServiceName fields are
	// ignored.
	Stdin io.Reader
}

// LogsService orchestrates the "vibewarden logs" use case.
// It reads structured log lines from docker compose (or stdin) and delegates
// formatting to a LogPrinter.
type LogsService struct {
	printer ports.LogPrinter
}

// NewLogsService creates a new LogsService.
func NewLogsService(printer ports.LogPrinter) *LogsService {
	return &LogsService{printer: printer}
}

// Run starts the log stream and pretty-prints each line to out.
// When opts.Stdin is set, lines are read from that reader; otherwise the
// service shells out to "docker compose logs [-f] vibewarden".
func (s *LogsService) Run(ctx context.Context, opts LogsOptions, out io.Writer) error {
	if opts.Stdin != nil {
		return s.stream(ctx, opts.Stdin, out)
	}
	return s.runCompose(ctx, opts, out)
}

// runCompose starts "docker compose logs [--follow] vibewarden" and streams
// the output through the printer.
func (s *LogsService) runCompose(ctx context.Context, opts LogsOptions, out io.Writer) error {
	args := []string{"compose", "logs", "--no-log-prefix"}
	if opts.Follow {
		args = append(args, "--follow")
	}
	args = append(args, "vibewarden")

	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	// Merge stderr into stdout so docker compose warnings are visible.
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting docker compose logs: %w", err)
	}

	if err := s.stream(ctx, stdout, out); err != nil {
		// Best-effort wait — process may already be done.
		_ = cmd.Wait()
		return err
	}

	if err := cmd.Wait(); err != nil {
		// Context cancellation (SIGKILL) is expected when --follow is used
		// and the user presses Ctrl-C; don't surface it as an error.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("docker compose logs: %w", err)
	}
	return nil
}

// stream reads lines from r, printing each via the configured LogPrinter.
func (s *LogsService) stream(ctx context.Context, r io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := scanner.Text()
		if err := s.printer.Print(line, out); err != nil {
			return fmt.Errorf("printing log line: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		// Ignore errors caused by the context being cancelled.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("reading log stream: %w", err)
	}
	return nil
}
