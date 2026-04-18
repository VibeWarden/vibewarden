# ADR-067: Move composition roots out of internal/app/


**Status:** Accepted
**Issue:** #809
**Date:** 2026-04-15

### Context

`internal/app/serve/` imported six adapter packages and `internal/mcp/`
imported both `internal/adapters/ops` and `internal/app/ops`, violating
the hexagonal invariant that the application layer depends only on ports.
ADR-064 cleaned the ports layer; this ADR completes the separation by
moving all composition-root wiring out of `internal/app/` and
`internal/mcp/`.

### Decision

1. **Serve wiring moves to `cmd/vibewarden/`** — the standard Go
   convention for composition roots. No `internal/wiring/` package.
   Files: `wiring_serve.go`, `wiring_serve_helpers.go`, and their tests.
   The `internal/app/serve/` package is deleted.

2. **MCP stays in `internal/mcp/`** — only adapter wiring is extracted.
   `RegisterDefaultTools` gains a `ToolDeps` struct parameter carrying
   `ports.HealthChecker` and a local `DoctorRunner` interface. Adapter
   construction moves to `internal/cli/cmd/mcp.go` (real deps) and
   zero-value `ToolDeps{}` for help-text generation.

3. **Two-phase commit** — serve wiring first, MCP wiring second. The
   tree compiles and tests pass at each commit.

### Consequences

**Positive:**
- `internal/app/**/*.go` (non-test) has zero import paths matching
  `internal/adapters/`.
- `internal/mcp/**/*.go` has zero import paths matching
  `internal/adapters/` or `internal/app/`.
- The hexagonal invariant is now structurally enforced: adapter imports
  live exclusively in composition roots (`cmd/`) and CLI commands.
- Pro binary reuse is straightforward — a separate `cmd/vibewarden-pro/`
  can import the same app packages and wire its own adapters.

**Negative:**
- `cmd/vibewarden/` gains ~550 lines of wiring code. This is acceptable
  because wiring code is inherently untestable by unit tests (it glues
  real implementations together) and is already excluded from the coverage
  scope.
- The `DoctorRunner` interface in `internal/mcp/` is a local adapter
  boundary rather than a port in `internal/ports/`. This is intentional:
  the MCP package is the only consumer, so the interface lives at the
  consumption site per Go convention.
