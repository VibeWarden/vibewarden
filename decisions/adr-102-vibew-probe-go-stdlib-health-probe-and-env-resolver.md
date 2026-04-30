# ADR-102: `vibew probe [--env <name>]` — Go-stdlib HTTPS probe of `/_vibewarden/health` and a generalisable env-resolver

**Date**: 2026-04-30
**Issue**: #1233
**Status**: Accepted

## Context

The v0.18.3 retrospective (qr-code-brutalist Node deploy) flagged a recurring
friction point that has now appeared across multiple retros:

> Used Python for HTTPS testing because system curl on macOS uses LibreSSL
> which couldn't handshake the self-signed dev cert. Workaround:
> `python3 -c "import ssl, urllib.request..."`. A `vibew curl <path>` or
> `vibew probe` helper that uses Go's TLS stack would erase a real friction
> point.

#1224 added a `vibew doctor` advisory that documents the LibreSSL workaround,
but the friction itself remains: every dev session on macOS that wants to
verify the local stack is forced into a Python one-liner because the system
`curl` cannot handshake the dev self-signed cert. The advisory bought us a
diagnostic; #1233 buys us the fix.

Two architectural concerns are entangled in this work:

1. **The local probe itself.** Needs to bypass the host's TLS stack and use
   Go's stdlib `crypto/tls`, which does handshake the dev cert without
   complaint. `InsecureSkipVerify: true` is appropriate for the dev path —
   the cert is self-signed by construction and the target is `localhost`.

2. **The `--env <name>` mechanism.** Multiple future verbs (`vibew status
   --env prod`, `vibew validate --env prod`, etc.) want to operate against a
   named environment overlay. Today `vibew bundle` rolls its own
   `prodConfigPathForEnv` helper plus a targeted `readProdTLSDomain` reader,
   and `vibew validate` rolls its own `discoverProdOverride` helper. Both
   shapes already exist in the codebase but are duplicated and ad-hoc. The
   shape we ship for `vibew probe --env <name>` will become the de facto
   pattern for every `--env` verb that follows. This is architectural, not
   incidental — hence ADR-worthy.

The wire format the probe consumes is already locked by ADR-098 / #1197:

```json
{
  "status": "ok|degraded",
  "version": "0.18.4",
  "components": {
    "sidecar": "ok",
    "upstream": "ok|failing|unknown"
  }
}
```

The 5–10s "boot gap" (where `components.upstream == "unknown"` because the
background probe has not run its first cycle yet) is a known property of the
sidecar. `vibew probe` must absorb that gap rather than report a spurious
failure during it.

### Alternatives considered

- **`vibew curl <path>`** — rejected at triage. Re-implements curl, ratchets
  scope, has no clear feature ceiling. The probe is a fixed verb against a
  fixed endpoint and that is its strength.
- **`vibew dev --no-tls`** — rejected at triage. Weakens dev/prod parity.
  Caddy's TLS pipeline is exactly what we want covered by the dev loop.
- **Shell out to system `curl`** — rejected. That is the bug we are fixing.
  LibreSSL is the friction.
- **Patch macOS LibreSSL trust upstream** — out of scope. Tracked separately;
  even when fixed, the in-binary probe is still useful because it removes the
  external dependency entirely.
- **Reuse `internal/adapters/ops.VibeWardenHealthProbe`** — close, but that
  port (`PortOwnerProbe`) returns a binary `OwnerVibeWarden | OwnerForeign`
  for `vibew doctor`'s port-ownership check (ADR-084). It deliberately reads
  only the first 512 bytes and matches a JSON prefix; it does not parse the
  full health response. Reusing it would require widening its surface in a
  way that defeats its purpose. We add a sibling adapter that parses the
  full body.

## Decision

Add a new CLI verb, `vibew probe`, that performs an HTTPS GET against the
sidecar's `/_vibewarden/health` endpoint using Go's stdlib HTTP client. Add
a small, focused `internal/app/env` package whose `Resolver` type is the
single canonical entry point for `--env <name>` flag handling across the
CLI.

### Domain model changes

None. The probe is a CLI/app-layer use case that consumes the existing
health wire format owned by `internal/adapters/caddy/health_handler.go`. No
new domain entity is introduced.

The probe response is modelled as a value object inside the new probe app
package (`HealthDocument`, see below), but that type lives in the app layer
because it is a transport/wire shape, not a domain concept. The domain
already owns the underlying state model in `internal/domain/upstream` and
`internal/domain/health`.

### Ports (interfaces)

Add **one** new outbound port:

```go
// internal/ports/health_prober.go
package ports

import "context"

// HealthProber issues an HTTPS GET against a sidecar's /_vibewarden/health
// endpoint and returns the parsed wire-format response.
//
// Implementations choose how strictly to verify the TLS cert chain — the
// localhost adapter sets InsecureSkipVerify=true; the production adapter
// uses the stdlib default (full chain verification). Callers express the
// difference by selecting the adapter, not via flags on the port.
type HealthProber interface {
    // Probe performs a single HTTPS GET against url and returns the parsed
    // body. Returns ErrProbeRefused when the connection is refused (stack
    // not running). Returns ErrProbeMalformed when the body cannot be
    // parsed as the expected wire format. Returns ErrProbeNon200 when the
    // server returns a non-2xx status — Body and StatusCode on the wrapped
    // error give the caller enough context to render a useful message.
    Probe(ctx context.Context, url string) (HealthDocument, error)
}

// HealthDocument is the parsed shape of /_vibewarden/health (ADR-098).
type HealthDocument struct {
    Status     string            // "ok" | "degraded"
    Version    string            // sidecar version, e.g. "0.18.4"
    Site       string            // multisite scope, "" for single-site
    Components map[string]string // {"sidecar": "ok", "upstream": "ok"|"failing"|"unknown"}
}
```

Sentinel errors live alongside the port:

```go
// internal/ports/health_prober.go
var (
    ErrProbeRefused   = errors.New("connection refused — stack is not running")
    ErrProbeMalformed = errors.New("malformed health response body")
    ErrProbeNon200    = errors.New("non-2xx response from health endpoint")
)

// ProbeNon200Error wraps ErrProbeNon200 with the status code and a bounded
// snippet of the response body so the CLI layer can render a useful message
// without re-issuing the request.
type ProbeNon200Error struct {
    StatusCode int
    Body       string // truncated to ~512 bytes
}

func (e *ProbeNon200Error) Error() string { ... }
func (e *ProbeNon200Error) Unwrap() error { return ErrProbeNon200 }
```

No new inbound port. `vibew probe` is a CLI-driven use case, not an
externally-driven one.

### Adapters

Add **one** new adapter, with a constructor for each TLS posture:

```go
// internal/adapters/health/prober.go
package health

// HTTPProber implements ports.HealthProber via Go's stdlib net/http.
//
// Construct with NewLocalhostProber for the dev path (InsecureSkipVerify),
// or NewStrictProber for the --env path (full cert chain verification).
type HTTPProber struct {
    client *http.Client
}

func NewLocalhostProber(timeout time.Duration) *HTTPProber {
    return &HTTPProber{client: &http.Client{
        Timeout: timeout,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }}
}

func NewStrictProber(timeout time.Duration) *HTTPProber {
    return &HTTPProber{client: &http.Client{Timeout: timeout}}
}
```

Both constructors return the same concrete type; only the TLS config
differs. The adapter file lives next to `internal/adapters/health/checker.go`
which already houses the server-side background probe — the package's role
is "everything HTTP-health-related on the client side as well as the server
side".

### Application service

Add `internal/app/probe/`:

```go
// internal/app/probe/service.go
package probe

// Options is the parameter object for Service.Run.
type Options struct {
    URL          string         // resolved target URL
    BootGapWait  time.Duration  // total wait budget for "upstream:unknown"; default 10s
    BootGapPoll  time.Duration  // retry interval inside the boot gap; default 1s
    PerProbe     time.Duration  // per-request timeout; default 3s
}

// Result is what the CLI renders. Keeps Doc separate from the URL so the
// renderer can print the URL even on error paths.
type Result struct {
    URL string
    Doc ports.HealthDocument
}

type Service struct {
    prober ports.HealthProber
    clock  func() time.Time            // injectable for tests
    sleep  func(d time.Duration)       // injectable for tests
}

func NewService(prober ports.HealthProber) *Service { ... }

// Run probes URL once, and if the response is healthy except for
// components.upstream == "unknown", retries every BootGapPoll until either
// the upstream clears or BootGapWait elapses. Returns the final Result.
//
// Sentinel errors propagate from the prober: ErrProbeRefused,
// ErrProbeMalformed, *ProbeNon200Error.
func (s *Service) Run(ctx context.Context, opts Options) (Result, error) { ... }
```

The boot-gap retry lives in the **app layer**, not the adapter, because it
is a use-case policy: "this CLI is willing to wait 10s for the boot probe
to converge". A different consumer (e.g. a future `vibew status` reuse)
might want a 0s budget. The adapter stays a single-shot HTTP call.

### Env-resolver package

Add `internal/app/env/`:

```go
// internal/app/env/resolver.go
package env

// Resolver loads the merged config for an optional environment name.
//
// When name is empty, only the base vibewarden.yaml is loaded (via
// config.Load). When name is non-empty, vibewarden.<name>.yaml is loaded
// from the same directory and deep-merged on top of the base via the
// existing bundleapp.LoadMergedConfig contract. Missing override files are
// an error — explicit env names map to existing files.
type Resolver interface {
    Resolve(name string) (Resolved, error)
}

// Resolved carries the merged config plus the file paths that produced it.
// Future verbs (vibew status --env prod, etc.) re-use this shape.
type Resolved struct {
    Cfg            *config.Config
    BasePath       string // absolute path to vibewarden.yaml
    OverridePath   string // absolute path to vibewarden.<name>.yaml; "" when name == ""
    EnvName        string // echoes name; "" for default
}

// FileResolver is the default implementation. ProjectRoot defaults to the
// caller's working directory when empty.
type FileResolver struct {
    ProjectRoot string
}

func NewFileResolver(projectRoot string) *FileResolver { ... }

func (r *FileResolver) Resolve(name string) (Resolved, error) { ... }

// Sentinel errors:
var (
    ErrBaseConfigMissing     = errors.New("base config not found: vibewarden.yaml")
    ErrOverrideConfigMissing = errors.New("override config not found")
)
```

`Resolve` delegates to `bundleapp.LoadMergedConfig` for the actual deep-merge
(the merge logic stays in `internal/app/bundle/resolve.go` — no duplication;
`env` is a thin coordinator that locates the files and forwards to the
existing merger). When `name == ""`, `Resolve` calls `config.Load` directly.

Migrating `vibew bundle` and `vibew validate` to consume `env.Resolver` is
**explicitly out of scope** for #1233 — those verbs already work and
swapping their plumbing risks regressions in unrelated code paths. The
package is positioned as the canonical home for future adopters; the
existing helpers stay until a follow-up issue migrates them.

### Probe target resolution

The `vibew probe` command resolves its target URL like this:

| `--env` | Source                                   | URL form                                        | TLS verify |
|---------|------------------------------------------|-------------------------------------------------|------------|
| absent  | base `vibewarden.yaml` `server.port`     | `https://localhost:<port>/_vibewarden/health`   | skip       |
| present | merged config `tls.domain`               | `https://<domain>/_vibewarden/health`           | full chain |

When `--env <name>` is set but the merged `tls.domain` is empty (or the
merged config's TLS plugin is not enabled), the command exits 1 with a
message pointing at the user's `vibewarden.<name>.yaml` — there is no
fallback to localhost in `--env` mode (that would mask a real config bug).

### File layout

New files (all paths absolute from repo root):

```
internal/ports/health_prober.go                          # port + sentinel errors + HealthDocument
internal/adapters/health/prober.go                       # HTTPProber, NewLocalhostProber, NewStrictProber
internal/adapters/health/prober_test.go                  # unit tests via httptest.Server
internal/app/probe/service.go                            # Service + Options + Result; boot-gap retry
internal/app/probe/service_test.go                       # unit tests with a fake prober
internal/app/probe/render.go                             # Render(io.Writer, Result) — output formatter
internal/app/probe/render_test.go                        # golden test for the output block
internal/app/env/resolver.go                             # Resolver, FileResolver, Resolved, sentinels
internal/app/env/resolver_test.go                        # unit tests covering name="" and name="prod"
internal/cli/cmd/probe.go                                # cobra command, flag wiring, exit codes
internal/cli/cmd/probe_test.go                           # CLI-level integration tests
decisions/adr-102-vibew-probe-go-stdlib-health-probe-and-env-resolver.md  # this ADR
```

Modified files:

```
internal/cli/cmd/root.go                                 # AddCommand(NewProbeCmd())
decisions/README.md                                      # ADR-102 row
CHANGELOG.md                                             # Unreleased / Added entry
```

No new dependencies. The probe uses only `net/http`, `crypto/tls`,
`encoding/json`, `time`, `context`, `errors`, all stdlib.

### Sequence

User types: `vibew probe --env production`

1. cobra dispatches to `NewProbeCmd().RunE`.
2. `requireScaffolding()` confirms a vibewarden project exists in cwd.
3. `requireConfig("")` confirms `vibewarden.yaml` exists.
4. Resolve probe target:
   - If `--env` empty: `loadAndResolve(ctx, "")` → use `cfg.Server.Port`.
     URL = `https://localhost:<port>/_vibewarden/health`. Construct
     `health.NewLocalhostProber(3 * time.Second)`.
   - If `--env <name>` set: `env.NewFileResolver(cwd).Resolve(name)` →
     read `cfg.TLS.Domain` from the merged result. URL =
     `https://<domain>/_vibewarden/health`. Construct
     `health.NewStrictProber(3 * time.Second)`.
5. Construct `probe.NewService(prober)`.
6. Call `service.Run(ctx, probe.Options{URL: url, BootGapWait: 10s, BootGapPoll: 1s, PerProbe: 3s})`.
7. Inside `Run`:
   1. Issue first probe via `prober.Probe(ctx, url)`.
   2. If `err == nil` and `Doc.Components["upstream"] != "unknown"`, return the result.
   3. If `err == nil` and `upstream == "unknown"`, sleep `BootGapPoll` and re-probe; repeat until either upstream clears or `BootGapWait` elapses.
   4. If a probe error occurs, return it immediately (no retry on hard errors — only on the soft "unknown" state).
8. Back in the CLI handler: render the `Result` to stdout via `probe.Render`.
9. Compute exit code:
   - `Doc.Components["upstream"] == "ok"` → exit 0.
   - Anything else (including post-retry "unknown", "failing", "degraded", or any error) → exit 1.

### Output format

Pinned by a golden test (`internal/app/probe/testdata/dev_ok.golden` plus
peers for each branch). The renderer writes:

```
https://localhost:8443/_vibewarden/health
  status:              ok
  version:             0.18.4
  components.sidecar:  ok
  components.upstream: ok

OK — dev stack healthy.
```

For non-default env:

```
https://app.example.com/_vibewarden/health
  status:              ok
  version:             0.18.4
  components.sidecar:  ok
  components.upstream: ok

OK — production stack healthy.
```

For boot-gap exhaustion:

```
https://localhost:8443/_vibewarden/health
  status:              degraded
  version:             0.18.4
  components.sidecar:  ok
  components.upstream: unknown

DEGRADED — upstream probe has not converged within 10s. Check `vibew logs vibewarden`.
```

For connection refused:

```
ERROR: https://localhost:8443/_vibewarden/health
Stack is not running. Start with: vibew dev
```

For non-200:

```
https://app.example.com/_vibewarden/health
  http_status: 502
  body:        Bad Gateway

ERROR: non-2xx response from health endpoint.
```

For malformed body:

```
ERROR: https://app.example.com/_vibewarden/health
Malformed response body — not the VibeWarden health wire format.
```

The trailing one-line summary uses an ASCII em-dash (—) for visual
consistency with `vibew status` (ADR-095) and `vibew bundle`.

### Error cases

| Condition                                         | Sentinel              | Exit | Stderr message |
|---------------------------------------------------|-----------------------|------|----------------|
| Connection refused (no listener)                  | `ErrProbeRefused`     | 1    | "Stack is not running. Start with: vibew dev" |
| TLS handshake failure (`--env` mode only)         | wrapped by transport  | 1    | "TLS verification failed: <detail>" — points the user at their cert |
| DNS / network error (`--env` mode only)           | wrapped by transport  | 1    | "Network error: <detail>" |
| HTTP non-2xx                                      | `*ProbeNon200Error`   | 1    | "non-2xx response (<code>): <body snippet>" |
| Body not JSON / missing fields                    | `ErrProbeMalformed`   | 1    | "Malformed response body — not the VibeWarden health wire format" |
| `components.upstream == "unknown"` after 10s wait | none (success path)   | 1    | the rendered block above |
| `components.upstream == "failing"`                | none (success path)   | 1    | the rendered block above |
| `--env name` requested, override file missing     | `env.ErrOverrideConfigMissing` | 1 | "config file not found: vibewarden.<name>.yaml" |
| `--env name` requested, merged `tls.domain` empty | local sentinel        | 1    | "tls.domain is empty in merged config; cannot resolve probe target" |

`ports.ErrDockerUnavailable` (exit 3) is **not** applicable — `vibew probe`
never touches docker. Reuse it would be misleading.

### Test strategy

Unit tests:

- `internal/adapters/health/prober_test.go` — drive `HTTPProber` against
  an `httptest.NewTLSServer`. Cases: 200 with valid body, 200 with
  malformed body, non-200, connection refused (target a closed port),
  body too large (truncation honoured).
- `internal/app/probe/service_test.go` — drive `Service` with a fake
  `ports.HealthProber`. Cases: first-probe ok → no retry; first-probe
  unknown → retry until ok; first-probe unknown → all retries return
  unknown → final result is unknown; first-probe error → returned
  immediately. Inject a fake `sleep` so tests run instantly.
- `internal/app/probe/render_test.go` — golden tests for every output
  branch (dev ok, env ok, boot-gap exhausted, refused, non-200,
  malformed). Goldens live under `internal/app/probe/testdata/`.
- `internal/app/env/resolver_test.go` — covers: empty name, present
  override, missing override (error), missing base (error), tls.domain
  preserved through merge.

Integration / CLI tests:

- `internal/cli/cmd/probe_test.go` — end-to-end cobra invocation against an
  `httptest.NewTLSServer` whose handler returns a synthesised wire-format
  body. Covers the default path (no flag), the `--env` path (writes a
  scratch `vibewarden.production.yaml` with `tls.domain` pointing at the
  test server), and the failure paths. Uses `cobra.Command.SetOut/SetErr`
  to capture output and asserts both content and exit code.

No testcontainers dependency; the test server replaces the live sidecar.
The probe is HTTP-only by construction so testcontainers would be
overkill.

### New dependencies

None. All functionality is built on stdlib (`net/http`, `crypto/tls`,
`encoding/json`, `time`, `context`, `errors`).

## Consequences

**Positive.** macOS dev sessions stop reaching for `python3 -c "import ssl..."`.
The Go binary owns its TLS dance from end to end and the dev cert is
handshakable by construction. The `--env` mechanism gets a single canonical
home (`internal/app/env`) instead of three duplicated ad-hoc helpers.

**Constraint on future code.** Every future verb that adopts `--env` must
route through `env.Resolver`. New ad-hoc `prodConfigPathForEnv`-style
helpers in CLI commands are now an architectural smell — the reviewer
agent should flag them. The migration of existing helpers
(`bundle.prodConfigPathForEnv`, `validate.discoverProdOverride`) is left
to a follow-up issue but the canonical path is locked here.

**TLS verification asymmetry is intentional.** `--env` mode does **not**
fall back to `InsecureSkipVerify`. A user with a self-signed prod cert
will get a clear handshake error rather than a silent "ok". The escape
hatch for that case is a separate flag that we explicitly do not ship in
v1 — that would weaken the safety property the `--env` path is meant to
provide. If the demand surfaces, a follow-up ADR can justify adding a
`--insecure-skip-verify` flag.

**Boot-gap retry is fixed at 10s.** The window matches ADR-098's stated
boot-gap range. If a future change to the background probe lengthens that
window, this ADR's 10s budget needs to grow with it — flagged as a
maintenance dependency in the implementation comment block.

**No domain-layer impact.** The probe operates entirely at the app/adapter
level. Domain layer remains untouched, preserving the zero-external-imports
property required by CLAUDE.md §Architecture principles.
