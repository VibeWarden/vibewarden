package caddy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// TestJWTBearerHandler_CaddyModule verifies the Caddy module metadata.
func TestJWTBearerHandler_CaddyModule(t *testing.T) {
	info := JWTBearerHandler{}.CaddyModule()

	if info.ID != "http.handlers.jwt_bearer" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.jwt_bearer")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}
	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*JWTBearerHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *JWTBearerHandler", mod)
	}
}

// TestJWTBearerHandler_InterfaceGuards verifies the handler satisfies required Caddy interfaces.
func TestJWTBearerHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*JWTBearerHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*JWTBearerHandler)(nil)
	var _ gocaddy.Module = (*JWTBearerHandler)(nil)
}

// TestJWTBearerHandler_isPublicPath_PrefixBoundaryBypass verifies that a
// URL-space sibling (e.g. "/auth-evil") is NOT matched by a wildcard public-path
// rule (e.g. "/auth/*"). Regression test for #1264 (Part B).
func TestJWTBearerHandler_isPublicPath_PrefixBoundaryBypass(t *testing.T) {
	h := &JWTBearerHandler{Config: JWTBearerHandlerConfig{
		PublicPaths: []string{"/auth/*", "/public/*"},
	}}

	tests := []struct {
		path    string
		want    bool
		comment string
	}{
		{"/auth/login", true, "sub-path of /auth/* must match"},
		{"/auth/", true, "trailing slash of /auth/* must match"},
		{"/auth", true, "exact prefix /auth must match"},
		{"/auth-evil", false, "sibling prefix /auth-evil must NOT match /auth/*"},
		{"/authentic", false, "sibling prefix /authentic must NOT match /auth/*"},
		{"/public/docs", true, "sub-path of /public/* must match"},
		{"/public-secret", false, "sibling /public-secret must NOT match /public/*"},
		{"/public-secret/admin", false, "deep sibling must NOT match /public/*"},
		{"/other", false, "unrelated path must not match"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := h.isPublicPath(tt.path)
			if got != tt.want {
				t.Errorf("isPublicPath(%q) = %v, want %v — %s", tt.path, got, tt.want, tt.comment)
			}
		})
	}
}

// TestJWTBearerHandler_ServeHTTP_StripsForggedXUserHeaders verifies that any
// X-User-* header injected by a client is removed before the request reaches
// the upstream application. This covers X-User-Name which was previously not
// in the Caddy-layer strip list.
//
// Regression test for #1264 (Part A — JWT handler path).
func TestJWTBearerHandler_ServeHTTP_StripsForggedXUserHeaders(t *testing.T) {
	// Use a public path so the handler calls next.ServeHTTP after stripping.
	h := &JWTBearerHandler{Config: JWTBearerHandlerConfig{
		PublicPaths: []string{"/public/*"},
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	forgedHeaders := []struct {
		name  string
		value string
	}{
		{"X-User-Id", "forged-id"},
		{"X-User-Email", "forged@evil.com"},
		{"X-User-Verified", "true"},
		{"X-User-Role", "admin"},
		{"X-User-Name", "injected-admin"},
	}

	for _, fh := range forgedHeaders {
		t.Run("forged "+fh.name+" stripped on public path", func(t *testing.T) {
			var capturedHeaders http.Header
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				capturedHeaders = r.Header.Clone()
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, "/public/page", nil)
			req.Header.Set(fh.name, fh.value)
			w := httptest.NewRecorder()

			if err := h.ServeHTTP(w, req, next); err != nil {
				t.Fatalf("ServeHTTP() error = %v", err)
			}
			if capturedHeaders == nil {
				t.Fatal("next handler was not called for public path")
			}
			if got := capturedHeaders.Get(fh.name); got != "" {
				t.Errorf("header %q = %q (forged value) reached upstream; expected empty", fh.name, got)
			}
		})
	}
}

// TestJWTBearerHandler_ServeHTTP_NoBearer returns 401 when no Authorization header is present.
func TestJWTBearerHandler_ServeHTTP_NoBearer(t *testing.T) {
	h := &JWTBearerHandler{Config: JWTBearerHandlerConfig{
		JWKSURL:  "http://localhost:9999/.well-known/jwks.json",
		Issuer:   "test",
		Audience: "test",
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	if nextCalled {
		t.Error("next should not be called when no Bearer token is present")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestJWTBearerHandler_ServeHTTP_PublicPathBypass verifies that public paths
// skip JWT validation and call next.
func TestJWTBearerHandler_ServeHTTP_PublicPathBypass(t *testing.T) {
	h := &JWTBearerHandler{Config: JWTBearerHandlerConfig{
		JWKSURL:     "http://unreachable:9999/.well-known/jwks.json",
		PublicPaths: []string{"/health", "/public/*"},
	}}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	tests := []struct {
		path string
	}{
		{"/health"},
		{"/public/docs"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			nextCalled := false
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			if err := h.ServeHTTP(w, req, next); err != nil {
				t.Errorf("ServeHTTP() error = %v", err)
			}
			if !nextCalled {
				t.Errorf("expected next to be called for public path %q", tt.path)
			}
		})
	}
}
