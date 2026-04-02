package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/vibewarden/vibewarden/internal/config"
)

// validTLSProviders is the set of accepted TLS provider values.
var validTLSProviders = map[string]bool{
	"letsencrypt": true,
	"self-signed": true,
	"external":    true,
}

// validLogLevels is the set of accepted log level values.
var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// validLogFormats is the set of accepted log format values.
var validLogFormats = map[string]bool{
	"json": true,
	"text": true,
}

// validFrameOptions is the set of accepted X-Frame-Options values.
var validFrameOptions = map[string]bool{
	"":           true, // empty = disabled
	"DENY":       true,
	"SAMEORIGIN": true,
}

// NewValidateCmd creates the `vibew validate` subcommand.
//
// The command loads vibewarden.yaml (or the path supplied as the first
// positional argument), runs semantic validation rules beyond what YAML
// parsing provides, and reports any errors. It exits with code 0 when the
// configuration is valid and code 1 otherwise.
func NewValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [config-file]",
		Short: "Validate vibewarden.yaml configuration",
		Long: `Validate the vibewarden.yaml configuration file.

Checks performed:
  - File exists and is valid YAML
  - server.port is in the range 1-65535
  - upstream.port is in the range 1-65535
  - tls.provider is one of: letsencrypt, self-signed, external
  - tls.domain is required when provider is letsencrypt
  - tls.cert_path and tls.key_path are required when provider is external
  - log.level is one of: debug, info, warn, error
  - log.format is one of: json, text
  - admin.token is required when admin.enabled is true
  - security_headers.frame_option is one of: DENY, SAMEORIGIN, or empty
  - rate_limit.per_ip.requests_per_second is greater than zero
  - rate_limit.per_ip.burst is greater than zero
  - user-management requires auth to be enabled (inter-plugin dependency)

Exits with code 0 when configuration is valid, code 1 when invalid.

Examples:
  vibew validate
  vibew validate ./path/to/vibewarden.yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := ""
			if len(args) > 0 {
				configPath = args[0]
			}

			displayPath := configPath
			if displayPath == "" {
				displayPath = "vibewarden.yaml"
			}

			// Check file existence explicitly when a path is given, so we
			// can report a clear error rather than a generic viper message.
			if configPath != "" {
				if _, err := os.Stat(configPath); err != nil {
					if os.IsNotExist(err) {
						return fmt.Errorf("config file not found: %s", configPath)
					}
					return fmt.Errorf("accessing config file: %w", err)
				}
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Configuration invalid: %v\n", err)
				return fmt.Errorf("loading config: %w", err)
			}

			errs := validateConfig(cfg)
			if len(errs) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Configuration invalid (%s):\n", displayPath)
				for _, e := range errs {
					fmt.Fprintf(cmd.ErrOrStderr(), "  - %s\n", e)
				}
				// Return a sentinel error so cobra exits with code 1, but
				// keep the message on stderr only (printed above).
				return fmt.Errorf("configuration has %d error(s)", len(errs))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Configuration valid (%s)\n", displayPath)
			return nil
		},
	}

	return cmd
}

// validateConfig checks semantic constraints on cfg that cannot be expressed
// in the YAML schema. It returns one entry per violation.
func validateConfig(cfg *config.Config) []string {
	var errs []string

	// server.port
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		errs = append(errs, fmt.Sprintf("server.port must be between 1 and 65535, got %d", cfg.Server.Port))
	}

	// upstream.port
	if cfg.Upstream.Port < 1 || cfg.Upstream.Port > 65535 {
		errs = append(errs, fmt.Sprintf("upstream.port must be between 1 and 65535, got %d", cfg.Upstream.Port))
	}

	// tls.provider
	if !validTLSProviders[cfg.TLS.Provider] {
		errs = append(errs, fmt.Sprintf("tls.provider must be one of letsencrypt, self-signed, external; got %q", cfg.TLS.Provider))
	}

	// tls: letsencrypt requires a domain
	if cfg.TLS.Enabled && cfg.TLS.Provider == "letsencrypt" && cfg.TLS.Domain == "" {
		errs = append(errs, "tls.domain is required when tls.provider is letsencrypt")
	}

	// tls: external requires cert_path and key_path
	if cfg.TLS.Enabled && cfg.TLS.Provider == "external" {
		if cfg.TLS.CertPath == "" {
			errs = append(errs, "tls.cert_path is required when tls.provider is external")
		}
		if cfg.TLS.KeyPath == "" {
			errs = append(errs, "tls.key_path is required when tls.provider is external")
		}
	}

	// log.level
	if !validLogLevels[cfg.Log.Level] {
		errs = append(errs, fmt.Sprintf("log.level must be one of debug, info, warn, error; got %q", cfg.Log.Level))
	}

	// log.format
	if !validLogFormats[cfg.Log.Format] {
		errs = append(errs, fmt.Sprintf("log.format must be one of json, text; got %q", cfg.Log.Format))
	}

	// admin.token required when admin is enabled
	if cfg.Admin.Enabled && cfg.Admin.Token == "" {
		errs = append(errs, "admin.token is required when admin.enabled is true (run: vibew secret generate --admin-token)")
	}

	// security_headers.frame_option
	if !validFrameOptions[cfg.SecurityHeaders.FrameOption] {
		errs = append(errs, fmt.Sprintf("security_headers.frame_option must be DENY, SAMEORIGIN, or empty; got %q", cfg.SecurityHeaders.FrameOption))
	}

	// rate_limit.per_ip values when rate limiting is enabled
	if cfg.RateLimit.Enabled {
		if cfg.RateLimit.PerIP.RequestsPerSecond <= 0 {
			errs = append(errs, "rate_limit.per_ip.requests_per_second must be greater than zero")
		}
		if cfg.RateLimit.PerIP.Burst <= 0 {
			errs = append(errs, "rate_limit.per_ip.burst must be greater than zero")
		}
	}

	// Plugin inter-dependency: user-management requires auth.
	if cfg.Admin.Enabled && !cfg.Auth.Enabled {
		errs = append(errs, "user-management plugin requires auth to be enabled (set auth.enabled: true)")
	}

	return errs
}
