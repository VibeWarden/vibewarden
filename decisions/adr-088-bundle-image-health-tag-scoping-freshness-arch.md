# ADR-088: Bundle image health — tag scoping, freshness, and arch warning

**Date**: 2026-04-20
**Issue**: #1084, #1085, #1091
**Status**: Accepted

## Context

Three independent bugs in `vibew bundle` combined to produce a silent
wrong-app deploy in the v0.16.0 fresh-agent retro (2026-04-21):

1. Scaffold and bundle hardcode a generic Docker tag (`vibewarden-app:latest`)
   that collides with every other VibeWarden-managed project on the same
   workstation.
2. Bundle never inspects the target image — it packages whatever happens to
   be tagged locally, regardless of age, origin, or whether the image was
   built for the current project.
3. Bundle never inspects the image architecture — an arm64 host producing a
   bundle for a presumed amd64 VPS silently ships an incompatible tar.

In combination: a green deploy of a completely unrelated app, masked by a
stale amd64 image under the shared tag. The sidecar proxied the wrong app
happily. This is a silent-wrong-result bug class — the worst kind, and a
release blocker for v0.17.0.

The PM spec (on issues #1084, #1085, #1091) combined the three fixes into a
single story, one branch, one PR. This ADR pins the technical design.

### Locked constraints from prior ADRs

- **ADR-082** (strict config merge): `vibew validate` already calls
  `config.LoadStrict`. The migration warning must hook after successful
  strict-load, not inside the loader.
- **ADR-085** (`vibew bundle` compose-only): bundle is purely local, writes
  under `--output`, never touches files outside. Any new behaviour must
  respect that boundary.
- **ADR-086** (sunset `vibew deploy`): do not re-introduce deploy semantics
  in the bundle pipeline.
- **CLAUDE.md** dependency rules: any new library must be Apache 2.0, MIT,
  BSD-2, or BSD-3.

### Existing source of truth for project name

`(*config.Config).ComposeProjectName()` (`internal/config/config.go:203`) is
the single derivation used by compose project name, build image naming
(`internal/app/ops/build.go:120`), bundle extras
(`internal/app/bundle/bundle_extras.go:61`), and dev stale-container detection
(`internal/app/ops/dev.go:304`). The same method is already used in
`internal/cli/cmd/bundle.go:145` to default `imageTag`. Reuse it verbatim —
there is exactly one project-name source of truth and this ADR keeps it
that way.

## Decision

Fold the three fixes into the existing `internal/app/bundle/` package and
surface a new `ImageInspector` port. No new external dependency.

### Invariants pinned by this ADR

These three invariants become part of the bundle contract. Any future
change that violates one is a regression.

1. **Tag-scope invariant.** Every generated `.env`, `docker-compose.yml`,
   `sample.env`, and build-image call uses
   `<ComposeProjectName>-app:latest`. The generic `vibewarden-app:latest` is
   never written by `vibew init`, `vibew wrap`, or `vibew bundle`. When a
   user project still carries the legacy tag, the tooling warns; it never
   silently rewrites user files.
2. **Freshness invariant.** `vibew bundle` inspects the target image before
   writing any file. If the most recent source-file mtime under the project
   root (respecting `.gitignore`, `.dockerignore`, and a fixed hard-coded
   ignore list) is newer than the image's `.Created` timestamp, the
   freshness field reads `STALE` and a warning is emitted. `--allow-stale`
   suppresses the warning but does not change the decision semantics.
   Staleness is never a hard failure.
3. **Arch invariant.** `vibew bundle` reads `.Os` / `.Architecture` from
   `docker image inspect` and compares against `--target-platform`
   (default `linux/amd64`). Mismatch emits a warning with the exact
   `vibew build --platform <target>` command. Mismatch is never a hard
   failure.

### Domain model changes

A pure-Go value type plus two error sentinels. No new aggregate, no new
domain event. All types live in `internal/app/bundle/` so the domain
remains untouched.

```go
// internal/app/bundle/image_health.go

// ImageInfo is the value object returned by ports.ImageInspector. It is a
// pure, serialisable view over `docker image inspect` output. All fields are
// required; callers must treat a zero value as "missing" only when
// ErrImageNotFound was returned from Inspect.
type ImageInfo struct {
    Tag          string    // fully qualified, e.g. "qr-van-gogh-app:latest"
    Digest       string    // sha256:…
    OS           string    // "linux"
    Architecture string    // "amd64", "arm64", etc.
    Created      time.Time // image creation timestamp (UTC)
    SizeBytes    int64
}

// Platform returns "<os>/<arch>" for rendering and comparison.
func (i ImageInfo) Platform() string

// FreshnessVerdict is the outcome of the stale-detector.
type FreshnessVerdict struct {
    Stale         bool
    ChangedCount  int       // number of files newer than ImageInfo.Created
    NewestMTime   time.Time // the mtime that tripped STALE; zero when !Stale
}

// ImageHealth is the final report rendered to stdout.
type ImageHealth struct {
    Image        ImageInfo
    Target       string           // e.g. "linux/amd64"
    Freshness    FreshnessVerdict
    ArchMismatch bool             // Image.Platform() != Target
    LegacyTag    bool             // image tag == "vibewarden-app:latest"
    AllowStale   bool
}
```

Two error sentinels live next to `ImageInspector` in `internal/ports/`:

```go
var ErrImageNotFound = errors.New("docker image not found")
var ErrDockerUnavailable = errors.New("docker daemon unavailable")
```

CLI maps `ErrImageNotFound` to exit code **2**, `ErrDockerUnavailable` to
exit code **3**, any other error to exit code **1**, success to **0**.

### Ports (interfaces)

One new port, added to `internal/ports/ops.go` next to `ImageExporter` so
related shell-out adapters cluster:

```go
// ImageInspector returns metadata from `docker image inspect <tag>` as a
// pure-Go value object. Implementations shell out to the docker CLI.
//
// Inspect returns ErrImageNotFound when the image is absent from the local
// daemon (a user-correctable condition, mapped to a distinct exit code by
// the CLI layer). It returns ErrDockerUnavailable when the daemon is
// unreachable. Any other error is wrapped and surfaced as a generic failure.
type ImageInspector interface {
    Inspect(ctx context.Context, tag string) (bundle.ImageInfo, error)
}
```

> Note on import direction: because `bundle.ImageInfo` is defined in
> `internal/app/bundle/`, and `internal/ports/` must not import `app/`, the
> interface is declared with its own identical struct in `internal/ports/` —
> see the File layout section for the exact declaration. This mirrors how
> `ports.ContainerInfo` is declared locally rather than imported from an
> application package.

### Adapters

One new adapter, `internal/adapters/ops/image_inspect.go`, implementing
`ports.ImageInspector` by shelling out to
`docker image inspect --format '{{json .}}' <tag>`. It parses the single-JSON
object output into the port's `ImageInfo` struct. Matches the existing
shell-out pattern established by `ImageExportAdapter`
(`internal/adapters/ops/image_export.go`).

Error mapping from `docker`:
- stderr contains `No such image` → `ports.ErrImageNotFound`
- `docker` command not found OR `Cannot connect to the Docker daemon` →
  `ports.ErrDockerUnavailable`
- anything else → wrapped with `fmt.Errorf("docker image inspect: %w", err)`

### Application service

Two new files plus two small additions to the existing service:

**`internal/app/bundle/image_health.go`** — new file.
Holds `ImageInfo`, `FreshnessVerdict`, `ImageHealth` types, the
`RenderImageHealth(h ImageHealth) string` pure formatter, and the
orchestrator `CheckImageHealth(ctx, opts) (ImageHealth, error)` that:

1. Calls `s.imageInspector.Inspect(ctx, opts.ImageTag)`.
2. If `ErrImageNotFound`, returns the error wrapped — **no freshness walk,
   no formatting**. The CLI layer prints the hard-failure message.
3. Otherwise walks the project root via `s.stalenessWalker.Walk(...)` to
   compute `FreshnessVerdict`.
4. Populates `ArchMismatch`, `LegacyTag`, `AllowStale` fields and returns
   the `ImageHealth` value.

**`internal/app/bundle/staleness.go`** — new file.
Holds the ignore-aware walker. See "Staleness walk" below for the
implementation choice. Exposes a narrow interface so tests inject fakes:

```go
type StalenessWalker interface {
    // NewestMTime returns the most recent mtime of any file under root
    // that is not ignored. It also returns the count of files whose mtime
    // is strictly after `threshold`. When root does not exist returns
    // zero time and zero count with no error.
    NewestMTime(root string, threshold time.Time) (newest time.Time, changedCount int, err error)
}
```

**`internal/app/bundle/service.go`** — additions only.
Adds `imageInspector ports.ImageInspector` and
`stalenessWalker StalenessWalker` fields plus `WithImageInspector(...)` and
`WithStalenessWalker(...)` chainable setters. Both nil-safe: when either is
nil, the orchestrator returns a "health block skipped" sentinel that the
CLI rewrites into a visible warning. Production wiring always sets both;
the nil path exists for existing tests that predate this ADR.

**`internal/app/bundle/bundle.go`** — the `Bundle` use case gains one new
step, ordered first:

1. If `opts.ImageInspector != nil`: compute `ImageHealth`. If image missing,
   return `ErrImageNotFound` unwrapped through the CLI layer. Else render
   the block to `opts.Out io.Writer`.
2. Existing generator + extras pipeline.

No existing behaviour moves. The health block is a pure new side-effect
ordered *before* the snapshot/restore dance.

### File layout

All paths are exact. Every new file is listed.

```
internal/
  ports/
    ops.go                                  # +ImageInspector iface, +errors, +ImageInfo (ports-local)
  adapters/
    ops/
      image_inspect.go                      # NEW: shell-out adapter
      image_inspect_test.go                 # NEW: table-driven tests
  app/
    bundle/
      bundle.go                             # +1 orchestration step, +BundleOptions fields
      image_health.go                       # NEW: ImageInfo/Freshness/ImageHealth types + formatter + orchestrator
      image_health_test.go                  # NEW: formatter goldens, orchestrator branches
      staleness.go                          # NEW: ignore-aware walker
      staleness_test.go                     # NEW: table-driven tests
      service.go                            # +imageInspector/stalenessWalker fields, +WithImageInspector / WithStalenessWalker
  cli/
    cmd/
      bundle.go                             # +--build, +--allow-stale, +--target-platform; wire inspector/walker
      validate.go                           # +legacy-tag warning (reads loaded cfg .env adjacent)
```

Docs / templates (not listed above but part of the PR):

```
internal/config/templates/env.template.tmpl         # switch default to <project>-app:latest
internal/config/templates/docker-compose.yml.tmpl   # fallback in ${VIBEWARDEN_APP_IMAGE:-…} becomes <project>-app:latest
AGENTS-VIBEWARDEN.md (template + generated copies)  # update tag + add health-block example
docs/llms-full.txt                                  # update bundle section
docs/getting-started.md                             # update docker build -t examples
docs/cli.md                                         # document new flags + exit codes + migration one-liner
CHANGELOG.md                                        # Breaking + Changed + Added
```

### Staleness walk — library choice

PM left the choice to the architect. Decision: **implement directly, no new
dependency.**

Rationale:

- `go.mod` already imports `github.com/moby/patternmatcher` (indirect,
  Apache 2.0) via Docker client — it is the canonical `.dockerignore`
  matcher. We can promote it to a direct dependency without a new license
  review.
- `.gitignore` semantics are nearly identical in the subset bundle cares
  about (negation `!`, `**`, directory suffix `/`, leading `/`). `moby/
  patternmatcher` handles the dockerignore subset and is a superset of
  what bundle needs for gitignore in practice.
- Alternative libraries evaluated:
  - `github.com/sabhiram/go-gitignore` — **not currently in go.mod**
    (retro notes asked us to check; it is absent). MIT license,
    well-maintained. Adding it would require a new direct dep just for
    this feature.
  - `github.com/denormal/go-dockerignore` — archived, not maintained.
    Rejected.
- Walker is shallow: ~200 lines total (recursive walk, compiled matchers,
  mtime comparison). Writing it directly avoids a new direct dep while
  reusing already-vendored code.

Implementation (in `internal/app/bundle/staleness.go`):

```go
// Hard-coded ignore set — always active regardless of .gitignore/.dockerignore.
// These are directories that must never count toward project freshness.
var hardIgnoreDirs = []string{
    ".git", ".vibewarden", "node_modules", "vendor", "dist",
    "build", "target", ".venv", "__pycache__",
}

// fileSystemStalenessWalker uses filepath.WalkDir + moby/patternmatcher.
// It reads .gitignore and .dockerignore from root once on construction,
// compiles the combined matcher, and walks the tree.
```

Behaviour:

- Follow symlinks: **no** (same default as `docker build`).
- Hidden files: follow `.gitignore` / `.dockerignore`. Hard-ignore list
  handles the common `.git`/`.venv` cases.
- Errors walking a subtree: log at `debug` level, skip, continue. Never
  error out of `NewestMTime` — bundle must not fail because of a single
  unreadable file.

### `--build` flag — UX table

The PM flagged this. Explicit matrix:

| Image exists? | Stale? | `--build` | `--allow-stale` | Outcome |
|---|---|---|---|---|
| yes | no  | off | any  | health block emitted, bundle proceeds |
| yes | yes | off | no   | health block emitted with STALE warning, bundle proceeds |
| yes | yes | off | yes  | health block emitted WITHOUT STALE, bundle proceeds |
| yes | any | on  | any  | `vibew build --platform <target>` runs first, then re-inspect, then bundle |
| no  | n/a | off | any  | **hard fail, exit 2**, no bundle files written |
| no  | n/a | on  | any  | `vibew build --platform <target>` runs first; if build succeeds, bundle proceeds |
| yes | any | any | any, arch mismatch | arch warning added; bundle proceeds |

`--build` is OFF by default (PM decision). When the image is missing and
`--build` is off, bundle exits 2 with the actionable message from the PM
spec §F (lists `vibew bundle --build`, `vibew build --platform <target>`,
and the raw `docker buildx` invocation as alternatives).

### Exit codes

| Code | Meaning | Source |
|---|---|---|
| 0 | success (including warnings and `--allow-stale` suppression) | normal return |
| 1 | unknown / generic failure (config invalid, I/O error, etc.) | `fmt.Errorf` path |
| 2 | image missing — user must build or supply `--build` | `ErrImageNotFound` from `ports.ImageInspector` |
| 3 | docker daemon unreachable | `ErrDockerUnavailable` from `ports.ImageInspector` |

Documented in `docs/cli.md` under a new "vibew bundle exit codes" section
and cross-linked from the `--help` long description.

### Sequence (request/response flow)

1. `cmd/bundle.go` parses flags (`--build`, `--allow-stale`,
   `--target-platform`) and calls `config.LoadStrict` (existing).
2. CLI resolves `imageTag` — precedence: `--image` flag > `.env`
   `VIBEWARDEN_APP_IMAGE` > `cfg.App.Image` > `cfg.ComposeProjectName() +
   "-app:latest"`.
3. CLI resolves `targetPlatform` — `--target-platform` > default
   `linux/amd64`.
4. If `--build`: invoke `app/ops.BuildService.Run(...)` with the resolved
   target platform. Abort on failure.
5. CLI constructs the bundle `Service` with real adapters:
   `bundlefs.OSFS`, `opsadapter.NewImageInspectAdapter()`,
   `bundle.NewFileSystemStalenessWalker(projectRoot)`,
   `opsadapter.NewImageExportAdapter()`.
6. Service orchestrator (`bundle.CheckImageHealth`) calls
   `ImageInspector.Inspect(ctx, imageTag)`.
   - On `ErrImageNotFound`: return wrapped; CLI prints hard-failure
     message and exits 2.
   - On `ErrDockerUnavailable`: return wrapped; CLI prints daemon message
     and exits 3.
7. Service computes `FreshnessVerdict` via `StalenessWalker.NewestMTime`.
8. Service assembles `ImageHealth` value and returns it.
9. CLI writes the rendered health block (from
   `bundle.RenderImageHealth(h)`) to stdout. The block is always printed,
   even with no warnings.
10. CLI proceeds to existing `svc.Bundle(ctx, opts)` flow.
11. On any error after health-block emission, bundle returns exit 1 and
    the snapshot/restore defer in `Bundle` protects `.env`.

### Docker image inspect — implementation

Shell out, not Docker API. Matches the existing adapter pattern
(`ImageExportAdapter` shells out to `docker save`). Avoids adding
`github.com/docker/docker` to direct deps — it is already indirect but
declaring it direct would pull in a large API surface that is not used
elsewhere in the sidecar.

Exact invocation:

```
docker image inspect --format '{{json .}}' <tag>
```

Parse the single JSON object. Fields consumed:
`Id`, `Architecture`, `Os`, `Created`, `Size`, `RepoDigests[0]` (when
`Digest` field is empty).

### `vibew validate` migration warning — hook point

ADR-082 locked `validate.go` around `config.LoadStrict`. The migration
check is a new post-validation step:

1. `validate.go` runs `config.LoadStrict` (existing, unchanged).
2. `validate.go` calls a new helper `detectLegacyAppImage(configDir)`
   which:
   - Looks for `.env` next to the base config.
   - Parses its `VIBEWARDEN_APP_IMAGE=` line with the same KEY=value
     parser used by `bundle_extras.readEnvTemplateKeys`.
   - Returns `true` when the value is exactly `vibewarden-app:latest`.
3. On `true`, `validate.go` prints the migration warning to stderr (not
   stdout — stdout is reserved for the `Configuration valid (...)`
   success line, which must stay machine-parsable).

The helper lives in `internal/cli/cmd/validate.go` — it is CLI concern,
not application logic. Unit tests drive it with table-driven inputs.

### Error cases

| Case | Error | Exit | UX |
|---|---|---|---|
| image absent | `ports.ErrImageNotFound` | 2 | hard-failure block; no bundle written; suggests `--build`, `vibew build`, `docker buildx` |
| docker daemon down | `ports.ErrDockerUnavailable` | 3 | one-line message, no health block |
| `--build` fails | wrapped `build.Service.Run` err | 1 | build output preserved; no bundle written |
| stale image | (no error) | 0 | health block with STALE warning |
| arch mismatch | (no error) | 0 | health block with arch warning |
| legacy tag (v0.16) | (no error) | 0 | health block with migration one-liner |
| walker hit unreadable dir | logged at `debug`; walker returns what it could | 0 | freshness might under-report; documented as known limit |
| `ImageInspector == nil` | defensive path | 0 | health block replaced with `Image health: skipped (no inspector wired)` — only possible in tests |

### Test strategy

- **Unit tests (next to code, `_test.go`):**
  - `image_health_test.go` — golden string tests for `RenderImageHealth`
    across four scenarios: fresh, stale, arch-mismatched, legacy-tag.
    Time formatting uses a fixed `time.Time` so goldens stay stable.
  - `staleness_test.go` — table-driven: fresh/stale with nested dirs,
    `.gitignore`/`.dockerignore` honored, hard-ignore list honored,
    unreadable-dir tolerated, missing-root returns zero.
  - `service_test.go` — `WithImageInspector` / `WithStalenessWalker`
    wiring; nil-safe paths.
  - `image_inspect_test.go` — fake `exec.Command` harness; covers
    missing-image, daemon-down, and happy-path JSON parse.
  - `bundle_test.go` — extended with cases: no bundle files written
    when `ErrImageNotFound`; health block emitted exactly once;
    `--allow-stale` removes STALE label.
  - `validate_test.go` — extended: legacy-tag in `.env` triggers
    warning; new-style tag does not.
- **Integration tests:**
  - `artifact_test.go` — extends with: `vibew init projectA` and
    `vibew init projectB` produce distinct tags in `.env`; bundled
    output references the project-scoped tag; legacy tag never appears.
  - `testcontainers-go` not needed — bundle does not need a live daemon
    for most cases; the `ImageInspector` fake suffices. A single opt-in
    integration test (`//go:build integration`) drives the real
    `docker image inspect` against a prebuilt image on CI.
- **Coverage target:** maintain ≥80% on `internal/app/bundle/` and
  `internal/adapters/ops/`. The PR CI (Build & Test workflow) enforces
  this.

### New dependencies

None direct. `github.com/moby/patternmatcher` is promoted from indirect
to direct in `go.mod` (same module, no new third-party code pulled in).
Apache 2.0 — already vetted by the existing Docker client import.

Verified: `go list -m -json github.com/moby/patternmatcher` shows
Apache-2.0. The library is the canonical `.dockerignore` matcher
maintained by the Moby project.

## Consequences

### Positive

- Single project-name source of truth preserved — `ComposeProjectName()`
  stays the one place that maps a project to its Docker identity.
- Silent-wrong-result bug class is closed for bundle: every bundle
  emits the health block once, every time, before any file is written.
- Exit codes carry real semantic content. Agents can distinguish
  "missing image" (user error, fixable with one command) from "bad
  config" (user error, different fix) from "docker daemon down"
  (environment failure).
- No new direct third-party dependency; promotion of an already-vendored
  module.
- Migration is opt-out, never silent: v0.16 projects see a loud warning
  on every `vibew validate` and `vibew bundle` until they run the
  one-liner.

### Negative / trade-offs

- `docker image inspect` adds ~100 ms to every bundle invocation.
  Acceptable: bundle is a human-triggered command, not hot-path.
- Staleness walk on large repos (e.g. a monorepo with 100k files under
  `node_modules`) depends on `.gitignore` / `.dockerignore` being
  correct. The hard-ignore list covers `node_modules`, so the common
  case is safe; edge cases are documented.
- One new port (`ImageInspector`) to maintain. Mitigated by modelling
  it on the existing `ImageExporter` shape.
- `--build` as an opt-in flag (rather than default-on) means a user who
  skips the flag still gets a STALE warning. This is deliberate — auto-
  building inside bundle would violate the "bundle is purely local,
  never mutates anything outside --output" contract from ADR-085. The
  build call is scoped to the same target platform the user requested
  for bundle, so it matches their intent.

### Future work (out of scope)

- Cross-arch emulation (buildx + qemu) without `--build`.
- Registry push from bundle.
- Auto-migration of legacy `.env` files (warn-only per spec).
- Multi-site bundle — tracked separately; this ADR applies only to
  single-site.

## References

- PM spec — combined across issues #1084, #1085, #1091 (2026-04-20)
- ADR-082 — strict config merge / validate hook point
- ADR-085 — `vibew bundle` compose-only contract
- ADR-086 — sunset `vibew deploy`
- ADR-081 — arch mismatch detection (deploy-side; this ADR mirrors the
  semantics on the bundle side)
- `feedback_artifact_tests` — tests must verify generated files, not
  just function calls
