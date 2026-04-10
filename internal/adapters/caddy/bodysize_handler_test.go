package caddy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestBodySizeHandler_CaddyModule verifies the Caddy module metadata.
func TestBodySizeHandler_CaddyModule(t *testing.T) {
	info := BodySizeHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_body_size" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_body_size")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}
	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*BodySizeHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *BodySizeHandler", mod)
	}
}

// TestBodySizeHandler_InterfaceGuards verifies the handler satisfies required Caddy interfaces.
func TestBodySizeHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*BodySizeHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*BodySizeHandler)(nil)
}

// TestBodySizeHandler_Provision verifies that Provision is a no-op and returns no error.
func TestBodySizeHandler_Provision(t *testing.T) {
	h := &BodySizeHandler{
		Config: BodySizeHandlerConfig{MaxBytes: 1048576},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Errorf("Provision() unexpected error: %v", err)
	}
}

// TestBodySizeHandler_ResolveLimit tests the path-based limit resolution logic.
func TestBodySizeHandler_ResolveLimit(t *testing.T) {
	tests := []struct {
		name      string
		handler   BodySizeHandler
		path      string
		wantLimit int64
	}{
		{
			name: "no overrides — returns global max",
			handler: BodySizeHandler{
				Config: BodySizeHandlerConfig{MaxBytes: 1048576},
			},
			path:      "/any/path",
			wantLimit: 1048576,
		},
		{
			name: "override matches — returns override max",
			handler: BodySizeHandler{
				Config: BodySizeHandlerConfig{
					MaxBytes: 1048576,
					Overrides: []BodySizeOverrideHandlerConfig{
						{Path: "/api/upload", MaxBytes: 52428800},
					},
				},
			},
			path:      "/api/upload",
			wantLimit: 52428800,
		},
		{
			name: "override prefix matches sub-path",
			handler: BodySizeHandler{
				Config: BodySizeHandlerConfig{
					MaxBytes: 1048576,
					Overrides: []BodySizeOverrideHandlerConfig{
						{Path: "/api/upload", MaxBytes: 52428800},
					},
				},
			},
			path:      "/api/upload/profile-picture",
			wantLimit: 52428800,
		},
		{
			name: "no override match — returns global max",
			handler: BodySizeHandler{
				Config: BodySizeHandlerConfig{
					MaxBytes: 1048576,
					Overrides: []BodySizeOverrideHandlerConfig{
						{Path: "/api/upload", MaxBytes: 52428800},
					},
				},
			},
			path:      "/api/other",
			wantLimit: 1048576,
		},
		{
			name: "longest prefix match wins",
			handler: BodySizeHandler{
				Config: BodySizeHandlerConfig{
					MaxBytes: 1048576,
					Overrides: []BodySizeOverrideHandlerConfig{
						{Path: "/api", MaxBytes: 5242880},
						{Path: "/api/upload", MaxBytes: 52428800},
					},
				},
			},
			path:      "/api/upload",
			wantLimit: 52428800,
		},
		{
			name: "override with zero max means no limit for that path",
			handler: BodySizeHandler{
				Config: BodySizeHandlerConfig{
					MaxBytes: 1048576,
					Overrides: []BodySizeOverrideHandlerConfig{
						{Path: "/api/stream", MaxBytes: 0},
					},
				},
			},
			path:      "/api/stream",
			wantLimit: 0,
		},
		{
			name: "zero global max and no overrides — no limit",
			handler: BodySizeHandler{
				Config: BodySizeHandlerConfig{MaxBytes: 0},
			},
			path:      "/anything",
			wantLimit: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.handler.resolveLimit(tt.path)
			if got != tt.wantLimit {
				t.Errorf("resolveLimit(%q) = %d, want %d", tt.path, got, tt.wantLimit)
			}
		})
	}
}

// TestBodySizeHandler_ServeHTTP_NoLimit verifies that when MaxBytes is 0 the
// handler passes the request through unmodified and next is called.
func TestBodySizeHandler_ServeHTTP_NoLimit(t *testing.T) {
	nextCalled := false

	h := &BodySizeHandler{
		Config: BodySizeHandlerConfig{MaxBytes: 0},
	}

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewBufferString("hello"))
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Errorf("ServeHTTP() unexpected error: %v", err)
	}
	if !nextCalled {
		t.Error("ServeHTTP() did not call next")
	}
}

// TestBodySizeHandler_ServeHTTP_WithinLimit verifies that a request body within
// the configured limit passes through successfully.
func TestBodySizeHandler_ServeHTTP_WithinLimit(t *testing.T) {
	nextCalled := false

	h := &BodySizeHandler{
		Config: BodySizeHandlerConfig{MaxBytes: 1024},
	}

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		// Read the full body to trigger MaxBytesReader enforcement.
		_, err := io.ReadAll(r.Body)
		return err
	})

	body := bytes.Repeat([]byte("x"), 512) // 512 bytes — within the 1KB limit
	req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Errorf("ServeHTTP() unexpected error: %v", err)
	}
	if !nextCalled {
		t.Error("ServeHTTP() did not call next")
	}
}

// TestBodySizeHandler_ServeHTTP_ExceedsLimit verifies that reading beyond the
// limit returns an error (which net/http turns into a 413 response).
func TestBodySizeHandler_ServeHTTP_ExceedsLimit(t *testing.T) {
	h := &BodySizeHandler{
		Config: BodySizeHandlerConfig{MaxBytes: 10},
	}

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		// Read the full body to trigger MaxBytesReader enforcement.
		_, err := io.ReadAll(r.Body)
		return err
	})

	body := bytes.Repeat([]byte("x"), 100) // 100 bytes — exceeds 10-byte limit
	req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, next)
	if err == nil {
		t.Error("ServeHTTP() expected error when body exceeds limit, got nil")
	}
}

// TestBodySizeHandler_ServeHTTP_NilBody verifies that a request with no body
// passes through without error.
func TestBodySizeHandler_ServeHTTP_NilBody(t *testing.T) {
	nextCalled := false

	h := &BodySizeHandler{
		Config: BodySizeHandlerConfig{MaxBytes: 1024},
	}

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Body = nil
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Errorf("ServeHTTP() unexpected error: %v", err)
	}
	if !nextCalled {
		t.Error("ServeHTTP() did not call next")
	}
}

// TestBodySizeHandler_ServeHTTP_PerPathOverride verifies that the per-path
// override is applied instead of the global limit.
func TestBodySizeHandler_ServeHTTP_PerPathOverride(t *testing.T) {
	h := &BodySizeHandler{
		Config: BodySizeHandlerConfig{
			MaxBytes: 10, // tight global limit
			Overrides: []BodySizeOverrideHandlerConfig{
				{Path: "/api/upload", MaxBytes: 1024 * 1024}, // generous override
			},
		},
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		_, err := io.ReadAll(r.Body)
		return err
	})

	// 100 bytes — exceeds global limit (10) but within override limit (1MB)
	body := bytes.Repeat([]byte("x"), 100)
	req := httptest.NewRequest(http.MethodPost, "/api/upload", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Errorf("ServeHTTP() unexpected error on override path: %v", err)
	}
	if !nextCalled {
		t.Error("ServeHTTP() did not call next on override path")
	}
}

// TestBuildBodySizeHandlerJSON verifies the JSON serialisation of the handler config.
func TestBuildBodySizeHandlerJSON(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ports.BodySizeConfig
		wantErr bool
		checks  func(t *testing.T, result map[string]any)
	}{
		{
			name: "produces correct handler name",
			cfg: ports.BodySizeConfig{
				Enabled:  true,
				MaxBytes: 1048576,
			},
			wantErr: false,
			checks: func(t *testing.T, result map[string]any) {
				t.Helper()
				handler, ok := result["handler"]
				if !ok {
					t.Fatal("result missing 'handler' key")
				}
				if handler != "vibewarden_body_size" {
					t.Errorf("handler = %q, want %q", handler, "vibewarden_body_size")
				}
			},
		},
		{
			name: "config key is present and valid JSON",
			cfg: ports.BodySizeConfig{
				Enabled:  true,
				MaxBytes: 1048576,
			},
			wantErr: false,
			checks: func(t *testing.T, result map[string]any) {
				t.Helper()
				raw, ok := result["config"]
				if !ok {
					t.Fatal("result missing 'config' key")
				}
				rawBytes, err := json.Marshal(raw)
				if err != nil {
					t.Fatalf("config value is not JSON-serialisable: %v", err)
				}
				var parsed map[string]any
				if err := json.Unmarshal(rawBytes, &parsed); err != nil {
					t.Fatalf("config value is not valid JSON: %v", err)
				}
				maxBytes, ok := parsed["max_bytes"].(float64)
				if !ok {
					t.Fatalf("config.max_bytes is %T, want float64", parsed["max_bytes"])
				}
				if int64(maxBytes) != 1048576 {
					t.Errorf("config.max_bytes = %v, want 1048576", maxBytes)
				}
			},
		},
		{
			name: "overrides round-trip",
			cfg: ports.BodySizeConfig{
				Enabled:  true,
				MaxBytes: 1048576,
				Overrides: []ports.BodySizeOverride{
					{Path: "/api/upload", MaxBytes: 52428800},
					{Path: "/api/avatar", MaxBytes: 5242880},
				},
			},
			wantErr: false,
			checks: func(t *testing.T, result map[string]any) {
				t.Helper()
				rawBytes, _ := json.Marshal(result["config"])
				var parsed map[string]any
				_ = json.Unmarshal(rawBytes, &parsed)
				overrides, ok := parsed["overrides"].([]any)
				if !ok {
					t.Fatal("config.overrides missing or wrong type")
				}
				if len(overrides) != 2 {
					t.Errorf("len(overrides) = %d, want 2", len(overrides))
				}
			},
		},
		{
			name: "no overrides omits field",
			cfg: ports.BodySizeConfig{
				Enabled:  true,
				MaxBytes: 1048576,
			},
			wantErr: false,
			checks: func(t *testing.T, result map[string]any) {
				t.Helper()
				rawBytes, _ := json.Marshal(result["config"])
				var parsed map[string]any
				_ = json.Unmarshal(rawBytes, &parsed)
				if _, present := parsed["overrides"]; present {
					t.Error("expected \"overrides\" to be omitted when empty")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildBodySizeHandlerJSON(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildBodySizeHandlerJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checks != nil {
				tt.checks(t, result)
			}
		})
	}
}
