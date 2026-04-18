# ADR-062: Hot Config Reload — File Watcher and Admin API Endpoint

**Date**: 2026-04-03
**Issues**: #471, #472
**Status**: Accepted

### Context

Configuration changes currently require a full VibeWarden restart. This creates
unnecessary downtime and friction for operators who want to tune settings like
rate limits, security headers, or CORS rules. The hot-reload epic (#464) addresses
this by enabling config changes to take effect without restarting.

Two complementary mechanisms are needed:

1. **File watcher** (issue #471): Automatically detect changes to vibewarden.yaml
   using OS file notification APIs (inotify on Linux, FSEvents on macOS, ReadDirectoryChangesW
   on Windows). Debounce rapid changes, validate before applying, and fail safely.

2. **Admin API** (issue #472): Programmatic reload trigger and config inspection
   for CI/CD pipelines and debugging. Operators can `POST /_vibewarden/config/reload`
   after deploying new config, or `GET /_vibewarden/config` to inspect running state.

The Caddy adapter already implements `Reload(ctx)` using `caddy.Load()`, which
applies new config without dropping connections. The challenge is orchestrating
the reload across all components: validate config, rebuild ProxyConfig, reinit
plugins that support hot reload, and reload Caddy.

### Decision

Implement hot config reload with the following architecture:

#### Domain model changes

**New domain events** in `internal/domain/events/config.go`:

```go
// Event type constants
const (
    // EventTypeConfigReloaded is emitted when configuration is successfully
    // reloaded from disk and applied to all components.
    EventTypeConfigReloaded = "config.reloaded"

    // EventTypeConfigReloadFailed is emitted when configuration reload fails
    // due to validation errors or other issues. The old config remains active.
    EventTypeConfigReloadFailed = "config.reload_failed"
)

// ConfigReloadedParams holds parameters for the config.reloaded event.
type ConfigReloadedParams struct {
    // ConfigPath is the path to the configuration file that was reloaded.
    ConfigPath string

    // TriggerSource identifies what initiated the reload: "file_watcher" or "admin_api".
    TriggerSource string

    // DurationMS is how long the reload took in milliseconds.
    DurationMS int64
}

// ConfigReloadFailedParams holds parameters for the config.reload_failed event.
type ConfigReloadFailedParams struct {
    // ConfigPath is the path to the configuration file.
    ConfigPath string

    // TriggerSource identifies what initiated the reload attempt.
    TriggerSource string

    // Reason is a human-readable description of why the reload failed.
    Reason string

    // ValidationErrors is a list of specific validation errors, if applicable.
    ValidationErrors []string
}
```

**New value object** in `internal/domain/config/` for redacted config representation:

```go
// RedactedConfig is a JSON-serializable representation of the running config
// with sensitive fields masked. Used by the admin API GET /_vibewarden/config.
type RedactedConfig struct {
    // All config fields with sensitive values replaced by "[REDACTED]"
}
```

#### Ports (interfaces)

**New file** `internal/ports/reload.go`:

```go
package ports

import "context"

// ConfigReloader orchestrates configuration reload across all components.
// It validates the new config, rebuilds internal state, and applies changes
// to the proxy server without dropping active connections.
type ConfigReloader interface {
    // Reload reads configuration from disk, validates it, and applies changes.
    // Returns an error if validation fails or if the reload cannot be applied.
    // On error, the previous configuration remains active.
    //
    // source identifies the reload trigger ("file_watcher" or "admin_api") and
    // is included in structured log events.
    Reload(ctx context.Context, source string) error

    // CurrentConfig returns the currently active configuration.
    // Sensitive fields are redacted for safe external exposure.
    CurrentConfig() RedactedConfig
}

// ConfigWatcherOption configures the file watcher.
type ConfigWatcherOption func(*configWatcherOptions)

// configWatcherOptions holds watcher configuration.
type configWatcherOptions struct {
    debounce time.Duration
}

// WithDebounce sets the debounce duration for file change events.
// Default: 500ms.
func WithDebounce(d time.Duration) ConfigWatcherOption {
    return func(o *configWatcherOptions) { o.debounce = d }
}
```

The existing `ConfigWatcher` interface in `internal/ports/watcher.go` is sufficient:

```go
type ConfigWatcher interface {
    Watch(ctx context.Context, path string) (<-chan struct{}, error)
}
```

**Extend `internal/ports/admin.go`** with reload-specific types:

```go
// ReloadResult represents the outcome of a config reload operation.
type ReloadResult struct {
    // Success is true when the reload completed successfully.
    Success bool

    // Message is a human-readable status message.
    Message string

    // Errors contains validation error details when Success is false.
    Errors []string
}
```

#### Adapters

**1. File watcher adapter** — `internal/adapters/fsnotify/watcher.go`:

```go
package fsnotify

import (
    "context"
    "time"

    "github.com/fsnotify/fsnotify"
    "github.com/vibewarden/vibewarden/internal/ports"
)

// Watcher implements ports.ConfigWatcher using fsnotify.
// It debounces rapid file change events and signals on the returned channel.
type Watcher struct {
    debounce time.Duration
    logger   *slog.Logger
}

// NewWatcher creates a Watcher with the specified debounce duration.
// Default debounce is 500ms if not specified via options.
func NewWatcher(logger *slog.Logger, opts ...ports.ConfigWatcherOption) *Watcher

// Watch implements ports.ConfigWatcher.Watch.
// It watches the file at path and sends on the returned channel each time
// a write or create event is detected (after debouncing).
func (w *Watcher) Watch(ctx context.Context, path string) (<-chan struct{}, error)
```

Implementation notes:
- Uses `fsnotify.NewWatcher()` with `Add(path)` for the config file
- Listens for `fsnotify.Write` and `fsnotify.Create` events (covers atomic writes)
- Debounces using a timer reset pattern: on each event, reset a 500ms timer;
  only signal when the timer fires without being reset
- Closes the output channel when ctx is cancelled or on unrecoverable error
- Logs watcher errors at WARN level (they are transient on most systems)

**2. Admin HTTP handlers** — extend `internal/adapters/http/admin_handlers.go`:

Add two new handlers:

```go
// reloadConfig handles POST /_vibewarden/config/reload.
// It triggers a config reload from disk and returns the result.
func (h *AdminHandlers) reloadConfig(w http.ResponseWriter, r *http.Request)

// getConfig handles GET /_vibewarden/config.
// It returns the current running configuration with sensitive fields redacted.
func (h *AdminHandlers) getConfig(w http.ResponseWriter, r *http.Request)
```

Response formats:

```json
// POST /_vibewarden/config/reload — success (200)
{
    "success": true,
    "message": "Configuration reloaded successfully"
}

// POST /_vibewarden/config/reload — validation error (400)
{
    "success": false,
    "message": "Configuration validation failed",
    "errors": [
        "rate_limit.per_ip.requests_per_second must be positive",
        "tls.domain is required when provider is letsencrypt"
    ]
}

// POST /_vibewarden/config/reload — internal error (500)
{
    "success": false,
    "message": "Reload failed: unable to apply new Caddy configuration"
}

// GET /_vibewarden/config (200)
{
    "server": {
        "host": "127.0.0.1",
        "port": 8443
    },
    "admin": {
        "enabled": true,
        "token": "[REDACTED]"
    },
    "database": {
        "url": "[REDACTED]"
    },
    // ... rest of config with sensitive fields redacted
}
```

**3. Caddy adapter** — already supports `Reload(ctx)`, no changes needed.

#### Application service

**New file** `internal/app/reload/service.go`:

```go
package reload

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/vibewarden/vibewarden/internal/config"
    "github.com/vibewarden/vibewarden/internal/domain/events"
    "github.com/vibewarden/vibewarden/internal/ports"
)

// Service orchestrates configuration hot reload.
// It is the single source of truth for the currently active configuration
// and coordinates reload across all dependent components.
type Service struct {
    mu           sync.RWMutex
    configPath   string
    currentCfg   *config.Config
    proxyServer  ports.ProxyServer
    eventLogger  ports.EventLogger
    logger       *slog.Logger

    // rebuildProxyConfig is a function that rebuilds the ProxyConfig from
    // the current config.Config. Injected from serve.go to access registry.
    rebuildProxyConfig func(*config.Config) *ports.ProxyConfig
}

// NewService creates a reload service.
func NewService(
    configPath string,
    initialCfg *config.Config,
    proxyServer ports.ProxyServer,
    eventLogger ports.EventLogger,
    logger *slog.Logger,
    rebuildFn func(*config.Config) *ports.ProxyConfig,
) *Service

// Reload implements ports.ConfigReloader.Reload.
// Steps:
// 1. Load config from disk
// 2. Validate config
// 3. If invalid: emit config.reload_failed event, return error
// 4. Acquire write lock
// 5. Store new config as current
// 6. Rebuild ProxyConfig
// 7. Call proxyServer.Reload(ctx)
// 8. Release lock
// 9. Emit config.reloaded event
// 10. Return nil
func (s *Service) Reload(ctx context.Context, source string) error

// CurrentConfig implements ports.ConfigReloader.CurrentConfig.
// Returns a redacted copy of the current configuration.
func (s *Service) CurrentConfig() ports.RedactedConfig

// Config returns the current config.Config (unredacted, for internal use).
// The returned pointer should not be modified.
func (s *Service) Config() *config.Config
```

**Redaction logic** in `internal/config/redact.go`:

```go
package config

// sensitiveFieldPatterns are substrings that identify sensitive fields.
var sensitiveFieldPatterns = []string{
    "password", "secret", "key", "token", "credential", "dsn", "url",
}

// Redact returns a copy of cfg with sensitive fields replaced by "[REDACTED]".
// Field names containing any of the sensitive patterns (case-insensitive) are redacted.
func Redact(cfg *Config) RedactedConfig
```

#### File layout

New files to create:

| Path | Purpose |
|------|---------|
| `internal/domain/events/config.go` | Domain events: config.reloaded, config.reload_failed |
| `internal/ports/reload.go` | ConfigReloader interface and related types |
| `internal/adapters/fsnotify/watcher.go` | fsnotify-based file watcher adapter |
| `internal/adapters/fsnotify/watcher_test.go` | Unit tests for watcher |
| `internal/app/reload/service.go` | Reload orchestration service |
| `internal/app/reload/service_test.go` | Unit tests for reload service |
| `internal/config/redact.go` | Config redaction logic |
| `internal/config/redact_test.go` | Tests for redaction |

Files to modify:

| Path | Changes |
|------|---------|
| `internal/config/config.go` | Add `Watch` config section |
| `internal/adapters/http/admin_handlers.go` | Add reloadConfig, getConfig handlers |
| `internal/adapters/http/admin_handlers_test.go` | Tests for new handlers |
| `internal/app/serve/serve.go` | Wire file watcher and reload service |

#### Config schema changes

Add to `internal/config/config.go`:

```go
// WatchConfig holds settings for the config file watcher.
type WatchConfig struct {
    // Enabled toggles automatic config reload on file changes (default: true).
    Enabled bool `mapstructure:"enabled"`

    // Debounce is the duration to wait after the last file change before
    // triggering a reload, expressed as a Go duration string (e.g. "500ms").
    // Default: "500ms".
    Debounce string `mapstructure:"debounce"`
}
```

Add `Watch WatchConfig` field to the main `Config` struct.

Default values in `Load()`:
```go
v.SetDefault("watch.enabled", true)
v.SetDefault("watch.debounce", "500ms")
```

#### Sequence — File watcher reload flow

1. User edits vibewarden.yaml and saves
2. fsnotify receives Write event
3. Watcher debounces (resets 500ms timer on each event)
4. After 500ms of quiet, watcher sends signal on channel
5. serve.go goroutine receives signal
6. Calls `reloadService.Reload(ctx, "file_watcher")`
7. Reload service loads config from disk
8. Validates config via `config.Validate()`
9. **If invalid**: logs error, emits `config.reload_failed` event, returns
10. **If valid**: acquires mutex, updates currentCfg, rebuilds ProxyConfig
11. Calls `adapter.Reload(ctx)` which calls `caddy.Load()` with new config
12. Caddy applies new config atomically (in-flight requests complete with old handlers)
13. Emits `config.reloaded` event with duration
14. Returns nil

#### Sequence — Admin API reload flow

1. Operator sends `POST /_vibewarden/config/reload` with X-Admin-Key header
2. Caddy routes to internal admin server (after auth validation)
3. AdminHandlers.reloadConfig receives request
4. Calls `reloadService.Reload(ctx, "admin_api")`
5. (Steps 7-13 same as file watcher flow)
6. Handler returns JSON response with success/failure

#### Sequence — Admin API config inspection

1. Operator sends `GET /_vibewarden/config` with X-Admin-Key header
2. Caddy routes to internal admin server (after auth validation)
3. AdminHandlers.getConfig receives request
4. Calls `reloadService.CurrentConfig()`
5. Service returns redacted config
6. Handler marshals to JSON and returns 200

#### Error cases

| Error | Handling |
|-------|----------|
| Config file not found | Return error, emit `config.reload_failed`, keep old config |
| Config YAML parse error | Return error with line number, emit `config.reload_failed`, keep old config |
| Config validation error | Return error with validation details, emit `config.reload_failed`, keep old config |
| Caddy reload fails | Return error, emit `config.reload_failed`, keep old config (Caddy remains on previous config) |
| fsnotify watcher error | Log at WARN, continue watching (transient errors are common) |
| fsnotify fatal error | Log at ERROR, close watcher channel, file watching stops |

#### Test strategy

**Unit tests:**

| Component | Tests |
|-----------|-------|
| `fsnotify/watcher_test.go` | Debounce behavior, context cancellation, event filtering |
| `reload/service_test.go` | Successful reload, validation failure handling, concurrent reload safety |
| `config/redact_test.go` | All sensitive field patterns are redacted |
| `http/admin_handlers_test.go` | Reload success/failure responses, getConfig response format |
| `events/config_test.go` | Event construction and payload structure |

**Integration tests:**

| Test | Purpose |
|------|---------|
| `adapters/fsnotify/watcher_integration_test.go` | Real file system watching with temp files |
| `reload/service_integration_test.go` | Full reload cycle with mock ProxyServer |

**Manual testing checklist:**

- [ ] Edit vibewarden.yaml, verify reload after 500ms
- [ ] Make syntax error in YAML, verify error logged, old config retained
- [ ] Rapid edits (vim save, save again), verify only one reload
- [ ] POST /_vibewarden/config/reload, verify success response
- [ ] GET /_vibewarden/config, verify sensitive fields redacted
- [ ] Disable watcher (`watch.enabled: false`), verify no auto-reload

#### New dependencies

| Library | Version | License | Purpose |
|---------|---------|---------|---------|
| github.com/fsnotify/fsnotify | latest (v1.8.0+) | BSD-3-Clause | Cross-platform file system notifications |

License verification:
```
Copyright © 2012 The Go Authors. All rights reserved.
Copyright © fsnotify Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met: [BSD-3-Clause terms]
```

BSD-3-Clause is on the approved license list.

### Consequences

**Positive:**

- Zero-downtime config changes for rate limits, headers, CORS, and other tunable settings
- Programmatic reload via API enables CI/CD integration
- Config inspection aids debugging without server access
- Graceful degradation: invalid configs are rejected, not applied
- Cross-platform support via fsnotify abstraction

**Negative:**

- Not all config changes can be hot-reloaded (e.g., changing listen port requires restart)
- File watcher adds a background goroutine and file descriptor
- Complexity of coordinating reload across multiple components
- Race condition window between config read and apply (mitigated by mutex)

**Limitations (documented, not addressed in this ADR):**

Some configuration changes require a full restart:
- `server.host` / `server.port` changes
- `tls.provider` changes (letsencrypt vs self-signed vs external)
- Plugin enable/disable (plugins init at startup)
- Database connection settings

The reload service should detect these cases and return an informative error
message indicating that a restart is required.

---
