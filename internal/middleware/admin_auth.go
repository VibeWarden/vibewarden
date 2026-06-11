package middleware

import (
	"crypto/subtle"
	"net/http"
	"path"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// adminPathPrefix is the URL prefix that requires admin authentication.
	adminPathPrefix = "/_vibewarden/admin/"

	// adminUIPrefix is the public sub-prefix for the embedded admin UI static
	// assets. Requests under this prefix bypass token validation so that the
	// HTML/CSS/JS can load in the browser without a token. The carve-out only
	// takes effect when cfg.Enabled is true; when admin is disabled all paths
	// under adminPathPrefix still return 404.
	//
	// The prefix covers both "/_vibewarden/admin/ui" (bare, for the 301) and
	// "/_vibewarden/admin/ui/" (with trailing slash, for assets and the index).
	adminUIPrefix = "/_vibewarden/admin/ui"

	// adminKeyHeader is the request header carrying the bearer token.
	adminKeyHeader = "X-Admin-Key"
)

// AdminAuthMiddleware returns HTTP middleware that protects all
// /_vibewarden/admin/* endpoints with a static bearer token.
//
// When cfg.ConfigPath is set, that path prefix is also protected.
//
// Request handling rules:
//   - Requests that do not start with /_vibewarden/admin/ (or cfg.ConfigPath
//     when set) pass through unchanged to the next handler.
//   - When cfg.Enabled is false, all protected requests receive 404 Not Found
//     so the admin surface is not disclosed.
//   - Requests under /_vibewarden/admin/ui (the embedded admin UI) bypass token
//     validation when cfg.Enabled is true. The assets contain no secrets; the
//     token gate on data routes is the real security boundary.
//   - When cfg.Enabled is true but cfg.Token is empty, all admin requests
//     (except the UI carve-out) receive 500 Internal Server Error to surface the
//     misconfiguration.
//   - When the X-Admin-Key header is absent or does not match cfg.Token the
//     middleware responds with 401 Unauthorized and a WWW-Authenticate hint.
//   - When the X-Admin-Key header matches cfg.Token the request is forwarded
//     to the next handler.
//
// The comparison is constant-time to prevent timing attacks.
//
// The auditLogger receives security audit events (audit.auth.success,
// audit.auth.failure) for each admin authentication decision. Audit events are
// always emitted regardless of operational log level. If auditLogger is nil,
// audit logging is skipped silently.
func AdminAuthMiddleware(cfg ports.AdminAuthConfig, auditLogger ports.AuditEventLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only apply to protected path prefixes.
			isAdmin := strings.HasPrefix(r.URL.Path, adminPathPrefix)
			isConfig := cfg.ConfigPath != "" && strings.HasPrefix(r.URL.Path, cfg.ConfigPath)
			if !isAdmin && !isConfig {
				next.ServeHTTP(w, r)
				return
			}

			// Admin API is disabled — return 404 to avoid disclosing existence.
			// This check runs BEFORE the UI carve-out so that the UI surface is
			// also hidden when admin is disabled.
			if !cfg.Enabled {
				http.NotFound(w, r)
				return
			}

			// UI carve-out: static assets under /_vibewarden/admin/ui do not
			// require a token. The carve-out is inside the Enabled guard above, so
			// the UI is only accessible when admin.enabled: true.
			//
			// Match against the CLEANED path so that traversal/encoding tricks
			// (e.g. /_vibewarden/admin/ui/../users, //ui, %2e%2e) cannot use the
			// tokenless carve-out to reach a gated data route. The exact-subtree
			// check (equal to the prefix, or prefix + "/") also rejects
			// prefix-confusion like /_vibewarden/admin/uisomething.
			clean := path.Clean(r.URL.Path)
			if clean == adminUIPrefix || strings.HasPrefix(clean, adminUIPrefix+"/") {
				next.ServeHTTP(w, r)
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
				emitAuditAuthFailure(r, auditLogger, nil, "", "missing or invalid admin key")
				w.Header().Set("WWW-Authenticate", `Bearer realm="vibewarden-admin"`)
				WriteErrorResponse(w, r, http.StatusUnauthorized, "unauthorized", "missing or invalid admin key")
				return
			}

			emitAuditAuthSuccess(r, auditLogger, nil, "", "")
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
