package caddy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestAdminAuthHandler_CaddyModule verifies the Caddy module metadata.
func TestAdminAuthHandler_CaddyModule(t *testing.T) {
	info := AdminAuthHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_admin_auth" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_admin_auth")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}

	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*AdminAuthHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *AdminAuthHandler", mod)
	}
}

// TestAdminAuthHandler_InterfaceGuards verifies the handler satisfies required Caddy interfaces.
func TestAdminAuthHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*AdminAuthHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*AdminAuthHandler)(nil)
}

// TestAdminAuthHandler_Provision verifies that Provision initialises the handler.
func TestAdminAuthHandler_Provision(t *testing.T) {
	tests := []struct {
		name    string
		config  AdminAuthHandlerConfig
		wantErr bool
	}{
		{
			name:    "provision with enabled config and token",
			config:  AdminAuthHandlerConfig{Enabled: true, Token: "secret"},
			wantErr: false,
		},
		{
			name:    "provision with disabled config",
			config:  AdminAuthHandlerConfig{Enabled: false, Token: ""},
			wantErr: false,
		},
		{
			name:    "provision with enabled config and empty token",
			config:  AdminAuthHandlerConfig{Enabled: true, Token: ""},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &AdminAuthHandler{Config: tt.config}

			err := h.Provision(gocaddy.Context{})
			if (err != nil) != tt.wantErr {
				t.Errorf("Provision() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && h.handler == nil {
				t.Error("Provision() did not create handler")
			}
		})
	}
}

// TestAdminAuthHandler_ServeHTTP verifies the handler integrates with the middleware.
func TestAdminAuthHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name       string
		config     AdminAuthHandlerConfig
		path       string
		token      string
		wantStatus int
		wantNext   bool
	}{
		{
			name:       "non-admin path passes through",
			config:     AdminAuthHandlerConfig{Enabled: true, Token: "secret"},
			path:       "/app/page",
			token:      "",
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		{
			name:       "admin path with correct token passes through",
			config:     AdminAuthHandlerConfig{Enabled: true, Token: "secret"},
			path:       "/_vibewarden/admin/users",
			token:      "secret",
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		{
			name:       "admin path with wrong token returns 401",
			config:     AdminAuthHandlerConfig{Enabled: true, Token: "secret"},
			path:       "/_vibewarden/admin/users",
			token:      "wrong",
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		{
			name:       "admin path when disabled returns 404",
			config:     AdminAuthHandlerConfig{Enabled: false, Token: "secret"},
			path:       "/_vibewarden/admin/users",
			token:      "secret",
			wantStatus: http.StatusNotFound,
			wantNext:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &AdminAuthHandler{Config: tt.config}
			if err := h.Provision(gocaddy.Context{}); err != nil {
				t.Fatalf("Provision() error = %v", err)
			}

			nextCalled := false
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.token != "" {
				req.Header.Set("X-Admin-Key", tt.token)
			}
			w := httptest.NewRecorder()

			err := h.ServeHTTP(w, req, next)
			if err != nil {
				t.Errorf("ServeHTTP() error = %v", err)
			}

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if nextCalled != tt.wantNext {
				t.Errorf("nextCalled = %v, want %v", nextCalled, tt.wantNext)
			}
		})
	}
}

// TestBuildAdminAuthHandlerJSON verifies the JSON serialisation of the handler config.
func TestBuildAdminAuthHandlerJSON(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ports.AdminAuthConfig
		wantErr bool
		checks  func(t *testing.T, result map[string]any)
	}{
		{
			name:    "produces correct handler name",
			cfg:     ports.AdminAuthConfig{Enabled: true, Token: "secret"},
			wantErr: false,
			checks: func(t *testing.T, result map[string]any) {
				t.Helper()
				handler, ok := result["handler"]
				if !ok {
					t.Fatal("result missing 'handler' key")
				}
				if handler != "vibewarden_admin_auth" {
					t.Errorf("handler = %q, want %q", handler, "vibewarden_admin_auth")
				}
			},
		},
		{
			name:    "config key is present and valid JSON",
			cfg:     ports.AdminAuthConfig{Enabled: true, Token: "my-token"},
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
				if enabled, _ := parsed["enabled"].(bool); !enabled {
					t.Error("config.enabled should be true")
				}
				if token, _ := parsed["token"].(string); token != "my-token" {
					t.Errorf("config.token = %q, want %q", token, "my-token")
				}
			},
		},
		{
			name:    "disabled config round-trips correctly",
			cfg:     ports.AdminAuthConfig{Enabled: false, Token: ""},
			wantErr: false,
			checks: func(t *testing.T, result map[string]any) {
				t.Helper()
				rawBytes, _ := json.Marshal(result["config"])
				var parsed map[string]any
				_ = json.Unmarshal(rawBytes, &parsed)
				if enabled, _ := parsed["enabled"].(bool); enabled {
					t.Error("config.enabled should be false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildAdminAuthHandlerJSON(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildAdminAuthHandlerJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checks != nil {
				tt.checks(t, result)
			}
		})
	}
}

// TestBuildCaddyConfig_AdminAuthHandlerPresent verifies the admin auth handler
// appears in the catch-all route handlers.
func TestBuildCaddyConfig_AdminAuthHandlerPresent(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *ports.ProxyConfig
		wantHandlerAt int
	}{
		{
			name: "admin auth handler present with no other optional handlers",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				AdminAuth:    ports.AdminAuthConfig{Enabled: true, Token: "secret"},
			},
			wantHandlerAt: 0,
		},
		{
			name: "admin auth handler after security headers when both enabled",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				SecurityHeaders: ports.SecurityHeadersConfig{
					Enabled:            true,
					ContentTypeNosniff: true,
				},
				AdminAuth: ports.AdminAuthConfig{Enabled: true, Token: "secret"},
			},
			wantHandlerAt: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildCaddyConfig(tt.cfg)
			if err != nil {
				t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
			}

			server := extractServer(t, result)
			routes, ok := server["routes"].([]map[string]any)
			if !ok || len(routes) < 2 {
				t.Fatalf("expected at least 2 routes, got %v", routes)
			}

			// The catch-all proxy route is always last.
			catchAll := routes[len(routes)-1]
			handlers, ok := catchAll["handle"].([]map[string]any)
			if !ok || len(handlers) == 0 {
				t.Fatal("no handlers in catch-all route")
			}

			if tt.wantHandlerAt >= len(handlers) {
				t.Fatalf("handler index %d out of range (len=%d)", tt.wantHandlerAt, len(handlers))
			}

			got := handlers[tt.wantHandlerAt]["handler"]
			if got != "vibewarden_admin_auth" {
				t.Errorf("handlers[%d].handler = %v, want 'vibewarden_admin_auth'", tt.wantHandlerAt, got)
			}
		})
	}
}
