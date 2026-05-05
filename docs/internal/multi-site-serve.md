# Multi-Site Serve Wiring — Internal Reference

> This file was relocated from `decisions/adr-072-cli-ux-wiring-for-multi-app.md`
> on 2026-05-04 as part of the ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`).
> The stub at that path remains stable; existing PR / commit references continue to resolve.

## From ADR-072 — CLI UX Wiring for Multi-App

**Date**: 2026-04-15
**Issue**: #874
**Original status**: Accepted (implemented in PR #879)

## Overview

ADR-072 wired the domain model (ADR-068), Caddy routes (ADR-069), and directory watcher
(ADR-071) into the CLI and serve entry point. The deploy-related branching from ADR-070
(`Detect()` / `BootstrapSidecar` / `DeployMultiApp`) was sunset by ADR-086. Only the
serve-side wiring described below remains active.

## Serve-Side Wiring

#### Multi-site serve wiring (`cmd/vibewarden/wiring_serve_multisite.go`)

`isMultiSiteDir(dir)` checks for a `sites/` subdirectory with at least one child
directory. `runServeMultiSite` orchestrates the full startup sequence:

1. Load global config from `global.yaml`
2. Load all per-site configs via `sites.LoadSites`
3. Populate `SiteRegistry`, validate domains
4. Create `NewMultiSiteAdapter` for Caddy
5. Start `SiteWatcher` for hot-reload
6. Create `MultiSiteService` event loop
7. Run proxy until shutdown signal

#### Serve detection

`wiring_serve.go` calls `isMultiSiteDir` and delegates to `runServeMultiSite` when true,
preserving the single-site path for backward compatibility. No config flag needed —
detection is purely filesystem-based.

#### Composition root placement

All adapter construction and wiring lives in `cmd/vibewarden/` (consistent with ADR-067),
not in `internal/app/`.

#### `--app` flag (status and logs)

`vibew deploy status` and `vibew deploy logs` gained an `--app` flag to target a specific
site in multi-app mode. With no `--app`, status shows all sites plus sidecar; logs show
sidecar logs. (Deploy commands were sunset by ADR-086 but the flag design is recorded here.)

## Behavior Summary

- `vibew serve` auto-detects mode from directory layout; no config flag needed.
- Backward compatible: existing single-app workflows are unchanged.
- `isMultiSiteDir` is a single `ReadDir` call on every serve startup — negligible cost.
