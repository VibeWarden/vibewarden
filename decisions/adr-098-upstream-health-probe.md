# ADR-098: Upstream health probe — wire the cached background probe into `/_vibewarden/health`

**Date**: 2026-04-28
**Issue**: #1197
**Status**: Accepted

## Context

After the qr-code-blackhole demo deploy on v0.18.1, the sidecar served:

```json
{"status":"ok","version":"0.18.1","components":{"sidecar":"ok","upstream":"unknown"}}
```

while the upstream app was answering 200s on `/`, `/api/qr`, and `/health`.
Production health endpoints reporting `unknown` will trigger false on-call
pages. **CRITICAL.**

Investigation shows that the architectural pieces already exist but are not
wired together:

- `internal/domain/health/health.go` — `UpstreamHealth` entity with
  threshold-based hysteresis (Unknown → Healthy / Unhealthy).
- `internal/ports/upstream_health.go` — `UpstreamHealthChecker` outbound
  port with `Start`, `Stop`, `CurrentStatus`, `Snapshot`.
- `internal/adapters/health/checker.go` — `HTTPChecker` adapter implementing
  the port. Probes `http://<host>:<port><path>` on a ticker.
- `internal/middleware/health.go` — `HealthHandler` reads from a
  `ports.UpstreamHealthChecker` to populate `components.upstream`.
- `internal/config/upstream.go` — `UpstreamHealthConfig` (`enabled`, `path`,
  `interval`, `timeout`, `unhealthy_threshold`, `healthy_threshold`).

What is missing:

1. The Caddy `/_vibewarden/health` route is a hard-coded `static_response`
   with `"upstream":"unknown"` (`internal/adapters/caddy/config_build.go`
   `buildHealthRoute`, mirrored in `multisite_config.go`). The
   `middleware.HealthHandler` is **never** registered in production.
2. `cfg.Upstream.Health.Enabled` defaults to `false`. Even users who
   redeploy will not get probing.
3. No composition root constructs the `HTTPChecker`. `health.NewHTTPChecker`
   has zero callers in `cmd/`.

The fix is wiring + sensible defaults, not a new domain model. We follow
the same shape used by `tls_handshake_fallback` (port-driven resolver,
domain value object, cached state, background goroutine) and the same
DI mechanism used by every other Caddy handler since ADR-092
(composition-root-published `RuntimeServices`).

## Decision

Replace the static `/_vibewarden/health` Caddy route with a custom Caddy
HTTP handler module (`vibewarden_health`) that renders the cached probe
result. Construct the upstream `HTTPChecker` in the composition root,
publish it through `caddyadapter.RuntimeServices`, and start/stop it in
the same goroutine lifecycle as the other plugins. Default the probe ON.

### Domain model changes

#### New value object: `internal/domain/upstream/state.go`

`internal/domain/health/health.go` already models the upstream entity, but
its statuses (`unknown`, `healthy`, `unhealthy`) are an internal-state
vocabulary. The `_vibewarden/health` response surface is a **component**
view that needs:

- a uniform `Healthy()` predicate per component, and
- a worst-component aggregator over a heterogeneous map.

We mirror the `internal/domain/tls/state.go` pattern. `internal/domain/upstream/`
is a **new** sibling to `internal/domain/tls/` and `internal/domain/health/`:

```go
// internal/domain/upstream/state.go
package upstream

type Kind int

const (
    KindUnknown  Kind = iota // pre-first-probe, or probe disabled with no signal
    KindOk                    // probe is reporting healthy
    KindDegraded              // probe disabled by config (operator opted out — neutral)
    KindFailing               // probe is reporting unhealthy
)

type State struct {
    kind      Kind
    lastError string
}

func NewUnknown() State                  { return State{kind: KindUnknown} }
func NewOk() State                       { return State{kind: KindOk} }
func NewDegraded() State                 { return State{kind: KindDegraded} }
func NewFailing(lastError string) State  { return State{kind: KindFailing, lastError: lastError} }

func (s State) Kind() Kind         { return s.kind }
func (s State) LastError() string  { return s.lastError }

// Healthy reports whether this component should NOT degrade the outer status.
// Ok is healthy; Degraded (probe disabled) is neutral and counts as healthy
// for the outer aggregator (it is an explicit operator opt-out, not a fault).
// Unknown and Failing are not healthy.
func (s State) Healthy() bool {
    switch s.kind {
    case KindOk, KindDegraded:
        return true
    default:
        return false
    }
}

// String returns the lowercase token used in the JSON `components.upstream`
// field. Stable wire format: callers depend on these strings.
func (s State) String() string {
    switch s.kind {
    case KindOk:
        return "ok"
    case KindDegraded:
        return "degraded"
    case KindFailing:
        return "failing"
    default:
        return "unknown"
    }
}
```

The wire vocabulary on the `components.upstream` field changes from the
existing `healthy`/`unhealthy`/`unknown` triple to the new
`ok`/`degraded`/`failing`/`unknown` quadruple. This is a contract change
on a v0.x endpoint; documented under **Consequences**.

#### Domain-pure aggregator: `internal/domain/healthsummary/aggregate.go`

```go
// internal/domain/healthsummary/aggregate.go
package healthsummary

// ComponentState is the minimal contract the aggregator needs.
// Both upstream.State and tls.State satisfy it; future components will too.
type ComponentState interface {
    Healthy() bool
    String() string
}

// Status is the outer wire status: "ok" or "degraded".
//
// Worst-component rule: if any component is not Healthy(), the outer
// status is "degraded". Otherwise "ok".
//
// "unhealthy" (HTTP 503) is reserved for the sidecar itself failing,
// which is decided at the HTTP layer (the handler can never run if the
// sidecar is down). This package does not return "unhealthy".
type Status string

const (
    StatusOK       Status = "ok"
    StatusDegraded Status = "degraded"
)

// AggregateStatus returns the outer status given a map of named
// components. Order-independent. Empty map → StatusOK.
func AggregateStatus(components map[string]ComponentState) Status {
    for _, c := range components {
        if c == nil || !c.Healthy() {
            return StatusDegraded
        }
    }
    return StatusOK
}
```

The aggregator is domain-pure (no I/O, no time, no logging) and exhaustively
unit-tested. `internal/middleware/health.go` will be updated to delegate
to it.

### Ports (interfaces)

**No new ports.** `ports.UpstreamHealthChecker` already exists with the
correct shape (`Start`, `Stop`, `CurrentStatus`, `Snapshot`). We extend
its semantic so callers can map `domainheal.UpstreamStatus` to
`upstream.State` — the mapping lives in the application layer (see
**Application service**).

We add **one** field to `caddyadapter.RuntimeServices`:

```go
// internal/adapters/caddy/runtime_services.go (appended)

type RuntimeServices struct {
    // ... existing fields ...

    // UpstreamHealthChecker is the cached upstream probe. May be nil — the
    // health handler renders "upstream":"unknown" in that case.
    UpstreamHealthChecker ports.UpstreamHealthChecker

    // SidecarVersion is the running binary version, used to render the
    // "version" field in the health response.
    SidecarVersion string
}
```

The version field is added so the dynamic handler can render `version`
without a separate plumbing path.

### Adapters

#### Existing adapter: `internal/adapters/health/checker.go`

No code changes. It already implements the port correctly. We will run
its existing tests and add **one** integration assertion described
under **Test strategy**.

#### New Caddy module: `internal/adapters/caddy/health_handler.go`

A custom Caddy HTTP handler module registered as
`http.handlers.vibewarden_health`. Provision reads
`currentServices()` and stamps the `UpstreamHealthChecker` and
`SidecarVersion` onto the handler. ServeHTTP renders the cached snapshot.

```go
// internal/adapters/caddy/health_handler.go
package caddy

import (
    "encoding/json"
    "log/slog"
    "net/http"

    gocaddy "github.com/caddyserver/caddy/v2"
    "github.com/caddyserver/caddy/v2/modules/caddyhttp"

    domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
    "github.com/vibewarden/vibewarden/internal/domain/healthsummary"
    "github.com/vibewarden/vibewarden/internal/domain/upstream"
    "github.com/vibewarden/vibewarden/internal/ports"
)

func init() { gocaddy.RegisterModule(HealthHandler{}) }

// HealthHandlerConfig is the JSON config for the handler. Currently the
// handler reads everything it needs from RuntimeServices, so the config
// is empty. SiteName is set by multisite to scope the response body.
type HealthHandlerConfig struct {
    SiteName string `json:"site_name,omitempty"`
}

type HealthHandler struct {
    Config HealthHandlerConfig `json:"config,omitempty"`

    logger     *slog.Logger
    checker    ports.UpstreamHealthChecker
    version    string
    siteName   string
}

func (HealthHandler) CaddyModule() gocaddy.ModuleInfo {
    return gocaddy.ModuleInfo{
        ID:  "http.handlers.vibewarden_health",
        New: func() gocaddy.Module { return new(HealthHandler) },
    }
}

func (h *HealthHandler) Provision(ctx gocaddy.Context) error {
    return h.ProvisionWith(ctx, currentServices())
}

func (h *HealthHandler) ProvisionWith(_ gocaddy.Context, s RuntimeServices) error {
    h.logger = s.Logger
    if h.logger == nil {
        h.logger = slog.Default()
    }
    h.checker = s.UpstreamHealthChecker
    h.version = s.SidecarVersion
    h.siteName = h.Config.SiteName
    return nil
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request,
    _ caddyhttp.Handler) error {

    upstreamState := upstream.NewUnknown()
    if h.checker != nil {
        upstreamState = mapUpstreamStatus(h.checker.CurrentStatus(),
            h.checker.Snapshot().LastError)
    }

    components := map[string]healthsummary.ComponentState{
        "sidecar":  alwaysOk{},   // unexported helper in this file
        "upstream": upstreamState,
    }
    outer := healthsummary.AggregateStatus(components)

    body := struct {
        Status     string            `json:"status"`
        Version    string            `json:"version,omitempty"`
        Site       string            `json:"site,omitempty"`
        Components map[string]string `json:"components"`
    }{
        Status:  string(outer),
        Version: h.version,
        Site:    h.siteName,
        Components: map[string]string{
            "sidecar":  "ok",
            "upstream": upstreamState.String(),
        },
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK) // always 200 — sidecar is up; outer status is informational
    return json.NewEncoder(w).Encode(body)
}

// mapUpstreamStatus translates the probe's domain-internal status into the
// component-facing State. Unknown → Unknown; Healthy → Ok; Unhealthy → Failing.
func mapUpstreamStatus(s domainheal.UpstreamStatus, lastError string) upstream.State {
    switch s {
    case domainheal.StatusHealthy:
        return upstream.NewOk()
    case domainheal.StatusUnhealthy:
        return upstream.NewFailing(lastError)
    default:
        return upstream.NewUnknown()
    }
}
```

`alwaysOk` is a one-line internal helper that satisfies
`healthsummary.ComponentState` — the sidecar is by definition healthy if
this handler runs.

`HTTP 200` is unconditional. `503` is reserved for `/_vibewarden/ready`
(orchestrator probe) and for sidecar-itself failures. Returning 503 for
upstream-failing on the *health* endpoint would break the existing AC of
"sidecar is still up; outer status is informational, ops uses ready/
metrics for paging".

When probe is **disabled by config**, the composition root passes
`s.UpstreamHealthChecker = nil`; `upstreamState` stays `Unknown`. After
the boot warning (see below) the operator has acknowledged this.

### Application service

#### Probe configuration mapping: `internal/app/health/config.go` (new)

A small pure helper that translates `config.UpstreamHealthConfig` into the
adapter's `health.Config`, applying the new defaults and emitting a boot
warning when the config is malformed or disabled:

```go
// internal/app/health/config.go
package health

import (
    "log/slog"
    "time"

    "github.com/vibewarden/vibewarden/internal/config"
    domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
)

// BuildDomainConfig parses the runtime UpstreamHealthConfig into a
// domain Config. It applies defaults (5s interval, 2s timeout, 3/2
// thresholds, "/health" path) and emits a Warn log when the operator
// has disabled the probe or supplied a malformed value. Returns the
// resolved Config and a bool indicating whether the probe should run.
func BuildDomainConfig(cfg config.UpstreamHealthConfig, logger *slog.Logger) (domainheal.Config, bool) {
    if !cfg.Enabled {
        if logger != nil {
            logger.Warn("upstream health probe disabled — _vibewarden/health will report upstream:unknown",
                slog.String("hint", "set upstream.health.enabled: true (default in v0.18.2+)"),
            )
        }
        return domainheal.Config{}, false
    }

    path := cfg.Path
    if path == "" {
        path = "/health"
    }

    interval := mustDuration(cfg.Interval, 5*time.Second, "upstream.health.interval", logger)
    timeout := mustDuration(cfg.Timeout, 2*time.Second, "upstream.health.timeout", logger)

    unhealthy := cfg.UnhealthyThreshold
    if unhealthy <= 0 {
        unhealthy = 3
    }
    healthy := cfg.HealthyThreshold
    if healthy <= 0 {
        healthy = 2
    }

    return domainheal.Config{
        Enabled:            true,
        Path:               path,
        Interval:           interval,
        Timeout:            timeout,
        UnhealthyThreshold: unhealthy,
        HealthyThreshold:   healthy,
    }, true
}

// mustDuration parses a duration string with a default and a Warn-on-bad-value.
func mustDuration(raw string, def time.Duration, name string, logger *slog.Logger) time.Duration {
    if raw == "" { return def }
    d, err := time.ParseDuration(raw)
    if err != nil {
        if logger != nil {
            logger.Warn("invalid duration in config — using default",
                slog.String("field", name),
                slog.String("value", raw),
                slog.String("default", def.String()),
                slog.String("error", err.Error()),
            )
        }
        return def
    }
    return d
}
```

This keeps `cmd/vibewarden/wiring_serve.go` thin — it only calls
`BuildDomainConfig`, and if the probe is enabled, constructs the
`HTTPChecker` and starts it.

#### Composition root changes: `cmd/vibewarden/wiring_serve.go`

After `registry.StartAll(ctx)` completes and **before**
`caddyadapter.SetRuntimeServices(...)` is called:

1. Call `health.BuildDomainConfig(cfg.Upstream.Health, logger)`.
2. If it returned `enabled=true`, construct the checker:
   ```go
   checker, err := healthadapter.NewHTTPChecker(healthadapter.Config{
       UpstreamHost: cfg.Upstream.Host,
       UpstreamPort: cfg.Upstream.Port,
       DomainConfig: domainCfg,
   }, logger, eventLogger, cbMetrics) // metrics: existing collector if MetricsCollectorWithUpstreamHealth, else nil
   ```
3. `_ = checker.Start(ctx)` — non-blocking; spawns the goroutine.
4. `defer checker.Stop(stopCtx)` inside the existing `StopAll` defer
   block; honour the same 10-second budget.
5. Pass the checker as `RuntimeServices.UpstreamHealthChecker` into
   `buildRuntimeServices(...)`. Update the function signature to accept
   the checker (or nil) and the version string.

#### Caddy config builder changes

`internal/adapters/caddy/config_build.go::buildHealthRoute` and
`internal/adapters/caddy/multisite_config.go` (the per-site health route):

Replace the `static_response` handler with the new module:

```go
// buildHealthRoute (single-site)
func buildHealthRoute(_ string) map[string]any {
    return map[string]any{
        "match": []map[string]any{
            {"path": []string{"/_vibewarden/health"}},
        },
        "handle": []map[string]any{
            {"handler": "vibewarden_health"},
        },
    }
}
```

The `version` string parameter is no longer read inside the route
builder — the handler reads it from `RuntimeServices.SidecarVersion`.
Existing call sites are updated (the parameter is dropped from the
signature; this is a private helper).

For multisite:

```go
healthRoute := map[string]any{
    "match": []map[string]any{
        {"host": []string{domain}, "path": []string{"/_vibewarden/health"}},
    },
    "handle": []map[string]any{
        {"handler": "vibewarden_health", "config": map[string]any{"site_name": s.Name()}},
    },
}
```

The site name is plumbed via the JSON config so the handler renders the
existing `"site"` field.

### File layout

**New files:**

- `internal/domain/upstream/state.go` — `State` value object.
- `internal/domain/upstream/state_test.go` — table-driven tests.
- `internal/domain/healthsummary/aggregate.go` — domain-pure aggregator.
- `internal/domain/healthsummary/aggregate_test.go` — exhaustive table-driven tests.
- `internal/app/health/config.go` — `BuildDomainConfig` helper.
- `internal/app/health/config_test.go` — tests for defaults, parse errors, disabled-warning.
- `internal/adapters/caddy/health_handler.go` — Caddy `vibewarden_health` module.
- `internal/adapters/caddy/health_handler_test.go` — `ProvisionWith` + ServeHTTP tests.

**Modified files:**

- `internal/adapters/caddy/runtime_services.go` — add `UpstreamHealthChecker`, `SidecarVersion` fields.
- `internal/adapters/caddy/runtime_services_test.go` — extend with the new fields.
- `internal/adapters/caddy/config_build.go` — `buildHealthRoute` returns the new module config; drop the static_response body.
- `internal/adapters/caddy/config_build_test.go` — update the JSON shape assertion.
- `internal/adapters/caddy/multisite_config.go` — same change for the per-site health route.
- `internal/adapters/caddy/multisite_config_test.go` — update the JSON shape assertion.
- `internal/middleware/health.go` — internal state vocabulary in `ComponentStatus.Upstream` migrates from `healthy`/`unhealthy` to the new `ok`/`failing`/`unknown` (the middleware path is still used by tests / future internal HTTP server). The aggregation logic is rewritten to delegate to `healthsummary.AggregateStatus`.
- `internal/middleware/health_test.go`, `health_upstream_test.go`, `health_deps_test.go` — update the expected status strings.
- `internal/config/config.go` — change the default for `upstream.health.enabled` from `false` to `true`. Defaults for `interval` and `timeout` updated to `"5s"` and `"2s"`.
- `internal/config/upstream_test.go` (or `config_test.go`) — update the default-value assertions.
- `cmd/vibewarden/wiring_serve.go` — construct, start, and stop the checker; pass it into `buildRuntimeServices`.
- `cmd/vibewarden/wiring_serve_helpers.go` — extend `buildRuntimeServices` signature with the checker and version.
- `cmd/vibewarden/wiring_serve_helpers_test.go` — update assertions.
- `vibewarden.reference.yaml` — flip the documented default for `upstream.health.enabled` to `true`, document the new defaults (`5s`, `2s`).
- `decisions/README.md` — index entry for ADR-098 (and the missing ADR-097 entry; orthogonal but cheap to fix here).

**Files explicitly out of scope (separate pipelines):**

- `vibew doctor` upstream check removal — companion issue #1198.
- `internal/plugins/readiness` and the dynamic ready route — out of scope.
  (`/ready` semantics are unchanged; this ADR only fixes `/health`.)

### Sequence

Boot:

1. `cmd/vibewarden/main.go` → `runServe` loads config (now with `upstream.health.enabled=true` by default).
2. `runServe` builds logger, registers and inits all plugins (`InitAll`), starts plugins (`StartAll`).
3. `runServe` calls `health.BuildDomainConfig(cfg.Upstream.Health, logger)`. If the probe is disabled or misconfigured, a Warn log line is emitted at boot.
4. If enabled, `runServe` constructs `healthadapter.NewHTTPChecker(...)` and calls `checker.Start(ctx)`. The first probe runs immediately inside the goroutine; subsequent probes run on the 5s ticker.
5. `runServe` calls `caddyadapter.SetRuntimeServices(RuntimeServices{..., UpstreamHealthChecker: checker, SidecarVersion: opts.version})`.
6. `runServe` builds `proxyCfg` and starts the Caddy adapter. Caddy loads the config, sees the `vibewarden_health` module on the `/_vibewarden/health` route, instantiates it, and calls `Provision`. Provision reads the same `RuntimeServices`.

Request:

7. Client `GET /_vibewarden/health`.
8. Caddy routes to `HealthHandler.ServeHTTP`.
9. Handler calls `h.checker.CurrentStatus()` (lock-free read; returns immediately).
10. Handler maps the domain status to `upstream.State`.
11. Handler builds the components map and calls `healthsummary.AggregateStatus`.
12. Handler writes `200 OK` + JSON body. Latency: a few microseconds — no I/O on the request path.

Boot gap (first 5–10 s):

- Between step 4 (`Start`) and the first probe completing, the entity is in `StatusUnknown`. The handler renders `"upstream":"unknown"`, outer `"status":"degraded"` (worst-component rule) for that brief window. AC accepts this.

Shutdown:

- `runServe` cancels its context on SIGINT/SIGTERM.
- The deferred `checker.Stop(stopCtx)` runs with the existing 10s budget.
- `checker.Stop` closes `stopCh`; the `loop` goroutine selects the close, returns, and closes `doneCh`.
- `Stop` either returns nil (clean) or wraps `ctx.Err()` (timeout). Logged but does not block process exit.

### Error cases

| Case | Behaviour |
|---|---|
| Probe disabled (`enabled: false`) | `RuntimeServices.UpstreamHealthChecker = nil`. Handler renders `upstream:"unknown"`. **Boot Warn log emitted.** |
| Probe path empty | Caught by `config.Validate` (existing). `runServe` exits non-zero with the validation error. |
| Probe interval/timeout malformed | Caught by `config.Validate` (existing). `runServe` exits non-zero. |
| Upstream unreachable for first probe | `recordFailure` increments `failures`. After `unhealthy_threshold` consecutive failures (3 by default = ~15s), state flips to `Unhealthy` → handler reports `failing`. |
| Upstream returns non-2xx | Same as above (`recordFailure` with `non-2xx status: NNN`). |
| Probe context cancelled mid-flight (shutdown) | `client.Do` returns `context.Canceled`; `recordFailure` records the error; the next iteration of the loop selects on `ctx.Done()` and returns. No leaked goroutine. |
| `RuntimeServices` not yet published when handler provisions | `currentServices()` returns zero-value; handler logs a Warn and renders `upstream:"unknown"`. The composition root must call `SetRuntimeServices` before `caddy.Load` runs — already true by ADR-092. |
| Two boot races (e.g. hot reload) | `SetRuntimeServices` is atomic; last write wins. Active handlers keep their first set (Caddy provisions once per load). New handlers post-reload get the new checker. The checker itself is not recreated on reload — it survives across reloads. |

Explicitly **not** added: retry logic, exponential backoff, circuit breaker on the probe itself. A failed probe is just a failure; the next 5s tick decides the next state. This is the AC's "avoid over-engineering" instruction.

### Test strategy

**Domain (unit, no I/O):**

- `internal/domain/upstream/state_test.go`: table-driven over each `Kind`; assert `String()`, `Healthy()`, `LastError()`. Includes the zero-value (Unknown) case.
- `internal/domain/healthsummary/aggregate_test.go`: table-driven matrix:
  - empty map → `ok`
  - all healthy → `ok`
  - one component failing → `degraded`
  - one component unknown → `degraded`
  - nil component value → `degraded` (defensive)
  - multiple components, varied → worst-wins.

**Application (unit):**

- `internal/app/health/config_test.go`:
  - disabled → `(zero, false)` and Warn log captured (use `slogtest`).
  - enabled with all defaults → 5s/2s/`/health`/3/2.
  - enabled with explicit values → values pass through.
  - enabled with malformed `interval` → falls back to 5s **and** Warn logged.
  - enabled with empty `path` → `/health` default.

**Adapter (unit + table-driven JSON shape):**

- `internal/adapters/caddy/health_handler_test.go`:
  - `ProvisionWith` reads checker + version from `RuntimeServices`.
  - ServeHTTP with no checker → `{"status":"degraded","components":{"sidecar":"ok","upstream":"unknown"}}` and HTTP 200.
  - ServeHTTP with checker reporting Healthy → `{"status":"ok","components":{"sidecar":"ok","upstream":"ok"}}`.
  - ServeHTTP with checker reporting Unhealthy → `{"status":"degraded","components":{"sidecar":"ok","upstream":"failing"}}`.
  - ServeHTTP with checker reporting Unknown → outer `"degraded"`.
  - With `Config.SiteName="demo"` → response body contains `"site":"demo"`.

- `internal/adapters/caddy/config_build_test.go`: assert the route's
  `handle[0].handler == "vibewarden_health"` and that the body **does
  not** contain a hardcoded `"upstream":"unknown"` string anywhere in
  the generated config.

**Integration (existing checker + new handler):**

- `internal/adapters/caddy/health_handler_integration_test.go` (new):
  - Spin up an `httptest.Server` with a `/health` endpoint that returns
    200, then 500, then 200.
  - Construct an `HTTPChecker` via `NewHTTPCheckerFromURL` with
    `Interval=50ms`, `Timeout=20ms`, `UnhealthyThreshold=2`,
    `HealthyThreshold=2`.
  - Drive it through enough probes to observe the state machine in the
    handler's response body. Use `assert.Eventually` (custom, no new dep)
    with a 1-second budget.

**Architecture invariants (existing pattern):**

- `test/architecture/ports_purity_test.go` already enforces that
  `internal/adapters/caddy/` does not import peer adapter packages
  (ADR-092). The new `health_handler.go` complies — it imports
  `domain/upstream`, `domain/healthsummary`, `domain/health`, `ports`.
  No new architecture test required.

**Regression / contract tests:**

- `internal/adapters/caddy/config_build_test.go`: explicit assertion
  that the produced JSON **does not** contain the literal string
  `"upstream":"unknown"` — this is the contract test that locks the bug
  closed.
- `internal/middleware/health_upstream_test.go`: update the expected
  string vocabulary (`healthy`→`ok`, `unhealthy`→`failing`).

### New dependencies

**None.** All required types live in the existing module. License audit
unnecessary.

## Alternatives considered

1. **Internal HTTP server + reverse_proxy** (mirror `/ready`). Adds a TCP
   port, an extra hop, more surface area. Rejected — the request path
   would do an extra TCP roundtrip even though the data is in memory.

2. **Embed the checker logic directly in the Caddy handler.** Couples
   transport to probing — violates hexagonal separation. Rejected.

3. **Reuse the existing `internal/middleware/health.go` HealthHandler via
   a Caddy reverse_proxy.** Same problem as (1) — extra TCP hop, plus
   we'd need to keep two health endpoints.

4. **Top-level `health.probe.*` config block** (instead of
   `upstream.health.*`). Cleaner namespace, but breaks the existing
   `UpstreamHealthConfig` and would require a config migration. The
   acceptance criteria are met without renaming. Rejected.

## Consequences

**Positive**

- `/_vibewarden/health` reports the actual upstream state. The retro
  bug closes.
- Probe defaults to ON. Users do not need to flip a flag to get correct
  behaviour.
- Aggregator is a domain-pure function — easy to extend with new
  components (TLS, Kratos, Postgres) without touching the handler.
- The new value-object pattern in `internal/domain/upstream/` mirrors
  `internal/domain/tls/` and gives us a clean foundation for surfacing
  upstream state in `vibew status` later.
- Lock-free read path on the request: `CurrentStatus()` is one atomic
  load. The endpoint stays cheap.

**Trade-offs**

- **Wire-format change** on `components.upstream`: `healthy` → `ok`,
  `unhealthy` → `failing`. We are pre-1.0 and this endpoint is documented
  as operator-internal (used by `vibew doctor`, `vibew status`,
  Prometheus and human ops, not by integrators). Document the change in
  the v0.18.2 release notes. The old vocabulary survives nowhere on the
  wire after this change.
- One new background goroutine per sidecar. Memory footprint:
  `~1 KiB` for the entity + ticker. Negligible.
- One new Caddy module (`vibewarden_health`). Adds a few microseconds
  to startup config-load. Negligible.

**Future-proofing**

- When TLS state moves into the health response (likely follow-up),
  it adds itself to the `components` map and reuses
  `healthsummary.AggregateStatus`. No handler edits required.
- When fleet (Pro) ships, the same `Snapshot()` data is the natural
  payload — no new code path, just a re-render.
- After this lands, `vibew doctor` drops its bespoke upstream probe
  (#1198) and reads the snapshot instead — one source of truth for
  upstream health.

**Operational**

- The boot Warn log when the probe is disabled is the only behavioural
  change for users who explicitly opt out (`enabled: false`).
  Acceptable — silent disable is the bug we are correcting.
- `vibew dev` (compose stack) and production both put the sidecar and
  the upstream on the same Docker network — the probe path
  (`http://<upstream.host>:<upstream.port><path>`) is identical in both
  environments. No environment-specific code.
