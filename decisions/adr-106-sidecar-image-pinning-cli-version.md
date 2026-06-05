# ADR-106: Sidecar image pinning — thread CLI version into compose render

**Date**: 2026-06-05
**Issue**: [#1385](https://github.com/vibewarden/vibewarden/issues/1385)
**Status**: Accepted

---

## Context

The generated `docker-compose.yml` (rendered by `vibew dev`, `vibew generate`,
`vibew obs up`) and the sidecar `docker-compose.yml` inside a `vibew bundle`
artifact both hardcoded:

```yaml
image: ghcr.io/vibewarden/vibewarden:latest
```

with no `pull_policy`. This caused silent CLI↔sidecar version skew: a stale
locally-cached `:latest` layer could run an arbitrarily old sidecar while the
CLI was a newer version. The regression surfaced during the v0.20.0 smoke test.

### Goreleaser version/tag invariant

`.goreleaser.yml` builds both `main.version` (via `-ldflags`) and the GHCR
image tag from the same `{{ .Version }}` expression, which strips the leading
`v`. A release tagged `v0.20.0` yields:

- `main.version == "0.20.0"` (no leading `v`)
- image tag `ghcr.io/vibewarden/vibewarden:0.20.0` (no leading `v`)

They match verbatim. Source/dev builds (`git describe --tags`) produce strings
like `v0.20.0-5-gabc1234` or the literal `dev` — always with a leading `v` or
the unversioned word `dev`.

---

## Decision

**Thread `main.version` into compose render; compute the sidecar image reference
in Go; templates only interpolate.**

### Image ref strategy

A `isReleaseVersion(v)` predicate (`^\d+\.\d+\.\d+([-.].+)?$`) decides:

| Version string | image | pull_policy |
|---|---|---|
| `0.20.0` (release) | `ghcr.io/vibewarden/vibewarden:0.20.0` | omitted |
| `0.20.0-rc.1` (pre-release) | `ghcr.io/vibewarden/vibewarden:0.20.0-rc.1` | omitted |
| `dev` | `ghcr.io/vibewarden/vibewarden:latest` | `always` |
| `v0.20.0-5-gabc1234` (git-describe) | `ghcr.io/vibewarden/vibewarden:latest` | `always` |
| `v0.20.0` (leading v) | `ghcr.io/vibewarden/vibewarden:latest` | `always` |
| `""` (empty) | `ghcr.io/vibewarden/vibewarden:latest` | `always` |

Rationale for omitting `pull_policy` on release tags: the tag is immutable
once published to GHCR — forcing a pull adds latency to `vibew dev` and breaks
airgapped deployments. `pull_policy: always` for dev/source builds keeps
contributors current; no `:dev` image is published so `:latest` + `always` is
the only valid target.

### Implementation

1. **`internal/config/sidecar_image.go`** — single source of truth:
   - `const sidecarImageRepo = "ghcr.io/vibewarden/vibewarden"`
   - `func SidecarImageRef(version string) (image, pullPolicy string)`
   - `func isReleaseVersion(v string) bool`

2. **`internal/config/config.go`** — two render-only fields (mirroring
   `ProjectRoot`/`DeployMode`, `mapstructure:"-"`):
   - `SidecarImage string`
   - `SidecarPullPolicy string`

3. **CLI version plumbing** — `main.version` already reaches `NewRootCmd(version)`.
   Thread it into the four compose-rendering subcommand constructors:
   `NewDevCmd(version)`, `NewGenerateCmd(version)`, `NewObsCmd(version)`,
   `NewBundleCmd(version)`. A shared `applySidecarImageRef(cfg, version)` helper
   (in `resolve_helpers.go`) sets the two cfg fields after `loadAndResolve`.

4. **Bundle path** — `bundle.Service.WithVersion(version)` setter propagates the
   version into `renderSidecarCompose` and `bundleMultiSiteSidecar`. Release CLI
   bundles pin `:<version>`; source CLI bundles use `:latest`+`always`.
   `vibew bundle` also calls `applySidecarImageRef` so the single-site
   docker-compose.yml is pinned too.

5. **Templates** — both templates replace the hardcoded line:
   - `docker-compose.yml.tmpl`: `image: {{ .SidecarImage }}` + conditional `pull_policy`
   - `sidecar-compose.yml.tmpl`: `image: {{ .Image }}` + conditional `pull_policy`

---

## Consequences

- Release users get fully reproducible stacks: CLI v0.20.0 always starts
  sidecar `:0.20.0`, never a stale local `:latest`.
- Airgapped deployments work correctly: once `:0.20.0` is pulled, no
  subsequent pull is forced.
- Source/contributor builds use `:latest`+`always`, keeping the local layer
  current without requiring a `:dev` image to be published.
- The goreleaser `main.version`==image-tag invariant (no `v` prefix) becomes a
  release-time invariant to protect: changing it would silently break pinning.
- `vibew bundle` artifacts from released CLIs pin a concrete sidecar version;
  bundles from source CLIs document the expected `always`-pull fallback.
