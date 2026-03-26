package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	scaffoldadapter "github.com/vibewarden/vibewarden/internal/adapters/scaffold"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
)

// NewInitCmd creates the `vibewarden init` subcommand.
//
// The command scaffolds vibewarden.yaml and docker-compose.yml in the current
// directory (or the directory supplied as the first positional argument).
// When --agent is specified, AI agent context files are also generated.
func NewInitCmd() *cobra.Command {
	var (
		upstream   int
		auth       bool
		rateLimit  bool
		tls        bool
		domain     string
		force      bool
		skipDocker bool
		agent      string
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
  vibewarden init --agent claude
  vibewarden init --agent all
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

			agentType, err := parseAgentType(agent)
			if err != nil {
				return err
			}

			renderer := templateadapter.NewRenderer(templates.FS)
			detector := scaffoldadapter.NewDetector()
			svc := scaffoldapp.NewService(renderer, detector)
			agentSvc := scaffoldapp.NewAgentContextService(renderer)

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

			var agentFiles []string
			if agentType != "" {
				agentFiles, err = agentSvc.GenerateAgentContext(context.Background(), dir, agentType, opts)
				if err != nil {
					if errors.Is(err, os.ErrExist) {
						return fmt.Errorf("%w\n\nRun with --force to overwrite existing files.", err)
					}
					return fmt.Errorf("generating agent context: %w", err)
				}
			}

			printSuccessMessage(cmd, dir, opts, agentFiles)
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
	cmd.Flags().StringVar(&agent, "agent", "all", `generate AI agent context files: "claude", "cursor", "generic", "all", or "none"`)

	return cmd
}

// parseAgentType converts the --agent flag string to a scaffold.AgentType.
// Returns an empty AgentType (and no error) when value is "none".
func parseAgentType(value string) (scaffold.AgentType, error) {
	switch scaffold.AgentType(value) {
	case scaffold.AgentTypeClaude,
		scaffold.AgentTypeCursor,
		scaffold.AgentTypeGeneric,
		scaffold.AgentTypeAll:
		return scaffold.AgentType(value), nil
	case scaffold.AgentType("none"), scaffold.AgentType(""):
		return "", nil
	default:
		return "", fmt.Errorf(
			"unknown --agent value %q: must be one of claude, cursor, generic, all, none",
			value,
		)
	}
}

// printSuccessMessage writes next-steps guidance to cmd's output writer.
func printSuccessMessage(cmd *cobra.Command, dir string, opts scaffoldapp.InitOptions, agentFiles []string) {
	w := cmd.OutOrStdout()

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "VibeWarden initialised successfully.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Generated files:")
	fmt.Fprintf(w, "  %s/vibewarden.yaml\n", dir)
	if !opts.SkipDocker {
		fmt.Fprintf(w, "  %s/docker-compose.yml\n", dir)
	}
	for _, f := range agentFiles {
		fmt.Fprintf(w, "  %s\n", f)
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
