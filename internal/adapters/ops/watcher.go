package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultDebounceDuration = 500 * time.Millisecond

// FsnotifyWatcher implements ports.ConfigWatcher using the fsnotify library.
// It debounces rapid successive file-write events before forwarding them to
// the caller.
type FsnotifyWatcher struct {
	debounce time.Duration
}

// NewFsnotifyWatcher creates a new FsnotifyWatcher with the default 500 ms
// debounce window.
func NewFsnotifyWatcher() *FsnotifyWatcher {
	return &FsnotifyWatcher{debounce: defaultDebounceDuration}
}

// NewFsnotifyWatcherWithDebounce creates a FsnotifyWatcher with a custom
// debounce duration.  Intended for tests where a shorter window speeds up
// execution.
func NewFsnotifyWatcherWithDebounce(d time.Duration) *FsnotifyWatcher {
	return &FsnotifyWatcher{debounce: d}
}

// Watch watches the file at path and sends on the returned channel each time a
// write or create event is debounced.  The channel is closed when ctx is
// cancelled.
func (w *FsnotifyWatcher) Watch(ctx context.Context, path string) (<-chan struct{}, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}
	if err := fw.Add(path); err != nil {
		fw.Close() //nolint:errcheck
		return nil, fmt.Errorf("watching %s: %w", path, err)
	}

	ch := make(chan struct{}, 1)

	go func() {
		defer fw.Close() //nolint:errcheck
		defer close(ch)

		var timer *time.Timer

		for {
			select {
			case <-ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return

			case event, ok := <-fw.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					// Reset the debounce timer on each new event.
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(w.debounce, func() {
						select {
						case ch <- struct{}{}:
						default:
							// A notification is already queued; drop the duplicate.
						}
					})
				}

			case _, ok := <-fw.Errors:
				if !ok {
					return
				}
				// Non-fatal watcher errors are silently ignored; the goroutine
				// continues watching.
			}
		}
	}()

	return ch, nil
}
