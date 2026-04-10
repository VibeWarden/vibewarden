package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/apispec"
)

func TestServeOpenAPISpec(t *testing.T) {
	tests := []struct {
		name            string
		method          string
		wantStatus      int
		wantContentType string
		wantBodyPrefix  string
	}{
		{
			name:            "GET returns YAML with correct content type",
			method:          http.MethodGet,
			wantStatus:      http.StatusOK,
			wantContentType: "application/yaml",
			wantBodyPrefix:  "openapi:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/_vibewarden/api/docs", nil)
			rr := httptest.NewRecorder()

			serveOpenAPISpec(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
			ct := rr.Header().Get("Content-Type")
			if ct != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", ct, tt.wantContentType)
			}
			body := rr.Body.String()
			if !strings.HasPrefix(strings.TrimSpace(body), tt.wantBodyPrefix) {
				t.Errorf("body does not start with %q, got: %s", tt.wantBodyPrefix, body[:min(len(body), 80)])
			}
		})
	}
}

func TestServeOpenAPISpec_BodyMatchesEmbeddedSpec(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/api/docs", nil)
	rr := httptest.NewRecorder()

	serveOpenAPISpec(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	want := string(apispec.Spec)
	got := rr.Body.String()
	if got != want {
		t.Errorf("response body does not match embedded spec\ngot  len=%d\nwant len=%d", len(got), len(want))
	}
}

func TestRegisterDocsRoute(t *testing.T) {
	mux := http.NewServeMux()
	RegisterDocsRoute(mux)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/api/docs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from registered docs route, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
