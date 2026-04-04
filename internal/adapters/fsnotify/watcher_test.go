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

func TestWatcher_DetectsWriteEvent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "vibewarden.yaml")

	if err := os.WriteFile(cfgPath, []byte("initial: true\n"), 0644); err != nil {
		t.Fatalf("creating config file: %v", err)
	}

	watcher := fsnotifyadapter.NewWatcher(slog.Default(), ports.WithDebounce(50*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := watcher.Watch(ctx, cfgPath)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Write a change to the file.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(cfgPath, []byte("updated: true\n"), 0644); err != nil {
		t.Fatalf("writing config update: %v", err)
	}

	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("channel was closed instead of receiving signal")
		}
		// Received signal as expected.
	case <-ctx.Done():
		t.Fatal("timeout: no signal received after file write")
	}
}

func TestWatcher_Debounce(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "vibewarden.yaml")

	if err := os.WriteFile(cfgPath, []byte("v: 1\n"), 0644); err != nil {
		t.Fatalf("creating config file: %v", err)
	}

	const debounce = 150 * time.Millisecond
	watcher := fsnotifyadapter.NewWatcher(slog.Default(), ports.WithDebounce(debounce))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := watcher.Watch(ctx, cfgPath)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Write multiple rapid changes.
	for i := range 5 {
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(cfgPath, []byte("v: "+string(rune('1'+i))+"\n"), 0644); err != nil {
			t.Fatalf("writing change %d: %v", i, err)
		}
	}

	// Wait for debounce window plus a small buffer.
	time.Sleep(debounce + 100*time.Millisecond)

	// Count signals received.
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
		t.Error("expected at least one signal, got none")
	}
	// Debouncing should collapse multiple writes into at most a few signals.
	// We allow up to 2 to account for timer races, but not 5.
	if count > 2 {
		t.Errorf("debounce failed: received %d signals for 5 rapid writes, want <= 2", count)
	}
}

func TestWatcher_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "vibewarden.yaml")

	if err := os.WriteFile(cfgPath, []byte("v: 1\n"), 0644); err != nil {
		t.Fatalf("creating config file: %v", err)
	}

	watcher := fsnotifyadapter.NewWatcher(slog.Default(), ports.WithDebounce(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := watcher.Watch(ctx, cfgPath)
	if err != nil {
		cancel()
		t.Fatalf("Watch: %v", err)
	}

	cancel()

	// Channel should close after context cancellation.
	// A spurious signal before close is acceptable; we drain until closed.
	select {
	case <-ch:
		// Signal or close received; fall through to drain loop below.
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: no activity on channel after context cancellation")
	}

	// Drain until closed.
	timeout := time.After(3 * time.Second)
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

func TestWatcher_NonExistentFile(t *testing.T) {
	watcher := fsnotifyadapter.NewWatcher(slog.Default())
	ctx := context.Background()

	_, err := watcher.Watch(ctx, "/does/not/exist/vibewarden.yaml")
	if err == nil {
		t.Fatal("expected error watching non-existent file, got nil")
	}
}
