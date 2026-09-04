# ADR-114: Per-client-IP lockout for failed admin-token attempts — domain state machine, in-memory guard, shared via RuntimeServices

**Date**: 2026-09-04
**Issue**: [#1478](https://github.com/vibewarden/vibewarden/issues/1478)
**Status**: Accepted

---

## Context

`AdminAuthMiddleware` (`internal/middleware/admin_auth.go`) runs at plugin priority 30,
ahead of the rate-limit middleware at 50, and `/_vibewarden/*` is unconditionally exempt
from rate limiting anyway (`NewExemptPathMatcher` always adds it). A wrong `X-Admin-Key`
is therefore rejected with 401 and an `audit.auth.failure` event, and nothing in the chain
slows down the next attempt. The token is 256 bits of entropy compared in constant time,
so this is defence in depth and an audit-log-volume improvement, not a critical fix.

Three facts from the current code shape the design:

1. **`AdminAuthMiddleware` is instantiated up to three times per Caddy config** —
   `buildAdminAuthHandlerJSON` is called from `buildAdminRoute` (`/_vibewarden/admin/*`),
   `buildConfigRoute` (`/_vibewarden/config`, `/_vibewarden/config/*`) and
   `buildCatchAllHandlers`. Failure state held per middleware instance would give an
   attacker three independent budgets. The counter must be one shared instance.
2. **`buildRuntimeServices` runs once per `vibew serve`**, before the first `caddy.Load`.
   A config hot-reload re-Provisions the handlers but does not republish RuntimeServices,
   so a guard held there survives reloads — which is what a lockout counter needs.
3. **The audit stream is not the v1 event schema.** `internal/adapters/audit/json_writer.go`
   writes its own JSONL record (`timestamp`, `event_type`, `actor`, `target`, `outcome`,
   `trace_id`, `details`) with no `schema_version`/`ai_summary`. Neither `audit.auth.*` nor
   `audit.rate_limit.*` appear in `schema/v1/event.json` (only `audit.log_failure`, which is
   a `domain/events` type, not an audit type). The PM acceptance criterion asking for
   `audit.auth.lockout` to be added "alongside the existing `audit.auth.*`" rests on a
   premise that does not hold; see Decision §7.

The closest existing precedent is the circuit breaker: a pure domain state machine
(`internal/domain/resilience`) with an explicit `now time.Time` parameter, wrapped by a
mutex-holding in-memory adapter (`internal/adapters/resilience`) behind a port
(`ports.CircuitBreaker`), injected through `RuntimeServices`. This ADR follows it exactly.

---

## Decision

### 1. Behaviour contract (locks down the issue's three open questions)

Per client IP, counting only failed token comparisons on gated admin paths:

| Attempt | Response | Audit event |
|---|---|---|
| failures 1–9 within the window | 401 + `WWW-Authenticate` (unchanged) | `audit.auth.failure` |
| failure 10 (arms the lockout) | 401 + `WWW-Authenticate` | `audit.auth.lockout` **instead of** `audit.auth.failure` |
| any request while in cooldown | 429 + `Retry-After`, token never compared | **none** |
| first request after cooldown expiry | evaluated normally, counter starts at zero | as above |
| correct token at any point | 200 (passes through), counter cleared | `audit.auth.success` |

- **Open question 1 (cooldown-window logging): emit nothing.** No audit event, no
  operational log line. One `audit.auth.lockout` per lockout episode is the whole point of
  the story; a per-request debug line would need a `*slog.Logger` parameter the middleware
  does not have, and the 429s remain visible in Caddy's server access log. A test asserts
  zero audit events for cooldown-window requests.
- **Open question 2 (UI carve-out): unaffected, and deliberately so.** The lockout check is
  inserted *after* the `/_vibewarden/admin/ui` carve-out, so static assets never consume the
  counter and never receive a 429. The carve-out cannot fail auth, so it cannot feed the
  counter either. A locked-out IP can still load the console shell; every data call it makes
  gets 429.
- **Open question 3 (constants): 10 / 1 min / 1 min stand.** No existing rate-limit default
  is comparable — `rate_limit` is a token bucket in requests-per-second, this is a
  consecutive-failure count. Tying them together would couple two unrelated policies.

The window is anchored on the **first failure of the current streak**: if
`now - windowStart > Window`, the streak restarts at 1. Cooldown expiry resets all state
(no indefinite lockout, no memory of pre-lockout failures).

### 2. No new config surface

Threshold, window, cooldown and the tracking cap ship as fixed exported constants. Nothing
is added to `vibewarden.yaml` or `vibewarden.reference.yaml`, and `vibew validate`'s strict
unknown-key loader is untouched. A tunable is a later decision if a real user hits the
default; shipping a knob nobody asked for costs a permanent public config key.

### 3. Domain model changes

New package `internal/domain/authguard` (stdlib only, no external imports):

- `Policy` — value object `{Threshold int; Window, Cooldown time.Duration}` with
  `Validate() error` (all three must be > 0) and `DefaultPolicy() Policy`.
- Exported constants `DefaultThreshold = 10`, `DefaultWindow = time.Minute`,
  `DefaultCooldown = time.Minute`.
- `Attempts` — the per-key entity. Not safe for concurrent use; the caller synchronises
  (same contract as `resilience.CircuitBreaker`). Fields are unexported:
  `failures int`, `windowStart time.Time`, `lockedUntil time.Time`.
  - `LockedOut(now time.Time) (locked bool, retryAfter time.Duration)` — advances state:
    when `lockedUntil` is set and has elapsed, resets the entity and returns false.
  - `RecordFailure(now time.Time, p Policy) (tripped bool)` — no-op increment when already
    locked; restarts the streak when the window elapsed; sets `lockedUntil = now + Cooldown`
    and returns true on the failure that reaches `Threshold`.
  - `Reset()` — clears all state (successful auth).
  - `Failures() int`.
  - `Idle(now time.Time, p Policy) bool` — true when no lock is active and the window has
    elapsed, i.e. the entry carries no information and is safe to evict.

New audit event type in `internal/domain/audit/audit.go`, in the existing `--- auth ---`
group:

```go
// EventTypeAuthLockout is recorded when repeated failed authentication attempts
// from one client trip a lockout, throttling further attempts.
EventTypeAuthLockout EventType = "audit.auth.lockout"
```

Additive only — no existing constant changes, so no schema version bump.

### 4. Ports

New file `internal/ports/auth_lockout.go`. No `context.Context` parameter: the
implementation is in-memory and on the request path, matching `ports.CircuitBreaker`
(and unlike `ports.RateLimiter`, which may be Redis-backed).

```go
// LockoutStatus is the outcome of a lockout evaluation for one client key.
type LockoutStatus struct {
    LockedOut  bool          // reject the request with 429
    Tripped    bool          // set only by RecordFailure, only on the call that arms the lockout
    RetryAfter time.Duration // meaningful only when LockedOut
    Failures   int           // consecutive failures after this call
    Threshold  int           // the configured threshold, for the audit payload
}

// AuthLockoutGuard throttles repeated authentication failures per client key.
// All methods must be safe for concurrent use. In v1 the only consumer is
// AdminAuthMiddleware and the key is always a client IP address.
type AuthLockoutGuard interface {
    // Status reports whether key is currently locked out. It may lazily expire
    // state but must never increment a failure count, and must not create an
    // entry for an unknown key.
    Status(key string) LockoutStatus

    // RecordFailure records one failed authentication attempt for key.
    RecordFailure(key string) LockoutStatus

    // RecordSuccess clears all failure state for key.
    RecordSuccess(key string)
}
```

Only `RecordFailure` may allocate an entry. `Status` and `RecordSuccess` are read/clear
only, so no traffic pattern other than actual auth failures can grow the map.

### 5. Adapter

`internal/adapters/authguard/memory_guard.go` — `MemoryGuard` implements
`ports.AuthLockoutGuard`:

- `entries map[string]*authguard.Attempts` plus a `lastSeen` per entry, guarded by one
  `sync.Mutex` (not `sync.Map`: entries are mutated, and admin-path contention is nil).
- `now func() time.Time`, defaulting to `time.Now`, replaceable via a
  `WithClock(func() time.Time)` functional option for deterministic tests.
- `NewMemoryGuard(p authguard.Policy, maxEntries int, opts ...Option) (*MemoryGuard, error)`
  — returns an error on an invalid policy or `maxEntries <= 0`.
- `DefaultMaxEntries = 10_000` (≈1–2 MB worst case).
- **Bounded memory, no goroutine.** Eviction happens inline in `RecordFailure` when a new
  entry would exceed the cap: one O(n) sweep deletes every `Idle` entry; if the map is still
  at the cap, the single oldest `lastSeen` entry is deleted to make room. Deliberately *not*
  the `ratelimit.MemoryStore` pattern (background GC goroutine + `Close`): that would need a
  lifecycle the Caddy handler cannot express, and would leak a goroutine per construction.
  The sweep only runs under an active flood, where an O(10 000) scan is irrelevant next to
  the request cost.

### 6. Middleware and wiring

`AdminAuthMiddleware` gains a third parameter:

```go
func AdminAuthMiddleware(
    cfg ports.AdminAuthConfig,
    auditLogger ports.AuditEventLogger,
    lockout ports.AuthLockoutGuard,
) func(http.Handler) http.Handler
```

A nil guard disables throttling and preserves today's behaviour exactly (all 16 existing
test call sites pass `nil` unchanged apart from the extra argument).

Insertion point: after the `cfg.Token == ""` misconfiguration check, before the header read.
Everything above it — path gating, `Enabled` 404, UI carve-out, 500 on empty token — is
untouched.

**Unresolvable client IP fails open for throttling only.** When
`ExtractClientIP(r, false)` returns `""`, the lockout logic is skipped and the token gate
runs as today. Keying on `""` would put every unidentifiable client in one shared bucket,
letting a single attacker lock out all of them — a self-inflicted DoS on the admin API. The
rate limiter's fail-closed 403 is the right call for a per-request budget; it is the wrong
call for a stateful lockout. This is a documented, tested branch.

`RuntimeServices` gains `AdminLockoutGuard ports.AuthLockoutGuard`; `ProvisionWith` passes
it straight through and, when nil, emits the same one-time `Warn` shape already used for a
missing `AuditEventLogger`. `buildRuntimeServices` constructs one `MemoryGuard` and assigns
it; a construction error is logged and leaves the field nil (degrade, never fail startup).

New response helper in `internal/middleware/error_response.go`, mirroring
`WriteRateLimitResponse`:

```go
// WriteLockoutResponse writes the 429 response for an auth lockout: the
// Retry-After header, Content-Type: application/json, and a body with
// error "too_many_failed_attempts" and retry_after_seconds.
func WriteLockoutResponse(w http.ResponseWriter, r *http.Request, retryAfterSeconds int)
```

A distinct error code rather than reusing `rate_limit_exceeded`: dashboards and agents key
on that string, and a lockout is a different condition with a different remedy. The 429 must
**not** carry `WWW-Authenticate` — that header invites an immediate retry.

### 7. Event schema: `schema/v1/event.json` is not touched

`schema/v1/event.json` and the `docs/ai-log-schema.md` event-type tables describe the
`domain/events` operational stream. The `audit.*` types (`audit.auth.success`,
`audit.auth.failure`, `audit.rate_limit.hit`, all fifteen of them) have never been listed
there, and the audit JSONL record does not even carry the v1 envelope fields. Adding
`audit.auth.lockout` alone to that schema would assert a contract the sidecar does not
emit and would leave the other fifteen types undocumented next to it.

The new event is instead documented where the audit stream is documented for operators:
`docs/production-hardening.md` (admin API section) and the `admin` table in
`docs/configuration.md`. Publishing the full audit-event catalogue is a separate,
worthwhile piece of work and is out of scope here.

### 8. File layout

New:

- `internal/domain/authguard/lockout.go`
- `internal/domain/authguard/lockout_test.go`
- `internal/ports/auth_lockout.go`
- `internal/adapters/authguard/memory_guard.go`
- `internal/adapters/authguard/memory_guard_test.go`

Modified:

- `internal/domain/audit/audit.go` — `EventTypeAuthLockout`
- `internal/middleware/admin_auth.go` — signature, lockout branch, godoc
- `internal/middleware/admin_auth_test.go` — existing call sites + new cases
- `internal/middleware/error_response.go` — `WriteLockoutResponse`
- `internal/middleware/error_response_test.go`
- `internal/middleware/auth.go` — `emitAuditAuthLockout` next to the other emitters
- `internal/middleware/audit_test.go` — existing call sites + lockout event assertions
- `internal/adapters/caddy/runtime_services.go` — `AdminLockoutGuard` field
- `internal/adapters/caddy/admin_auth_handler.go` — pass the guard through `ProvisionWith`
- `internal/adapters/caddy/admin_auth_handler_test.go` (if present) / handler tests
- `cmd/vibewarden/wiring_serve_helpers.go` — construct the guard
- `docs/production-hardening.md`, `docs/configuration.md`, `docs/openapi.yaml`
- `CHANGELOG.md`

### 9. Sequence (gated admin request)

1. Path is not gated → pass through. Admin disabled → 404. Path under
   `/_vibewarden/admin/ui` (cleaned) → pass through. `cfg.Token == ""` → 500. *(unchanged)*
2. `clientIP := ExtractClientIP(r, false)`. Empty, or `lockout == nil` → skip to step 5.
3. `st := lockout.Status(clientIP)`. If `st.LockedOut`:
   `WriteLockoutResponse(w, r, retryAfterSeconds(st.RetryAfter))`, no audit event, return.
   The token is never read or compared on this path.
4. Fall through.
5. `provided := r.Header.Get("X-Admin-Key")`; `secureEqual(provided, cfg.Token)`.
6. Mismatch → `st := lockout.RecordFailure(clientIP)` (skipped when the guard is inactive);
   `st.Tripped` ? `emitAuditAuthLockout(...)` : `emitAuditAuthFailure(...)`; then the
   existing `WWW-Authenticate` header + 401 body, unchanged in both cases.
7. Match → `lockout.RecordSuccess(clientIP)`, `emitAuditAuthSuccess(...)`, `next.ServeHTTP`.

`audit.auth.lockout` payload: `Actor.IP = clientIP`, `Target.Path = r.URL.Path`,
`Outcome = failure`, `Details = {method, failures, threshold, window_seconds,
cooldown_seconds, retry_after_seconds}`. `window_seconds` and `cooldown_seconds` come from
`retryAfterSeconds(st.RetryAfter)` and the guard's policy exposed through `LockoutStatus`;
do not import the domain policy into the middleware.

### 10. Error cases

| Case | Handling |
|---|---|
| Client IP unresolvable | Skip lockout entirely, evaluate the token as today. Never key on `""`. |
| `AdminLockoutGuard` nil in RuntimeServices | One-time `Warn`, throttling disabled, auth unchanged. |
| `NewMemoryGuard` returns an error at startup | Log `Error`, leave the field nil, continue serving. Never panic, never fail startup. |
| Map at the entry cap | Sweep idle entries, then evict the oldest. Never unbounded, never an allocation failure. |
| Audit sink returns an error on the lockout event | Existing `logAudit` path: `Warn` + `IncEventLogDrop`. The 401/429 is unaffected. |
| Concurrent requests from one IP racing the threshold | Single mutex around read-modify-write, so exactly one call observes `Tripped == true`. This is the guarantee behind "exactly one lockout event per episode" and needs a `-race` test. |
| Clock moves backwards | `LockedOut` uses `!now.Before(lockedUntil)`, so a backwards jump extends the lockout rather than releasing it early. Acceptable and documented. |

### 11. Test strategy

**Unit, `internal/domain/authguard` (pure, table-driven, injected `now`)** — streak
increments; window expiry restarting the streak; trip at exactly `Threshold`; no increment
while locked; cooldown expiry resetting state; `Reset` after success; `Idle` classification.
This is where the timing semantics are pinned. Target ≥ 90%.

**Unit, `internal/adapters/authguard`** — `WithClock`-driven equivalents of the five PM
acceptance cases, plus: `Status` does not create entries; `Status` does not increment;
eviction at the cap prefers idle entries and never exceeds `maxEntries` under a
10 001-distinct-key flood; an active lockout survives a sweep triggered by other keys;
`go test -race` with concurrent `RecordFailure` on one key produces exactly one `Tripped`.

**Unit, `internal/middleware`** — a hand-written `fakeLockoutGuard` in the test file
(the package must not import an adapter), scripting `LockoutStatus` values:
(a) under threshold → 401 + `audit.auth.failure`; (b) `Tripped` → 401 + exactly one
`audit.auth.lockout` and zero `audit.auth.failure`; (c) `LockedOut` → 429, `Retry-After`
present and parseable, zero audit events, `WWW-Authenticate` absent, and the fake asserts
its token was never read; (d) success → `RecordSuccess` called; (e) nil guard → today's
behaviour byte-for-byte; (f) UI carve-out and non-gated paths never touch the guard;
(g) empty `RemoteAddr` → no guard call, normal 401.

**Integration** — none needed. No new I/O, no new external system. The Caddy handler test
covers only that `ProvisionWith` forwards the guard from `RuntimeServices`.

Per ADR-087, the domain and adapter tests sit next to their code; the shared-instance
property (three handler instances, one counter) is asserted in the caddy handler test, not
inferred.

### 12. New dependencies

None. Stdlib only (`sync`, `time`).

---

## Consequences

- **A shared mutable guard now hangs off `RuntimeServices`.** It is the third such
  service (rate limiter factory, circuit breaker factory) and follows their contract, but
  it is process-global state reached through the atomic registry. It is created once per
  `vibew serve` and survives config hot-reloads by design: a reload must not hand an
  attacker a fresh budget.
- **The admin API can now return 429**, a status it never returned before. Any client or
  script that treats "not 200 and not 401" as a transport error will see a new failure
  mode. Documented in `docs/openapi.yaml` on all four gated operations and in
  `docs/production-hardening.md`.
- **A shared-NAT team can lock itself out for a minute** by fat-fingering the token ten
  times from one egress IP. One minute, self-healing, and the console assets keep loading;
  acceptable for an admin surface that CLAUDE.md already says should not be publicly
  reachable.
- **`X-Forwarded-For` is not honoured** — `AdminAuthConfig` carries no `TrustProxyHeaders`
  flag and this ADR does not add one. Behind a proxy that terminates connections, every
  admin request presents the proxy's IP, so the lockout degrades to a global counter for
  admin traffic. That is a safe degradation (it throttles harder, not less), but it is the
  first thing to revisit if the feature is extended.
- **`audit.auth.lockout` is a new public event-type value.** Additive, so consumers that
  switch on `event_type` keep working; consumers that count `audit.auth.failure` to detect
  brute force will now see the tenth failure reported under the new type instead. That
  change in `audit.auth.failure` volume is the intended outcome, and it is a behaviour
  change worth a CHANGELOG line.
- **The audit-event catalogue remains undocumented** as a whole. This ADR records why
  `schema/v1/event.json` was left alone and makes the gap explicit; closing it is
  follow-up work.
- **`authguard` is deliberately generic.** Kratos login lockout stays with Kratos
  (out of scope), but if the API-key middleware ever needs the same treatment it consumes
  the same port with its own guard instance and its own key space.
