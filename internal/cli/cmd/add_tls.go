package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	yamlmodadapter "github.com/vibewarden/vibewarden/internal/adapters/yamlmod"
	tlsdomain "github.com/vibewarden/vibewarden/internal/app/tlsdomain"
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
//
// Provider auto-derivation (when --provider is not explicitly set):
//   - ACME-compatible domain → writes provider: letsencrypt + domain to production.yaml.
//   - ACME-incompatible domain (localhost, IP, reserved TLD) → writes only domain to
//     production.yaml; prints a hint to stderr.
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

Provider auto-derivation (when --provider is not supplied):
  ACME-compatible domain  → provider: letsencrypt written to production.yaml
  localhost / IP / reserved TLD → only domain written; stderr hint printed

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

			// Validate --provider value up-front, before any file writes.
			providerChanged := cmd.Flags().Changed("provider")
			if providerChanged && !validTLSProviders[provider] {
				return fmt.Errorf("unknown --provider %q; valid: letsencrypt, zerossl, buypass, letsencrypt-staging, self-signed, external", provider)
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

			// Determine which provider (if any) to write to production.yaml.
			prodProvider := resolveProdProvider(cmd, domain, provider, providerChanged)

			// Write the domain, optional provider, and optional email to
			// vibewarden.production.yaml.
			prodPath := filepath.Join(projDir, "vibewarden.production.yaml")
			prodDiff, prodErr := upsertTLSFieldsInProdConfig(prodPath, domain, email, prodProvider)
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

// resolveProdProvider decides which provider string (if any) to write to
// vibewarden.production.yaml, according to the following rules:
//
//   - Explicit --provider flag → always honor it (write provider).
//   - ACME-compatible domain + no explicit flag → write "letsencrypt".
//   - ACME-incompatible domain + no explicit flag → write nothing; emit hint to stderr.
func resolveProdProvider(cmd *cobra.Command, domain, provider string, providerChanged bool) string {
	if providerChanged {
		return provider
	}

	incompatible, reason := tlsdomain.IsACMEIncompatible(domain)
	if incompatible {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"hint: domain %q is %s; Let's Encrypt cannot issue for it. tls.provider stays at the base default (self-signed). To override in production.yaml, re-run with --provider self-signed (dev) or --provider external (you manage TLS).\n",
			domain, reason,
		)
		return ""
	}

	return "letsencrypt"
}

// upsertTLSFieldsInProdConfig reads vibewarden.production.yaml, sets
// tls.domain, optionally tls.provider, and optionally tls.email to the given
// values, and writes the file back while preserving comments and ordering.
// When the file does not exist, it is created with sensible production
// defaults via productionSeedFactory.
//
// provider is written only when it is non-empty. Pass an empty string to leave
// any existing tls.provider key in the file untouched.
//
// If the file exists but cannot be parsed as YAML, this function returns a
// wrapped error pointing the user at `vibew validate` — it never regenerates
// the file from scratch, which would silently destroy the user's edits.
func upsertTLSFieldsInProdConfig(path, domain, email, provider string) (domainscaffold.Diff, error) {
	return yamlmodadapter.UpsertFields(
		path,
		productionSeedFactory,
		func(root *yaml.Node, b *yamlmodadapter.DiffBuilder) error {
			yamlmodadapter.UpsertScalar(root, b, "tls", "domain", domain, "!!str")
			if provider != "" {
				yamlmodadapter.UpsertScalar(root, b, "tls", "provider", provider, "!!str")
			}
			if email != "" {
				yamlmodadapter.UpsertScalar(root, b, "tls", "email", email, "!!str")
			}
			return nil
		},
	)
}

// productionSeedFactory returns the seed mapping written to a freshly-created
// vibewarden.production.yaml. It sets server.port = 443 and tls.enabled = true.
// The tls.provider is intentionally omitted here; it is written by
// upsertTLSFieldsInProdConfig based on domain classification.
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
