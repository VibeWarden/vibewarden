package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
)

// NewDownCmd creates the "vibew down" subcommand.
//
// The command stops the local Docker Compose dev environment started by
// "vibew dev". It is the canonical counterpart to "vibew dev" and is
// referenced in the startup summary printed by "vibew dev".
//
// Examples:
//
//	vibew down                  # stop the stack, preserve volumes
//	vibew down -v --yes         # stop and remove volumes, no prompt
//	vibew down --remove-orphans # also remove orphaned containers
func NewDownCmd() *cobra.Command {
	var (
		volumes       bool
		removeOrphans bool
		yes           bool
	)

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the local dev environment",
		Long: `Stop the VibeWarden Docker Compose dev environment started by 'vibew dev'.

Idempotent: running on an already-stopped stack is a no-op (exit 0).

Flags:
  -v, --volumes         also remove named volumes (Let's Encrypt certs,
                        Postgres data, etc.). Prompts for confirmation in a
                        TTY unless --yes is also set.
      --remove-orphans  remove containers for services no longer in the
                        compose file.
      --yes             skip the confirmation prompt for --volumes.

See also:
  vibew dev     start the dev environment (prints this command in its summary)
  vibew logs -f tail structured logs while the stack is running

Examples:
  vibew down
  vibew down -v --yes
  vibew down --remove-orphans`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			compose := opsadapter.NewComposeAdapter()
			svc := opsapp.NewDownService(compose)

			isTTY := term.IsTerminal(int(os.Stdout.Fd())) //nolint:gosec // file descriptor fits in int on all supported platforms

			opts := opsapp.DownOptions{
				Volumes:       volumes,
				RemoveOrphans: removeOrphans,
				Yes:           yes,
				In:            os.Stdin,
				IsTTY:         isTTY,
			}
			return svc.Run(cmd.Context(), opts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVarP(&volumes, "volumes", "v", false, "also remove named volumes (destructive)")
	cmd.Flags().BoolVar(&removeOrphans, "remove-orphans", false, "remove containers for services no longer in the compose file")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt for --volumes")

	return cmd
}
