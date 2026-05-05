package config

import (
	"fmt"
	"regexp"
	"strings"
)

// DeployConfig holds settings consumed by `vibew bundle` when producing the
// deploy artifact. Fields here describe the deploy *target* — they have no
// effect on the running sidecar (vibewarden serve never reads them).
type DeployConfig struct {
	// TargetPlatform is the expected deployment platform, in the standard
	// Docker Buildx format "<os>/<arch>" (e.g. "linux/amd64", "linux/arm64").
	// `vibew bundle` compares this against the bundled image architecture
	// and aborts on mismatch with an actionable rebuild command.
	//
	// Default: "linux/amd64" (Hetzner and most cloud VMs).
	TargetPlatform string `mapstructure:"target_platform"`

	// Host is the SSH target used in the "Next: deploy" block printed by
	// `vibew bundle` and in the bundle README fenced deploy block. When set,
	// the validated value (e.g. "alice@host.example" or a ~/.ssh/config alias)
	// is substituted (single-quoted) into all three ssh lines. When empty (the
	// default), the bracketed placeholder "<your-ssh-user>@<your-ssh-host>"
	// is used and a hint paragraph is appended.
	//
	// Accepted forms (validated at config-load time):
	//   - plain host/alias:      [a-zA-Z0-9.-]+
	//   - user@host:             [a-zA-Z0-9._-]+@[a-zA-Z0-9.-]+
	//   - user@host:port:        [a-zA-Z0-9._-]+@[a-zA-Z0-9.-]+:[0-9]+
	//   - IPv4 bare or :port:    [0-9.]+ or [0-9.]+:[0-9]+
	//
	// Shell metacharacters (;, &, |, $, backtick, newlines, etc.) are rejected
	// to prevent command injection when the value is interpolated into shell strings.
	Host string `mapstructure:"host"`
}

// deployHostRe is the allowlist regexp for deploy.host values.
var deployHostRe = regexp.MustCompile(
	`^([a-zA-Z0-9._-]+@)?` +
		`([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?` +
		`(\.[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?)*` +
		`|[0-9]{1,3}(\.[0-9]{1,3}){3})` +
		`(:[0-9]{1,5})?$`,
)

// ValidateDeployHost reports whether host is a safe SSH target value. An empty
// host is always valid (treated as use placeholder). Non-empty values must
// match the DNS/IPv4 allowlist; values containing shell metacharacters or
// whitespace are rejected.
func ValidateDeployHost(host string) error {
	if host == "" {
		return nil
	}
	if strings.ContainsAny(host, " \t\n\r") {
		return fmt.Errorf(
			"deploy.host %q contains whitespace -- only DNS names, IPv4 addresses, "+
				"and optional user@ / :port suffixes are accepted "+
				`(e.g. "alice@host.example" or "1.2.3.4:22")`,
			host,
		)
	}
	if !deployHostRe.MatchString(host) {
		return fmt.Errorf(
			"deploy.host %q is invalid -- only DNS names, IPv4 addresses, "+
				"and optional user@ / :port suffixes are accepted "+
				`(e.g. "alice@host.example", "host.example:2222", "1.2.3.4"); `+
				"shell metacharacters are not permitted",
			host,
		)
	}
	return nil
}

// ShellQuoteSingleDeploy wraps a deploy host value in POSIX single-quotes for
// safe interpolation into shell command strings. Embedded single-quotes are
// escaped using the POSIX end-quote + backslash-single-quote + re-open-quote
// idiom so the result is always valid shell regardless of input content. This
// is defence-in-depth: ValidateDeployHost already rejects values containing
// single-quotes at config-load time, but the escape ensures safety at any
// future call site that bypasses validation.
func ShellQuoteSingleDeploy(host string) string {
	safe := strings.ReplaceAll(host, "'", `'\''`)
	return "'" + safe + "'"
}

// validateDeploy validates the deploy section of Config and returns a slice of
// error strings. Called by Config.Validate().
func validateDeploy(c *Config) []string {
	if err := ValidateDeployHost(c.Deploy.Host); err != nil {
		return []string{err.Error()}
	}
	return nil
}
