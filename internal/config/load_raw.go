package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// LoadRaw loads and unmarshals the configuration from configPath (or the
// default search paths when configPath is empty) without running Validate.
//
// This is used when secret:// URIs must be resolved before validation, because
// unresolved URIs in fields like tls.domain or database.url would fail
// Validate's checks. The typical call sequence is:
//
//	cfg, err := config.LoadRaw(configPath)
//	err = config.ResolveSecrets(ctx, cfg, store)
//	err = cfg.Validate()
//
// All conditional defaults (acme normalisation, TLS storage path) are applied
// exactly as in Load.
func LoadRaw(configPath string) (*Config, error) {
	return loadInternal(configPath, false)
}

// loadInternal is the shared implementation for Load and LoadRaw. When
// validate is true, cfg.Validate() is called before returning.
func loadInternal(configPath string, validate bool) (*Config, error) {
	v := viper.New()

	setDefaults(v)

	// Config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("vibewarden")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/vibewarden")
	}

	// Environment variables
	v.SetEnvPrefix("VIBEWARDEN")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file (ignore "not found" error — env vars may be sufficient)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	// Reject removed keys before unmarshal.
	if err := rejectRemovedAuthEnabled(v); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Populate ProjectRoot from the resolved config file path so that
	// cfg.ComposeProjectName() can use the dirname fallback for legacy projects
	// without name: set in vibewarden.yaml. This is the single authoritative
	// assignment; callers of ComposeProjectName() MUST use the config returned
	// by loadInternal (or Load / LoadRaw) — do not set ProjectRoot manually.
	if used := v.ConfigFileUsed(); used != "" {
		if abs, err := filepath.Abs(used); err == nil {
			cfg.ProjectRoot = filepath.Dir(abs)
		}
	}

	// Apply conditional defaults that depend on the values of other fields.
	if cfg.TLS.Provider == "acme" {
		cfg.TLS.Provider = "letsencrypt"
	}

	if cfg.TLS.Provider == "letsencrypt" && cfg.TLS.StoragePath == "" {
		cfg.TLS.StoragePath = "/root/.local/share/caddy"
	}

	if validate {
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
	}

	return &cfg, nil
}
