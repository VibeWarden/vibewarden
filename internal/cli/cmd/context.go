package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	"github.com/vibewarden/vibewarden/internal/config"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// agentContextFiles returns the output paths for each agent type, relative to
// the project root. These must stay in sync with the specs in
// internal/app/scaffold/agent_context.go.
var agentContextFiles = map[domainscaffold.AgentType]string{
	domainscaffold.AgentTypeClaude:  filepath.Join(".claude", "CLAUDE.md"),
	domainscaffold.AgentTypeCursor:  filepath.Join(".cursor", "rules"),
	domainscaffold.AgentTypeGeneric: "AGENTS.md",
}

// NewContextCmd creates the `vibewarden context` subcommand group.
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

// newContextRefreshCmd creates the `vibewarden context refresh` subcommand.
//
// It reads vibewarden.yaml, derives the current feature state, and
// regenerates each AI agent context file that already exists on disk.
// Files that do not yet exist are skipped unless --force is supplied.
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

			var updated []string
			var skipped []string

			for agentType, relPath := range agentContextFiles {
				absPath := filepath.Join(dir, relPath)

				// Skip files that don't exist unless --force is set.
				if !force {
					if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
						skipped = append(skipped, relPath)
						continue
					}
				}

				// Force=true here because we already did the existence guard above.
				refreshOpts := opts
				refreshOpts.Force = true

				written, genErr := agentSvc.GenerateAgentContext(context.Background(), dir, agentType, refreshOpts)
				if genErr != nil {
					if errors.Is(genErr, os.ErrExist) {
						skipped = append(skipped, relPath)
						continue
					}
					return fmt.Errorf("refreshing context for %s: %w", agentType, genErr)
				}
				updated = append(updated, written...)
			}

			w := cmd.OutOrStdout()

			if len(updated) == 0 && len(skipped) > 0 {
				fmt.Fprintln(w, "No context files found to refresh.")
				fmt.Fprintln(w, "")
				fmt.Fprintln(w, "Skipped (file not found):")
				for _, s := range skipped {
					fmt.Fprintf(w, "  %s\n", s)
				}
				fmt.Fprintln(w, "")
				fmt.Fprintln(w, "Run 'vibewarden init --agent all' to create context files,")
				fmt.Fprintln(w, "or use --force to create them during refresh.")
				return nil
			}

			fmt.Fprintln(w, "Context files refreshed:")
			for _, f := range updated {
				fmt.Fprintf(w, "  %s\n", f)
			}

			if len(skipped) > 0 {
				fmt.Fprintln(w, "")
				fmt.Fprintln(w, "Skipped (file not found):")
				for _, s := range skipped {
					fmt.Fprintf(w, "  %s\n", s)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().BoolVar(&force, "force", false, "create context files even when they do not exist")

	return cmd
}
