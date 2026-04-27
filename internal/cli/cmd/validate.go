package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	multisiteapp "github.com/vibewarden/vibewarden/internal/app/multisite"
	validateapp "github.com/vibewarden/vibewarden/internal/app/validate"
	"github.com/vibewarden/vibewarden/internal/config"
)

// validTLSProviders is the set of accepted TLS provider values.
var validTLSProviders = map[string]bool{
	"letsencrypt":         true,
	"zerossl":             true,
	"buypass":             true,
	"letsencrypt-staging": true,
	"self-signed":         true,
	"external":            true,
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
// The command loads vibewarden.yaml (or the path supplied via --config or as
// the first positional argument), runs semantic validation rules beyond what
// YAML parsing provides, and reports any errors. It exits with code 0 when
// the configuration is valid and code 1 otherwise.
//
// When both --config and a positional argument are provided, --config takes
// precedence. The positional argument is kept for backward compatibility.
func NewValidateCmd() *cobra.Command {
	var configFlag string

	cmd := &cobra.Command{
		Use:   "validate [config-file]",
		Short: "Validate vibewarden.yaml configuration",
		Long: `Validate the vibewarden.yaml configuration file.

Checks performed:
  - File exists and is valid YAML
  - server.port is in the range 1-65535
  - upstream.port is in the range 1-65535
  - tls.provider is one of: letsencrypt, zerossl, buypass, letsencrypt-staging, self-signed, external
  - tls.domain is required when provider is letsencrypt
  - tls.cert_path and tls.key_path are required when provider is external
  - log.level is one of: debug, info, warn, error
  - log.format is one of: json, text
  - admin.token is required when admin.enabled is true
  - security_headers.frame_option is one of: DENY, SAMEORIGIN, or empty
  - rate_limit.per_ip.requests_per_second is greater than zero
  - rate_limit.per_ip.burst is greater than zero
  - user-management requires auth to be enabled (inter-plugin dependency)

Runtime checks (next-command failure prevention):
  - Name collision: name unset and directory is named "vibewarden" (would collide with sidecar binary)
  - EXPOSE/upstream.port mismatch: Dockerfile EXPOSE port does not match upstream.port
  - Image-tag drift: .env VIBEWARDEN_APP_IMAGE does not match the tag vibew bundle would produce
  - ACME-incompatible domain: tls.domain is localhost, an IP, or a reserved TLD (.local, .test, etc.)
  - WAF prod-mode sanity: WAF enabled with mode: log in production config

Exits with code 0 when configuration is valid, code 1 when invalid.

Examples:
  vibew validate
  vibew validate ./path/to/vibewarden.yaml
  vibew validate --config ./my-vibewarden.yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --config takes precedence over the positional argument.
			configPath := configFlag
			if configPath == "" && len(args) > 0 {
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

			// Strict mode: reject unknown keys in the base file and in the
			// production override (when discovered) so typos like tls.dmain fail
			// loudly at validate time instead of being silently dropped at merge
			// time. The runtime loader (config.Load) stays lenient per ADR-065.
			prodPath := discoverProdOverride(configPath)
			cfg, err := config.LoadStrict(configPath, prodPath)
			if err != nil {
				var unknown *config.UnknownKeyError
				if errors.As(err, &unknown) {
					fmt.Fprintf(cmd.ErrOrStderr(), "Configuration invalid: %s\n", unknown.Error())
					return fmt.Errorf("loading config: %w", err)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Configuration invalid: %v\n", err)
				return fmt.Errorf("loading config: %w", err)
			}

			// Multi-site bundle is post-v1 (#1169). Emit a FAIL row consistent
			// with the OK/OFF/FAIL convention from #1143/#1159 and exit 1 so
			// that agents and scripts detect the limitation immediately.
			checkPath := configPath
			if checkPath == "" {
				checkPath = "vibewarden.yaml"
			}
			absCheck, err := filepath.Abs(checkPath)
			if err != nil {
				return fmt.Errorf("resolving abs path %q: %w", checkPath, err)
			}
			if multisiteapp.IsProject(absCheck) {
				fmt.Fprintf(cmd.ErrOrStderr(), "FAIL  multi-site bundle is post-v1 (see #1169 — N apps on one VM architecture). Dev path works locally; no production deploy path yet.\n")
				return fmt.Errorf("multi-site bundle is post-v1 (see #1169)")
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

			// ADR-089 §G: migration warning when .env still carries the legacy
			// generic tag. Printed to stderr so stdout stays machine-parsable.
			configDir := "."
			if configPath != "" {
				configDir = filepath.Dir(configPath)
			}
			if detectLegacyAppImage(configDir) {
				fmt.Fprintf(cmd.ErrOrStderr(), "\nMigration hint: .env contains VIBEWARDEN_APP_IMAGE=vibewarden-app:latest (the old generic tag).\n")
				fmt.Fprintf(cmd.ErrOrStderr(), "This tag is shared across all projects on this workstation and may cause wrong-app deploys.\n")
				fmt.Fprintf(cmd.ErrOrStderr(), "Update it to your project-scoped tag:\n")
				fmt.Fprintf(cmd.ErrOrStderr(), "  sed -i 's/vibewarden-app:latest/%s-app:latest/g' .env\n", cfg.ComposeProjectName())
				fmt.Fprintf(cmd.ErrOrStderr(), "Regenerate with: vibew bundle --overwrite\n\n")
			}

			// Runtime checks: detect next-command failure modes before they
			// reach vibew bundle or vibew up. Each non-skipped result is printed
			// to stderr; FAIL rows also cause a non-zero exit.
			//
			// When a production override exists the runtime checks must see the
			// merged config (base + production) so that production-only values
			// such as waf.mode are visible. The strict-schema check above already
			// validated both files; a merge failure here is a non-fatal fallback
			// to the base config.
			projectRoot := filepath.Dir(absCheck)
			prodOverrideExists := prodPath != ""
			runtimeCfg := cfg
			if prodOverrideExists {
				if merged, mergeErr := bundleapp.LoadMergedConfig(absCheck, prodPath); mergeErr == nil {
					runtimeCfg = merged
				}
			}
			results, runtimeFailures := validateapp.RunChecks(cmd.Context(), projectRoot, runtimeCfg, prodOverrideExists)
			for _, r := range results {
				fmt.Fprintf(cmd.ErrOrStderr(), "%-6s%s\n", r.State.String(), r.Message)
			}
			if runtimeFailures > 0 {
				return fmt.Errorf("configuration has %d runtime check failure(s)", runtimeFailures)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Configuration valid (%s)\n", displayPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&configFlag, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	if err := cmd.RegisterFlagCompletionFunc("config", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}); err != nil {
		// registration can only fail when called on a non-existent flag; safe to ignore
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}

	return cmd
}

// detectLegacyAppImage reports whether the .env file in configDir contains
// VIBEWARDEN_APP_IMAGE=vibewarden-app:latest (the old project-agnostic tag).
// A missing .env returns false — no migration hint needed. This is a CLI
// concern (not app logic) per ADR-089 §"vibew validate migration warning".
func detectLegacyAppImage(configDir string) bool {
	envPath := filepath.Join(configDir, ".env")
	f, err := os.Open(envPath) //nolint:gosec // envPath is derived from project root
	if err != nil {
		return false // .env absent is fine
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "VIBEWARDEN_APP_IMAGE=vibewarden-app:latest" {
			return true
		}
	}
	return false
}

// discoverProdOverride returns the path to vibewarden.production.yaml that
// sits next to configPath, when it exists. The empty string is returned when
// configPath is empty, the override file does not exist, or the directory
// cannot be read. Strict validation still succeeds when no override is
// present.
func discoverProdOverride(configPath string) string {
	if configPath == "" {
		return ""
	}
	dir := filepath.Dir(configPath)
	candidate := filepath.Join(dir, "vibewarden.production.yaml")
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
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
		errs = append(errs, fmt.Sprintf("tls.provider must be one of letsencrypt, zerossl, buypass, letsencrypt-staging, self-signed, external; got %q", cfg.TLS.Provider))
	}

	// tls: ACME providers require a domain
	acmeProviders := map[string]bool{
		"letsencrypt": true, "zerossl": true, "buypass": true, "letsencrypt-staging": true,
	}
	if cfg.TLS.Enabled && acmeProviders[cfg.TLS.Provider] && cfg.TLS.Domain == "" {
		errs = append(errs, fmt.Sprintf("tls.domain is required when tls.provider is %s", cfg.TLS.Provider))
	}

	// tls: zerossl requires email
	if cfg.TLS.Enabled && cfg.TLS.Provider == "zerossl" && cfg.TLS.Email == "" {
		errs = append(errs, "tls.email is required when tls.provider is zerossl")
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
	if cfg.Admin.Enabled && !cfg.Auth.Active() {
		errs = append(errs, "user-management plugin requires auth to be enabled (set auth.mode to \"kratos\", \"jwt\", or \"api-key\")")
	}

	return errs
}
