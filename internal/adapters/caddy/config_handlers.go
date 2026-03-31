package caddy

import (
	"fmt"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// defaultCompressionAlgorithms is the fallback list used when
// CompressionConfig.Algorithms is empty.
var defaultCompressionAlgorithms = []string{"zstd", "gzip"}

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

// buildCompressionHandlerJSON creates a Caddy encode handler that compresses
// response bodies using the algorithms listed in cfg.Algorithms.
//
// Caddy's encode handler (module: http.handlers.encode) negotiates the best
// encoding with the client via the Accept-Encoding request header. Algorithms
// are applied in the order they appear in the encodings map; zstd is preferred
// over gzip when both are offered by the client.
//
// The handler is placed in the middleware chain after all request-phase
// middleware (auth, rate limit, etc.) and before the reverse proxy so that
// Caddy can compress the upstream response before sending it to the client.
func buildCompressionHandlerJSON(cfg ports.CompressionConfig) map[string]any {
	algos := cfg.Algorithms
	if len(algos) == 0 {
		algos = defaultCompressionAlgorithms
	}

	encodings := map[string]any{}
	for _, algo := range algos {
		switch algo {
		case "gzip":
			encodings["gzip"] = map[string]any{}
		case "zstd":
			encodings["zstd"] = map[string]any{}
		}
	}

	return map[string]any{
		"handler":   "encode",
		"encodings": encodings,
	}
}
