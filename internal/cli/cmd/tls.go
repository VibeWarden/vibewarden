package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	sshadapter "github.com/vibewarden/vibewarden/internal/adapters/ssh"
	"github.com/vibewarden/vibewarden/internal/app/tlsstatus"
)

// NewTLSCmd creates the "vibew tls" subcommand group.
//
// The tls command group provides utilities for inspecting TLS certificates
// on remote VibeWarden deployments.
func NewTLSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tls",
		Short: "TLS certificate inspection utilities",
		Long: `Utilities for inspecting TLS certificates on remote VibeWarden deployments.

Examples:
  vibew tls status --domain example.com --target ssh://ubuntu@203.0.113.10
  vibew tls status --domain example.com --target ssh://ubuntu@203.0.113.10 --port 8443`,
		// Default: print help when no subcommand is given.
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help() //nolint:errcheck
		},
	}

	cmd.AddCommand(newTLSStatusCmd())

	return cmd
}

// newTLSStatusCmd creates the "vibew tls status" subcommand.
//
// It connects to the remote host via SSH, runs openssl to inspect the TLS
// certificate for the given domain, and displays certificate details including
// subject, issuer, validity period, SANs, and expiry status.
func newTLSStatusCmd() *cobra.Command {
	var (
		domain string
		target string
		sshKey string
		port   int
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show TLS certificate details for a remote domain",
		Long: `Connect to the remote host via SSH, run openssl to inspect the TLS
certificate presented for the specified domain, and display the results.

The command runs on the remote host so that the certificate is inspected from
the server's perspective (useful when the domain resolves differently from
your local machine, or when testing a newly deployed certificate before DNS
propagation).

Displayed fields:
  - Subject (CN)
  - Issuer (CA)
  - Valid from / to
  - Serial number
  - Subject Alternative Names (SANs)
  - Expiry status (OK / WARNING / CRITICAL / EXPIRED)

Examples:
  vibew tls status --domain example.com --target ssh://ubuntu@203.0.113.10
  vibew tls status --domain example.com --target ssh://ubuntu@203.0.113.10 --port 8443
  vibew tls status --domain example.com --target ssh://ubuntu@203.0.113.10 --ssh-key ~/.ssh/id_ed25519`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if domain == "" {
				return fmt.Errorf("--domain is required")
			}
			if target == "" {
				return fmt.Errorf("--target is required (e.g. ssh://user@host)")
			}

			t, err := sshadapter.ParseTarget(target)
			if err != nil {
				return fmt.Errorf("invalid --target: %w", err)
			}

			var executor *sshadapter.Executor
			if sshKey != "" {
				executor = sshadapter.NewExecutorWithKey(t, sshKey)
			} else {
				executor = sshadapter.NewExecutor(t)
			}

			svc := tlsstatus.NewService(executor)
			certInfo, err := svc.Inspect(cmd.Context(), domain, port)
			if err != nil {
				return fmt.Errorf("inspecting certificate: %w", err)
			}

			out := cmd.OutOrStdout()
			now := time.Now()

			fmt.Fprintf(out, "TLS Certificate Status for %s:%d\n", domain, port)
			fmt.Fprintln(out, "──────────────────────────────────────────")
			fmt.Fprintf(out, "Subject:    %s\n", certInfo.Subject())
			fmt.Fprintf(out, "Issuer:     %s\n", certInfo.Issuer())
			fmt.Fprintf(out, "Not Before: %s\n", certInfo.NotBefore().Format(time.RFC3339))
			fmt.Fprintf(out, "Not After:  %s\n", certInfo.NotAfter().Format(time.RFC3339))
			if certInfo.Serial() != "" {
				fmt.Fprintf(out, "Serial:     %s\n", certInfo.Serial())
			}

			sans := certInfo.SANs()
			if len(sans) > 0 {
				fmt.Fprintf(out, "SANs:       %s\n", sans[0])
				for _, san := range sans[1:] {
					fmt.Fprintf(out, "            %s\n", san)
				}
			}

			fmt.Fprintf(out, "Status:     %s\n", certInfo.ExpiryStatus(now))

			return nil
		},
	}

	cmd.Flags().StringVar(&domain, "domain", "", "domain name to inspect (required)")
	cmd.Flags().StringVar(&target, "target", "", "remote target in ssh://user@host[:port] format (required)")
	cmd.Flags().StringVar(&sshKey, "ssh-key", "", "path to the SSH private key file (default: use SSH agent / ~/.ssh/config)")
	cmd.Flags().IntVar(&port, "port", 443, "TLS port to connect to on the remote")

	if err := cmd.MarkFlagRequired("domain"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag required registration failed:", err)
	}
	if err := cmd.MarkFlagRequired("target"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag required registration failed:", err)
	}

	return cmd
}
