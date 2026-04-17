package reload_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/reload"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/site"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ------------------------------------------------------------------
// Test doubles for MultiSiteService
// ------------------------------------------------------------------

// fakeReloader tracks calls to the ReloadFunc.
type fakeReloader struct {
	err   error
	count int
}

func (f *fakeReloader) Reload(_ context.Context) error {
	f.count++
	return f.err
}

// writeSiteConfig writes a minimal valid vibewarden.yaml under sitesDir/name/.
func writeSiteConfig(t *testing.T, sitesDir, name, content string) string {
	t.Helper()
	siteDir := filepath.Join(sitesDir, name)
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		t.Fatalf("creating site dir %q: %v", name, err)
	}
	path := filepath.Join(siteDir, "vibewarden.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing site config %q: %v", name, err)
	}
	return path
}

const siteMinimalConfig = `
profile: dev
server:
  host: 127.0.0.1
  port: 8443
upstream:
  host: 127.0.0.1
  port: 3000
`

// ------------------------------------------------------------------
// Tests
// ------------------------------------------------------------------

func TestMultiSiteService_CreatedEvent_Success(t *testing.T) {
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	dir := t.TempDir()
	configPath := writeSiteConfig(t, dir, "app1", siteMinimalConfig)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventCreated,
		SiteName:   "app1",
		ConfigPath: configPath,
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// Verify site was added to registry.
	s, ok := reg.Get("app1")
	if !ok {
		t.Fatal("site app1 not found in registry after Created event")
	}
	if !s.IsHealthy() {
		t.Errorf("site status = %v, want healthy", s.Status())
	}

	// Verify reload was called.
	if reloader.count != 1 {
		t.Errorf("reload called %d times, want 1", reloader.count)
	}

	// Verify site.added event was emitted.
	added := eventLog.eventsOfType(events.EventTypeSiteAdded)
	if len(added) != 1 {
		t.Errorf("got %d site.added events, want 1", len(added))
	}
}

func TestMultiSiteService_CreatedEvent_LoadFailure(t *testing.T) {
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventCreated,
		SiteName:   "bad-app",
		ConfigPath: "/does/not/exist/vibewarden.yaml",
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// Site should be in error state, not healthy.
	s, ok := reg.Get("bad-app")
	if !ok {
		t.Fatal("expected error site in registry")
	}
	if s.IsHealthy() {
		t.Error("site should be in error state, not healthy")
	}

	// Reload should NOT have been called.
	if reloader.count != 0 {
		t.Errorf("reload called %d times, want 0 on load failure", reloader.count)
	}

	// Should emit site.load_failed event.
	failed := eventLog.eventsOfType(events.EventTypeSiteLoadFailed)
	if len(failed) != 1 {
		t.Errorf("got %d site.load_failed events, want 1", len(failed))
	}
}

func TestMultiSiteService_CreatedEvent_ReloadFailureRollback(t *testing.T) {
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{err: errors.New("caddy reload failed")}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	dir := t.TempDir()
	configPath := writeSiteConfig(t, dir, "app1", siteMinimalConfig)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventCreated,
		SiteName:   "app1",
		ConfigPath: configPath,
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// Site should have been rolled back (removed).
	_, ok := reg.Get("app1")
	if ok {
		t.Error("site app1 should have been removed after reload failure")
	}

	// Should emit site.load_failed event.
	failed := eventLog.eventsOfType(events.EventTypeSiteLoadFailed)
	if len(failed) != 1 {
		t.Errorf("got %d site.load_failed events, want 1", len(failed))
	}
}

func TestMultiSiteService_ModifiedEvent_Success(t *testing.T) {
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	dir := t.TempDir()
	configPath := writeSiteConfig(t, dir, "app1", siteMinimalConfig)

	// Pre-populate registry with existing site.
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	existing, _ := site.NewSite("app1", configPath, cfg)
	reg.Add(existing)

	// Now modify the config file.
	newConfig := `
profile: prod
server:
  host: 127.0.0.1
  port: 9999
upstream:
  host: 127.0.0.1
  port: 4000
`
	if err := os.WriteFile(configPath, []byte(newConfig), 0644); err != nil {
		t.Fatalf("writing updated config: %v", err)
	}

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventModified,
		SiteName:   "app1",
		ConfigPath: configPath,
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// Verify site was updated in registry.
	s, ok := reg.Get("app1")
	if !ok {
		t.Fatal("site app1 not found in registry after Modified event")
	}
	if !s.IsHealthy() {
		t.Errorf("site status = %v, want healthy", s.Status())
	}

	// Verify reload was called.
	if reloader.count != 1 {
		t.Errorf("reload called %d times, want 1", reloader.count)
	}

	// Verify site.updated event.
	updated := eventLog.eventsOfType(events.EventTypeSiteUpdated)
	if len(updated) != 1 {
		t.Errorf("got %d site.updated events, want 1", len(updated))
	}
}

func TestMultiSiteService_ModifiedEvent_ReloadFailureRollback(t *testing.T) {
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{err: errors.New("proxy error")}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	dir := t.TempDir()
	configPath := writeSiteConfig(t, dir, "app1", siteMinimalConfig)

	// Pre-populate registry with existing site.
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	original, _ := site.NewSite("app1", configPath, cfg)
	reg.Add(original)

	// Modify the config (valid, so config.Load succeeds; reloader will fail).
	newConfig := `
profile: prod
server:
  host: 127.0.0.1
  port: 9999
upstream:
  host: 127.0.0.1
  port: 4000
`
	if err := os.WriteFile(configPath, []byte(newConfig), 0644); err != nil {
		t.Fatalf("writing updated config: %v", err)
	}

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventModified,
		SiteName:   "app1",
		ConfigPath: configPath,
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// After rollback, the original site should still be in the registry.
	s, ok := reg.Get("app1")
	if !ok {
		t.Fatal("site app1 should still be in registry after rollback")
	}
	// The original had profile "dev".
	if s.Config().Profile != "dev" {
		t.Errorf("after rollback, profile = %q, want %q", s.Config().Profile, "dev")
	}
}

func TestMultiSiteService_RemovedEvent_Success(t *testing.T) {
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	// Pre-populate registry.
	existing, _ := site.NewSite("app1", "/srv/sites/app1/vibewarden.yaml", &config.Config{})
	reg.Add(existing)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventRemoved,
		SiteName:   "app1",
		ConfigPath: "/srv/sites/app1/vibewarden.yaml",
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// Site should be removed from registry.
	_, ok := reg.Get("app1")
	if ok {
		t.Error("site app1 should have been removed from registry")
	}

	// Reload should be called.
	if reloader.count != 1 {
		t.Errorf("reload called %d times, want 1", reloader.count)
	}

	// site.removed event emitted.
	removed := eventLog.eventsOfType(events.EventTypeSiteRemoved)
	if len(removed) != 1 {
		t.Errorf("got %d site.removed events, want 1", len(removed))
	}
}

func TestMultiSiteService_RemovedEvent_ReloadFailureRollback(t *testing.T) {
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{err: errors.New("proxy error")}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	// Pre-populate registry.
	existing, _ := site.NewSite("app1", "/srv/sites/app1/vibewarden.yaml", &config.Config{})
	reg.Add(existing)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventRemoved,
		SiteName:   "app1",
		ConfigPath: "/srv/sites/app1/vibewarden.yaml",
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// After rollback, the site should be restored.
	s, ok := reg.Get("app1")
	if !ok {
		t.Fatal("site app1 should be restored after reload rollback")
	}
	if !s.IsHealthy() {
		t.Errorf("restored site status = %v, want healthy", s.Status())
	}
}

func TestMultiSiteService_RemovedEvent_UnknownSite(t *testing.T) {
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventRemoved,
		SiteName:   "nonexistent",
		ConfigPath: "/srv/sites/nonexistent/vibewarden.yaml",
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// Reload should NOT be called for a site that was not in the registry.
	if reloader.count != 0 {
		t.Errorf("reload called %d times, want 0 for unknown site removal", reloader.count)
	}
}

func TestMultiSiteService_ErrorIsolation(t *testing.T) {
	// A broken site should not affect a healthy one.
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	// Add a healthy site first.
	dir := t.TempDir()
	configPath := writeSiteConfig(t, dir, "healthy-app", siteMinimalConfig)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	healthy, _ := site.NewSite("healthy-app", configPath, cfg)
	reg.Add(healthy)

	// Now send a Created event for a broken site.
	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventCreated,
		SiteName:   "broken-app",
		ConfigPath: "/does/not/exist/vibewarden.yaml",
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// Healthy site should still be healthy and in the registry.
	s, ok := reg.Get("healthy-app")
	if !ok {
		t.Fatal("healthy-app should still be in registry")
	}
	if !s.IsHealthy() {
		t.Errorf("healthy-app status = %v, want healthy", s.Status())
	}

	// Broken site should be in error state.
	broken, ok := reg.Get("broken-app")
	if !ok {
		t.Fatal("broken-app should be in registry as error site")
	}
	if broken.IsHealthy() {
		t.Error("broken-app should be in error state")
	}
}

func TestMultiSiteService_ContextCancellation(t *testing.T) {
	reg := site.NewRegistry()
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, nil, slog.Default(), reloader.Reload)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan ports.SiteEvent, 1)

	done := make(chan struct{})
	go func() {
		svc.Run(ctx, ch)
		close(done)
	}()

	// Cancel context to stop Run.
	cancel()

	select {
	case <-done:
		// Run returned as expected.
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: Run did not return after context cancellation")
	}
}

func TestMultiSiteService_NilEventLog(t *testing.T) {
	// Verify that nil eventLog does not panic.
	reg := site.NewRegistry()
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, nil, slog.Default(), reloader.Reload)

	dir := t.TempDir()
	configPath := writeSiteConfig(t, dir, "app1", siteMinimalConfig)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventCreated,
		SiteName:   "app1",
		ConfigPath: configPath,
	}
	close(ch)

	// Should not panic.
	svc.Run(context.Background(), ch)

	_, ok := reg.Get("app1")
	if !ok {
		t.Fatal("site app1 should be in registry")
	}
}

func TestMultiSiteService_DomainConflict_Rollback(t *testing.T) {
	reg := site.NewRegistry()
	eventLog := &fakeEventLogger{}
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	dir := t.TempDir()

	// Add first site with a domain.
	cfg1Content := `
profile: dev
server:
  host: 127.0.0.1
  port: 8443
upstream:
  host: 127.0.0.1
  port: 3000
tls:
  enabled: true
  domain: shared.example.com
`
	path1 := writeSiteConfig(t, dir, "app1", cfg1Content)
	cfg1, err := config.Load(path1)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	s1, _ := site.NewSite("app1", path1, cfg1)
	reg.Add(s1)

	// Try to add a second site with the same domain.
	path2 := writeSiteConfig(t, dir, "app2", cfg1Content)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventCreated,
		SiteName:   "app2",
		ConfigPath: path2,
	}
	close(ch)

	svc.Run(context.Background(), ch)

	// app2 should have been rolled back due to domain conflict.
	_, ok := reg.Get("app2")
	if ok {
		t.Error("app2 should not be in registry after domain conflict")
	}

	// app1 should still be healthy.
	s, ok := reg.Get("app1")
	if !ok {
		t.Fatal("app1 should still be in registry")
	}
	if !s.IsHealthy() {
		t.Errorf("app1 status = %v, want healthy", s.Status())
	}

	// Reload should NOT have been called (domain validation fails before reload).
	if reloader.count != 0 {
		t.Errorf("reload called %d times, want 0 (domain conflict should prevent reload)", reloader.count)
	}

	// site.load_failed should be emitted.
	failed := eventLog.eventsOfType(events.EventTypeSiteLoadFailed)
	if len(failed) != 1 {
		t.Errorf("got %d site.load_failed events, want 1", len(failed))
	}
}
