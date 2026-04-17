// Package fsnotify implements the ports.ConfigWatcher interface using the
// fsnotify library for cross-platform file system notifications.
package fsnotify

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/vibewarden/vibewarden/internal/ports"
)

const defaultDebounce = 500 * time.Millisecond

// Watcher implements ports.ConfigWatcher using fsnotify.
// It debounces rapid file change events and signals on the returned channel.
type Watcher struct {
	debounce time.Duration
	logger   *slog.Logger
}

// NewWatcher creates a Watcher with the given logger and optional configuration.
// If no WithDebounce option is supplied the debounce duration defaults to 500ms.
func NewWatcher(logger *slog.Logger, opts ...ports.ConfigWatcherOption) *Watcher {
	o := &ports.ConfigWatcherOptions{
		Debounce: defaultDebounce,
	}
	for _, opt := range opts {
		opt(o)
	}
	return &Watcher{
		debounce: o.Debounce,
		logger:   logger,
	}
}

// Watch implements ports.ConfigWatcher.Watch.
// It watches the file at path and sends on the returned channel each time a
// Write or Create event is detected, after debouncing rapid changes.
//
// The returned channel is closed when ctx is cancelled or an unrecoverable
// error occurs. Watch returns an error only if the watcher cannot be
// initialised (e.g. the path does not exist).
func (w *Watcher) Watch(ctx context.Context, path string) (<-chan struct{}, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}

	if err := fw.Add(path); err != nil {
		_ = fw.Close()
		return nil, fmt.Errorf("watching config file %q: %w", path, err)
	}

	ch := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		defer close(ch)
		defer func() {
			if closeErr := fw.Close(); closeErr != nil {
				w.logger.Warn("closing fsnotify watcher", slog.String("error", closeErr.Error()))
			}
		}()

		var timer *time.Timer

		for {
			select {
			case <-ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				close(done)
				return

			case event, ok := <-fw.Events:
				if !ok {
					w.logger.Error("fsnotify events channel closed unexpectedly")
					close(done)
					return
				}

				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					w.logger.Debug("config file change detected, starting debounce timer",
						slog.String("path", event.Name),
						slog.String("op", event.Op.String()),
					)

					if timer != nil {
						timer.Stop()
					}
					// AfterFunc fires in a separate goroutine after the debounce
					// window. The select races with the outer loop closing `done`:
					//   - <-done wins if the watcher shuts down before the timer fires.
					//   - ch <- wins if the watcher is still alive and ch has capacity.
					//   - default wins if ch is already full (duplicate signal dropped).
					// All three outcomes are safe; the only risk is a late send on ch
					// after done is closed, which the <-done case handles.
					timer = time.AfterFunc(w.debounce, func() {
						select {
						case <-done:
							// Watcher shut down; do not send.
						case ch <- struct{}{}:
						default:
							// A signal is already pending; drop the duplicate.
						}
					})
				}

			case watchErr, ok := <-fw.Errors:
				if !ok {
					w.logger.Error("fsnotify errors channel closed unexpectedly")
					close(done)
					return
				}
				// Watcher errors are transient on most systems (e.g. spurious
				// EINTR on Linux). Log at WARN and continue watching.
				w.logger.Warn("fsnotify watcher error", slog.String("error", watchErr.Error()))
			}
		}
	}()

	return ch, nil
}
