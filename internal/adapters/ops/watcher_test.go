package ops_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
)

func TestFsnotifyWatcher_SendsEventOnWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "vibewarden.yaml")

	// Create the file first so the watcher can add it.
	if err := os.WriteFile(cfgPath, []byte("initial: true\n"), 0o600); err != nil {
		t.Fatalf("writing initial config: %v", err)
	}

	// Use a very short debounce so the test is fast.
	w := opsadapter.NewFsnotifyWatcherWithDebounce(10 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := w.Watch(ctx, cfgPath)
	if err != nil {
		t.Fatalf("Watch() error: %v", err)
	}

	// Write to the file to trigger a change event.
	if err := os.WriteFile(cfgPath, []byte("updated: true\n"), 0o600); err != nil {
		t.Fatalf("writing updated config: %v", err)
	}

	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before receiving event")
		}
		// Success — event received.
	case <-ctx.Done():
		t.Fatal("timed out waiting for file-change event")
	}
}

func TestFsnotifyWatcher_NoEventAfterCancel(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "vibewarden.yaml")

	if err := os.WriteFile(cfgPath, []byte("initial: true\n"), 0o600); err != nil {
		t.Fatalf("writing initial config: %v", err)
	}

	w := opsadapter.NewFsnotifyWatcherWithDebounce(10 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := w.Watch(ctx, cfgPath)
	if err != nil {
		t.Fatalf("Watch() error: %v", err)
	}

	// Cancel the context before any writes.
	cancel()

	// The channel should be closed shortly after cancel.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after context cancel, but received a value")
		}
		// Channel closed — correct.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel to close after cancel")
	}
}

func TestFsnotifyWatcher_ErrorOnMissingFile(t *testing.T) {
	w := opsadapter.NewFsnotifyWatcher()
	ctx := context.Background()

	_, err := w.Watch(ctx, "/nonexistent/path/vibewarden.yaml")
	if err == nil {
		t.Fatal("Watch() expected error for missing file, got nil")
	}
}

func TestFsnotifyWatcher_DebouncesRapidWrites(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "vibewarden.yaml")

	if err := os.WriteFile(cfgPath, []byte("v: 1\n"), 0o600); err != nil {
		t.Fatalf("writing initial config: %v", err)
	}

	// Use a 100 ms debounce window.
	w := opsadapter.NewFsnotifyWatcherWithDebounce(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := w.Watch(ctx, cfgPath)
	if err != nil {
		t.Fatalf("Watch() error: %v", err)
	}

	// Write three times in rapid succession — should produce at most one event.
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(cfgPath, []byte("v: 2\n"), 0o600); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Wait for the debounce to fire.
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for debounced event")
	}

	// There should be at most one more event queued; the channel capacity is 1
	// so any extra events are dropped.
	extraEvents := 0
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
			extraEvents++
		case <-time.After(200 * time.Millisecond):
			// No more events — done draining.
			goto done
		}
	}
done:
	// We may see 0 or 1 extra event depending on timing; the important thing is
	// we do not see more than 1 (the channel cap prevents flooding).
	if extraEvents > 1 {
		t.Errorf("expected at most 1 extra event after rapid writes, got %d", extraEvents)
	}
}
