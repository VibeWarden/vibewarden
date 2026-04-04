package reload_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/reload"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ------------------------------------------------------------------
// Test doubles
// ------------------------------------------------------------------

// fakeProxyServer records calls to Reload and Start/Stop.
type fakeProxyServer struct {
	mu          sync.Mutex
	reloadErr   error
	reloadCount int
	lastCfg     *ports.ProxyConfig
}

func (f *fakeProxyServer) Start(_ context.Context) error { return nil }
func (f *fakeProxyServer) Stop(_ context.Context) error  { return nil }
func (f *fakeProxyServer) UpdateConfig(cfg *ports.ProxyConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastCfg = cfg
}
func (f *fakeProxyServer) Reload(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloadCount++
	return f.reloadErr
}

// fakeEventLogger records emitted events.
type fakeEventLogger struct {
	mu     sync.Mutex
	events []events.Event
}

func (f *fakeEventLogger) Log(_ context.Context, ev events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return nil
}

func (f *fakeEventLogger) eventsOfType(t string) []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []events.Event
	for _, e := range f.events {
		if e.EventType == t {
			out = append(out, e)
		}
	}
	return out
}

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

// writeConfig writes a minimal valid vibewarden.yaml to a temp path.
func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

const minimalConfig = `
profile: dev
server:
  host: 127.0.0.1
  port: 8443
upstream:
  host: 127.0.0.1
  port: 3000
`

func newTestService(t *testing.T, proxy ports.ProxyServer, eventLog ports.EventLogger) (*reload.Service, string) {
	t.Helper()
	dir := t.TempDir()
	path := writeConfig(t, dir, minimalConfig)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}

	rebuildFn := func(c *config.Config) *ports.ProxyConfig {
		return &ports.ProxyConfig{
			ListenAddr:   "127.0.0.1:8443",
			UpstreamAddr: "127.0.0.1:3000",
		}
	}

	svc := reload.NewService(path, cfg, proxy, eventLog, slog.Default(), rebuildFn)
	return svc, path
}

// ------------------------------------------------------------------
// Tests
// ------------------------------------------------------------------

func TestService_Reload_Success(t *testing.T) {
	proxy := &fakeProxyServer{}
	eventLog := &fakeEventLogger{}

	svc, _ := newTestService(t, proxy, eventLog)

	if err := svc.Reload(context.Background(), "admin_api"); err != nil {
		t.Fatalf("Reload returned unexpected error: %v", err)
	}

	if proxy.reloadCount != 1 {
		t.Errorf("proxy.Reload called %d times, want 1", proxy.reloadCount)
	}

	// Expect a config.reloaded event.
	reloadedEvents := eventLog.eventsOfType(events.EventTypeConfigReloaded)
	if len(reloadedEvents) != 1 {
		t.Errorf("got %d config.reloaded events, want 1", len(reloadedEvents))
	}
	if reloadedEvents[0].Payload["trigger_source"] != "admin_api" {
		t.Errorf("trigger_source = %v, want admin_api", reloadedEvents[0].Payload["trigger_source"])
	}
}

func TestService_Reload_ProxyError(t *testing.T) {
	proxyErr := errors.New("caddy reload failed")
	proxy := &fakeProxyServer{reloadErr: proxyErr}
	eventLog := &fakeEventLogger{}

	svc, _ := newTestService(t, proxy, eventLog)

	err := svc.Reload(context.Background(), "file_watcher")
	if err == nil {
		t.Fatal("expected error from Reload, got nil")
	}
	if !errors.Is(err, proxyErr) {
		t.Errorf("error = %v, want to wrap %v", err, proxyErr)
	}

	// Expect a config.reload_failed event.
	failedEvents := eventLog.eventsOfType(events.EventTypeConfigReloadFailed)
	if len(failedEvents) != 1 {
		t.Errorf("got %d config.reload_failed events, want 1", len(failedEvents))
	}
	if failedEvents[0].Payload["trigger_source"] != "file_watcher" {
		t.Errorf("trigger_source = %v, want file_watcher", failedEvents[0].Payload["trigger_source"])
	}
}

func TestService_Reload_InvalidConfig(t *testing.T) {
	proxy := &fakeProxyServer{}
	eventLog := &fakeEventLogger{}

	dir := t.TempDir()
	path := writeConfig(t, dir, minimalConfig)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}

	rebuildFn := func(c *config.Config) *ports.ProxyConfig {
		return &ports.ProxyConfig{}
	}

	svc := reload.NewService(path, cfg, proxy, eventLog, slog.Default(), rebuildFn)

	// Corrupt the config file to trigger a parse error.
	if err := os.WriteFile(path, []byte(":\tinvalid yaml\t:\n"), 0644); err != nil {
		t.Fatalf("writing invalid config: %v", err)
	}

	reloadErr := svc.Reload(context.Background(), "admin_api")
	if reloadErr == nil {
		t.Fatal("expected error for invalid config, got nil")
	}

	// Proxy should NOT have been called.
	if proxy.reloadCount != 0 {
		t.Errorf("proxy.Reload called %d times, want 0 (invalid config should abort)", proxy.reloadCount)
	}

	// Expect config.reload_failed event.
	failedEvents := eventLog.eventsOfType(events.EventTypeConfigReloadFailed)
	if len(failedEvents) != 1 {
		t.Errorf("got %d config.reload_failed events, want 1", len(failedEvents))
	}
}

func TestService_CurrentConfig_ReturnsRedactedConfig(t *testing.T) {
	proxy := &fakeProxyServer{}
	eventLog := &fakeEventLogger{}

	dir := t.TempDir()
	path := writeConfig(t, dir, minimalConfig+`
admin:
  enabled: true
  token: "should-be-redacted"
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}

	rebuildFn := func(c *config.Config) *ports.ProxyConfig {
		return &ports.ProxyConfig{}
	}

	svc := reload.NewService(path, cfg, proxy, eventLog, slog.Default(), rebuildFn)

	redacted := svc.CurrentConfig()

	// Config struct fields are marshalled with Go field names (capitalised) since
	// there are no json struct tags.
	adminMap, ok := redacted["Admin"].(map[string]any)
	if !ok {
		t.Fatal("Admin field missing from CurrentConfig")
	}
	if adminMap["Token"] != "[REDACTED]" {
		t.Errorf("Admin.Token = %v, want [REDACTED]", adminMap["Token"])
	}
}

func TestService_Config_ReturnsCurrentConfig(t *testing.T) {
	proxy := &fakeProxyServer{}
	eventLog := &fakeEventLogger{}

	svc, _ := newTestService(t, proxy, eventLog)

	cfg := svc.Config()
	if cfg == nil {
		t.Fatal("Config() returned nil")
	}
	if cfg.Server.Port == 0 {
		t.Error("Config().Server.Port is 0, expected non-zero")
	}
}

func TestService_ConcurrentReloads(t *testing.T) {
	proxy := &fakeProxyServer{}
	eventLog := &fakeEventLogger{}

	svc, _ := newTestService(t, proxy, eventLog)

	const goroutines = 5
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = svc.Reload(context.Background(), "file_watcher")
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	if proxy.reloadCount != goroutines {
		t.Errorf("proxy.Reload called %d times, want %d", proxy.reloadCount, goroutines)
	}
}

func TestService_Reload_UpdatesCurrentConfig(t *testing.T) {
	proxy := &fakeProxyServer{}
	eventLog := &fakeEventLogger{}

	dir := t.TempDir()
	path := writeConfig(t, dir, minimalConfig)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}

	var latestCfg *config.Config
	var mu sync.Mutex
	rebuildFn := func(c *config.Config) *ports.ProxyConfig {
		mu.Lock()
		latestCfg = c
		mu.Unlock()
		return &ports.ProxyConfig{}
	}

	svc := reload.NewService(path, cfg, proxy, eventLog, slog.Default(), rebuildFn)

	// Update the config file with a new port.
	newContent := `
profile: dev
server:
  host: 127.0.0.1
  port: 9999
upstream:
  host: 127.0.0.1
  port: 3000
`
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		t.Fatalf("writing updated config: %v", err)
	}

	if err := svc.Reload(context.Background(), "admin_api"); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	mu.Lock()
	updatedCfg := latestCfg
	mu.Unlock()

	if updatedCfg == nil {
		t.Fatal("rebuildFn was not called")
	}
	if updatedCfg.Server.Port != 9999 {
		t.Errorf("after reload, server.port = %d, want 9999", updatedCfg.Server.Port)
	}

	// Ensure CurrentConfig also reflects the new config.
	_ = svc.Config()                 // just ensure no panic
	time.Sleep(5 * time.Millisecond) // let the mutex settle
}
