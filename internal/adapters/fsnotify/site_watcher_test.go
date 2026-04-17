package fsnotify_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	fsnotifyadapter "github.com/vibewarden/vibewarden/internal/adapters/fsnotify"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// helper creates a sites/ directory with optional subdirs, each containing a
// vibewarden.yaml with the given content.
func setupSitesDir(t *testing.T, sites map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range sites {
		siteDir := filepath.Join(dir, name)
		if err := os.MkdirAll(siteDir, 0755); err != nil {
			t.Fatalf("creating site dir %q: %v", name, err)
		}
		if content != "" {
			if err := os.WriteFile(filepath.Join(siteDir, "vibewarden.yaml"), []byte(content), 0644); err != nil {
				t.Fatalf("writing config for %q: %v", name, err)
			}
		}
	}
	return dir
}

func TestSiteWatcher_DetectsCreatedEvent(t *testing.T) {
	sitesDir := setupSitesDir(t, map[string]string{})

	watcher := fsnotifyadapter.NewSiteWatcher(slog.Default(), fsnotifyadapter.WithSiteDebounce(50*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := watcher.Watch(ctx, sitesDir)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Allow watcher to settle.
	time.Sleep(50 * time.Millisecond)

	// Create a new site directory with a config file.
	siteDir := filepath.Join(sitesDir, "new-app")
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		t.Fatalf("creating new site dir: %v", err)
	}
	// Small delay to let the watcher pick up the new directory.
	time.Sleep(100 * time.Millisecond)

	if err := os.WriteFile(filepath.Join(siteDir, "vibewarden.yaml"), []byte("profile: dev\n"), 0644); err != nil {
		t.Fatalf("writing new site config: %v", err)
	}

	select {
	case ev := <-ch:
		// On some OS/editor combos the watcher may report Write instead of Create
		// when the file appears in a newly watched directory.
		if ev.Kind != ports.SiteEventCreated && ev.Kind != ports.SiteEventModified {
			t.Errorf("Kind = %v, want SiteEventCreated or SiteEventModified", ev.Kind)
		}
		if ev.SiteName != "new-app" {
			t.Errorf("SiteName = %q, want %q", ev.SiteName, "new-app")
		}
	case <-ctx.Done():
		t.Fatal("timeout: no event received for new site creation")
	}
}

func TestSiteWatcher_DetectsModifiedEvent(t *testing.T) {
	sitesDir := setupSitesDir(t, map[string]string{
		"app1": "profile: dev\n",
	})

	watcher := fsnotifyadapter.NewSiteWatcher(slog.Default(), fsnotifyadapter.WithSiteDebounce(50*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := watcher.Watch(ctx, sitesDir)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Allow watcher to settle.
	time.Sleep(50 * time.Millisecond)

	// Modify the existing config.
	if err := os.WriteFile(filepath.Join(sitesDir, "app1", "vibewarden.yaml"), []byte("profile: prod\n"), 0644); err != nil {
		t.Fatalf("modifying config: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.Kind != ports.SiteEventModified && ev.Kind != ports.SiteEventCreated {
			// Some OS/editor combos emit Create instead of Write.
			t.Errorf("Kind = %v, want SiteEventModified or SiteEventCreated", ev.Kind)
		}
		if ev.SiteName != "app1" {
			t.Errorf("SiteName = %q, want %q", ev.SiteName, "app1")
		}
	case <-ctx.Done():
		t.Fatal("timeout: no event received for config modification")
	}
}

func TestSiteWatcher_DetectsRemovedEvent(t *testing.T) {
	sitesDir := setupSitesDir(t, map[string]string{
		"doomed": "profile: dev\n",
	})

	watcher := fsnotifyadapter.NewSiteWatcher(slog.Default(), fsnotifyadapter.WithSiteDebounce(50*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := watcher.Watch(ctx, sitesDir)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Allow watcher to settle.
	time.Sleep(50 * time.Millisecond)

	// Delete the config file.
	if err := os.Remove(filepath.Join(sitesDir, "doomed", "vibewarden.yaml")); err != nil {
		t.Fatalf("removing config: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.Kind != ports.SiteEventRemoved {
			t.Errorf("Kind = %v, want SiteEventRemoved", ev.Kind)
		}
		if ev.SiteName != "doomed" {
			t.Errorf("SiteName = %q, want %q", ev.SiteName, "doomed")
		}
	case <-ctx.Done():
		t.Fatal("timeout: no event received for config removal")
	}
}

func TestSiteWatcher_DebouncesRapidWrites(t *testing.T) {
	sitesDir := setupSitesDir(t, map[string]string{
		"app1": "v: 1\n",
	})

	const debounce = 200 * time.Millisecond
	watcher := fsnotifyadapter.NewSiteWatcher(slog.Default(), fsnotifyadapter.WithSiteDebounce(debounce))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := watcher.Watch(ctx, sitesDir)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Perform 5 rapid writes.
	for i := range 5 {
		time.Sleep(10 * time.Millisecond)
		content := []byte("v: " + string(rune('1'+i)) + "\n")
		if err := os.WriteFile(filepath.Join(sitesDir, "app1", "vibewarden.yaml"), content, 0644); err != nil {
			t.Fatalf("writing change %d: %v", i, err)
		}
	}

	// Wait for debounce window plus buffer.
	time.Sleep(debounce + 200*time.Millisecond)

	// Drain and count events.
	count := 0
drain:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break drain
			}
			count++
		default:
			break drain
		}
	}

	if count == 0 {
		t.Error("expected at least one event, got none")
	}
	if count > 2 {
		t.Errorf("debounce failed: received %d events for 5 rapid writes, want <= 2", count)
	}
}

func TestSiteWatcher_ContextCancellation(t *testing.T) {
	sitesDir := setupSitesDir(t, map[string]string{
		"app1": "v: 1\n",
	})

	watcher := fsnotifyadapter.NewSiteWatcher(slog.Default(), fsnotifyadapter.WithSiteDebounce(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := watcher.Watch(ctx, sitesDir)
	if err != nil {
		cancel()
		t.Fatalf("Watch: %v", err)
	}

	cancel()

	// Channel should close after cancellation.
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed as expected
			}
		case <-timeout:
			t.Fatal("timeout waiting for channel to close after context cancellation")
		}
	}
}

func TestSiteWatcher_NonExistentDirectory(t *testing.T) {
	watcher := fsnotifyadapter.NewSiteWatcher(slog.Default())
	ctx := context.Background()

	_, err := watcher.Watch(ctx, "/does/not/exist/sites")
	if err == nil {
		t.Fatal("expected error watching non-existent directory, got nil")
	}
}

func TestSiteWatcher_IgnoresNonConfigFiles(t *testing.T) {
	sitesDir := setupSitesDir(t, map[string]string{
		"app1": "profile: dev\n",
	})

	watcher := fsnotifyadapter.NewSiteWatcher(slog.Default(), fsnotifyadapter.WithSiteDebounce(50*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := watcher.Watch(ctx, sitesDir)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Write a non-config file in the site directory.
	if err := os.WriteFile(filepath.Join(sitesDir, "app1", "README.md"), []byte("# App1\n"), 0644); err != nil {
		t.Fatalf("writing non-config file: %v", err)
	}

	// Expect no events (wait a bit to verify silence).
	select {
	case ev := <-ch:
		t.Errorf("unexpected event received: %+v", ev)
	case <-time.After(500 * time.Millisecond):
		// No event received, as expected.
	}
}
