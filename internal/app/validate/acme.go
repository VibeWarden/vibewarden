package validate

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// acmeProviders is the set of tls.provider values that use ACME (Let's Encrypt
// and compatible CAs). These providers cannot issue certificates for IP
// addresses, localhost, RFC 1918 addresses, or reserved TLDs.
var acmeProviders = map[string]bool{
	"letsencrypt":         true,
	"zerossl":             true,
	"buypass":             true,
	"letsencrypt-staging": true,
}

// reservedTLDs is the set of TLD suffixes that ACME CAs will never issue for.
// Includes RFC 6761 reserved names and the .local mDNS TLD.
var reservedTLDs = map[string]bool{
	".local":     true,
	".localhost": true,
	".test":      true,
	".invalid":   true,
	".example":   true,
}

// CheckACME iterates the configured TLS domains and reports any that are
// incompatible with ACME certificate issuance. Only fires when tls.provider is
// one of the ACME providers (letsencrypt, zerossl, buypass, letsencrypt-staging).
//
// Per spec: checks cfg.TLS.Domain (singular). The config struct does not have a
// Domains slice in the current schema, so only the singular field is checked.
//
// Skip conditions:
//   - tls.provider is not an ACME provider.
//   - tls.domain is empty.
//   - The domain is ACME-compatible.
func CheckACME(_ context.Context, _ string, cfg *config.Config, _ bool) Result {
	if !acmeProviders[cfg.TLS.Provider] {
		return Result{Skip: true}
	}
	if cfg.TLS.Domain == "" {
		return Result{Skip: true}
	}

	incompatible, reason := isACMEIncompatible(cfg.TLS.Domain)
	if !incompatible {
		return Result{Skip: true}
	}

	return Result{
		State: ops.StatusFAIL,
		Message: fmt.Sprintf(
			"tls.domain is %q which Let's Encrypt cannot issue for (%s) — use tls.provider: self-signed for local dev or tls.provider: external to manage TLS yourself",
			cfg.TLS.Domain,
			reason,
		),
	}
}

// isACMEIncompatible reports whether the given domain cannot receive an ACME
// certificate. It returns (true, reason) for incompatible domains, and
// (false, "") for compatible ones.
//
// Rules applied in order:
//  1. Empty input → compatible (caller already guards this).
//  2. domain == "localhost" → incompatible.
//  3. net.ParseIP succeeds (any IP literal, including RFC 1918 and ::1) → incompatible.
//  4. Suffix matches a reserved TLD (.local, .localhost, .test, .invalid, .example) → incompatible.
//  5. Otherwise → compatible.
func isACMEIncompatible(domain string) (bool, string) {
	if domain == "" {
		return false, ""
	}

	// Normalise: lowercase, strip a single trailing dot.
	d := strings.ToLower(strings.TrimSuffix(domain, "."))

	if d == "localhost" {
		return true, "localhost"
	}

	if ip := net.ParseIP(d); ip != nil {
		return true, "IP literal"
	}

	// Check reserved TLDs: look at the suffix after the last dot.
	if idx := strings.LastIndex(d, "."); idx >= 0 {
		tld := d[idx:] // includes the leading dot
		if reservedTLDs[tld] {
			return true, fmt.Sprintf("reserved TLD %s", tld)
		}
	}

	return false, ""
}
