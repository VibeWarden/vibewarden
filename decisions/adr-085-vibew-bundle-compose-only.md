# ADR-085: `vibew bundle` — compose-only deployment artifact generator

**Date**: 2026-04-20
**Issue**: [#1044](https://github.com/VibeWarden/vibewarden/issues/1044)
**Status**: Accepted
**Supersedes**: — (replaces `vibew deploy --dry-run` as the user-facing path
once ADR for deploy sunset lands via #1051)

---

## Context

Four retros (`~/notes/vibewarden/`) converged on the same finding: `vibew
deploy` is the single largest source of user friction. Sixteen deploy bugs have
been logged across three retro cycles. The replacement path is a
*bundle-then-deploy-manually* flow — vibew produces a self-contained Docker
Compose artifact and the user runs `scp` / `ssh` / `docker compose up -d`
themselves.

The functional core already exists: `vibew deploy --dry-run` calls
`deployapp.Service.Bundle` and prints the generated file tree. #1044 promotes
that path to a first-class `vibew bundle` command, scoped to Docker Compose
only. Helm, Fly.io, and raw Kubernetes manifests are split out to #1052. The
package rename (`internal/app/deploy/` → `internal/app/bundle/`) is deferred to
#1051 (deploy sunset) so that `vibew deploy --dry-run` keeps working as a
fallback during the transition.

Dependency: #1053 merged — `config.LoadStrict` is the required entry point.
Unknown keys in `vibewarden.production.yaml` must abort before any files are
written.

## Decision

Add `vibew bundle` as a thin cobra command that invokes
`deployapp.Service.Bundle` with the same options `vibew deploy --dry-run`
currently passes. Extend the service with four additive artifacts (`deploy.sh`,
`sample.env`, `.env`, `README.md`, `image.tar`) and a pluggable writer so
idempotency and image export are both testable without disk/docker I/O.

Migration approach (c): shared internal package, no duplication; `vibew deploy
--dry-run` keeps working unchanged because both commands route through the
same `Service.Bundle`.

### CLI surface

```
vibew bundle [flags]

Flags:
  --output <dir>    Output directory (default: .vibewarden/bundle)
  --overwrite       Overwrite an existing .env inside the output dir
  --image <tag>     Docker image tag to package (default: <project>-app:latest)
  --skip-image      Do not package image.tar
```

No `--target`, no `--env`, no `--dry-run`. Environment selection stays
file-based (`vibewarden.production.yaml`). Godoc is a hard requirement —
`vibew bundle --help` must be readable by an LLM agent.

### Output layout

```
.vibewarden/bundle/
  docker-compose.yml    # image: pinned, never build:
  vibewarden.yaml       # merged base + prod override, strict-validated
  sample.env            # regenerated every run
  .env                  # written on first run only; --overwrite to replace
  deploy.sh             # mode 0o750, 10-line reference script
  image.tar             # omitted with --skip-image
  README.md             # 3-paragraph manual-deploy guide
  kratos/, credentials  # anything the existing generator produces
```

### Domain model changes

None. `config.Config`, `deployapp.BundleOptions`, and the generator pipeline
are already sufficient. The four new artifacts are derived values, not new
entities.

### Ports (interfaces)

Two new small ports are introduced so the bundle command is testable with zero
docker and zero real filesystem touches.

```go
// internal/ports/bundle.go

// BundleFS is the filesystem port used by the bundle service to write and
// inspect artifacts. Implementations back onto the real filesystem in
// production and onto an in-memory map in tests. Paths are absolute.
type BundleFS interface {
    Exists(path string) (bool, error)
    WriteFile(path string, data []byte, perm fs.FileMode) error
    MkdirAll(path string, perm fs.FileMode) error
}

// ImageSaver saves a local Docker image to a tar archive. This is the same
// shape as ports.ImageExporter but scoped to the bundle use case so the
// bundle service does not pick up the rsync-to-remote semantics baked into
// the deploy Service. Implementations shell out to "docker save -o".
type ImageSaver interface {
    Save(ctx context.Context, imageTag, destPath string) error
}
```

`ImageSaver` has the identical signature as the existing `ports.ImageExporter`
but is renamed to decouple bundling from deploy transfer (future-proofs the
sunset in #1051). The existing `opsadapter.ImageExportAdapter` satisfies both
interfaces by structural typing.

### Adapters

- `internal/adapters/bundlefs/osfs.go` — thin `BundleFS` implementation over
  `os` / `io/fs`. Default in `cmd/bundle.go`.
- `internal/adapters/ops/image_export.go` — already implements `ImageSaver`
  by shape; no changes required.

### Application service

The existing `deployapp.Service.Bundle` is extended with four new render
helpers and idempotency. All new artifact logic lives in
`internal/app/deploy/bundle_extras.go` (new file) so `bundle.go` stays focused
on the compose/yaml pipeline.

#### New render functions (pure)

- `renderSampleEnv(cfg *config.Config) string`
  Emits `VIBEWARDEN_APP_IMAGE=<cfg.ComposeProjectName()>-app:latest` plus any
  keys discovered from the project's `.env.template` (if present) with
  sensible defaults. No random values, no timestamps — output is
  deterministic given the config.

- `renderDotEnv(cfg *config.Config) string`
  Identical body to `renderSampleEnv` for v1; this exists as a distinct call
  so idempotency rules (`.env` written only on first run) are enforced at
  the orchestration layer, not the render layer.

- `renderDeploySH(projectName string, skipImage bool) string`
  10-line bash reference. Exact content:

  ```bash
  #!/usr/bin/env bash
  # Reference deploy script generated by `vibew bundle`.
  # Edit freely — vibew will never modify this file.
  set -euo pipefail

  if [[ $# -lt 1 ]]; then
    echo "usage: $0 <user@host>" >&2
    exit 1
  fi

  HOST="$1"
  tar czf bundle.tar.gz .
  scp bundle.tar.gz "$HOST":~/
  ssh "$HOST" 'tar xzf bundle.tar.gz && {{LOAD}}docker compose up -d'
  ```

  The `{{LOAD}}` placeholder becomes `docker load -i image.tar && ` when
  `--skip-image=false`, and `docker compose pull && ` when `--skip-image=true`.

- `renderBundleReadme(projectName string, skipImage bool) string`
  Three paragraphs, ≤ 40 lines. Sections: "What this is", "Deploy in three
  steps" (scp / ssh / up -d), "Rebuild for a different arch" (with the
  `vibew build --platform linux/amd64` hint).

#### Bundle orchestration (single-site)

The single-site branch in `bundleSingleSite` is extended after the existing
generator call:

1. Run existing `generator.Generate(...)` → writes compose, kratos, credentials.
2. Write merged `vibewarden.yaml` (existing behaviour — unchanged).
3. Write `sample.env` (always overwrite).
4. Write `.env` — only when `!exists || opts.Overwrite`.
5. Write `deploy.sh` (mode `0o750`, always overwrite).
6. Write `README.md` (always overwrite).
7. Save image to `image.tar` — only when `!opts.SkipImage`. Uses
   `ImageSaver.Save(ctx, opts.ImageTag, filepath.Join(outDir, "image.tar"))`.

`BundleOptions` gains three fields:

```go
type BundleOptions struct {
    // ... existing fields ...
    Overwrite bool    // overwrite .env (default: false)
    SkipImage bool    // omit image.tar (default: false)
    ImageTag  string  // default: cfg.ComposeProjectName()+"-app:latest"
}
```

`ImageTag` defaults are resolved in the CLI layer so the service never
invents names.

### File layout

| Path | Purpose |
|---|---|
| `internal/cli/cmd/bundle.go` | New cobra command; thin wrapper around `deployapp.Service.Bundle`. |
| `internal/cli/cmd/bundle_test.go` | Flag parsing + help text golden. |
| `internal/app/deploy/bundle_extras.go` | `renderSampleEnv`, `renderDotEnv`, `renderDeploySH`, `renderBundleReadme`. |
| `internal/app/deploy/bundle_extras_test.go` | Table-driven unit tests per renderer. |
| `internal/app/deploy/bundle_idempotency_test.go` | Run bundle twice, assert `.env` stable, `sample.env` rewritten. |
| `internal/app/deploy/bundle_parity_test.go` | Parity guard (see below). |
| `internal/app/deploy/testdata/bundle/minimal/` | Golden output for minimal config. |
| `internal/app/deploy/testdata/bundle/full/` | Golden output for full config. |
| `internal/ports/bundle.go` | `BundleFS`, `ImageSaver` interfaces. |
| `internal/adapters/bundlefs/osfs.go` | Default `BundleFS` implementation. |
| `internal/adapters/bundlefs/osfs_test.go` | Adapter round-trip test. |

No edits to `internal/app/deploy/bundle.go`'s existing code paths — all new
behaviour is appended via `bundle_extras.go` so the rename in #1051 is a
simple `git mv`.

### Sequence — `vibew bundle`

1. `cmd/bundle.go` calls `requireScaffolding()` (same as deploy).
2. Resolves `absConfig = filepath.Abs("vibewarden.yaml")` when `--output` or
   `--config` omitted (reuses existing helpers).
3. Computes `prodConfigPath = prodConfigPathForEnv(absConfig, "production")`
   (hard-coded to production for v1 — no `--env` flag exposed).
4. Calls `config.LoadStrict(absConfig, prodConfigPath)`. On
   `*UnknownKeyError` → prints the error and returns a non-zero exit. No
   output files are created.
5. Derives `projectName` via the same chain used by `runDeploy`
   (`cfg.Name` → `cfg.App.Image` → `ProjectNameFromConfig`).
6. Constructs a `deployapp.Service` with `nil` executor, a `ConfigGenerator`,
   and an `ImageSaver` (when `!skip-image`).
7. If `cfg.MultiSite`: prints a clear error and exits non-zero
   (see "Multi-site" section).
8. Calls `svc.Bundle(ctx, BundleOptions{..., Overwrite, SkipImage, ImageTag})`.
9. Prints a file listing (reuses the `filepath.Walk` block in
   `runDeployDryRun`, extracted to `bundleListing(out io.Writer, dir string)`).
10. Prints "Bundle written to <outDir>. Next: ./deploy.sh <user@host>".

### Error cases

| Condition | Behaviour |
|---|---|
| Scaffold missing (`.vibewarden/` absent) | Error via `requireScaffolding()`; exit non-zero. |
| Unknown key in base or production config | `*UnknownKeyError` printed with file + key; no files written. |
| Output dir is a file (not a dir) | `BundleFS.MkdirAll` returns an error; propagated. |
| `docker save` fails (image missing) | `ImageSaver.Save` returns wrapped error; bundle aborts. Compose/yaml/env/readme/deploy.sh remain on disk (fail-late by design — user can fix image and rerun with `--skip-image` to inspect the rest). |
| `cfg.MultiSite == true` | Error: `multi-site projects are not supported by vibew bundle in v1; use vibew deploy` — non-zero exit. |
| `--overwrite` passed but `.env` absent | No-op; `.env` is written normally. |
| `--skip-image` + `image.tar` exists in output | Left in place (not deleted); `deploy.sh` and `README.md` are regenerated for the registry-pull flow. Documented in the README as a known quirk. |

### Test strategy

| Layer | Scope | Mocks |
|---|---|---|
| Unit | Each renderer (`sample.env`, `.env`, `deploy.sh`, `README.md`) | None — pure functions. |
| Unit | `Service.Bundle` idempotency (run twice, `.env` unchanged) | In-memory `BundleFS`, fake `ImageSaver`. |
| Unit | `Service.Bundle` `--overwrite` replaces `.env` | In-memory `BundleFS`. |
| Unit | `Service.Bundle` `--skip-image` omits `image.tar` | Fake `ImageSaver` with a `.Called` flag. |
| Golden | Full output tree vs `testdata/bundle/minimal,full/` | None — writes to `t.TempDir()`. |
| Regression | `*UnknownKeyError` in `vibewarden.production.yaml` aborts before write | Real `LoadStrict`, `t.TempDir()`. |
| Integration (`//go:build integration`) | `vibew init foo && vibew build && vibew bundle && docker compose up -d && curl /healthz` | None. Skipped when `DOCKER_HOST` unset. |
| **Parity guard** | See below. | See below. |

#### Parity guard — semantic equivalence, not byte-identical

A byte-identical comparison between `vibew deploy --dry-run` output and
`vibew bundle` output is **not feasible** without code changes:

1. `runDeployDryRun` writes the bundle to `os.MkdirTemp("", "vibewarden-dry-run-*")`
   and the resulting absolute paths bleed into no file content today — but
   the output listing they print differs by temp-dir prefix.
2. `docker-compose.yml` is 100% deterministic given the same config (no
   timestamps, no random bytes). `vibewarden.yaml` is the merged YAML map —
   deterministic via `yaml.Marshal`. These two CAN be byte-identical.
3. `.credentials` is randomised every run (passwords generated by
   `credentialsadapter.NewGenerator`). This file is NOT under parity.

**Decision**: parity is defined on two files only — `docker-compose.yml` and
`vibewarden.yaml` — and must be byte-identical. Other files are compared
semantically (file set equality; `.credentials` existence; `.env` vs
`sample.env` idempotency rules). This matches what the PM spec actually
needs (regression guard for #1053) and avoids a brittle full-tree diff.

Test lives at `internal/app/deploy/bundle_parity_test.go`. It drives
`Service.Bundle` twice with the same `BundleOptions` into two separate
temp dirs and asserts:

- `docker-compose.yml` — `bytes.Equal`
- `vibewarden.yaml` — `bytes.Equal`
- file set — `reflect.DeepEqual` after removing `image.tar` (docker-dependent)
  and `.credentials` (randomised)

No call to `runDeployDryRun` is needed — both paths share the identical
`Service.Bundle` call by construction; the parity test proves the call is
deterministic and wire-compatible.

### Multi-site — single-site only in v1

`Service.Bundle` currently branches on `opts.MultiSite` and produces
`.sidecar/` + per-site layouts. `vibew bundle` in v1 hard-errors when
`cfg.MultiSite == true`:

```
Error: multi-site projects are not supported by `vibew bundle` yet.
Use `vibew deploy --target <host>` for multi-site deploys.
Track progress: https://github.com/VibeWarden/vibewarden/issues/1052
```

Rationale: multi-site output is two compose files plus a shared `global.yaml`,
and the idempotency rules for per-site `.env` files need their own design
pass. Defer to a follow-up issue.

### New dependencies

None. Everything reuses existing packages (`gopkg.in/yaml.v3`,
`text/template`, `cobra`, `os`, `path/filepath`).

## Consequences

### Positive

- `vibew deploy` complexity stops growing — all new artifact generation
  lands in the isolated `bundle_extras.go` file and is trivially testable.
- Users get a mental model that matches what they actually need: "produce
  files, then do what you want with them". No SSH, no rsync, no docker-save
  hidden behind a flag.
- `.env` idempotency unblocks the documented workflow — users can edit
  secrets once and rerun bundle without fear of losing them.
- Parity guard (semantic, not byte) protects the #1053 regression without
  introducing a brittle full-tree diff.

### Negative / trade-offs

- Two commands temporarily expose the same bundle output: `vibew deploy
  --dry-run` and `vibew bundle`. This is by design until #1051 deletes
  `vibew deploy` entirely. Documented in the issue; no deprecation warning
  added in this story.
- `image.tar` is always named `image.tar` in v1 (no `--image-tar-name` flag).
  If a user packages two images, they overwrite one another. Acceptable for
  single-site v1; revisit when multi-site lands.
- `deploy.sh` is a reference, not a tested artifact. Users editing it is
  expected and fine. The integration test does not exercise `deploy.sh`.

### Follow-ups

- #1051 — sunset `vibew deploy`, rename `internal/app/deploy/` to
  `internal/app/bundle/`, delete this ADR's "package rename deferred" clause.
- #1052 — Helm / Fly.io / k8s bundle targets.
- Future — multi-site bundle support (separate issue, TBD).

### Alternatives considered

**Option (a) — extract `bundle` into a new package now.**
Rejected: breaks `vibew deploy --dry-run` during the transition; #1051 is
the right place to do the rename because it can delete `Deploy.Deploy` at
the same time.

**Option (b) — inline the four renderers into `cmd/bundle.go`.**
Rejected: violates the hexagonal rule — render logic in the CLI layer is
not unit-testable without cobra plumbing. `bundle_extras.go` keeps the pure
functions in the app layer where they belong.

**Option (c) — chosen.**
Thin `cmd/bundle.go` entrypoint, shared `deployapp.Service.Bundle`. Lowest
risk, smallest diff, cleanest migration path to #1051.
