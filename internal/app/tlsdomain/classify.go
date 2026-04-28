// Package tlsdomain provides domain classification helpers for TLS provider
// selection. It is a pure, dependency-free package importable from both the
// validate and CLI cmd packages without introducing cross-package cycles.
package tlsdomain

import (
	"fmt"
	"net"
	"strings"
)

// reservedTLDs is the set of TLD suffixes that ACME CAs will never issue for.
// Includes RFC 6761 reserved names and the .local mDNS TLD.
var reservedTLDs = map[string]bool{
	".local":     true,
	".localhost": true,
	".test":      true,
	".invalid":   true,
	".example":   true,
}

// IsACMEIncompatible reports whether the given domain cannot receive an ACME
// certificate. It returns (true, reason) for incompatible domains, and
// (false, "") for compatible ones.
//
// Rules applied in order:
//  1. Empty input → compatible (caller already guards this).
//  2. domain == "localhost" → incompatible.
//  3. net.ParseIP succeeds (any IP literal, including RFC 1918 and ::1) → incompatible.
//  4. Suffix matches a reserved TLD (.local, .localhost, .test, .invalid, .example) → incompatible.
//  5. Otherwise → compatible.
func IsACMEIncompatible(domain string) (bool, string) {
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
