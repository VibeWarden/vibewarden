package middleware

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// ErrorPageResolver looks up a custom error page file for a given HTTP status
// code and serves it, falling back to the default JSON error response when no
// file is found.
//
// File naming convention: <status_code>.<ext> inside the configured directory,
// e.g. "401.html", "403.json", "429.html". Content-Type is inferred from the
// file extension:
//   - .html → text/html; charset=utf-8
//   - .json → application/json
//
// All other extensions are served as application/octet-stream. The resolver
// probes .html first, then .json when both could theoretically exist.
type ErrorPageResolver struct {
	// directory is the path to the custom error pages directory.
	directory string
}

// NewErrorPageResolver creates an ErrorPageResolver that serves files from the
// given directory. The directory value must be a non-empty path when the caller
// intends to use custom pages; use NopErrorPageResolver when the feature is
// disabled.
func NewErrorPageResolver(directory string) *ErrorPageResolver {
	return &ErrorPageResolver{directory: directory}
}

// NopErrorPageResolver returns an ErrorPageResolver with an empty directory.
// Its WriteResponse method always falls back to the default JSON response.
func NopErrorPageResolver() *ErrorPageResolver {
	return &ErrorPageResolver{}
}

// WriteResponse attempts to serve a custom error page for status. When a
// matching file is found in the directory, it is written to w with the
// appropriate Content-Type and the given status code. When no matching file
// exists, or the resolver is a no-op (empty directory), WriteErrorResponse is
// called instead.
//
// retryAfterSeconds is only used for the 429 fallback path (passed to
// WriteRateLimitResponse). For all other status codes the caller should pass 0.
func (r *ErrorPageResolver) WriteResponse(w http.ResponseWriter, req *http.Request, status int, errorCode, message string, retryAfterSeconds int) {
	if r.directory != "" {
		if served := r.tryServeFile(w, status); served {
			return
		}
	}

	// Fallback to built-in JSON responses.
	if status == http.StatusTooManyRequests {
		WriteRateLimitResponse(w, req, retryAfterSeconds)
		return
	}
	WriteErrorResponse(w, req, status, errorCode, message)
}

// tryServeFile probes for <directory>/<status>.html and <directory>/<status>.json
// (in that order). Returns true and writes the response when a readable file is
// found. Returns false without touching w when neither file exists.
func (r *ErrorPageResolver) tryServeFile(w http.ResponseWriter, status int) bool {
	code := strconv.Itoa(status)

	candidates := []struct {
		ext         string
		contentType string
	}{
		{".html", "text/html; charset=utf-8"},
		{".json", "application/json"},
	}

	for _, c := range candidates {
		path := filepath.Join(r.directory, code+c.ext)
		data, err := os.ReadFile(path) //nolint:gosec // path is built from trusted config + integer status code
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			// File exists but is unreadable — log nothing here; fall back silently.
			continue
		}
		w.Header().Set("Content-Type", c.contentType)
		w.WriteHeader(status)
		_, _ = w.Write(data)
		return true
	}

	return false
}

// contentTypeForExt returns the MIME type for a given file extension.
// Supported: ".html" and ".json". All other extensions return
// "application/octet-stream".
func contentTypeForExt(ext string) string {
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// ServeCustomErrorPage attempts to serve a custom error page from dir for the
// given HTTP status code. It probes for files named <status>.<ext> (trying
// .html then .json). Returns true when a file was served, false otherwise.
//
// This function is a lower-level alternative to ErrorPageResolver and is
// exported for use in adapters that manage their own ResponseWriter lifecycle.
func ServeCustomErrorPage(w http.ResponseWriter, dir string, status int) bool {
	if dir == "" {
		return false
	}
	r := NewErrorPageResolver(dir)
	return r.tryServeFile(w, status)
}

// validateErrorPagesDirectory checks that dir is a readable directory.
// Returns nil when dir is empty (feature disabled). Returns a wrapped error
// when the path does not exist or is not a directory.
func validateErrorPagesDirectory(dir string) error {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("error_pages.directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("error_pages.directory %q is not a directory", dir)
	}
	return nil
}
