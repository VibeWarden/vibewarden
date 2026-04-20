package caddy

import "github.com/vibewarden/vibewarden/internal/ports"

// NOTE: The ACME issuer helpers and constants in this file are intentionally
// duplicated in internal/plugins/tls/plugin.go. The duplication exists because
// adapters (caddy/) and plugins (plugins/tls/) cannot import each other without
// breaking the hexagonal architecture boundary. If you change ACME URLs or
// issuer logic here, update the plugin copy as well.
//
// PRIMARY SOURCE OF TRUTH: this file. The mirror copy in
// internal/plugins/tls/plugin.go carries a "MUST MIRROR" banner referencing
// this file. De-duplication into a shared pure-Go package is tracked as a
// follow-up per ADR-083 §4.

// ACME directory URLs for supported certificate authorities.
const (
	// acmeCALetsEncrypt is the Let's Encrypt production ACME directory.
	acmeCALetsEncrypt = "https://acme-v02.api.letsencrypt.org/directory"

	// acmeCALetsEncryptStaging is the Let's Encrypt staging ACME directory.
	acmeCALetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"

	// acmeCAZeroSSL is the ZeroSSL production ACME directory.
	acmeCAZeroSSL = "https://acme.zerossl.com/v2/DV90"

	// acmeCABuypass is the Buypass Go SSL production ACME directory.
	// As of #1055 this endpoint returns 403 Forbidden in production; Buypass is
	// therefore no longer included in the default fallback chain but remains
	// selectable as an explicit provider opt-in (with a deprecation event).
	acmeCABuypass = "https://api.buypass.com/acme/directory"
)

// Machine-readable reason codes for skipped issuers. The v1 schema freezes
// the set of allowed values; see ADR-083 §3a.
const (
	skipReasonEmailNotConfigured = "email_not_configured"
)

// SkippedIssuer records an issuer that was evaluated for the default ACME
// fallback chain but excluded (e.g. ZeroSSL skipped because tls.email is
// empty). Callers emit one tls.acme.chain_skipped event per entry.
type SkippedIssuer struct {
	// Provider is the short identifier of the skipped issuer (e.g. "zerossl").
	Provider string

	// Reason is a machine-readable skip reason (e.g. "email_not_configured").
	Reason string
}

// isACMEProvider returns true when the TLS provider uses ACME for certificate
// provisioning (as opposed to self-signed or external). This helper is used to
// determine whether the ACME issuer chain should be configured and whether a
// redirect server should be suppressed (Caddy manages HTTP-01 challenges).
func isACMEProvider(provider ports.TLSProvider) bool {
	switch provider {
	case ports.TLSProviderLetsEncrypt,
		ports.TLSProviderZeroSSL,
		ports.TLSProviderBuypass,
		ports.TLSProviderLetsEncryptStaging:
		return true
	default:
		return false
	}
}

// buildACMEIssuers constructs the ordered list of ACME issuer configurations
// for the given TLS config and reports any issuers that were evaluated but
// excluded from the default chain.
//
// Behaviour (see ADR-083):
//   - provider "letsencrypt" without acme_ca:
//     – when cfg.Email is empty: single-issuer chain [LE] (ZeroSSL skipped,
//     Buypass removed).
//     – when cfg.Email is non-empty: two-issuer chain [LE, ZeroSSL]
//     (Buypass removed from the default chain unconditionally).
//   - provider "letsencrypt" with acme_ca: single issuer with the custom CA
//     URL (backward-compatible override; no default chain, no skipped slice).
//   - provider "zerossl": single issuer targeting ZeroSSL.
//   - provider "buypass": single issuer targeting Buypass (explicit opt-in,
//     deprecation event emitted by the caller).
//   - provider "letsencrypt-staging": single issuer targeting LE staging.
//
// The second return value lists issuers that were evaluated for the default
// chain but excluded; it is nil for all explicit-provider and acme_ca-override
// paths. Callers emit one tls.acme.chain_skipped event per entry.
//
// When email is non-empty it is included in every issuer for ACME account
// registration and expiry notifications.
func buildACMEIssuers(cfg ports.TLSConfig) (issuers []map[string]any, skipped []SkippedIssuer) {
	email := cfg.Email

	switch cfg.Provider {
	case ports.TLSProviderLetsEncrypt:
		if cfg.ACMECA != "" {
			// Custom CA override disables the fallback chain (backward compat).
			return []map[string]any{buildSingleACMEIssuer(cfg.ACMECA, email)}, nil
		}
		// Default chain: LE, optionally followed by ZeroSSL when email is set.
		// Buypass is intentionally absent from the default chain per ADR-083.
		chain := []map[string]any{
			buildSingleACMEIssuer(acmeCALetsEncrypt, email),
		}
		if email == "" {
			// ZeroSSL requires an email for EAB registration; skip it with a
			// log-event signal instead of failing config validation.
			return chain, []SkippedIssuer{
				{Provider: string(ports.TLSProviderZeroSSL), Reason: skipReasonEmailNotConfigured},
			}
		}
		chain = append(chain, buildSingleACMEIssuer(acmeCAZeroSSL, email))
		return chain, nil

	case ports.TLSProviderZeroSSL:
		return []map[string]any{buildSingleACMEIssuer(acmeCAZeroSSL, email)}, nil

	case ports.TLSProviderBuypass:
		return []map[string]any{buildSingleACMEIssuer(acmeCABuypass, email)}, nil

	case ports.TLSProviderLetsEncryptStaging:
		return []map[string]any{buildSingleACMEIssuer(acmeCALetsEncryptStaging, email)}, nil

	default:
		// Fallback for unknown ACME providers — should not happen after validation.
		return []map[string]any{buildSingleACMEIssuer(acmeCALetsEncrypt, email)}, nil
	}
}

// buildSingleACMEIssuer returns an ACME issuer map targeting the given CA
// directory URL. When email is non-empty it is included for account registration.
func buildSingleACMEIssuer(ca, email string) map[string]any {
	issuer := map[string]any{
		"module": "acme",
		"ca":     ca,
	}
	if email != "" {
		issuer["email"] = email
	}
	return issuer
}
