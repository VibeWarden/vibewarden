package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

// caddyLocalCARootCRT is the filename Caddy uses for the local CA root certificate.
const caddyLocalCARootCRT = "pki/authorities/local/root.crt"

// NewCertCmd creates the "vibewarden cert" subcommand group.
//
// The cert command group provides utilities for working with the TLS certificates
// managed by the VibeWarden sidecar.
func NewCertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert",
		Short: "Manage TLS certificates used by VibeWarden",
		Long: `Utilities for working with the TLS certificates managed by the VibeWarden sidecar.

Examples:
  vibewarden cert export
  vibewarden cert export > vibewarden-ca.pem
  vibewarden cert export --path`,
		// Default: print help when no subcommand is given.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
	}

	cmd.AddCommand(newCertExportCmd())

	return cmd
}

// newCertExportCmd creates the "vibewarden cert export" subcommand.
//
// It locates Caddy's local CA root certificate and either prints the PEM
// content to stdout or, with --path, prints only the resolved file path.
//
// The --cert-path flag overrides automatic discovery and reads from the given
// path directly. This is intended for testing and advanced use cases.
func newCertExportCmd() *cobra.Command {
	var (
		pathOnly     bool
		certPathFlag string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the local CA certificate used by VibeWarden",
		Long: `Find the Caddy-generated local CA root certificate and write it to stdout.

The certificate is stored by Caddy in a platform-specific location:
  Linux:  ~/.local/share/caddy/pki/authorities/local/root.crt
  macOS:  ~/Library/Application Support/Caddy/pki/authorities/local/root.crt
  Docker: /data/caddy/pki/authorities/local/root.crt

Use the PEM output to import the certificate into tools such as Postman,
mobile devices, or share it with teammates.

Examples:
  vibewarden cert export
  vibewarden cert export > vibewarden-ca.pem
  vibewarden cert export --path`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var certPath string
			if certPathFlag != "" {
				// Explicit override: verify the path exists before using it.
				if _, err := os.Stat(certPathFlag); err != nil {
					return fmt.Errorf(
						"local CA certificate not found at %s: start the VibeWarden sidecar first",
						certPathFlag,
					)
				}
				certPath = certPathFlag
			} else {
				var err error
				certPath, err = findCaddyLocalCACert()
				if err != nil {
					return err
				}
			}

			if pathOnly {
				fmt.Fprintln(cmd.OutOrStdout(), certPath)
				return nil
			}

			pem, err := os.ReadFile(certPath) //nolint:gosec // certPath comes from the vibewarden.yaml config, not direct user input
			if err != nil {
				return fmt.Errorf("reading certificate at %s: %w", certPath, err)
			}

			_, err = cmd.OutOrStdout().Write(pem)
			if err != nil {
				return fmt.Errorf("writing certificate to stdout: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&pathOnly, "path", false, "print only the file path, not the certificate content")
	cmd.Flags().StringVar(&certPathFlag, "cert-path", "", "override the automatic certificate path discovery")

	return cmd
}

// findCaddyLocalCACert searches the standard Caddy CA certificate locations in
// priority order and returns the path of the first one that exists.
//
// Search order:
//  1. Docker container path (/data/caddy/...)
//  2. Platform-specific user data directory (Linux/macOS)
//
// Returns an error if no certificate is found at any of the candidate paths.
func findCaddyLocalCACert() (string, error) {
	candidates := caddyCACertCandidates()

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf(
		"local CA certificate not found; start the VibeWarden sidecar first (tried: %v)",
		candidates,
	)
}

// caddyCACertCandidates returns the ordered list of paths where Caddy may have
// written its local CA root certificate.
func caddyCACertCandidates() []string {
	candidates := []string{
		// Docker container path — checked first so it works inside the container.
		filepath.Join("/data/caddy", caddyLocalCARootCRT),
	}

	// Platform-specific user data directory.
	if dir := caddyUserDataDir(); dir != "" {
		candidates = append(candidates, filepath.Join(dir, caddyLocalCARootCRT))
	}

	return candidates
}

// caddyUserDataDir returns the platform-specific base directory where Caddy
// stores its data (the part before "pki/..."). Returns an empty string when
// the home directory cannot be determined.
func caddyUserDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Caddy")
	default:
		// Linux and other Unix-like systems follow the XDG base directory spec.
		// Caddy defaults to ~/.local/share/caddy when XDG_DATA_HOME is not set.
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "caddy")
		}
		return filepath.Join(home, ".local", "share", "caddy")
	}
}
