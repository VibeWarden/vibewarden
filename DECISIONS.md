# Architectural Decision Records

ADRs have been split into individual files for easier navigation.

See **[decisions/README.md](decisions/README.md)** for the full index.

Each ADR is a standalone file at `decisions/adr-NNN-title.md`.

## PM Log

### 2026-09-04 — #1495 batched Dependabot image bumps spec finalised

- Posted full spec as a PM comment on #1495 (https://github.com/VibeWarden/vibewarden/issues/1495#issuecomment-5533743608).
- Label `status:ready-for-arch` added (kept existing `tech-debt`, no `epic:*` — consistent with the audit/tech-debt batch convention already established for #1306/#1311/#1458).
- Not challenged: verified the story is real and current before writing the spec. All 10 named PRs (#1485-#1494) are still open; `postgres:17-alpine` and `redis:7-alpine` are still the live pins in `internal/config/templates/docker-compose.yml.tmpl`; no ADR or merged decision already covers either major bump (`decisions/README.md` has no postgres/redis entry); the #1298 drift-guard test (`internal/config/templates/image_pins_test.go`) exists and is exactly what the acceptance criteria reference. This is a well-scoped, correctly-escalated chore that follows CLAUDE.md's dependency rules to the letter (no speculative major bumps, license check required, ADR-documented pin if rejected) — proceeded with a spec rather than pushing back.
- Kept the issue's own structure (majors needing a decision vs. routine bumps vs. batch-into-one-PR-and-close-superseded acceptance) since it already read like a near-complete spec; reformatted into standard PM sections and made each row a testable checkbox.
- Flagged the redis license check as higher-stakes than the issue's own framing: Redis Ltd's relicensing (RSALv2/SSPLv1, later AGPLv3 as of the Redis 8 announcement) intersects directly with CLAUDE.md's rejected-license list (AGPL is explicitly rejected). Made explicit in acceptance criteria that if the `redis:8-alpine` image or the edition it packages isn't under an approved license, the bump must be rejected and pinned, not merged on the strength of routine bumps riding along.
- Open questions delegated to architect (matching the issue's own two flagged decisions, not resolved here per the "don't guess on locked/strategic decisions" rule): (1) postgres 17→18 pin-and-ignore vs. bump-with-doctor-check, issue recommends pin-and-ignore as default; (2) redis 7→8 changelog-diff-against-adapter-commands plus the license verification outcome; (3) whether each rejected major's ADR note lands as an append to an existing ADR or a new one.
- Added to v1 project board successfully (`gh project item-add 1 --owner VibeWarden` succeeded).

### 2026-09-03 — #1258 npm distribution (`@vibewarden/cli`) spec finalised

- Posted full spec as a PM comment on #1258 (https://github.com/VibeWarden/vibewarden/issues/1258#issuecomment-5526046016).
- Label `status:ready-for-arch` added. No new `epic:*` label applied — checked sibling child story #665 (Homebrew tap, same parent epic #664) and it carries no epic label either, consistent with existing convention that not every epic has a dedicated `epic:xxx` sub-label. Noted (not fixed) that #1258 itself carries the generic `epic` label despite reading as a single scoped story, likely a mislabel — flagged in the spec, left for cleanup.
- Not fully rejected, but did not wave it through blind either: two concrete concerns surfaced and were put in the spec as open questions rather than resolved unilaterally, per the "don't guess on locked/strategic decisions" rule.
  1. **Likely-redundant sibling #536** ("Publish npm/pip thin wrappers that pull the Docker image", `defer:post-stable`, predates the epic #664 audience-fit narrowing on 2026-05-02) covers the same npm-for-vibe-coders ground with a weaker mechanism (Docker-image pull vs. the binary-download-plus-checksum pattern #1258 proposes) and an extra pip target the epic already dropped. It was never closed when #664 was narrowed. Recommended closing #536 as superseded by #1258; did not close it myself since that's outside a single-issue spec's scope.
  2. **Label inconsistency**: #664's body explicitly pairs #1258 and #665 as an equal-priority P0 pair ("promote to P0 alongside #665 Homebrew tap"), but #665 carries `defer:post-stable` and #1258 does not. `docs/faq.md` already describes core features as "stable and tested" pre-1.0, so it's unclear whether that label is simply stale on #665 or whether #1258 was meant to carry it too and got missed. This determines whether the architect should pick #1258 up now or park it — flagged for the user to decide, not assumed.
  Proceeded with the full spec regardless, since the story itself (mechanism, acceptance criteria) is sound and well-scoped whichever way those two questions resolve.
  - Verified before writing the spec that no npm-package scaffolding exists yet anywhere in the repo (`find` for `package.json` outside `examples/`, `.goreleaser.yml` has no npm/homebrew publish block yet) — not already in progress or done.
- Other open questions delegated to architect: exact npm package name/org availability (`@vibewarden/cli` vs. `vibewarden` fallback — needs a live registry check before dev starts), auto-publish mechanism wiring into the existing release pipeline, and custody of the npm publish credential.
- Added to v1 project board successfully (`gh project item-add 1 --owner VibeWarden` succeeded).

### 2026-09-03 — #1306 sidecar container resource limits spec finalised

- Posted full spec as a PM comment on #1306 (https://github.com/VibeWarden/vibewarden/issues/1306#issuecomment-5525171838).
- Label `status:ready-for-arch` added. No `epic:*` label applied — same audit:2026-05-03 batch convention already established for #1311 (medium-priority perf findings from this batch don't carry epic labels; kept `priority:medium` + `audit:2026-05-03` only).
- Verified before writing the spec that the finding is real and current on `main`: neither `internal/config/templates/docker-compose.yml.tmpl` (single-app, `vibewarden:` block lines 71-145) nor `internal/config/templates/sidecar-compose.yml.tmpl` (multi-app) sets `mem_limit`, `cpus`, `pids_limit`, or `deploy.resources.limits` on the `vibewarden` service, or on any other service. Not redundant with #1311 (merged, ADR-110, `server.max_connections`) — that's an application-layer connection cap; this is OS-level container resource limits, a different, complementary layer. Not challenged — proceeded with the spec.
- Core decision: add memory/CPU/PID limits to the `vibewarden` service in both templates, configurable via the existing `server:` block in `vibewarden.yaml` (same block introduced by #1311/ADR-110), following the same "0 = unlimited" convention as `server.max_connections`. Defaults carried from the original finding: memory `512M`, CPU `1.0`, PIDs `200` — architect/dev may adjust with justification.
- Required a behavioral test (start a container from the rendered compose, verify via `docker inspect` that the limit is actually enforced under plain `docker compose up`, non-swarm) rather than a config-shape-only assertion, per the project's standing "behavioral test for silent no-ops" lesson — this matters here because `deploy.resources.limits` is swarm-only under plain Compose and would be a silent no-op if chosen naively.
- Scope: only the `vibewarden` sidecar service. App/kratos/postgres/redis/openbao containers explicitly out of scope.
- Open questions delegated to architect: (1) exact key naming/nesting under `server:` (flat vs. nested `server.resources:`); (2) whether this warrants extending ADR-110 or a new ADR; (3) whether `pids_limit` needs a different default in multi-app mode (one sidecar fanning out to multiple sites) vs. single-app mode.

### 2026-04-20 — #1051 sunset `vibew deploy` spec finalised

- Posted full spec as a PM comment on #1051 (https://github.com/VibeWarden/vibewarden/issues/1051#issuecomment-4285143645).
- Status label set to `status:READY_FOR_ARCH`.
- Inventory: DELETE ~4 900 LOC (`cmd/deploy*.go`, `internal/app/deploy/{service,multiapp,detect,health,image_transfer,openbao,arch,resolve,errors}.go` + tests); KEEP+RENAME `internal/app/deploy/bundle*` → `internal/app/bundle/`; REWRITE `docs/deploy-to-vps.md` (delete), `docs/deploy-reference.md` (rewrite as breaking-change landing), new `docs/guide/bundle-to-vps.md`, plus `README.md`, `llms-full.txt`, `docs/examples/AGENTS-VIBEWARDEN.md`, all `examples/*/AGENTS-VIBEWARDEN.md`.
- Recommendation: one-release stub for `vibew deploy` (hidden, exit 2, fixed deprecation message) before full removal in the following release. Follow-up issue for stub removal is to be filed by the dev when the stub lands.
- Unblocks #1059 (remote-doctor check removal) — kept as a separate PR to contain blast radius.
- ADR guidance: write a new ADR (ADR-086) for this sunset, covering the package rename (satisfying ADR-082 and ADR-085 deferrals) and marking ADR-080/ADR-081 as historical. Do not rewrite merged ADRs.
- Open questions: (1) root-level exit-code wiring — does cobra's `RunE` map to exit 1 by default? If yes, the stub calls `os.Exit(2)` directly. Architect to confirm. (2) Whether the MCP-server tools `vibewarden_prepare_deploy` / `verify_deploy` / `get_deploy_logs` still exist; if yes, file a separate issue — out of scope here.

### 2026-04-23 — #1106 + #1107 hexagonal hygiene spec finalised

- Updated #1106 with full spec absorbing #1107. Both changes ship in one PR on branch `refactor/1106-1107-ports-hygiene`.
- #1106: delete `SessionCheckerToIdentityProvider`, `sessionCheckerAdapter`, `ports.SessionChecker`, and `auth_compat_test.go`. Pre-condition: verify zero production callers before deleting (abort if any found).
- #1107: move `HTTPClient` (`internal/app/upgrade`), `ConfigUpdater` (`internal/app/reload`), `ConfigBuilder` (`internal/app/eject`) to `internal/ports/`. Rename `AdminServerIface` → `AdminServer` (or equivalent idiomatic name, architect decides). Document consumer-side seam exceptions for `StalenessWalker`, `DoctorRunner`, `PostgresProber`.
- Both issues labelled `status:ready-for-arch`. #1107 carries an absorption comment pointing to #1106.
- Open questions: none. Architect decides target file(s) within `internal/ports/` for the 3 moved interfaces and the exact `AdminServerIface` rename.

### 2026-04-23 — #1139 stop generating Dockerfile/.dockerignore spec finalised

- Posted full spec as a PM comment on #1139 (https://github.com/VibeWarden/vibewarden/issues/1139#issuecomment-4322956121).
- Status label set to `status:READY_FOR_ARCH`.
- Decision locked: drop both `Dockerfile` AND `.dockerignore`. Rationale: `.dockerignore` without a `Dockerfile` is an orphaned artifact; same artifact-policy logic.
- Folds in #1151 (drop HEALTHCHECK from Dockerfile contract) — the contract checklist explicitly says "No HEALTHCHECK directive; compose owns it." Mark #1151 as resolved-by when the PR merges.
- Files touched: `init-dockerfile.tmpl` (delete), `init-dockerignore.tmpl` (delete), `init_project.go`, `init_project_test.go`, `init.go`, `init_test.go`, `require_config.go`, `agents-vibewarden.md.tmpl`, `docs/examples/AGENTS-VIBEWARDEN.md`, `docs/getting-started.md`, `README.md`, `llms-full.txt`, `CHANGELOG.md`.
- New negative-assertion test: rendered AGENTS-VIBEWARDEN.md template must NOT contain a code-fenced `FROM ` block. Architect decides which test file.
- `vibew init` stdout gains "Write your Dockerfile (see AGENTS-VIBEWARDEN.md §Dockerfile contract)" as the first "Next steps" line.
- `test/quickstart/test.sh` requires no changes (tests `vibew wrap`, not `vibew init`; no Dockerfile assertions present).
- Open questions: none.

### 2026-04-23 — #1146 bundle staleness content-hash spec finalised

- Posted full spec as a PM comment on #1146 (https://github.com/VibeWarden/vibewarden/issues/1146#issuecomment-4323182386).
- Status label set to `status:ready-for-arch`.
- Core decision: replace mtime-vs-image-Created comparison with a SHA-256 content hash of all inputs the staleness walker already visits. Hash stored at `.vibewarden/.input-digest` (format: `sha256:<hex>`). Written on successful bundle completion only.
- Migration: missing digest file falls back to mtime behavior; corrupt digest file treated as missing (debug log, no error). First post-upgrade bundle writes the file; subsequent runs use content-hash.
- ADR-089 §Freshness invariant must be amended to reflect the content-hash approach.
- `.vibewarden/.input-digest` must be gitignored (automatically or documented — architect decides).
- Open questions: none. Hash algorithm, aggregation order, and exact file placement within `internal/app/bundle/` delegated to architect.

### 2026-09-02 — #1436 stale kratos-db volume / regenerated credentials spec finalised

- Posted full spec as a PM comment on #1436 (https://github.com/VibeWarden/vibewarden/issues/1436#issuecomment-5512976876).
- Labels set: `epic:generated-stack`, `priority:high`, `status:ready-for-arch`.
- Investigated before writing the spec: `internal/app/ops/dev.go`'s `verifySidecar` already exists (added post-#1028/#939-945) and already fails `vibew dev` non-zero when the sidecar never reaches "running" — the original report's "`vibew dev` exits 0" premise no longer reproduces on current `main`. The real, still-open gap: the failure message is generic ("sidecar failed to start ... run vibew logs or vibew doctor") and the sidecar's own logs are empty (it never started), so the user has no path to the actual root cause (`kratos-migrate` retry-looping on `password authentication failed`) without manually running `vibew logs kratos-migrate`.
- Scoped spec to: (1) detect the specific Postgres-auth-failure signature in `kratos-migrate` when `vibew dev`'s post-up check fails, and print a distinct, actionable message naming the root cause + pointing at the *existing* recovery commands (`vibew down -v --yes` then `vibew dev`, or `vibew dev --rebuild --volumes` — no new flag needed); (2) an equivalent `vibew doctor` check for the case where the user returns to an already-stuck stack; (3) explicitly no auto-remediation (never runs a destructive command automatically); (4) explicitly no `vibew status` change.
- **Escalated, not resolved in this spec**: traced the actual root cause further than the issue's own framing. ADR-009 (locked, Accepted, #283) mandates fresh random credentials on *every* `vibewarden generate` run, and `vibew dev` calls the generator unconditionally on every invocation (confirmed in `internal/app/generate/service.go` and `internal/cli/cmd/dev.go`). Since the `kratos-db-data` named volume persists across runs (`restart: unless-stopped`) but Postgres only honors `POSTGRES_PASSWORD` on first init of an empty volume, this bug is reproducible on literally the **second** `vibew dev` run of any Kratos-mode project once the volume has data — not only on "fresh clone" as the issue describes. Fixing that properly (idempotent per-project credentials, regenerated only on explicit opt-in) would contradict ADR-009 as currently worded, so per PM rules I did not spec it. Flagged as Open Question #1 in the issue comment: recommend a separate follow-up story + new ADR (ADR-009 stays historical) for the architect/user to decide on.
- Other open questions delegated to architect: `vibew doctor` check severity (FAIL vs WARN, ADR-084 precedent suggests FAIL); exact detection mechanism (PS state vs log grep vs both, and where it composes with `verifySidecar`).
- Not challenged/rejected: the issue is real and, per the investigation above, more severe than originally reported (near-guaranteed on 2nd run, not an edge case) — proceeded with a spec rather than pushing back.
- `gh project item-add 1 --owner VibeWarden` failed with `missing required scopes [read:project]` on the current `gh` auth token — issue was **not** added to the v1 project board. User needs to run `gh auth refresh -s read:project` (or add manually) and add #1436 to project #1.

### 2026-09-02 — #1458 bundle dir perms + Docker stderr sanitization spec finalised

- Posted full spec as a PM comment on #1458 (https://github.com/VibeWarden/vibewarden/issues/1458#issuecomment-5516114942).
- Labels: `priority:medium`, `security` (already present), `status:ready-for-arch` added. No `epic:*` label applied — checked sibling low-priority findings from the same 2026-05-03 audit batch (#1310–#1314 etc.) and none carry an epic label; `epic:v0.19-stabilization` is reserved for critical/high-priority audit findings only, consistent with existing repo convention.
- #1458 consolidates #1283 (bundle output dir `0o750` → should be `0o700`) and #1285 (Docker stderr echoed to terminal unsanitized, ANSI/control-char escape injection risk); both predecessor issues were closed `not planned` in favor of this single issue on the same day.
- Verified before writing the spec that neither underlying bug is already fixed on `main`: `0o750` is still live at `internal/cli/cmd/bundle.go:294`, `internal/app/bundle/bundle.go:537`, `internal/app/bundle/input_digest.go:121,377`; `renderDockerUnavailable` in `internal/cli/cmd/docker_error_render.go` still re-prints `de.Stderr` with no sanitization. Not redundant, not already resolved — proceeded with the spec.
- Scoped Part A (dir perms) to exactly the 4 call sites named in #1283's original finding — deliberately excluded `internal/adapters/credentials/store.go`'s identical `permDir = 0o750` (appears to be dead/unwired production code, only referenced by its own test) and `internal/app/scaffold/init_project.go`'s `0o750` (different directory class — general project scaffold, not credentials/bundle). Flagged the dead-code question as Open Question 1 for a possible separate follow-up issue rather than folding it in here.
- Scoped Part B (ANSI/control-char stripping) to `renderDockerUnavailable`, which already fans out to all three current call sites (`bundle.go`, `dev.go`, `logs.go`) — one fix point covers all three.
- Required both parts' tests to assert actual on-disk directory mode / actual bytes written, not just that a function was called — per the artifact-testing rule already stated in the issue body itself, and consistent with the project's standing "behavioral test for silent no-ops" lesson.
- Open questions delegated to architect: (1) whether `internal/adapters/credentials/store.go`'s dead-code status merits its own tech-debt issue or deletion — recommend a separate issue, out of scope here; (2) whether ANSI-stripping should be a shared reusable sanitizer or stay local to `docker_error_render.go` — no other call site currently needs it, so no strong signal either way.
- `gh project item-add 1 --owner VibeWarden` failed again with the same `missing required scopes [read:project]` token issue as #1436 — #1458 was **not** added to the v1 project board. Same remediation needed: `gh auth refresh -s read:project`, then add #1436 and #1458 to project #1.

### 2026-09-03 — #1311 Caddy connection limit spec finalised

- Posted full spec as a PM comment on #1311 (https://github.com/VibeWarden/vibewarden/issues/1311#issuecomment-5524612282).
- Label `status:ready-for-arch` added. No `epic:*` or `security` label applied — checked sibling `audit:2026-05-03` perf findings (#1306, #1309, #1312, #1313, #1314): none carry an epic label, kept `priority:medium` only, consistent with existing convention.
- Verified before writing the spec that the finding is real and current on `main`: `buildMainServer` in `internal/adapters/caddy/config_build.go` sets only `listen`/`routes` plus timeouts, no `max_connections` or listener-limit field anywhere in the Caddy adapter or `vibewarden.reference.yaml`. Not redundant with #1306 (container-level `mem_limit`/`cpus`/`pids_limit` — a different, complementary layer). Not challenged — proceeded with the spec.
- Core decision: new `server.max_connections` config key (integer) under the existing `server:` block, default `1000`, `0` = explicit unlimited opt-out, negative values rejected by `vibew validate`/`vibew bundle`. Once the cap is hit, new connections must be refused (not accepted then hung); existing connections unaffected.
- Required a behavioral test (dial past the cap, observe refusal) rather than a config-shape-only assertion, per the project's standing "behavioral test for silent no-ops" lesson.
- Open questions delegated to architect: (1) whether `vibew doctor` should warn on unusually low/high `max_connections` relative to system ulimit — optional fast-follow, not required for v1; (2) exact Caddy-level enforcement mechanism (`max_connections` field vs. `limit` listener wrapper); (3) the `1000` default is carried from the original finding as a reasonable single-VPS ceiling — architect/dev may adjust with justification, called out in the PR description.
- Successfully added #1311 to the v1 project board (`gh project item-add 1 --owner VibeWarden` succeeded this time — prior `missing required scopes [read:project]` failure on #1436/#1458 appears resolved).
