# ADR-110: `server.max_connections` — concurrent-connection cap via a custom Caddy listener wrapper placed before TLS

**Date**: 2026-09-03
**Issue**: [#1311](https://github.com/vibewarden/vibewarden/issues/1311)
**Status**: Accepted

---

## Context

`buildMainServer` in `internal/adapters/caddy/config_build.go` emits `listen`, `routes`,
read/write/idle timeouts, `automatic_https` and TLS policies. It sets no bound on the
number of concurrent inbound connections. A connection flood (DDoS, or a client that
leaks sockets) grows file-descriptor usage until the process dies, taking the app it is
protecting down with it. Timeouts bound the *lifetime* of a connection, not the *count*.

The PM spec (#1311) fixes the product contract: a `server.max_connections` integer under
the existing `server:` block, default `1000`, `0` = explicit unlimited opt-out, negative
values rejected by the strict loader, and refusal behaviour (not accept-and-hang) once
the cap is reached. It delegates three things to this ADR:

1. Whether `vibew doctor` should warn on values that look wrong against the system ulimit.
2. The Caddy-level enforcement mechanism.
3. Whether `1000` is the right default.

**Mechanism survey.** Caddy 2.11.4 has no per-server connection cap. Grepping the module
cache confirms the `caddy.listeners` namespace ships exactly three wrappers:
`caddy.listeners.tls` (a no-op placeholder), `caddy.listeners.http_redirect`, and
`caddy.listeners.proxy_protocol`. There is no `max_connections` field on
`caddyhttp.Server`. The finding's suggested `server["max_header_bytes"]` is a different
control (header size, not connection count) and does not address the risk.

`golang.org/x/net/netutil.LimitListener` is the obvious library answer and is the wrong
one: it *blocks* in `Accept` until a slot frees, which is precisely the accept-and-hang
behaviour the spec's acceptance criteria rule out. Excess connections would sit in the
kernel backlog with no signal to the client.

That leaves a custom `caddy.ListenerWrapper` module, which this repo is already set up
for — 17 custom `http.handlers.vibewarden_*` modules are registered in
`internal/adapters/caddy/`. This is the first module in the `caddy.listeners` namespace,
so this ADR also fixes the wrapper-ordering convention for any future one.

## Decision

Add a config-driven, per-listener concurrent-connection cap enforced by a new custom
Caddy listener wrapper module, `caddy.listeners.vibewarden_conn_limit`, positioned
**before** the TLS listener in the wrapper chain.

### Domain model changes

None. This is a transport-level resource control with no domain meaning — no new entity,
value object, or domain event. `internal/domain/` is untouched.

### Ports (interfaces)

No new interface. One new field on the existing `ports.ProxyConfig` value object
(`internal/ports/proxy.go`):

```go
// MaxConnections caps the number of concurrent inbound TCP connections the
// server accepts. When the cap is reached, further connections are accepted
// and immediately closed (refused) until an existing connection ends.
// A value of 0 disables the cap. Negative values are rejected by config
// validation and treated as 0 here.
MaxConnections int
```

It is deliberately **not** folded into `ServerTimeoutsConfig` — that struct is about
durations, and a connection count is a different concern (ISP).

### Adapters

`internal/adapters/caddy/conn_limit_listener.go` — new file, one new exported type.

```go
// ConnLimitListener is a Caddy listener wrapper module that caps the number
// of concurrent connections on the listener it wraps.
type ConnLimitListener struct {
    MaxConnections int `json:"max_connections,omitempty"`

    logger  *slog.Logger
    active  atomic.Int64
    refused atomic.Int64
    lastLog atomic.Int64 // unix nanos of the last refusal warning
}
```

- `func init() { gocaddy.RegisterModule(ConnLimitListener{}) }` — matching the existing
  handler files.
- `CaddyModule()` returns `ID: "caddy.listeners.vibewarden_conn_limit"`, `New` returning
  `new(ConnLimitListener)`.
- `Provision(ctx gocaddy.Context) error` — takes the logger from `currentServices()`
  (ADR-092 registry), falling back to `slog.Default()` when nil; returns an error when
  `MaxConnections <= 0`, since the builder never emits the wrapper in that case and a
  zero here means a malformed config.
- `WrapListener(ln net.Listener) net.Listener` returns a wrapper whose `Accept` is:

  1. `conn, err := ln.Accept()`; on error, return it unchanged.
  2. `n := l.active.Add(1)`.
  3. If `int(n) > l.MaxConnections`: `l.active.Add(-1)`, `conn.Close()`, record a
     refusal, **continue the accept loop** (do not return an error — an error from
     `Accept` would make Caddy tear the server down).
  4. Otherwise return the conn wrapped in a `limitedConn` whose `Close` decrements
     `active` exactly once (`sync.Once`).

  The wrapper embeds `net.Listener`, so `Close`/`Addr` delegate.

- Refusal observability: `refused` is incremented on every rejection; a `Warn` is emitted
  at most once per 30s (`lastLog` CAS on `time.Now().UnixNano()`) carrying
  `max_connections` and the cumulative `refused_total`. A per-connection log line during
  a flood would itself be a denial of service against the log sink.
- Interface guards: `var _ gocaddy.ListenerWrapper = (*ConnLimitListener)(nil)` and
  `var _ gocaddy.Provisioner = (*ConnLimitListener)(nil)`.

Accepting-then-closing is the deliberate choice over `netutil.LimitListener`: it drains
the kernel backlog, releases the fd immediately, and gives the client a prompt FIN/RST
instead of an indefinite hang. Under sustained flood the process does accept+close work
but never accumulates descriptors, which is exactly the graceful degradation the story
asks for.

### Config layer

`internal/config/sidecar.go` — `ServerConfig` gains:

```go
// MaxConnections caps concurrent inbound connections to the sidecar's main
// listener. When the cap is reached, new connections are refused (accepted
// and immediately closed) until an existing connection ends; established
// connections and in-flight requests are unaffected.
// A value of 0 explicitly disables the cap (unlimited). Negative values are
// rejected by validation. Default: 1000.
MaxConnections int `mapstructure:"max_connections"`
```

`internal/config/config.go` — `setDefaults` gains
`v.SetDefault("server.max_connections", 1000)`. This is what makes "omitted = 1000" and
"explicit `0` = unlimited" distinguishable without a pointer field, following the exact
pattern already used by `server.port`. (The string-sentinel pattern used by
`server.read_timeout` is not reusable here — this key is an int.)

`internal/config/sidecar.go` — `validateSidecar` gains:

```go
if c.Server.MaxConnections < 0 {
    errs = append(errs, fmt.Sprintf(
        "server.max_connections must be >= 0 (0 disables the limit), got %d",
        c.Server.MaxConnections))
}
```

The strict loader (`LoadStrict`, ADR-082) needs no change: `checkUnknownKeys` walks
`mapstructure` tags reflectively, so a new field on `ServerConfig` is automatically a
known key. `Validate()` runs inside `loadInternal`, which `LoadStrict` calls, so
`vibew validate` and `vibew bundle` both reject negatives. The env override
`VIBEWARDEN_SERVER_MAX_CONNECTIONS` works for free via the existing
`SetEnvKeyReplacer(".", "_")` + `AutomaticEnv`.

### Application service

No new application service. The composition root
(`cmd/vibewarden/wiring_serve_helpers.go`, in the `ports.ProxyConfig` literal, next to
`ServerTimeouts`) adds `MaxConnections: cfg.Server.MaxConnections`. No parse helper is
needed — it is already an int.

### Caddy config build

`internal/adapters/caddy/config_build.go`:

```go
// applyConnectionLimit adds the vibewarden_conn_limit listener wrapper to the
// server map when max > 0. The explicit trailing {"wrapper": "tls"} entry is
// load-bearing: it marks where TLS terminates, so the connection cap runs on
// the raw TCP listener rather than after the handshake. Without it Caddy
// prepends the TLS placeholder itself and every wrapper lands after TLS.
func applyConnectionLimit(server map[string]any, max int) {
    if max <= 0 {
        return
    }
    server["listener_wrappers"] = []map[string]any{
        {"wrapper": "vibewarden_conn_limit", "max_connections": max},
        {"wrapper": "tls"},
    }
}
```

Called from `buildMainServer` right after `applyServerTimeouts`, and from
`buildCaddyApps` on the redirect server. `buildHTTPRedirectServer` in
`internal/adapters/caddy/config.go` takes a `maxConns int` parameter so the port-80
redirect listener is capped too — it is publicly reachable under the self-signed and
external TLS providers, and leaving it unbounded would reopen the same fd hole through a
different door.

The two listeners hold **independent counters**. The cap is therefore per-listener: the
process-wide ceiling is `max_connections` normally, `2 × max_connections` while the
redirect server is running. A shared counter would need a cross-module registry for no
practical gain; both numbers are bounded, which is the property that matters.

### File layout

New:
- `internal/adapters/caddy/conn_limit_listener.go`
- `internal/adapters/caddy/conn_limit_listener_test.go`
- `decisions/adr-110-server-max-connections-listener-wrapper.md` (this file)

Modified:
- `internal/ports/proxy.go` — `MaxConnections` on `ProxyConfig`
- `internal/config/sidecar.go` — `ServerConfig.MaxConnections` + `validateSidecar` rule
- `internal/config/config.go` — `setDefaults` entry
- `cmd/vibewarden/wiring_serve_helpers.go` — config → port mapping
- `internal/adapters/caddy/config_build.go` — `applyConnectionLimit`, wired into
  `buildMainServer` and `buildCaddyApps`
- `internal/adapters/caddy/config.go` — `buildHTTPRedirectServer(maxConns int)`
- `internal/adapters/caddy/config_build_test.go` — JSON-shape assertions
- `internal/config/reference_yaml_test.go` — reference-value row + `TestSetDefaults_EmptyYAML` row
- `internal/config/sidecar_test.go` (or `config_test.go`) — negative-value validation test
- `vibewarden.reference.yaml` — document the key in the `server:` block
- `docs/configuration.md` — table row (~line 40) and the `server:` example
- `CHANGELOG.md` — Unreleased entry
- `decisions/README.md` — index row for ADR-110

### Sequence

1. `vibew serve` → `config.Load` → viper applies `server.max_connections` default `1000`
   unless the YAML or `VIBEWARDEN_SERVER_MAX_CONNECTIONS` sets it.
2. `Config.Validate` → `validateSidecar` rejects negatives, naming the field and value.
3. Composition root copies `cfg.Server.MaxConnections` into `ports.ProxyConfig`.
4. `BuildCaddyConfig` → `buildMainServer` → `applyConnectionLimit` emits
   `listener_wrappers: [vibewarden_conn_limit, tls]` when the value is > 0; nothing at
   all when it is 0.
5. Caddy loads the config, resolves `caddy.listeners.vibewarden_conn_limit` from the
   module registry, `Provision` grabs the logger and validates the value.
6. At listen time Caddy wraps: raw TCP listener → `ConnLimitListener` → (TLS
   termination) → `http2Listener`.
7. Inbound connection under the cap: counted, served normally; `Close` decrements.
8. Inbound connection at the cap: accepted, immediately closed, counter untouched,
   accept loop continues. Established connections and in-flight requests are unaffected.

### Error cases

| Case | Handling |
|---|---|
| `server.max_connections` negative | `vibew validate` / `vibew bundle` fail with `server.max_connections must be >= 0 (0 disables the limit), got -5` |
| `server.max_connections: 0` | Wrapper omitted from the JSON entirely; unlimited, as specified |
| Non-integer YAML value | viper/mapstructure type error from `Unmarshal`, existing behaviour |
| `Provision` sees `MaxConnections <= 0` | Returns an error — unreachable via the builder, a tripwire for hand-edited Caddy JSON |
| Cap reached | Connection refused (accept + immediate close); throttled `Warn` at most once per 30s with the cumulative refusal count |
| `Accept` returns an error | Propagated unchanged; the wrapper never manufactures errors, which would kill the server loop |
| Config reload while at the cap | New module instance starts at zero while the old server drains; brief over-admission is accepted (bounded by the reload window) |

### Test strategy

**Unit, no build tag — the load-bearing one.** `conn_limit_listener_test.go` opens a real
`net.Listen("tcp", "127.0.0.1:0")`, wraps it with `MaxConnections: 2`, serves accepts in a
goroutine, then:
- dials 2 connections, writes/reads on both, asserts they work;
- dials a 3rd, asserts the read returns EOF/RST promptly (refused, not hung — assert
  against a deadline so a blocking implementation fails instead of timing the suite out);
- closes one of the first 2, asserts a new dial then succeeds (slot released);
- asserts `active` returns to 0 after all conns close (no counter leak), including on the
  refused path.

This is the criterion the spec calls out explicitly: a test that only asserts the Caddy
JSON contains a field would pass green against a silent no-op (see the Caddy-regex
precedent in `CLAUDE.md`).

**Unit, config shape.** `config_build_test.go`: wrapper present with the right value when
`MaxConnections > 0`; `listener_wrappers` key entirely absent when `0`; the `tls` entry is
present and **last** (this is the assertion that pins the pre-TLS ordering); the redirect
server carries the wrapper too.

**Unit, config layer.** Default `1000` on empty YAML — add the row to the existing
`TestSetDefaults_EmptyYAML` in `internal/config/reference_yaml_test.go` rather than a new
test, since that test exists exactly to catch "documented default that `setDefaults`
never registers". Table-driven validation test for `-1`, `0`, `1`, large values.

**Integration (`//go:build integration`), optional but recommended.** Boot the adapter
with `MaxConnections: 2` and assert Caddy provisions the module without error — this is
what catches a typo in the module ID, which no unit test can see.

Nothing needs mocking: the wrapper's only dependency is a `net.Listener`.

### New dependencies

**None.** Everything used is stdlib (`net`, `sync`, `sync/atomic`, `time`, `log/slog`)
plus the already-vendored `github.com/caddyserver/caddy/v2` (Apache 2.0).
`golang.org/x/net/netutil` was evaluated and rejected on behaviour, not licence.

### Resolved open questions

**Q1 — `vibew doctor` ulimit check: no, not in this story.** Go raises `RLIMIT_NOFILE`
soft to hard at process start (Go 1.19+), so the realistic budget is the *hard* limit,
which a doctor check would have to read from the container rather than the host to be
meaningful. Container-level resource caps are #1306's territory; a check here would
duplicate it against the wrong reference value. File a fast-follow only if #1306 lands a
cgroup-aware limits reader that this could reuse.

**Q3 — default stays `1000`.** The fd arithmetic is the thing worth stating: a saturated
sidecar holds roughly two descriptors per active request (client conn + upstream conn),
so `1000` implies a ~2000-fd working set. That is well inside budget precisely because
Go raises the soft `nofile` limit to the hard limit at startup (typically 524288 or
1048576 in modern container runtimes) — the classic 1024 soft limit does not apply to
this process. `1000` concurrent connections is also far above anything a single-VPS
vibe-coded app sees legitimately, so false refusals are unlikely. No change from the
spec's value, so nothing to call out in the PR description.

## Consequences

- **First `caddy.listeners` module in the repo.** The `{"wrapper": "tls"}` trailing entry
  is now the house convention for any future listener wrapper that must run pre-TLS. It
  is easy to omit and impossible to notice from a passing test that only checks for the
  wrapper's presence — hence the explicit ordering assertion.
- **HTTP/3 is not covered.** Caddy's default protocol set is `["h1","h2","h3"]`, and QUIC
  arrives over a `PacketConn`, not a `net.Listener`, so the cap does not apply to it.
  This is acceptable rather than a hole: QUIC multiplexes every connection over a single
  UDP socket, so it cannot cause the fd exhaustion this control exists to prevent. It
  does mean the cap is not a general request-concurrency limit. Documented in the
  reference YAML comment; not worth disabling h3 to close.
- **Per-listener, not per-process.** `2 × max_connections` while the port-80 redirect
  server is up. Stated in the docs so the number is not surprising.
- **Multi-site serve is not covered.** `internal/adapters/caddy/multisite_config.go`
  builds its server map independently and already skips `applyServerTimeouts` for the
  same reason; adding the cap there needs `server.max_connections` threaded through
  `internal/config/sites`. Consistent with existing behaviour on that path, and left
  alone deliberately rather than by omission.
- **`internal/plugins/tls/plugin.go` has a second, dead `buildHTTPRedirectServer`.**
  `Plugin.RedirectServer()` has no production caller. It is intentionally left untouched
  here so the diff stays reviewable; it is dead-code-removal work, not this story's.
- **Reload resets the counter.** Bounded over-admission during a config reload, accepted.
- **Observability is a throttled log, not a metric.** A
  `vibewarden_connections_refused_total` counter would be the better operator surface but
  needs a metrics handle inside a listener wrapper, which the ADR-092 services registry
  does not currently carry. Reasonable fast-follow.
- **`server.read_timeout` / `write_timeout` / `idle_timeout` remain undocumented in
  `vibewarden.reference.yaml`** despite being supported. Noticed while writing this;
  out of scope here, worth a separate docs issue.

## Note (2026-09-04)

Step 3 of the `Accept` algorithm above documented the order as `l.active.Add(-1)`,
`conn.Close()`, record a refusal. That was the shipped order and it was wrong:
`fix(#1311)` (carried in #1495) swaps the last two steps so `noteRefusal()` runs
*before* `conn.Close()`. Closing is what the client observes (FIN/RST), so recording
the refusal afterwards left a window where a client-side reset was visible before the
counter and the throttled log line were updated — a real observability gap, not only a
test race. `TestConnLimitListener_RefusesOverCap` now asserts the count without
polling, which pins the corrected order. See `CHANGELOG.md` under `[Unreleased] →
Fixed`.
