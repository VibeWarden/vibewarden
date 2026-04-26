# ADR-095: `vibew status` three-state model — OK / OFF / FAIL

**Date**: 2026-04-23
**Issue**: #1143
**Status**: Accepted

## Context

`vibew status` currently uses a boolean `ComponentStatus.Healthy` rendered as
`✓` (green) or `✗` (red). Two false-alarm sources break trust on a clean
`vibew dev` stack:

1. **Auth probe ignores config** — `gatherStatuses` always probes Kratos at
   `cfg.Kratos.AdminURL` even when `cfg.Auth.Active() == false` (mode `none`
   or empty). Result: `✗ Auth (Kratos) unreachable` on stacks where auth is
   intentionally disabled.

2. **TLS near-expiry alarm fires for self-signed dev certs** — the renderer
   in `renderTLSStatusLine` treats `KindExpiringSoon` as `healthy = false`
   regardless of whether the cert is the local Caddy-issued dev cert.
   Result: `✗ TLS near expiry (expires in 0 days)` on a fresh dev stack.

Both alarms train operators (and agents) to ignore `FAIL` rows, defeating
the point of the dashboard.

The PM spec (#1143) pre-decides the user-visible behaviour: three states
(OK / OFF / FAIL), text labels (no glyphs), TTY colour (green/dim/red),
disabled features render OFF without probing, `KindSelfSignedLocal` renders
OK with annotation. This ADR codifies the type, the per-row gates, and the
test surface.

## Decision

### Domain model changes

None. The status state is an application-layer concern (presentation +
probing-gate), not a domain concept. Adding it to `internal/domain/` would
overreach — the domain has no opinion on rendering.

### `StatusState` type — location and shape

Add a new file `internal/app/ops/status_state.go`:

```go
package ops

// StatusState is the tri-state rendering tag for a status row.
type StatusState int

const (
    StatusOK   StatusState = iota // enabled + check passed (or always-on infra)
    StatusOFF                     // disabled in config — probe suppressed
    StatusFAIL                    // enabled + check failed
)

// String returns the canonical text label rendered to non-TTY output.
func (s StatusState) String() string {
    switch s {
    case StatusOK:
        return "OK"
    case StatusOFF:
        return "OFF"
    case StatusFAIL:
        return "FAIL"
    default:
        return "?"
    }
}
```

`ComponentStatus` gains a `State StatusState` field. The legacy `Healthy bool`
field is **removed** (not deprecated) — there are only two callers
(`status.go` and `status_tls_render.go`) and one external test file
(`status_tls_render_test.go`). A clean break is cheaper than carrying both.

```go
type ComponentStatus struct {
    Name   string
    State  StatusState
    Detail string
}
```

`renderTLSStatusLine` signature changes from
`(state) (detail string, healthy bool)` to
`(state) (detail string, status StatusState)`.

### Per-row probing rules

The architect's contract for each row in `gatherStatuses`:

| Row | Gate | OK | OFF | FAIL |
|---|---|---|---|---|
| Proxy | always probed (always-on infra) | reachable | — | unreachable |
| Auth (Kratos) | `cfg.Auth.Active()` | active + reachable | inactive — **no HTTP call** | active + unreachable |
| Rate Limit | config-only, no probe | always | when `!cfg.RateLimit.Enabled`, detail `"disabled"` | never |
| Metrics | `cfg.Metrics.Enabled` | enabled + reachable | disabled — no probe | enabled + unreachable |
| TLS | resolver state (see below) | per state | n/a — TLS row never OFF (use detail) | `KindFailing` only |

**TLS state mapping** (drives `renderTLSStatusLine` rewrite):

| `tlsdomain.Kind` | StatusState | Detail |
|---|---|---|
| `KindDisabled` | `StatusOK` | `"disabled"` |
| `KindSelfSignedLocal` | `StatusOK` | `"self-signed (dev)"` |
| `KindObtaining` | `StatusOK` | `"obtaining (ACME in progress)"` |
| `KindObtained` | `StatusOK` | `"obtained (expires YYYY-MM-DD)"` |
| `KindExpiringSoon` | `StatusOK` | `"obtained (expires in N days)"` — annotation only, **never FAIL** |
| `KindFailing` | `StatusFAIL` | `"failing"` or `"failing (last error: ...)"` |
| `KindUnknown` | `StatusOK` | `"state unavailable — start 'vibew dev'"` |

Rationale for `KindExpiringSoon → StatusOK`: a near-expiry cert is still
serving traffic; the user already gets a real `FAIL` once renewal fails
(`KindFailing`). The PM spec is explicit: "self-signed dev TLS no longer
triggers near-expiry alarm". This ADR generalises that rule — near-expiry
is an annotation, not a failure, for any cert. If the operator wants
proactive alerting on expiring certs, that belongs in `vibew doctor`, which
already has WARN severity (`renderTLSDoctorCheck`).

### Auth row — code shape

Replace the unconditional `s.checkHTTP("Auth (Kratos)", ...)` call with:

```go
if cfg.Auth.Active() {
    statuses = append(statuses, s.checkHTTP(ctx, "Auth (Kratos)", kratosURL+"/admin/health/ready", kratosURL))
} else {
    statuses = append(statuses, ComponentStatus{
        Name:   "Auth (Kratos)",
        State:  StatusOFF,
        Detail: "auth disabled",
    })
}
```

`checkHTTP` returns `StatusOK` on success and `StatusFAIL` on error/non-2xx.

### `printStatusTable` rewrite

Replace the `mark := green("✓") / red("✗")` block with a label renderer
honouring TTY detection. The `github.com/fatih/color` library (already
imported) handles TTY detection internally and respects `NO_COLOR`. No
new dependency.

```go
labels := map[StatusState]string{
    StatusOK:   color.New(color.FgGreen).Sprint("OK"),
    StatusOFF:  color.New(color.FgHiBlack).Sprint("OFF"), // dim/grey
    StatusFAIL: color.New(color.FgRed).Sprint("FAIL"),
}
```

Column width: pad labels to width 4 (`"OK  "`, `"OFF "`, `"FAIL"`) so the
component name aligns regardless of state. Use `runewidth`-safe padding
of the *plain* label, then wrap with colour, since ANSI codes inflate
`len()` but not visual width.

### Legend — included

Print a one-line legend below the title, before the rows:

```
VibeWarden Status
─────────────────────────────────────────
States: OK = healthy   OFF = disabled   FAIL = check failed

  OK    Proxy                 https://localhost:8443
  OFF   Auth (Kratos)         auth disabled
  ...
```

Decision: **include the legend**. Self-explanation matters more than
brevity for a command operators read once a week. Three short states fit
on one line. The PM spec lists the legend as an acceptance criterion.

### Plugins table — unchanged scope

The `Plugins` sub-table (`gatherPluginStatuses`) is *out of scope*: it
already renders enabled/disabled correctly with `✓` / `-` glyphs. The
PM spec restricts the change to the component table. Leave the plugins
table alone in this PR; if convergence is desired, file a follow-up.

### File layout

New files:
- `internal/app/ops/status_state.go` — `StatusState` type + `String()`.
- `internal/app/ops/status_state_test.go` — table-driven unit tests for
  the type's `String()` and label rendering.

Modified files:
- `internal/app/ops/status.go` — `ComponentStatus` field swap; auth gate;
  metrics already gated (verify); `printStatusTable` rewrite with label
  renderer + legend.
- `internal/app/ops/status_tls_render.go` — return `StatusState` instead
  of `bool`; remap `KindExpiringSoon` to `StatusOK`.
- `internal/app/ops/status_tls_render_test.go` — update assertions to
  the new return signature; add `KindSelfSignedLocal` and
  `KindExpiringSoon` cases asserting `StatusOK`.
- `internal/app/ops/status_test.go` — add tests for the auth-disabled
  suppression (no HTTP call), and table tests for `printStatusTable`
  label rendering with TTY/non-TTY.

Unchanged:
- `internal/cli/cmd/status.go` — wiring only; no surface change.
- `internal/adapters/caddy/tls_state.go` — already produces
  `KindSelfSignedLocal` correctly (PR #1096). No change.
- `vibew doctor` — flagged in PR notes if it shares `ComponentStatus`,
  but `doctor.go` uses its own `CheckResult` type with a `Severity`
  enum (already three-state). No impact.

### Sequence (new request flow)

1. CLI `vibew status` calls `StatusService.Run(ctx, cfg, out)`.
2. `gatherStatuses` builds the rows:
   1. Proxy → `checkHTTP` → `StatusOK` or `StatusFAIL`.
   2. Auth → `if cfg.Auth.Active()` then `checkHTTP`, else
      `{State: StatusOFF, Detail: "auth disabled"}` — **no network call**.
   3. Rate Limit → config-only, `StatusOK` always (detail says
      `"disabled"` or `"enabled (...)"`).
   4. Metrics → if enabled probe, else `StatusOFF`.
   5. TLS → `tlsComponentStatus` → resolver → `renderTLSStatusLine`
      returning `(detail, StatusState)`. Resolver-failure or nil falls
      back to `StatusOK` + config-only detail.
3. `printStatusTable` writes:
   1. Title + horizontal rule.
   2. One-line legend.
   3. Each row: padded coloured label + name + detail.
   4. Plugins table (unchanged).
4. Existing proxy-down diagnostic loop continues to inspect
   `st.State == StatusFAIL && st.Name == "Proxy"`.

### Error cases

- **TLS resolver returns error** — fall through to config-only detail
  with `StatusOK` (current behaviour preserved). Never crash the
  dashboard.
- **`cfg.Auth.Active()` false but `cfg.Kratos.AdminURL` set** — still
  OFF; Kratos URL noise is irrelevant when auth is off.
- **Health-check timeout (5 s context)** — `StatusFAIL` with detail
  `"unreachable (...)"` (current behaviour preserved).
- **`color.New` on non-TTY / `NO_COLOR=1`** — `fatih/color` strips
  ANSI; plain `"OK"` / `"OFF"` / `"FAIL"` survive. Verified by the
  library's own tests; we add a regression test that captures stdout
  via a `bytes.Buffer` (always non-TTY) and asserts no escape codes.

### Test strategy

All unit tests, no integration test required for this PR. Fakes for
`HealthChecker` and `TLSStateResolver` already exist in `status_test.go`.

#### `internal/app/ops/status_state_test.go`

- Table test: `StatusOK.String() == "OK"`, etc.

#### `internal/app/ops/status_tls_render_test.go` (extend)

Update existing cases and add:

| Case | Input | Expected detail | Expected state |
|---|---|---|---|
| disabled | `KindDisabled` | `"disabled"` | `StatusOK` |
| self-signed | `NewSelfSignedLocal()` | `"self-signed (dev)"` | `StatusOK` |
| obtaining | `KindObtaining` | `"obtaining (ACME in progress)"` | `StatusOK` |
| obtained | `KindObtained` (expires 90d) | `"obtained (expires YYYY-MM-DD)"` | `StatusOK` |
| near-expiry | `KindExpiringSoon` (5 days) | `"obtained (expires in 5 days)"` | `StatusOK` |
| failing-with-error | `NewFailing("dns")` | `"failing (last error: dns)"` | `StatusFAIL` |
| failing-no-error | `NewFailing("")` | `"failing"` | `StatusFAIL` |
| unknown | `KindUnknown` | `"state unavailable — start 'vibew dev'"` | `StatusOK` |

#### `internal/app/ops/status_test.go` (extend)

Add tests:

1. **Auth disabled → OFF, no probe**:
   - Build `cfg` with `auth.mode: none`.
   - Use a `fakeHealthChecker` with a counter incremented on every call.
   - Assert: row for `Auth (Kratos)` has `State == StatusOFF`,
     `Detail == "auth disabled"`.
   - Assert: counter has zero increments for the Kratos URL (other rows
     may still call the checker).

2. **Auth enabled + reachable → OK**:
   - `cfg.Auth.Mode = "kratos"`, fake checker returns `(true, 200, nil)`.
   - Row state `StatusOK`.

3. **Auth enabled + unreachable → FAIL**:
   - Fake checker returns `error`.
   - Row state `StatusFAIL`, detail contains `"unreachable"`.

4. **`printStatusTable` rendering**:
   - Capture output via `bytes.Buffer`.
   - Assert legend line `"States: OK"` present.
   - Assert plain-text labels `"OK"`, `"OFF"`, `"FAIL"` (buffer is
     non-TTY so no ANSI codes — `fatih/color` strips them).

5. **`vibew dev` smoke (acceptance criterion translation)**:
   Build a `cfg` mirroring the default `vibew dev` output (auth off,
   metrics off, rate-limit off, TLS self-signed), run `gatherStatuses`
   with a fake checker that 200s the proxy and a resolver returning
   `KindSelfSignedLocal`. Assert: zero rows have `State == StatusFAIL`.

#### Coverage target

`internal/app/ops/` already exceeds the 80 % gate (per CLAUDE.md). The
new lines are in pure rendering and a single `if` in `gatherStatuses`,
both fully covered by the tests above.

### New dependencies

None.

- `github.com/fatih/color` — already vendored, MIT licensed, already
  used by `printStatusTable`. Reused for label colouring.
- TTY detection — handled by `fatih/color` internally; no `golang.org/x/term`
  needed.

### CHANGELOG

Append to `CHANGELOG.md` under `[Unreleased] / ### Fixed` (per PM spec):

> `vibew status`: disabled features now show OFF instead of probing and
> failing; self-signed dev TLS no longer triggers near-expiry alarm.

## Consequences

**Good**:
- Every `FAIL` row is now actionable. Operators can rebuild trust in the
  dashboard.
- The probing-gate pattern (`if cfg.Auth.Active()`) is the same one used
  by `gatherPluginStatuses` for the plugins table — convergent style.
- `StatusState` is a small typed enum; adding future states (e.g. `WARN`)
  is one constant + one label.

**Trade-offs**:
- Removing `Healthy bool` is a breaking change to the internal
  `ComponentStatus` API. There are zero external consumers — the type is
  unexported in spirit (defined in `internal/`), used only within
  `internal/app/ops/`. Net zero blast radius.
- The "near-expiry is OK, not FAIL" generalisation is a behaviour change
  beyond self-signed certs. If a real Let's Encrypt cert is 5 days from
  expiry and renewal stalls, `vibew status` will show `OK` with an
  expiry annotation, not `FAIL`. The mitigating factor: ACME-renewal
  stalls produce `KindFailing`, which is still `FAIL`. `vibew doctor`
  retains its `WARN` severity for this case. Documented here so future
  readers don't relitigate.

**Future**:
- The plugins table could converge on `StatusState` for visual symmetry
  (currently uses `✓` / `-` glyphs). Out of scope for this PR — file a
  follow-up if desired.
- `--json` output (#1135) should serialise `StatusState` as the lowercase
  string (`"ok"`, `"off"`, `"fail"`) when that PR lands. Note in the JSON
  spec, not in this ADR.
