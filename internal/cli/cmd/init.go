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
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// NewInitCmd creates the `vibewarden init` subcommand.
//
// The command scaffolds vibewarden.yaml and the vibew wrapper scripts in the
// current directory (or the directory supplied as the first positional argument).
// When --agent is specified, AI agent context files are also generated.
// Docker Compose and Kratos config are generated at runtime by `vibew dev`.
func NewInitCmd() *cobra.Command {
	var (
		upstream    int
		auth        bool
		rateLimit   bool
		tls         bool
		domain      string
		force       bool
		skipWrapper bool
		version     string
		agent       string
	)

	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Initialise VibeWarden in a project",
		Long: `Scaffold vibewarden.yaml and the vibew wrapper scripts in the project directory.

Docker Compose and Kratos config are generated at runtime by ` + "`vibew dev`" + `.
The command detects the project type and upstream port automatically.
Pass flags to enable optional features.

Examples:
  vibewarden init
  vibewarden init --upstream 8000
  vibewarden init --auth --rate-limit
  vibewarden init --tls --domain example.com
  vibewarden init --version v0.2.0
  vibewarden init --skip-wrapper
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
				SkipWrapper:      skipWrapper,
				Version:          version,
			}

			if err := svc.Init(context.Background(), dir, opts); err != nil {
				if errors.Is(err, os.ErrExist) {
					return fmt.Errorf("%w\n\nRun with --force to overwrite existing files.", err) //nolint:revive,staticcheck // user-facing CLI hint: intentional newline and trailing period
				}
				return err
			}

			var agentFiles []string
			if agentType != "" {
				agentFiles, err = agentSvc.GenerateAgentContext(context.Background(), dir, agentType, opts)
				if err != nil {
					if errors.Is(err, os.ErrExist) {
						return fmt.Errorf("%w\n\nRun with --force to overwrite existing files.", err) //nolint:revive,staticcheck // user-facing CLI hint: intentional newline and trailing period
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
	cmd.Flags().BoolVar(&skipWrapper, "skip-wrapper", false, "skip vibew wrapper script generation")
	cmd.Flags().StringVar(&version, "version", "", "VibeWarden version to pin in .vibewarden-version (default: latest)")
	cmd.Flags().StringVar(&agent, "agent", "all", `generate AI agent context files: "claude", "cursor", "generic", "all", or "none"`)

	return cmd
}

// parseAgentType converts the --agent flag string to a domainscaffold.AgentType.
// Returns an empty AgentType (and no error) when value is "none".
func parseAgentType(value string) (domainscaffold.AgentType, error) {
	switch domainscaffold.AgentType(value) {
	case domainscaffold.AgentTypeClaude,
		domainscaffold.AgentTypeCursor,
		domainscaffold.AgentTypeGeneric,
		domainscaffold.AgentTypeAll:
		return domainscaffold.AgentType(value), nil
	case domainscaffold.AgentType("none"), domainscaffold.AgentType(""):
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
	fmt.Fprintln(w, "VibeWarden initialized!")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Files created:")
	fmt.Fprintf(w, "  vibewarden.yaml          Configuration\n")
	if !opts.SkipWrapper {
		fmt.Fprintf(w, "  vibew                    Wrapper script (macOS/Linux)\n")
		fmt.Fprintf(w, "  vibew.ps1                Wrapper script (Windows)\n")
		fmt.Fprintf(w, "  .vibewarden-version      Version pin\n")
	}
	fmt.Fprintf(w, "  .gitignore               Git ignore rules\n")
	for _, f := range agentFiles {
		fmt.Fprintf(w, "  %s\n", f)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Next steps:")
	if !opts.SkipWrapper {
		fmt.Fprintln(w, "  ./vibew dev              Start the dev environment")
		fmt.Fprintln(w, "  ./vibew status           Check component health")
	} else {
		fmt.Fprintln(w, "  Review and adjust vibewarden.yaml as needed.")
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Documentation: https://vibewarden.dev/docs/quickstart")
}
