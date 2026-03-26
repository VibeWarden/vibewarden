package cmd

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
)

// NewPluginsCmd creates the "vibewarden plugins" subcommand.
//
// Without a subcommand it lists all compiled-in plugins with their
// enabled/disabled status as read from the config file.
// The "show" subcommand prints detailed configuration options for one plugin.
func NewPluginsCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "plugins",
		Short: "List available plugins and their status",
		Long: `List all plugins compiled into this VibeWarden binary.

The enabled/disabled status is read from vibewarden.yaml (or the path
supplied with --config). When no config file is found the defaults apply.

Examples:
  vibewarden plugins
  vibewarden plugins --config ./my-vibewarden.yaml
  vibewarden plugins show tls`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			printPluginList(cmd.OutOrStdout(), cfg)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.AddCommand(newPluginsShowCmd())

	return cmd
}

// newPluginsShowCmd creates the "vibewarden plugins show <name>" subcommand.
func newPluginsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <plugin-name>",
		Short: "Show detailed configuration options for a plugin",
		Long: `Show the full configuration schema and an example for a single plugin.

Examples:
  vibewarden plugins show tls
  vibewarden plugins show rate-limiting`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			d, ok := plugins.FindDescriptor(name)
			if !ok {
				return fmt.Errorf("unknown plugin %q; run 'vibewarden plugins' to list available plugins", name)
			}
			printPluginDetail(cmd.OutOrStdout(), d)
			return nil
		},
	}
}

// enabledPlugins returns a set of plugin names that are enabled in cfg.
// The set keys are canonical plugin names as they appear in the Catalog.
func enabledPlugins(cfg *config.Config) map[string]bool {
	return map[string]bool{
		"tls":              cfg.TLS.Enabled,
		"security-headers": cfg.SecurityHeaders.Enabled,
		"rate-limiting":    cfg.RateLimit.Enabled,
		"auth":             cfg.Auth.Enabled,
		"metrics":          cfg.Metrics.Enabled,
		"user-management":  cfg.Admin.Enabled,
	}
}

// printPluginList writes a tabular list of all compiled-in plugins to out.
func printPluginList(out io.Writer, cfg *config.Config) {
	enabled := enabledPlugins(cfg)

	tw := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tENABLED\tDESCRIPTION")
	for _, d := range plugins.Catalog {
		enabledStr := "false"
		if enabled[d.Name] {
			enabledStr = "true"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", d.Name, enabledStr, d.Description)
	}
	tw.Flush() //nolint:errcheck
}

// printPluginDetail writes the full metadata for a single plugin to out.
func printPluginDetail(out io.Writer, d plugins.PluginDescriptor) {
	fmt.Fprintf(out, "Plugin: %s\n", d.Name)
	fmt.Fprintf(out, "Description: %s\n\n", d.Description)

	fmt.Fprintln(out, "Configuration (under plugins:):")

	// Sort fields alphabetically for stable output.
	fields := make([]string, 0, len(d.ConfigSchema))
	for k := range d.ConfigSchema {
		fields = append(fields, k)
	}
	sort.Strings(fields)

	tw := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	for _, field := range fields {
		fmt.Fprintf(tw, "  %s\t%s\n", field, d.ConfigSchema[field])
	}
	tw.Flush() //nolint:errcheck

	fmt.Fprintf(out, "\nExample:\n  plugins:\n%s\n", d.Example)
}
