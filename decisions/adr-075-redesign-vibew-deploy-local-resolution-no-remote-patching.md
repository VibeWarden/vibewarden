# ADR-075: Redesign vibew deploy -- local resolution, no remote patching

> **Historical.** The `vibew deploy` command described here was sunset by
> [ADR-086](adr-086-sunset-vibew-deploy.md). The "local resolution, no remote
> patching" principle survived it and is now realised by `vibew bundle`
> ([ADR-085](adr-085-vibew-bundle-compose-only.md)). Deploy-contract lineage:
> ADR-075 → [ADR-085](adr-085-vibew-bundle-compose-only.md) →
> [ADR-086](adr-086-sunset-vibew-deploy.md) →
> [ADR-088](adr-088-deploy-sh-local-run-convention.md). This ADR is retained as
> a record of the original decision.

**Date**: 2026-04-18
**Issue**: #938
**Status**: Accepted (2026-04-18); historical since ADR-086 sunset `vibew deploy`

### Context

`vibew deploy` is unreliable. Two independent agent tests abandoned it and wrote
manual compose files. The root causes are:

1. **Remote `sed` patching**: `deploySite()` in `multiapp.go` rewrites
   `upstream.host` on the remote via `sed` after transferring `vibewarden.yaml`.
   This is fragile (depends on exact YAML formatting, breaks on quoted values,
   fails silently on BSD vs GNU sed differences).

2. **Broken build context path**: `docker-compose.yml.tmpl` uses
   `context: ../../{{ .App.Build }}` which assumes the compose file lives at
   `.vibewarden/generated/docker-compose.yml` (two levels deep). On the remote
   the compose file lives at `~/vibewarden/<project>/docker-compose.yml` (one
   level deep), so the build context resolves to the wrong directory.

3. **Non-deterministic rsync source**: The single-site `Deploy()` rsyncs
   `.vibewarden/generated/` which is a dev-oriented output directory. The
   multi-site `deploySite()` writes files via SSH `cat > ...` heredocs instead
   of rsyncing a local directory. The two paths are inconsistent and neither
   produces a self-contained deploy bundle.

4. **Config transfer after generation**: The current flow generates files to
   `.vibewarden/generated/`, then separately rsyncs `vibewarden.yaml` via
   `TransferFile`. This means the generated `docker-compose.yml` and the config
   file travel via different mechanisms with ordering constraints (build context
   must come before config to avoid overwrite).

5. **No local preview**: There is no way to inspect exactly what will be
   deployed before deploying.

The principle: **everything is resolved locally; the remote only runs
`docker compose up -d`**.

### Decision

Introduce a new **deploy bundle** concept: a self-contained directory at
`.vibewarden/deploy/` that contains every file needed to run the stack on the
remote. The bundle is produced entirely locally with all values resolved -- no
sed, no runtime patching, no relative path tricks.

#### 1. New output directory: `.vibewarden/deploy/`

The deploy flow writes to `.vibewarden/deploy/` (not `.vibewarden/generated/`).
The existing `.vibewarden/generated/` directory continues to be used by
`vibew dev` / `vibew generate` for local development. The deploy directory is a
separate, prod-oriented output.

#### 2. Deploy bundle contents

**Single-site mode** (no TLS domain, legacy path):
```
.vibewarden/deploy/
  docker-compose.yml     # fully resolved, build context: ./<build-dir>
  vibewarden.yaml        # prod config with upstream.host resolved
  .credentials           # generated credentials
  .env                   # Docker Compose env vars
  kratos/                # (if auth mode kratos)
  openbao/               # (if secrets enabled)
  observability/         # (if observability enabled)
```

**Multi-site mode** (has TLS domain):
```
.vibewarden/deploy/
  .sidecar/
    docker-compose.yml   # sidecar compose (rendered from template)
    global.yaml          # sidecar global config
  sites/<project>/
    docker-compose.yml   # per-app compose (rendered from template)
    vibewarden.yaml      # prod config with upstream.host = container name
```

#### 3. Local resolution of upstream.host

Instead of transferring the user's `vibewarden.yaml` and then running `sed` on
the remote, the deploy service:

1. Reads the user's config via `config.Load(configPath)`.
2. Creates a **prod-resolved copy** by calling a new pure function
   `ResolveProdConfig(cfg, projectName)` in `internal/app/deploy/resolve.go`.
   This function:
   - For multi-site: if `upstream.host` is a local address (0.0.0.0, 127.0.0.1,
     localhost), replaces it with the container name
     (`vibewarden-<project>-app`).
   - For single-site: if `upstream.host` is a local address and the app runs in
     a container (`app.image` or `app.build` is set), replaces it with `app`
     (the Docker service name in the single-site compose).
   - Leaves all other config unchanged.
3. Marshals the resolved config to YAML and writes it to
   `.vibewarden/deploy/[sites/<project>/]vibewarden.yaml`.

This eliminates all remote `sed` calls. The function is pure (no I/O except the
final write) and trivially testable.

#### 4. Local resolution of build context paths

The existing `docker-compose.yml.tmpl` uses `context: ../../{{ .App.Build }}`
which breaks remotely. Two changes:

- **Single-site deploy**: A new template variant or template data field is used.
  When generating for deploy, set `App.Build` to a path relative to the deploy
  directory (e.g., `./app` if the build context is copied into
  `.vibewarden/deploy/app/`). The deploy service copies (or notes for rsync)
  the build context into the deploy bundle.

- **Multi-site deploy**: The `app-compose.yml.tmpl` already uses
  `context: .` which is correct because the build context is rsynced into the
  site directory.

For single-site, add a new field `BuildContextInBundle` to the template data
(or modify `App.Build` before passing to the template). When producing the
deploy bundle, set `App.Build = "."` or `App.Build = "./<subdir>"` so the
compose file's build context is relative to the deploy directory, not to the
original project root.

#### 5. Deploy service refactoring

**`internal/app/deploy/resolve.go`** (new file):

```go
// ResolveProdConfig returns a copy of cfg with all values resolved for
// production deployment. The returned config can be marshalled to YAML
// and deployed as-is with no further patching.
func ResolveProdConfig(cfg *config.Config, projectName string, multiSite bool) *config.Config

// MarshalConfig serialises a Config to YAML bytes suitable for writing
// to the deploy bundle.
func MarshalConfig(cfg *config.Config) ([]byte, error)
```

**`internal/app/deploy/bundle.go`** (new file):

```go
// BundleOptions holds parameters for producing the deploy bundle.
type BundleOptions struct {
    Config      *config.Config
    ConfigPath  string      // original config file path
    ProjectName string
    MultiSite   bool
    OutputDir   string      // defaults to ".vibewarden/deploy"
}

// Bundle produces a complete deploy bundle under OutputDir.
// For single-site mode, it generates docker-compose.yml, vibewarden.yaml,
// credentials, and supporting files.
// For multi-site mode, it generates the .sidecar/ and sites/<project>/
// subdirectories.
// All values are fully resolved -- no sed or runtime patching needed.
func (s *Service) Bundle(ctx context.Context, opts BundleOptions) error
```

**`internal/app/deploy/service.go`** (modified):

- `Deploy()` is refactored to call `Bundle()` first, then rsync
  `.vibewarden/deploy/` to the remote. The rsync source is always
  `.vibewarden/deploy/` -- deterministic, never CWD or parent-of-CWD.
- Remove the separate `TransferFile` call for `vibewarden.yaml` (it is now
  inside the bundle).
- Remove the separate build context `Transfer` call for single-site (the build
  context is now inside the bundle or rsynced as a second Transfer from a known
  path).
- The `generator.Generate()` call is replaced by the `Bundle()` method which
  internally calls the generator for docker-compose.yml rendering but writes
  everything to the deploy directory.

**`internal/app/deploy/multiapp.go`** (modified):

- `deploySite()` no longer calls `sed` on the remote. Instead, it calls
  `Bundle()` locally to produce `sites/<project>/` under
  `.vibewarden/deploy/`, then rsyncs the site directory.
- `BootstrapSidecar()` calls `Bundle()` for the sidecar compose and
  global.yaml (written to `.vibewarden/deploy/.sidecar/`), then rsyncs the
  entire `.vibewarden/deploy/` directory.
- `DeployMultiApp()` calls `Bundle()` for just the new site, then rsyncs
  only `.vibewarden/deploy/sites/<project>/` to the remote.
- Remove `writeRemoteFile()` (files are written locally and rsynced).
- Remove `renderGlobalYAML()`, `renderSidecarCompose()`, `renderAppCompose()`
  from multiapp.go -- move rendering into `Bundle()`.

#### 6. Transfer flow

**Single-site**:
```
rsync -az .vibewarden/deploy/ user@host:~/vibewarden/<project>/
   + (if app.build): rsync -az <build-context>/ user@host:~/vibewarden/<project>/<build-subdir>/
ssh user@host "cd ~/vibewarden/<project> && docker compose up -d [--build]"
```

**Multi-site bootstrap** (fresh install):
```
rsync -az .vibewarden/deploy/.sidecar/ user@host:~/vibewarden/.sidecar/
rsync -az .vibewarden/deploy/sites/<project>/ user@host:~/vibewarden/sites/<project>/
   + (if app.build): rsync -az <build-context>/ user@host:~/vibewarden/sites/<project>/<build-subdir>/
ssh: docker network create vibewarden-multiapp 2>/dev/null || true
ssh: cd ~/vibewarden/sites/<project> && docker compose up -d [--build]
ssh: cd ~/vibewarden/.sidecar && docker compose pull && docker compose up -d
```

**Multi-site add-site**:
```
rsync -az .vibewarden/deploy/sites/<project>/ user@host:~/vibewarden/sites/<project>/
   + (if app.build): rsync -az <build-context>/ user@host:~/vibewarden/sites/<project>/<build-subdir>/
ssh: cd ~/vibewarden/sites/<project> && docker compose up -d [--build]
ssh: cd ~/vibewarden/.sidecar && docker compose restart vibewarden
```

#### 7. Drift detection compatibility (issue #920)

Drift detection currently runs a dry-run rsync comparing the local generated
directory to the remote. With the new model, the dry-run compares
`.vibewarden/deploy/` (or the specific subdirectory being deployed) to the
remote. This is strictly better because the deploy bundle is the complete truth
of what will be deployed.

#### 8. `vibew generate --profile prod` integration

`vibew deploy` calls `Bundle()` internally -- `vibew generate` remains a
separate command that writes to `.vibewarden/generated/` for local development.
No change to `vibew generate` in this ADR. A future ADR may add
`vibew generate --profile prod --output .vibewarden/deploy/` for users who want
to inspect the deploy bundle without deploying.

#### 9. `vibew dev` is unchanged

`vibew dev` continues to use `.vibewarden/generated/` with the `context: ../../`
relative path which is correct when Docker Compose runs from
`.vibewarden/generated/`. No changes to the dev flow.

### Domain model changes

No new domain entities. The deploy bundle is a build artifact, not a domain
concept.

### Ports (interfaces)

No new port interfaces. The existing `ports.ConfigGenerator` and
`ports.RemoteExecutor` are sufficient. `Bundle()` uses `ConfigGenerator` to
render templates and writes output to the filesystem directly (the deploy
service is in the app layer, filesystem writes for local bundle creation are
acceptable -- the bundle is an ephemeral build artifact, not I/O through an
adapter boundary).

### Adapters

No new adapters. The existing SSH executor adapter is used unchanged.

### Application service

**Modified**: `internal/app/deploy/service.go`
- `Deploy()` calls `Bundle()` then rsyncs `.vibewarden/deploy/` to remote.
- Remove direct `TransferFile` for config and separate build context `Transfer`.

**Modified**: `internal/app/deploy/multiapp.go`
- `deploySite()` no longer calls `sed`. Calls `Bundle()` locally.
- `BootstrapSidecar()` writes sidecar files to local bundle, rsyncs to remote.
- `DeployMultiApp()` writes site files to local bundle, rsyncs to remote.
- Remove `writeRemoteFile()`, `renderGlobalYAML()`, `renderSidecarCompose()`,
  `renderAppCompose()`.

**New**: `internal/app/deploy/resolve.go`
- `ResolveProdConfig()` -- pure function, returns config with upstream.host
  resolved for Docker networking.
- `MarshalConfig()` -- serializes config to YAML bytes.

**New**: `internal/app/deploy/bundle.go`
- `BundleOptions` struct
- `Bundle()` method on `Service` -- produces the complete deploy directory.
- `bundleSingleSite()` -- internal helper for single-site bundle.
- `bundleMultiSiteSidecar()` -- internal helper for sidecar compose + global.yaml.
- `bundleMultiSiteSite()` -- internal helper for per-site compose + config.

### File layout

New files:
- `internal/app/deploy/resolve.go`
- `internal/app/deploy/resolve_test.go`
- `internal/app/deploy/bundle.go`
- `internal/app/deploy/bundle_test.go`

Modified files:
- `internal/app/deploy/service.go` -- refactor `Deploy()` to use Bundle+rsync
- `internal/app/deploy/multiapp.go` -- remove sed, use Bundle+rsync
- `internal/app/deploy/service_test.go` -- update tests for new flow
- `internal/app/deploy/multiapp_test.go` -- remove sed tests, add bundle tests
- `internal/config/templates/docker-compose.yml.tmpl` -- add conditional for
  deploy mode build context (or use template data override)

### Sequence

#### Single-site deploy (no TLS domain):

1. CLI parses flags, loads config via `config.Load(configPath)`.
2. `Deploy()` calls `Bundle(ctx, BundleOptions{...})`.
3. `Bundle()` calls `ResolveProdConfig(cfg, projectName, false)` to get a
   prod-ready config with upstream.host resolved.
4. `Bundle()` calls `MarshalConfig(resolvedCfg)` and writes
   `.vibewarden/deploy/vibewarden.yaml`.
5. `Bundle()` calls `generator.Generate(ctx, input, ".vibewarden/deploy")` to
   render docker-compose.yml, kratos/, credentials, etc. into the deploy dir.
   The generator input uses the resolved config (not the original).
6. If `app.build` is set, `Bundle()` copies/notes the build context directory
   into `.vibewarden/deploy/<build-subdir>/`.
7. `Deploy()` checks remote prerequisites (docker, docker compose).
8. `Deploy()` runs drift detection: `DryRunTransfer(".vibewarden/deploy/", remoteDir)`.
9. `Deploy()` calls `Transfer(".vibewarden/deploy/", remoteDir, true)`.
10. If `app.build`, `Deploy()` calls `Transfer(buildContextLocal, remoteBuildDir, false)`.
11. `Deploy()` runs `docker compose pull` / `docker compose up -d [--build]`.
12. `Deploy()` runs health check via SSH.

#### Multi-site bootstrap (fresh install with TLS domain):

1. CLI parses flags, loads config, detects `ModeFreshInstall`.
2. `BootstrapSidecar()` calls `bundleMultiSiteSidecar()` to write
   `.vibewarden/deploy/.sidecar/docker-compose.yml` and
   `.vibewarden/deploy/.sidecar/global.yaml`.
3. `BootstrapSidecar()` calls `bundleMultiSiteSite(cfg, projectName)` to write
   `.vibewarden/deploy/sites/<project>/docker-compose.yml` and
   `.vibewarden/deploy/sites/<project>/vibewarden.yaml` (with upstream.host
   resolved).
4. Rsync `.vibewarden/deploy/.sidecar/` to `~/vibewarden/.sidecar/`.
5. Rsync `.vibewarden/deploy/sites/<project>/` to `~/vibewarden/sites/<project>/`.
6. If `app.build`, rsync build context to remote site dir.
7. SSH: `docker network create vibewarden-multiapp 2>/dev/null || true`.
8. SSH: `cd ~/vibewarden/sites/<project> && docker compose up -d [--build]`.
9. SSH: `cd ~/vibewarden/.sidecar && docker compose pull && docker compose up -d`.
10. Health check via SSH.

#### Multi-site add-site:

1. CLI parses flags, loads config, detects `ModeAddSite`.
2. `DeployMultiApp()` calls `bundleMultiSiteSite(cfg, projectName)`.
3. Rsync `.vibewarden/deploy/sites/<project>/` to `~/vibewarden/sites/<project>/`.
4. If `app.build`, rsync build context to remote site dir.
5. SSH: `cd ~/vibewarden/sites/<project> && docker compose up -d [--build]`.
6. SSH: `cd ~/vibewarden/.sidecar && docker compose restart vibewarden`.
7. Health check via SSH.

### Error cases

| Error | Handling |
|-------|----------|
| Config load failure | Return `fmt.Errorf("loading config: %w", err)` -- unchanged |
| ResolveProdConfig fails (invalid config) | Return `fmt.Errorf("resolving prod config: %w", err)` |
| MarshalConfig fails | Return `fmt.Errorf("marshalling config: %w", err)` |
| Bundle directory creation fails | Return `fmt.Errorf("creating deploy bundle: %w", err)` |
| Generator fails | Return `fmt.Errorf("generating config files: %w", err)` -- unchanged |
| Remote prerequisites missing | Return `fmt.Errorf("remote prerequisites check failed: %w", err)` -- unchanged |
| Drift detected (no --force) | Return `*DriftError` -- unchanged, but now compares deploy/ dir |
| Rsync transfer fails | Return `fmt.Errorf("transferring deploy bundle: %w", err)` |
| Docker compose up fails | Return `fmt.Errorf("docker compose up: %w", err)` -- unchanged |
| Health check timeout | Return `ErrHealthCheck` -- unchanged |
| Build context dir not found | Return `fmt.Errorf("build context %q not found: %w", path, err)` |

### Test strategy

**Unit tests (new files)**:

- `resolve_test.go`:
  - Table-driven tests for `ResolveProdConfig()` covering: local hosts
    (0.0.0.0, 127.0.0.1, localhost) are rewritten, custom hosts are preserved,
    empty host is handled, single-site vs multi-site produce correct container
    names.
  - `MarshalConfig()` round-trip test: marshal then unmarshal, verify equality.

- `bundle_test.go`:
  - `Bundle()` with single-site config produces correct file layout under a
    temp dir.
  - `Bundle()` with multi-site config produces .sidecar/ and sites/<project>/
    structure.
  - `Bundle()` with `app.build` set includes build context path in compose.
  - `Bundle()` writes resolved upstream.host (not the original).
  - `Bundle()` fails gracefully when generator returns error.

**Unit tests (modified files)**:

- `service_test.go`:
  - Remove or update tests that assert `TransferFile` for config (now inside
    bundle).
  - Update tests that assert `Transfer` count (may change due to bundle rsync).
  - Add test verifying rsync source is always `.vibewarden/deploy/` (never CWD).
  - Keep existing drift detection tests (source dir changes but behavior same).

- `multiapp_test.go`:
  - Remove `TestDeploySite_UpstreamHostRewrite` (sed is gone).
  - Remove `TestDeploySite_UpstreamHostRewriteFails`.
  - Add test verifying upstream.host is resolved in the written
    `vibewarden.yaml` file (via the bundle, not sed).
  - Keep existing health check, transfer, and compose template tests.

**Integration tests**:

- No new integration tests in this ADR. The existing SSH adapter integration
  tests cover the rsync mechanics. The bundle output is verified by unit tests
  reading files from the temp dir.

### New dependencies

None. All marshalling uses `gopkg.in/yaml.v3` which is already a transitive
dependency (MIT license, used by the config loader via mapstructure/viper).
Verify at implementation time that a direct import is needed or if
`encoding/json` + manual YAML writing suffices.

If `gopkg.in/yaml.v3` is imported directly:
- License: MIT (Apache 2.0 compatible)
- Already a transitive dependency via viper

### Consequences

#### Positive

- **Deterministic**: What you see in `.vibewarden/deploy/` is exactly what runs
  on the remote. No runtime patching, no sed, no surprises.
- **Inspectable**: Users can run the bundle step alone (future
  `vibew generate --profile prod`) and inspect every file before deploying.
- **Testable**: `ResolveProdConfig()` is a pure function. `Bundle()` writes to a
  temp dir that unit tests can read and verify.
- **Simpler transfer**: One rsync per deployment target (single-site) or two
  rsyncs (multi-site: sidecar + site). Always from a known local directory.
- **Drift detection improvement**: Dry-run rsync compares the complete deploy
  bundle, not a partial generated directory.
- **No sed dependency**: Eliminates the BSD vs GNU sed compatibility issue.

#### Negative

- **Disk space**: `.vibewarden/deploy/` duplicates some files that also exist in
  `.vibewarden/generated/`. Both directories are gitignored and ephemeral.
  Acceptable for a CLI tool.
- **Migration**: Existing deploys that relied on the old flow will need a
  `--force` on first deploy with the new code to overwrite the remote layout.
  Document in CHANGELOG.
- **`vibew dev` divergence**: The dev path (`.vibewarden/generated/` with
  `context: ../../`) and the deploy path (`.vibewarden/deploy/` with
  `context: .`) use different relative paths. This is intentional -- the two
  environments have different filesystem layouts.

#### Environment separation model

- `vibewarden.yaml` = local dev config (self-signed TLS, port 8443).
- `vibewarden.production.yaml` = production overrides (letsencrypt, port 443).
- `vibew init` generates BOTH files.
- `vibew deploy` reads both files, deep-merges the production override on top
  of the base config, resolves upstream.host, and writes the merged result to
  `.vibewarden/deploy/<env>/`.
- `vibew dev` only reads `vibewarden.yaml` -- unaffected by production overrides.
- `vibew add tls --domain` writes the domain to `vibewarden.production.yaml`
  when the file exists.
- The `--env` flag on `vibew deploy` controls which override file is loaded
  (default: "production" reads `vibewarden.production.yaml`).
- YAML serialisation uses `map[string]any` (not `yaml.Marshal(Config{})`) to
  preserve underscore field names like `rate_limit` and `security_headers`.

#### Supersedes

- This ADR supersedes the remote `sed` patching introduced in the multi-app
  deploy flow (ADR-070). The detection logic (ADR-070) is unchanged; only the
  file writing and transfer mechanics change.
- The single-site deploy flow (ADR-063) is preserved in spirit but refactored
  to use the bundle pattern.
