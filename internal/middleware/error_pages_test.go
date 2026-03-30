package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a test helper that writes content to path, creating parent
// directories as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestNewErrorPageResolver_ServeHTMLPage verifies that an HTML file is served
// with the correct Content-Type and status code.
func TestNewErrorPageResolver_ServeHTMLPage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "401.html"), "<h1>Unauthorized</h1>")

	resolver := NewErrorPageResolver(dir)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	resolver.WriteResponse(w, req, http.StatusUnauthorized, "unauthorized", "go away", 0)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/html; charset=utf-8")
	}
	if got := w.Body.String(); got != "<h1>Unauthorized</h1>" {
		t.Errorf("body = %q, want %q", got, "<h1>Unauthorized</h1>")
	}
}

// TestNewErrorPageResolver_ServeJSONPage verifies that a .json custom page is
// served with application/json Content-Type when no .html exists.
func TestNewErrorPageResolver_ServeJSONPage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "403.json"), `{"code":403,"msg":"forbidden"}`)

	resolver := NewErrorPageResolver(dir)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	resolver.WriteResponse(w, req, http.StatusForbidden, "forbidden", "", 0)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if got := w.Body.String(); got != `{"code":403,"msg":"forbidden"}` {
		t.Errorf("body = %q, want %q", got, `{"code":403,"msg":"forbidden"}`)
	}
}

// TestNewErrorPageResolver_HTMLPreferredOverJSON verifies that .html takes
// priority over .json when both files exist for the same status code.
func TestNewErrorPageResolver_HTMLPreferredOverJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "403.html"), "<p>html forbidden</p>")
	writeFile(t, filepath.Join(dir, "403.json"), `{"code":403}`)

	resolver := NewErrorPageResolver(dir)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	resolver.WriteResponse(w, req, http.StatusForbidden, "forbidden", "", 0)

	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q — .html should win over .json", ct, "text/html; charset=utf-8")
	}
	if got := w.Body.String(); got != "<p>html forbidden</p>" {
		t.Errorf("body = %q, want %q", got, "<p>html forbidden</p>")
	}
}

// TestNewErrorPageResolver_FallbackToJSON verifies that when no custom file
// exists the default JSON error response is returned.
func TestNewErrorPageResolver_FallbackToJSON(t *testing.T) {
	dir := t.TempDir() // empty directory — no custom pages

	resolver := NewErrorPageResolver(dir)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	resolver.WriteResponse(w, req, http.StatusForbidden, "forbidden", "access denied", 0)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body is not valid JSON: %v (body=%q)", err, w.Body.String())
	}
	if resp.Error != "forbidden" {
		t.Errorf("error = %q, want %q", resp.Error, "forbidden")
	}
}

// TestNopErrorPageResolver_AlwaysFallsBack verifies that NopErrorPageResolver
// never attempts to read a file and always produces the default JSON response.
func TestNopErrorPageResolver_AlwaysFallsBack(t *testing.T) {
	resolver := NopErrorPageResolver()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	resolver.WriteResponse(w, req, http.StatusUnauthorized, "unauthorized", "nope", 0)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

// TestErrorPageResolver_429FallbackUsesRateLimitResponse verifies that a
// missing custom page for 429 falls back to WriteRateLimitResponse (which sets
// Retry-After) rather than the generic WriteErrorResponse.
func TestErrorPageResolver_429FallbackUsesRateLimitResponse(t *testing.T) {
	dir := t.TempDir() // no custom 429 page

	resolver := NewErrorPageResolver(dir)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	resolver.WriteResponse(w, req, http.StatusTooManyRequests, "rate_limit_exceeded", "", 42)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if got := w.Header().Get("Retry-After"); got != "42" {
		t.Errorf("Retry-After = %q, want %q", got, "42")
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body is not valid JSON: %v (body=%q)", err, w.Body.String())
	}
	if resp.RetryAfterSeconds != 42 {
		t.Errorf("retry_after_seconds = %d, want 42", resp.RetryAfterSeconds)
	}
}

// TestErrorPageResolver_429CustomPageSkipsRetryAfterHeader verifies that when a
// custom HTML page exists for 429, it is served and the Retry-After header is
// NOT automatically set (the custom page owns the response entirely).
func TestErrorPageResolver_429CustomPageSkipsRetryAfterHeader(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "429.html"), "<p>slow down</p>")

	resolver := NewErrorPageResolver(dir)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	resolver.WriteResponse(w, req, http.StatusTooManyRequests, "rate_limit_exceeded", "", 10)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/html; charset=utf-8")
	}
	// Custom page was served — Retry-After is not set by the resolver.
	if got := w.Header().Get("Retry-After"); got != "" {
		t.Errorf("Retry-After = %q, want empty when custom page is served", got)
	}
	if got := w.Body.String(); got != "<p>slow down</p>" {
		t.Errorf("body = %q, want %q", got, "<p>slow down</p>")
	}
}

// TestServeCustomErrorPage_ReturnsTrue verifies the lower-level helper function.
func TestServeCustomErrorPage_ReturnsTrue(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "502.html"), "<p>bad gateway</p>")

	w := httptest.NewRecorder()
	if !ServeCustomErrorPage(w, dir, http.StatusBadGateway) {
		t.Fatal("ServeCustomErrorPage returned false, want true")
	}
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// TestServeCustomErrorPage_ReturnsFalseWhenMissing verifies the helper returns
// false when neither .html nor .json exists for the status code.
func TestServeCustomErrorPage_ReturnsFalseWhenMissing(t *testing.T) {
	dir := t.TempDir()

	w := httptest.NewRecorder()
	if ServeCustomErrorPage(w, dir, http.StatusBadGateway) {
		t.Error("ServeCustomErrorPage returned true, want false for missing file")
	}
}

// TestServeCustomErrorPage_ReturnsFalseForEmptyDir verifies that an empty
// directory string always returns false.
func TestServeCustomErrorPage_ReturnsFalseForEmptyDir(t *testing.T) {
	w := httptest.NewRecorder()
	if ServeCustomErrorPage(w, "", http.StatusForbidden) {
		t.Error("ServeCustomErrorPage returned true, want false for empty dir")
	}
}

// TestContentTypeForExt covers the contentTypeForExt helper.
func TestContentTypeForExt(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".html", "text/html; charset=utf-8"},
		{".json", "application/json"},
		{".txt", "application/octet-stream"},
		{"", "application/octet-stream"},
		{".xml", "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			got := contentTypeForExt(tt.ext)
			if got != tt.want {
				t.Errorf("contentTypeForExt(%q) = %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}

// TestValidateErrorPagesDirectory_Valid verifies that a real directory passes.
func TestValidateErrorPagesDirectory_Valid(t *testing.T) {
	dir := t.TempDir()
	if err := validateErrorPagesDirectory(dir); err != nil {
		t.Errorf("validateErrorPagesDirectory(%q) = %v, want nil", dir, err)
	}
}

// TestValidateErrorPagesDirectory_Empty verifies that an empty string is accepted.
func TestValidateErrorPagesDirectory_Empty(t *testing.T) {
	if err := validateErrorPagesDirectory(""); err != nil {
		t.Errorf("validateErrorPagesDirectory(\"\") = %v, want nil", err)
	}
}

// TestValidateErrorPagesDirectory_NonExistent verifies that a missing path is rejected.
func TestValidateErrorPagesDirectory_NonExistent(t *testing.T) {
	if err := validateErrorPagesDirectory("/does/not/exist/surely"); err == nil {
		t.Error("validateErrorPagesDirectory returned nil for non-existent path, want error")
	}
}

// TestValidateErrorPagesDirectory_File verifies that a regular file path is rejected.
func TestValidateErrorPagesDirectory_File(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "somefile")
	writeFile(t, file, "data")

	if err := validateErrorPagesDirectory(file); err == nil {
		t.Error("validateErrorPagesDirectory returned nil for a regular file, want error")
	}
}

// TestErrorPageResolver_TableDriven exercises WriteResponse across the four
// status codes called out in the issue (401, 403, 429, 502/503).
func TestErrorPageResolver_TableDriven(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "401.html"), "<p>401</p>")
	writeFile(t, filepath.Join(dir, "403.html"), "<p>403</p>")
	writeFile(t, filepath.Join(dir, "429.html"), "<p>429</p>")
	writeFile(t, filepath.Join(dir, "502.json"), `{"error":"bad_gateway"}`)
	// 503 intentionally omitted — tests fallback path.

	resolver := NewErrorPageResolver(dir)

	tests := []struct {
		name              string
		status            int
		errorCode         string
		retryAfterSeconds int
		wantContentType   string
		wantBodyContains  string
	}{
		{
			name:             "401 custom HTML",
			status:           http.StatusUnauthorized,
			errorCode:        "unauthorized",
			wantContentType:  "text/html; charset=utf-8",
			wantBodyContains: "<p>401</p>",
		},
		{
			name:             "403 custom HTML",
			status:           http.StatusForbidden,
			errorCode:        "forbidden",
			wantContentType:  "text/html; charset=utf-8",
			wantBodyContains: "<p>403</p>",
		},
		{
			name:              "429 custom HTML (no Retry-After added)",
			status:            http.StatusTooManyRequests,
			errorCode:         "rate_limit_exceeded",
			retryAfterSeconds: 5,
			wantContentType:   "text/html; charset=utf-8",
			wantBodyContains:  "<p>429</p>",
		},
		{
			name:             "502 custom JSON",
			status:           http.StatusBadGateway,
			errorCode:        "bad_gateway",
			wantContentType:  "application/json",
			wantBodyContains: `{"error":"bad_gateway"}`,
		},
		{
			name:             "503 falls back to default JSON",
			status:           http.StatusServiceUnavailable,
			errorCode:        "service_unavailable",
			wantContentType:  "application/json",
			wantBodyContains: `"service_unavailable"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			resolver.WriteResponse(w, req, tt.status, tt.errorCode, "", tt.retryAfterSeconds)

			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
			if ct := w.Header().Get("Content-Type"); ct != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", ct, tt.wantContentType)
			}
			if body := w.Body.String(); !contains(body, tt.wantBodyContains) {
				t.Errorf("body = %q, want it to contain %q", body, tt.wantBodyContains)
			}
		})
	}
}

// contains is a simple substring check helper to avoid importing strings in tests.
func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}())
}
