package http

import (
	"io/fs"
	"net/http"
	"strings"
)

const (
	// adminUIPrefix is the URL prefix at which the admin UI is mounted.
	// The Caddy /_vibewarden/admin/* reverse-proxy route forwards this path to
	// the internal admin server automatically — no new Caddy route is required.
	adminUIPrefix = "/_vibewarden/admin/ui"

	// adminUIPrefixSlash is the canonical UI prefix with a trailing slash,
	// used as the serve root in the admin server mux.
	adminUIPrefixSlash = "/_vibewarden/admin/ui/"
)

// AdminUIHandler serves the embedded user-management admin UI static assets
// at /_vibewarden/admin/ui/.
//
// Routing behaviour:
//   - GET /_vibewarden/admin/ui  → 301 redirect to /_vibewarden/admin/ui/
//   - GET /_vibewarden/admin/ui/ → index.html, 200, text/html; charset=utf-8,
//     Cache-Control: no-store
//   - GET /_vibewarden/admin/ui/<file> → asset from the embedded FS with the
//     correct MIME type derived from the file extension.
//   - Any sub-path not found in the embedded FS → 404.
//   - Directory listing is disabled: requests for paths that resolve to a
//     directory (other than the index) return 404.
type AdminUIHandler struct {
	// assets is the filesystem rooted at the assets directory.
	assets fs.FS
	// fileServer serves assets under the /ui/ prefix via StripPrefix.
	// It is used for non-index asset requests (app.js, styles.css, etc.).
	fileServer http.Handler
}

// NewAdminUIHandler constructs an AdminUIHandler backed by the provided
// assets filesystem. assets must be rooted at the directory containing
// index.html, app.js, and styles.css (i.e. the result of ui.Assets()).
func NewAdminUIHandler(assets fs.FS) *AdminUIHandler {
	// Wrap the stdlib file server with StripPrefix so that the handler
	// receives bare names like "app.js" rather than full paths.
	stripped := http.StripPrefix(adminUIPrefixSlash, http.FileServerFS(assets))
	return &AdminUIHandler{assets: assets, fileServer: stripped}
}

// ServeHTTP handles all requests under /_vibewarden/admin/ui.
func (h *AdminUIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Redirect bare prefix → prefix-with-slash so relative asset paths resolve.
	if path == adminUIPrefix {
		http.Redirect(w, r, adminUIPrefixSlash, http.StatusMovedPermanently)
		return
	}

	// Reject sub-paths ending with "/" other than the index root.
	// This disables directory listing for any sub-directory.
	if path != adminUIPrefixSlash && strings.HasSuffix(path, "/") {
		http.NotFound(w, r)
		return
	}

	// At the index: serve index.html directly from the FS with no-store.
	// We read from the FS ourselves to avoid http.FileServer redirecting
	// "index.html" back to "./" (its standard canonicalisation behaviour).
	if path == adminUIPrefixSlash {
		h.serveIndex(w, r)
		return
	}

	h.fileServer.ServeHTTP(w, r)
}

// serveIndex reads index.html from the embedded FS and writes it with
// Content-Type: text/html; charset=utf-8 and Cache-Control: no-store.
// It bypasses http.FileServer to prevent the automatic "index.html → ./"
// redirect that the stdlib file server applies to index files.
func (h *AdminUIHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(h.assets, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// data is the trusted index.html embedded into the binary at build time
	// (internal/plugins/usermgmt/ui) — never user input — so the gosec G705
	// XSS-taint finding is a false positive.
	_, _ = w.Write(data) //nolint:errcheck,gosec // trusted embedded asset, not user input
}

// RegisterAdminUIRoutes registers the AdminUIHandler on mux for both the
// bare prefix (for the 301 redirect) and the slash-terminated prefix (for
// assets and the index).
func RegisterAdminUIRoutes(mux *http.ServeMux, h *AdminUIHandler) {
	// Exact-path match for the bare prefix triggers the 301 redirect.
	mux.Handle(adminUIPrefix, h)
	// Prefix match for all assets under /ui/.
	mux.Handle(adminUIPrefixSlash, h)
}
