# ADR-100: Image identity via `org.vibewarden.project-root-hash` label

**Date**: 2026-04-28
**Issue**: #1219 (paired with #1220)
**Status**: Accepted

## Context

The v0.18.2 retrospective (qr-code-blackhole, Node/Express) surfaced the most
dangerous failure mode of the release: `vibew dev` silently reused an existing
`<project>-app:latest` image from a prior unrelated project that happened to
share the directory name. The stack came up "healthy", `/_vibewarden/health`
returned 200, but the served HTML was a Go binary from a different project
entirely. The agent had to `docker exec` into the container to discover the
divergence.

### Mechanism

- The build image tag is derived from `cfg.ComposeProjectName()`, which falls
  back to the directory name (`internal/config/config.go:198`).
- Two unrelated projects with the same directory name → same image tag.
- Compose's default `image_pull_policy: if-not-present` → existing image wins.
- No warning anywhere; no comparison of the resolved image's origin to the
  current project root.

### Existing precedents we reuse

- **ADR-089** introduced `ports.ImageInspector` and the `ImageInfo` value
  object backed by `docker image inspect --format '{{json .}}'`. It exposes
  `OS`, `Architecture`, `Created`, `Digest`, `SizeBytes` — but does **not**
  read `Config.Labels`. We extend it.
- **ADR-093** unified image-name resolution between `vibew bundle` and
  `vibew build` so they always agree byte-for-byte. The label written at
  build time and inspected at dev time MUST be derived from the same
  source as the tag (i.e. the project root used to compute
  `ComposeProjectName()`).
- The `BuildAdapter` (`internal/adapters/ops/build.go`) shells out to
  `docker build` and accepts only `tag`, `contextDir`, `noCache`, `platform`
  today. We extend its port and adapter to accept structured labels.
- `ports.ErrImageNotFound` and `ports.ErrDockerUnavailable` already exist
  (`internal/ports/image_inspector.go:12`). Mismatch becomes a third
  sentinel, locally scoped.

### Constraint from CLAUDE.md "artifact policy"

No example-shaped middle-ground artifacts. Labels are real (validated, kept
current, owned by vibew) — they go on every image vibew builds.

## Decision

`vibew build` stamps two Docker labels onto the produced image. `vibew dev`
inspects them before compose-up and blocks with exit 1 on mismatch or
absence. The recovery path is `vibew dev --rebuild` (#1220).

### Label namespace and field format

| Label | Value | Purpose |
|---|---|---|
| `org.vibewarden.project-root-hash` | `sha256:<64-hex>` of the realpath of the project root, lowercased | **Authoritative** — used for equality comparison |
| `org.vibewarden.project-root` | the realpath of the project root, verbatim (e.g. `/Users/tibtof/qr-code-blackhole`) | Informational — shown in error messages for human debugging |

Two labels because:
- The hash is path-shape-stable (no leakage concerns even if an image is
  ever pushed; today vibew images are never pushed but this future-proofs).
- The literal path makes the error message immediately actionable without a
  reverse lookup table. The retro made clear that "Built from: /Users/foo/old-project"
  is the line that converts confusion into a one-step fix.
- Both labels are written at build time. Only the hash is compared. The path
  is shown in messages but never matched.

The OCI-recommended `org.vibewarden.*` reverse-DNS prefix matches our
domain `vibewarden.dev`. We do **not** use `org.opencontainers.image.*` —
those labels have well-defined OCI semantics (source, revision, vendor) and
are not the right home for a project-root identity.

### Hash computation (deterministic)

```
realpath := filepath.EvalSymlinks(cfg.ProjectRoot)
// fallback: filepath.Abs(cfg.ProjectRoot) when EvalSymlinks fails
//   (e.g. the directory existed at build time but a symlink no longer
//   resolves; surface the abs path with a debug log, do not error)
hash := sha256.Sum256([]byte(realpath))
labelValue := "sha256:" + hex.EncodeToString(hash[:])
```

Path normalisation:
- `filepath.EvalSymlinks` resolves NFS / Time Machine / Docker Desktop's
  bind-mount symlinks to the canonical path so that the same project on
  the same filesystem hashes identically across `cd /tmp/symlink-to-root`
  vs `cd /Users/foo/realdir`.
- Lowercasing is **not** applied — Linux paths are case-sensitive. macOS's
  default APFS is case-insensitive but `EvalSymlinks` returns the on-disk
  case; that is the canonical form.
- Trailing slash is not added (filepath.EvalSymlinks normalises this).

### Domain model changes

A small value object plus one new error sentinel. No new aggregate, no new
domain event.

```go
// internal/app/ops/image_identity.go

// ImageIdentity is the project-identity stamped on a vibew-built image via
// Docker labels. Both fields are populated from labels read at inspect time.
// A zero ImageIdentity (Hash == "" and Path == "") means the image carries
// no vibew project-root labels — i.e. it was built before this feature
// shipped, or was built by something other than `vibew build`.
type ImageIdentity struct {
    // Hash is the value of the `org.vibewarden.project-root-hash` label,
    // formatted as "sha256:<64-hex>". Empty when the label is absent.
    Hash string
    // Path is the value of the `org.vibewarden.project-root` label, the
    // human-readable absolute project root used at build time. Empty when
    // the label is absent. Informational only — NEVER used for comparison.
    Path string
}

// IsLabelled reports whether the image carries a vibew project-root hash
// label. Used by the dev pre-flight to distinguish "unlabelled (legacy)"
// from "mismatched (foreign project)".
func (i ImageIdentity) IsLabelled() bool { return i.Hash != "" }
```

```go
// internal/app/ops/image_identity.go

// ProjectRootHash returns the canonical sha256 hash for an absolute project
// root. It calls filepath.EvalSymlinks to normalise; on EvalSymlinks failure
// it falls back to filepath.Abs and the caller logs a debug warning.
func ProjectRootHash(projectRoot string) (hashLabel, pathLabel string, err error)
```

`ProjectRootHash` is a pure function (modulo the filesystem read for
`EvalSymlinks`) and lives in the `ops` app package so that both `BuildService`
and `DevService` import it from one place.

### Ports (interfaces)

#### Extension 1: `ports.DockerBuilder` accepts labels

The current `Build` signature (`internal/ports/ops.go:151`) takes positional
args and is already at five parameters. We replace it with a struct-based
signature to keep adding fields painless and to match `ComposeUpOptions`'s
precedent.

```go
// BuildOptions holds optional arguments for DockerBuilder.Build. Defined as
// a struct so future flags can be added without breaking callers.
type BuildOptions struct {
    NoCache  bool
    Platform string
    // Labels is a map of Docker label keys to values, applied to the built
    // image via repeated `--label key=value` arguments. Order is not
    // preserved; callers must not assume label ordering matters.
    Labels map[string]string
}

type DockerBuilder interface {
    Build(ctx context.Context, tag string, contextDir string, opts BuildOptions) error
}
```

This is a breaking change to the port. Migration is local: `BuildAdapter`,
`BuildService`, and any tests that fake `DockerBuilder`. No public API
exposure.

#### Extension 2: `ports.ImageInfo` exposes labels

Today `ports.ImageInfo` does not expose `Config.Labels`. We add one field
and update the adapter's JSON parse.

```go
// internal/ports/image_inspector.go

type ImageInfo struct {
    Tag          string
    Digest       string
    OS           string
    Architecture string
    Created      time.Time
    SizeBytes    int64
    // Labels is the map of OCI/Docker labels stamped on the image. Keys
    // follow reverse-DNS convention (e.g. "org.vibewarden.project-root-hash").
    // Empty map when the image carries no labels — never nil after a
    // successful Inspect.
    Labels map[string]string
}
```

The adapter's `dockerInspectOutput` struct gains a `Config struct{ Labels map[string]string }`
field. The unmarshalled `Labels` map is copied into `ImageInfo.Labels`,
defaulting to an empty (non-nil) map when absent.

#### New sentinel: project-root mismatch

Lives in `internal/app/ops/image_identity.go` (NOT in ports — it is dev's
private failure mode, not a generic image-inspection error).

```go
// ErrProjectRootMismatch is returned by VerifyAppImageIdentity when the
// resolved app image's project-root-hash label is missing or does not match
// the current project root. The error wraps an ImageIdentity value so the
// CLI layer can format the actionable message.
var ErrProjectRootMismatch = errors.New("app image was built from a different project")
```

### Adapters

Two adapter changes; no new adapters introduced.

**`internal/adapters/ops/build.go` — `BuildAdapter.Build`** is updated to the
new signature. It iterates `opts.Labels` (sorted by key for deterministic
test output) and appends `--label key=value` pairs to the `docker build`
arg list:

```go
keys := make([]string, 0, len(opts.Labels))
for k := range opts.Labels {
    keys = append(keys, k)
}
sort.Strings(keys)
for _, k := range keys {
    args = append(args, "--label", k+"="+opts.Labels[k])
}
```

Sorting matters only for test determinism (table-driven tests assert the
exact arg slice); `docker build` does not care.

**`internal/adapters/ops/image_inspect.go` — `ImageInspectAdapter.Inspect`**
gains label parsing. The format string changes from
`{{json .}}` to `{{json .}}` (no change — the full JSON already includes
`Config.Labels`). Only the Go struct decoding changes:

```go
type dockerInspectOutput struct {
    ID           string   `json:"Id"`
    Architecture string   `json:"Architecture"`
    Os           string   `json:"Os"`
    Created      string   `json:"Created"`
    Size         int64    `json:"Size"`
    RepoDigests  []string `json:"RepoDigests"`
    Config       struct {
        Labels map[string]string `json:"Labels"`
    } `json:"Config"`
}
```

The returned `ImageInfo.Labels` is `out.Config.Labels` if non-nil, else an
empty map. Never nil — callers can iterate without a guard.

### Application service

Three small additions in `internal/app/ops/`:

**1. `internal/app/ops/image_identity.go` (NEW)** — pure helpers and the
verifier function:

```go
package ops

const (
    // LabelProjectRootHash is the authoritative identity label stamped on
    // every vibew-built image. Value format: "sha256:<64-hex>".
    LabelProjectRootHash = "org.vibewarden.project-root-hash"
    // LabelProjectRoot is the informational human-readable project root
    // path stamped alongside the hash. Used only in error messages.
    LabelProjectRoot = "org.vibewarden.project-root"
)

func ProjectRootHash(projectRoot string) (hashLabel, pathLabel string, err error)

// VerifyAppImageIdentity inspects the named image and returns
// ErrProjectRootMismatch when the image's project-root-hash label is
// missing or does not match expectedHash. The image must already exist
// in the local daemon (callers must have already passed the existence
// check). On success returns the parsed ImageIdentity for telemetry.
func VerifyAppImageIdentity(
    ctx context.Context,
    inspector ports.ImageInspector,
    image string,
    expectedHash string,
) (ImageIdentity, error)
```

`VerifyAppImageIdentity` returns the typed error so the CLI can render the
actionable message. The function does not format messages itself — that is
CLI concern.

**2. `internal/app/ops/build.go` — `BuildService.Run`** updates:

After `resolveImageTag(...)` and before `s.builder.Build(...)`, compute
the labels:

```go
hashLabel, pathLabel, err := ProjectRootHash(projectRoot)
if err != nil {
    // EvalSymlinks failed and Abs also failed. Surface but do not block —
    // the build can still produce a working image. Log at warn.
    slog.Warn("project-root hash unavailable; image will not carry identity labels", "error", err)
} else {
    labels = map[string]string{
        LabelProjectRootHash: hashLabel,
        LabelProjectRoot:     pathLabel,
    }
}
```

`projectRoot` is derived from:
1. `cfg.ProjectRoot` when `cfg != nil` (config loader sets this — see
   `internal/config/load_raw.go:67`).
2. else `filepath.Abs(opts.WorkDir)` — same logic `resolveImageTag`
   already performs.

The build path change is additive: existing callers (`cmd/build.go` and
`runBundle`) keep their flow — they just pass the new `BuildOptions{Labels: ...}`
field. No behavioural divergence between standalone build and bundle-build.

**3. `internal/app/ops/dev.go` — `DevService.checkAppImage`** is extended.

The current pre-flight only checks "does the image exist". We add a step:
when the image exists, also verify its project-root-hash label.

```go
// Pre-flight ordering is important — see #1222 for the doctor analogue.
// Order:
//   1. checkAppImage existence (existing) — fail early if image absent.
//   2. verifyAppImageIdentity (NEW)        — fail early on cross-project shadow.
//   3. checkContainerFreshness (existing)  — only after identity is confirmed.

func (s *DevService) verifyAppImageIdentity(
    ctx context.Context,
    cfg *config.Config,
    opts DevOptions,
    out io.Writer,
) error {
    if s.imageInspector == nil {
        return nil // not wired (production wiring always wires it; tests opt in)
    }
    if cfg.App.Image == "" || cfg.App.Build != "" {
        return nil // user-managed image OR compose-builds — see "User-set image"
    }
    if isUserSetImage(cfg) {
        // user-set image: app.image points at a tag the user manages directly
        // (e.g. ghcr.io/foo/bar:latest). Skip the identity check with an INFO
        // line — labels likely belong to someone else.
        fmt.Fprintf(out, "Custom image %q — skipping project-root identity check.\n", cfg.App.Image)
        return nil
    }

    expectedHash, _, err := ProjectRootHash(cfg.ProjectRoot)
    if err != nil {
        // realpath failed — log debug, skip the check rather than block.
        slog.Debug("project-root hash unavailable; skipping identity check", "error", err)
        return nil
    }

    identity, verifyErr := VerifyAppImageIdentity(ctx, s.imageInspector, cfg.App.Image, expectedHash)
    if errors.Is(verifyErr, ErrProjectRootMismatch) {
        return formatProjectRootMismatch(cfg.App.Image, cfg.ProjectRoot, identity)
    }
    if verifyErr != nil {
        // Inspector failed for some other reason (daemon down, etc.).
        // Surface but do not invent a mismatch — this matches the existing
        // "graceful degradation" pattern of checkContainerFreshness.
        slog.Warn("could not verify app image identity", "error", verifyErr)
    }
    return nil
}
```

The `DevService` gains an optional `imageInspector ports.ImageInspector`
field and a `WithImageInspector(...)` chainable setter (mirrors
`WithImageChecker`). The CLI wires it from
`opsadapter.NewImageInspectAdapter()`.

### "User-set image" detection

The escape hatch: a user can put `app.image: ghcr.io/foo/bar:latest` in
`vibewarden.yaml`. That image is not vibew-built and the labels likely
don't belong. The identity check skips with an INFO line.

```go
// isUserSetImage reports whether cfg.App.Image was authored by the user
// rather than derived from cfg.ComposeProjectName(). True when the image
// reference does NOT match the canonical "<project>-app:latest" pattern
// derived from cfg.ComposeProjectName().
func isUserSetImage(cfg *config.Config) bool {
    canonical := cfg.ComposeProjectName() + "-app:latest"
    return cfg.App.Image != "" && cfg.App.Image != canonical
}
```

This is a heuristic. The tradeoff: if a user explicitly sets
`app.image: <project>-app:latest` in YAML (matching exactly), we still run
the check. That is the safer default — the check fires when the tag is
the one vibew would have built.

### File layout

All paths exact. New files explicitly marked.

```
internal/
  ports/
    ops.go                                 # MODIFY: BuildOptions struct; DockerBuilder.Build signature change
    image_inspector.go                     # MODIFY: +Labels field on ImageInfo
  adapters/
    ops/
      build.go                             # MODIFY: accept BuildOptions, emit --label args (sorted)
      build_test.go                        # MODIFY: assert arg slice contains label flags in deterministic order
      image_inspect.go                     # MODIFY: parse Config.Labels into ImageInfo.Labels
      image_inspect_test.go                # MODIFY: golden tests cover labels round-trip; empty-labels case
  app/
    ops/
      image_identity.go                    # NEW: constants, ImageIdentity, ProjectRootHash, VerifyAppImageIdentity, ErrProjectRootMismatch
      image_identity_test.go               # NEW: pure unit tests (hash determinism, symlink resolution, mismatch detection)
      build.go                             # MODIFY: compute labels; pass via BuildOptions
      build_test.go                        # MODIFY: assert labels passed to fake builder
      dev.go                               # MODIFY: +verifyAppImageIdentity step; +imageInspector field; +WithImageInspector
      dev_test.go                          # MODIFY: +cases for matched/mismatched/unlabelled/skip-when-user-set
      dev_format.go                        # NEW (optional): formatProjectRootMismatch — pinned message wording
      dev_format_test.go                   # NEW: golden test for the exact mismatch message
  cli/
    cmd/
      dev.go                               # MODIFY: wire NewImageInspectAdapter() onto DevService
      build.go                             # NO CHANGE (BuildService computes labels internally)
```

Docs / templates touched in the same PR (not files but content):

- `CHANGELOG.md` — Breaking note: first post-upgrade `vibew dev` blocks for
  every project until `vibew dev --rebuild` runs. Documented as intentional.
- `docs/cli.md` — `vibew dev` blocking-behaviour section; cross-link to
  `vibew dev --rebuild` (#1220).
- `AGENTS-VIBEWARDEN.md` template — one paragraph under "vibew dev" telling
  the agent that an unlabelled image will block and the recovery is
  `vibew dev --rebuild`.

### Sequence (request/response flow)

#### Build path (`vibew build`)

1. `cmd/build.go` loads config, constructs `BuildService`, calls `Run`.
2. `BuildService.Run` resolves `tag` (existing).
3. `BuildService.Run` resolves `projectRoot`:
   - prefer `cfg.ProjectRoot` when non-empty
   - else `filepath.Abs(opts.WorkDir)`
4. `ProjectRootHash(projectRoot)` returns `hashLabel`, `pathLabel`. On
   failure: log warn, omit labels (build still succeeds; future `vibew dev`
   will block on this image until rebuilt — that is the correct behaviour).
5. `BuildService.Run` calls `builder.Build(ctx, tag, workDir, BuildOptions{
   NoCache, Platform, Labels})`.
6. `BuildAdapter.Build` shells out:
   `docker build --label org.vibewarden.project-root=<path> --label
   org.vibewarden.project-root-hash=sha256:<hex> [-t tag] [--platform p]
   [--no-cache] <contextDir>`.
7. Existing post-build shell-prober step is unchanged.

#### Dev path (`vibew dev`)

1. `cmd/dev.go` loads cfg, wires `compose`, `generator`, `imageChecker`
   (existing) AND `imageInspector` (NEW).
2. `DevService.Run` calls `resolveComposeFile` (existing).
3. `DevService.Run` calls `checkAppImage` (existing, ImageExists).
4. `DevService.Run` calls **`verifyAppImageIdentity`** (NEW). Decision tree:
   - `imageInspector` nil → skip (non-production / tests).
   - `cfg.App.Image == "" || cfg.App.Build != ""` → skip.
   - `isUserSetImage(cfg)` → INFO log, skip.
   - `ProjectRootHash(cfg.ProjectRoot)` failed → debug log, skip.
   - call `VerifyAppImageIdentity(ctx, inspector, image, expectedHash)`:
     - `ErrProjectRootMismatch` → return formatted error → CLI prints
       message → exit 1.
     - any other error → warn log, continue (graceful degradation).
     - nil → continue.
5. `DevService.Run` calls `checkContainerFreshness` (existing) — only
   reached when identity matched.
6. Compose-up + sidecar verification (existing).

### Pinned error message wording

The error must be copy-pasteable. Test pin via golden assertion. Two
variants — "labelled but mismatched" and "unlabelled".

**Variant 1 — image carries a different project's labels:**

```
Error: app image qr-code-blackhole-app:latest was built from a different project.
  Built from: /Users/foo/old-project
  Current:    /Users/tibtof/qr-code-blackhole

Rebuild with: vibew dev --rebuild
```

**Variant 2 — image carries no project-root labels (legacy / foreign builder):**

```
Error: app image qr-code-blackhole-app:latest is missing the vibew project-root label.
  This image was built before VibeWarden v0.19.0 OR by something other than vibew build.
  Current project: /Users/tibtof/qr-code-blackhole

Rebuild with: vibew dev --rebuild
```

Both variants:
- Start with `Error:` (matches `buildMissingImageError` in the same file).
- Two-space indent for the data block.
- Trailing blank line then the `Rebuild with:` line.
- Reference `vibew dev --rebuild` even before #1220 lands — the dev agent
  for #1219 must coordinate with the dev agent for #1220 so the recovery
  flag exists when this message ships. If #1220 is not merged in time,
  the message can substitute `vibew down && docker rmi <image> && vibew
  build && vibew dev` as a fallback.

`formatProjectRootMismatch` lives in `internal/app/ops/dev_format.go`. The
function is pure (no I/O) and table-driven tests assert the exact byte
output for both variants.

### Error cases

| Case | Detection | Outcome | Exit |
|---|---|---|---|
| image absent | `imageChecker.ImageExists == false` (existing) | `buildMissingImageError` (existing) | 1 |
| image present, hash matches | `Labels[LabelProjectRootHash] == expectedHash` | proceed | 0 |
| image present, hash differs | `Labels[LabelProjectRootHash] != expectedHash` | `formatProjectRootMismatch` Variant 1 | 1 |
| image present, no hash label | `Labels[LabelProjectRootHash] == ""` | `formatProjectRootMismatch` Variant 2 | 1 |
| user-set `app.image` (non-canonical tag) | `isUserSetImage(cfg) == true` | INFO line; skip check | 0 |
| `cfg.App.Build != ""` (compose builds) | guard at top of `verifyAppImageIdentity` | skip check | 0 |
| inspector returns `ErrImageNotFound` | bug — existence already checked | warn log; skip | 0 |
| inspector returns `ErrDockerUnavailable` | docker daemon down | warn log; skip | 0 |
| inspector returns wrapped error | unknown failure | warn log; skip | 0 |
| EvalSymlinks fails on `cfg.ProjectRoot` | filesystem oddity | fall back to `Abs`; debug log | 0 |
| `ProjectRootHash` fails entirely | both EvalSymlinks and Abs fail | debug log; skip check | 0 |

The "skip on inspector failure" branches preserve existing graceful-
degradation semantics. The intent is: never invent a false mismatch. The
identity check is a safety net layered on top of compose's normal flow,
not a gatekeeper that must run.

### Test strategy

#### Unit tests (next to code, `_test.go`)

- **`internal/app/ops/image_identity_test.go`** — pure, no Docker:
  - `ProjectRootHash` produces stable `sha256:<64-hex>` output (table).
  - Symlink resolution: a tmpdir + `os.Symlink` → identical hashes.
  - Mismatch detection: synth `ports.ImageInfo` with `Labels` map →
    `VerifyAppImageIdentity` returns `ErrProjectRootMismatch`.
  - Match detection: same hash → returns nil.
  - Missing label: empty / nil `Labels` → `ErrProjectRootMismatch`.
- **`internal/app/ops/dev_format_test.go`** — golden:
  - Variant 1 byte-exact match.
  - Variant 2 byte-exact match.
  - Tag with `:` and `/` in it (e.g. `ghcr.io/foo/bar:v1`) renders
    correctly.
- **`internal/app/ops/dev_test.go`** — extends:
  - happy path (labels match) → no block, proceeds to compose-up.
  - mismatch → `Run` returns wrapped `ErrProjectRootMismatch`; compose
    is never called (`fakeCompose.UpCalls == 0`).
  - unlabelled → same as mismatch.
  - user-set image (`ghcr.io/foo/bar:latest`) → INFO line emitted; check
    skipped; compose called.
  - `cfg.App.Build` set → check skipped.
  - inspector returns daemon-down → warn log; compose called (graceful
    degradation).
- **`internal/app/ops/build_test.go`** — extends:
  - assert `fakeBuilder.LastBuildOpts.Labels` contains both label keys
    with the expected values.
  - `cfg.ProjectRoot` populated → use it.
  - `cfg == nil` → fall back to `WorkDir`.
- **`internal/adapters/ops/build_test.go`** — assert the constructed
  arg slice contains `--label org.vibewarden.project-root=<path>` and
  `--label org.vibewarden.project-root-hash=sha256:<hex>` in
  alphabetical key order.
- **`internal/adapters/ops/image_inspect_test.go`** — extends with two
  cases:
  - JSON includes `Config.Labels` → `ImageInfo.Labels` round-trips.
  - JSON has no `Config` field or `Config.Labels` is null →
    `ImageInfo.Labels` is empty (non-nil) map.

#### Integration tests

- `test/architecture/...` (existing pattern from ADR-087): add an
  invariant test that `BuildService.Run` always passes both label keys
  to the fake builder (catches a future regression where someone
  bypasses the helper).
- A `//go:build integration` test in `internal/adapters/ops/` that
  builds a tiny `FROM scratch` image with the real adapter and inspects
  the labels with `docker image inspect`. Skipped when docker is
  unavailable. Optional — the unit tests cover the contract.

#### Coverage target

Maintain ≥80% on `internal/app/ops/` per CLAUDE.md. The new files are
small and pure-Go; meeting the bar is straightforward.

### New dependencies

**None.** SHA-256 is `crypto/sha256` (stdlib). Hex encoding is
`encoding/hex` (stdlib). All other imports are already in `go.mod`.

### Bundle-time inspection — deferred

The original PM thought-process asked whether `vibew bundle` should also
verify the label. Decision: **defer**.

Rationale:
- ADR-089 already inspects the image at bundle time for freshness and arch.
  Adding a third check (project-root) into the same flow is the natural
  next step BUT it expands the scope of #1219 beyond the dev path that
  caused the retro.
- A separate follow-up issue (filed in the status comment) tracks the
  bundle-time check. The implementation will be ~20 lines reusing
  `VerifyAppImageIdentity` from this ADR — no new abstractions needed.
- This keeps #1219's PR small and reviewable.

### Backwards-compat / migration

Every existing local image is unlabelled. After upgrade, the first
`vibew dev` per project will block with Variant 2 of the message. The user
runs `vibew dev --rebuild` (#1220) and the next run succeeds. CHANGELOG
documents this under **Breaking** with the one-line mitigation.

The check has no opt-out flag in v1. We considered `--no-image-identity-check`
and rejected it: an opt-out is a footgun precisely because the failure
mode is "silent wrong result". A user determined to bypass can edit
`vibewarden.yaml` to set `app.build: "."` (compose-build path skips the
check by design).

## Consequences

### Positive

- Closes the silent-wrong-result bug class for `vibew dev`. The most
  dangerous failure mode of v0.18.2 is detected before the stack starts.
- Reuses ADR-089's inspector port — one new method (label parsing) on an
  existing adapter, not a new adapter.
- Pure-Go identity computation — no new dependency, no new domain
  concepts, label format is a future-stable public contract.
- The label namespace `org.vibewarden.*` lays the foundation for future
  identity facets (e.g. `org.vibewarden.config-digest`,
  `org.vibewarden.cli-version`) without a redesign.

### Negative / trade-offs

- Breaking change to `ports.DockerBuilder.Build` signature. Local-only
  blast radius — fixed in the same PR.
- First `vibew dev` after upgrade blocks every project. Mitigated by the
  CHANGELOG note and the `vibew dev --rebuild` recovery flag (#1220).
  This is the same trade-off ADR-089 took with the bundle migration warn.
- The label is a single source of truth — if it is wrong, dev is wrong.
  Mitigated by computing it in one place (`ProjectRootHash`) used by
  both stamping and verification.
- macOS Time Machine and Docker Desktop occasionally surface paths
  through unusual symlink chains. `EvalSymlinks` should resolve them; if
  it does not, the Abs fallback still produces a deterministic hash for
  any single workstation. Cross-machine builds (where the same project
  lives at different paths) are out of scope — vibew is local-only by
  design (CLAUDE.md "Sidecar locality" lock).

### Future work (out of scope)

- Bundle-time identity inspection (separate issue, filed in status
  comment).
- Additional vibew-owned labels: `org.vibewarden.config-digest`,
  `org.vibewarden.binary-version`. Use this label's namespace as the
  reference precedent.
- Doctor-time identity check (`vibew doctor` reports the identity of
  every running app container).

## References

- ADR-089 — Bundle image health: tag scoping, freshness, arch.
- ADR-093 — bundle / build image-name resolution unified.
- ADR-087 — Test placement: contract tests with adapter, architectural
  invariants in `test/architecture/`.
- Issue #1219 — image-identity (this ADR).
- Issue #1220 — `vibew dev --rebuild` (the recovery flag).
- v0.18.2 retro — `~/notes/vibewarden/retro-0.18.2.md` (2026-04-28).
