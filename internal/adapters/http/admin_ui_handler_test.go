package http_test

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	vibehttp "github.com/vibewarden/vibewarden/internal/adapters/http"
)

// testAssetFS returns a minimal in-memory filesystem that mimics the shape of
// the embedded admin UI assets, suitable for use in handler tests.
func testAssetFS() fs.FS {
	return fstest.MapFS{
		"index.html": {Data: []byte(`<!doctype html><html><head><title>Test</title></head><body></body></html>`)},
		"app.js":     {Data: []byte(`// app`)},
		"styles.css": {Data: []byte(`/* css */`)},
	}
}

// newTestUIHandler returns an AdminUIHandler backed by the in-memory test FS.
func newTestUIHandler() *vibehttp.AdminUIHandler {
	return vibehttp.NewAdminUIHandler(testAssetFS())
}

// TestAdminUIHandler_RedirectBarePrefix verifies that a request to the bare
// prefix without a trailing slash receives a 301 redirect to the slash form.
func TestAdminUIHandler_RedirectBarePrefix(t *testing.T) {
	h := newTestUIHandler()
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/ui", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want %d (301 redirect)", w.Code, http.StatusMovedPermanently)
	}
	location := w.Header().Get("Location")
	if location != "/_vibewarden/admin/ui/" {
		t.Errorf("Location = %q, want %q", location, "/_vibewarden/admin/ui/")
	}
}

// TestAdminUIHandler_IndexHeaders verifies that GET /ui/ returns 200 with the
// expected Content-Type and Cache-Control: no-store headers.
func TestAdminUIHandler_IndexHeaders(t *testing.T) {
	h := newTestUIHandler()
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/ui/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}

	cc := w.Header().Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

// TestAdminUIHandler_IndexBody verifies that GET /ui/ returns the index.html content.
func TestAdminUIHandler_IndexBody(t *testing.T) {
	h := newTestUIHandler()
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/ui/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "<!doctype html>") {
		t.Errorf("body does not contain expected HTML; got: %s", string(body))
	}
}

// TestAdminUIHandler_AssetContentTypes verifies that individual assets are
// served with the correct MIME types.
func TestAdminUIHandler_AssetContentTypes(t *testing.T) {
	tests := []struct {
		path   string
		wantCT string
	}{
		{"/_vibewarden/admin/ui/app.js", "text/javascript"},
		{"/_vibewarden/admin/ui/styles.css", "text/css"},
	}

	h := newTestUIHandler()

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}
			ct := w.Header().Get("Content-Type")
			if !strings.Contains(ct, tt.wantCT) {
				t.Errorf("Content-Type = %q, want it to contain %q", ct, tt.wantCT)
			}
		})
	}
}

// TestAdminUIHandler_UnknownSubPath verifies that requesting a path that does
// not exist in the embedded FS returns 404.
func TestAdminUIHandler_UnknownSubPath(t *testing.T) {
	h := newTestUIHandler()
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/ui/nonexistent.txt", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d for unknown asset", w.Code, http.StatusNotFound)
	}
}

// TestAdminUIHandler_NoDirectoryListing verifies that requesting a sub-directory
// path (ending in /) does not return a directory listing (must return 404).
func TestAdminUIHandler_NoDirectoryListing(t *testing.T) {
	// Build a FS that has a sub-directory to confirm the handler blocks listing.
	subFS := fstest.MapFS{
		"index.html":      {Data: []byte(`<!doctype html><html></html>`)},
		"app.js":          {Data: []byte(`// app`)},
		"styles.css":      {Data: []byte(`/* css */`)},
		"sub/private.txt": {Data: []byte(`secret`)},
	}
	h := vibehttp.NewAdminUIHandler(subFS)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/ui/sub/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (no directory listing)", w.Code, http.StatusNotFound)
	}

	// The body must not contain a directory listing (HTML with file links).
	body := w.Body.String()
	if strings.Contains(body, "private.txt") {
		t.Errorf("body contains directory entry %q — directory listing is enabled", "private.txt")
	}
}

// TestRegisterAdminUIRoutes verifies that both the bare prefix and the slash
// prefix are registered on the mux and routed to the UI handler.
func TestRegisterAdminUIRoutes(t *testing.T) {
	h := newTestUIHandler()
	mux := http.NewServeMux()
	vibehttp.RegisterAdminUIRoutes(mux, h)

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/_vibewarden/admin/ui", http.StatusMovedPermanently},
		{"/_vibewarden/admin/ui/", http.StatusOK},
		{"/_vibewarden/admin/ui/app.js", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("path %q: status = %d, want %d", tt.path, w.Code, tt.wantStatus)
			}
		})
	}
}
