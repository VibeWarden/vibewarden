package caddy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// TestContentTypeHandler_CaddyModule verifies the Caddy module metadata.
func TestContentTypeHandler_CaddyModule(t *testing.T) {
	info := ContentTypeHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_waf_content_type" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_waf_content_type")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}
	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*ContentTypeHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *ContentTypeHandler", mod)
	}
}

// TestContentTypeHandler_InterfaceGuards verifies the handler satisfies
// required Caddy interfaces.
func TestContentTypeHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*ContentTypeHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*ContentTypeHandler)(nil)
}

// TestContentTypeHandler_Provision verifies that Provision compiles the
// middleware and returns no error.
func TestContentTypeHandler_Provision(t *testing.T) {
	h := &ContentTypeHandler{
		Config: ContentTypeHandlerConfig{
			Allowed: []string{"application/json"},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Errorf("Provision() unexpected error: %v", err)
	}
	if h.mw == nil {
		t.Error("Provision() did not set mw field")
	}
}

// TestContentTypeHandler_ServeHTTP_AllowedContentType verifies that a request
// with an allowed Content-Type passes through to next.
func TestContentTypeHandler_ServeHTTP_AllowedContentType(t *testing.T) {
	h := &ContentTypeHandler{
		Config: ContentTypeHandlerConfig{
			Allowed: []string{"application/json"},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Errorf("ServeHTTP() unexpected error: %v", err)
	}
	if !nextCalled {
		t.Error("expected next to be called for allowed Content-Type")
	}
}

// TestContentTypeHandler_ServeHTTP_MissingContentType verifies that a POST
// request with no Content-Type is rejected with 415.
func TestContentTypeHandler_ServeHTTP_MissingContentType(t *testing.T) {
	h := &ContentTypeHandler{
		Config: ContentTypeHandlerConfig{
			Allowed: []string{"application/json"},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api", nil)
	// no Content-Type header
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Errorf("ServeHTTP() unexpected error: %v", err)
	}
	if nextCalled {
		t.Error("next should not be called when Content-Type is missing")
	}
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

// TestContentTypeHandler_ServeHTTP_DisallowedContentType verifies that a POST
// with a disallowed Content-Type is rejected with 415.
func TestContentTypeHandler_ServeHTTP_DisallowedContentType(t *testing.T) {
	h := &ContentTypeHandler{
		Config: ContentTypeHandlerConfig{
			Allowed: []string{"application/json"},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api", nil)
	req.Header.Set("Content-Type", "text/xml")
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Errorf("ServeHTTP() unexpected error: %v", err)
	}
	if nextCalled {
		t.Error("next should not be called for disallowed Content-Type")
	}
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

// TestContentTypeHandler_ServeHTTP_NoBodyMethod verifies that GET requests
// always pass through regardless of Content-Type configuration.
func TestContentTypeHandler_ServeHTTP_NoBodyMethod(t *testing.T) {
	h := &ContentTypeHandler{
		Config: ContentTypeHandlerConfig{
			Allowed: []string{"application/json"},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	noBodyMethods := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodDelete,
		http.MethodOptions,
	}

	for _, method := range noBodyMethods {
		t.Run(method, func(t *testing.T) {
			nextCalled := false
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(method, "/api", nil)
			// no Content-Type
			w := httptest.NewRecorder()

			if err := h.ServeHTTP(w, req, next); err != nil {
				t.Errorf("%s: ServeHTTP() unexpected error: %v", method, err)
			}
			if !nextCalled {
				t.Errorf("%s: expected next to be called", method)
			}
		})
	}
}

// TestBuildContentTypeHandlerJSON verifies the JSON serialisation of the
// handler config fragment.
func TestBuildContentTypeHandlerJSON(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		wantErr bool
	}{
		{
			name:    "single type",
			allowed: []string{"application/json"},
			wantErr: false,
		},
		{
			name:    "multiple types",
			allowed: []string{"application/json", "application/x-www-form-urlencoded", "multipart/form-data"},
			wantErr: false,
		},
		{
			name:    "empty allowed list",
			allowed: []string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildContentTypeHandlerJSON(tt.allowed)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildContentTypeHandlerJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			// Verify handler name.
			if result["handler"] != "vibewarden_waf_content_type" {
				t.Errorf("handler = %v, want \"vibewarden_waf_content_type\"", result["handler"])
			}

			// Verify config is valid JSON with the correct allowed list.
			rawCfg, ok := result["config"].(json.RawMessage)
			if !ok {
				t.Fatalf("config is %T, want json.RawMessage", result["config"])
			}

			var parsed ContentTypeHandlerConfig
			if err := json.Unmarshal(rawCfg, &parsed); err != nil {
				t.Fatalf("json.Unmarshal config: %v", err)
			}

			if len(parsed.Allowed) != len(tt.allowed) {
				t.Errorf("allowed count = %d, want %d", len(parsed.Allowed), len(tt.allowed))
			}
		})
	}
}
