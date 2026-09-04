package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/authguard"
	"github.com/vibewarden/vibewarden/internal/plugins"
)

func TestResolveCSP(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{
			name: "raw string takes precedence over structured config",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					ContentSecurityPolicy: "default-src 'self'; script-src 'none'",
					CSP: config.CSPConfig{
						DefaultSrc: []string{"'self'", "https://cdn.example.com"},
					},
				},
			},
			want: "default-src 'self'; script-src 'none'",
		},
		{
			name: "structured config used when raw string is empty",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					ContentSecurityPolicy: "",
					CSP: config.CSPConfig{
						DefaultSrc: []string{"'self'"},
						ScriptSrc:  []string{"'self'", "https://cdn.example.com"},
						StyleSrc:   []string{"'self'", "'unsafe-inline'"},
						ImgSrc:     []string{"'self'", "data:"},
						ConnectSrc: []string{"'self'"},
						FontSrc:    []string{"'self'"},
						FrameSrc:   []string{"'none'"},
					},
				},
			},
			want: "default-src 'self'; script-src 'self' https://cdn.example.com; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-src 'none'",
		},
		{
			name: "both empty produces empty string",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					ContentSecurityPolicy: "",
					CSP:                   config.CSPConfig{},
				},
			},
			want: "",
		},
		{
			name: "raw string only — no structured config",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					ContentSecurityPolicy: "default-src 'none'",
				},
			},
			want: "default-src 'none'",
		},
		{
			name: "structured config only — default-src self",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					CSP: config.CSPConfig{
						DefaultSrc: []string{"'self'"},
					},
				},
			},
			want: "default-src 'self'",
		},
		{
			name: "all structured directives",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					CSP: config.CSPConfig{
						DefaultSrc:     []string{"'self'"},
						ScriptSrc:      []string{"'self'"},
						StyleSrc:       []string{"'self'"},
						ImgSrc:         []string{"'self'"},
						ConnectSrc:     []string{"'self'"},
						FontSrc:        []string{"'self'"},
						FrameSrc:       []string{"'none'"},
						MediaSrc:       []string{"'self'"},
						ObjectSrc:      []string{"'none'"},
						ManifestSrc:    []string{"'self'"},
						WorkerSrc:      []string{"'self'"},
						ChildSrc:       []string{"'self'"},
						FormAction:     []string{"'self'"},
						FrameAncestors: []string{"'none'"},
						BaseURI:        []string{"'self'"},
					},
				},
			},
			want: "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; font-src 'self'; frame-src 'none'; media-src 'self'; object-src 'none'; manifest-src 'self'; worker-src 'self'; child-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'self'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCSP(tt.cfg)
			if got != tt.want {
				t.Errorf("resolveCSP() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}

func TestBuildServerTimeoutsConfig(t *testing.T) {
	tests := []struct {
		name      string
		serverCfg config.ServerConfig
		wantRead  time.Duration
		wantWrite time.Duration
		wantIdle  time.Duration
	}{
		{
			name:      "empty strings use defaults",
			serverCfg: config.ServerConfig{},
			wantRead:  30 * time.Second,
			wantWrite: 60 * time.Second,
			wantIdle:  120 * time.Second,
		},
		{
			name: "explicit zero disables timeout",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "0",
				WriteTimeout: "0",
				IdleTimeout:  "0",
			},
			wantRead:  0,
			wantWrite: 0,
			wantIdle:  0,
		},
		{
			name: "custom valid durations are parsed",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "10s",
				WriteTimeout: "20s",
				IdleTimeout:  "90s",
			},
			wantRead:  10 * time.Second,
			wantWrite: 20 * time.Second,
			wantIdle:  90 * time.Second,
		},
		{
			name: "invalid read timeout falls back to default",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "notaduration",
				WriteTimeout: "60s",
				IdleTimeout:  "120s",
			},
			wantRead:  30 * time.Second,
			wantWrite: 60 * time.Second,
			wantIdle:  120 * time.Second,
		},
		{
			name: "invalid write timeout falls back to default",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "30s",
				WriteTimeout: "bad",
				IdleTimeout:  "120s",
			},
			wantRead:  30 * time.Second,
			wantWrite: 60 * time.Second,
			wantIdle:  120 * time.Second,
		},
		{
			name: "invalid idle timeout falls back to default",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "30s",
				WriteTimeout: "60s",
				IdleTimeout:  "bad",
			},
			wantRead:  30 * time.Second,
			wantWrite: 60 * time.Second,
			wantIdle:  120 * time.Second,
		},
		{
			name: "minute durations are accepted",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "1m",
				WriteTimeout: "2m",
				IdleTimeout:  "5m",
			},
			wantRead:  time.Minute,
			wantWrite: 2 * time.Minute,
			wantIdle:  5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Server: tt.serverCfg}
			got := buildServerTimeoutsConfig(cfg)
			if got.ReadTimeout != tt.wantRead {
				t.Errorf("ReadTimeout = %v, want %v", got.ReadTimeout, tt.wantRead)
			}
			if got.WriteTimeout != tt.wantWrite {
				t.Errorf("WriteTimeout = %v, want %v", got.WriteTimeout, tt.wantWrite)
			}
			if got.IdleTimeout != tt.wantIdle {
				t.Errorf("IdleTimeout = %v, want %v", got.IdleTimeout, tt.wantIdle)
			}
		})
	}
}

// buildTestRuntimeServices calls buildRuntimeServices with a discard logger and
// an empty plugin registry.
//
// os.Stdout is redirected for the duration of the call because
// buildRuntimeServices builds the audit logger over os.Stdout and captures it at
// construction time; without the redirect the wrong-token attempts below would
// print audit JSON into the test output.
func buildTestRuntimeServices(t *testing.T, cfg *config.Config) caddyadapter.RuntimeServices {
	t.Helper()

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = devNull.Close() })

	realStdout := os.Stdout
	os.Stdout = devNull
	defer func() { os.Stdout = realStdout }()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return buildRuntimeServices(logger, nil, plugins.NewRegistry(logger), nil, "test", cfg)
}

// TestBuildRuntimeServices_WiresAdminLockoutGuard pins the composition-root
// assignment itself. Without it, deleting the AdminLockoutGuard line from
// buildRuntimeServices leaves every other test green while the lockout is a
// production no-op: the adapter and middleware tests all inject their own guard.
func TestBuildRuntimeServices_WiresAdminLockoutGuard(t *testing.T) {
	svc := buildTestRuntimeServices(t, nil)

	if svc.AdminLockoutGuard == nil {
		t.Fatal("RuntimeServices.AdminLockoutGuard is nil; admin-token lockout would be disabled in production")
	}
}

// TestBuildRuntimeServices_AdminLockoutGuardThrottlesAdminAuthHandler drives the
// wired guard end to end: an AdminAuthHandler provisioned from the RuntimeServices
// that buildRuntimeServices actually returns must throttle after
// authguard.DefaultThreshold wrong-token attempts from one client IP.
//
// This covers the composition root with the real adapter and the real default
// policy, rather than a fake guard configured by the test.
func TestBuildRuntimeServices_AdminLockoutGuardThrottlesAdminAuthHandler(t *testing.T) {
	svc := buildTestRuntimeServices(t, nil)

	h := &caddyadapter.AdminAuthHandler{
		Config: caddyadapter.AdminAuthHandlerConfig{Enabled: true, Token: "correct-token"},
	}
	if err := h.ProvisionWith(svc); err != nil {
		t.Fatalf("ProvisionWith() error = %v", err)
	}

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})

	probe := func(token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
		req.RemoteAddr = "198.51.100.7:5555"
		req.Header.Set("X-Admin-Key", token)
		w := httptest.NewRecorder()
		if err := h.ServeHTTP(w, req, next); err != nil {
			t.Fatalf("ServeHTTP() error = %v", err)
		}
		return w
	}

	// Attempts 1..DefaultThreshold are plain 401s; the last one arms the lockout.
	for i := 1; i <= authguard.DefaultThreshold; i++ {
		if got := probe("wrong").Code; got != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want %d", i, got, http.StatusUnauthorized)
		}
	}

	// The next request is rejected during the cooldown. The correct token is
	// supplied on purpose: a locked-out client must be turned away before the
	// token is compared.
	w := probe("correct-token")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("post-threshold status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Error("Retry-After header is missing on the lockout response")
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("WWW-Authenticate = %q on a lockout response, want none", got)
	}

	// A different client IP is unaffected by the first client's lockout.
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	req.RemoteAddr = "198.51.100.8:5555"
	req.Header.Set("X-Admin-Key", "correct-token")
	other := httptest.NewRecorder()
	if err := h.ServeHTTP(other, req, next); err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	if other.Code != http.StatusOK {
		t.Errorf("second client status = %d, want %d", other.Code, http.StatusOK)
	}
}
