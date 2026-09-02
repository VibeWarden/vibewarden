# Architectural Decision Records

ADRs have been split into individual files for easier navigation.

See **[decisions/README.md](decisions/README.md)** for the full index.

Each ADR is a standalone file at `decisions/adr-NNN-title.md`.

## PM Log

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
