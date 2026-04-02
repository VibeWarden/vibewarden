package cmd

import (
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	upgradeapp "github.com/vibewarden/vibewarden/internal/app/upgrade"
)

// NewUpgradeCmd creates the `vibew upgrade` subcommand.
//
// The command downloads the specified (or latest) VibeWarden release from
// GitHub Releases, verifies its SHA-256 checksum, installs the binary to
// ~/.vibewarden/bin/, updates .vibewarden-version when found in the current
// or a parent directory, and touches the vibew wrapper scripts so tooling
// knows they were considered.
func NewUpgradeCmd() *cobra.Command {
	var (
		dryRun     bool
		installDir string
	)

	cmd := &cobra.Command{
		Use:   "upgrade [version]",
		Short: "Update the VibeWarden binary and wrapper scripts",
		Long: `Download and install a new VibeWarden release.

When no version is supplied the command fetches the latest release from the
GitHub API. When a version is supplied (e.g. "v0.4.0") that specific release
is installed.

The command:
  1. Resolves the target version (latest or the supplied tag).
  2. Downloads the binary archive for the current OS and architecture.
  3. Verifies the SHA-256 checksum.
  4. Installs the binary to ~/.vibewarden/bin/ (or --install-dir).
  5. Updates .vibewarden-version if found in the current or a parent directory.
  6. Touches vibew, vibew.ps1, vibew.cmd in the current directory when present.

Use --dry-run to see what would happen without writing any files.

Examples:
  vibew upgrade
  vibew upgrade v0.4.0
  vibew upgrade --dry-run
  vibew upgrade --install-dir /usr/local/bin`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			version := ""
			if len(args) > 0 {
				version = args[0]
			}

			client := &http.Client{Timeout: 60 * time.Second}
			svc := upgradeapp.NewService(client)

			opts := upgradeapp.Options{
				Version:    version,
				InstallDir: installDir,
				DryRun:     dryRun,
				Stdout:     cmd.OutOrStdout(),
			}

			if err := svc.Run(cmd.Context(), opts); err != nil {
				return fmt.Errorf("upgrade: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would happen without writing any files")
	cmd.Flags().StringVar(&installDir, "install-dir", "", "directory to install the binary into (default: ~/.vibewarden/bin)")

	return cmd
}
