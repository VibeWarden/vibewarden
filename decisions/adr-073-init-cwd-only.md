# ADR-073: Make `vibew init` scaffold in current directory only

## Status
Accepted (implemented in PR #TBD)

## Context

`vibew init myapp` created a subdirectory `myapp/` and scaffolded into it.
`vibew init .` scaffolded into the current directory. `vibew init` (no args)
used the cwd name. The `--name` flag was an alternative to the positional arg.

This created confusion:
- Three ways to do the same thing (positional arg, `--name`, bare invocation)
- Inconsistent with `vibew wrap` which always works in the current directory
- Docs and install scripts were updated to say `mkdir myapp && cd myapp && vibew init`
  (bare form) but the CLI still accepted the old forms

## Decision

1. **Remove the positional `[project-name]` argument.** `vibew init` accepts no args.
2. **Remove the `--name` flag.** Project name is derived from `filepath.Base(cwd)`.
3. **Always scaffold in the current directory.** Same convention as `vibew wrap`.
4. **In interactive mode**, prompt only for description (not project name).

The canonical flow is now:

```bash
mkdir myapp && cd myapp
vibew init
```

## Consequences

### Positive
- One way to do it — no ambiguity
- Consistent with `vibew wrap`
- Docs, install scripts, and CLI all agree
- Simpler code (removed ~50 lines of name-resolution logic)

### Negative
- Breaking change for scripts that pass `vibew init myapp`
- Pre-adoption (v0.10.0, 0 stars) so impact is minimal
