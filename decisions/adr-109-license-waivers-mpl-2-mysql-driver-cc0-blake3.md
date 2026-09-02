# ADR-109: License waivers for two transitive Caddy dependencies — MPL-2.0 (go-sql-driver/mysql) and CC0-1.0 (zeebo/blake3)

**Date**: 2026-09-03
**Issue**: [#1347](https://github.com/vibewarden/vibewarden/issues/1347), [#1292](https://github.com/vibewarden/vibewarden/issues/1292), [#1293](https://github.com/vibewarden/vibewarden/issues/1293)
**Status**: Accepted

---

## Context

`CLAUDE.md` defines a strict approved-license set for dependencies: Apache 2.0, MIT,
BSD-2, BSD-3. Everything else is rejected or requires an explicit waiver. The
2026-05-03 dependency audit found two transitive dependencies that fall outside that
set. Both arrive through the embedded Caddy module graph; neither is imported by
VibeWarden code.

### 1. `github.com/go-sql-driver/mysql` — MPL-2.0 (#1292)

```
github.com/vibewarden/vibewarden/internal/adapters/caddy
  → github.com/caddyserver/caddy/v2/modules/standard
    → github.com/caddyserver/caddy/v2/modules/caddypki/acmeserver
      → github.com/smallstep/nosql
        → github.com/smallstep/nosql/mysql
          → github.com/go-sql-driver/mysql   [MPL-2.0, // indirect]
```

**Remediation via build tag is not viable.** `smallstep/nosql` ships a `nomysql` build
tag (`//go:build nomysql` in `mysql/nomysql.go`), but the mysql package is imported
inside Caddy's module, not inside VibeWarden's. Passing `-tags nomysql` to our
`go build` does not propagate into Caddy's transitive closure — the tag is inert from
our build. Dropping the driver would require forking or replacing
`caddy/modules/caddypki/acmeserver`.

**Copyleft exposure is negligible.** MPL-2.0 is file-level copyleft: the source-release
obligation triggers only on distributing *modified* source files of the MPL-licensed
component. VibeWarden does not modify the driver, distributes a compiled binary and a
Docker image only, and never reaches the driver's code path at runtime (VibeWarden is
Postgres-only; the ACME-server PKI backend that would select MySQL is not enabled).

### 2. `github.com/zeebo/blake3` — CC0-1.0 (#1293)

```
github.com/vibewarden/vibewarden/internal/adapters/caddy
  → github.com/caddyserver/caddy/v2
    → github.com/caddyserver/certmagic
      → github.com/zeebo/blake3   [CC0-1.0, // indirect]
```

CC0-1.0 is a public-domain dedication. It is strictly more permissive than MIT: no
attribution requirement, no conditions of any kind. It was flagged only because the
approved list is an allow-list and CC0 is not enumerated on it, not because it carries
any obligation.

### Why this ADR exists at number 109

The MPL waiver was originally decided in #1292 and assigned **ADR-104**, but the file
was written to a working tree and never committed. ADR-104 was later legitimately
reused for the OpenBao prod-init decision, so #1292's closing comment now cites a
number belonging to an unrelated ADR. ADR-105–ADR-108 have since been taken. This ADR
reconstructs the decision at the next free number and consolidates #1293's waiver into
the same record, since both are the same category of decision about the same dependency
graph.

---

## Decision

**Waiver granted** for both dependencies. `CLAUDE.md`'s approved-license list is
**not** modified — this ADR is the canonical exception record, and the allow-list stays
strict.

### `go-sql-driver/mysql` (MPL-2.0) — conditional waiver

All five conditions must hold. If any stops holding, the waiver lapses and the
dependency must be re-evaluated in a new ADR:

1. The dependency appears as `// indirect` only, never imported by VibeWarden code.
2. The driver's source is not modified.
3. Distribution is binary / Docker-image only, never source distribution of the driver.
4. The driver's code path is never activated at runtime.
5. Upstream remediation stays tracked: an issue with `smallstep/nosql` (or Caddy)
   requesting that the MySQL driver be excludable in Caddy's PKI-module context.

### `zeebo/blake3` (CC0-1.0) — unconditional waiver

CC0-1.0 and other public-domain dedications impose zero restrictions and are acceptable
for transitive dependencies. No conditions attach.

### Scope

These waivers are **per-dependency, not per-license**. A new MPL-2.0 dependency does not
inherit the mysql-driver waiver; it needs its own review. A new CC0 dependency may cite
the reasoning here but still needs an index entry so the exception set stays auditable.

---

## Consequences

- No code change. Both dependencies stay in `go.mod` as `// indirect`.
- The audit trail is complete: every dependency outside the approved set now has a
  committed ADR, so a license sweep can be reconciled against `decisions/README.md`.
- Condition 4 (dormant code path) couples the mysql waiver to Caddy's PKI/ACME-server
  configuration. If VibeWarden ever enables the embedded ACME server with a MySQL
  backend, this ADR must be revisited before shipping.
- #1292's closing comment cites ADR-104, which now points at the OpenBao prod-init
  decision. That comment is corrected to reference ADR-109; the stale citation is
  recorded here so future readers of #1292 are not misled.
