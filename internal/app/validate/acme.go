package validate

import (
	"context"
	"fmt"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/app/tlsdomain"
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
// Per spec: checks inputs.Cfg.TLS.Domain (singular). The config struct does not
// have a Domains slice in the current schema, so only the singular field is
// checked.
//
// Skip conditions:
//   - tls.provider is not an ACME provider.
//   - tls.domain is empty.
//   - The domain is ACME-compatible.
//
// Source attribution: the FAIL row is attributed to "vibewarden.production.yaml"
// only when a production override exists AND the merged tls.domain differs from
// the base config's tls.domain — meaning the override introduced the
// incompatible domain.
func CheckACME(_ context.Context, inputs CheckInputs) Result {
	if !acmeProviders[inputs.Cfg.TLS.Provider] {
		return Result{Skip: true}
	}
	if inputs.Cfg.TLS.Domain == "" {
		return Result{Skip: true}
	}

	incompatible, reason := tlsdomain.IsACMEIncompatible(inputs.Cfg.TLS.Domain)
	if !incompatible {
		return Result{Skip: true}
	}

	// Attribute to production override when it is what introduced the
	// incompatible domain.
	source := ""
	if inputs.ProdOverrideExists && inputs.BaseCfg != nil && inputs.Cfg.TLS.Domain != inputs.BaseCfg.TLS.Domain {
		source = "vibewarden.production.yaml"
	}

	return Result{
		State:  ops.StatusFAIL,
		Source: source,
		Message: fmt.Sprintf(
			"tls.domain is %q which Let's Encrypt cannot issue for (%s) — use tls.provider: self-signed for local dev or tls.provider: external to manage TLS yourself",
			inputs.Cfg.TLS.Domain,
			reason,
		),
	}
}
