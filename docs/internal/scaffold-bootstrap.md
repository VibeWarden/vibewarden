# Scaffold Bootstrap — Internal Design Reference

> This file was relocated from `decisions/adr-003-project-scaffold-technical-design.md` on
> 2026-05-04 as part of the ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`).
> The stub at that path remains stable; existing PR / commit references continue to resolve.

## From ADR-003 — Project Scaffold Technical Design

**Date**: 2026-03-20
**Issue**: #1
**Original status**: READY_FOR_DEV

### Context

This is the foundational epic. Before any business logic can be implemented, we need:
- Go module initialized with correct module path
- Directory structure matching the hexagonal architecture from CLAUDE.md
- Development tooling (Makefile, linting, CI)
- Local dev environment (Docker Compose with Postgres, Kratos)
- Configuration loading infrastructure

All subsequent epics depend on this scaffold being complete and correct.

### Decision

Implement the project scaffold with the following specifications:

#### Go Module

- Module path: `github.com/vibewarden/vibewarden`
- Minimum Go version: 1.26 (specified in go.mod)
- Use latest stable Go (1.26.1) per project policy

#### Dependencies (all licenses verified)

| Dependency | Version | License | Purpose |
|------------|---------|---------|---------|
| github.com/spf13/cobra | latest | Apache 2.0 | CLI framework (locked decision L-06) |
| github.com/spf13/viper | latest | MIT | Config loading (YAML + env vars) |

Note: golangci-lint (GPL-3.0) is used as a development tool only, not linked into the binary.
This is standard practice and does not trigger copyleft requirements.

### File Layout

The dev agent must create exactly this structure (as of the initial bootstrap):

```
vibewarden/
├── .github/
│   └── workflows/
│       └── ci.yml
├── .claude/
│   ├── agents/
│   │   └── .gitkeep
│   └── decisions.md
├── cmd/
│   └── vibewarden/
│       └── main.go
├── internal/
│   ├── domain/
│   ├── ports/
│   ├── adapters/
│   │   ├── caddy/
│   │   ├── kratos/
│   │   ├── postgres/
│   │   └── log/
│   ├── app/
│   ├── config/
│   │   └── config.go
│   └── plugins/
├── migrations/
├── dev/
│   └── kratos/
│       ├── kratos.yml
│       └── identity.schema.json
├── .gitignore
├── .golangci.yml
├── docker-compose.yml
├── go.mod
├── go.sum
├── Makefile
├── vibewarden.example.yaml
├── CLAUDE.md
└── LICENSE
```

### Makefile Specification

The Makefile established in this bootstrap has evolved significantly. See the current
`Makefile` at the repo root for the live specification. The original targets were:
`build`, `test`, `lint`, `run`, `docker-up`, `docker-down`, `clean`.

### Consequences

**Positive:**
- Clean separation of concerns from day one
- Local dev environment ready with single `docker compose up`
- CI pipeline catches issues before merge

**Negative:**
- Many placeholder `.gitkeep` files (minor clutter)
- Config struct has fields for features not yet implemented

**Follow-up work (now implemented):**
- Caddy adapter (`internal/adapters/caddy/`)
- Structured log schema (ADR-015)
- Full CLI with `serve` subcommand
