package caddy

import "github.com/vibewarden/vibewarden/internal/ports"

// ACME directory URLs for supported certificate authorities.
const (
	// acmeURLLetsEncrypt is the production ACME directory for Let's Encrypt.
	acmeURLLetsEncrypt = "https://acme-v02.api.letsencrypt.org/directory"

	// acmeURLLetsEncryptStaging is the staging ACME directory for Let's Encrypt.
	// Certificates issued are not publicly trusted.
	acmeURLLetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"

	// acmeURLZeroSSL is the ACME directory for ZeroSSL.
	acmeURLZeroSSL = "https://acme.zerossl.com/v2/DV90"

	// acmeURLBuypass is the ACME directory for Buypass Go SSL.
	acmeURLBuypass = "https://api.buypass.com/acme/directory"
)

// buildACMEIssuers constructs the list of ACME issuer configurations for the
// given TLS config. The returned slice is used in Caddy's TLS automation policy
// "issuers" field.
//
// Behaviour:
//   - provider "letsencrypt" without acme_ca: 3-issuer fallback chain
//     (Let's Encrypt -> ZeroSSL -> Buypass) for maximum availability.
//   - provider "letsencrypt" with acme_ca: single issuer with the specified CA
//     (backward-compatible override).
//   - provider "zerossl": single ZeroSSL issuer (requires email for EAB).
//   - provider "buypass": single Buypass issuer.
//   - provider "letsencrypt-staging": single Let's Encrypt staging issuer.
func buildACMEIssuers(cfg ports.TLSConfig) []map[string]any {
	switch cfg.Provider {
	case ports.TLSProviderLetsEncrypt:
		if cfg.ACMECA != "" {
			// Explicit CA override — single issuer, backward compat.
			return []map[string]any{buildSingleACMEIssuer(cfg.ACMECA, cfg.Email)}
		}
		// Default: 3-issuer fallback chain for maximum availability.
		return []map[string]any{
			buildSingleACMEIssuer(acmeURLLetsEncrypt, cfg.Email),
			buildSingleACMEIssuer(acmeURLZeroSSL, cfg.Email),
			buildSingleACMEIssuer(acmeURLBuypass, cfg.Email),
		}

	case ports.TLSProviderZeroSSL:
		return []map[string]any{buildSingleACMEIssuer(acmeURLZeroSSL, cfg.Email)}

	case ports.TLSProviderBuypass:
		return []map[string]any{buildSingleACMEIssuer(acmeURLBuypass, cfg.Email)}

	case ports.TLSProviderLetsEncryptStaging:
		return []map[string]any{buildSingleACMEIssuer(acmeURLLetsEncryptStaging, cfg.Email)}

	default:
		// Non-ACME providers should not call this function.
		// Return a single default LE issuer as a defensive fallback.
		return []map[string]any{buildSingleACMEIssuer(acmeURLLetsEncrypt, cfg.Email)}
	}
}

// buildSingleACMEIssuer constructs a single ACME issuer map for Caddy's JSON
// config. The "ca" field is always set explicitly so each issuer in a fallback
// chain targets a specific directory. The "email" field is included when
// non-empty (required for ZeroSSL EAB, recommended for others).
func buildSingleACMEIssuer(caURL, email string) map[string]any {
	issuer := map[string]any{
		"module": "acme",
		"ca":     caURL,
	}
	if email != "" {
		issuer["email"] = email
	}
	return issuer
}

// isACMEProvider returns true if the given TLS provider uses the ACME protocol
// for certificate provisioning (i.e., requires a domain and supports issuers).
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
