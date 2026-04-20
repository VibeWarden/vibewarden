package cmd

import (
	"context"
	"fmt"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// loadAndResolve loads the config using LoadRaw (skipping validation), resolves
// any secret:// URIs via the builtin secret store, then validates the result.
//
// This is the standard config loading sequence for commands that support
// secret:// URIs in their config fields. The secrets.* config section itself is
// never resolved (bootstrap constraint).
//
// When the secrets plugin is not enabled or the master key is unavailable, the
// config is still loaded and validated normally -- secret:// URIs will cause a
// resolution error only when they are actually present in the config.
func loadAndResolve(ctx context.Context, configPath string) (*config.Config, error) {
	cfg, err := config.LoadRaw(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	if err := resolveConfigSecrets(ctx, cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// resolveConfigSecrets builds a SecretKVReader from the config's secrets
// section and resolves any secret:// URIs in the config. When the secret store
// cannot be built (e.g. master key not available), resolution is skipped --
// any secret:// URIs remaining in the config will be used as-is (and likely
// fail downstream validation or at runtime).
func resolveConfigSecrets(ctx context.Context, cfg *config.Config) error {
	store, err := buildSecretKVReader(cfg)
	if err != nil {
		return fmt.Errorf("building secret store for config resolution: %w", err)
	}
	if store == nil {
		// No secret store available; skip resolution.
		return nil
	}

	if err := config.ResolveSecrets(ctx, cfg, store); err != nil {
		return fmt.Errorf("resolving secret:// URIs in config: %w", err)
	}
	return nil
}

// buildSecretKVReader creates a read-only SecretKVReader from the config's
// secrets section. Returns nil when the store cannot be built (e.g. master key
// not configured).
func buildSecretKVReader(cfg *config.Config) (ports.SecretKVReader, error) {
	store, err := buildSecretStore(cfg)
	if err != nil {
		return nil, err
	}
	return store, nil
}
