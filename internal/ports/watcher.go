package ports

import "context"

// ConfigWatcher watches a file for changes and notifies over a channel.
// Implementations must debounce rapid successive events before signalling.
type ConfigWatcher interface {
	// Watch watches the file at path and sends on the returned channel each
	// time a write or create event is detected.  The channel is closed when
	// ctx is cancelled or an unrecoverable error occurs.  Watch blocks until
	// the watcher is initialised and returns an error only on setup failure.
	Watch(ctx context.Context, path string) (<-chan struct{}, error)
}
