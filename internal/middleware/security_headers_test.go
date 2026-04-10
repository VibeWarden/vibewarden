package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// tlsRequest returns an *http.Request that simulates an HTTPS connection by
// populating the TLS field with a non-nil *tls.ConnectionState.
func tlsRequest(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	req.TLS = &tls.ConnectionState{}
	return req
}

func TestSecurityHeaders_HSTS_OverHTTPS(t *testing.T) {
	tests := []struct {
		name     string
		cfg      ports.SecurityHeadersConfig
		wantHSTS string
	}{
		{
			name: "max-age only",
			cfg: ports.SecurityHeadersConfig{
				Enabled:    true,
				HSTSMaxAge: 3600,
			},
			wantHSTS: "max-age=3600",
		},
		{
			name: "max-age with includeSubDomains",
			cfg: ports.SecurityHeadersConfig{
				Enabled:               true,
				HSTSMaxAge:            31536000,
				HSTSIncludeSubDomains: true,
			},
			wantHSTS: "max-age=31536000; includeSubDomains",
		},
		{
			name: "max-age with includeSubDomains and preload",
			cfg: ports.SecurityHeadersConfig{
				Enabled:               true,
				HSTSMaxAge:            31536000,
				HSTSIncludeSubDomains: true,
				HSTSPreload:           true,
			},
			wantHSTS: "max-age=31536000; includeSubDomains; preload",
		},
		{
			name: "zero max-age disables HSTS even over HTTPS",
			cfg: ports.SecurityHeadersConfig{
				Enabled:    true,
				HSTSMaxAge: 0,
			},
			wantHSTS: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := SecurityHeaders(tt.cfg)

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := tlsRequest(t)
			w := httptest.NewRecorder()

			mw(next).ServeHTTP(w, req)

			got := w.Header().Get("Strict-Transport-Security")
			if got != tt.wantHSTS {
				t.Errorf("Strict-Transport-Security = %q, want %q", got, tt.wantHSTS)
			}
		})
	}
}

func TestSecurityHeaders_HSTS_NotSentOverHTTP(t *testing.T) {
	tests := []struct {
		name string
		cfg  ports.SecurityHeadersConfig
	}{
		{
			name: "max-age set but request is plain HTTP",
			cfg: ports.SecurityHeadersConfig{
				Enabled:    true,
				HSTSMaxAge: 31536000,
			},
		},
		{
			name: "all HSTS flags set but request is plain HTTP",
			cfg: ports.SecurityHeadersConfig{
				Enabled:               true,
				HSTSMaxAge:            31536000,
				HSTSIncludeSubDomains: true,
				HSTSPreload:           true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := SecurityHeaders(tt.cfg)

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Plain HTTP request — r.TLS is nil.
			req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			w := httptest.NewRecorder()

			mw(next).ServeHTTP(w, req)

			got := w.Header().Get("Strict-Transport-Security")
			if got != "" {
				t.Errorf("Strict-Transport-Security must not be set over HTTP, got %q", got)
			}
		})
	}
}

func TestSecurityHeaders_ContentTypeOptions(t *testing.T) {
	tests := []struct {
		name    string
		nosniff bool
		want    string
	}{
		{"nosniff enabled", true, "nosniff"},
		{"nosniff disabled", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ports.SecurityHeadersConfig{
				Enabled:            true,
				ContentTypeNosniff: tt.nosniff,
			}

			mw := SecurityHeaders(cfg)
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			got := w.Header().Get("X-Content-Type-Options")
			if got != tt.want {
				t.Errorf("X-Content-Type-Options = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSecurityHeaders_FrameOptions(t *testing.T) {
	tests := []struct {
		name        string
		frameOption string
		want        string
	}{
		{"DENY", "DENY", "DENY"},
		{"SAMEORIGIN", "SAMEORIGIN", "SAMEORIGIN"},
		{"disabled (empty)", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ports.SecurityHeadersConfig{
				Enabled:     true,
				FrameOption: tt.frameOption,
			}

			mw := SecurityHeaders(cfg)
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			got := w.Header().Get("X-Frame-Options")
			if got != tt.want {
				t.Errorf("X-Frame-Options = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSecurityHeaders_CSP(t *testing.T) {
	cfg := ports.SecurityHeadersConfig{
		Enabled:               true,
		ContentSecurityPolicy: "default-src 'self'; script-src 'none'",
	}

	mw := SecurityHeaders(cfg)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	got := w.Header().Get("Content-Security-Policy")
	want := "default-src 'self'; script-src 'none'"
	if got != want {
		t.Errorf("Content-Security-Policy = %q, want %q", got, want)
	}
}

func TestSecurityHeaders_ReferrerPolicy(t *testing.T) {
	cfg := ports.SecurityHeadersConfig{
		Enabled:        true,
		ReferrerPolicy: "strict-origin-when-cross-origin",
	}

	mw := SecurityHeaders(cfg)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	got := w.Header().Get("Referrer-Policy")
	want := "strict-origin-when-cross-origin"
	if got != want {
		t.Errorf("Referrer-Policy = %q, want %q", got, want)
	}
}

func TestSecurityHeaders_PermissionsPolicy(t *testing.T) {
	cfg := ports.SecurityHeadersConfig{
		Enabled:           true,
		PermissionsPolicy: "camera=(), microphone=()",
	}

	mw := SecurityHeaders(cfg)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	got := w.Header().Get("Permissions-Policy")
	want := "camera=(), microphone=()"
	if got != want {
		t.Errorf("Permissions-Policy = %q, want %q", got, want)
	}
}

func TestSecurityHeaders_NextIsAlwaysCalled(t *testing.T) {
	cfg := ports.SecurityHeadersConfig{
		Enabled:            true,
		ContentTypeNosniff: true,
	}

	mw := SecurityHeaders(cfg)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodGet, "/any-path", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next handler was not called")
	}
	if w.Code != http.StatusCreated {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestDefaultSecurityHeadersConfig(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.HSTSMaxAge != 31536000 {
		t.Errorf("HSTSMaxAge = %d, want 31536000", cfg.HSTSMaxAge)
	}
	if !cfg.HSTSIncludeSubDomains {
		t.Error("expected HSTSIncludeSubDomains to be true")
	}
	if cfg.HSTSPreload {
		t.Error("expected HSTSPreload to be false (requires manual submission)")
	}
	if !cfg.ContentTypeNosniff {
		t.Error("expected ContentTypeNosniff to be true")
	}
	if cfg.FrameOption != "DENY" {
		t.Errorf("FrameOption = %q, want %q", cfg.FrameOption, "DENY")
	}
	if cfg.ContentSecurityPolicy != "" {
		t.Errorf("expected ContentSecurityPolicy to be empty (opt-in), got %q", cfg.ContentSecurityPolicy)
	}
	if cfg.ReferrerPolicy == "" {
		t.Error("expected ReferrerPolicy to be non-empty")
	}
}
