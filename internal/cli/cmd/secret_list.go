package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// newSecretListCmd creates the `vibew secret list` subcommand.
//
// It lists all managed secret paths from both OpenBao and the .credentials
// file, deduplicated and sorted.
func newSecretListCmd() *cobra.Command {
	var (
		configPath string
		outputDir  string
		asJSON     bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all managed secret paths",
		Long: `List all managed secret paths from OpenBao and the .credentials file.

Well-known aliases are always included. When OpenBao is running, additional
paths from the 'infra/' and 'app/' prefixes are listed as well.

Examples:
  vibew secret list
  vibew secret list --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := buildSecretService(configPath, outputDir)
			if err != nil {
				return fmt.Errorf("initialising secret service: %w", err)
			}

			paths, err := svc.List(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing secrets: %w", err)
			}

			if asJSON {
				return printListJSON(cmd, paths)
			}
			return printListHuman(cmd, paths)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&outputDir, "output-dir", defaultOutputDir, "directory containing the .credentials file")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as a JSON array")

	return cmd
}

// printListHuman writes paths one per line to cmd's stdout.
func printListHuman(cmd *cobra.Command, paths []string) error {
	for _, p := range paths {
		fmt.Fprintln(cmd.OutOrStdout(), p) //nolint:errcheck
	}
	return nil
}

// printListJSON writes paths as a JSON array to cmd's stdout.
func printListJSON(cmd *cobra.Command, paths []string) error {
	if paths == nil {
		paths = []string{}
	}
	out, err := json.Marshal(paths)
	if err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(out)) //nolint:errcheck
	return nil
}
