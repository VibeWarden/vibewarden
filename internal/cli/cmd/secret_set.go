package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibewarden/vibewarden/internal/config"
)

// newSecretSetCmd creates the `vibew secret set <path> <key=value>...` subcommand.
//
// It writes one or more key/value pairs to the configured secret store at the
// given path. This requires the builtin encrypted file store (or OpenBao) to be
// configured with valid credentials.
func newSecretSetCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "set <path> <key=value>...",
		Short: "Write a secret to the secret store",
		Long: `Write one or more key=value pairs to the secret store at the given path.

The secret is encrypted and stored in the configured secret store (builtin by
default). Requires VIBEWARDEN_SECRETS_MASTER_KEY env var or
secrets.builtin.key_file config for the builtin store.

Examples:
  vibew secret set app/db password=s3cret host=db.example.com
  vibew secret set app/api-key token=sk_live_abc123`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			data, err := parseKeyValueArgs(args[1:])
			if err != nil {
				return err
			}

			cfg, err := loadConfigForCLI(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			store, err := buildSecretStore(cfg)
			if err != nil {
				return fmt.Errorf("building secret store: %w", err)
			}
			if store == nil {
				return fmt.Errorf("no secret store available; set VIBEWARDEN_SECRETS_MASTER_KEY or configure secrets.builtin.key_file")
			}

			if err := store.Put(cmd.Context(), path, data); err != nil {
				return fmt.Errorf("writing secret: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Secret written to %q (%d key(s))\n", path, len(data))
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	return cmd
}

// parseKeyValueArgs parses a list of "key=value" strings into a map.
// Returns an error if any argument is not in "key=value" format.
func parseKeyValueArgs(args []string) (map[string]string, error) {
	data := make(map[string]string, len(args))
	for _, arg := range args {
		idx := strings.Index(arg, "=")
		if idx < 1 {
			return nil, fmt.Errorf("invalid key=value format: %q (expected key=value)", arg)
		}
		key := arg[:idx]
		value := arg[idx+1:]
		data[key] = value
	}
	return data, nil
}

// loadConfigForCLI loads the config for CLI commands, returning a default
// config when the config file does not exist.
func loadConfigForCLI(configPath string) (*config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		// Return a default config so the builtin store can still be used
		// with just the VIBEWARDEN_SECRETS_MASTER_KEY env var.
		return &config.Config{}, nil
	}
	return cfg, nil
}
