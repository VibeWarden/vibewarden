package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// newAddTLSCmd creates the `vibew add tls` subcommand.
//
// This command enables TLS in vibewarden.yaml with a domain and provider.
// When --domain is provided, the domain is also written to
// vibewarden.production.yaml if that file exists.
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
When --domain is provided, the domain is also written to
vibewarden.production.yaml if that file exists.

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
			if err := runAddFeature(cmd, dir, domainscaffold.FeatureTLS, opts); err != nil {
				return err
			}

			// Also write the domain to vibewarden.production.yaml when it exists.
			if domain != "" {
				projDir := dir
				if projDir == "" {
					projDir = "."
				}
				prodPath := filepath.Join(projDir, "vibewarden.production.yaml")
				if err := upsertDomainInProdConfig(prodPath, domain); err != nil {
					// Non-fatal: the production override file is optional.
					fmt.Fprintf(cmd.OutOrStdout(), "Note: could not update %s: %v\n", prodPath, err)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&domain, "domain", "", "domain for TLS certificate (required)")
	cmd.Flags().StringVar(&provider, "provider", "letsencrypt", `TLS provider: "letsencrypt", "self-signed", or "external"`)

	return cmd
}

// upsertDomainInProdConfig reads vibewarden.production.yaml, sets
// tls.domain to the given value, and writes the file back. If the file does
// not exist, it is silently skipped (returns nil).
func upsertDomainInProdConfig(path, domain string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path is the vibewarden.production.yaml resolved from project root
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no production override file — nothing to update
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	if m == nil {
		m = make(map[string]any)
	}

	// Ensure tls section exists.
	tls, ok := m["tls"].(map[string]any)
	if !ok {
		tls = make(map[string]any)
		m["tls"] = tls
	}
	tls["domain"] = domain

	out, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", path, err)
	}

	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
