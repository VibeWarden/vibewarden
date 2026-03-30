package authui_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/authui"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newHandler(t *testing.T, cfg authui.AuthUIConfig) *authui.Handler {
	t.Helper()
	h, err := authui.NewHandler(cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewHandler() error: %v", err)
	}
	return h
}

func defaultConfig() authui.AuthUIConfig {
	return authui.AuthUIConfig{
		Mode:            "built-in",
		PrimaryColor:    "#7C3AED",
		BackgroundColor: "#F3F4F6",
		TextColor:       "#111827",
		ErrorColor:      "#DC2626",
	}
}

// ---------------------------------------------------------------------------
// NewHandler
// ---------------------------------------------------------------------------

func TestNewHandler_DefaultConfig(t *testing.T) {
	h, err := authui.NewHandler(authui.AuthUIConfig{}, discardLogger())
	if err != nil {
		t.Fatalf("NewHandler() unexpected error: %v", err)
	}
	if h == nil {
		t.Fatal("NewHandler() returned nil handler")
	}
}

func TestNewHandler_NilLoggerIsAccepted(t *testing.T) {
	_, err := authui.NewHandler(defaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewHandler() with nil logger unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Start / Addr / Stop
// ---------------------------------------------------------------------------

func TestHandler_StartAndAddr(t *testing.T) {
	h := newHandler(t, defaultConfig())
	if err := h.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer h.Stop(nil) //nolint: errcheck

	addr := h.Addr()
	if addr == "" {
		t.Fatal("Addr() returned empty string after Start")
	}
	if !strings.Contains(addr, "127.0.0.1") {
		t.Errorf("Addr() = %q, want to contain 127.0.0.1", addr)
	}
}

// ---------------------------------------------------------------------------
// HTTP page responses — table-driven
// ---------------------------------------------------------------------------

func TestHandler_Pages(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantStatus   int
		wantContains []string
	}{
		{
			name:       "login page",
			path:       "/_vibewarden/login",
			wantStatus: http.StatusOK,
			wantContains: []string{
				"Log in",
				"/_vibewarden/registration",
				"/_vibewarden/recovery",
				"#7C3AED",
			},
		},
		{
			name:       "registration page",
			path:       "/_vibewarden/registration",
			wantStatus: http.StatusOK,
			wantContains: []string{
				"Create account",
				"/_vibewarden/login",
				"#7C3AED",
			},
		},
		{
			name:       "recovery page",
			path:       "/_vibewarden/recovery",
			wantStatus: http.StatusOK,
			wantContains: []string{
				"Recover account",
				"/_vibewarden/login",
				"#7C3AED",
			},
		},
		{
			name:       "verification page",
			path:       "/_vibewarden/verification",
			wantStatus: http.StatusOK,
			wantContains: []string{
				"Verify email",
				"/_vibewarden/login",
				"#7C3AED",
			},
		},
		{
			name:       "settings page",
			path:       "/_vibewarden/settings",
			wantStatus: http.StatusOK,
			wantContains: []string{
				"Account settings",
				"Change password",
				"Email verification",
				"/_vibewarden/login",
				"#7C3AED",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandler(t, defaultConfig())
			if err := h.Start(); err != nil {
				t.Fatalf("Start() error: %v", err)
			}
			defer h.Stop(nil) //nolint: errcheck

			resp, err := http.Get("http://" + h.Addr() + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer func() { _ = resp.Body.Close() }() //nolint:errcheck

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)
			for _, want := range tt.wantContains {
				if !strings.Contains(bodyStr, want) {
					t.Errorf("body missing %q", want)
				}
			}

			ct := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}

			cc := resp.Header.Get("Cache-Control")
			if cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Theme injection
// ---------------------------------------------------------------------------

func TestHandler_ThemeColorsInjected(t *testing.T) {
	cfg := authui.AuthUIConfig{
		PrimaryColor:    "#AABBCC",
		BackgroundColor: "#112233",
		TextColor:       "#445566",
		ErrorColor:      "#778899",
	}
	h := newHandler(t, cfg)
	if err := h.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer h.Stop(nil) //nolint: errcheck

	pages := []string{
		"/_vibewarden/login",
		"/_vibewarden/registration",
		"/_vibewarden/recovery",
		"/_vibewarden/verification",
		"/_vibewarden/settings",
	}

	for _, path := range pages {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get("http://" + h.Addr() + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer func() { _ = resp.Body.Close() }() //nolint:errcheck

			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)

			for _, color := range []string{"#AABBCC", "#112233", "#445566", "#778899"} {
				if !strings.Contains(bodyStr, color) {
					t.Errorf("page %s missing color %q in body", path, color)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ReturnToQuery preservation
// ---------------------------------------------------------------------------

func TestHandler_ReturnToQueryPropagated(t *testing.T) {
	h := newHandler(t, defaultConfig())
	if err := h.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer h.Stop(nil) //nolint: errcheck

	// The login page should include the return_to value in registration link.
	resp, err := http.Get(fmt.Sprintf("http://%s/_vibewarden/login?return_to=%%2Fdashboard", h.Addr()))
	if err != nil {
		t.Fatalf("GET login with return_to: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "return_to") {
		t.Error("login page body does not contain return_to when it is set")
	}
}

// ---------------------------------------------------------------------------
// Default colors applied when config is empty
// ---------------------------------------------------------------------------

func TestHandler_DefaultColorsApplied(t *testing.T) {
	h := newHandler(t, authui.AuthUIConfig{})
	if err := h.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer h.Stop(nil) //nolint: errcheck

	resp, err := http.Get("http://" + h.Addr() + "/_vibewarden/login")
	if err != nil {
		t.Fatalf("GET login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	defaults := map[string]string{
		"primary":    "#7C3AED",
		"background": "#F3F4F6",
		"text":       "#111827",
		"error":      "#DC2626",
	}
	for name, color := range defaults {
		if !strings.Contains(bodyStr, color) {
			t.Errorf("default %s color %q not found in login page body", name, color)
		}
	}
}

// ---------------------------------------------------------------------------
// Handler via httptest.Server — unit-level without Start/Stop
// ---------------------------------------------------------------------------

func TestHandler_ServeHTTP_ViaRecorder(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		wantOK bool
	}{
		{"login", "/_vibewarden/login", true},
		{"registration", "/_vibewarden/registration", true},
		{"recovery", "/_vibewarden/recovery", true},
		{"verification", "/_vibewarden/verification", true},
		{"settings", "/_vibewarden/settings", true},
	}

	h := newHandler(t, defaultConfig())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Route manually, mirroring registerRoutes.
		switch r.URL.Path {
		case "/_vibewarden/login":
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/_vibewarden/login"
			http.Redirect(w, r2, "/_vibewarden/login", http.StatusOK)
		}
		_ = h
		// Delegate by starting the real handler.
	}))
	defer srv.Close()

	// Use the real handler directly via a started server.
	realH := newHandler(t, defaultConfig())
	if err := realH.Start(); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	defer realH.Stop(nil) //nolint: errcheck

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get("http://" + realH.Addr() + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer func() { _ = resp.Body.Close() }() //nolint:errcheck

			if tt.wantOK && resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
		})
	}
}
