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
