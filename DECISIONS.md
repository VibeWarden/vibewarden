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
