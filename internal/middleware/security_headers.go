package middleware

import (
	"fmt"
	"net/http"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// SecurityHeaders creates a middleware that adds security headers to responses.
// Headers are set before calling the next handler so they appear on all responses.
// HSTS is only applied when the connection is over TLS (r.TLS != nil).
func SecurityHeaders(cfg ports.SecurityHeadersConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setSecurityHeaders(w, r, cfg)
			next.ServeHTTP(w, r)
		})
	}
}

// setSecurityHeaders applies all configured security headers to the response writer.
// HSTS is only set when the request was received over TLS (r.TLS != nil).
func setSecurityHeaders(w http.ResponseWriter, r *http.Request, cfg ports.SecurityHeadersConfig) {
	// Strict-Transport-Security (HSTS) — must not be sent over plain HTTP
	if cfg.HSTSMaxAge > 0 && r.TLS != nil {
		hsts := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAge)
		if cfg.HSTSIncludeSubDomains {
			hsts += "; includeSubDomains"
		}
		if cfg.HSTSPreload {
			hsts += "; preload"
		}
		w.Header().Set("Strict-Transport-Security", hsts)
	}

	// X-Content-Type-Options
	if cfg.ContentTypeNosniff {
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}

	// X-Frame-Options
	if cfg.FrameOption != "" {
		w.Header().Set("X-Frame-Options", cfg.FrameOption)
	}

	// Content-Security-Policy
	if cfg.ContentSecurityPolicy != "" {
		w.Header().Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
	}

	// Referrer-Policy
	if cfg.ReferrerPolicy != "" {
		w.Header().Set("Referrer-Policy", cfg.ReferrerPolicy)
	}

	// Permissions-Policy
	if cfg.PermissionsPolicy != "" {
		w.Header().Set("Permissions-Policy", cfg.PermissionsPolicy)
	}
}

// DefaultSecurityHeadersConfig returns sensible default security header settings.
func DefaultSecurityHeadersConfig() ports.SecurityHeadersConfig {
	return ports.SecurityHeadersConfig{
		Enabled:               true,
		HSTSMaxAge:            31536000, // 1 year
		HSTSIncludeSubDomains: true,
		HSTSPreload:           false, // Preload requires manual submission to browser lists
		ContentTypeNosniff:    true,
		FrameOption:           "DENY",
		ContentSecurityPolicy: "", // disabled by default — users opt in via config
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		PermissionsPolicy:     "",
	}
}
