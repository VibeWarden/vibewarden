package plugins_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
)

func authFlowBoolPtr(b bool) *bool { return &b }

// TestRegisterBuiltinPlugins_AuthUIFlowTogglesReachRenderedPage is the
// regression test for #1512: auth.ui.show_registration / auth.ui.show_recovery
// must travel config → registry → plugin → internal UI server → HTML, so a
// closed-registration deployment never renders a link to a flow Kratos
// refuses to create.
func TestRegisterBuiltinPlugins_AuthUIFlowTogglesReachRenderedPage(t *testing.T) {
	cfg := kratosAuthConfig(config.AuthUIConfig{
		Mode:             "built-in",
		ShowRegistration: authFlowBoolPtr(false),
		ShowRecovery:     authFlowBoolPtr(false),
	})

	logger := discardLogger()
	registry := plugins.NewRegistry(logger)
	plugins.RegisterBuiltinPlugins(registry, cfg, stubEventLogger{}, logger)

	authPlugin := findAuthPlugin(registry)
	if authPlugin == nil {
		t.Fatal("auth plugin not registered")
	}
	if err := authPlugin.Init(context.Background()); err != nil {
		t.Fatalf("auth plugin Init() error: %v", err)
	}
	t.Cleanup(func() {
		if err := authPlugin.Stop(context.Background()); err != nil {
			t.Errorf("auth plugin Stop() error: %v", err)
		}
	})

	addr := authUIDialAddr(t, authPlugin)
	if addr == "" {
		t.Fatal("auth plugin contributes no built-in auth UI route")
	}

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	page := authUIPage(t, client, "http://"+addr+"/_vibewarden/login")

	for _, unwanted := range []string{"/_vibewarden/registration", "/_vibewarden/recovery"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("rendered login page links %q although the flow is disabled", unwanted)
		}
	}

	for _, path := range []string{"/_vibewarden/registration", "/_vibewarden/recovery"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+path, nil)
		if err != nil {
			t.Fatalf("building request for %s: %v", path, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close() //nolint:errcheck // response body close in test
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404 for a disabled flow", path, resp.StatusCode)
		}
	}
}

// authUIPage fetches url and returns the response body as a string.
func authUIPage(t *testing.T, client *http.Client, url string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close in test

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", url, err)
	}
	return string(body)
}
