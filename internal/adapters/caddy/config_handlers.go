package caddy

import (
	"fmt"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// buildUserHeaderStripHandler creates a Caddy headers handler that deletes the
// X-User-Id, X-User-Email, and X-User-Verified request headers.
//
// This handler must be placed as the very first handler in every route's chain.
// Removing these headers on every inbound request prevents a client from
// impersonating an authenticated user by injecting them directly. VibeWarden
// re-injects them only after a valid session has been verified.
func buildUserHeaderStripHandler() map[string]any {
	return map[string]any{
		"handler": "headers",
		"request": map[string]any{
			"delete": []string{"X-User-Id", "X-User-Email", "X-User-Verified"},
		},
	}
}

// buildSecurityHeadersHandler creates the Caddy headers handler for security headers.
// tlsEnabled must be true for the HSTS header to be included; HSTS must not be sent
// over plain HTTP connections.
func buildSecurityHeadersHandler(cfg ports.SecurityHeadersConfig, tlsEnabled bool) map[string]any {
	headers := map[string][]string{}

	// HSTS — only over HTTPS.
	if cfg.HSTSMaxAge > 0 && tlsEnabled {
		hsts := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAge)
		if cfg.HSTSIncludeSubDomains {
			hsts += "; includeSubDomains"
		}
		if cfg.HSTSPreload {
			hsts += "; preload"
		}
		headers["Strict-Transport-Security"] = []string{hsts}
	}

	// X-Content-Type-Options.
	if cfg.ContentTypeNosniff {
		headers["X-Content-Type-Options"] = []string{"nosniff"}
	}

	// X-Frame-Options.
	if cfg.FrameOption != "" {
		headers["X-Frame-Options"] = []string{cfg.FrameOption}
	}

	// Content-Security-Policy.
	if cfg.ContentSecurityPolicy != "" {
		headers["Content-Security-Policy"] = []string{cfg.ContentSecurityPolicy}
	}

	// Referrer-Policy.
	if cfg.ReferrerPolicy != "" {
		headers["Referrer-Policy"] = []string{cfg.ReferrerPolicy}
	}

	// Permissions-Policy.
	if cfg.PermissionsPolicy != "" {
		headers["Permissions-Policy"] = []string{cfg.PermissionsPolicy}
	}

	// Cross-Origin-Opener-Policy.
	if cfg.CrossOriginOpenerPolicy != "" {
		headers["Cross-Origin-Opener-Policy"] = []string{cfg.CrossOriginOpenerPolicy}
	}

	// Cross-Origin-Resource-Policy.
	if cfg.CrossOriginResourcePolicy != "" {
		headers["Cross-Origin-Resource-Policy"] = []string{cfg.CrossOriginResourcePolicy}
	}

	// X-Permitted-Cross-Domain-Policies.
	if cfg.PermittedCrossDomainPolicies != "" {
		headers["X-Permitted-Cross-Domain-Policies"] = []string{cfg.PermittedCrossDomainPolicies}
	}

	response := map[string]any{
		"set": headers,
	}

	// Suppress the Via header added by Caddy's reverse proxy to reduce
	// information disclosure about the proxy infrastructure.
	if cfg.SuppressViaHeader {
		response["delete"] = []string{"Via"}
	}

	return map[string]any{
		"handler":  "headers",
		"response": response,
	}
}
