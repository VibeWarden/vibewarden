# ADR-096: `vibew eject` — keep-and-clarify as the non-Docker escape hatch

**Date**: 2026-04-23
**Issue**: #1147
**Status**: Accepted

## Context

Retro 0.17.0 (agent additional drop #4) flagged `vibew eject` as redundant with
`vibew bundle`. The PM investigation found that the two commands are
**categorically different**:

| Command | Output | Use case |
|---|---|---|
| `vibew eject` | Raw Caddy JSON to stdout | Operator running Caddy directly without Docker (vanilla Caddy / k8s sidecar) |
| `vibew bundle` | Docker-Compose-shaped directory (`docker-compose.yml`, `image.tar`, merged YAML, `.env`, `README.md`) | Deploying the secured app to a VPS via Docker Compose |

`vibew bundle` does not cover the non-Docker case at all (ADR-085 locks
bundle as Compose-only).

**Root cause of the agent confusion** is a single wrong line in the agents
template (`internal/cli/templates/agents/agents-vibewarden.md.tmpl:94`):

```
| `vibew eject` | Eject to raw Docker Compose |
```

Eject has nothing to do with Docker Compose. That stale description is what
makes agents conflate eject with bundle. The retro complaint is a docs problem
masquerading as a feature problem.

**Investment context**: `internal/app/eject/` was raised to 100% test coverage
in PR #1128 (referenced from #1101). LOC: ~1127 (app + cmd + tests). External
callers in docs/MCP/CI/examples/integration tests: zero.

The PM spec recommended deprecate-and-remove in v0.19.0. The orchestrator
pushed back: per CLAUDE.md §Artifact policy ("real or pure instruction"), eject
is a real artifact (raw Caddy JSON) — the fix belongs at the instruction
layer, not the verb. Removing a working escape hatch with 100% coverage
because one line of doc was wrong is overcorrection.

## Decision

**Keep `vibew eject`. Fix the docs that caused the confusion.** No deprecation,
no removal milestone.

Three concrete doc-layer changes resolve the retro friction:

1. **Fix the agents-template line** — replace the factually wrong "Eject to
   raw Docker Compose" description with one that names the actual artifact
   and the disambiguator.
2. **Tighten `vibew eject --help`** — the `Short` description leads with the
   non-Docker positioning; the `Long` description explicitly disambiguates
   from `vibew bundle`.
3. **Add a one-liner to README.md and llms-full.txt** that names the use
   case (non-Docker) so the verb table rows are self-explanatory.

No new ADR for the `vibew bundle` side — bundle's scope (Compose-only) is
already locked by ADR-085.

### Domain model changes

None. No domain, ports, or adapter changes. This is a docs + cobra-string
change.

### Ports (interfaces)

None.

### Adapters

None.

### Application service

No code changes to `internal/app/eject/`. The application service stays as-is
(the 100% test coverage from #1128 is preserved).

### File layout (no new files; edits only)

- `internal/cli/cmd/eject.go` — update `Short` and `Long` strings on the
  cobra command. Update the godoc on `NewEjectCmd`.
- `internal/cli/templates/agents/agents-vibewarden.md.tmpl` — line 94: replace
  the wrong description.
- `README.md` — lines 210 and 319: clarify the use case.
- `llms-full.txt` — line 1341: clarify the use case.
- `docs/examples/AGENTS-VIBEWARDEN.md` — if it carries the same wrong line,
  fix it (the dev should grep to confirm; PM's investigation said zero
  occurrence, but the developer must verify).

### Sequence

This is a docs change. There is no runtime sequence change. The runtime
behaviour of `vibew eject` is unchanged.

### Concrete copy

**`vibew eject` `Short` (cobra)** — replace:

> Export the equivalent raw proxy config from vibewarden.yaml

with:

> Export raw Caddy JSON for non-Docker deploys (most users want `vibew bundle`)

**`vibew eject` `Long` (cobra)** — first paragraph replacement:

> Export the raw Caddy JSON configuration equivalent to the current
> vibewarden.yaml. Use this when you run Caddy directly (vanilla Caddy host,
> k8s sidecar, or any non-Docker setup) and want VibeWarden to generate the
> Caddy config for you.
>
> If you deploy via Docker Compose, use `vibew bundle` instead — it produces
> a complete deploy package (compose file, image tar, merged config, .env,
> README) rather than just the raw proxy config.

The remainder of `Long` (supported formats, internal-endpoint note,
examples) stays as-is.

**`agents-vibewarden.md.tmpl:94`** — replace:

```
| `vibew eject` | Eject to raw Docker Compose |
```

with:

```
| `vibew eject` | Export raw Caddy JSON config for non-Docker deploys (most setups should use `vibew bundle` instead) |
```

**README.md:210** — replace:

```
| Eject | `vibew eject` — export raw proxy config to graduate past VibeWarden |
```

with:

```
| Eject | `vibew eject` — export raw Caddy JSON for non-Docker deploys (Docker users want `vibew bundle`) |
```

**README.md:319 and llms-full.txt:1341** — same disambiguation tail
(`(Docker users want `vibew bundle`)`).

### Error cases

No new error paths. Existing error paths in `internal/app/eject/` and
`internal/cli/cmd/eject.go` are unchanged.

### Test strategy

This is a copy/docs change with two test-worthy invariants:

1. **`vibew eject --help` output contains the disambiguating phrase.** Add
   one assertion to an existing CLI-help test (or a new tiny test in
   `internal/cli/cmd/eject_test.go`) that checks `Short` and `Long` mention
   the words `non-Docker` and `vibew bundle`. This locks the
   "agent reads --help, knows when to use it" guarantee.

2. **The agents template no longer says "Docker Compose" on the eject row.**
   Add an assertion to the agents-template test (the file already has
   coverage; locate it via `grep -rn "agents-vibewarden.md.tmpl" internal/`
   — likely under `internal/cli/templates/agents/` or
   `internal/app/scaffold/`). Check that the rendered template does **not**
   match `vibew eject.*Docker Compose` and **does** match `vibew eject.*Caddy`.

3. **Existing `internal/app/eject/` tests stay green** (no behaviour change).

No new integration tests; no new unit tests beyond the two assertions above.

### New dependencies

None.

## Consequences

**Positive:**

- Preserves a real escape hatch (raw Caddy JSON for non-Docker users) at
  zero ongoing cost. The 100% coverage from #1128 is not wasted.
- Resolves the retro friction at its actual source (the wrong agents-template
  line) rather than amputating the feature.
- No deprecation cycle, no migration burden, no v0.19.0 follow-up issue.
- Honours CLAUDE.md §Artifact policy: keep the real artifact, fix the
  instructions.

**Trade-offs / negative:**

- One more verb in the surface area — agents must learn one more disambiguation
  rule (Docker → bundle, non-Docker → eject). The clarified docs make this
  explicit, but it is one more thing to read.
- `--format` flag still only supports `caddy`. The stub `nginx`/`traefik`
  values in earlier comments remain dead surface. **This ADR does not address
  format expansion.** If the dead-format question becomes pressing, file a
  follow-up issue; do not bundle it into the docs fix.
- If a future retro re-raises the same complaint, we revisit deprecation.
  The clarified docs are the test: if agents still get confused after this
  change, the hypothesis (docs were the problem) was wrong and removal
  becomes the right answer.

**Did the agents-template fix alone prevent the qr-dali agent's confusion?**
Yes. The investigation showed the agent read line 94, saw "Eject to raw
Docker Compose," and inferred eject and bundle were two paths to the same
artifact. Replacing that one line with the correct description ("raw Caddy
JSON for non-Docker deploys") removes the conflation entirely. The
`--help` and README clarifications are belt-and-braces.
