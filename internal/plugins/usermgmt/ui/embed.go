// Package ui provides the embedded static assets for the user-management
// admin UI served at /_vibewarden/admin/ui/.
//
// The assets are compiled directly into the binary via go:embed so that no
// external files are required at runtime. Call Assets to obtain an fs.FS
// rooted at the assets/ sub-directory, ready for use with http.FileServerFS.
package ui

import (
	"embed"
	"fmt"
	"io/fs"
)

// UIFS is the raw embedded filesystem containing the assets/ directory tree.
// Prefer Assets() for serving, which strips the "assets/" path prefix.
//
//go:embed assets
var UIFS embed.FS

// Assets returns an fs.FS rooted at the embedded assets/ sub-directory.
// The returned FS is suitable for use with http.FileServerFS: paths relative
// to it are "index.html", "app.js", "styles.css", etc.
//
// Assets panics if the embedded filesystem is corrupt (which indicates a
// broken build, not a runtime condition).
func Assets() fs.FS {
	sub, err := fs.Sub(UIFS, "assets")
	if err != nil {
		// The assets directory is embedded at build time; a Sub error here means
		// the binary itself is broken — not recoverable at runtime.
		panic(fmt.Sprintf("usermgmt/ui: failed to sub assets from embedded FS: %v", err))
	}
	return sub
}
