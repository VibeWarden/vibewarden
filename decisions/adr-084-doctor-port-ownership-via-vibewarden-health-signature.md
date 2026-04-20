# ADR-084: Doctor port ownership via VibeWarden health-signature probe

**Date**: 2026-04-20
**Issue**: #1054
**Status**: Accepted

## Context

`vibew doctor` performs a blind `net.Listen` probe on the proxy port (default
`127.0.0.1:8443`). When a sibling `vibew dev` is already running — i.e. the
expected state — the listener bind fails with `EADDRINUSE` and the doctor
reports `port 8443 is already in use` as FAIL. The user sees a red error for
the exact state the sidecar is supposed to be in.

A correct port-ownership decision must distinguish two cases deterministically,
on macOS and Linux:

1. `host:port` is bound by a local `vibew dev` — expected, report OK.
2. `host:port` is bound by any other process — conflict, report FAIL.

Four implementation options were considered:

| Option | Cross-platform | Coupling | Fragility |
|---|---|---|---|
| (a) PID file / lockfile written by `vibew dev` | yes, pure Go | new coupling between `dev` and `doctor` via the filesystem; PID staleness on crashes is a known failure mode | medium |
| (b) `lsof` / `ss` / `netstat` scrape | different binary per OS; format differs; `ss` absent on macOS | shell-out; output parsing | high |
| (c) TLS probe of `https://host:port/_vibewarden/health` looking for the VibeWarden JSON signature | pure Go (`crypto/tls` + `net/http`); identical code paths on all supported dev platforms | zero new coupling — `/_vibewarden/health` is an already-published route (see `internal/adapters/caddy/config.go` lines 164-184) | low |
| (d) Downgrade severity on `EADDRINUSE` | cross-platform but imprecise | no new mechanism | high — hides real conflicts |

Option (c) wins on all three axes. The `/_vibewarden/health` endpoint already
returns a deterministic JSON body whose prefix (`{"status":"ok","version":`) is
unique to VibeWarden and stable across versions (it is part of the published
liveness contract). Detecting the signature over TLS with
`InsecureSkipVerify: true` (self-signed certs in dev are normal) is robust,
fast (single handshake + single tiny HTTPS GET), and requires no new coupling
between `vibew dev` and `vibew doctor` beyond the already-published liveness
endpoint.

This ADR is cross-cutting because port-ownership detection will be reused by
other health tools (MCP, future admin CLI). Codifying the strategy once
prevents re-deriving it every time a new tool needs the same answer.

## Decision

Introduce a new outbound port `ports.PortOwnerProbe` that, given a `host:port`,
returns one of three ownership verdicts:

- `OwnerVibeWarden` — a VibeWarden sidecar responded on `/_vibewarden/health`.
- `OwnerForeign` — something else is listening (TLS handshake succeeded but
  body did not match, or handshake failed with `EADDRINUSE`-consistent
  symptoms), or a plain-TCP listener is present that does not speak TLS.
- `OwnerUnknown` — the port is free.

The doctor `checkPort` function composes the existing `PortChecker` result
with the `PortOwnerProbe` result:

| `PortChecker.available` | `PortOwnerProbe` | Severity | Detail |
|---|---|---|---|
| true | (not queried) | OK | `port N is available` |
| false | `OwnerVibeWarden` | OK | `in use by local vibew dev (expected)` |
| false | `OwnerForeign` | FAIL | `port N is already in use` |
| false | `OwnerUnknown` (probe error) | FAIL | `port N is already in use` |

Probe errors intentionally degrade to FAIL — a foreign listener that rejects
the TLS handshake is indistinguishable from a probe that failed for any other
reason, and FAIL is the safe default.

### Domain model changes

No domain entities are introduced. `OwnerVerdict` is a value object (a typed
string constant) defined in `internal/ports/ops.go` so it is accessible from
both the port contract and the app service.

### Ports (interfaces)

Add to `internal/ports/ops.go`:

```go
// PortOwner identifies who is listening on a TCP port.
type PortOwner string

const (
    OwnerUnknown    PortOwner = "unknown"
    OwnerVibeWarden PortOwner = "vibewarden"
    OwnerForeign    PortOwner = "foreign"
)

// PortOwnerProbe identifies the owner of a listener on host:port by probing
// the /_vibewarden/health endpoint over TLS. Implementations must be safe to
// call with InsecureSkipVerify because self-signed certs are the norm in dev.
type PortOwnerProbe interface {
    // ProbeOwner returns the identity of the process bound to host:port.
    // Never returns an error — ambiguous results map to OwnerUnknown.
    ProbeOwner(ctx context.Context, host string, port int) PortOwner
}
```

### Adapters

Add a new adapter in `internal/adapters/ops/port_owner.go`:

```go
// VibeWardenHealthProbe implements ports.PortOwnerProbe by issuing a short
// TLS GET to https://host:port/_vibewarden/health and inspecting the body.
type VibeWardenHealthProbe struct {
    client *http.Client
}

func NewVibeWardenHealthProbe(client *http.Client) *VibeWardenHealthProbe { ... }

func (p *VibeWardenHealthProbe) ProbeOwner(ctx context.Context, host string, port int) ports.PortOwner {
    // 1. GET https://host:port/_vibewarden/health with InsecureSkipVerify
    //    and a 2-second deadline derived from ctx.
    // 2. On transport error (connection refused, timeout on a reachable host,
    //    TLS handshake failure) return OwnerForeign.
    // 3. On 2xx + body prefix match `{"status":"ok","version":` → OwnerVibeWarden.
    // 4. On 2xx with no prefix match → OwnerForeign.
    // 5. On any other status → OwnerForeign.
}
```

Client construction (in `NewVibeWardenHealthProbe`) injects a dedicated
`http.Client` with `Transport: &http.Transport{TLSClientConfig:
&tls.Config{InsecureSkipVerify: true}}` and a 2-second timeout. The probe is
only ever run against localhost during local diagnostics, so
`InsecureSkipVerify` is safe and already the pattern used by
`checkRemoteTLSCert` (see `doctor.go` line 615).

### Application service

Modify `DoctorService` (`internal/app/ops/doctor.go`):

- Add field `ownerProbe ports.PortOwnerProbe` and a setter
  `WithPortOwnerProbe(...)` that returns a copy (same pattern as
  `WithRemoteExecutor`). `NewDoctorService` signature is **not** changed — to
  avoid churning every call site. When `ownerProbe` is nil the port check
  degrades to the current FAIL-on-busy behaviour so pre-existing tests that
  do not set it still pass.
- Rewrite `checkPort` to consult `ownerProbe` when `available == false` and
  map the verdict per the table above.
- `checkTLSCertValid` rewrite (bug 2, see below) calls the same `ownerProbe`
  transitively via a new local helper that performs a live TLS handshake
  against the sidecar — but we do **not** add a second port; the handshake
  reuses standard `crypto/tls` directly from the check. The rationale is
  that the cert-inspection check needs the leaf certificate, not just an
  ownership verdict, so a separate tiny helper `probeLocalLeafCert(host,
  port)` is added in the same package rather than a new port.

#### Bug 2 — TLS certificate

Replace the hardcoded `filepath.Join(workDir, ".vibewarden", "generated",
"certs", "server.crt")` with a **live TLS probe first, filesystem fallback
never** strategy:

- If `cfg.TLS.Provider != "self-signed"` → leave the existing "provider is
  X — skipping local cert check" result. No change.
- Otherwise, perform a TLS handshake against `proxyHost:proxyPort` with
  `InsecureSkipVerify: true`, read the leaf from `ConnectionState().
  PeerCertificates[0]`, and apply the existing expiry/notAfter severity
  logic to it.
- If the handshake fails:
  - severity = WARN
  - detail = `"sidecar not reachable on <host>:<port> — start 'vibew dev'"`

The filesystem scan is **deleted**. Self-signed cert paths are owned by Caddy
and the correct place to inspect them is through the live TLS handshake that
Caddy itself terminates. Doctor does not peek into Caddy's storage — this
keeps `app/ops/doctor.go` free of knowledge of Caddy's internal storage
layout (which is `adapters/caddy/`'s concern, not `app/ops/`'s).

This avoids option (b) from PM's open question — we never need to resolve
`cfg.TLS.StoragePath` in the doctor; Caddy already serves the cert we want.

#### Bug 3 — remote container check reshape

The command string is rewritten and the error surface is sanitised. Location:
`checkRemoteContainerHealth` in `doctor.go`.

New remote command:

```
docker compose ps --format json
```

That is the complete remote command string. Rationale:

- The `|| docker-compose ps` fallback is dropped. Docker Compose v1
  (`docker-compose`, hyphenated) reached end-of-life in June 2023 and has
  been removed from the official Docker distribution. The production
  environments this check runs against are provisioned via `vibew deploy`,
  which guarantees Compose v2. If v1 ever matters again it is a
  documentation problem, not a doctor-check problem.
- `2>/dev/null` is removed. Stderr is needed for the user-facing error
  message. The SSH adapter already merges stderr into the combined output
  (see `internal/adapters/ssh/executor.go` line 104) and wraps `exec.Run`
  errors with `%w`, so stderr surfaces naturally.

Error formatting is factored into a **pure function** in a new file
`internal/app/ops/remote_error.go` (kept in the app layer — see next
paragraph):

```go
// formatRemoteError converts a ports.RemoteExecutor error into a single-line,
// user-safe detail string. It never echoes the raw remote command, strips
// the "ssh <cmd>: " prefix written by ssh.Executor.Run, extracts the exit
// code when present, and includes at most one line of stderr.
func formatRemoteError(err error) string
```

Contract:

- Input: `err` returned from `ports.RemoteExecutor.Run`.
- Output: single line, no trailing newline, no shell fragments (`2>/dev/null`,
  `||`, `ssh `), at most `~180` chars.
- The exit code is extracted via `errors.As(err, &exitErr)` where
  `exitErr *exec.ExitError` — so the function depends on `os/exec` but not
  on the ssh package.
- The first non-empty line of stderr is surfaced; subsequent lines dropped.
- Followed by one of a small set of hard-coded hints keyed off exit code:
  - `127` → `"docker compose not installed on remote (expected after deploy)"`
  - `126` → `"docker compose not executable — check permissions"`
  - default → `"check remote docker compose installation"`

The reason this lives in `app/ops/` (and not in the SSH adapter) is
separation of concerns: the adapter returns structured errors; the
application layer decides how to render them for this particular use case.
A different caller (e.g. `vibew deploy`) may render the same error
differently.

**Bonus micro-fix to the ssh adapter**: the `"output:"` fragment in
`executor.go:106` is removed from the `Run` error format to stop echoing
the entire captured output inside the error itself — the caller already
gets the output as the `(string, error)` tuple's first return value, so
appending `"output: ..."` to the error string is duplication and the root
cause of the raw-pipe leak. New format:

```go
return buf.String(), fmt.Errorf("ssh exit: %w", err)
```

`cmd` is deliberately **not** interpolated into the error message — callers
that need the command string already have it (they passed it in). Dropping
it eliminates the "raw shell in user-facing error" leak structurally.

### File layout

New files:

- `internal/ports/ops.go` — edit: add `PortOwner` type + constants and
  `PortOwnerProbe` interface.
- `internal/adapters/ops/port_owner.go` — new: `VibeWardenHealthProbe`.
- `internal/adapters/ops/port_owner_test.go` — new: unit tests using
  `httptest.NewTLSServer` for VibeWarden signature match, foreign 200
  body, connection refused, wrong status.
- `internal/app/ops/remote_error.go` — new: `formatRemoteError(err error)
  string` (pure function).
- `internal/app/ops/remote_error_test.go` — new: table-driven tests for
  formatRemoteError.
- `internal/app/ops/doctor.go` — edit:
  - add `ownerProbe ports.PortOwnerProbe` field
  - add `WithPortOwnerProbe(p) *DoctorService` setter
  - rewrite `checkPort` to consult `ownerProbe`
  - rewrite `checkTLSCertValid(cfg, workDir)` → `checkTLSCertValid(ctx,
    cfg, proxyHost, proxyPort)` — signature change; `workDir` becomes
    unused and is removed from the call site in `runChecks`
  - rewrite `checkRemoteContainerHealth` to call `formatRemoteError` and
    use the simplified remote command
- `internal/app/ops/doctor_test.go` — edit: add new cases, update
  existing cases for the `checkTLSCertValid` signature.
- `internal/adapters/ssh/executor.go` — edit: simplify `Run` error
  format per "Bonus micro-fix" above.
- `internal/adapters/ssh/executor_test.go` — edit: update any tests
  that assert on the `"output:"` prefix (grep first; if none exist, no
  edit).
- `internal/cli/cmd/doctor.go` — edit: construct the new probe and pass
  via `.WithPortOwnerProbe(...)`.
- `internal/cli/cmd/mcp.go` — edit: same wiring as `doctor.go`.

### Sequence — bug 1 path (port 8443 bound by sibling `vibew dev`)

1. `DoctorService.checkPort(ctx, "Proxy port", "127.0.0.1", 8443)` is called.
2. `portChecker.IsPortAvailable(ctx, "127.0.0.1", 8443)` returns `(false, nil)`
   — bind failed.
3. `checkPort` consults `ownerProbe.ProbeOwner(ctx, "127.0.0.1", 8443)`.
4. The probe issues `GET https://127.0.0.1:8443/_vibewarden/health` with
   `InsecureSkipVerify`.
5. Caddy's static response handler returns
   `{"status":"ok","version":"<v>","components":{...}}` with HTTP 200.
6. The probe matches the `{"status":"ok","version":` prefix and returns
   `OwnerVibeWarden`.
7. `checkPort` returns `CheckResult{Severity: SeverityOK, Detail: "in use by
   local vibew dev (expected)"}`.

### Sequence — bug 1 path (port 8443 bound by some other service)

1–3 identical to above.
4. The probe issues the TLS GET. The foreign service either (a) rejects the
   TLS handshake, (b) returns a non-2xx response, or (c) returns a 2xx
   response whose body does not start with the VibeWarden signature.
5. Probe returns `OwnerForeign`.
6. `checkPort` returns `CheckResult{Severity: SeverityFail, Detail: "port
   8443 is already in use"}`.

### Error cases

| Case | Outcome |
|---|---|
| `ownerProbe` is nil (legacy wiring, or test without probe) | `checkPort` falls back to the current "busy = FAIL" behaviour — back-compat safe. |
| Probe times out on a reachable host | Returns `OwnerForeign`. FAIL is the safe default. |
| `cfg.Server.Host` is `0.0.0.0` | Probe uses `127.0.0.1` for the actual HTTP request (documented in the adapter godoc). |
| `checkTLSCertValid` fails the handshake but the port check itself said OK (owner = vibewarden) | WARN — "sidecar reachable but cert handshake failed: <one-line error>". |
| `formatRemoteError` receives an error that is not an `*exec.ExitError` | Returns `"remote command failed: <first line of err>"` with the default hint. |
| PM open question (1) — route via health endpoint | Answered: yes, that is the recommended implementation. |
| PM open question (2) — TLS probe vs storage scan vs both | Answered: TLS probe only. Storage scan is deleted. |

### Test strategy

**Unit tests (new/updated in `internal/adapters/ops/port_owner_test.go`):**

- `TestVibeWardenHealthProbe_VibeWardenSignature` — `httptest.NewTLSServer`
  returning the signature JSON → `OwnerVibeWarden`.
- `TestVibeWardenHealthProbe_ForeignHTTPS` — TLS server returning `{"foo":
  "bar"}` → `OwnerForeign`.
- `TestVibeWardenHealthProbe_NonTLS` — `net.Listen("tcp", ...)` and accept
  nothing → `OwnerForeign` (handshake fails).
- `TestVibeWardenHealthProbe_PortClosed` — no listener → `OwnerForeign`.
- `TestVibeWardenHealthProbe_Non2xx` — TLS server returning 500 with the
  signature body → `OwnerForeign` (status takes precedence over body).

**Unit tests (new/updated in `internal/app/ops/doctor_test.go`):**

- `TestCheckPort_InUseByVibeWarden_OK` — fake probe returns
  `OwnerVibeWarden` → severity OK, detail contains "vibew dev".
- `TestCheckPort_InUseByForeign_Fail` — probe returns `OwnerForeign` →
  severity FAIL.
- `TestCheckPort_NoProbe_BusyStillFails` — `ownerProbe` nil + busy → FAIL
  (back-compat).
- `TestCheckTLSCertValid_LiveSidecarCertOK` — fake handshake returns a
  cert valid >30 days → severity OK, detail mentions "valid until".
- `TestCheckTLSCertValid_LiveSidecarCertExpiringSoon` — cert valid 3 days →
  WARN.
- `TestCheckTLSCertValid_SidecarUnreachable` — handshake fails → WARN,
  detail contains "start 'vibew dev'".
- `TestCheckTLSCertValid_ProviderNotSelfSigned` — cfg.TLS.Provider = "external"
  → existing OK path unchanged.

**Unit tests (new in `internal/app/ops/remote_error_test.go`):**

Table-driven, cases cover:
- `exit status 127` + stderr `"docker: command not found"`.
- `exit status 126` + stderr `"permission denied"`.
- `exit status 1` + stderr containing `2>/dev/null` (pre-existing leak) —
  must strip the literal redirection from output.
- `exit status 1` + multi-line stderr — must emit only the first non-empty
  line.
- nil error — empty string.

Assertions (for every case):
- no newline in output
- no substring `"2>/dev/null"`, `"||"`, `"ssh "`
- exit code present when non-zero
- trailing hint present

**Integration test (new in `internal/app/ops/doctor_integration_test.go`)
guarded by `//go:build integration` tag:**

- Start a real `vibew dev` against an ephemeral project directory.
- Poll `/_vibewarden/health` until it returns 200.
- Run `DoctorService.Run` with the real probe wired.
- Assert zero FAIL and zero WARN on "Proxy port", "TLS certificate", and
  "Container health".
- Cleanup via `t.Cleanup(...)`.
- Skip on CI when Docker is unavailable via `testing.Short()`.

### New dependencies

None. Uses only `crypto/tls`, `net/http`, `encoding/json`, `context`,
`errors`, `os/exec` — all stdlib.

## Consequences

**Positive**

- Deterministic, pure-Go port-ownership detection on macOS and Linux.
  No binary dependencies (`lsof`, `ss`, `netstat`) and no new coupling
  between `vibew dev` and `vibew doctor` beyond the already-published
  liveness endpoint.
- The TLS cert check is now honest: it reports whether the sidecar is
  actually serving a valid cert rather than whether a file sits at a
  hard-coded path that has been wrong since the switch to Caddy-managed
  storage.
- User-facing doctor output contains no shell fragments — the cosmetic
  bug is fixed structurally at the SSH adapter boundary, not by
  string-munging inside the doctor check.
- `PortOwnerProbe` is reusable — future tools (MCP diagnostics, admin
  CLI) that need the same "is this my sidecar?" answer share one
  implementation.

**Negative**

- `/_vibewarden/health` becomes a soft public contract for doctor — the
  JSON prefix `{"status":"ok","version":` must remain stable. This is
  already effectively the case (it is the liveness endpoint users and
  monitoring tools hit). Any future change to the body must keep the
  prefix or update this ADR.
- TLS handshake + HTTP round-trip costs ~5-20ms on localhost on top of
  the current ~1ms bind probe. Acceptable — doctor is interactive and
  one extra roundtrip is imperceptible.

**Follow-up**

- Full deletion of the production-only doctor checks (`checkSSHConnectivity`,
  `checkArchCompatibility`, `checkRemoteContainerHealth`, `checkDomainDNS`,
  `checkRemoteTLSCert`, and the `Target` / `RemoteExecutor` plumbing) is
  tracked as a separate chore issue blocked on `#1051` (deploy sunset).
  This is not ADR-worthy — the architectural decision (delete production
  surface from doctor when deploy is sunset) was already made in the
  scope of #1051. The follow-up is scoped tracking work.
