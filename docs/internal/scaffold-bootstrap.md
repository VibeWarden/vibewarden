# Scaffold Bootstrap — Internal Design Reference

> This file was relocated from `decisions/adr-003-project-scaffold-technical-design.md` on
> 2026-05-04 as part of the ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`).
> The stub at that path remains stable; existing PR / commit references continue to resolve.

## From ADR-003 — Project Scaffold Technical Design

**Date**: 2026-03-20
**Issue**: #1
**Original status**: READY_FOR_DEV

## Background

The project scaffold established the Go module, directory structure, development tooling, and local dev environment. All subsequent work depends on this foundation.

## Specification

Everything under this heading records the state at the 2026-03-20 bootstrap and is not
kept current. Pinned versions in particular have moved on (the repository tracks the
latest stable Go; see `go.mod`).

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

> **Historical snapshot, not a specification.** The tree below is the layout the initial
> bootstrap created on 2026-03-20. It is kept as a record of what ADR-003 asked for; it
> does not describe the repository today and must not be used as a target.
>
> For the current layout, read the tree itself: `find internal -maxdepth 1 -type d` for
> the top-level packages, `find internal/adapters -maxdepth 1 -type d` for the adapters,
> and `ls -d */` at the repository root. **CLAUDE.md § Directory layout** states the
> architectural contract those directories have to satisfy (domain has no external
> dependencies, ports hold the interfaces, adapters implement them), but its tree is
> abbreviated and is not an inventory, so do not read either document as a directory
> listing.

The bootstrap issue (#1) specified this structure:

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

## Notes

Follow-up work from the initial scaffold that is now implemented:
- Caddy adapter (`internal/adapters/caddy/`)
- Structured log schema (ADR-015)
- Full CLI with `serve` subcommand
