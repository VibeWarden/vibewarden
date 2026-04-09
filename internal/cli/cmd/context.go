package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	"github.com/vibewarden/vibewarden/internal/config"
)

// agentContextFile is the output path (relative to project root) for the
// vibew-owned agent context file. It must stay in sync with the spec in
// internal/app/scaffold/agent_context.go.
const agentContextFile = "AGENTS-VIBEWARDEN.md"

// NewContextCmd creates the `vibew context` subcommand group.
//
// The context command group contains operations for managing AI agent context
// files. Currently only the `refresh` subcommand is registered.
func NewContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage AI agent context files",
		Long: `Manage the AI agent context files that tell your coding assistant
about VibeWarden's security layer.

Run 'vibewarden context refresh' to regenerate context files from the current
vibewarden.yaml configuration.`,
		// Default: print help when no subcommand is given.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
	}

	cmd.AddCommand(newContextRefreshCmd())

	return cmd
}

// newContextRefreshCmd creates the `vibew context refresh` subcommand.
//
// It reads vibewarden.yaml, derives the current feature state, and
// regenerates AGENTS-VIBEWARDEN.md (always) and updates AGENTS.md (if needed).
// When --force is not supplied, the refresh is skipped if AGENTS-VIBEWARDEN.md
// does not yet exist.
func newContextRefreshCmd() *cobra.Command {
	var (
		configPath string
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Regenerate AI agent context files from vibewarden.yaml",
		Long: `Regenerate AI agent context files to reflect the current vibewarden.yaml
configuration.

This is useful after changing feature flags (auth, rate limiting, TLS) so that
your AI coding assistant receives up-to-date instructions about the security layer.

By default only files that already exist on disk are regenerated. Pass --force
to create them even when they are missing.

Examples:
  vibewarden context refresh
  vibewarden context refresh --force
  vibewarden context refresh --config ./path/to/vibewarden.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := "."

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			renderer := templateadapter.NewRenderer(templates.FS)
			agentSvc := scaffoldapp.NewAgentContextService(renderer)

			// Build InitOptions from the loaded config so AgentContextService
			// can derive the correct feature flags for template rendering.
			opts := scaffoldapp.InitOptions{
				UpstreamPort:     cfg.Upstream.Port,
				AuthEnabled:      cfg.Kratos.PublicURL != "",
				RateLimitEnabled: cfg.RateLimit.Enabled,
				TLSEnabled:       cfg.TLS.Enabled,
				TLSDomain:        cfg.TLS.Domain,
				Force:            force,
			}

			absPath := filepath.Join(dir, agentContextFile)

			// Skip files that don't exist unless --force is set.
			if !force {
				if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
					w := cmd.OutOrStdout()
					fmt.Fprintln(w, "No context files found to refresh.")
					fmt.Fprintln(w, "")
					fmt.Fprintf(w, "Skipped (file not found):\n  %s\n", agentContextFile)
					fmt.Fprintln(w, "")
					fmt.Fprintln(w, "Run 'vibew wrap' to create context files,")
					fmt.Fprintln(w, "or use --force to create them during refresh.")
					return nil
				}
			}

			// Force=true here because we already did the existence guard above.
			refreshOpts := opts
			refreshOpts.Force = true

			updated, genErr := agentSvc.GenerateAgentContext(context.Background(), dir, refreshOpts)
			if genErr != nil {
				return fmt.Errorf("refreshing context: %w", genErr)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "Context files refreshed:")
			for _, f := range updated {
				fmt.Fprintf(w, "  %s\n", f)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().BoolVar(&force, "force", false, "create context files even when they do not exist")

	return cmd
}
