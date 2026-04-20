package caddy

import "github.com/vibewarden/vibewarden/internal/ports"

// NOTE: The ACME issuer helpers and constants in this file are intentionally
// duplicated in internal/plugins/tls/plugin.go. The duplication exists because
// adapters (caddy/) and plugins (plugins/tls/) cannot import each other without
// breaking the hexagonal architecture boundary. If you change ACME URLs or
// issuer logic here, update the plugin copy as well.

// ACME directory URLs for supported certificate authorities.
const (
	// acmeCALetsEncrypt is the Let's Encrypt production ACME directory.
	acmeCALetsEncrypt = "https://acme-v02.api.letsencrypt.org/directory"

	// acmeCALetsEncryptStaging is the Let's Encrypt staging ACME directory.
	acmeCALetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"

	// acmeCAZeroSSL is the ZeroSSL production ACME directory.
	acmeCAZeroSSL = "https://acme.zerossl.com/v2/DV90"

	// acmeCABuypass is the Buypass Go SSL production ACME directory.
	acmeCABuypass = "https://api.buypass.com/acme/directory"
)

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
// for the given TLS config. The issuer chain provides automatic failover: if
// the primary CA is unreachable, Caddy attempts the next issuer in order.
//
// Behaviour:
//   - provider "letsencrypt" without acme_ca: 3-issuer chain LE -> ZeroSSL -> Buypass
//   - provider "letsencrypt" with acme_ca: single issuer with the custom CA URL (backward compat)
//   - provider "zerossl": single issuer targeting ZeroSSL
//   - provider "buypass": single issuer targeting Buypass
//   - provider "letsencrypt-staging": single issuer targeting LE staging
//
// When email is non-empty it is included in every issuer for ACME account
// registration and expiry notifications.
func buildACMEIssuers(cfg ports.TLSConfig) []map[string]any {
	email := cfg.Email

	switch cfg.Provider {
	case ports.TLSProviderLetsEncrypt:
		if cfg.ACMECA != "" {
			// Custom CA override disables the fallback chain (backward compat).
			return []map[string]any{buildSingleACMEIssuer(cfg.ACMECA, email)}
		}
		// Default: 3-issuer fallback chain.
		return []map[string]any{
			buildSingleACMEIssuer(acmeCALetsEncrypt, email),
			buildSingleACMEIssuer(acmeCAZeroSSL, email),
			buildSingleACMEIssuer(acmeCABuypass, email),
		}

	case ports.TLSProviderZeroSSL:
		return []map[string]any{buildSingleACMEIssuer(acmeCAZeroSSL, email)}

	case ports.TLSProviderBuypass:
		return []map[string]any{buildSingleACMEIssuer(acmeCABuypass, email)}

	case ports.TLSProviderLetsEncryptStaging:
		return []map[string]any{buildSingleACMEIssuer(acmeCALetsEncryptStaging, email)}

	default:
		// Fallback for unknown ACME providers — should not happen after validation.
		return []map[string]any{buildSingleACMEIssuer(acmeCALetsEncrypt, email)}
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
