# ADR-092: Caddy handler dependency injection via composition-root-populated services registry

**Date**: 2026-04-23
**Issue**: #1102
**Status**: Accepted

## Context

Eight Caddy HTTP handler modules under `internal/adapters/caddy/` construct
their own `EventLogger`, `AuditEventLogger`, `RateLimiter`, and `CircuitBreaker`
adapters directly inside `Provision()`, with hardcoded sinks — typically
`os.Stdout` for the event logger and **`io.Discard` for the audit logger**.

Affected handlers:

| Handler | Currently instantiates | Sink written to |
|---|---|---|
| `AdminAuthHandler` | `auditadapter.NewJSONWriter` | `io.Discard` |
| `MaintenanceHandler` | `logadapter.NewSlogEventLogger` | `os.Stdout` (duplicate) |
| `RateLimitHandler` | `logadapter.NewSlogEventLogger`, `auditadapter.NewJSONWriter`, `ratelimitadapter.NewDefaultMemoryFactory` | audit → `io.Discard` |
| `RetryHandler` | `logadapter.NewSlogEventLogger` | `os.Stdout` (duplicate) |
| `WebhookSignatureHandler` | `logadapter.NewSlogEventLogger` | `os.Stdout` (duplicate) |
| `TimeoutHandler` | `logadapter.NewSlogEventLogger` | `os.Stdout` (duplicate) |
| `CircuitBreakerHandler` | `logadapter.NewSlogEventLogger`, `auditadapter.NewJSONWriter`, `resilienceadapter.NewInMemoryCircuitBreaker` | audit → `io.Discard` |
| `IPFilterHandler` | `logadapter.NewSlogEventLogger`, `auditadapter.NewJSONWriter` | audit → `io.Discard` |

Two problems result:

1. **Correctness bug.** Audit events emitted by admin-auth, rate-limit,
   circuit-breaker, and IP-filter handlers are written to `io.Discard` — they
   *never* reach the operator's audit sink. Rate-limit and circuit-breaker
   state transitions silently vanish.
2. **Architectural violation.** The handlers act as their own composition
   roots, importing peer adapter packages (`adapters/audit`, `adapters/log`,
   `adapters/ratelimit`, `adapters/resilience`). This violates ADR-067
   (composition root lives in `cmd/`, not in adapters) and breaks the
   testability promise: tests cannot swap the sinks.

The composition root in `cmd/vibewarden/wiring_serve.go` already builds an
`EventLogger` (stdout + OTel + ring buffer). It does **not** yet build an
`AuditEventLogger` for the sidecar-level audit trail (only the user-management
plugin builds a separate `AuditLogger` for DB-backed admin actions). The
`RateLimiterFactory` and circuit-breaker construction are only done today
inside the flawed handler `Provision` calls.

All four required port interfaces already exist:

- `ports.EventLogger` — `internal/ports/logger.go`
- `ports.AuditEventLogger` — `internal/ports/audit_event_logger.go`
- `ports.RateLimiter` + `ports.RateLimiterFactory` — `internal/ports/ratelimit.go`
- `ports.CircuitBreaker` — `internal/ports/circuit_breaker.go`

One thin factory port must be introduced so the handler no longer imports
`adapters/resilience` just to construct a circuit breaker from the Caddy JSON
threshold/timeout. This is minimal — it mirrors the existing
`ports.RateLimiterFactory` pattern one-to-one.

## Decision

Introduce a composition-root-populated **runtime services registry** scoped to
`internal/adapters/caddy/`. The composition root (`cmd/vibewarden/`) wires the
concrete adapters once at startup and publishes them through a package-private
`atomic.Pointer` using an exported setter. Each handler exposes a
`ProvisionWith(ctx, services)` helper that accepts the services explicitly —
the default `Provision(ctx)` implementation reads from the atomic pointer and
forwards to `ProvisionWith`. Tests call `ProvisionWith` directly with mocks,
never touching the registry.

We chose this pattern over the two alternatives considered:

- **Caddy App module (`ctx.App("vibewarden")`)** — idiomatic Caddy, but
  requires making the services JSON-visible (they aren't) or still needing a
  side-channel holder. Net: more code, same moving parts.
- **Re-register handlers per-start with closures** — `caddy.RegisterModule`
  panics on double-register and must run in `init()`. Not viable for a
  DI-per-Start model.

The registry is set **once** before `caddy.Load` runs for the first time, and
is safe to re-set on reload. It is not a Go package global in the harmful
sense — it is narrowly-scoped DI infrastructure for the Caddy module system,
mirroring how Caddy itself tracks modules in global tables. The explicit
`ProvisionWith` helper ensures the registry is never consulted from tests.

### Domain model changes

None.

### Ports (interfaces)

Add exactly one new factory port:

```go
// internal/ports/circuit_breaker.go (appended)

// CircuitBreakerFactory creates CircuitBreaker instances from a
// configuration. Implementations wire logger, event logger, and metrics once
// at construction time; NewCircuitBreaker captures only the per-instance
// threshold and timeout.
type CircuitBreakerFactory interface {
    // NewCircuitBreaker returns a fresh CircuitBreaker configured with the
    // supplied threshold and timeout. The returned breaker honours the
    // factory's pre-wired logger, event logger, and metrics sinks.
    NewCircuitBreaker(cfg CircuitBreakerConfig) (CircuitBreaker, error)
}
```

No other new ports. All four other interfaces already exist.

### Adapters

- New factory in `internal/adapters/resilience/circuit_breaker_factory.go`
  implementing `ports.CircuitBreakerFactory`:

  ```go
  type InMemoryCircuitBreakerFactory struct {
      logger       *slog.Logger
      eventLogger  ports.EventLogger
      metrics      ports.MetricsCollectorWithCircuitBreaker
      auditLogger  ports.AuditEventLogger
  }

  func NewInMemoryCircuitBreakerFactory(
      logger *slog.Logger,
      eventLogger ports.EventLogger,
      metrics ports.MetricsCollectorWithCircuitBreaker,
      auditLogger ports.AuditEventLogger,
  ) *InMemoryCircuitBreakerFactory

  func (f *InMemoryCircuitBreakerFactory) NewCircuitBreaker(cfg ports.CircuitBreakerConfig) (ports.CircuitBreaker, error)
  ```

  `NewCircuitBreaker` calls the existing `NewInMemoryCircuitBreaker(...)`,
  attaches the audit logger via `.WithAuditLogger(...)`, and returns the
  `ports.CircuitBreaker`.

- **No other adapter additions.** `logadapter.NewSlogEventLogger`,
  `auditadapter.NewJSONWriter`, and `ratelimitadapter.NewDefaultMemoryFactory`
  are unchanged.

- New file `internal/adapters/caddy/runtime_services.go` holding the
  unexported registry and exported setter. Details under **File layout**.

### Composition root (cmd/vibewarden)

`cmd/vibewarden/wiring_serve.go` is amended to:

1. Build one `ports.AuditEventLogger` for the sidecar-level audit trail
   (rate-limit, circuit-breaker, admin-auth, IP-filter events). Initial
   implementation: `auditadapter.NewJSONWriter(os.Stdout)`. Extension to
   OTel/multi-writer is a later, orthogonal task.
2. Build one `ports.RateLimiterFactory` via
   `ratelimitadapter.NewDefaultMemoryFactory()`.
3. Build one `ports.CircuitBreakerFactory` via
   `resilienceadapter.NewInMemoryCircuitBreakerFactory(logger, eventLogger, collector, auditLogger)`
   — collector comes from the metrics plugin when present, nil otherwise.
4. Call `caddyadapter.SetRuntimeServices(caddyadapter.RuntimeServices{
   EventLogger: eventLogger, AuditEventLogger: auditLogger,
   RateLimiterFactory: rlFactory, CircuitBreakerFactory: cbFactory,
   Logger: logger})` **before** `adapter.Start(ctx)`.
5. Re-call `SetRuntimeServices` from the reload path is not required —
   services are stable across reloads — but the setter is idempotent and
   concurrency-safe so callers can refresh if they need to.

### File layout

New files:
- `internal/adapters/caddy/runtime_services.go` — runtime services struct, atomic pointer, `SetRuntimeServices`, `currentServices()` accessor.
- `internal/adapters/caddy/runtime_services_test.go` — tests for setter idempotency + concurrency.
- `internal/adapters/resilience/circuit_breaker_factory.go` — `InMemoryCircuitBreakerFactory` implementing `ports.CircuitBreakerFactory`.
- `internal/adapters/resilience/circuit_breaker_factory_test.go` — unit tests for the factory.

Modified files (existing, content-only changes — no new files):
- `internal/ports/circuit_breaker.go` — append `CircuitBreakerFactory` interface.
- `internal/adapters/caddy/admin_auth_handler.go`
- `internal/adapters/caddy/maintenance_handler.go`
- `internal/adapters/caddy/ratelimit_handler.go`
- `internal/adapters/caddy/retry_handler.go`
- `internal/adapters/caddy/webhook_signature_handler.go`
- `internal/adapters/caddy/timeout_handler.go`
- `internal/adapters/caddy/circuit_breaker_handler.go`
- `internal/adapters/caddy/ipfilter_handler.go`
- `cmd/vibewarden/wiring_serve.go`
- `cmd/vibewarden/wiring_serve_helpers.go` — add `buildAuditEventLogger`, `buildCircuitBreakerFactory` helpers (or inline if trivial).
- `test/architecture/ports_purity_test.go` — new test `TestCaddyAdapter_NoPeerAdapterImports` (see **Test strategy**).

Handler tests (update to call `ProvisionWith`):
- `internal/adapters/caddy/admin_auth_handler_test.go`
- `internal/adapters/caddy/maintenance_handler_test.go`
- `internal/adapters/caddy/ratelimit_handler_test.go`
- `internal/adapters/caddy/retry_handler_test.go`
- `internal/adapters/caddy/timeout_handler_test.go`
- `internal/adapters/caddy/circuit_breaker_handler_test.go`
- `internal/adapters/caddy/ipfilter_handler_test.go`
- `internal/adapters/caddy/ipfilter_audit_test.go`
- Add `internal/adapters/caddy/webhook_signature_handler_test.go` if one does not already exist.

### Runtime services struct

```go
// internal/adapters/caddy/runtime_services.go

// RuntimeServices bundles the live service dependencies required by
// VibeWarden's Caddy HTTP handler modules. The composition root
// (cmd/vibewarden) builds these once at startup and publishes them through
// SetRuntimeServices before caddy.Load runs for the first time. Handlers
// retrieve the current set during Provision.
//
// Individual fields may be nil — handlers must check and degrade gracefully
// (log a warning, skip the optional behaviour). This is the same contract
// already honoured by the existing middleware helpers.
type RuntimeServices struct {
    Logger                *slog.Logger
    EventLogger           ports.EventLogger
    AuditEventLogger      ports.AuditEventLogger
    RateLimiterFactory    ports.RateLimiterFactory
    CircuitBreakerFactory ports.CircuitBreakerFactory
}

// SetRuntimeServices publishes the services to the atomic registry.
// Safe for concurrent use. Calling this after Provision has run for a
// handler has no effect on already-provisioned handlers.
func SetRuntimeServices(s RuntimeServices)

// currentServices returns the most recently published services, or a
// zero-value RuntimeServices if SetRuntimeServices has never been called.
// Unexported — only the caddy adapter package may read the registry.
func currentServices() RuntimeServices
```

### Handler changes (canonical shape)

Every affected handler follows the same pattern. Example for `MaintenanceHandler`:

```go
// Provision reads services from the composition-root registry and forwards
// to ProvisionWith. Production code path.
func (h *MaintenanceHandler) Provision(ctx gocaddy.Context) error {
    return h.ProvisionWith(ctx, currentServices())
}

// ProvisionWith initialises the handler with explicit services. Tests call
// this directly with mock services; production calls it via Provision.
//
// When services.EventLogger is nil the handler falls back to a
// stderr-only slog logger so that early-boot misconfiguration remains
// observable.
func (h *MaintenanceHandler) ProvisionWith(_ gocaddy.Context, services RuntimeServices) error {
    h.logger = services.Logger
    if h.logger == nil {
        h.logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
    }
    h.eventLogger = services.EventLogger
    h.inner = middleware.MaintenanceMiddleware(
        middleware.MaintenanceConfig{
            Enabled: true,
            Message: h.Config.Message,
        },
        h.eventLogger,
        nil,
    )
    return nil
}
```

Per-handler service requirements:

| Handler | Uses from RuntimeServices |
|---|---|
| `AdminAuthHandler` | `AuditEventLogger`, `Logger` |
| `MaintenanceHandler` | `EventLogger`, `Logger` |
| `RateLimitHandler` | `RateLimiterFactory`, `EventLogger`, `AuditEventLogger`, `Logger` |
| `RetryHandler` | `EventLogger`, `Logger` |
| `WebhookSignatureHandler` | `EventLogger`, `Logger` |
| `TimeoutHandler` | `EventLogger`, `Logger` |
| `CircuitBreakerHandler` | `CircuitBreakerFactory`, `EventLogger`, `AuditEventLogger`, `Logger` |
| `IPFilterHandler` | `EventLogger`, `AuditEventLogger`, `Logger` |

`AuthHandler`, `BodySizeHandler`, `ContentTypeHandler`, `IdentityHeadersHandler`,
`JWTBearerHandler`, `MeHandler`, `WAFEngineHandler`, `ComposeHandler`, and the
other existing handlers under `internal/adapters/caddy/` are already clean
(they do not import peer adapter packages) and are out of scope for this ADR.

### Sequence

1. `cmd/vibewarden/main.go` → `runServe` loads config, builds logger,
   initialises plugins.
2. After `registry.InitAll` returns, `runServe` builds the
   `AuditEventLogger` (`auditadapter.NewJSONWriter(os.Stdout)`),
   `RateLimiterFactory` (`ratelimitadapter.NewDefaultMemoryFactory()`),
   and `CircuitBreakerFactory`
   (`resilienceadapter.NewInMemoryCircuitBreakerFactory(logger, eventLogger, collector, auditLogger)`).
3. `runServe` calls `caddyadapter.SetRuntimeServices(RuntimeServices{...})`.
4. `runServe` calls `svc.Run(ctx)` → `caddyadapter.Adapter.Start(ctx)` →
   `caddy.Load(cfgJSON, true)`.
5. Caddy unmarshals the JSON, instantiates each handler, and calls
   `h.Provision(ctx)`.
6. Each handler's `Provision` calls `ProvisionWith(ctx, currentServices())`
   which stamps the services onto the handler struct.
7. Requests flow through the provisioned handler chain. Events and audit
   events reach the sinks wired by `runServe`, not `io.Discard`.
8. On reload, `ReloadService` rebuilds `ProxyConfig` and re-calls
   `adapter.Reload(ctx)`. Services do not change; handlers are re-provisioned
   from the same registry.

### Error cases

- **`SetRuntimeServices` never called.** `currentServices()` returns a
  zero-value `RuntimeServices`. Handlers fall back to a stderr slog logger
  and skip the optional sinks. No panic. Operational surfaces: metrics
  counter `vibewarden_caddy_handler_missing_services_total` (a
  `Warn`-level slog line is sufficient for v1 — we do not add a dedicated
  metric in this ADR).
- **`SetRuntimeServices` called twice.** The atomic pointer swap is last
  write wins. Handlers provisioned before the second write keep the first
  set — this is intentional and matches Caddy's own module lifecycle. Both
  sets must be valid; the composition root is the only caller.
- **`RuntimeServices.CircuitBreakerFactory.NewCircuitBreaker` returns an
  error.** The CircuitBreakerHandler returns a wrapped error from
  `ProvisionWith`; Caddy aborts the config load, and `runServe` returns
  the error. Matches today's behaviour.
- **`RuntimeServices.AuditEventLogger` is nil.** Handlers emit a
  one-time `Warn` log entry via `services.Logger` and skip the audit
  event. Request processing continues.
- **Factory services nil in handlers that require them
  (`RateLimitHandler`, `CircuitBreakerHandler`).** `ProvisionWith` returns
  a wrapped error (`missing RateLimiterFactory in runtime services`).
  Caddy aborts the config load. This is a programmer error — it means
  the composition root failed to wire the dependency — so a loud failure
  is correct.

### Test strategy

Unit tests (update in-place):

- Every affected `*_handler_test.go` replaces `h.Provision(gocaddy.Context{})`
  with `h.ProvisionWith(gocaddy.Context{}, caddyadapter.RuntimeServices{...})`
  and passes an in-memory `ports.EventLogger` / `ports.AuditEventLogger` /
  `ports.RateLimiterFactory` / `ports.CircuitBreakerFactory` via
  package-local fakes.
- New assertion per affected handler: after a request that should emit an
  event, the fake sink contains **exactly one** record of the expected
  event type. This is the regression test — it would fail against the
  current `io.Discard` code path.

New tests:

- `internal/adapters/caddy/runtime_services_test.go`:
  - `TestRuntimeServices_ZeroValueWhenUnset` — `currentServices()` returns
    zero-value if `SetRuntimeServices` never called.
  - `TestRuntimeServices_LastWriteWins` — two concurrent `SetRuntimeServices`
    calls; final `currentServices()` equals the last value.
- `internal/adapters/resilience/circuit_breaker_factory_test.go`:
  - `TestInMemoryCircuitBreakerFactory_NewCircuitBreaker_WiresAuditLogger`
    — factory with an audit logger produces a CB that emits audit events
    on state transition.
  - `TestInMemoryCircuitBreakerFactory_NewCircuitBreaker_RejectsInvalidConfig`
    — `threshold=0` → error.
- `test/architecture/ports_purity_test.go`:
  - Add `TestCaddyAdapter_NoPeerAdapterImports`. Walk every non-test `.go`
    file under `internal/adapters/caddy/`; for each import starting with
    `github.com/vibewarden/vibewarden/internal/adapters/`, fail if the
    suffix is anything other than `caddy` (the package's own directory).
    This guards the architecture going forward.

Integration test:

- `internal/adapters/caddy/adapter_integration_test.go` (new `TestAdapter_CaddyHandlerEventsReachWiredSinks`):
  - Boot `caddyadapter.Adapter` with a minimal single-site ProxyConfig that
    activates rate limiting with a 0-requests-per-second rule.
  - Call `caddyadapter.SetRuntimeServices` with in-memory sinks.
  - Fire a request; assert the in-memory `EventLogger` recorded the
    `rate_limit.exceeded` event *and* the in-memory `AuditEventLogger`
    recorded the `audit.rate_limit.exceeded` event.

Ports purity test current state: `test/architecture/ports_purity_test.go`
catches `app → adapters` and `mcp → adapters|app`. It does **not** catch
`adapters/caddy → adapters/audit`. The new
`TestCaddyAdapter_NoPeerAdapterImports` test closes that gap and turns this
regression-prone pattern into a compile-time invariant.

### New dependencies

None.

## Consequences

**Positive**

- Audit and operational events emitted by all eight handlers now reach the
  operator's configured sinks. This is a user-facing correctness fix, not
  just a cleanup.
- `internal/adapters/caddy/` no longer imports peer adapter packages —
  restores the ADR-067 invariant for the Caddy adapter and makes it
  machine-enforceable via the new architecture test.
- `ProvisionWith` makes handlers trivially testable with mock services, no
  global state leakage into tests.
- Adding an observability sink later (e.g. OTel audit writer) is a one-line
  change in the composition root — no handler edits required.

**Trade-offs**

- Introduces one atomic package-scope variable in `internal/adapters/caddy/`.
  Strictly this is shared mutable state, but it is written only by the
  composition root and read only within the Caddy adapter; the blast radius
  matches Caddy's own module registry. The exported `ProvisionWith` method
  keeps the registry out of tests entirely.
- Adds one new port (`CircuitBreakerFactory`) and one new adapter
  (`InMemoryCircuitBreakerFactory`). Minimal — mirrors the
  pre-existing `RateLimiterFactory` pattern.
- Composition root grows by four lines (build audit logger, build CB
  factory, build RL factory, call `SetRuntimeServices`). All four are pure
  wiring — no new business logic.

**Future-proofing**

- When multi-site (ADR-068/069) lands fully, the composition root may need
  per-site services. The `RuntimeServices` struct is a value type, so a
  future `SetRuntimeServicesForSite(name, services)` variant is a pure
  extension. Out of scope for this ADR.
- The audit logger built by `runServe` is a single `JSONWriter` to stdout
  for v1. Fan-out to OTel / PostgreSQL / a ring buffer is a drop-in via
  `auditadapter.NewMultiWriter`.
