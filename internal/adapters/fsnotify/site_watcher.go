package fsnotify

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	defaultSiteDebounce = 500 * time.Millisecond
	siteConfigFilename  = "vibewarden.yaml"
)

// SiteWatcherOption configures the SiteWatcher adapter.
type SiteWatcherOption func(*siteWatcherOptions)

type siteWatcherOptions struct {
	debounce time.Duration
}

// WithSiteDebounce sets the per-site debounce duration.
// Default: 500ms.
func WithSiteDebounce(d time.Duration) SiteWatcherOption {
	return func(o *siteWatcherOptions) { o.debounce = d }
}

// SiteWatcher implements ports.SiteWatcher using fsnotify.
// It watches the sites/ directory tree for per-site vibewarden.yaml changes
// and emits typed SiteEvent values after per-site debouncing.
type SiteWatcher struct {
	debounce time.Duration
	logger   *slog.Logger
}

// NewSiteWatcher creates a SiteWatcher with the given logger and options.
func NewSiteWatcher(logger *slog.Logger, opts ...SiteWatcherOption) *SiteWatcher {
	o := &siteWatcherOptions{debounce: defaultSiteDebounce}
	for _, opt := range opts {
		opt(o)
	}
	return &SiteWatcher{
		debounce: o.debounce,
		logger:   logger,
	}
}

// Watch implements ports.SiteWatcher.Watch.
//
// It watches sitesDir for subdirectory-level changes to vibewarden.yaml files.
// Each detected change (after per-site debouncing) is sent as a SiteEvent on
// the returned channel. The channel is closed when ctx is cancelled.
func (sw *SiteWatcher) Watch(ctx context.Context, sitesDir string) (<-chan ports.SiteEvent, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}

	// Watch the parent directory for new subdirectory creation.
	if err := fw.Add(sitesDir); err != nil {
		_ = fw.Close()
		return nil, fmt.Errorf("watching sites directory %q: %w", sitesDir, err)
	}

	// Watch existing subdirectories.
	entries, err := os.ReadDir(sitesDir)
	if err != nil {
		_ = fw.Close()
		return nil, fmt.Errorf("reading sites directory %q: %w", sitesDir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subDir := filepath.Join(sitesDir, entry.Name())
		if addErr := fw.Add(subDir); addErr != nil {
			sw.logger.Warn("failed to watch site subdirectory",
				slog.String("path", subDir),
				slog.String("error", addErr.Error()),
			)
		}
	}

	ch := make(chan ports.SiteEvent, 16)
	done := make(chan struct{})

	go sw.watchLoop(ctx, fw, sitesDir, ch, done)

	return ch, nil
}

// watchLoop is the main goroutine that processes fsnotify events.
func (sw *SiteWatcher) watchLoop(
	ctx context.Context,
	fw *fsnotify.Watcher,
	sitesDir string,
	ch chan<- ports.SiteEvent,
	done chan struct{},
) {
	defer close(ch)
	defer func() {
		if closeErr := fw.Close(); closeErr != nil {
			sw.logger.Warn("closing site watcher", slog.String("error", closeErr.Error()))
		}
	}()

	var mu sync.Mutex
	timers := make(map[string]*time.Timer) // site name -> debounce timer

	cancelTimers := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, t := range timers {
			t.Stop()
		}
	}

	for {
		select {
		case <-ctx.Done():
			cancelTimers()
			close(done)
			return

		case event, ok := <-fw.Events:
			if !ok {
				sw.logger.Error("fsnotify events channel closed unexpectedly")
				cancelTimers()
				close(done)
				return
			}

			sw.handleFSEvent(ctx, fw, event, sitesDir, ch, done, &mu, timers)

		case watchErr, ok := <-fw.Errors:
			if !ok {
				sw.logger.Error("fsnotify errors channel closed unexpectedly")
				cancelTimers()
				close(done)
				return
			}
			sw.logger.Warn("site watcher fsnotify error", slog.String("error", watchErr.Error()))
		}
	}
}

// handleFSEvent processes a single fsnotify event and maps it to a SiteEvent.
func (sw *SiteWatcher) handleFSEvent(
	ctx context.Context,
	fw *fsnotify.Watcher,
	event fsnotify.Event,
	sitesDir string,
	ch chan<- ports.SiteEvent,
	done chan struct{},
	mu *sync.Mutex,
	timers map[string]*time.Timer,
) {
	// Determine if this event is for a vibewarden.yaml in a site subdirectory.
	siteName, configPath, ok := sw.parseSitePath(event.Name, sitesDir)
	if !ok {
		// Might be a new subdirectory being created. If so, start watching it.
		if event.Has(fsnotify.Create) {
			sw.maybeWatchNewDir(fw, event.Name, sitesDir)
		}
		return
	}

	var kind ports.SiteEventKind

	switch {
	case event.Has(fsnotify.Create):
		kind = ports.SiteEventCreated
	case event.Has(fsnotify.Write):
		kind = ports.SiteEventModified
	case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
		kind = ports.SiteEventRemoved
	default:
		return
	}

	sw.logger.Debug("site config change detected",
		slog.String("site", siteName),
		slog.String("op", event.Op.String()),
		slog.String("path", event.Name),
	)

	siteEvent := ports.SiteEvent{
		Kind:       kind,
		SiteName:   siteName,
		ConfigPath: configPath,
	}

	sw.debounceSend(ctx, siteEvent, ch, done, mu, timers)
}

// debounceSend debounces SiteEvent sends per site name.
func (sw *SiteWatcher) debounceSend(
	ctx context.Context,
	event ports.SiteEvent,
	ch chan<- ports.SiteEvent,
	done chan struct{},
	mu *sync.Mutex,
	timers map[string]*time.Timer,
) {
	mu.Lock()
	defer mu.Unlock()

	_ = ctx // used indirectly via done channel

	if existing, ok := timers[event.SiteName]; ok {
		existing.Stop()
	}

	timers[event.SiteName] = time.AfterFunc(sw.debounce, func() {
		select {
		case <-done:
			return
		case ch <- event:
		}
	})
}

// parseSitePath extracts the site name and config path from an fsnotify event path.
// It returns the site name, full config path, and true if the path matches
// the pattern sitesDir/<site-name>/vibewarden.yaml.
func (sw *SiteWatcher) parseSitePath(eventPath, sitesDir string) (string, string, bool) {
	// Normalise to absolute paths for reliable comparison.
	absEvent, err := filepath.Abs(eventPath)
	if err != nil {
		return "", "", false
	}
	absSites, err := filepath.Abs(sitesDir)
	if err != nil {
		return "", "", false
	}

	rel, err := filepath.Rel(absSites, absEvent)
	if err != nil {
		return "", "", false
	}

	// We expect exactly: <site-name>/vibewarden.yaml
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || parts[1] != siteConfigFilename {
		return "", "", false
	}

	siteName := parts[0]
	configPath := filepath.Join(absSites, siteName, siteConfigFilename)
	return siteName, configPath, true
}

// maybeWatchNewDir adds a newly created subdirectory to the watcher.
func (sw *SiteWatcher) maybeWatchNewDir(fw *fsnotify.Watcher, path, sitesDir string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return
	}
	absSites, err := filepath.Abs(sitesDir)
	if err != nil {
		return
	}

	rel, err := filepath.Rel(absSites, absPath)
	if err != nil {
		return
	}

	// Must be a direct child (no path separator).
	if strings.Contains(rel, string(filepath.Separator)) || rel == "." {
		return
	}

	info, err := os.Stat(absPath)
	if err != nil || !info.IsDir() {
		return
	}

	if addErr := fw.Add(absPath); addErr != nil {
		sw.logger.Warn("failed to watch new site directory",
			slog.String("path", absPath),
			slog.String("error", addErr.Error()),
		)
		return
	}

	sw.logger.Debug("watching new site directory", slog.String("path", absPath))
}
