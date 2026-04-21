package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	yamlmodadapter "github.com/vibewarden/vibewarden/internal/adapters/yamlmod"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// newAddTLSCmd creates the `vibew add tls` subcommand.
//
// This command enables TLS in vibewarden.yaml with a domain and provider.
//
// When TLS is already enabled in the base config, the command does NOT modify
// vibewarden.yaml's tls section (preserving the existing provider, typically
// "self-signed" for local dev). The --domain is written only to
// vibewarden.production.yaml.
//
// When TLS is not yet enabled, the command enables it in vibewarden.yaml and
// also writes the domain to vibewarden.production.yaml.
func newAddTLSCmd() *cobra.Command {
	var (
		domain   string
		provider string
		email    string
	)

	cmd := &cobra.Command{
		Use:   "tls [directory]",
		Short: "Enable TLS termination",
		Long: `Enable TLS termination in vibewarden.yaml.

When TLS is not yet enabled, updates the tls section with enabled: true plus
provider settings. The --domain is written to vibewarden.production.yaml (not
to vibewarden.yaml) so that the base config stays correct for local dev.

When TLS is already enabled, vibewarden.yaml is left unchanged and the domain
is written only to vibewarden.production.yaml.

Supported providers:
  letsencrypt          Automatic certificate with fallback chain: LE -> ZeroSSL -> Buypass (default)
  zerossl              ZeroSSL only (requires --email for automatic EAB registration)
  buypass              Buypass Go SSL only
  letsencrypt-staging  Let's Encrypt staging (for testing, no rate limits)
  self-signed          Self-signed certificate for local/internal use
  external             You manage the certificate (Cloudflare, registrar, AWS ACM, etc.)

Run 'vibew wrap' first if vibewarden.yaml does not exist.

Examples:
  vibew add tls --domain example.com
  vibew add tls --domain example.com --provider letsencrypt --email admin@example.com
  vibew add tls --domain example.com --provider zerossl --email admin@example.com
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

			projDir := dir
			if projDir == "" {
				projDir = "."
			}

			configPath := filepath.Join(projDir, "vibewarden.yaml")

			// Check if TLS is already enabled in the base config.
			toggler := yamlmodadapter.NewToggler()
			state, err := toggler.ReadFeatures(context.Background(), configPath)
			if err != nil {
				return fmt.Errorf("reading config: %w", err)
			}

			if state.TLSEnabled {
				// TLS is already enabled in the base config. Do NOT modify
				// vibewarden.yaml — the provider (typically "self-signed"
				// for dev) must remain unchanged. Only write the domain to
				// the production override file.
				fmt.Fprintf(cmd.OutOrStdout(), "TLS is already enabled in vibewarden.yaml (provider unchanged).\n")
			} else {
				// TLS is not yet enabled — enable it in vibewarden.yaml
				// WITHOUT the domain (domain belongs in the production file).
				opts := domainscaffold.FeatureOptions{
					TLSProvider: provider,
				}
				if err := runAddFeature(cmd, dir, domainscaffold.FeatureTLS, opts); err != nil {
					return err
				}
			}

			// Write the domain and email to vibewarden.production.yaml.
			prodPath := filepath.Join(projDir, "vibewarden.production.yaml")
			prodDiff, prodErr := upsertTLSFieldsInProdConfig(prodPath, domain, email)
			if prodErr != nil {
				// Parse failure is fatal — we must not silently regenerate
				// the file and destroy the user's edits. The error already
				// carries the "run `vibew validate`" remediation.
				return fmt.Errorf("updating %s: %w", prodPath, prodErr)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Domain %q written to %s\n", domain, prodPath)
			if email != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Email %q written to %s\n", email, prodPath)
			}
			PrintAddSummary(cmd.OutOrStdout(), prodDiff)

			return nil
		},
	}

	cmd.Flags().StringVar(&domain, "domain", "", "domain for TLS certificate (required)")
	cmd.Flags().StringVar(&provider, "provider", "letsencrypt", `TLS provider: "letsencrypt", "zerossl", "buypass", "letsencrypt-staging", "self-signed", or "external"`)
	cmd.Flags().StringVar(&email, "email", "", "ACME account email for certificate notifications and EAB registration (required for zerossl)")

	return cmd
}

// upsertTLSFieldsInProdConfig reads vibewarden.production.yaml, sets
// tls.domain and optionally tls.email to the given values, and writes the
// file back while preserving comments and ordering. When the file does not
// exist, it is created with sensible production defaults via
// productionSeedFactory.
//
// If the file exists but cannot be parsed as YAML, this function returns a
// wrapped error pointing the user at `vibew validate` — it never regenerates
// the file from scratch, which would silently destroy the user's edits.
func upsertTLSFieldsInProdConfig(path, domain, email string) (domainscaffold.Diff, error) {
	return yamlmodadapter.UpsertFields(
		path,
		productionSeedFactory,
		func(root *yaml.Node, b *yamlmodadapter.DiffBuilder) error {
			yamlmodadapter.UpsertScalar(root, b, "tls", "domain", domain, "!!str")
			if email != "" {
				yamlmodadapter.UpsertScalar(root, b, "tls", "email", email, "!!str")
			}
			return nil
		},
	)
}

// productionSeedFactory returns the seed mapping written to a freshly-created
// vibewarden.production.yaml. It matches the former map-round-trip defaults:
// server.port = 443 and tls.enabled = true with provider = letsencrypt.
func productionSeedFactory() *yaml.Node {
	server := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	server.Content = append(server.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "port"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "443"},
	)

	tls := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	tls.Content = append(tls.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "provider"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "letsencrypt"},
	)

	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "server"},
		server,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tls"},
		tls,
	)
	return root
}
