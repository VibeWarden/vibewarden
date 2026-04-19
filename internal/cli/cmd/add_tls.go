package cmd

import (
	"context"
	"fmt"
	"os"
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

			// Write the domain to vibewarden.production.yaml.
			prodPath := filepath.Join(projDir, "vibewarden.production.yaml")
			if err := upsertDomainInProdConfig(prodPath, domain); err != nil {
				// Non-fatal: the production override file is optional.
				fmt.Fprintf(cmd.OutOrStdout(), "Note: could not update %s: %v\n", prodPath, err)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Domain %q written to %s\n", domain, prodPath)
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
	var m map[string]any

	data, err := os.ReadFile(path) //nolint:gosec // path is the vibewarden.production.yaml resolved from project root
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist — create it with sensible production defaults.
			m = map[string]any{
				"server": map[string]any{"port": 443},
				"tls":    map[string]any{"enabled": true, "provider": "letsencrypt"},
			}
		} else {
			return fmt.Errorf("reading %s: %w", path, err)
		}
	} else {
		if err := yaml.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		if m == nil {
			m = make(map[string]any)
		}
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
