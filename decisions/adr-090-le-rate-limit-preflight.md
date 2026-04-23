# ADR-090: Let's Encrypt rate-limit preflight via Certificate Transparency

**Date**: 2026-04-23
**Issue**: #1057
**Status**: Accepted
**Accepted**: 2026-04-23 — feature shipped in PR #1118

## Context

Let's Encrypt (LE) enforces a hard limit of **5 certificates per registered
domain per 168-hour window**
(<https://letsencrypt.org/docs/rate-limits/#new-certificates-per-registered-domain>).
When a deployment hits this limit, the ACME fallback chain shipped in ADR-079
/ ADR-083 silently retries against ZeroSSL (which then fails for other
reasons) before surfacing a useful error. In the v0.15.0 fresh-agent retro
this produced ~10 minutes of production downtime with no advance warning —
the class of failure operators discover only after the site is already down.

The v0.16 hardening (ADR-083) removed Buypass from the default chain and
added ZeroSSL email preflight. It explicitly deferred the rate-limit story
to this ADR. The PM spec (#1057) then asked the architect to resolve nine
specific design questions before dev picks up.

### Locked constraints from prior ADRs

- **CLAUDE.md — sidecar locality**: the sidecar is *always local, localhost
  only*. The preflight is run by a human via `vibew doctor` from an
  operator workstation; the sidecar itself never initiates the outbound
  HTTPS call. This matches the existing `vibew bundle` / `vibew deploy`
  outbound pattern (docker image pulls, health checks) and does not violate
  the locality constraint. See §D below.
- **ADR-079 / ADR-083 — fallback chain**: only `provider: letsencrypt` uses
  LE as the primary issuer. `zerossl`, `buypass`, `letsencrypt-staging`,
  `self-signed`, and `external` are out of scope for this check.
- **ADR-084 — doctor port ownership**: `CheckResult` / `Severity` / section
  model is the contract every new check follows. No new surface here.
- **ADR-089 — bundle image health exit codes**: bundle uses distinct exit
  codes (2 = image missing, 3 = daemon down). `vibew doctor` has its own
  convention — exit 0 on all-OK, exit 1 on any FAIL. This ADR keeps doctor
  semantics unchanged; no new exit code is introduced.
- **CLAUDE.md — dependency rules**: any new direct dependency must be
  Apache-2.0, MIT, BSD-2, or BSD-3.

### Existing ACME email preflight pattern

`checkACMEEmail` (`internal/app/ops/doctor.go:432`) is the template: a
pure function over `*config.Config`, returns a `CheckResult`, added into
`runChecks` with `withSection(...)`. The LE rate-limit preflight slots in
next to it, with one addition: it depends on an outbound HTTP call, so it
needs an injected port (`CertTransparencyQuerier`).

## Decision

Add a single new doctor check — **"LE rate-limit: `<domain>`"** — that
queries the crt.sh Certificate Transparency JSON API to count certificates
issued for the registered domain in the last 168 hours. The check runs
only when `tls.provider == "letsencrypt"` and is skipped otherwise. It
emits a severity-mapped `CheckResult` following the existing doctor
conventions. No new top-level subcommand is introduced.

The check composes two new pieces:

1. A new outbound port `ports.CertTransparencyQuerier` and a crt.sh
   adapter `internal/adapters/crtsh.CrtShClient`.
2. A new application package `internal/app/tlspreflight` exposing a
   `Service.Check(ctx, domains []string) ([]Result, error)` and its
   `Result` value object. `DoctorService` holds an optional pointer to
   this service and, when wired, runs one check per configured domain.

A new pure-Go domain package `internal/domain/tlspreflight` holds the
`Result`, `Budget`, and threshold constants — no external deps.

### PM decision-question resolutions

Sub-decisions from the PM spec §4 / the architect prompt §2:

#### (a) Command surface — doctor check

**Chosen: doctor check (PM option A).** No new top-level subcommand. No
`--tls-preflight` flag.

Rationale:

- Reuses `CheckResult` / section / `--json` infrastructure from ADR-084.
  No new output format to spec or test.
- `vibew doctor` is already the "pre-deploy diagnostic" entry point; users
  run it before `vibew bundle`. Adding the check here is discoverable by
  definition.
- A standalone `vibew tls preflight` subcommand would duplicate the flag
  parsing, config loading, and JSON marshalling already in `doctor.go` for
  a check that takes ~300 ms on the happy path.
- A pre-deploy hook (blocking `vibew bundle` on preflight FAIL) is
  explicitly out of scope per PM §5. If we want that later, this ADR's
  `tlspreflight.Service.Check(...)` is the reusable entry point — the
  bundle CLI can call it directly.

Integration point: `DoctorService.runChecks` appends one `CheckResult`
per domain to the `sectionConfigDocker` group, immediately after the
existing `checkACMEEmail` call. The check is named
`"LE rate-limit: <domain>"` (AC-7).

#### (b) Data source — crt.sh JSON API

**Chosen: crt.sh JSON API (PM option A).**

Authoritative source for the 5-certs-per-registered-domain-per-168h rule:
the crt.sh public database aggregates all Certificate Transparency logs
that LE publishes to
(<https://letsencrypt.org/docs/ct-logs/>). Every certificate LE issues
shows up in CT logs within minutes; counting them is the simplest source
of truth that does not require an LE account.

Request format:

```
GET https://crt.sh/?Identity=<registered-domain>&exclude=expired&output=json
```

Notes:

- `Identity` is preferred over `q` because `q` returns partial matches
  (e.g. `exampleapp.com` when searching `example.com`). `Identity` queries
  the `commonName` / `nameValue` columns exactly (AC matches literal domain
  and `*.domain` wildcards).
- `&exclude=expired` is a crt.sh performance hint. It does not affect
  correctness — the 168 h window is well inside any cert's validity.
- No API key, no auth, public endpoint, CORS enabled.

Response format: a JSON array of objects; the fields we consume are
`not_before` (RFC 3339), `issuer_name`, `common_name`, `name_value`.

Parser contract (pure function in `internal/domain/tlspreflight/parse.go`):

```go
// CountIssuedSince returns the number of certificates in records whose
// not_before is strictly after threshold AND whose issuer_name contains
// "Let's Encrypt". It also returns the oldest not_before that is still
// within the window — this is the timestamp the rate limit releases a
// slot at (oldest_not_before + 168h).
func CountIssuedSince(records []CrtShRecord, threshold time.Time) (count int, oldestInWindow time.Time)
```

Issuer filter: `strings.Contains(strings.ToLower(rec.IssuerName), "let's encrypt")`.
ZeroSSL / Buypass / Sectigo certs do not count toward the LE limit and
must be excluded. (Users with mixed issuance history are a realistic case
— e.g. a site that switched from Sectigo to LE three months ago.)

Error handling (adapter-level — `internal/adapters/crtsh/client.go`):

| Case | Behaviour |
|---|---|
| Network timeout / DNS failure / connection refused | return `tlspreflight.ErrCTUnavailable` wrapped with context |
| HTTP status ≠ 200 | return `tlspreflight.ErrCTUnavailable` wrapped with the status |
| Empty body | treated as "zero certs found" — **not** an error |
| `Content-Type` not JSON | return `tlspreflight.ErrCTResponseMalformed` |
| JSON decode error | return `tlspreflight.ErrCTResponseMalformed` |
| crt.sh HTTP 429 (rate-limited by crt.sh itself) | return `tlspreflight.ErrCTThrottled` wrapped |

The application service maps all three error sentinels to `SeverityWarn`
with a domain-aware message (AC-8). It never returns an error upward.

#### (c) Blocking vs advisory — FAIL → doctor exits 1

**Chosen: hard block via severity, consistent with doctor convention
(PM option B).**

`vibew doctor` already exits 1 when any check is `SeverityFail`. The
preflight check emits `SeverityFail` at 5/5 and `SeverityWarn` at 4/5;
this inherits the existing doctor exit-code behaviour with zero changes
to `Run`, `runChecks`, or the CLI wrapper.

Severity mapping (AC-4):

| Certs in 168h | Remaining | Severity | Detail |
|---|---|---|---|
| 0 | 5 | OK | `5 of 5 slots free for <domain>` |
| 1 | 4 | OK | `4 of 5 slots remaining for <domain>` |
| 2 | 3 | OK | `3 of 5 slots remaining for <domain>` |
| 3 | 2 | OK | `2 of 5 slots remaining for <domain>` |
| 4 | 1 | **WARN** | `1 of 5 slots remaining this week for <domain>` |
| 5 | 0 | **FAIL** | `LE rate limit exhausted for <domain>; next slot at <time>; use --skip-le-preflight to bypass` |
| >5 (shouldn't happen; defensive) | 0 | FAIL | same as 5 with `(observed %d issuances)` suffix |

"Next slot" time = `oldestNotBeforeInWindow.Add(168 * time.Hour)` rendered
in local time (`time.RFC1123`). This is the earliest moment LE will
accept a new issuance, because the oldest cert in the window will then
fall outside it.

No new doctor exit code is added. `vibew doctor --json` inherits the
standard `CheckResult` schema automatically (AC-9).

#### (d) False-negative handling — two opt-outs

**Chosen: both `--skip-le-preflight` flag and `tls.skip_rate_limit_check`
config (PM option C).**

Flag: `--skip-le-preflight` on `vibew doctor`. When present, the check is
skipped without being added to the results array — same pattern as
`imageChecker == nil` in the existing code. JSON output does not include
a "skipped" entry to avoid polluting diff output for scripts.

Config: `tls.skip_rate_limit_check: bool` (new field in `config.TLSConfig`).
Defaults to `false`. When `true`, the check is skipped with the same
semantics as the flag.

Semantics are identical ("bypass the check entirely — do not run, do not
log"). A future enhancement could add `--allow-rate-limit` to log-but-not-fail;
deferred, not in scope for v1.

Error message at FAIL explicitly names `--skip-le-preflight` verbatim in
the `Detail` field (AC-6). The flag name and the config key name are
**frozen** by this ADR — renaming either is a breaking change for scripts.

#### (e) Domain enumeration — eTLD+1 from cfg.TLS.Domain, multi-site aware

**Chosen: derive the registered-domain set from `cfg.TLS.Domain`,
transformed via `publicsuffix.EffectiveTLDPlusOne`.**

The LE rate limit applies to the **registered domain** (eTLD+1), not the
FQDN (PM open question — see §7). `app.example.com` and `api.example.com`
share a single 5-cert budget under `example.com`. Querying crt.sh with
the FQDN would miss issuances against sibling subdomains and produce
false-negative results.

Domain enumeration rules:

1. Single-site mode: `cfg.TLS.Domain` → `publicsuffix.EffectiveTLDPlusOne(domain)`.
   Check runs once for the registered domain.
2. Multi-site mode (ADR-068): iterate `site.Config().TLS.Domain` for every
   `site` in the registry. Deduplicate at the eTLD+1 level (two sites on
   `app.example.com` and `api.example.com` produce one check, not two).
3. Wildcard / SAN handling: out of scope per PM §5 — we only query the
   single `cfg.TLS.Domain` value. Primary + www is NOT auto-expanded.
   A user with `www.example.com` and `example.com` pointing at two sites
   still sees one check (both collapse to `example.com`).

Implementation: `tlspreflight.Service` takes `[]string` FQDNs. The caller
(doctor wiring) is responsible for deduplication via `golang.org/x/net/publicsuffix`.

The resulting check names follow AC-7: `"LE rate-limit: <registered-domain>"`,
one `CheckResult` per unique registered domain.

#### (f) Network / outbound policy — operator-initiated, allowed

**Chosen: outbound HTTPS to crt.sh is allowed under `vibew doctor`,
because the command is operator-initiated and runs from the operator's
machine.**

The CLAUDE.md locality constraint applies to the **sidecar process** (the
long-running Go binary that Caddy is embedded in). `vibew doctor` is a
short-lived CLI invocation initiated by a human operator. It is not the
sidecar. `vibew bundle` already makes outbound calls (docker image pull,
health checks), and `vibew deploy` makes outbound SSH calls — this is
consistent with precedent.

The preflight adapter is never instantiated by the sidecar's `Plugin.Init`
or by anything called from `internal/plugins/tls/plugin.go`. It is only
wired by `internal/cli/cmd/doctor.go`. Grep contract — dev and reviewer
must confirm:

```
grep -rn "crtsh\." internal/plugins/ internal/adapters/caddy/ → must return zero matches
```

#### (g) Caching — none in v1

**Chosen: no caching.**

CT log propagation delay is measured in minutes. Within a single `vibew
doctor` invocation the check runs once per registered domain and the
result is used once; there is no within-process cache to add.

Cross-invocation caching is deliberately rejected for v1:

- A stale cached result that misses a recent issuance is the
  false-negative case we are trying to prevent.
- crt.sh query latency at p50 is ~200–400 ms — acceptable for a doctor
  command.
- Disk caching adds filesystem side-effects to `vibew doctor` which is
  currently pure-read-only (modulo stdout). Avoid without evidence of
  need.

If crt.sh throughput becomes a problem, an in-memory cache keyed on
`(registeredDomain, bucket=floor(now/5min))` can be added later without
changing the port shape.

#### (h) Privacy — acknowledged, no mitigation in v1

**Chosen: accept the privacy trade-off.**

crt.sh queries are logged and public. Querying `example.com` telegraphs
that the operator is about to issue or renew a cert for that domain. This
leak is not a new risk:

- The certificate, once issued, is itself published to CT logs and is
  publicly searchable on crt.sh.
- Any operator running `vibew doctor` for a domain has (or will shortly
  have) DNS pointing at their infrastructure — the domain is already
  public.
- The preflight does not send any data beyond the domain name.

Document the behaviour in `docs/cli.md` (doctor section) and in the
check's `Detail` field when skipped: "LE rate-limit check queries the
public crt.sh CT log; skip with `--skip-le-preflight` if this is not
desired."

No mitigation is required. A future Tor-via-SOCKS escape hatch is
deferred.

#### (i) LE-specific coupling — skip silently for non-LE providers

**Chosen: skip silently (no `CheckResult` appended) when
`cfg.TLS.Provider != "letsencrypt"`.**

Rationale: adding a `CheckResult{Severity: OK, Detail: "skipped — not
using LE"}` per non-LE deployment would pollute output for every user
running self-signed / external / ZeroSSL / staging. ADR-083 already
taught us that ZeroSSL has no comparable per-identifier public CT budget,
and Buypass / staging have their own limits that we do not track.

The check is added to the results slice only when:

1. `cfg.TLS.Enabled == true`, AND
2. `cfg.TLS.Provider == string(ports.TLSProviderLetsEncrypt)`, AND
3. `cfg.TLS.Domain != ""`, AND
4. `cfg.TLS.ACMECA == ""` (override disables default chain — ADR-079),
   AND
5. `cfg.TLS.SkipRateLimitCheck == false`, AND
6. the `--skip-le-preflight` flag is not set.

When the provider is `letsencrypt-staging`, the check is also skipped —
the staging endpoint has its own (much higher) limits and is out of
scope.

### Domain model

`internal/domain/tlspreflight/` — pure, no external imports except stdlib
and `golang.org/x/net/publicsuffix` (BSD-3, already indirect in go.mod).

```go
// internal/domain/tlspreflight/result.go

// Status is the verdict of a single preflight check.
type Status string

const (
    StatusOK   Status = "OK"
    StatusWarn Status = "WARN"
    StatusFail Status = "FAIL"
)

// Budget is the hardcoded Let's Encrypt per-registered-domain budget.
// Values are frozen in this ADR — changing them is a new ADR.
const (
    BudgetWindow     = 168 * time.Hour
    BudgetCapacity   = 5
    WarnRemainingAt  = 1 // remaining <= 1 → WARN
    FailRemainingAt  = 0 // remaining <= 0 → FAIL
)

// Result is the outcome of checking a single registered domain.
// All fields are populated on success; on error, Err is non-nil and
// other fields are zero-valued except Domain and Status.
type Result struct {
    Domain              string    // eTLD+1, e.g. "example.com"
    Status              Status    // OK / WARN / FAIL
    IssuedInWindow      int       // count of LE certs in last 168h
    RemainingBudget     int       // BudgetCapacity - IssuedInWindow, floored at 0
    OldestInWindow      time.Time // not_before of oldest cert still in window (zero when none)
    NextSlotAvailableAt time.Time // OldestInWindow + 168h (zero when RemainingBudget > 0)
    Detail              string    // rendered detail string for CheckResult.Detail
    Err                 error     // set when the query failed; Status is WARN in that case
}
```

Error sentinels (in the same package):

```go
var (
    ErrCTUnavailable       = errors.New("crt.sh unreachable")
    ErrCTResponseMalformed = errors.New("crt.sh response malformed")
    ErrCTThrottled         = errors.New("crt.sh throttled request")
)
```

No aggregate, no domain event. `Result` is a pure value object.

### Ports (interfaces)

One new port, added as a new file `internal/ports/cert_transparency.go`
so it does not pollute the TLS-plugin-oriented `ports/tls.go`:

```go
// CertTransparencyQuerier returns the raw certificate records published
// to Certificate Transparency logs for a single registered domain.
// Implementations shell out to the crt.sh public JSON API.
//
// Query MUST use the registered domain (eTLD+1), not a FQDN. Callers are
// responsible for normalising the input via publicsuffix.
//
// Query returns tlspreflight.ErrCTUnavailable on network failure,
// tlspreflight.ErrCTThrottled on crt.sh 429, and
// tlspreflight.ErrCTResponseMalformed on any decode/content-type error.
type CertTransparencyQuerier interface {
    Query(ctx context.Context, registeredDomain string) ([]CrtShRecord, error)
}

// CrtShRecord is the slim projection of a crt.sh row that the preflight
// needs. Parallel to ports.ImageInfo in ADR-089 — declared locally so
// the ports layer does not import app/ or domain/.
type CrtShRecord struct {
    NotBefore  time.Time
    IssuerName string
    CommonName string
    NameValue  string
}
```

### Adapters

One new adapter: `internal/adapters/crtsh/client.go`.

```go
// Client implements ports.CertTransparencyQuerier by calling crt.sh over
// HTTPS. The client is configured with a single *http.Client; callers
// pass a timeout-bounded client (default 10s per AC-8).
type Client struct {
    http *http.Client
    base string // default "https://crt.sh"
}

func NewClient(httpClient *http.Client) *Client
func NewClientWithBase(httpClient *http.Client, base string) *Client // tests
func (c *Client) Query(ctx context.Context, registeredDomain string) ([]ports.CrtShRecord, error)
```

Request:

```
GET {base}/?Identity={urlescape(registeredDomain)}&exclude=expired&output=json
Header: User-Agent: vibew-doctor/<semver>
Header: Accept: application/json
```

Response parsing: decode JSON array of objects; map each to
`ports.CrtShRecord`. `not_before` is parsed as RFC 3339 — on per-row
parse failure, the row is skipped (logged at `debug`) rather than failing
the whole query, so one malformed row in a 40-record response does not
drop the check to WARN.

Timeout: `http.Client.Timeout` is the source of truth. The adapter does
not create its own timeout; the caller (CLI wiring) constructs the
`*http.Client` with `Timeout: 10 * time.Second`.

User-Agent string: `fmt.Sprintf("vibew-doctor/%s (+https://vibewarden.dev)", version.Get())`.
crt.sh is community-operated; sending an honest UA is polite and helps
the operator reach us if we ever cause load problems.

### Application service

`internal/app/tlspreflight/service.go`:

```go
// Service orchestrates the LE rate-limit preflight. It is stateless and
// safe for concurrent use — calls fan out one crt.sh query per domain.
type Service struct {
    ct    ports.CertTransparencyQuerier
    clock func() time.Time // injected for tests; defaults to time.Now
}

func NewService(ct ports.CertTransparencyQuerier) *Service
func (s *Service) WithClock(fn func() time.Time) *Service

// Check runs the preflight for each registered domain. Domains must
// already be normalised to eTLD+1; the service does not re-normalise
// (that is the CLI wiring's job).
//
// Returns one Result per input domain, in the same order. A query
// failure produces a Result with Status=WARN, Err set, and Detail
// populated with the "crt.sh unreachable" message per AC-8.
func (s *Service) Check(ctx context.Context, registeredDomains []string) []tlspreflight.Result
```

The service does not fan out in parallel in v1 — sequential queries keep
the code simple and crt.sh rate-limit friendly. N is small (1–3 in
practice). If N grows (fleet mode later), a bounded worker pool is easy
to add without changing the port.

### File layout

Every new file listed. Edits to existing files marked with `+`.

```
internal/
  domain/
    tlspreflight/                               # NEW
      result.go                                 # NEW: Status, Result, Budget consts, error sentinels
      result_test.go                            # NEW: Result.RenderDetail goldens
      parse.go                                  # NEW: CountIssuedSince pure function
      parse_test.go                             # NEW: table-driven threshold + issuer-filter tests
  ports/
    cert_transparency.go                        # NEW: CertTransparencyQuerier, CrtShRecord
  adapters/
    crtsh/                                      # NEW package
      client.go                                 # NEW: HTTP adapter
      client_test.go                            # NEW: httptest.Server-driven tests
      doc.go                                    # NEW: package godoc
  app/
    tlspreflight/                               # NEW package
      service.go                                # NEW: Service, Check
      service_test.go                           # NEW: fake querier, all severity branches
      doc.go                                    # NEW: package godoc
    ops/
      doctor.go                                 # + field, + WithLERateLimitService setter,
                                                #   + checkLERateLimit per-domain loop, + skip flag option
      doctor_test.go                            # + table cases for the new check
  config/
    config.go                                   # + TLSConfig.SkipRateLimitCheck field (default false)
  cli/
    cmd/
      doctor.go                                 # + --skip-le-preflight flag,
                                                #   + crtsh adapter wiring,
                                                #   + tlspreflight service wiring,
                                                #   + publicsuffix normalisation of domains
      doctor_test.go                            # + CLI-level test of the flag gating
```

Go module changes (`go.mod`):

```
golang.org/x/net → promoted from indirect to direct (BSD-3, already vendored)
```

Docs updated alongside the PR (writer agent owns this):

```
docs/cli.md                                     # + "LE rate-limit preflight" subsection + exit code note
docs/llms-full.txt                              # + preflight in doctor section
AGENTS-VIBEWARDEN.md (+ generated copies)       # + brief mention
CHANGELOG.md                                    # Added: preflight; Changed: doctor exits 1 at 5/5
```

### Sequence (request/response flow)

For a single-site, `provider: letsencrypt`, `tls.domain: app.example.com`
deployment with 4 certs issued in the last 168 h:

1. `vibew doctor` (optionally with `--skip-le-preflight`) is invoked.
   CLI loads config via `config.Load`.
2. CLI constructs the real adapters — existing compose / port / owner /
   TLS-state plus the new `crtsh.NewClient(&http.Client{Timeout: 10s})`
   and `tlspreflight.NewService(crtsh)`.
3. CLI normalises `cfg.TLS.Domain` — `publicsuffix.EffectiveTLDPlusOne("app.example.com")`
   → `"example.com"`. Result list deduplicated.
4. CLI builds `DoctorOptions` with the normalised domain list and the
   skip-flag boolean.
5. `DoctorService.Run` → `runChecks` → after `checkACMEEmail`, runs the
   new `checkLERateLimit` helper. Gating per §(i) — if any guard fails,
   no check is emitted.
6. `checkLERateLimit` calls `tlspreflight.Service.Check(ctx, domains)`.
7. Service iterates domains, calling `CertTransparencyQuerier.Query` for
   each. For `example.com`, crt.sh returns an array of ~12 records
   (some Sectigo, some LE).
8. Service passes records to `tlspreflight.CountIssuedSince(records,
   now.Add(-168h))` → `(count=4, oldestInWindow=2026-04-18T11:04Z)`.
9. Service constructs `Result{Domain: "example.com", Status: WARN,
   IssuedInWindow: 4, RemainingBudget: 1, ...}` and renders `Detail`:
   `"1 of 5 slots remaining this week for example.com"`.
10. `checkLERateLimit` maps `Result` → `CheckResult{Name: "LE
    rate-limit: example.com", Severity: WARN, Detail: ..., Section:
    "Config & Docker"}`.
11. Result appended to `results` slice. Flow continues to existing Layer
    2 checks.
12. Doctor renders report. WARN contributes nothing to exit code — doctor
    exits 0 because no check is FAIL.

For a 5/5 scenario, step 9 produces `Status: FAIL`, step 10 maps to
`SeverityFail`, step 12 makes doctor exit 1. `Detail` includes the
`--skip-le-preflight` flag name verbatim (AC-6).

For a crt.sh timeout:

1–7 as above.
8. `Query` returns `tlspreflight.ErrCTUnavailable`.
9. Service constructs `Result{Domain: "example.com", Status: WARN, Err:
   ..., Detail: "crt.sh unreachable — rate-limit check skipped; run with
   --skip-le-preflight to suppress"}`.
10. Maps to `SeverityWarn`. Doctor exits 0 (AC-8).

### Error cases

| Case | Result.Status | CheckResult.Severity | Detail |
|---|---|---|---|
| happy path, 0–3 issued | OK | OK | `N of 5 slots remaining` |
| 4 issued | WARN | WARN | `1 of 5 slots remaining this week` |
| 5 issued | FAIL | FAIL | `LE rate limit exhausted; next slot at <time>; use --skip-le-preflight to bypass` |
| crt.sh timeout | WARN | WARN | `crt.sh unreachable — rate-limit check skipped; run with --skip-le-preflight to suppress` |
| crt.sh 429 | WARN | WARN | `crt.sh throttled — try again in a few minutes; run with --skip-le-preflight to suppress` |
| crt.sh malformed JSON | WARN | WARN | `crt.sh returned unexpected response — rate-limit check skipped` |
| `cfg.TLS.Enabled == false` | n/a | (not emitted) | — |
| `cfg.TLS.Provider != letsencrypt` | n/a | (not emitted) | — |
| `cfg.TLS.Domain == ""` | n/a | (not emitted) | — |
| `cfg.TLS.ACMECA` set | n/a | (not emitted) | — |
| `--skip-le-preflight` flag | n/a | (not emitted) | — |
| `tls.skip_rate_limit_check: true` | n/a | (not emitted) | — |
| `publicsuffix.EffectiveTLDPlusOne` errors (e.g. single label) | n/a | SeverityWarn | `cannot derive registered domain from "<x>" — rate-limit check skipped` |
| Multi-site: two sites share the same eTLD+1 | one Result | one CheckResult | deduplicated |

### Test strategy

- **Unit tests** (all next to code, table-driven):
  - `internal/domain/tlspreflight/parse_test.go` — `CountIssuedSince`
    table: empty input, all outside window, mix of LE + Sectigo (verifies
    issuer filter), records with malformed `not_before` (skipped), exact
    168h boundary (exclusive, not inclusive — `After` not `!Before`),
    `oldestInWindow` correctness, count > 5 clamp.
  - `internal/domain/tlspreflight/result_test.go` — `Result.RenderDetail`
    goldens for every severity path + the five error cases. Fixed clock
    via injected `now`.
  - `internal/app/tlspreflight/service_test.go` — fake querier returns
    per-domain canned responses. Covers all threshold transitions
    (0/1/2/3/4/5), all three error sentinels, injected clock for
    deterministic "next slot" time rendering, multi-domain input with
    mixed outcomes.
  - `internal/adapters/crtsh/client_test.go` — `httptest.NewTLSServer`
    (and plain httptest.Server under custom base) driven tests:
    - 200 with realistic JSON → parsed records.
    - 200 with empty array `[]` → empty records, no error.
    - 200 with malformed body → `ErrCTResponseMalformed`.
    - 200 with `Content-Type: text/html` → `ErrCTResponseMalformed`.
    - 429 → `ErrCTThrottled`.
    - 500 → `ErrCTUnavailable`.
    - Network error via context cancel → `ErrCTUnavailable` wrapped.
    - URL escaping: domain with `*` (wildcard) is properly encoded.
    - User-Agent header sent (assert on request).
  - `internal/app/ops/doctor_test.go` — extended:
    - `tls.provider == letsencrypt`, valid `Domain`, fake service returns
      WARN → single `CheckResult{Severity: WARN}` appended.
    - `tls.provider == letsencrypt`, `SkipRateLimitCheck == true` → no
      CheckResult appended.
    - `tls.provider == self-signed` → no CheckResult appended.
    - `tls.provider == letsencrypt`, multiple sites, shared eTLD+1 →
      single CheckResult appended.
    - `tls.provider == letsencrypt`, invalid domain (single label) →
      SeverityWarn with "cannot derive registered domain" detail.
  - `internal/cli/cmd/doctor_test.go` — smoke test that the
    `--skip-le-preflight` flag suppresses the check (drives via fake
    service that would otherwise emit FAIL, asserts no FAIL in output).
- **Integration tests**: none required. The adapter is pure HTTP over
  stdlib. A `//go:build integration` test hitting real crt.sh would be
  flaky and is not proportionate to the risk.
- **Coverage**: maintain ≥ 80 % on `internal/domain/tlspreflight/` and
  `internal/app/tlspreflight/`. Adapter is less strict (≥ 70 %) because
  the error-path permutations dominate.

### New dependencies

- `golang.org/x/net` — **promoted from indirect to direct** in
  `go.mod`. Already vendored (currently v0.52.0). License: **BSD-3-Clause**
  (Go standard pattern). Verified with
  `go list -m -json golang.org/x/net` — `"BSD-3-Clause"`. Only the
  `publicsuffix` subpackage is consumed, which ships with an embedded
  Mozilla Public Suffix List (also acceptable: MPL-2.0 data file, not
  linked code).
- No other new dependency. The HTTP client is stdlib `net/http`. The JSON
  parser is stdlib `encoding/json`.

## Consequences

### Positive

- Closes the silent-ACME-retry failure mode that cost ~10 minutes in the
  v0.15.0 retro. Operators see a loud WARN at 4/5 and a hard FAIL at
  5/5 before deploying — weeks ahead of the actual cutover in most
  cases.
- Reuses the `CheckResult` / section / JSON / exit-code machinery from
  ADR-084 — zero new CLI surface, zero new output format to document or
  test beyond a single table row.
- `tlspreflight.Service.Check` is reusable. A future
  `vibew bundle --preflight-tls` or a pre-deploy hook can call it
  directly without touching the doctor code path.
- No coupling between the sidecar runtime and crt.sh. The port only
  exists on the `vibew` CLI side; the sidecar binary never issues an
  outbound CT query.
- `publicsuffix` is stdlib-adjacent (golang.org/x), BSD-3, and already
  vendored — no new license review work.

### Negative / trade-offs

- **New external network dependency for doctor.** If crt.sh is down, the
  check degrades to WARN. The operator loses the rate-limit guarantee
  for that invocation. Mitigated by the explicit WARN message and the
  `--skip-le-preflight` escape hatch. Re-evaluate if crt.sh has >2
  outages in a 90-day window.
- **~200–400 ms added to `vibew doctor` on LE deployments.** A single
  HTTP round-trip to crt.sh. Acceptable for a human-interactive command.
  Multi-site deployments pay N× that (sequential) — still under 2 s for
  any realistic N.
- **Privacy leak.** See §(h) — the domain being queried is telegraphed
  to crt.sh. No mitigation in v1. Documented in `docs/cli.md`.
- **No cache.** Repeated `vibew doctor` runs within a minute each hit
  crt.sh. Acceptable for v1; revisit if operators report complaints.
- **CT log propagation delay (few minutes).** A cert that just got
  issued may not be counted for 1–3 minutes. This is a false-positive
  risk (budget appears higher than it is); after 3 minutes the check is
  accurate. The 4/5 WARN threshold absorbs this lag — even a recently
  issued cert that is not yet visible on crt.sh still leaves the
  operator at 4/5 with a WARN. Document the caveat.
- **No wildcard tracking.** `*.example.com` certs have a separate
  10-per-7-day limit. Out of scope for v1 per PM §5. Document as a
  known gap.

### Future work (out of scope)

- Extend preflight to wildcards (separate LE bucket, requires different
  crt.sh query and distinct budget tracking).
- Pre-deploy hook: call `tlspreflight.Service.Check` from `vibew bundle`
  and gate bundle creation on FAIL. Requires a new exit code and a new
  flag story — file a follow-up issue in the PR description.
- In-memory cache keyed on `(registeredDomain, 5-min bucket)` if
  multi-site deployments make crt.sh latency painful.
- Support alternative CT log APIs if crt.sh becomes unreliable — the
  port shape supports swapping adapters without changing the domain
  logic.

## References

- PM spec — #1057 (2026-04-23)
- Parent — #1055 / ADR-083 (ACME chain hardening)
- ADR-079 — ACME fallback chain (multi-issuer failover)
- ADR-084 — doctor port ownership / `CheckResult` contract
- ADR-089 — bundle image health (exit-code convention, port pattern)
- Let's Encrypt rate limits — <https://letsencrypt.org/docs/rate-limits/>
- crt.sh JSON API — <https://crt.sh/> (undocumented stable contract;
  `?Identity=<domain>&output=json` is widely used by tooling)
- CT logs for LE — <https://letsencrypt.org/docs/ct-logs/>
- `golang.org/x/net/publicsuffix` — BSD-3, Mozilla Public Suffix List
