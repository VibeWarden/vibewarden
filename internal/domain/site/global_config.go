package site

import (
	"errors"
	"fmt"
	"net"
)

// GlobalConfig holds per-VM settings that apply to the VibeWarden sidecar
// as a whole, independent of any individual site. It is loaded from
// global.yaml and is a standalone value object (not embedding Config).
type GlobalConfig struct {
	// AdminToken is the bearer token for the sidecar's admin API.
	// Must be non-empty in production.
	AdminToken string

	// ListenHost is the address the sidecar binds to (e.g. "0.0.0.0").
	// Defaults to "0.0.0.0" when empty.
	ListenHost string

	// ListenPort is the port the sidecar listens on for HTTPS traffic.
	// Defaults to 443 when zero.
	ListenPort int

	// LogLevel controls the sidecar-wide log verbosity (e.g. "info", "debug").
	// Defaults to "info" when empty.
	LogLevel string

	// ACMEEmail is the email address used for ACME certificate registration
	// across all sites. Optional — Caddy can use a staging directory without it.
	ACMEEmail string
}

// validLogLevels is the set of recognised log levels.
var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// DefaultGlobalConfig returns a GlobalConfig with sensible defaults applied.
func DefaultGlobalConfig() GlobalConfig {
	return GlobalConfig{
		ListenHost: "0.0.0.0",
		ListenPort: 443,
		LogLevel:   "info",
	}
}

// Validate checks that the GlobalConfig is internally consistent.
// It returns an error describing the first invalid field found.
func (g GlobalConfig) Validate() error {
	if g.ListenHost != "" {
		if ip := net.ParseIP(g.ListenHost); ip == nil {
			return fmt.Errorf("listen_host %q is not a valid IP address", g.ListenHost)
		}
	}

	if g.ListenPort < 0 || g.ListenPort > 65535 {
		return fmt.Errorf("listen_port %d is out of range (0-65535)", g.ListenPort)
	}

	if g.LogLevel != "" && !validLogLevels[g.LogLevel] {
		return fmt.Errorf("log_level %q is not valid (use debug, info, warn, or error)", g.LogLevel)
	}

	if g.ACMEEmail != "" {
		// Lightweight check: must contain an '@'.
		if !containsAt(g.ACMEEmail) {
			return errors.New("acme_email must be a valid email address")
		}
	}

	return nil
}

// containsAt is a minimal email sanity check.
func containsAt(s string) bool {
	for _, c := range s {
		if c == '@' {
			return true
		}
	}
	return false
}
