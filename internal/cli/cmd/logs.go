package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// NewLogsCmd creates the "vibew logs" subcommand.
//
// The command streams docker compose logs for the local dev stack. Stdout and
// stderr are inherited from the parent process so --follow streams without
// buffering. Services can be scoped by passing service names as positional
// arguments.
func NewLogsCmd() *cobra.Command {
	var (
		tail   int
		follow bool
		since  string
	)

	cmd := &cobra.Command{
		Use:   "logs [<service>...]",
		Short: "Stream local dev-stack logs",
		Long: `Stream docker compose logs for the local VibeWarden dev stack.

All services are interleaved by default. Pass one or more service names to
scope the output to those services only.

Flags:
  --tail N         show the last N lines per container (default 100)
  -f, --follow     stream output continuously until Ctrl-C
  --since <value>  show logs since a duration or RFC3339 timestamp
                   (passed verbatim to docker compose, e.g. "5m", "1h")

Services in the default stack:
  vibewarden   VibeWarden sidecar (proxy, auth, TLS)
  app          your application container
  kratos       Ory Kratos identity server
  postgres     PostgreSQL database
  mailslurper  local email sink

If the stack has not been started yet, run: vibew dev

Tips:
  vibew logs vibewarden       # sidecar logs only
  vibew logs app              # app logs only
  vibew logs --follow         # stream all services
  vibew logs --since 5m       # last 5 minutes`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireScaffolding(); err != nil {
				return err
			}
			if err := requireConfig(""); err != nil {
				return err
			}

			cfg, err := loadAndResolve(cmd.Context(), "")
			if err != nil {
				return err
			}

			compose := opsadapter.NewComposeAdapter()
			streamer := opsadapter.NewComposeLogsStreamAdapter()
			svc := opsapp.NewLogsStreamService(compose, streamer)

			runErr := svc.Run(cmd.Context(), cfg, opsapp.LogsStreamOptions{
				Services: args,
				Tail:     tail,
				Follow:   follow,
				Since:    since,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())

			if runErr == nil {
				return nil
			}

			// Map sentinel errors to canonical messages and exit codes.
			if errors.Is(runErr, opsapp.ErrStackNotRunning) {
				fmt.Fprintln(cmd.ErrOrStderr(), "Stack is not running. Start with: vibew dev")
				os.Exit(1) //nolint:gocritic // intentional: semantic exit code 1
			}

			var unknownSvc *opsapp.ErrUnknownService
			if errors.As(runErr, &unknownSvc) {
				fmt.Fprintln(cmd.ErrOrStderr(), runErr.Error())
				os.Exit(1) //nolint:gocritic // intentional: semantic exit code 1
			}

			if errors.Is(runErr, ports.ErrDockerUnavailable) {
				renderDockerUnavailable(cmd.ErrOrStderr(), runErr)
				os.Exit(3) //nolint:gocritic // intentional: semantic exit code 3
			}

			return runErr
		},
	}

	cmd.Flags().IntVar(&tail, "tail", 100, "number of lines to show from the end of the log for each container")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream log output continuously")
	cmd.Flags().StringVar(&since, "since", "", "show logs since a duration (e.g. 5m) or RFC3339 timestamp")

	return cmd
}
