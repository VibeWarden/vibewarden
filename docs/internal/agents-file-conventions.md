# Agents File Conventions — Internal Reference

> This file was relocated from `decisions/adr-061-generate-agents-vibewarden-md-instead-of-claude-agents.md`
> on 2026-05-04 as part of the ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`).
> The stub at that path remains stable; existing PR / commit references continue to resolve.

## From ADR-061 — Generate AGENTS-VIBEWARDEN.md instead of .claude/agents/

**Date**: 2026-04-03
**Issue**: #632

## Background

The original `vibew init` scaffolding generated agent instruction files inside
`.claude/agents/`. This approach conflicted with user customizations and was
incompatible with the `AGENTS.md` convention used by Claude, Cursor, Windsurf,
and other AI coding tools.

## Two-File Approach

`vibew init` now generates:

1. **`AGENTS-VIBEWARDEN.md`** — fully owned by vibew, always regenerated on `vibew init --force`
2. **`AGENTS.md`** — user-owned, contains a reference line to `AGENTS-VIBEWARDEN.md`

### Implementation details

- `AGENTS-VIBEWARDEN.md` is produced by combining the shared
  `agents/agents-vibewarden.md.tmpl` with the language-specific code conventions template.
- The warning header at the top of `AGENTS-VIBEWARDEN.md` ("Do not edit — changes will
  be overwritten") makes vibew ownership explicit.
- `AGENTS.md`: created with minimal reference if absent; reference appended if file
  exists but lacks it; left unchanged if reference already present.
- Reference detection: simple substring match for `AGENTS-VIBEWARDEN.md`.

### Generated output structure (current)

```
<project>/
├── AGENTS.md                    # User-owned, references AGENTS-VIBEWARDEN.md
├── AGENTS-VIBEWARDEN.md         # vibew-owned, regenerated on updates
├── CLAUDE.md                    # Still generated (project instructions)
└── ...
```

### Templates

| Path | Purpose |
|------|---------|
| `internal/cli/templates/agents/agents-vibewarden.md.tmpl` | Shared base |
| `internal/cli/templates/agents/agents.md.tmpl` | Template for new AGENTS.md |

### Migration path for existing projects

1. Run `vibew init --force` to regenerate scaffolding
2. Move any customizations from `.claude/agents/*.md` to `AGENTS.md`
3. Delete `.claude/agents/` directory
4. Commit changes
