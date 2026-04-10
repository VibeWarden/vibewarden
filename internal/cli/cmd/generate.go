package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
)

// NewGenerateCmd creates the "vibew generate" subcommand.
//
// The command reads vibewarden.yaml and writes the generated runtime
// configuration files under .vibewarden/generated/ without starting any
// services. This is useful for inspecting the generated files before running
// `vibew dev`, or for integrating into a CI pipeline.
func NewGenerateCmd() *cobra.Command {
	var (
		configPath string
		outputDir  string
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate runtime configuration files from vibewarden.yaml",
		Long: `Generate runtime configuration files from vibewarden.yaml.

The following files are written under the output directory
(default: .vibewarden/generated/):

  kratos/kratos.yml          — Ory Kratos configuration
  kratos/identity.schema.json — Identity schema (preset or custom)
  docker-compose.yml         — Docker Compose stack definition

Re-run this command after editing vibewarden.yaml to refresh generated files.
The generated directory is excluded from version control by default (.gitignore).

Examples:
  vibew generate
  vibew generate --config ./my-vibewarden.yaml
  vibew generate --output-dir /tmp/vw-generated`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireScaffolding(); err != nil {
				return err
			}
			if err := requireConfig(configPath); err != nil {
				return err
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			renderer := templateadapter.NewRenderer(configtemplates.FS)
			generator := generateapp.NewServiceWithCredentials(
				renderer,
				credentialsadapter.NewGenerator(),
				credentialsadapter.NewStore(),
			).WithConfigSourcePath(configPath)

			if err := generator.Generate(cmd.Context(), cfg, outputDir); err != nil {
				return fmt.Errorf("generating config files: %w", err)
			}

			dir := outputDir
			if dir == "" {
				dir = ".vibewarden/generated"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Generated runtime configuration files in %s\n", dir)
			fmt.Fprintf(cmd.OutOrStdout(), "  %s/docker-compose.yml\n", dir)
			if cfg.Auth.Enabled && cfg.Auth.Mode == config.AuthModeKratos && !cfg.Kratos.External {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s/kratos/kratos.yml\n", dir)
				fmt.Fprintf(cmd.OutOrStdout(), "  %s/kratos/identity.schema.json\n", dir)
			}
			if cfg.Observability.Enabled {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s/observability/ (prometheus, grafana, loki, promtail, otel-collector)\n", dir)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "directory to write generated files (default: .vibewarden/generated)")

	if err := cmd.RegisterFlagCompletionFunc("config", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}); err != nil {
		// registration can only fail when called on a non-existent flag; safe to ignore
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}

	return cmd
}
