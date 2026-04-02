package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// newAddTLSCmd creates the `vibew add tls` subcommand.
//
// This command enables TLS in vibewarden.yaml with a domain and provider.
func newAddTLSCmd() *cobra.Command {
	var (
		domain   string
		provider string
	)

	cmd := &cobra.Command{
		Use:   "tls [directory]",
		Short: "Enable TLS termination",
		Long: `Enable TLS termination in vibewarden.yaml.

Updates the tls section with enabled: true plus domain and provider settings.
Supported providers:
  letsencrypt   Automatic certificate from Let's Encrypt (default, requires public domain)
  self-signed   Self-signed certificate for local/internal use
  external      You manage the certificate (Cloudflare, registrar, AWS ACM, etc.)

Run 'vibew wrap' first if vibewarden.yaml does not exist.

Examples:
  vibew add tls --domain example.com
  vibew add tls --domain example.com --provider letsencrypt
  vibew add tls --domain internal.corp --provider self-signed`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if domain == "" {
				return fmt.Errorf("--domain is required (e.g. --domain example.com)")
			}

			dir := ""
			if len(args) > 0 {
				dir = args[0]
			}

			opts := domainscaffold.FeatureOptions{
				TLSDomain:   domain,
				TLSProvider: provider,
			}
			return runAddFeature(cmd, dir, domainscaffold.FeatureTLS, opts)
		},
	}

	cmd.Flags().StringVar(&domain, "domain", "", "domain for TLS certificate (required)")
	cmd.Flags().StringVar(&provider, "provider", "letsencrypt", `TLS provider: "letsencrypt", "self-signed", or "external"`)

	return cmd
}
