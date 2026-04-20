## ADR-083: ACME chain hardening — email preflight for ZeroSSL, Buypass removed from default chain
**Date**: 2026-04-20
**Issue**: #1055
**Status**: Accepted
**Supersedes (partially)**: ADR-079 (revises the default fallback chain)

### Context

ADR-079 introduced a 3-issuer ACME fallback chain (`letsencrypt` → `zerossl` →
`buypass`) for the default `provider: letsencrypt` configuration. During the
v0.15.0 fresh-agent retro the chain caused ~10+ minutes of production downtime.
PM issue #1055 decomposed the failure into three separable defects:

- **(a) Buypass is defunct.** `https://api.buypass.com/acme/directory` currently
  returns `403 Forbidden`. Caddy still exhausts its retry budget against it
  before abandoning the chain, wasting recovery time.
- **(b) ZeroSSL is tried without an email preflight.** `buildACMEIssuers` wires
  ZeroSSL into the default chain even when `tls.email` is empty. ZeroSSL then
  rejects the order (EAB requires email), surfacing as a transient issuance
  error rather than a deterministic configuration failure.
- **(c) No LE rate-limit preflight.** Hitting the 5-certs-in-168h per-identifier
  limit is not detected ahead of time; the chain retries through ZeroSSL +
  Buypass before surfacing the root cause.

PM scoped this ADR to (a) and (b). Defect (c) is deferred to #1057, which needs
a separate design call (new network dependency, where preflight lives, blocking
vs advisory).

The chain-build logic lives in two intentionally-duplicated copies per the
header comment in `internal/adapters/caddy/acme_issuers.go`:

- `internal/adapters/caddy/acme_issuers.go` — `buildACMEIssuers`, issuer URL
  constants, `isACMEProvider`
- `internal/plugins/tls/plugin.go` (lines ~258-325) — duplicate
  `buildACMEIssuers`, same URL constants, same chain

The duplication exists because the adapter package (`caddy/`) and the plugin
package (`plugins/tls/`) cannot import each other without breaking the
hexagonal boundary. De-duplication would require a new shared helper package
and is out of scope per the PM (point 5 of issue #1055).

### Decision

Apply two surgical changes to both copies of `buildACMEIssuers`, add two new
structured log event types to the v1 schema, and keep the duplication — flag
de-duplication as a follow-up.

#### 1. Default chain: remove Buypass

For `provider: letsencrypt` without an `acme_ca` override, the default chain
becomes a two-step candidate list, further filtered by email presence (see
point 2):

| Email present? | Default chain                  |
|----------------|--------------------------------|
| yes            | `[letsencrypt, zerossl]`       |
| no             | `[letsencrypt]`                |

`provider: buypass` continues to work as a first-class single-issuer
explicit opt-in, unchanged from ADR-079. A `tls.acme.provider_deprecated`
structured event is emitted at plugin Init when `provider: buypass` is
resolved, so operators who explicitly depend on Buypass are not silently
affected.

Rationale: the Buypass ACME directory is currently returning 403 Forbidden in
production; keeping it in the default chain strictly increases recovery time.
Users who want Buypass can still select it with `provider: buypass`.

#### 2. Email-preflight for ZeroSSL (skip-with-log, not a validation error)

When the default chain is being built for `provider: letsencrypt` and
`tls.email` is empty, ZeroSSL is skipped. No error is returned. A structured
log event `tls.acme.chain_skipped` is emitted at Init time to explain the
skip to the operator.

Rationale (why runtime-skip, not config-validate-reject):

- `tls.email` is legitimately absent for non-ACME providers (self-signed,
  external) and for `letsencrypt`-only deployments that do not care about
  expiry notifications. Elevating "email empty while provider=letsencrypt" to
  a validation error would break those legitimate configs.
- ZeroSSL hard-requires email for EAB registration; that remains a validation
  error when `provider: zerossl` is selected explicitly (existing behaviour,
  `internal/config/config.go:1753-1757`).
- LoadStrict (ADR-082) already rejects structurally-malformed configs, so the
  skip-with-log path is only reachable for configs that are strictly
  well-formed but cannot use ZeroSSL as a fallback.

#### 3. New structured log event types (v1 schema)

Three new event types are added to the v1 event schema. All three conform to
the existing Event envelope (`schema_version`, `event_type`, `timestamp`,
`severity`, `category`, `ai_summary`, `payload`) per
`internal/schema/v1/event.json`.

##### 3a. `tls.acme.chain_skipped`

Emitted once at plugin Init for every issuer that was evaluated and excluded
from the default chain.

| Field              | Type   | Example                                         |
|--------------------|--------|-------------------------------------------------|
| `provider`         | string | `"zerossl"`                                     |
| `reason`           | string | `"email_not_configured"`                        |
| `primary_provider` | string | `"letsencrypt"`                                 |

`severity`: `info`
`category`: `network`
`ai_summary` template:
`"ACME issuer %s skipped in fallback chain for %s: %s — set tls.email in your config to enable it"`
Example: `"ACME issuer zerossl skipped in fallback chain for letsencrypt: email_not_configured — set tls.email in your config to enable it"`

Allowed `reason` values (v1 frozen set — adding new values requires an ADR):
- `email_not_configured` — skipped because `tls.email` is empty.

Future reasons (e.g. `provider_unhealthy`, `provider_unsupported`) may be added
in later ADRs; consumers must not assume the set is closed.

##### 3b. `tls.acme.chain_fallback`

Emitted whenever Caddy transitions between issuers in an active fallback chain.

| Field           | Type   | Example                    |
|-----------------|--------|----------------------------|
| `from_provider` | string | `"letsencrypt"`            |
| `to_provider`   | string | `"zerossl"`                |
| `reason`        | string | `"upstream_unreachable"`   |
| `domain`        | string | `"app.example.com"`        |

`severity`: `medium`
`category`: `network`
`ai_summary` template:
`"ACME issuer failover for %s: %s → %s (%s)"`
Example: `"ACME issuer failover for app.example.com: letsencrypt → zerossl (upstream_unreachable)"`

Allowed `reason` values (v1 frozen set):
- `upstream_unreachable` — the previous issuer's ACME directory could not be
  reached (DNS, TCP, TLS, HTTP 5xx).
- `rate_limited` — the previous issuer returned a rate-limit response
  (HTTP 429 or ACME `rateLimited` problem type).
- `unknown` — Caddy signalled a transition but the cause was not classifiable.

**Feasibility note.** The dev should first audit whether Caddy's certmagic /
acmez libraries expose a hook callback for issuer transitions. If no stable
hook exists, the dev must fall back to emitting `tls.acme.chain_configured`
(see 3c) at Init time listing the resolved chain, and document the
runtime-transition limitation in-code. Do not hack Caddy internals.

##### 3c. `tls.acme.chain_configured` (emitted unconditionally at Init)

Emitted once per plugin Init, regardless of whether any issuers were skipped.
Captures the resolved chain so operators and log aggregators can see what was
actually wired up.

| Field               | Type     | Example                                |
|---------------------|----------|----------------------------------------|
| `primary_provider`  | string   | `"letsencrypt"`                        |
| `resolved_chain`    | []string | `["letsencrypt", "zerossl"]`           |
| `domain`            | string   | `"app.example.com"`                    |

`severity`: `info`
`category`: `network`
`ai_summary` template:
`"ACME fallback chain configured for %s (primary=%s): %s"`
Example: `"ACME fallback chain configured for app.example.com (primary=letsencrypt): letsencrypt,zerossl"`

##### 3d. `tls.acme.provider_deprecated` (optional, emitted only for buypass)

Emitted once at Init when `provider: buypass` is resolved, warning that the
provider is currently unhealthy per #1055.

| Field      | Type   | Example                                            |
|------------|--------|----------------------------------------------------|
| `provider` | string | `"buypass"`                                        |
| `reason`   | string | `"directory_returns_403"`                          |
| `guidance` | string | `"consider provider: letsencrypt with tls.email"`  |

`severity`: `medium`
`category`: `network`
`ai_summary` template:
`"ACME provider %s is deprecated: %s — %s"`

#### 4. Source-of-truth policy for the duplicated chain builder

- **Primary source of truth**: `internal/adapters/caddy/acme_issuers.go`.
  The canonical comment already lives there ("intentionally duplicated", etc.)
  and the file is the first entry point for `multisite_config.go`.
- **Mirror copy**: `internal/plugins/tls/plugin.go` (`buildACMEIssuers`,
  constants `acmeCA*`, `isACMEProvider`). Any change to the primary must be
  mirrored byte-for-byte in the plugin copy.
- **Both copies must be updated in this fix.** The dev adds a `// MUST MIRROR:
  internal/adapters/caddy/acme_issuers.go` banner to the plugin copy to make
  drift harder.
- **De-duplication is a follow-up.** Creating `internal/domain/tls/` or
  `internal/acme/` as a shared pure-Go helper package is out of scope per the
  PM. The dev should open a follow-up issue ("refactor: unify `buildACMEIssuers`
  copies into a shared pure package") as part of the PR description.

#### 5. New interfaces and types

No new port interfaces. The existing `ports.EventLogger` is used to emit all
four new event types. The only domain-layer additions are:

- Four new event-type constants in `internal/domain/events/events.go`:
  - `EventTypeTLSACMEChainSkipped    = "tls.acme.chain_skipped"`
  - `EventTypeTLSACMEChainFallback   = "tls.acme.chain_fallback"`
  - `EventTypeTLSACMEChainConfigured = "tls.acme.chain_configured"`
  - `EventTypeTLSACMEProviderDeprecated = "tls.acme.provider_deprecated"`
- Four corresponding `NewX` constructor functions in
  `internal/domain/events/tls_acme.go` (new file), following the pattern in
  `internal/domain/events/placeholders.go`.
- Four payload `$defs` entries + four `allOf` branches added to
  `internal/schema/v1/event.json`.
- Matching entries in `internal/mcp/schema_registry.go` so the MCP
  schema-discovery surface advertises the new types.

#### 6. Chain-builder signature (no change — still pure, still same name)

```go
// internal/adapters/caddy/acme_issuers.go
func buildACMEIssuers(cfg ports.TLSConfig) []map[string]any
```

The function signature is unchanged; the behaviour changes are purely
internal. To capture the *reason* an issuer was skipped (needed for the log
event), the pure function returns an additional value:

```go
// SkippedIssuer records an issuer that was evaluated for the default chain
// but excluded. The dev emits one tls.acme.chain_skipped event per entry.
type SkippedIssuer struct {
    Provider string // e.g. "zerossl"
    Reason   string // e.g. "email_not_configured"
}

func buildACMEIssuers(cfg ports.TLSConfig) (issuers []map[string]any, skipped []SkippedIssuer)
```

The second return value is `nil` for all non-default paths (explicit
`provider: zerossl`, `provider: buypass`, `acme_ca` override, etc.), keeping
existing call sites straightforward. The caller in the plugin Init emits one
log event per `SkippedIssuer` plus the unconditional `chain_configured` event.

Callers that do not care about the skipped slice (`multisite_config.go`) can
discard it with `issuers, _ := buildACMEIssuers(tlsCfg)` — no behaviour change.

#### 7. File layout

| Path | Change |
|------|--------|
| `internal/adapters/caddy/acme_issuers.go` | Remove `acmeCABuypass` from default chain; add email-preflight for ZeroSSL; change signature to return `(issuers, skipped)`. |
| `internal/adapters/caddy/acme_issuers_test.go` | Extend table with 4 new rows (see test plan). |
| `internal/adapters/caddy/multisite_config.go` | Update call site to accept the 2-value return (`issuers, _ :=`). |
| `internal/plugins/tls/plugin.go` | Mirror the two behaviour changes; add a mirror banner comment; call site in `Init` emits the 3–4 new events via `ports.EventLogger`. |
| `internal/plugins/tls/plugin_test.go` | Mirror the extended table; add slog-assertion tests for the new events. |
| `internal/domain/events/events.go` | Add four event-type constants. |
| `internal/domain/events/tls_acme.go` | **NEW** — four constructor functions `NewTLSACMEChainSkipped`, `NewTLSACMEChainFallback`, `NewTLSACMEChainConfigured`, `NewTLSACMEProviderDeprecated`. |
| `internal/domain/events/tls_acme_test.go` | **NEW** — table-driven constructor tests. |
| `internal/schema/v1/event.json` | Add four payload `$defs` and four `allOf` branches. |
| `internal/schema/v1/schema_test.go` | Add conformance assertions for the four new event types. |
| `internal/mcp/schema_registry.go` | Register the four new event types. |
| `internal/mcp/schema_registry_test.go` | Assert the four new event types are registered. |
| `internal/plugins/catalog.go` | Update the TLS plugin description: remove Buypass from the default-chain phrasing; note it is opt-in only. |
| `internal/plugins/tls/plugin.go` (Init) | Pass `ports.EventLogger` through when not nil; emit the events on Init. |
| `docs/getting-started/tls.md` (or equivalent) | Writer agent updates user-facing docs to match. |

#### 8. Application-service flow (plugin Init sequence)

1. `Plugin.Init(ctx)` is called with a non-nil `ports.EventLogger` (passed via
   `New(cfg, eventLog, logger)` — already exists).
2. `Init` calls `validateTLSConfig(cfg)` — unchanged; ZeroSSL-as-primary still
   requires email.
3. `Init` calls `buildACMEIssuers(cfg)` → `(issuers, skipped)`.
4. For each `sk` in `skipped`: `eventLog.Log(ctx, events.NewTLSACMEChainSkipped(...))`.
5. If `cfg.Provider == ports.TLSProviderBuypass`:
   `eventLog.Log(ctx, events.NewTLSACMEProviderDeprecated(...))`.
6. `eventLog.Log(ctx, events.NewTLSACMEChainConfigured(...))` — always, so an
   operator can grep for one event type and see the resolved chain regardless
   of whether anything was skipped.
7. The resolved `issuers` are wired into `buildACMETLSApp` as today.

The `tls.acme.chain_fallback` event (runtime issuer transition) is emitted
only if the dev can confirm a Caddy/certmagic/acmez callback exists. If not,
the dev must document the limitation in-code and note it in the PR
description so it is not silently dropped.

#### 9. Error cases

| Scenario | Handling |
|----------|----------|
| `provider: letsencrypt`, `email: ""` | Default chain = `[letsencrypt]`. Emit `chain_skipped` (zerossl, email_not_configured) + `chain_configured`. No error. |
| `provider: letsencrypt`, `email: "x@y"` | Default chain = `[letsencrypt, zerossl]`. Emit only `chain_configured`. |
| `provider: letsencrypt`, `acme_ca: "..."` | Single issuer with custom CA. No chain events (single-issuer path is explicit operator choice). |
| `provider: zerossl`, `email: ""` | Validation error from `validateTLSConfig`, unchanged from #1026. |
| `provider: zerossl`, `email: "x@y"` | Single issuer ZeroSSL. Emit `chain_configured` with resolved_chain=["zerossl"]. |
| `provider: buypass` | Single issuer Buypass. Emit `provider_deprecated` + `chain_configured`. |
| `provider: letsencrypt-staging` | Single issuer. Emit `chain_configured` only. |
| `eventLog == nil` | Init must not panic. Skip all event emissions, keep the warning `logger.Warn` for slog-only observability. |

#### 10. Test strategy

Unit tests only. Chain selection is a pure function over `ports.TLSConfig`;
no I/O, no Caddy runtime involvement.

**`internal/adapters/caddy/acme_issuers_test.go` (extend existing table):**

| Row | cfg | expected_chain | expected_skipped |
|-----|-----|----------------|------------------|
| 1 | `{Provider: letsencrypt}` (no email) | `[LE]` | `[{zerossl, email_not_configured}]` |
| 2 | `{Provider: letsencrypt, Email: "x@y"}` | `[LE, ZeroSSL]` | `[]` |
| 3 | `{Provider: letsencrypt, ACMECA: "custom"}` | `[custom]` | `[]` |
| 4 | `{Provider: letsencrypt, ACMECA: "custom", Email: "x@y"}` | `[custom]` | `[]` |
| 5 | `{Provider: zerossl, Email: "x@y"}` | `[ZeroSSL]` | `[]` |
| 6 | `{Provider: buypass}` | `[Buypass]` | `[]` |
| 7 | `{Provider: letsencrypt-staging}` | `[LE staging]` | `[]` |
| 8 | (existing) buypass URL never appears in `[letsencrypt, no-override]` output | assertion: `acmeCABuypass` not in returned `ca` fields for any default-chain row | — |

**`internal/plugins/tls/plugin_test.go` (mirror the table + slog
assertions):**

- Mirror rows 1–7 against the plugin-package `buildACMEIssuers`.
- Add slog-handler-based assertions that Init emits:
  - `tls.acme.chain_skipped` with `provider=zerossl`, `reason=email_not_configured` for row 1.
  - `tls.acme.chain_configured` for every case.
  - `tls.acme.provider_deprecated` for row 6.
- Verify Init does not panic when `eventLog == nil`.

**`internal/domain/events/tls_acme_test.go`** — table-driven constructor
tests, matching the style of `events_test.go` for other TLS events. Assert
schema-version, event-type, severity, category, and payload shape.

**`internal/schema/v1/schema_test.go`** — extend the existing conformance
test to cover the four new event types (serialize a sample event of each
type, validate against `event.json`).

**No integration tests required.** Caddy's own issuer-selection behaviour is
unchanged; a full end-to-end test against a real ACME directory has never
been part of CI and is out of scope per PM.

#### 11. New dependencies

None. All changes use existing domain types, the existing `ports.EventLogger`
interface, and Go stdlib. No third-party library is added.

### Consequences

**Reliability**
- Default `provider: letsencrypt` deployments recover faster when LE is
  unreachable, because the chain no longer wastes attempts on a 403-returning
  Buypass endpoint.
- Default deployments without `tls.email` no longer attempt a doomed ZeroSSL
  handshake; they degrade to single-issuer LE (pre-ADR-079 behaviour) with a
  clear log event explaining why.

**Observability**
- Three new structured event types (`chain_skipped`, `chain_configured`,
  `provider_deprecated`) are published as part of the v1 event schema. Per
  CLAUDE.md "Treat schema stability with the same care as a public API": these
  event types are frozen from the moment this ADR lands. Adding new `reason`
  values is backward-compatible; renaming fields is not.
- A fourth type (`chain_fallback`) is reserved in the schema but emitted only
  if Caddy/certmagic exposes a usable hook; if not, the dev documents the gap
  and emits `chain_configured` alone.

**Backward compatibility**
- **Breaking for users who relied on Buypass as a silent default fallback.**
  Anyone using `provider: letsencrypt` who was transparently recovering via
  Buypass during LE outages will lose that path. Given Buypass is currently
  returning 403, this is a no-op for every current deployment; documented in
  release notes regardless.
- `provider: buypass` (explicit) continues to work; a deprecation log event is
  emitted on Init.
- Empty `tls.email` under `provider: letsencrypt` previously produced a
  transient ZeroSSL failure; now produces a deterministic single-issuer LE
  chain. Strictly more predictable; visible behaviour change.

**Hexagonal discipline**
- Duplication of `buildACMEIssuers` between `internal/adapters/caddy/` and
  `internal/plugins/tls/` remains. A mirror-banner comment is added to make
  drift easier to spot in code review. A follow-up issue should be filed to
  extract a shared pure-Go helper package (proposed location:
  `internal/domain/tls/acme/`), deferred per PM scope.

**Deferred**
- **(c) LE rate-limit preflight** → #1057, needs its own ADR.
- **De-duplication** of the two chain-builder copies → new follow-up issue.

### Alternatives considered

1. **Keep Buypass in the default chain but mark it "degraded".**
   Rejected: no mechanism exists in Caddy to "try only briefly" before
   falling through. Keeping a 403-returning endpoint in the chain is dead
   weight by construction. If Buypass recovers, users can opt back in via
   `provider: buypass` or via a future `tls.acme_providers: [letsencrypt,
   zerossl, buypass]` escape hatch (out of scope).

2. **Treat empty `tls.email` as a validation error when
   `provider: letsencrypt`.**
   Rejected: `tls.email` is legitimately optional for LE-only deployments
   that do not care about expiry notifications. Elevating to a validation
   error would break existing valid configs. Runtime-skip + log is the
   minimally-disruptive path.

3. **Introduce `tls.acme_providers: []string` config field to let users
   compose the chain explicitly.**
   Rejected as out-of-scope: PM #1055 explicitly scopes the fix to "no new
   config fields". A chain-composition field is worth considering after
   #1057 settles the preflight design; file as a follow-up.

4. **De-duplicate `buildACMEIssuers` into a shared
   `internal/domain/tls/acme/` package in this PR.**
   Rejected: adds a new package + import surface, not required to fix the
   two defects, expands review burden, and PM explicitly defers it. Mirror-
   banner comment is the minimum-risk safeguard for the duplicate.

5. **Emit the fallback event by polling Caddy's admin API for issuer
   state.**
   Rejected: admin-API polling is a new network dependency on localhost and
   does not give real-time transition signals. If no callback hook exists,
   the dev emits `chain_configured` at Init and documents the limitation
   rather than introducing a polling loop.
