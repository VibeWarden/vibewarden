package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
)

// NewInitCmd creates the `vibewarden init` subcommand.
//
// The command scaffolds vibewarden.yaml and docker-compose.yml in the current
// directory (or the directory supplied as the first positional argument).
func NewInitCmd() *cobra.Command {
	var (
		upstream     int
		auth         bool
		rateLimit    bool
		tls          bool
		domain       string
		force        bool
		skipDocker   bool
	)

	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Initialise VibeWarden in a project",
		Long: `Scaffold vibewarden.yaml and docker-compose.yml in the project directory.

The command detects the project type and upstream port automatically.
Pass flags to enable optional features.

Examples:
  vibewarden init
  vibewarden init --upstream 8000
  vibewarden init --auth --rate-limit
  vibewarden init --tls --domain example.com
  vibewarden init --force`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			if tls && domain == "" {
				return fmt.Errorf("--domain is required when --tls is set")
			}

			renderer := templateadapter.NewRenderer(templates.FS)
			detector := scaffoldapp.NewDetector()
			svc := scaffoldapp.NewService(renderer, detector)

			opts := scaffoldapp.InitOptions{
				UpstreamPort:     upstream,
				AuthEnabled:      auth,
				RateLimitEnabled: rateLimit,
				TLSEnabled:       tls,
				TLSDomain:        domain,
				Force:            force,
				SkipDocker:       skipDocker,
			}

			if err := svc.Init(context.Background(), dir, opts); err != nil {
				if errors.Is(err, os.ErrExist) {
					return fmt.Errorf("%w\n\nRun with --force to overwrite existing files.", err)
				}
				return err
			}

			printSuccessMessage(cmd, dir, opts)
			return nil
		},
	}

	cmd.Flags().IntVar(&upstream, "upstream", 0, "upstream app port (default: auto-detected or 3000)")
	cmd.Flags().BoolVar(&auth, "auth", false, "enable authentication (Ory Kratos)")
	cmd.Flags().BoolVar(&rateLimit, "rate-limit", false, "enable rate limiting")
	cmd.Flags().BoolVar(&tls, "tls", false, "enable TLS (requires --domain)")
	cmd.Flags().StringVar(&domain, "domain", "", "domain for TLS certificate (required with --tls)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	cmd.Flags().BoolVar(&skipDocker, "skip-docker", false, "skip docker-compose.yml generation")

	return cmd
}

// printSuccessMessage writes next-steps guidance to cmd's output writer.
func printSuccessMessage(cmd *cobra.Command, dir string, opts scaffoldapp.InitOptions) {
	w := cmd.OutOrStdout()

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "VibeWarden initialised successfully.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Generated files:")
	fmt.Fprintf(w, "  %s/vibewarden.yaml\n", dir)
	if !opts.SkipDocker {
		fmt.Fprintf(w, "  %s/docker-compose.yml\n", dir)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintln(w, "  1. Review and adjust vibewarden.yaml as needed.")
	if !opts.SkipDocker {
		fmt.Fprintln(w, "  2. Start the local dev environment:")
		fmt.Fprintln(w, "       docker compose up")
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Documentation: https://vibewarden.dev/docs/quickstart")
}
