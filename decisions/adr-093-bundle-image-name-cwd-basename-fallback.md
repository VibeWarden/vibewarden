# ADR-093: bundle image-name resolution — cwd-basename fallback unified across `vibew bundle` and `--build`

**Date**: 2026-04-23
**Issue**: #1141
**Status**: Accepted

## Context

`vibew bundle` and `vibew bundle --build` walk different project-name
derivation chains when neither `name:` nor `app.image:` is set in
`vibewarden.yaml`:

- **`runBundle`** computes the image tag default at `bundle.go:192` as
  `cfg.ComposeProjectName() + "-app:latest"`. `ComposeProjectName()` chains
  `Name → App.Image → ProjectRoot → "vibewarden"`. `LoadRaw` (the loader used
  by `vibew bundle`) never populates `ProjectRoot`, so step 3 silently fails
  and the chain returns the literal `"vibewarden"`. Result: `imageTag =
  "vibewarden-app:latest"`.

- **`BuildService.resolveImageTag`** (in `internal/app/ops/build.go`) has a
  defensive guard: when `ComposeProjectName()` returns `"vibewarden"` it
  falls through to `filepath.Base(workDir)`. Result: `--build` builds
  `qr-dali-app:latest`.

Two derivation chains produce two different names from the same input,
breaking ADR-089's "single source of truth" invariant.

PM (#1141) selected **Option A — fix the derivation chain** so both code
paths use cwd-basename fallback. Option B (mandatory `name:`) is rejected.

PM placed **out of scope**: setting `cfg.ProjectRoot` inside `LoadRaw`. This
constrains the architect to fix the bug at the **leaf** (pipe a single
derived name into both call sites) rather than at the source (mutate the
load path).

`deriveProjectName` already resolves the correct cwd-basename fallback via
`bundleapp.ProjectNameFromConfig(absConfig)` and is run through
`sanitiseProjectName`. Its result is currently only used for
`BundleOptions.ProjectName`, not for `imageTag` or `BuildService`. The fix
is to make `deriveProjectName` the single source of truth for the project
name within `runBundle` and feed it to every downstream consumer.

## Decision

`deriveProjectName(cfg, absConfig)` becomes the **single project-name
authority** inside `runBundle`. Its result is used to:

1. compute the default `imageTag` (`"<projectName>-app:latest"`) when
   `--image-tag` is not supplied; and
2. drive the `--build` step so `BuildService` resolves the same tag.

`BuildService` gains an optional explicit `ImageTag` (or `ProjectName`)
input on `BuildOptions` so the caller can pin the tag without re-running
the chain inside `resolveImageTag`. When `ImageTag` is set on
`BuildOptions`, `resolveImageTag` returns it verbatim. Otherwise the
existing chain runs (preserving behavior for `vibew build` invoked
standalone, where `runBundle` is not on the call stack).

### Domain model changes

None. This is a use-case-layer fix.

### Ports (interfaces)

No new ports. `ports.DockerBuilder` and `ports.DockerShellProber` are
unchanged.

### Adapters

No adapter changes.

### Application service

`internal/app/ops/build.go::BuildService.Run` currently calls
`resolveImageTag(cfg, workDir)`. Change:

- Add `ImageTag string` field to `BuildOptions` (godoc: "Pre-resolved image
  tag. When non-empty, BuildService skips its own derivation and uses this
  tag verbatim. Callers that have already resolved the project name (e.g.
  `vibew bundle --build`) pass it here so the build step matches the
  bundle's image lookup. When empty, the existing config/workdir chain
  runs.").
- In `Run`, prefer `opts.ImageTag` when non-empty; otherwise call
  `resolveImageTag(cfg, workDir)` as today.

`runBundle` (`internal/cli/cmd/bundle.go`) changes:

- Compute `projectName := deriveProjectName(cfg, absConfig)` (already done
  at line 190 — keep it).
- Replace lines 191–193 with:
  ```go
  if imageTag == "" {
      imageTag = projectName + "-app:latest"
  }
  ```
  This drops the `cfg.ComposeProjectName()` call entirely from the bundle
  path. `deriveProjectName` already chains `Name → App.Image →
  ProjectNameFromConfig` and sanitises every step.
- In the `if build {}` block (lines 200–211) pass `ImageTag: imageTag` on
  the `BuildOptions` struct so the build step uses the same tag.

### File layout

No new files. Edits only:

- `/Users/tibtof/workspace/vibewarden/internal/app/ops/build.go`
  - Add `ImageTag string` to `BuildOptions`.
  - Branch in `Run` to prefer `opts.ImageTag`.
  - Update godoc on `resolveImageTag` to note that callers with a
    pre-resolved tag should set `BuildOptions.ImageTag`.
- `/Users/tibtof/workspace/vibewarden/internal/cli/cmd/bundle.go`
  - Use `projectName` (already computed) to build the default `imageTag`.
  - Set `BuildOptions.ImageTag = imageTag` in the `--build` branch.
- `/Users/tibtof/workspace/vibewarden/internal/app/ops/build_test.go` (or
  equivalent existing test file) — extend tests for the new
  `ImageTag` short-circuit.
- `/Users/tibtof/workspace/vibewarden/internal/cli/cmd/bundle_test.go` (or
  the existing `internal/app/bundle/` tests) — table-driven case for
  `imageTag` resolution covering `name:` set, `app.image:` set, and
  cwd-basename fallback.
- `/Users/tibtof/workspace/vibewarden/internal/config/templates/agents-vibewarden.md.tmpl`
- `/Users/tibtof/workspace/vibewarden/docs/examples/AGENTS-VIBEWARDEN.md`
  - Correct fallback description: `name:` field → `app.image:` strip → cwd
    directory basename. State `name:` is optional in the common case.
- `/Users/tibtof/workspace/vibewarden/CHANGELOG.md`
  - `[Unreleased] ### Fixed` entry per PM acceptance criterion.
- New regression test (described below).

### Sequence

1. User runs `vibew bundle` in `/path/to/qr-dali` with no `name:` set.
2. `runBundle` calls `config.LoadRaw(absConfig)` → `cfg` is returned with
   `cfg.Name == ""`, `cfg.App.Image == ""`, `cfg.ProjectRoot == ""`.
3. `runBundle` calls `projectName := deriveProjectName(cfg, absConfig)`.
   - Step 1 (cfg.Name) → empty, skip.
   - Step 2 (cfg.App.Image) → empty, skip.
   - Step 3 → `ProjectNameFromConfig(absConfig)` returns `qr-dali` (sanitised).
4. `runBundle` defaults `imageTag = projectName + "-app:latest"` = `qr-dali-app:latest`.
5. `runBundle` passes `BundleOptions.ProjectName = projectName`, the bundle
   service writes `IMAGE=qr-dali-app:latest` into `sample.env` / `.env`.

For `vibew bundle --build`:

1. Same steps 1–4.
2. `runBundle` constructs `BuildOptions{Platform, ConfigPath, ImageTag:
   imageTag}` and calls `BuildService.Run(ctx, cfg, opts, out)`.
3. `BuildService.Run` sees `opts.ImageTag != ""` and uses `qr-dali-app:latest`
   verbatim, skipping `resolveImageTag`.
4. `docker build` runs with tag `qr-dali-app:latest`, matching the bundle
   lookup byte-for-byte.

For standalone `vibew build` (unrelated to this bug, but invariant
preserved):

1. `vibew build` does NOT pre-resolve a tag; it calls `BuildService.Run`
   with `opts.ImageTag == ""`.
2. `resolveImageTag` runs its existing chain (cfg → workDir base) — no
   behavior change.

### Error cases

- `deriveProjectName` returns the empty string only when `absConfig`
  itself is unusable (`ProjectNameFromConfig` returns empty AND `cfg.Name`
  / `cfg.App.Image` are empty). `runBundle` should treat empty
  `projectName` as a hard error: `"cannot derive project name from
  configuration path %q"` and abort before any file is written. Without
  this guard the image tag would default to `"-app:latest"` (a Docker
  syntax error). Today the guard is implicit through Compose's last-resort
  `"vibewarden"` literal — removing the literal removes the implicit
  guard, so the explicit one must be added.
- All other error paths are unchanged. `BuildService.Run` errors continue
  to wrap as `"building image: %w"`.

### Test strategy

**Unit tests (table-driven, no mocks needed):**

1. `internal/cli/cmd/bundle_test.go` (or new file) —
   `TestRunBundle_ImageTagDerivation` table:
   - `name: myapp` set → `myapp-app:latest`.
   - `app.image: ghcr.io/org/myapp:latest` set, no `name:` → `myapp-app:latest`.
   - Neither set, cwd basename `qr-dali` → `qr-dali-app:latest`.
   - Neither set, cwd basename contains adversarial chars (`my; rm` ) →
     sanitised tag (covered by existing
     `TestDeriveProjectName_SanitisesAdversarialInput`; ensure unchanged).
2. `internal/app/ops/build_test.go` —
   `TestBuildService_Run_UsesPreResolvedImageTag`: when
   `BuildOptions.ImageTag` is set, the recorded tag passed to the
   `DockerBuilder` mock equals it byte-for-byte regardless of `cfg`
   contents. Reuses existing fake builder.
3. `internal/config/config_test.go` —
   `TestComposeProjectName_FallsBackToVibewardenWhenProjectRootEmpty`:
   document current behavior (returns `"vibewarden"` when all sources
   empty). This is a guard test, not a behavior change. It pins the fact
   that `ComposeProjectName()` is NOT the right authority for `runBundle`
   without a populated `ProjectRoot`.

**Integration test (the regression test PM asked for):**

`internal/cli/cmd/bundle_integration_test.go` (or
`test/integration/bundle_image_name_test.go`):

- `TestVibewBundle_DerivesImageTagFromCwdBasename`:
  1. `t.TempDir()` → rename or create subdir `qr-dali`.
  2. Write a minimal `vibewarden.yaml` with no `name:`, no `app.image:`.
  3. Invoke `runBundle` (or the cobra command) with `--skip-image`.
  4. Assert the generated `sample.env` (and `.env`) contains
     `IMAGE=qr-dali-app:latest`.
- `TestVibewBundleBuild_TagMatchesBundleLookup`:
  1. Same temp setup.
  2. Inject a fake `DockerBuilder` (via the existing test seam if
     available, otherwise via a constructor override) that records the
     `(tag, workDir, platform)` triple.
  3. Invoke `runBundle` with `--build`.
  4. Assert the recorded build tag equals `qr-dali-app:latest` AND equals
     the `IMAGE=` line in the generated env files.
- Both tests are `qr-dali` so the failure mode named in the retro
  reproduces directly.

Existing `TestDeriveProjectName_SanitisesAdversarialInput` must continue
to pass unchanged.

### New dependencies

None.

## Consequences

**Positive.**

- `vibew bundle` and `vibew bundle --build` produce identical image tags
  by construction, not by coincidence. The two-chain divergence is
  eliminated.
- `name:` remains optional, matching the ergonomic goal of #1144 / #1145.
- `BuildService` becomes more testable: callers can pin the tag, removing
  hidden dependence on `cfg.ComposeProjectName()` for callers that have
  already resolved the name.
- Standalone `vibew build` behavior is preserved (no flag-day for users
  invoking `vibew build` directly).

**Negative / trade-offs.**

- `BuildOptions` grows a field. Mitigated by clear godoc and the explicit
  short-circuit in `Run`.
- `cfg.ComposeProjectName()` continues to return `"vibewarden"` in the
  load path, which is a latent bug for any future caller that
  re-introduces it without populating `ProjectRoot`. PM scoped the source
  fix out; we accept the risk and document it in the new
  `TestComposeProjectName_FallsBackToVibewardenWhenProjectRootEmpty` guard
  test, which makes the misbehavior visible if anyone adds a new
  consumer.

**Future work (not in this issue).**

- Track a follow-up to populate `cfg.ProjectRoot` inside `LoadRaw` (the
  source fix). Once that lands, `ComposeProjectName()` becomes a valid
  authority and `BuildOptions.ImageTag` can be removed if desired.
