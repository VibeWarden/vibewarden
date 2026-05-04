# ADR-070: Deploy Detection

**Date**: 2026-04-15
**Issue**: #872
**Status**: Obsolete — the `Detect` / `BootstrapSidecar` / `DeployMultiApp` flow was sunset alongside the rest of `vibew deploy` by ADR-086 (#1138). Kept as historical record.

### Context

`vibew deploy` must support both first-time deployment (bootstrap a new
sidecar) and adding a site to an existing sidecar. The CLI needs to
detect which case applies on the remote host before choosing a deploy
strategy. Additionally, multi-app deploys require a specific directory
layout, shared Docker network, and compose templates for both the
sidecar and per-app containers.

### Decision

1. **Deploy mode detection.** `Detect` in `internal/app/deploy/detect.go`
   runs `test -f ~/vibewarden/.sidecar/global.yaml` via SSH. File
   present means `ModeAddSite`; exit status 1 means `ModeFreshInstall`;
   any other error is propagated. `IsMultiApp` similarly tests for the
   `sites/` directory to support status/logs commands.

2. **CLI branching (4-cell matrix).** In `deploy.go` RunE:
   - FreshInstall + has domain: `BootstrapSidecar` (multi-app bootstrap)
   - FreshInstall + no domain: legacy `Deploy` (backward compatible)
   - AddSite + has domain: `DeployMultiApp`
   - AddSite + no domain: error ("cannot add a site without a TLS domain")

3. **BootstrapSidecar** (`internal/app/deploy/multiapp.go`). Creates the
   full directory layout (`~/vibewarden/.sidecar/`, `~/vibewarden/sites/<project>/`),
   creates the shared `vibewarden-multiapp` Docker network, writes
   `global.yaml`, renders and writes the sidecar compose file, deploys
   the first site, starts the sidecar, and runs a health check.

4. **DeployMultiApp.** Writes per-app config and compose file to
   `~/vibewarden/sites/<project>/`, starts the app container, then
   restarts the sidecar with `docker compose restart vibewarden` to
   pick up the new site. The file watcher (#873) will later eliminate
   the restart requirement.

5. **Shared Docker network.** `vibewarden-multiapp` network is created
   by the sidecar compose, joined as external by per-app compose files.
   Container naming uses `vibewarden-<project>-app` for collision
   avoidance.

6. **Compose templates.** `sidecar-compose.yml.tmpl` and
   `app-compose.yml.tmpl` in `internal/config/templates/` render the
   sidecar and per-app docker-compose files respectively.

### Consequences

#### Positive

- Existing single-app deploys (no TLS domain) work unchanged.
- Detection is a single SSH round-trip (fast, no state to manage).
- Directory layout cleanly separates sidecar state from per-app state.
- Shared network avoids port mapping complexity between containers.

#### Negative

- `ModeAddSite` deploys require a sidecar restart, causing brief
  downtime. Resolved by the file watcher in ADR-071.
- Detection relies on a marker file (`global.yaml`); manual deletion
  would cause a re-bootstrap. Acceptable for the target audience.
- `test -f` exit code parsing is fragile across SSH implementations,
  but the pattern is well-established and tested.
