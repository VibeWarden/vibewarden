package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	openbao "github.com/vibewarden/vibewarden/internal/adapters/openbao"
	appsecret "github.com/vibewarden/vibewarden/internal/app/secret"
	"github.com/vibewarden/vibewarden/internal/config"
	domainsecret "github.com/vibewarden/vibewarden/internal/domain/secret"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// newSecretGetCmd creates the `vibew secret get <alias-or-path>` subcommand.
//
// It retrieves credentials for a well-known service alias or an arbitrary
// OpenBao path. OpenBao is tried first; on failure the .credentials file is
// used as a fallback (only for well-known aliases).
func newSecretGetCmd() *cobra.Command {
	var (
		configPath string
		outputDir  string
		asJSON     bool
		asEnv      bool
	)

	cmd := &cobra.Command{
		Use:   "get <alias-or-path>",
		Short: "Retrieve credentials for a service alias or OpenBao path",
		Long: `Retrieve credentials for a well-known service alias or an arbitrary OpenBao path.

Well-known aliases: postgres, kratos, grafana, openbao

OpenBao is queried first. If it is not running, the .credentials file in the
output directory is used as a fallback (only for well-known aliases).

Output formats:
  Default  — human-readable key: value pairs
  --json   — JSON object
  --env    — export KEY=value lines suitable for sourcing in a shell

Examples:
  vibew secret get postgres
  vibew secret get kratos --json
  vibew secret get grafana --env
  vibew secret get demo/api-key`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON && asEnv {
				return fmt.Errorf("--json and --env are mutually exclusive")
			}

			aliasOrPath := args[0]

			svc, err := buildSecretService(configPath, outputDir)
			if err != nil {
				return fmt.Errorf("initialising secret service: %w", err)
			}

			result, err := svc.Get(cmd.Context(), aliasOrPath)
			if err != nil {
				return formatSecretGetError(err, aliasOrPath)
			}

			switch {
			case asJSON:
				return printSecretJSON(cmd, result)
			case asEnv:
				return printSecretEnv(cmd, result)
			default:
				return printSecretHuman(cmd, result)
			}
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&outputDir, "output-dir", defaultOutputDir, "directory containing the .credentials file")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as a JSON object")
	cmd.Flags().BoolVar(&asEnv, "env", false, "output as export KEY=value lines")

	return cmd
}

// defaultOutputDir is the standard generated files directory.
const defaultOutputDir = ".vibewarden/generated"

// buildSecretService constructs the SecretService, wiring OpenBao and credentials adapters.
// If OpenBao is not configured in vibewarden.yaml, a nil SecretStore is passed
// to the service (falling back to .credentials for all lookups).
func buildSecretService(configPath, outputDir string) (*appsecret.Service, error) {
	if outputDir == "" {
		outputDir = defaultOutputDir
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		// Config may not exist in simple setups — proceed with credentials-file-only mode.
		return appsecret.NewService(nil, credentialsadapter.NewStore(), outputDir), nil
	}

	var secretStore ports.SecretStore
	if cfg.Secrets.OpenBao.Address != "" {
		adapter := openbao.New(openbao.Config{
			Address:   cfg.Secrets.OpenBao.Address,
			MountPath: cfg.Secrets.OpenBao.MountPath,
			Auth: openbao.AuthConfig{
				Method:   openbao.AuthMethod(cfg.Secrets.OpenBao.Auth.Method),
				Token:    cfg.Secrets.OpenBao.Auth.Token,
				RoleID:   cfg.Secrets.OpenBao.Auth.RoleID,
				SecretID: cfg.Secrets.OpenBao.Auth.SecretID,
			},
		}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
		secretStore = adapter
	}

	return appsecret.NewService(secretStore, credentialsadapter.NewStore(), outputDir), nil
}

// formatSecretGetError converts service errors into user-friendly messages.
func formatSecretGetError(err error, aliasOrPath string) error {
	switch {
	case isErrNoSourceAvailable(err):
		//nolint:revive,staticcheck // user-facing CLI hint: intentionally capitalised with trailing period
		return fmt.Errorf("No secret source available. Run 'vibew generate' to create credentials, or start the stack with 'vibew dev'.") //nolint:revive,staticcheck
	case isErrSecretNotFound(err):
		//nolint:revive,staticcheck // user-facing CLI hint: intentionally capitalised with trailing period
		return fmt.Errorf("Secret %q not found. Use 'vibew secret list' to see available secrets.", aliasOrPath) //nolint:revive,staticcheck
	default:
		return err
	}
}

// isErrNoSourceAvailable reports whether err wraps ErrNoSourceAvailable.
func isErrNoSourceAvailable(err error) bool {
	return errors.Is(err, appsecret.ErrNoSourceAvailable)
}

// isErrSecretNotFound reports whether err wraps ErrSecretNotFound.
func isErrSecretNotFound(err error) bool {
	return errors.Is(err, appsecret.ErrSecretNotFound)
}

// printSecretHuman writes human-readable key: value output to cmd's stdout.
func printSecretHuman(cmd *cobra.Command, result *domainsecret.RetrievedSecret) error {
	label := result.Path
	if result.Alias != "" {
		label = result.Alias
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s credentials (source: %s):\n", label, result.Source)

	// Print keys in sorted order for deterministic output.
	keys := sortedKeys(result.Data)
	for _, k := range keys {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", k, result.Data[k])
	}
	return nil
}

// printSecretJSON writes the data map as a compact JSON object to cmd's stdout.
func printSecretJSON(cmd *cobra.Command, result *domainsecret.RetrievedSecret) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetEscapeHTML(false)
	if err := enc.Encode(result.Data); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	return nil
}

// printSecretEnv writes export KEY=value lines to cmd's stdout.
// Key names are upper-cased and prefixed with the alias env prefix when available.
func printSecretEnv(cmd *cobra.Command, result *domainsecret.RetrievedSecret) error {
	alias := domainsecret.ResolveAlias(result.Alias)

	keys := sortedKeys(result.Data)
	for _, k := range keys {
		envKey := buildEnvKey(alias, k)
		fmt.Fprintf(cmd.OutOrStdout(), "export %s=%s\n", envKey, result.Data[k])
	}
	return nil
}

// buildEnvKey constructs an environment variable name from the alias prefix
// (if any) and the field name.
func buildEnvKey(alias *domainsecret.WellKnownAlias, fieldName string) string {
	upper := strings.ToUpper(fieldName)
	if alias == nil || alias.EnvPrefix == "" {
		return upper
	}
	// Avoid double-prefixing when the field already starts with the prefix.
	if strings.HasPrefix(upper, alias.EnvPrefix) {
		return upper
	}
	return alias.EnvPrefix + upper
}

// sortedKeys returns the keys of m sorted alphabetically.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
