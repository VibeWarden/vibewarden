# ADR-068: Site Domain Model for Multi-App

**Date**: 2026-04-15
**Issue**: #870
**Status**: Accepted (implemented in PR #875)

### Context

VibeWarden needs to manage multiple applications behind a single sidecar
instance on the same VM. The multi-app architecture requires a domain
model that represents each application as an independent unit with its
own complete configuration, operational status, and TLS domain, plus a
global configuration for sidecar-wide settings. Error isolation is a
hard requirement: a broken `sites/app2/vibewarden.yaml` must not prevent
`app1` from serving.

### Decision

1. **Site entity** (`internal/domain/site/site.go`). Each `Site` has a
   DNS-safe name (1-63 chars, matching `ValidNamePattern`), a
   `configPath`, a full `*config.Config`, a `Status` (Healthy, Error,
   Degraded), and an optional error. Constructors: `NewSite` for healthy
   sites, `NewErrorSite` for sites that failed to load. Pointer
   receivers for `SetStatus`/`SetErr` to allow in-place mutation via
   registry.

2. **Pragmatic boundary violation.** `Site` holds a `*config.Config`
   from `internal/config` rather than duplicating ~500 lines of domain
   types. This is documented as an intentional trade-off: the config
   package is internal, not an external dependency.

3. **SiteRegistry aggregate** (`internal/domain/site/registry.go`).
   Thread-safe (sync.RWMutex) collection with upsert semantics on
   `Add`, `Remove`, `Get`, and sorted `All`/`HealthySites`/`ErrorSites`
   queries. Stores `*Site` (not value type) so pointer-receiver
   mutators work. Includes `ValidateDomains` to detect duplicate TLS
   domains across healthy sites.

4. **GlobalConfig value object** (`internal/domain/site/global_config.go`).
   Sidecar-wide settings: `AdminToken`, `ListenHost`, `ListenPort`,
   `LogLevel`, `ACMEEmail`. `DefaultGlobalConfig()` provides sensible
   defaults (0.0.0.0:443, info). `Validate()` checks IP, port range,
   log level, and email format.

5. **Config loading** (`internal/config/sites/`). `LoadGlobal(path)`
   reads `global.yaml` with defaults for missing file. `LoadSites(basePath)`
   scans subdirectories, loading each `vibewarden.yaml` via
   `config.Load`. Partial success: broken sites become error Sites;
   healthy sites load independently.

### Consequences

#### Positive

- Each site is independently loadable and isolatable.
- Registry provides thread-safe concurrent access for the watcher and
  serve paths.
- Domain validation catches duplicate TLS domains before they reach Caddy.
- Alphabetical ordering in `All()` makes output deterministic.

#### Negative

- `internal/domain/site` imports `internal/config`, violating the strict
  hexagonal rule that domain has zero external deps. Accepted as a
  pragmatic trade-off to avoid massive type duplication.
- `GlobalConfig` lives in the domain but has no domain behavior beyond
  validation; it is effectively a configuration value object.
