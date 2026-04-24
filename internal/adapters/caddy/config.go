// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
	"fmt"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// BuildCaddyConfig constructs the Caddy JSON configuration from ProxyConfig.
// Uses Caddy's native JSON config format (not Caddyfile).
//
// TLS behaviour is driven entirely by cfg.TLS.Provider:
//   - "letsencrypt" — automatic ACME certificate via Let's Encrypt
//   - "self-signed"  — Caddy generates an internal self-signed certificate
//   - "external"     — operator-supplied certificate and key files
//
// When TLS is enabled with self-signed or external provider, an HTTP-to-HTTPS
// redirect server is added automatically. For the letsencrypt provider, no
// manual redirect server is created — Caddy's built-in automatic HTTPS handles
// ACME HTTP-01 challenges and HTTP→HTTPS redirects on port 80 natively.
//
// When auth is active (cfg.Auth.Enabled && cfg.Auth.KratosPublicURL != ""),
// Kratos self-service flow routes are inserted between the health check route
// and the catch-all proxy route. Requests to those paths are forwarded to the
// Kratos public API transparently so browsers can complete self-service flows.
// cfg.Auth.Enabled is the ports-layer DTO flag populated from
// config.AuthConfig.Active() at the app/serve boundary — see ADR-065.
func BuildCaddyConfig(cfg *ports.ProxyConfig) (map[string]any, error) {
	if err := validateBuildInput(cfg); err != nil {
		return nil, err
	}

	handlers, err := buildCatchAllHandlers(cfg)
	if err != nil {
		return nil, err
	}

	routes := buildRoutes(cfg, handlers)
	server := buildMainServer(cfg, routes)

	apps, err := buildCaddyApps(cfg, server)
	if err != nil {
		return nil, err
	}

	return assembleTopLevelConfig(cfg, apps)
}

// buildExtraRoute converts a ports.CaddyRoute into the raw Caddy route map
// expected by the Caddy JSON configuration. CaddyRoute.Handler already contains
// the full route object (with "match" and "handle" keys) so it is used directly.
func buildExtraRoute(r ports.CaddyRoute) map[string]any {
	return r.Handler
}

// validateTLSConfig checks that the TLS configuration is self-consistent.
func validateTLSConfig(cfg ports.TLSConfig) error {
	switch cfg.Provider {
	case ports.TLSProviderLetsEncrypt,
		ports.TLSProviderBuypass,
		ports.TLSProviderLetsEncryptStaging:
		if cfg.Domain == "" {
			return fmt.Errorf("domain is required for provider %q", cfg.Provider)
		}
	case ports.TLSProviderZeroSSL:
		if cfg.Domain == "" {
			return fmt.Errorf("domain is required for provider %q", cfg.Provider)
		}
		if cfg.Email == "" {
			return fmt.Errorf("email is required for provider %q (ZeroSSL needs it for automatic EAB registration)", cfg.Provider)
		}
	case ports.TLSProviderExternal:
		if cfg.CertPath == "" {
			return fmt.Errorf("cert_path is required for provider %q", cfg.Provider)
		}
		if cfg.KeyPath == "" {
			return fmt.Errorf("key_path is required for provider %q", cfg.Provider)
		}
	case ports.TLSProviderSelfSigned, "":
		// No additional fields required.
	default:
		return fmt.Errorf("unknown tls provider %q; valid values: letsencrypt, zerossl, buypass, letsencrypt-staging, self-signed, external", cfg.Provider)
	}
	return nil
}

// buildTLSPolicy creates TLS connection policies for Caddy.
// For the external provider the policy references the operator-supplied certificate by tag.
// For all other providers an empty default policy lets Caddy select the certificate.
func buildTLSPolicy(cfg ports.TLSConfig) []map[string]any {
	if cfg.Provider == ports.TLSProviderExternal {
		return []map[string]any{
			{
				"certificate_selection": map[string]any{
					"any_tag": []string{"vibewarden_external"},
				},
			},
		}
	}

	// For self-signed, set a default SNI so Caddy can match the certificate
	// even when clients connect by IP (no SNI in the TLS handshake).
	if cfg.Provider == ports.TLSProviderSelfSigned {
		domain := cfg.Domain
		if domain == "" {
			domain = "localhost"
		}
		return []map[string]any{{"default_sni": domain}}
	}

	// For ACME providers, include a match.sni entry for the domain.
	// Without this, Caddy does not associate the TLS connection policy with the
	// domain and will not trigger proactive ACME certificate issuance.
	if isACMEProvider(cfg.Provider) && cfg.Domain != "" {
		return []map[string]any{{
			"match": map[string]any{
				"sni": []string{cfg.Domain},
			},
		}}
	}

	// Default policy — Caddy selects the certificate automatically.
	return []map[string]any{{}}
}

// buildTLSApp builds the Caddy "tls" app configuration for the chosen provider.
// Returns nil when no TLS app section is needed.
func buildTLSApp(cfg ports.TLSConfig) (map[string]any, error) {
	if isACMEProvider(cfg.Provider) {
		return buildACMETLSApp(cfg), nil
	}
	switch cfg.Provider {
	case ports.TLSProviderSelfSigned, "":
		return buildSelfSignedTLSApp(cfg), nil
	case ports.TLSProviderExternal:
		return buildExternalTLSApp(cfg), nil
	default:
		// Already validated in validateTLSConfig; this is a defensive fallback.
		return nil, fmt.Errorf("unknown tls provider: %q", cfg.Provider)
	}
}

// buildACMETLSApp returns a Caddy TLS app configuration that provisions
// certificates automatically via ACME. The issuer chain is determined by
// buildACMEIssuers: for `provider: letsencrypt` without an acme_ca override
// a 3-issuer fallback chain (LE -> ZeroSSL -> Buypass) is configured; for
// specific providers (zerossl, buypass, letsencrypt-staging) a single issuer
// targeting that CA is used.
//
// Note: storage is intentionally NOT set here. Caddy's storage backend is a
// top-level field on the Config struct; placing it inside apps.tls causes Caddy
// to reject the config with "unknown field: storage". Storage is set at the
// top-level in BuildCaddyConfig when cfg.StoragePath is non-empty.
func buildACMETLSApp(cfg ports.TLSConfig) map[string]any {
	// The caddy adapter's buildACMETLSApp is only invoked for legacy
	// single-site code paths; the TLS plugin Init owns observability for the
	// default chain and therefore emits the skipped-issuer events.
	issuers, _ := buildACMEIssuers(cfg)
	policy := map[string]any{
		"subjects": []string{cfg.Domain},
		"issuers":  issuers,
	}

	return map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}
}

// buildSelfSignedTLSApp returns a Caddy TLS app configuration that instructs
// Caddy to generate an internal self-signed certificate.
// This is intended for local development and testing only.
//
// Note: storage is intentionally NOT set here. See buildACMETLSApp for
// the explanation of why storage belongs at the top-level Caddy config.
func buildSelfSignedTLSApp(cfg ports.TLSConfig) map[string]any {
	policy := map[string]any{
		"issuers": []map[string]any{
			{"module": "internal"},
		},
	}

	// Scope the policy to the domain. For self-signed certs, Caddy's internal
	// issuer needs at least one subject to generate a certificate. Default to
	// "localhost" when no domain is configured (typical for local development).
	domain := cfg.Domain
	if domain == "" {
		domain = "localhost"
	}
	policy["subjects"] = []string{domain}

	return map[string]any{
		"automation": map[string]any{
			"policies": []map[string]any{policy},
		},
	}
}

// buildExternalTLSApp returns a Caddy TLS app configuration that loads
// certificates from the file paths supplied by the operator.
func buildExternalTLSApp(cfg ports.TLSConfig) map[string]any {
	return map[string]any{
		"certificates": map[string]any{
			"load_files": []map[string]any{
				{
					"certificate": cfg.CertPath,
					"key":         cfg.KeyPath,
					"tags":        []string{"vibewarden_external"},
				},
			},
		},
	}
}

// buildHTTPRedirectServer returns a Caddy server configuration that permanently
// (HTTP 301) redirects all plain HTTP requests to HTTPS.
func buildHTTPRedirectServer() map[string]any {
	return map[string]any{
		"listen": []string{":80"},
		"routes": []map[string]any{
			{
				"handle": []map[string]any{
					{
						"handler": "static_response",
						"headers": map[string][]string{
							"Location": {"https://{http.request.host}{http.request.uri}"},
						},
						"status_code": 301,
					},
				},
			},
		},
		"automatic_https": map[string]any{"disable": true},
	}
}
