# ADR-105: Remove unused grafana plugin-name constant; grafana is a compose service

**Date**: 2026-06-05
**Issue**: [#1371](https://github.com/vibewarden/vibewarden/issues/1371)
**Status**: Accepted

---

## Context

`internal/domain/plugin/names.go` defined a `NameGrafana = "grafana"` constant
alongside the other well-known plugin-name constants (`NameTLS`, `NameUserManagement`,
`NameRateLimiting`, `NameFleet`).

Grafana is not a plugin. It is a Docker Compose service in the `observability` profile,
gated by `observability.enabled` in `vibewarden.yaml`. It has no plugin lifecycle, no
`Plugin` interface implementation, and no entry in the plugin registry
(`internal/plugins/builtin.go`). The constant was unused in all production code — the
only reference was in the test file that asserted the constant was non-empty.

Simultaneously, `CLAUDE.md`'s "Plugin model" section showed a fictional `plugins:`
wrapper block with a `grafana: enabled: false` entry. The strict loader
(`internal/config/strict.go`) rejects any unknown top-level key, so this example would
fail validation if copied verbatim. It also showed a `fleet:` entry as a loadable
config plugin, which is not implemented. This phantom documentation taught LLM agents
and human contributors an incorrect model of how plugin activation works.

The real activation surface is flat top-level keys: `tls:`, `admin:` + `kratos:` +
`database:` for user management, `rate_limit:`, `observability:`, etc. Verified against
`internal/config/config.go:14-143` and `vibewarden.reference.yaml`.

### What about `NameFleet`?

`NameFleet` is also unused in production code but is kept. Fleet is a locked decision
("fleet is the bridge to the Pro tier" — CLAUDE.md Locked decisions table). It is a
deliberate roadmap placeholder and must not be removed without a new ADR that explicitly
revisits that locked decision. The constant is annotated as reserved/roadmap.

---

## Decision

1. **Remove `NameGrafana`** from `internal/domain/plugin/names.go`. Grafana is delivered
   as a compose service, never as a plugin. A `plugin.Name` constant for a non-plugin
   entity is a domain-model lie that causes downstream drift.

2. **Keep `NameFleet`**, annotated with a comment marking it as a reserved roadmap
   placeholder per the locked decision in CLAUDE.md.

3. **Update the package godoc** in `names.go` to describe the real flat top-level key
   activation model instead of the phantom `plugins:` wrapper.

4. **Update `CLAUDE.md` "Plugin model" section** to show the real flat-key activation
   pattern, cross-checked against `vibewarden.reference.yaml`. The "Locked decisions"
   table is not modified.

5. **Update `internal/plugins/usermgmt/config.go`** godoc to name the real source keys
   (`admin.enabled`, `admin.token`, `kratos.admin_url`, `database.url`) instead of the
   phantom `plugins.user-management` section.

6. **Drop `plugin.NameGrafana` from `names_test.go`** so the test compiles and passes.

---

## Consequences

- The domain model accurately reflects reality: grafana is a compose service, not a plugin.
- LLM agents reading `CLAUDE.md` or `names.go` will no longer be taught a config
  shape that fails strict validation.
- No production behaviour changes — the constant was never loaded or evaluated at runtime.
- `NameFleet` remains as a reserved placeholder; its removal is deferred until the Pro-tier
  fleet roadmap decision is revisited.
