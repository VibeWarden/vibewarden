# ADR-072: CLI UX Wiring for Multi-App

**Date**: 2026-04-15
**Issue**: #874
**Status**: Accepted (implemented in PR #879)

### Context

With the domain model (ADR-068), Caddy routes (ADR-069), deploy
orchestration (ADR-070), and directory watcher (ADR-071) in place, the
final piece is wiring everything into the CLI and serve entry point so
that `vibew deploy` automatically selects the right strategy and
`vibew serve` starts in multi-site mode when it detects the
`sites/` directory layout.

### Decision

1. **Deploy RunE branching** (`internal/cli/cmd/deploy.go`). The deploy
   command calls `Detect()` then branches on a 4-cell matrix:
   - `ModeFreshInstall` + `tls.domain` set: `BootstrapSidecar`
   - `ModeFreshInstall` + no domain: legacy single-app `Deploy`
   - `ModeAddSite` + domain: `DeployMultiApp`
   - `ModeAddSite` + no domain: error with actionable message

2. **`--app` flag on status and logs** (`deploy.go`). Both
   `vibew deploy status` and `vibew deploy logs` gain an `--app` flag
   to target a specific site in multi-app mode. Without `--app`,
   status shows all sites plus sidecar; logs show sidecar logs.
   Auto-detection via `IsMultiApp` SSH check.

3. **Multi-site serve wiring** (`cmd/vibewarden/wiring_serve_multisite.go`).
   `isMultiSiteDir(dir)` checks for a `sites/` subdirectory with at
   least one child directory. `runServeMultiSite` orchestrates the full
   startup sequence:
   - Load global config from `global.yaml`
   - Load all per-site configs via `sites.LoadSites`
   - Populate `SiteRegistry`, validate domains
   - Create `NewMultiSiteAdapter` for Caddy
   - Start `SiteWatcher` for hot-reload
   - Create `MultiSiteService` event loop
   - Run proxy until shutdown signal

4. **Serve detection.** `wiring_serve.go` calls `isMultiSiteDir` and
   delegates to `runServeMultiSite` when true, preserving the
   single-site path for backward compatibility.

5. **Composition root placement.** All adapter construction and wiring
   lives in `cmd/vibewarden/` (consistent with ADR-067), not in
   `internal/app/`.

### Consequences

#### Positive

- Single `vibew deploy` command handles both single-app and multi-app
  scenarios transparently.
- `vibew serve` auto-detects mode from directory layout; no config
  flag needed.
- `--app` flag provides targeted observability in multi-app deployments.
- Backward compatible: existing single-app workflows are unchanged.

#### Negative

- Deploy RunE has grown in complexity with the 4-cell branching matrix.
  Acceptable because the branching is well-tested and each path
  delegates to a focused service method.
- `isMultiSiteDir` is a filesystem check on every serve startup; the
  cost is negligible (single `ReadDir` call).
