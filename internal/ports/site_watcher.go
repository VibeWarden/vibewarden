package ports

import "context"

// SiteEventKind identifies the type of filesystem change detected for a site.
type SiteEventKind int

const (
	// SiteEventCreated indicates a new site's vibewarden.yaml was detected.
	SiteEventCreated SiteEventKind = iota

	// SiteEventModified indicates an existing site's vibewarden.yaml was changed.
	SiteEventModified

	// SiteEventRemoved indicates a site's vibewarden.yaml was deleted.
	SiteEventRemoved
)

// String returns a human-readable name for the event kind.
func (k SiteEventKind) String() string {
	switch k {
	case SiteEventCreated:
		return "created"
	case SiteEventModified:
		return "modified"
	case SiteEventRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

// SiteEvent represents a filesystem change detected for a single site.
// The SiteName is derived from the directory name under sites/.
type SiteEvent struct {
	// Kind identifies the type of change.
	Kind SiteEventKind

	// SiteName is the directory name (DNS-safe) under the sites/ folder.
	SiteName string

	// ConfigPath is the absolute path to the site's vibewarden.yaml.
	ConfigPath string
}

// SiteWatcher watches the sites/ directory tree for per-site configuration
// changes. It emits typed SiteEvent values on a channel, one per detected
// change (after per-site debouncing).
//
// Implementations must:
//   - Watch for Create, Write, Remove, and Rename events on sites/*/vibewarden.yaml.
//   - Debounce rapid changes on a per-site basis.
//   - Close the returned channel when ctx is cancelled.
type SiteWatcher interface {
	// Watch begins watching the directory at sitesDir for per-site config
	// changes. The returned channel emits a SiteEvent for each detected
	// change. The channel is closed when ctx is cancelled or an
	// unrecoverable error occurs. Watch returns an error only if the
	// watcher cannot be initialised.
	Watch(ctx context.Context, sitesDir string) (<-chan SiteEvent, error)
}
