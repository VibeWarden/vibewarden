# ADR-091: Ports hygiene — delete dead SessionChecker adapter; move three outbound ports to `internal/ports/`; rename `AdminServerIface`

**Date**: 2026-04-23
**Issues**: #1106, #1107
**Status**: Accepted

## Context

Two hexagonal-architecture hygiene defects were found during audit:

1. **Dead code (#1106).** `middleware.SessionCheckerToIdentityProvider`,
   `middleware.sessionCheckerAdapter`, and `ports.SessionChecker` are deprecated.
   The only references outside their own definitions are in
   `internal/middleware/auth_compat_test.go` (which exists solely to cover the
   deprecated bridge). No production caller remains.
   `internal/adapters/kratos/adapter.go` still declares it *implements*
   `ports.SessionChecker` for backward-compat, but that reference will naturally
   fall to a comment-only mention once the interface is removed — acceptable in
   this PR.

2. **Port placement (#1107).** Three outbound port interfaces live inside
   `internal/app/` instead of the canonical `internal/ports/` location,
   violating the architectural invariant enforced by
   `test/architecture/ports_purity_test.go`:
   - `upgrade.HTTPClient` — `internal/app/upgrade/service.go:117`
   - `reload.ConfigUpdater` — `internal/app/reload/service.go:179`
   - `eject.ConfigBuilder` — `internal/app/eject/eject.go:33`

   All three are true outbound ports: the application layer *calls* them; an
   adapter implements them (e.g. `internal/adapters/caddy/eject_builder.go`
   implements `eject.ConfigBuilder`; `*http.Client` satisfies
   `upgrade.HTTPClient`).

3. **Naming (#1107).** `internal/plugins/usermgmt/plugin.go` declares
   `AdminServerIface`. The `Iface` suffix is non-idiomatic Go.

4. **Consumer-side seam exceptions.** Three narrow seams remain exempt from the
   move because they are consumer-side test seams, not outbound ports that
   cross a layer boundary:
   - `bundle.StalenessWalker` (`internal/app/bundle/service.go`)
   - `mcp.DoctorRunner` (`internal/mcp/tools.go`)
   - `usermgmt.PostgresProber` (`internal/plugins/usermgmt/plugin.go`)

## Decision

### #1106 — Delete the dead SessionChecker adapter

Verified grep results (with `.claude/worktrees` excluded):

```
internal/middleware/auth.go          (adapter + bridge function — delete)
internal/middleware/auth_compat_test.go (compat test — delete whole file)
internal/adapters/kratos/adapter.go  (only in comments "implements ports.SessionChecker")
decisions/…                          (historical ADRs — do not touch)
```

No production caller exists. Safe to delete:

- Delete in `internal/middleware/auth.go`:
  - `sessionCheckerAdapter` struct (with `checker` and `cookieName` fields).
  - `sessionCheckerAdapter.Name()` method.
  - `sessionCheckerAdapter.Authenticate(ctx, r)` method.
  - `SessionCheckerToIdentityProvider(checker, cookieName)` function.
  - Any now-orphaned imports (notably the `//nolint:staticcheck` line disappears
    with the struct field).
- Delete `internal/ports/auth.go`'s `SessionChecker` interface and its
  `Deprecated:` godoc. Any other types in that file stay.
- Delete the entire file `internal/middleware/auth_compat_test.go`.
- In `internal/adapters/kratos/adapter.go`, update the two comments that say
  `implements ports.SessionChecker and ports.IdentityProvider` / `implements
  ports.SessionChecker (deprecated)` so they drop the `ports.SessionChecker`
  reference (the method `CheckSession` stays; its godoc becomes "CheckSession
  performs a Kratos `/sessions/whoami` lookup. // TODO(#1106): remove once no
  external caller depends on it.").
- **Do not** delete `CheckSession` itself — out of scope per PM spec.

### #1107 — Move three outbound ports to `internal/ports/`

Each interface moves to its own file in `internal/ports/`. Justification: the
existing `internal/ports/` directory follows one-concept-per-file (e.g.
`reload.go`, `proxy.go`, `upgrade` would collide — no file yet). Dedicated
files minimize merge friction.

| Interface | New file | Old file |
|---|---|---|
| `HTTPClient` | `internal/ports/upgrade_http_client.go` | remove from `internal/app/upgrade/service.go` |
| `ConfigUpdater` | `internal/ports/config_updater.go` | remove from `internal/app/reload/service.go` |
| `ConfigBuilder` | `internal/ports/config_builder.go` | remove from `internal/app/eject/eject.go` |

**Why `upgrade_http_client.go` (not `http_client.go`).** The interface is
specific to the upgrade use case (`Do(req) (resp, err)` narrowed to the upgrade
flow). A generic `http_client.go` filename would imply a shared HTTP port and
invite future misuse as a catch-all. The `upgrade_` prefix documents scope.

### Rename `AdminServerIface` → `AdminServerAPI`

Rationale: `internal/adapters/http/admin_server.go` already exports a concrete
`AdminServer` struct, and `internal/plugins/usermgmt/plugin.go` imports that
package as `httpadapter`. A plain `AdminServer` interface in the same
`usermgmt` package would force readers to juggle `usermgmt.AdminServer` vs
`httpadapter.AdminServer` — high cognitive cost. `AdminServerAPI` names the
abstraction for what it is: the API surface the plugin requires from *any*
admin-server implementation.

The interface stays in `internal/plugins/usermgmt/plugin.go` (it is consumed
only by this package — same pattern as `PostgresProber`; see "Consumer-side
seams" below).

### Consumer-side seam exemption documentation

Add a canonical block comment directly **above each seam interface
declaration** (not a package-level doc.go) to make the rationale greppable and
co-located with the exempt symbol:

```go
// <Name> is a consumer-side test seam: this interface is defined here because
// it is consumed only by this package and its tests; it is not an outbound
// port that crosses a layer boundary.
//
// Do not move to internal/ports/.
type <Name> interface { … }
```

Apply to:

1. `internal/app/bundle/service.go` — above `type StalenessWalker interface`.
2. `internal/mcp/tools.go` — above `type DoctorRunner interface`.
3. `internal/plugins/usermgmt/plugin.go` — above `type PostgresProber interface`.

### File layout

**Deletions:**
- `internal/middleware/auth_compat_test.go` (whole file)

**New files:**
- `internal/ports/upgrade_http_client.go`
- `internal/ports/config_updater.go`
- `internal/ports/config_builder.go`

**Modifications:**
- `internal/middleware/auth.go` — remove adapter/bridge symbols.
- `internal/ports/auth.go` — remove `SessionChecker` interface.
- `internal/adapters/kratos/adapter.go` — update two comments, add `TODO(#1106)`.
- `internal/app/upgrade/service.go` — remove `HTTPClient` decl; change the
  struct field type and `NewService` parameter to `ports.HTTPClient`; add
  `ports` import if missing.
- `internal/app/upgrade/service_test.go` — change any local `upgrade.HTTPClient`
  references (fakes satisfy the interface structurally; update doc comments).
- `internal/app/reload/service.go` — remove `ConfigUpdater` decl; type-asserts
  `UpdateConfig` method set change to `ports.ConfigUpdater`.
- `internal/app/reload/multisite_service.go` — update the type assertion/usage
  site referencing `ConfigUpdater` (confirm during implementation).
- `internal/app/eject/eject.go` — remove `ConfigBuilder` decl; update
  `Service`/`NewService` to reference `ports.ConfigBuilder`.
- `internal/app/eject/eject_test.go` — update doc comment on `fakeBuilder`.
- `internal/adapters/caddy/eject_builder.go` — update comments mentioning
  `eject.ConfigBuilder` to `ports.ConfigBuilder`.
- `internal/plugins/usermgmt/plugin.go` — rename `AdminServerIface` →
  `AdminServerAPI`; add seam comment above `PostgresProber`.
- `internal/plugins/usermgmt/plugin_test.go` — update all 6
  `usermgmt.AdminServerIface` references to `usermgmt.AdminServerAPI`.
- `internal/app/bundle/service.go` — add seam comment above `StalenessWalker`.
- `internal/mcp/tools.go` — add seam comment above `DoctorRunner`.

**Expected final counts after PR:**
- Files deleted: 1 (`auth_compat_test.go`)
- Files added: 3 (three new ports files)
- Files modified: ~12 (exact list above — plus any test file that locally
  referenced the relocated interfaces via their old package-qualified names)

### Sequence (implementation order)

1. Create the three new port files in `internal/ports/`.
2. Update `internal/app/upgrade/*`, `internal/app/reload/*`,
   `internal/app/eject/*` and their adapter(s) to import `ports` and reference
   the relocated interfaces; delete the in-app declarations.
3. `go build ./...` — confirm clean.
4. Rename `AdminServerIface` → `AdminServerAPI` (prefer IDE-safe rename or
   `goimports`-aware sed across `plugin.go` and `plugin_test.go`).
5. Delete `internal/middleware/auth_compat_test.go`.
6. Delete bridge/adapter symbols from `internal/middleware/auth.go`.
7. Delete `SessionChecker` interface from `internal/ports/auth.go`.
8. Update comments in `internal/adapters/kratos/adapter.go`.
9. Add seam exemption comments to the three consumer-side seam interfaces.
10. `go build ./... && go test ./...` — full gate.
11. `make check` — golangci-lint and architecture invariants.
12. Verify `TestPortsPackage_OnlyImportsStdlibAndDomain` and
    `TestAppPackages_NoAdapterImports` both pass.

### Error cases

- **Kratos adapter no longer compiles** because of orphaned comment tokens or
  imports. Mitigation: remove only comment text, not code; re-run build after
  each delete.
- **Hidden downstream type assertion** of
  `someValue.(reload.ConfigUpdater)` or `eject.ConfigBuilder` via the old
  package path. Mitigation: the dev agent must grep for each old
  package-qualified name after the move and before committing.
- **Test-only callers** of `SessionCheckerToIdentityProvider` outside
  `auth_compat_test.go`. Pre-condition grep already confirmed none exist.

### Test strategy

- **No new tests required.** This is a pure refactor. Correctness is enforced
  by:
  - `go build ./...` (compilation).
  - `go test ./...` (existing test suite continues to pass).
  - `test/architecture/ports_purity_test.go` — the architecture invariant.
  - `golangci-lint` via `make check`.
- **Tests to delete:** `internal/middleware/auth_compat_test.go` (entire file).
- **Tests to update only for rename:** `internal/plugins/usermgmt/plugin_test.go`
  (mechanical `AdminServerIface` → `AdminServerAPI`).

### New dependencies

None.

## Consequences

**Positive:**
- Eliminates dead code (~30 lines in `auth.go` plus the 140-line
  `auth_compat_test.go`).
- Restores the invariant that all outbound ports live in `internal/ports/`.
- Removes the non-idiomatic `Iface` suffix.
- Documents *why* three consumer-side seams are exempt, so future audits do
  not flag them again.

**Neutral:**
- The Kratos adapter's `CheckSession` method is now orphaned in principle but
  retained out of scope per the PM spec. Tracked inline with a
  `TODO(#1106)` marker.

**Trade-offs:**
- Splitting the three moved ports into three new files (vs. appending to
  existing files like `reload.go`) trades a slightly larger file count for
  merge-safety and single-responsibility per file. This is consistent with the
  existing `internal/ports/` convention (e.g. `logger.go`, `metrics.go`,
  `reload.go`, `proxy.go` are each narrowly scoped).
