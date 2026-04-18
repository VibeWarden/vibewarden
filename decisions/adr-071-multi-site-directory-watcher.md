# ADR-071: Multi-Site Directory Watcher

**Date**: 2026-04-15
**Issue**: #873
**Status**: Accepted (implemented in PR #878)

### Context

After ADR-070, adding a new site requires restarting the sidecar. For
zero-downtime site management, the sidecar needs to detect filesystem
changes in the `sites/` directory tree and hot-reload the Caddy
configuration without dropping connections. This extends the single-file
watcher from ADR-062 to a multi-site directory-watching pattern with
per-site debouncing and rollback on failure.

### Decision

1. **SiteWatcher port** (`internal/ports/site_watcher.go`). Defines
   `SiteWatcher` interface with `Watch(ctx, sitesDir) (<-chan SiteEvent, error)`,
   `SiteEvent` struct (Kind, SiteName, ConfigPath), and `SiteEventKind`
   enum (Created, Modified, Removed). Implementations must watch
   `sites/*/vibewarden.yaml`, debounce per site, and close the channel
   on context cancellation.

2. **fsnotify adapter** (`internal/adapters/fsnotify/site_watcher.go`).
   Watches the `sites/` parent directory plus all existing subdirectories.
   Dynamically adds watches for newly created subdirectories.
   `parseSitePath` extracts the site name from paths matching
   `sites/<name>/vibewarden.yaml`. Per-site debounce via
   `time.AfterFunc` map (default 500ms, configurable via
   `WithSiteDebounce`).

3. **Domain events** (`internal/domain/events/site.go`). Four event
   types: `site.added`, `site.updated`, `site.removed`,
   `site.load_failed`. Each has a typed params struct and constructor.
   Events follow the existing schema (SchemaVersion, severity, category,
   AI summary).

4. **MultiSiteService** (`internal/app/reload/multisite_service.go`).
   Consumes `SiteEvent` values from the watcher channel. On Created:
   loads config, creates Site, adds to registry, validates domains,
   calls `reloadFn`. On Modified: loads new config, snapshots previous
   site for rollback, updates registry, validates, reloads. On Removed:
   snapshots, removes from registry, reloads. All operations include
   rollback on failure (domain validation or proxy reload error) and
   emit domain events.

5. **Error isolation.** A broken site config results in an error Site
   in the registry (visible in status output) but does not affect
   healthy sites. Proxy reload failures trigger rollback to the
   previous registry state.

### Consequences

#### Positive

- Zero-downtime site addition, update, and removal via filesystem
  changes.
- Per-site debouncing prevents reload storms from editors that write
  files in multiple steps.
- Rollback ensures a failed reload never leaves the proxy in an
  inconsistent state.
- Domain events provide AI-readable audit trail for all site lifecycle
  changes.
- No new dependencies: fsnotify v1.9.0 was already present from ADR-062.

#### Negative

- The watch loop runs as a background goroutine with its own
  concurrency model (timers map + mutex), adding complexity.
- fsnotify does not detect changes on network filesystems reliably;
  acceptable because the sidecar runs locally.
- Subdirectory watch adds are not atomic with the Create event; a
  config file written before the watch is added could be missed.
  Mitigated by the initial directory scan at startup.
