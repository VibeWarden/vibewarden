package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// okHandler is a simple next handler that always returns 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestAdminAuthMiddleware_NonAdminPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"root", "/"},
		{"app path", "/dashboard"},
		{"health endpoint", "/_vibewarden/health"},
		{"metrics endpoint", "/_vibewarden/metrics"},
		{"vibewarden non-admin prefix", "/_vibewarden/other"},
		// exact prefix without trailing slash is not an admin path
		{"exact prefix no slash", "/_vibewarden/admin"},
	}

	cfg := ports.AdminAuthConfig{Enabled: true, Token: "secret"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			mw := AdminAuthMiddleware(cfg)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			if !nextCalled {
				t.Errorf("path %q: next handler was not called (should pass through)", tt.path)
			}
			if w.Code != http.StatusOK {
				t.Errorf("path %q: status = %d, want %d", tt.path, w.Code, http.StatusOK)
			}
		})
	}
}

func TestAdminAuthMiddleware_AdminDisabled(t *testing.T) {
	cfg := ports.AdminAuthConfig{Enabled: false, Token: "secret"}
	mw := AdminAuthMiddleware(cfg)

	tests := []struct {
		name string
		path string
	}{
		{"admin root", "/_vibewarden/admin/"},
		{"admin users", "/_vibewarden/admin/users"},
		{"admin deep path", "/_vibewarden/admin/users/123/disable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			mw(okHandler).ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("path %q: status = %d, want %d (admin disabled)", tt.path, w.Code, http.StatusNotFound)
			}
		})
	}
}

func TestAdminAuthMiddleware_MisconfiguredNoToken(t *testing.T) {
	// Admin enabled but no token configured — should return 500.
	cfg := ports.AdminAuthConfig{Enabled: true, Token: ""}
	mw := AdminAuthMiddleware(cfg)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	req.Header.Set(adminKeyHeader, "any-value")
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d (misconfigured: enabled but no token)", w.Code, http.StatusInternalServerError)
	}
}

func TestAdminAuthMiddleware_MissingHeader(t *testing.T) {
	cfg := ports.AdminAuthConfig{Enabled: true, Token: "secret-token"}
	mw := AdminAuthMiddleware(cfg)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	// No X-Admin-Key header.
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (missing header)", w.Code, http.StatusUnauthorized)
	}
	if got := w.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("expected WWW-Authenticate header to be set on 401")
	}
}

func TestAdminAuthMiddleware_WrongToken(t *testing.T) {
	cfg := ports.AdminAuthConfig{Enabled: true, Token: "correct-token"}
	mw := AdminAuthMiddleware(cfg)

	tests := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"wrong token", "wrong-token"},
		{"partial match", "correct"},
		{"prefix match", "correct-token-extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
			if tt.token != "" {
				req.Header.Set(adminKeyHeader, tt.token)
			}
			w := httptest.NewRecorder()
			mw(okHandler).ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("token %q: status = %d, want %d", tt.token, w.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestAdminAuthMiddleware_CorrectToken(t *testing.T) {
	cfg := ports.AdminAuthConfig{Enabled: true, Token: "my-secret-admin-token"}
	mw := AdminAuthMiddleware(cfg)

	tests := []struct {
		name string
		path string
	}{
		{"admin root", "/_vibewarden/admin/"},
		{"admin users list", "/_vibewarden/admin/users"},
		{"admin user detail", "/_vibewarden/admin/users/abc-123"},
		{"admin nested", "/_vibewarden/admin/users/abc-123/sessions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set(adminKeyHeader, "my-secret-admin-token")
			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			if !nextCalled {
				t.Errorf("path %q: next handler not called for valid token", tt.path)
			}
			if w.Code != http.StatusOK {
				t.Errorf("path %q: status = %d, want %d", tt.path, w.Code, http.StatusOK)
			}
		})
	}
}

func TestAdminAuthMiddleware_WWWAuthenticateHeader(t *testing.T) {
	cfg := ports.AdminAuthConfig{Enabled: true, Token: "secret"}
	mw := AdminAuthMiddleware(cfg)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	req.Header.Set(adminKeyHeader, "wrong")
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	want := `Bearer realm="vibewarden-admin"`
	got := w.Header().Get("WWW-Authenticate")
	if got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestAdminAuthMiddleware_401IsJSON(t *testing.T) {
	// When the admin key is wrong, the 401 response must be JSON with a
	// correlation ID (trace_id or request_id).
	cfg := ports.AdminAuthConfig{Enabled: true, Token: "correct-token"}
	mw := AdminAuthMiddleware(cfg)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	req.Header.Set(adminKeyHeader, "wrong-token")
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Error != "unauthorized" {
		t.Errorf("error = %q, want %q", body.Error, "unauthorized")
	}
	if body.RequestID == "" && body.TraceID == "" {
		t.Error("expected request_id or trace_id in 401 response body")
	}
}

func TestSecureEqual(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"equal strings", "abc", "abc", true},
		{"different strings", "abc", "xyz", false},
		{"different lengths", "abc", "abcd", false},
		{"both empty", "", "", true},
		{"one empty", "", "abc", false},
		{"whitespace matters", "abc ", "abc", false},
		{"case sensitive", "Secret", "secret", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secureEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("secureEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
