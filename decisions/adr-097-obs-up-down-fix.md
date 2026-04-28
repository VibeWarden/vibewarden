## ADR-097: Fix `vibew obs up` no-op and `vibew obs down` nuking the main stack

**Date**: 2026-04-23
**Issues**: #1176, #1177
**Status**: Accepted

### Context

#1149 / PR #1168 introduced two regressions surfaced by the v0.18.0-candidate smoke
test:

1. **#1176** — `vibew obs up` silently no-ops on fresh projects. The compose template
   wraps every observability service in `{{- if .Observability.Enabled }}`, a config
   flag the user must set manually. `docker compose --profile observability up -d`
   succeeds with zero containers when no service matches the profile, so the CLI
   prints the success banner and Grafana/Prometheus URLs even though nothing started.

2. **#1177** — `vibew obs down` calls `docker compose down --profile observability`.
   Compose's `down` verb removes ALL services in the project regardless of profile —
   the `--profile` flag is an *activation* mechanism for `up`, not a *scope limiter*
   for `down`. After `obs up` + `obs down`, the sidecar and app are stopped.

Both bugs share the same subsystem (compose template + ObsService + ComposeRunner).
A single PR fixes them. The profile-overlay design from #1149 stays — we are
correcting how it is *used*, not reverting it.

### Decision

#### Domain model changes
None. This is an adapter + template fix; no new domain entities.

#### Ports (interfaces)

`internal/ports/ops.go` — `ComposeDownOptions` gains a `Services` field:

```go
// ComposeDownOptions carries optional arguments for ComposeRunner.Down.
type ComposeDownOptions struct {
    Volumes       bool
    RemoveOrphans bool

    // Services limits teardown to the named compose services.
    //
    // When non-empty, the adapter performs a service-targeted teardown by
    // running `docker compose stop <services...>` followed by
    // `docker compose rm -f <services...>` instead of `docker compose down`.
    // This is the correct way to tear down a subset of a compose project —
    // `docker compose down --profile <name>` does NOT scope teardown by
    // profile (compose's --profile is an activation flag for `up`, not a
    // scope limiter for `down`) and would remove all services in the
    // project. When empty, behaviour is unchanged: full project teardown
    // via `docker compose down`.
    //
    // Volumes and RemoveOrphans interact as follows when Services is set:
    //   - Volumes: not honoured by `compose stop`/`compose rm`. Named
    //     volumes used only by the listed services are removed by a
    //     follow-up `docker volume rm` step (best-effort; ignore
    //     "in use" / "no such volume" errors).
    //   - RemoveOrphans: ignored when Services is non-empty (orphan
    //     removal is a project-level concept).
    Services []string

    // Profiles is RETAINED for the project-level Up flow on the adapter
    // (passed through `Up`'s separate profiles argument). For Down it is
    // DEPRECATED — keeping the field avoids breaking compilation but the
    // adapter no longer forwards it to `docker compose down`. Remove in
    // a follow-up cleanup once no caller references it.
    Profiles []string
}
```

The `Profiles` field stays for now (zero callers other than the broken
`ObsService.Down` path; a removal in this same PR is fine if the dev confirms
no downstream uses — architect's preference is **delete `Profiles` in this PR**
since the only known caller is the one being fixed).

#### Adapters

`internal/adapters/ops/compose.go` — `Down` is updated to branch on
`opts.Services`:

- `len(opts.Services) == 0` (current behaviour): run `docker compose [-f <file>] down [--volumes] [--remove-orphans]` exactly as today.
- `len(opts.Services) > 0`: run `docker compose [-f <file>] stop <services...>` then `docker compose [-f <file>] rm -f <services...>`. If `opts.Volumes` is true, follow with a best-effort `docker volume rm <project>_<volname>` for each volume known to belong only to the listed services. The volume-name list is the obs volume set (`prometheus-data`, `loki-data`, `grafana-data`) — pass this through from the caller to keep the adapter generic. Concretely: add a sibling field `VolumeNames []string` to `ComposeDownOptions` that, when set together with `Volumes=true`, controls which named volumes are removed. When `VolumeNames` is empty, fall back to the existing `--volumes` flag (full project semantics).
- "no such service" / "has no containers" stderr is treated as a no-op (same tolerance as the current `down` path).
- Stop/rm output is parsed for `Removed` lines into the existing `DownResult` counters so the UX summary keeps working.

The adapter doc comment must call out: "When `Services` is non-empty, this method
performs `stop` + `rm` rather than `down`, because `docker compose down --profile`
does not scope teardown by profile."

#### Application service

`internal/app/ops/obs.go`:

1. Add a package-level static list:

   ```go
   // obsServices is the canonical list of services in the "observability" profile.
   // It must stay in sync with internal/config/templates/docker-compose.yml.tmpl.
   var obsServices = []string{
       "prometheus",
       "loki",
       "promtail",
       "otel-collector",
       "jaeger",
       "grafana",
   }
   ```

   Static list is fine — the obs profile is fixed and there is no plugin-driven
   extension point. A drift-detection unit test (see Test strategy) keeps the list
   honest against the template.

2. Rewrite `ObsService.Down` to call:

   ```go
   result, err := s.compose.Down(ctx, composeFile, ports.ComposeDownOptions{
       Volumes:     opts.Volumes,
       Services:    obsServices,
       VolumeNames: []string{"prometheus-data", "loki-data", "grafana-data"},
   })
   ```

   Drop `Profiles` from the call. Drop `RemoveOrphans` propagation (orphan removal
   does not apply to a service-targeted teardown — see field doc above).

3. `Up` is unchanged: profiles are still the correct activation mechanism for `up`.

#### Compose template change

`internal/config/templates/docker-compose.yml.tmpl`:

- **Line 28–35** (header comment block): make unconditional — drop the `if .Observability.Enabled` guard. The comment lists what services exist in the profile; that is true regardless of activation.
- **Line 303** (services block): drop the `{{- if .Observability.Enabled }}` guard around prometheus, loki, promtail, otel-collector, jaeger, grafana. Each service already has `profiles: [observability]`, which makes them inert until `--profile observability` is passed to `up`.
- **Line 463** (volumes block): drop the `if .Observability.Enabled` guard. Declared-but-unused named volumes are inert in compose.
- **Line 109** (sidecar service env vars `VIBEWARDEN_TELEMETRY_OTLP_*`): **keep gated by `.Observability.Enabled`**. Open question 1 in the PM spec asks whether to make these unconditional. Architect decision: **keep gated**. Making them unconditional means the sidecar attempts OTLP push to `otel-collector:4318` whenever the user runs `vibew dev` — even if they never run `vibew obs up`. The collector is not in the running set in that case, so the sidecar emits connection errors on every export. This degrades the default UX. The OTLP env vars are correctly tied to the user's *intent* to use observability (`Observability.Enabled = true`), not to whether the services *exist* in the compose file. Users who want the obs stack to start running set `Observability.Enabled: true` in `vibewarden.yaml` AND can run `vibew obs up` to activate the profile; users who only set the flag get the OTLP wiring but no containers (intentional — they may have an external collector). The bug fix decouples *service definition* from *flag*, but keeps *sidecar telemetry config* tied to the flag.
- **Line 445** (depends_on conditional `or … .Observability.Enabled …`): leave unchanged. This depends-on-the-collector edge is meaningful only when the collector will be running; gating by `Observability.Enabled` is consistent with the OTLP-env-vars decision above.

#### `cfg.Observability.Enabled` — keep

Survey of uses (from grep):

- `internal/config/config.go:232` — config validation hook.
- `internal/config/telemetry.go:186` — telemetry plugin wiring.
- `internal/app/generate/service.go:292` and `helpers.go:25` — generator rendering paths (not the compose template gate; the OTLP env-var gate and friends).
- `internal/cli/cmd/generate.go:81` — CLI banner / generator branch.
- `internal/config/generator_input.go:26` — pipes the bool into `ObservabilityEnabled` for the template.

The flag still drives sidecar OTLP wiring, telemetry plugin activation, and other
plumbing. **Keep the field.** Only the three `if .Observability.Enabled` gates in
the compose template that control *service definition* and *volume declarations*
are removed. The `if .Observability.Enabled` gate around the sidecar OTLP env
vars stays.

#### File layout

No new files. All edits in existing files:

- `internal/ports/ops.go` — add `Services []string` and `VolumeNames []string` to `ComposeDownOptions`; remove `Profiles []string` from `ComposeDownOptions` (architect's preferred cleanup) — confirm no other caller before deletion.
- `internal/adapters/ops/compose.go` — `Down` branches on `len(opts.Services)`; new helper for service-targeted teardown; volume removal best-effort.
- `internal/adapters/ops/compose_test.go` — new tests for the service-targeted path.
- `internal/app/ops/obs.go` — add `obsServices` static list; rewrite `Down` body; drop Profiles.
- `internal/app/ops/obs_test.go` — replace `TestObsService_Down_PassesObservabilityProfile` with `TestObsService_Down_PassesObsServices`; drift test asserting `obsServices` matches the template.
- `internal/config/templates/docker-compose.yml.tmpl` — remove three `if .Observability.Enabled` guards (header comment, services block, volumes block).
- `test/integration/obs_lifecycle_test.go` — new file; full lifecycle test gated by `testing.Short()` and Docker availability.
- `CHANGELOG.md` — `[Unreleased] / Fixed` entry referencing both issues.

#### Sequence

`vibew obs up` (unchanged in shape, but now actually works because services exist
in the compose file regardless of `Observability.Enabled`):

1. CLI parses flags, loads `vibewarden.yaml` → `cfg`.
2. `ObsService.Up` optionally regenerates compose via `Generate(cfg, dir)`.
3. Sidecar advisory check (`compose.PS`).
4. `compose.Up(ctx, composeFile, []string{"observability"}, opts)` → adapter runs `docker compose -f … --profile observability up -d`.
5. Banner with URLs.

`vibew obs down`:

1. CLI parses flags.
2. Volume confirmation if `--volumes` and not `--yes`.
3. `compose.Down(ctx, composeFile, ComposeDownOptions{ Volumes, Services: obsServices, VolumeNames: obsVolumes })`.
4. Adapter runs `docker compose -f … stop prometheus loki promtail otel-collector jaeger grafana` then `docker compose -f … rm -f prometheus loki promtail otel-collector jaeger grafana`. If `Volumes` is true, follow with best-effort `docker volume rm <project>_prometheus-data` etc.
5. Parse stop/rm stderr into `DownResult`; print summary.
6. Main sidecar + app containers are untouched.

#### Error cases

| Case | Handling |
|---|---|
| `docker compose stop` reports "no such service" or "has no containers" for an obs service | Treated as no-op (same as existing `down` tolerance). Continue to `rm`. |
| `docker compose rm -f` reports the same | No-op. Don't fail. |
| `docker volume rm` fails with "in use" or "no such volume" | Best-effort; log at debug or skip silently. Don't fail the command. |
| Stderr is genuinely an error (daemon down, permission, etc.) | Wrap with `fmt.Errorf("docker compose stop: %w\nstderr: %s", err, stderr)`. |
| Compose file missing | Existing tolerance applies (no-op). |

#### Test strategy

**Unit — `internal/app/ops/obs_test.go`:**
- Replace `TestObsService_Down_PassesObservabilityProfile` with `TestObsService_Down_PassesObsServices`: fake `ComposeRunner` records the `ComposeDownOptions` it received; assert `Services == []string{"prometheus","loki","promtail","otel-collector","jaeger","grafana"}` and `Profiles == nil` (or absent if removed from struct).
- Add `TestObsService_Down_PassesObsVolumeNames`: when `Volumes=true`, assert `VolumeNames == []string{"prometheus-data","loki-data","grafana-data"}`.
- Add a drift test `TestObsServices_MatchTemplate`: parse `internal/config/templates/docker-compose.yml.tmpl`, extract every service that has `profiles: [observability]`, assert the set equals `obsServices`. Prevents future template drift.

**Unit — `internal/adapters/ops/compose_test.go`:**
- `TestComposeAdapter_Down_ServicesPath_RunsStopThenRm`: stub `exec.Command` (or use a fake `docker` on PATH) — assert two invocations in order: `docker compose -f <file> stop prometheus loki ...` and `docker compose -f <file> rm -f prometheus loki ...`. **Do NOT** assert a `down` invocation.
- `TestComposeAdapter_Down_NoServices_RunsDown`: regression — when `Services` is empty, the adapter still runs `docker compose down`.
- `TestComposeAdapter_Down_ServicesPath_VolumesCallsVolumeRm`: when `Volumes=true` and `VolumeNames` is set, assert `docker volume rm <project>_<vol>` is invoked for each name; tolerate failure.
- `TestComposeAdapter_Down_ServicesPath_TolerantOfNoSuchService`: stderr "no such service" → no error returned.

**Integration — `test/integration/obs_lifecycle_test.go` (new):**
Skipped by default unless Docker is available and `testing.Short()` is false:
1. `t.TempDir()` → `vibew init` (or programmatic generator) producing a fresh compose file.
2. `vibew dev` (or the equivalent `compose.Up` call) — assert sidecar + app are running.
3. `vibew obs up` — assert `docker compose ps --format json` lists 6 obs containers (or at least `grafana` and `prometheus`) plus the original sidecar/app set.
4. `vibew obs down` — assert obs containers are gone AND sidecar + app are still running.
5. `vibew down` — full teardown.
6. Use `testcontainers-go` only if needed; preferred path is shelling to the real `docker compose` CLI in the test workspace.

Coverage gates: existing 80% target on `internal/app/`. The `obs_test.go` table
already covers the Up path; the Down rewrite must keep the same coverage shape.

#### New dependencies

None. `os/exec` is stdlib; no new third-party libraries are introduced.

### Consequences

**Positive:**
- `vibew obs up` works on a fresh project with no `vibewarden.yaml` edits — matches user expectation.
- `vibew obs down` no longer destroys the user's running app.
- `ComposeDownOptions.Services` becomes a reusable seam for any future "tear down a subset" need (e.g. removing a single plugin's services).
- Drift-detection test pins the obs service list to the template — future template edits cannot silently desync.

**Negative / trade-offs:**
- The compose file is slightly larger on projects that don't use observability (six inert service definitions and three unused volumes). Compose loads them lazily; the cost is ~50 lines of YAML and a few KB on disk. Acceptable trade.
- `ComposeDownOptions.Volumes` semantics now depend on whether `Services` is set (project `--volumes` vs. service-targeted `volume rm`). The struct doc comments must make this explicit; tests must cover both paths.
- The decision to keep OTLP env vars gated by `Observability.Enabled` means a user who only sets the flag (without running `vibew obs up`) gets sidecar OTLP push attempts to a non-existent collector. This is the existing behaviour and is out of scope for this fix; if it surfaces as a real problem, address it in a follow-up that decouples *config flag* from *service activation* more cleanly (e.g. make `Observability.Enabled` mean "the obs profile activates by default" and introduce a separate `Telemetry.OTLPEndpoint` for the wiring).
- Removing `ComposeDownOptions.Profiles` is a minor breaking change to `internal/ports/`. Internal package; no external consumers; safe.
