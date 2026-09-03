package reload_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/reload"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/site"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// failingEventLogger always fails to persist an event. Reload must treat the
// event sink as best-effort: a broken log must never abort a reload or leave
// the registry in an inconsistent state.
type failingEventLogger struct {
	mu    sync.Mutex
	calls int
}

func (f *failingEventLogger) Log(_ context.Context, _ events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return errors.New("event sink unavailable")
}

func (f *failingEventLogger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestService_Reload_EventSinkFailure_DoesNotBreakReload covers the error
// branches of emitReloaded and emitReloadFailed.
func TestService_Reload_EventSinkFailure_DoesNotBreakReload(t *testing.T) {
	t.Run("successful reload", func(t *testing.T) {
		proxy := &fakeProxyServer{}
		eventLog := &failingEventLogger{}
		svc, _ := newTestService(t, proxy, eventLog)

		if err := svc.Reload(context.Background(), "admin_api"); err != nil {
			t.Fatalf("Reload: %v", err)
		}
		if proxy.reloadCount != 1 {
			t.Errorf("proxy.Reload called %d times, want 1", proxy.reloadCount)
		}
		if eventLog.count() != 1 {
			t.Errorf("event log called %d times, want 1", eventLog.count())
		}
	})

	t.Run("failed reload", func(t *testing.T) {
		proxy := &fakeProxyServer{reloadErr: errors.New("proxy is down")}
		eventLog := &failingEventLogger{}
		svc, _ := newTestService(t, proxy, eventLog)

		err := svc.Reload(context.Background(), "file_watcher")
		if err == nil {
			t.Fatal("Reload with a failing proxy: want error, got nil")
		}
		if eventLog.count() != 1 {
			t.Errorf("event log called %d times, want 1", eventLog.count())
		}
	})
}

// TestMultiSiteService_EventSinkFailure_DoesNotBreakRegistry covers the error
// branches of emitSiteAdded, emitSiteUpdated, emitSiteRemoved, and
// emitSiteLoadFailed: registry mutations must still land when the event sink
// rejects every write.
func TestMultiSiteService_EventSinkFailure_DoesNotBreakRegistry(t *testing.T) {
	dir := t.TempDir()
	configPath := writeSiteConfig(t, dir, "app1", siteMinimalConfig)
	brokenPath := writeSiteConfig(t, dir, "broken", "server:\n\tport: [nope\n")

	eventLog := &failingEventLogger{}
	reg := site.NewRegistry()
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, eventLog, slog.Default(), reloader.Reload)

	// created -> emitSiteAdded, load failure -> emitSiteLoadFailed.
	ch := make(chan ports.SiteEvent, 2)
	ch <- ports.SiteEvent{Kind: ports.SiteEventCreated, SiteName: "app1", ConfigPath: configPath}
	ch <- ports.SiteEvent{Kind: ports.SiteEventCreated, SiteName: "broken", ConfigPath: brokenPath}
	close(ch)
	svc.Run(context.Background(), ch)

	if _, ok := reg.Get("app1"); !ok {
		t.Fatal("site app1 was not added despite a healthy config")
	}
	broken, ok := reg.Get("broken")
	if !ok {
		t.Fatal("site broken was not recorded as an error site")
	}
	if broken.IsHealthy() {
		t.Errorf("broken site status = %v, want an error status", broken.Status())
	}

	// modified -> emitSiteUpdated.
	updatedConfig := `
profile: dev
server:
  host: 127.0.0.1
  port: 9999
upstream:
  host: 127.0.0.1
  port: 4000
`
	if err := os.WriteFile(configPath, []byte(updatedConfig), 0o600); err != nil {
		t.Fatalf("writing updated config: %v", err)
	}
	ch = make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{Kind: ports.SiteEventModified, SiteName: "app1", ConfigPath: configPath}
	close(ch)
	svc.Run(context.Background(), ch)

	s, ok := reg.Get("app1")
	if !ok {
		t.Fatal("site app1 disappeared after a Modified event")
	}
	if !s.IsHealthy() {
		t.Errorf("site app1 status = %v, want healthy", s.Status())
	}

	// removed -> emitSiteRemoved.
	ch = make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{Kind: ports.SiteEventRemoved, SiteName: "app1", ConfigPath: configPath}
	close(ch)
	svc.Run(context.Background(), ch)

	if _, ok := reg.Get("app1"); ok {
		t.Error("site app1 is still registered after a Removed event")
	}

	if eventLog.count() < 4 {
		t.Errorf("event log called %d times, want at least 4 (added, load_failed, updated, removed)", eventLog.count())
	}
}

// TestMultiSiteService_ModifiedEvent_UnknownSite_LoadFailure exercises
// handleModified for a site that is not yet in the registry and whose config
// cannot be loaded: the site must be recorded as errored and no reload must be
// attempted.
func TestMultiSiteService_ModifiedEvent_UnknownSite_LoadFailure(t *testing.T) {
	dir := t.TempDir()
	brokenPath := writeSiteConfig(t, dir, "ghost", "server:\n\tport: [nope\n")

	reg := site.NewRegistry()
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, &fakeEventLogger{}, slog.Default(), reloader.Reload)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{Kind: ports.SiteEventModified, SiteName: "ghost", ConfigPath: brokenPath}
	close(ch)
	svc.Run(context.Background(), ch)

	s, ok := reg.Get("ghost")
	if !ok {
		t.Fatal("site ghost was not recorded as an error site")
	}
	if s.IsHealthy() {
		t.Errorf("site ghost status = %v, want an error status", s.Status())
	}
	if reloader.count != 0 {
		t.Errorf("reload called %d times after a load failure, want 0", reloader.count)
	}
}

// TestMultiSiteService_CreatedEvent_LoadFailure_InvalidName covers
// markSiteError's NewErrorSite failure arm: when the config cannot be loaded
// *and* the site name is not DNS-safe, there is no valid entity to record, so
// the registry must be left untouched rather than holding a half-built site.
func TestMultiSiteService_CreatedEvent_LoadFailure_InvalidName(t *testing.T) {
	reg := site.NewRegistry()
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, &fakeEventLogger{}, slog.Default(), reloader.Reload)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{
		Kind:       ports.SiteEventCreated,
		SiteName:   "Not_DNS_Safe",
		ConfigPath: "/does/not/exist/vibewarden.yaml",
	}
	close(ch)
	svc.Run(context.Background(), ch)

	if _, ok := reg.Get("Not_DNS_Safe"); ok {
		t.Error("site with an invalid name was added to the registry")
	}
	if len(reg.All()) != 0 {
		t.Errorf("registry has %d sites, want 0", len(reg.All()))
	}
	if reloader.count != 0 {
		t.Errorf("reload called %d times after a load failure, want 0", reloader.count)
	}
}

// TestMultiSiteService_ModifiedEvent_UnknownSite_ReloadFailure covers
// rollbackSite's no-previous arm: handleModified adds a site that was not in
// the registry before, and when the proxy reload then fails the partial add
// must be undone by removing the site (there is no previous version to
// restore).
func TestMultiSiteService_ModifiedEvent_UnknownSite_ReloadFailure(t *testing.T) {
	dir := t.TempDir()
	configPath := writeSiteConfig(t, dir, "ghost", siteMinimalConfig)

	reg := site.NewRegistry()
	reloader := &fakeReloader{err: errors.New("caddy reload failed")}
	svc := reload.NewMultiSiteService(reg, &fakeEventLogger{}, slog.Default(), reloader.Reload)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{Kind: ports.SiteEventModified, SiteName: "ghost", ConfigPath: configPath}
	close(ch)
	svc.Run(context.Background(), ch)

	if reloader.count != 1 {
		t.Errorf("reload called %d times, want 1", reloader.count)
	}
	if _, ok := reg.Get("ghost"); ok {
		t.Error("site ghost is still registered after the reload failure; the partial add was not rolled back")
	}
	if len(reg.All()) != 0 {
		t.Errorf("registry has %d sites after rollback, want 0", len(reg.All()))
	}
}

// TestMultiSiteService_UnknownEventKind is the default arm of handleEvent: an
// event kind the service does not recognise must be ignored, not panic.
func TestMultiSiteService_UnknownEventKind(t *testing.T) {
	reg := site.NewRegistry()
	reloader := &fakeReloader{}
	svc := reload.NewMultiSiteService(reg, &fakeEventLogger{}, slog.Default(), reloader.Reload)

	ch := make(chan ports.SiteEvent, 1)
	ch <- ports.SiteEvent{Kind: ports.SiteEventKind(99), SiteName: "app1"}
	close(ch)
	svc.Run(context.Background(), ch)

	if reloader.count != 0 {
		t.Errorf("reload called %d times for an unknown event kind, want 0", reloader.count)
	}
	if len(reg.All()) != 0 {
		t.Errorf("registry has %d sites, want 0", len(reg.All()))
	}
}

// compile-time check that the fake still satisfies the port.
var _ ports.EventLogger = (*failingEventLogger)(nil)
