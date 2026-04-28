package validate

import (
	"context"
	"fmt"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/app/tlsdomain"
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

	incompatible, reason := tlsdomain.IsACMEIncompatible(cfg.TLS.Domain)
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
