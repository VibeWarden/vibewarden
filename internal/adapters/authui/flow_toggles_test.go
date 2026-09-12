package authui_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/authui"
)

// boolPtr returns a pointer to b, for setting the optional flow toggles.
func boolPtr(b bool) *bool { return &b }

// TestHandler_LoginLinksFollowFlowToggles pins the fix for #1512: when a
// self-service flow is disabled the login page must not link to it, because
// following the link lands on a flow Kratos refuses to create.
func TestHandler_LoginLinksFollowFlowToggles(t *testing.T) {
	const (
		registrationLink = "/_vibewarden/registration"
		recoveryLink     = "/_vibewarden/recovery"
	)

	tests := []struct {
		name             string
		showRegistration *bool
		showRecovery     *bool
		wantRegistration bool
		wantRecovery     bool
	}{
		{
			name:             "unset shows both links",
			wantRegistration: true,
			wantRecovery:     true,
		},
		{
			name:             "explicitly enabled shows both links",
			showRegistration: boolPtr(true),
			showRecovery:     boolPtr(true),
			wantRegistration: true,
			wantRecovery:     true,
		},
		{
			name:             "registration disabled hides register link",
			showRegistration: boolPtr(false),
			wantRegistration: false,
			wantRecovery:     true,
		},
		{
			name:             "recovery disabled hides forgot-password link",
			showRecovery:     boolPtr(false),
			wantRegistration: true,
			wantRecovery:     false,
		},
		{
			name:             "both disabled hides both links",
			showRegistration: boolPtr(false),
			showRecovery:     boolPtr(false),
			wantRegistration: false,
			wantRecovery:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.ShowRegistration = tt.showRegistration
			cfg.ShowRecovery = tt.showRecovery

			body := getBody(t, startHandler(t, cfg), "/_vibewarden/login")

			if got := strings.Contains(body, registrationLink); got != tt.wantRegistration {
				t.Errorf("login page contains %q = %v, want %v", registrationLink, got, tt.wantRegistration)
			}
			if got := strings.Contains(body, recoveryLink); got != tt.wantRecovery {
				t.Errorf("login page contains %q = %v, want %v", recoveryLink, got, tt.wantRecovery)
			}
			// The page itself must still render regardless of the toggles.
			if !strings.Contains(body, "Log in") {
				t.Error("login page did not render")
			}
		})
	}
}

// TestHandler_DisabledFlowPagesReturn404 verifies the pages of disabled flows
// are not served at all, so a bookmarked or hand-typed URL fails fast instead
// of showing a form that Kratos rejects.
func TestHandler_DisabledFlowPagesReturn404(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		mutate     func(*authui.AuthUIConfig)
		wantStatus int
	}{
		{
			name:       "registration enabled",
			path:       "/_vibewarden/registration",
			mutate:     func(c *authui.AuthUIConfig) { c.ShowRegistration = boolPtr(true) },
			wantStatus: http.StatusOK,
		},
		{
			name:       "registration disabled",
			path:       "/_vibewarden/registration",
			mutate:     func(c *authui.AuthUIConfig) { c.ShowRegistration = boolPtr(false) },
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "recovery enabled",
			path:       "/_vibewarden/recovery",
			mutate:     func(c *authui.AuthUIConfig) { c.ShowRecovery = boolPtr(true) },
			wantStatus: http.StatusOK,
		},
		{
			name:       "recovery disabled",
			path:       "/_vibewarden/recovery",
			mutate:     func(c *authui.AuthUIConfig) { c.ShowRecovery = boolPtr(false) },
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "login is unaffected by disabled registration",
			path:       "/_vibewarden/login",
			mutate:     func(c *authui.AuthUIConfig) { c.ShowRegistration = boolPtr(false) },
			wantStatus: http.StatusOK,
		},
		{
			name:       "settings is unaffected by disabled recovery",
			path:       "/_vibewarden/settings",
			mutate:     func(c *authui.AuthUIConfig) { c.ShowRecovery = boolPtr(false) },
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			tt.mutate(&cfg)

			h := startHandler(t, cfg)
			resp, err := http.Get("http://" + h.Addr() + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer func() { _ = resp.Body.Close() }() //nolint:errcheck

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("GET %s status = %d, want %d", tt.path, resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
