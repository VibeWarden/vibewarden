package middleware

import (
	"fmt"
	"net/http"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// SecurityHeaders creates a middleware that adds security headers to responses.
// Headers are set before calling the next handler so they appear on all responses.
func SecurityHeaders(cfg ports.SecurityHeadersConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setSecurityHeaders(w, cfg)
			next.ServeHTTP(w, r)
		})
	}
}

// setSecurityHeaders applies all configured security headers to the response writer.
func setSecurityHeaders(w http.ResponseWriter, cfg ports.SecurityHeadersConfig) {
	// Strict-Transport-Security (HSTS)
	if cfg.HSTSMaxAge > 0 {
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
		ContentSecurityPolicy: "default-src 'self'",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		PermissionsPolicy:     "",
	}
}
