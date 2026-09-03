# ADR-111: Sidecar container resource limits — legacy compose keys, config-driven, with a matching `GOMEMLIMIT`

**Date**: 2026-09-03
**Issue**: [#1306](https://github.com/vibewarden/vibewarden/issues/1306)
**Status**: Accepted

---

## Context

Neither generated compose file caps the `vibewarden` service. A memory leak or a
goroutine explosion in the sidecar consumes host RAM/CPU/PIDs until the VPS dies —
taking down the app the sidecar exists to protect. ADR-110 bounded *inbound connections*
at the Caddy listener; this ADR bounds the *container* itself. They are complementary:
`server.max_connections` is a graceful in-process refusal, container limits are the
backstop for everything `max_connections` cannot see (leaks, runaway loops, fork bombs
in an exec'd subprocess).

The PM spec (#1306) fixes: three limits on the `vibewarden` service only, in both
templates, defaults `512M` / `1.0` / `200`, configurable under the existing `server:`
block, `0` disables (matching ADR-110's mental model), negatives rejected by
`vibew validate` / `vibew bundle`, and — explicitly — a test that proves *enforcement*
via `docker inspect`, not a rendered-string assertion.

Three things delegated here: exact key names, whether to extend ADR-110 or write a new
ADR, and whether multi-app mode needs different PID defaults.

**Compose key survey (empirically verified, not read off docs).** Docker 29.7.2 /
Compose v5.3.1, non-swarm `docker compose up -d`, then `docker inspect`:

| Compose YAML | `HostConfig.Memory` | `HostConfig.NanoCpus` | `HostConfig.PidsLimit` |
|---|---|---|---|
| `mem_limit: 512M` + `cpus: 1.0` + `pids_limit: 200` | 536870912 | 1000000000 | 200 |
| `deploy.resources.limits.{memory,cpus,pids}` | 536870912 | 1000000000 | 200 |
| all three set to `0` | 0 (unlimited) | 0 (unlimited) | nil (unlimited) |

Both spellings work on current Compose. The legacy service-level keys are chosen: they
carry no Swarm connotation (the PM spec rules Swarm semantics out of scope), they have
been honoured by every Compose v2+ release rather than only recent ones, and
`pids` under `deploy.resources.limits` is a much newer and less widely supported spelling
than `pids_limit`. Quoted and unquoted scalars both apply correctly; this ADR quotes the
two that could otherwise be YAML-type-ambiguous.

## Decision

Add three keys to the existing `server:` block that are consumed **only at
generate/bundle time** to emit `mem_limit`, `cpus`, and `pids_limit` on the `vibewarden`
service in both compose templates, plus a derived `GOMEMLIMIT` environment variable that
makes the memory cap degrade into GC pressure instead of an OOM kill.

### Domain model changes

None. `internal/domain/` is untouched. These are deployment-artifact parameters with no
domain meaning.

### Ports (interfaces)

**No new port, and deliberately no change to `ports.ProxyConfig`.** Unlike
`server.max_connections`, these values never reach the running sidecar's proxy — they are
render-time inputs to a compose file. Threading them into `ProxyConfig` would imply a
runtime control that does not exist. The one thing the *process* sees is `GOMEMLIMIT`,
and it sees it as an environment variable read by the Go runtime, not as config.

### Config layer

`internal/config/sidecar.go` — `ServerConfig` gains three fields:

```go
// MemLimit caps the memory of the vibewarden sidecar container in the
// generated Docker Compose file (compose `mem_limit`). Accepts a byte size
// with an optional unit — "512MB", "512M", "1GB" — or a plain byte count.
// A value of "0" or "" disables the cap: the key is omitted from the
// generated compose file entirely. Malformed or negative values are rejected
// by validation. Consumed only when generating deployment artifacts; the
// running sidecar never reads it. Default: "512MB".
MemLimit string `mapstructure:"mem_limit"`

// CPULimit caps the CPU available to the vibewarden sidecar container
// (compose `cpus`), expressed in cores — 0.5 means half a core. A value of 0
// disables the cap. Negative values are rejected by validation.
// Generate-time only. Default: 1.0.
CPULimit float64 `mapstructure:"cpu_limit"`

// PidsLimit caps the number of processes and OS threads the vibewarden
// sidecar container may create (compose `pids_limit`). A value of 0 disables
// the cap. Negative values are rejected by validation. Generate-time only.
// Default: 200.
PidsLimit int `mapstructure:"pids_limit"`
```

Key names are the PM's suggested flat names. Flat, not a nested `server.resources:`
sub-block: `server.max_connections` is already a flat resource cap in the same block, and
a two-key-deep path buys nothing at three keys. `mem_limit` and `pids_limit` match the
compose keys they render to exactly; `cpu_limit` deviates from compose's `cpus` on
purpose, so the three read as a set.

`internal/config/config.go` — `setDefaults`:

```go
v.SetDefault("server.mem_limit", "512MB")
v.SetDefault("server.cpu_limit", 1.0)
v.SetDefault("server.pids_limit", 200)
```

This is what makes "omitted = default" and "explicit 0 = unlimited" distinguishable
without pointer fields — same pattern as `server.max_connections` (ADR-110).

`internal/config/sidecar.go` — `validateSidecar` gains:

```go
if c.Server.MemLimit != "" {
    if _, err := ParseMemLimit(c.Server.MemLimit); err != nil {
        errs = append(errs, fmt.Sprintf("server.mem_limit: %s", err.Error()))
    }
}
if c.Server.CPULimit < 0 {
    errs = append(errs, fmt.Sprintf(
        "server.cpu_limit must be >= 0 (0 disables the limit), got %g", c.Server.CPULimit))
}
if c.Server.PidsLimit < 0 {
    errs = append(errs, fmt.Sprintf(
        "server.pids_limit must be >= 0 (0 disables the limit), got %d", c.Server.PidsLimit))
}
```

The strict loader (ADR-082) needs no change — `checkUnknownKeys` walks `mapstructure`
tags reflectively, so new fields on `ServerConfig` are automatically known keys, and
`Validate()` runs inside `loadInternal`, which both `vibew validate` and `vibew bundle`
go through. Env overrides `VIBEWARDEN_SERVER_MEM_LIMIT` / `_CPU_LIMIT` / `_PIDS_LIMIT`
work for free via the existing replacer.

### New file: `internal/config/resource_limits.go`

```go
// ParseMemLimit parses a memory limit into bytes, accepting both VibeWarden's
// byte-size syntax ("512MB", "1GB") and Docker's single-letter shorthand
// ("512M", "1g"). "" and "0" return 0, meaning unlimited.
func ParseMemLimit(s string) (int64, error)

// ComposeResourceLimits is the render-ready view of the sidecar container's
// resource caps. Every field is a string, and an empty string means "omit this
// key from the generated compose file" — so templates need only a plain
// {{ if }} guard and never have to reason about zero values.
type ComposeResourceLimits struct {
    MemLimitBytes   string // decimal byte count, e.g. "536870912"
    MemLimitDisplay string // the configured value echoed back, e.g. "512MB"
    GoMemLimit      string // GOMEMLIMIT value, e.g. "483183820B"
    CPULimit        string // e.g. "1" or "0.5"
    PidsLimit       string // e.g. "200"
}

// ResourceLimits returns the render-ready compose resource caps for the
// vibewarden sidecar service.
func (c ServerConfig) ResourceLimits() ComposeResourceLimits
```

`ParseMemLimit` normalises a trailing bare `k`/`m`/`g`/`t` (case-insensitive, preceded by
a digit) by appending `B`, then delegates to the existing `ParseBodySize`, which already
rejects negatives and unknown units. Reusing it keeps one byte-size grammar in the
codebase; the normalisation exists because the PM spec and every Docker doc write `512M`,
and rejecting that with `unknown unit "M"` would be a hostile paper cut.

`ResourceLimits()` is a method on the value receiver so the single-app template can call
it as `{{ with .Server.ResourceLimits }}`. It renders bytes rather than passing the user's
string through: the generated compose then carries one unambiguous integer, the
integration assertion is exact, and the byte count is needed for `GOMEMLIMIT` anyway. The
human value is preserved as a trailing YAML comment so the file stays readable.
`CPULimit` is rendered with `strconv.FormatFloat(v, 'f', -1, 64)`. A malformed `MemLimit`
yields empty strings — validation rejects it long before render, so this path is a
tripwire, not a behaviour.

**`GOMEMLIMIT` = 90% of `MemLimitBytes`, rendered with a `B` suffix.** This is the
non-obvious half of the change and the reason it is worth an ADR. Go's GC sizes the heap
against a target it derives from live heap, not from the cgroup limit; a container memory
cap with no `GOMEMLIMIT` makes the sidecar *more* likely to die under load, because the
kernel OOM-kills a process that would otherwise have collected. With `GOMEMLIMIT` at 90%
of the cap the runtime GCs harder as it approaches the ceiling and the container degrades
in latency rather than being killed. The 10% headroom covers non-heap allocation (stacks,
runtime metadata, cgo). Emitted only when the memory cap is on.

The CPU counterpart needs nothing: `GOMAXPROCS` has been cgroup-quota-aware by default
since Go 1.25 and this module is on `go 1.27`, so `cpus: 1.0` already yields
`GOMAXPROCS=1` inside the container.

### Templates

`internal/config/templates/docker-compose.yml.tmpl`, in the `vibewarden` service only,
immediately after `restart: unless-stopped`:

```
{{- with .Server.ResourceLimits }}
{{- if .MemLimitBytes }}
    mem_limit: {{ .MemLimitBytes }}  # {{ .MemLimitDisplay }}
{{- end }}
{{- if .CPULimit }}
    cpus: "{{ .CPULimit }}"
{{- end }}
{{- if .PidsLimit }}
    pids_limit: {{ .PidsLimit }}
{{- end }}
{{- end }}
```

and, inside that service's existing unconditional `environment:` list:

```
{{- with .Server.ResourceLimits }}{{ if .GoMemLimit }}
      - GOMEMLIMIT={{ .GoMemLimit }}
{{- end }}{{ end }}
```

`internal/config/templates/sidecar-compose.yml.tmpl` (multi-app) gets the same three keys
from `.Limits`, plus a *conditional* `environment:` block — that template has none today:

```
{{- if .Limits.GoMemLimit }}
    environment:
      - GOMEMLIMIT={{ .Limits.GoMemLimit }}
{{- end }}
```

`internal/app/bundle/helpers.go` — `SidecarComposeData` gains
`Limits config.ComposeResourceLimits`. `internal/app/bundle/bundle.go` —
`renderSidecarCompose(listenPort int, version string, limits config.ComposeResourceLimits)`;
`bundleMultiSiteSidecar` already holds `cfg`, so it passes `cfg.Server.ResourceLimits()`.
The single-app path needs no signature change — `RenderToFile` already receives `cfg`.

**Multi-app uses the same defaults as single-app** (PM open question 3). One sidecar
process serves every site in both modes; PID count tracks Go runtime threads
(≈ `GOMAXPROCS` plus blocking syscalls), not site count, and each site's app runs in its
own container with its own compose file. Divergent defaults would be a number with no
derivation behind it. Multi-app users who fan out to many high-traffic sites tune the
same three keys.

### Application service

No new application service and no new use case. Two existing flows gain a mapping step:
`internal/app/generate` (template data is already the whole `*config.Config`) and
`internal/app/bundle` (one new struct field).

### File layout

New:
- `internal/config/resource_limits.go`
- `internal/config/resource_limits_test.go`
- `test/integration/compose_limits_test.go`
- `decisions/adr-111-sidecar-container-resource-limits.md` (this file)

Modified:
- `internal/config/sidecar.go` — three `ServerConfig` fields + three `validateSidecar` rules
- `internal/config/config.go` — three `setDefaults` entries
- `internal/config/templates/docker-compose.yml.tmpl` — limits + `GOMEMLIMIT` on `vibewarden`
- `internal/config/templates/sidecar-compose.yml.tmpl` — same, via `.Limits`
- `internal/app/bundle/helpers.go` — `SidecarComposeData.Limits`
- `internal/app/bundle/bundle.go` — `renderSidecarCompose` signature + call site
- `internal/config/reference_yaml_test.go` — three rows in `TestSetDefaults_EmptyYAML`
- `internal/config/sidecar_test.go` — validation table rows
- `internal/app/generate/service_test.go` — rendered single-app compose assertions
- `internal/app/bundle/bundle_test.go` — rendered multi-app sidecar compose assertions
- `vibewarden.reference.yaml` — annotate the three keys in the `server:` block
- `docs/configuration.md` — three table rows (~line 41) and the `server:` example
- `llms-full.txt` — three rows in the config table (~line 269) and the `server:` example
- `CHANGELOG.md` — Unreleased entry, flagged as a default-behaviour change
- `decisions/README.md` — index row for ADR-111

### Sequence

1. `vibew generate` / `vibew bundle` → `config.Load` → viper applies `512MB` / `1.0` / `200`
   unless YAML or env overrides them.
2. `Config.Validate` → `validateSidecar` rejects a malformed `mem_limit`, a negative
   `cpu_limit`, or a negative `pids_limit`, naming the field and the value.
3. Render: `ServerConfig.ResourceLimits()` parses `MemLimit` to bytes, computes
   `GOMEMLIMIT` at 90%, formats the CPU float, and blanks any field whose cap is off.
4. Single-app: the template emits `mem_limit` / `cpus` / `pids_limit` on the `vibewarden`
   service and appends `GOMEMLIMIT` to its `environment:` list. Multi-app:
   `bundleMultiSiteSidecar` passes the same struct into `SidecarComposeData` and the
   sidecar template emits the same keys plus a conditional `environment:` block.
5. `docker compose up -d` → Compose maps the keys onto `HostConfig.Memory`,
   `HostConfig.NanoCpus`, `HostConfig.PidsLimit` on the sidecar container only.
6. The sidecar process starts with `GOMEMLIMIT` set; the Go GC targets a heap that fits
   inside the cgroup ceiling.

### Error cases

| Case | Handling |
|---|---|
| `server.mem_limit: "512X"` | `vibew validate` / `vibew bundle` fail: `server.mem_limit: invalid body size "512X": unknown unit "XB" (supported: B, KB, MB, GB, TB)` |
| `server.mem_limit: "-1MB"` | Rejected by `ParseBodySize`'s non-negative check |
| `server.cpu_limit: -0.5` / `server.pids_limit: -1` | Rejected with the ADR-110 phrasing: `must be >= 0 (0 disables the limit), got …` |
| Any limit set to `0` / `""` | The corresponding key is omitted from the generated compose file. Not emitted as `0` — Compose does treat `0` as unlimited, but an absent key states the intent unambiguously and keeps the artifact clean |
| `mem_limit` = 0 | `GOMEMLIMIT` is omitted too; the Go runtime keeps its default unlimited soft limit |
| Non-numeric YAML for `cpu_limit` / `pids_limit` | viper/mapstructure type error from `Unmarshal`, existing behaviour |
| Sidecar exceeds the memory cap | Kernel OOM-kills the container; `restart: unless-stopped` restarts it. Seconds of sidecar downtime instead of a dead host — the trade this ADR exists to make. `GOMEMLIMIT` is what makes this the rare path rather than the normal one |
| Sidecar exceeds the CPU cap | CFS-throttled, requests slow down, nothing is killed |
| Sidecar hits the PID cap | `pthread_create` fails; the Go runtime panics with `too many threads`, container restarts. 200 is far above a healthy sidecar's thread count |
| Host kernel without swap-limit support | Docker prints `Your kernel does not support swap limit capabilities` on start; the memory limit still applies. Cosmetic |
| `overrides.compose_file` set | The user's compose file is copied verbatim — no limits injected. Existing, documented escape-hatch behaviour |

### Test strategy

**Integration, `//go:build integration`, `test/integration/compose_limits_test.go` — the
load-bearing one.** The spec calls this out explicitly and `CLAUDE.md`'s Caddy-regex
precedent is the reason. Render a real compose file through the production generate path
with the default limits, `docker compose up -d vibewarden`, then

```
docker inspect <container> --format '{{.HostConfig.Memory}} {{.HostConfig.NanoCpus}} {{.HostConfig.PidsLimit}}'
```

and assert exactly `536870912`, `1000000000`, `200`. Tear down with `down -t 0` in a
`t.Cleanup`. Gate on `exec.LookPath("docker")` **and** `docker version` succeeding, per the
existing pattern in `test/integration/bundle_test.go`. Use `cfg.SidecarImage =
"vibewarden:local-test"` (the image `make integration` already builds) so the test neither
pulls from ghcr nor depends on a release tag, and give `server.port` a high random port so
concurrent runs do not collide. The sidecar's upstream being unreachable is fine — the
container runs, which is all `docker inspect` needs. A second case sets all three limits to
`0` and asserts the container comes up with `Memory=0`, `NanoCpus=0`, `PidsLimit` unset.

**Unit — config.** `resource_limits_test.go`, table-driven: `ParseMemLimit` over `512MB`,
`512M`, `512m`, `1GB`, `1g`, `536870912`, `0`, `""`, `-1MB`, `512X`, `abc`;
`ResourceLimits()` over the default config (asserting `MemLimitBytes == "536870912"`,
`GoMemLimit == "483183820B"`, `CPULimit == "1"`, `PidsLimit == "200"`), each cap
individually zeroed, and all three zeroed. Add the three default rows to
`TestSetDefaults_EmptyYAML` in `reference_yaml_test.go` — that test exists precisely to
catch a documented default that `setDefaults` never registers. Validation rows in
`sidecar_test.go` for each rejection above.

**Unit — render shape.** Assert the three keys appear inside the `vibewarden` service
block in both templates, that `GOMEMLIMIT` lands in that service's environment, and — the
assertion that actually protects scope — that **no other service** in the single-app
compose gains any of the three keys. Assert full absence when the caps are `0`. These are
necessary but explicitly not sufficient; they exist to localise a failure the integration
test would only report as "the wrong number".

Nothing needs mocking. The config layer is pure; the integration test uses the real
daemon.

### New dependencies

**None.** `strconv`, `strings`, `fmt`, `os/exec` from stdlib, plus the existing
`ParseBodySize`. No new library, so no licence review needed.

### Resolved open questions

**Q1 — key names: flat `server.mem_limit` / `server.cpu_limit` / `server.pids_limit`.**
Rationale above.

**Q2 — new ADR, not an ADR-110 extension.** ADR-110 is an in-process Caddy listener
wrapper; this is a deployment-artifact contract with a different enforcement layer, a
different consumer (Docker, not the sidecar), and a different failure mode (OOM kill vs
connection refusal). Amending ADR-110 would blur two decisions that a future reader needs
to tell apart. The change clears the ADR threshold on its own: three new config keys and a
change to the generated compose file, which is a public artifact.

**Q3 — same defaults in both modes.** Rationale above.

## Consequences

- **Behaviour change on regenerate.** Any existing project that re-runs
  `vibew generate` / `vibew bundle` gets a capped sidecar where it previously had none.
  That is the point, but it must be a visible CHANGELOG line, not a silent tightening.
  Users on constrained or unusually loaded hosts tune the three keys or set them to `0`.
- **A memory cap trades host death for sidecar restart.** With `restart: unless-stopped`
  an OOM kill costs seconds of proxy downtime. That is strictly better than the VPS dying,
  and `GOMEMLIMIT` should make it rare, but the sidecar is now killable by a limit we
  chose. `512MB` is generous for a Go reverse proxy on a single-VPS workload; the number
  is a default, not a claim about every workload.
- **`GOMEMLIMIT` is a soft limit, not a guarantee.** A genuine unbounded leak still ends
  in an OOM kill — it just arrives after the GC has fought it rather than before the GC ever reacted.
- **The escape hatch is unchanged.** `overrides.compose_file` users get no limits; that is
  consistent with every other generated-compose feature.
- **First render-time-only keys in the `server:` block.** `server.host`, `port`,
  `read_timeout`, `max_connections` all reach the running process; these three do not. The
  godoc on each field says so explicitly, because "I set `server.mem_limit` and `vibew
  serve` ignored it" is the obvious confusion. If a third render-time cluster ever appears
  in `server:`, that is the signal to reconsider the flat-key decision.
- **Not covered: `vibew dev`.** The dev-mode path and any hand-run `docker run` get no
  limits. Acceptable — this control is about production hosts.
- **Deliberately not covered: every other container.** `app`, `kratos`, `postgres`,
  `redis`, `openbao` stay unlimited, per the spec. The sidecar is the one container
  VibeWarden is responsible for; capping a user's app container without asking would be
  overreach. A follow-up could offer opt-in caps for the stack services.
- **`vibew doctor` could sanity-check the limits against host capacity** (spec fast-follow)
  and would now have a real reference value to check against — which is the gap ADR-110's
  Q1 deferred to this issue. Worth filing.
