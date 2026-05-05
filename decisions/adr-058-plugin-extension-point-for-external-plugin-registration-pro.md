# ADR-058: Plugin extension point for external plugin registration (Pro)

**Date**: 2026-03-28
**Issue**: #575
**Status**: Accepted

**Note (2026-04-15):** Per ADR-067 the `RunServe` orchestration moved out of `internal/app/serve/` into `cmd/vibewarden/wiring_serve.go`. The `PluginRegistrar` extension contract recorded here is unchanged; only the composition-root location moved.

### Context

The VibeWarden Pro binary needs to register additional plugins alongside the OSS
catalog. Currently `registerPlugins()` in `cmd/vibewarden/serve_plugins.go` is an
unexported function that hardcodes all plugin registrations. There is no way for an
external package (the vibewarden-pro repo) to add plugins without forking.

The Pro repo will import the OSS module and compile a binary with additional plugins
registered. This requires the OSS registration to be composable and the serve
entrypoint to accept extension hooks.

### Decision

Introduce a `PluginRegistrar` function type and move the plugin registration logic
from `cmd/vibewarden/` into the `internal/plugins/` package. The serve command becomes
a thin wrapper that calls an exported `RunServe` function, which accepts optional
`PluginRegistrar` hooks for extension.

#### Domain model changes

None. This is a pure refactoring of serve-time wiring code. No entities, value
objects, or domain events are introduced or modified.

#### Ports (interfaces)

No new interfaces in `internal/ports/`. The `PluginRegistrar` type is a function
type, not an interface, and lives in `internal/plugins/` alongside the `Registry`.

#### Adapters

No new adapters. Existing adapters are unaffected.

#### Application service

Create a new package `internal/app/serve/` containing the serve orchestration logic.
This centralizes the startup sequence and makes it importable from both the OSS
`main.go` and the Pro `main.go`.

#### File layout

New files to create:

| Path | Purpose |
|------|---------|
| `internal/plugins/registrar.go` | Defines `PluginRegistrar` type |
| `internal/plugins/builtin.go` | Exports `RegisterBuiltinPlugins` function |
| `internal/plugins/builtin_helpers.go` | Plugin builder helpers (moved from `serve_config.go`) |
| `internal/app/serve/serve.go` | Exports `RunServe` function with extension hooks |
| `internal/app/serve/logger.go` | Logger construction helpers (moved from `serve.go`) |
| `internal/app/serve/config.go` | ProxyConfig construction helpers (moved from `serve_config.go`) |

Files to modify:

| Path | Change |
|------|--------|
| `cmd/vibewarden/serve.go` | Thin wrapper calling `serve.RunServe()` |

Files to delete:

| Path | Reason |
|------|--------|
| `cmd/vibewarden/serve_plugins.go` | Logic moved to `internal/plugins/builtin.go` |
| `cmd/vibewarden/serve_config.go` | Logic moved to `internal/app/serve/config.go` and `internal/plugins/builtin_helpers.go` |

#### Type definitions

```go
// internal/plugins/registrar.go

// PluginRegistrar is a function that registers plugins with a Registry.
// It is called during serve startup to allow external packages to contribute
// additional plugins.
type PluginRegistrar func(
    registry *Registry,
    cfg *config.Config,
    eventLogger ports.EventLogger,
    logger *slog.Logger,
)
```

```go
// internal/plugins/builtin.go

// RegisterBuiltinPlugins registers all OSS plugins with the registry based on
// the provided configuration. This function is called automatically by RunServe
// and should not be called directly unless composing a custom startup sequence.
func RegisterBuiltinPlugins(
    registry *Registry,
    cfg *config.Config,
    eventLogger ports.EventLogger,
    logger *slog.Logger,
)
```

```go
// internal/app/serve/serve.go

// Options configures the serve command behavior.
type Options struct {
    ConfigPath string
    Version    string
}

// RunServe loads config, builds the plugin registry, wires Caddy via plugin
// contributors, and runs until a shutdown signal is received.
//
// Additional plugins can be registered by passing PluginRegistrar functions.
// They are called after RegisterBuiltinPlugins, allowing Pro or custom plugins
// to be added to the registry.
func RunServe(ctx context.Context, opts Options, extraPlugins ...plugins.PluginRegistrar) error
```

#### Sequence

1. `cmd/vibewarden/main.go` calls `rootCmd.AddCommand(newServeCmd())`
2. User runs `vibewarden serve --config /path/to/config.yaml`
3. `newServeCmd().RunE` calls `serve.RunServe(ctx, opts)`
4. `RunServe`:
   a. Calls `config.Load(opts.ConfigPath)` to load configuration
   b. Calls `serve.buildLogger(cfg.Log)` to create the logger
   c. Calls `config.MigrateLegacyMetrics(cfg, logger)` for backward compat
   d. Creates initial event logger (stdout-only)
   e. Creates plugin registry via `plugins.NewRegistry(logger)`
   f. Calls `plugins.RegisterBuiltinPlugins(registry, cfg, eventLogger, logger)`
   g. Calls each `extraPlugins[i](registry, cfg, eventLogger, logger)` in order
   h. Sets up OS signal handling (SIGINT, SIGTERM)
   i. Calls `registry.InitAll(ctx)`
   j. Calls `serve.buildEventLogger(registry, logger)` to upgrade to OTel if enabled
   k. Calls `serve.wireTLSMetricsCollector(registry)` for cert expiry metrics
   l. Calls `registry.StartAll(ctx)`
   m. Defers `registry.StopAll(stopCtx)` for cleanup
   n. Calls `serve.buildProxyConfig(cfg, registry, opts.Version)` to construct ProxyConfig
   o. Creates Caddy adapter via `caddyadapter.NewAdapter(proxyCfg, logger, eventLogger)`
   p. Creates proxy service via `proxy.NewService(adapter, logger)`
   q. Calls `svc.Run(ctx)` and blocks until shutdown
5. Pro binary: `vibewarden-pro/main.go` calls `serve.RunServe(ctx, opts, proplugins.RegisterAll)`
   - `proplugins.RegisterAll` is a `PluginRegistrar` that registers Pro plugins

#### Error cases

| Error | Handling |
|-------|----------|
| Config load failure | Return wrapped error immediately |
| Critical plugin Init failure | Return wrapped error, no Start |
| Non-critical plugin Init failure | Log warning, mark degraded, continue |
| Critical plugin Start failure | Return wrapped error |
| Non-critical plugin Start failure | Log warning, mark degraded, continue |
| Proxy Run failure | Return wrapped error |
| Plugin Stop failure | Log error, continue stopping remaining plugins |

Unknown config sections: Viper's `Unmarshal` naturally ignores keys that do not
map to struct fields. When the OSS binary encounters config for Pro plugins (e.g.,
`pii_masking:`, `llm_cost:`), the values are silently ignored. No warning is logged
because the config loader has no knowledge of which plugins are compiled in.

The Pro binary will validate that required config sections are present for its
plugins during plugin Init (not at config load time). This keeps the config
loader simple and stateless.

#### Test strategy

**Unit tests:**

- `internal/plugins/builtin_test.go`:
  - Test `RegisterBuiltinPlugins` registers expected plugin count
  - Test plugin order matches expected priority order (verify via `registry.Plugins()`)
  - Use mock config to verify each plugin is conditionally registered based on enabled flags

- `internal/app/serve/serve_test.go`:
  - Test `RunServe` with mock registry and mock proxy server
  - Test that `extraPlugins` are called after builtin registration
  - Test that context cancellation triggers graceful shutdown
  - Test error propagation from config load, InitAll, StartAll, Run

**Integration tests:**

None required. This is a pure refactoring with no behavior change. Existing
integration tests in `cmd/vibewarden/serve_test.go` and `cmd/vibewarden/serve_config_test.go`
continue to exercise the same code paths (now via `RunServe`).

#### New dependencies

None. This refactoring uses only existing dependencies and stdlib.

### Consequences

**Positive:**

- Pro repo can compile a binary with additional plugins without forking OSS
- Clear extension point via `PluginRegistrar` function type
- `cmd/vibewarden/serve.go` becomes a thin entrypoint (< 30 lines)
- Plugin registration logic is testable in isolation
- `RunServe` is reusable for embedding VibeWarden in other Go programs

**Negative:**

- Moving code from `cmd/` to `internal/` increases the "internal API surface"
  that the Pro repo depends on. Changes to `RunServe` signature or
  `PluginRegistrar` type require coordinated releases.
- Three new files created, two deleted. Net increase of one file.

**Migration for Pro repo:**

The Pro repo currently does not exist. When it is created:

1. Import `github.com/vibewarden/vibewarden/internal/app/serve`
2. Import `github.com/vibewarden/vibewarden/internal/plugins`
3. Implement `proplugins.RegisterAll` as a `plugins.PluginRegistrar`
4. Call `serve.RunServe(ctx, opts, proplugins.RegisterAll)` from Pro's `main.go`

**Unknown config handling:**

No warning is logged for unknown config sections. This is intentional:

- Viper silently ignores unknown keys by design
- Adding a warning would require the config loader to know about all valid keys,
  which changes with each plugin
- Strict validation is problematic when users share a single config file between
  OSS and Pro deployments

If a user misconfigures a Pro-only section in an OSS deployment, the section is
simply ignored. When they upgrade to Pro, the section takes effect. This is the
expected behavior.

---
