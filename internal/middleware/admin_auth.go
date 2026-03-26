package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// adminPathPrefix is the URL prefix that requires admin authentication.
	adminPathPrefix = "/_vibewarden/admin/"

	// adminKeyHeader is the request header carrying the bearer token.
	adminKeyHeader = "X-Admin-Key"
)

// AdminAuthMiddleware returns HTTP middleware that protects all
// /_vibewarden/admin/* endpoints with a static bearer token.
//
// Request handling rules:
//   - Requests that do not start with /_vibewarden/admin/ pass through
//     unchanged to the next handler.
//   - When cfg.Enabled is false, all /_vibewarden/admin/* requests receive
//     404 Not Found so the admin surface is not disclosed.
//   - When cfg.Enabled is true but cfg.Token is empty, all admin requests
//     receive 500 Internal Server Error to surface the misconfiguration.
//   - When the X-Admin-Key header is absent or does not match cfg.Token the
//     middleware responds with 401 Unauthorized and a WWW-Authenticate hint.
//   - When the X-Admin-Key header matches cfg.Token the request is forwarded
//     to the next handler.
//
// The comparison is constant-time to prevent timing attacks.
func AdminAuthMiddleware(cfg ports.AdminAuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only apply to the admin path prefix.
			if !strings.HasPrefix(r.URL.Path, adminPathPrefix) {
				next.ServeHTTP(w, r)
				return
			}

			// Admin API is disabled — return 404 to avoid disclosing existence.
			if !cfg.Enabled {
				http.NotFound(w, r)
				return
			}

			// Misconfiguration: admin enabled but no token set.
			if cfg.Token == "" {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			// Validate the X-Admin-Key header.
			provided := r.Header.Get(adminKeyHeader)
			if !secureEqual(provided, cfg.Token) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="vibewarden-admin"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// secureEqual compares two strings in constant time to prevent timing attacks.
// It delegates to crypto/subtle.ConstantTimeCompare, which avoids leaking
// length information through early returns.
// It returns true only when both strings are identical.
func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
