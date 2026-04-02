package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/vibewarden/vibewarden/internal/adapters/logprint"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
)

// NewLogsCmd creates the "vibew logs" subcommand.
//
// Without flags the command tails recent docker compose logs for the vibewarden
// service and pretty-prints each structured JSON line with colors. Non-JSON
// lines (e.g. docker compose banners) are forwarded verbatim.
func NewLogsCmd() *cobra.Command {
	var (
		follow  bool
		filter  string
		rawJSON bool
		verbose bool
		stdin   bool
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Pretty-print VibeWarden structured logs",
		Long: `Stream and pretty-print VibeWarden's structured JSON logs.

By default the command runs "docker compose logs vibewarden" and formats
each line with colors and a human-friendly layout:

  2026-03-26 10:30:45  INFO   [auth.session_validated]  User authenticated
  2026-03-26 10:30:46  WARN   [rate_limit.exceeded]     Rate limit hit
  2026-03-26 10:30:47  ERROR  [proxy.upstream_error]    Upstream returned 502

Non-JSON lines (docker compose banners, etc.) are printed verbatim.

Pipe mode:
  docker compose logs vibewarden | vibewarden logs --stdin

Examples:
  vibewarden logs
  vibewarden logs --follow
  vibewarden logs --filter auth
  vibewarden logs --json
  vibewarden logs --verbose
  docker compose logs vibewarden | vibewarden logs --stdin`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			printerOpts := logprint.PrinterOptions{
				Verbose: verbose,
				Filter:  filter,
				RawJSON: rawJSON,
			}
			printer := logprint.NewPrinter(printerOpts)
			svc := opsapp.NewLogsService(printer)

			logsOpts := opsapp.LogsOptions{
				Follow: follow,
			}
			if stdin {
				logsOpts.Stdin = os.Stdin
			}

			return svc.Run(cmd.Context(), logsOpts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output (tail -f)")
	cmd.Flags().StringVar(&filter, "filter", "", "Filter by event type prefix (e.g. auth, proxy, rate_limit)")
	cmd.Flags().BoolVar(&rawJSON, "json", false, "Output raw JSON without pretty-printing")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Include the event payload in the output")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read log lines from stdin instead of docker compose")

	return cmd
}
