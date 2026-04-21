# ADR-087: Test placement — contract tests live with their adapter, architectural invariants live in test/architecture

**Date**: 2026-04-20
**Issue**: #717
**Status**: Accepted

### Context

`internal/ports/` contained four test files (`audit_test.go`, `otel_test.go`,
`ratelimit_test.go`, `purity_test.go`). Ports are pure interface and
value-object definitions; having `*_test.go` siblings blurs the hexagonal
boundary and makes routine port edits look like test-churn to reviewers.

Three distinct test shapes were mixed in the ports package:

1. **Shared contract tests** — assertions about a port's value objects,
   zero-values, exported constants, and `var _ Port = (*stub)(nil)` compile
   checks. These constrain the contract every adapter must honour.
   (`audit_test.go`, `ratelimit_test.go`.)
2. **Single-adapter tests** — assertions about helpers consumed by exactly
   one adapter package. (`otel_test.go` — `DescriptionOf`/`UnitOf`/
   `BucketsOf` are consumed only by `internal/adapters/otel/meter.go`.)
3. **Architectural invariants** — assertions that enforce a repo-wide
   constraint by introspecting packages via `go/build`. The ports-purity
   guardrail locked in ADR-064 (`purity_test.go`, `//go:build purity`)
   falls here. It is not a contract test; it is a repo-level tripwire.

The repo had no established convention for any of these three shapes.
PM issue #717 proposed three placements but flagged the shared-contract
home and the invariant home as architect decisions. This ADR records
those decisions and makes them reusable for any future test of the same
shape.

### Decision

Tests are placed by the *shape* of what they assert, not by the layer
they happen to sit near today.

1. **Shared contract tests → the adapter package that owns the port's
   implementations**, filename `contract_test.go`, with a godoc banner
   naming the port(s) the contract covers.

   The adapter package is the single Go package that hosts every
   implementation of the port (`internal/adapters/audit` hosts
   `json_writer`, `multi_writer`, `otel_writer`; `internal/adapters/
   ratelimit` hosts `memory`, `redis_adapter`, `fallback_*`). It is not
   arbitrary — the package *is* the unit of shared implementation. Go's
   convention is "tests live next to the code they test"; for a contract
   shared across N sibling files in one package, that one package is the
   correct home.

   Rejected alternative: a new `test/contract/ports/` subtree. With only
   two shared contract tests today, it would create a parallel-structure
   maintenance burden (import-cycle risk, duplicate `ports_test` package
   scaffolding, discovery split between `internal/adapters/*` and
   `test/contract/*`) for no discovery benefit. If the ratio of shared
   contracts ever grows large enough to justify a dedicated tree, revisit
   this decision with a follow-up ADR.

   Naming: `contract_test.go` (unprefixed) is sufficient — the host
   package name disambiguates. `audit_contract_test.go` inside
   `package audit_test` would be redundant. Files must open with a godoc
   comment of the form:
   `// Contract test — applies to all <PortName> adapters.`

2. **Single-adapter tests → the sole adapter's package**, filename
   descriptive (`instrument_option_test.go`), no special banner.

   If a second adapter is later introduced and the test's assertions
   apply to it too, promote the file to `contract_test.go` at that
   time — do not pre-promote speculatively.

3. **Architectural invariants → `test/architecture/`**, as a new sibling
   of `test/benchmarks/`, `test/e2e/`, `test/integration/`,
   `test/egress/`, `test/quickstart/`. Package `architecture_test`.

   Invariants are repo-wide constraints, not adapter contracts and not
   domain logic. Their correct home is the `test/` tree already used for
   cross-cutting concerns. Filename encodes the invariant target
   (`ports_purity_test.go` here). Build tags from the original file are
   preserved (`//go:build purity`). The invocation path changes from
   `go test -tags=purity ./internal/ports/...` to
   `go test -tags=purity ./test/architecture/...` and must be documented
   wherever the former was referenced.

   Rejected alternative: keep `purity_test.go` in `internal/ports/` with
   a build-tag carve-out. The point of issue #717 is *zero* `*_test.go`
   files in `internal/ports/`; an exception contradicts the goal.

### Ruling on the issue's open questions

1. **Purity-test destination**: `test/architecture/ports_purity_test.go`,
   new subtree created as part of the implementing PR. Build tag
   preserved.
2. **File naming**: `contract_test.go` (unprefixed) for shared contract
   tests. Godoc banner on the first non-tag line identifies the port(s).

### File layout after implementation

```
internal/adapters/
  audit/
    contract_test.go                 # was internal/ports/audit_test.go
  otel/
    instrument_option_test.go        # was internal/ports/otel_test.go
  ratelimit/
    contract_test.go                 # was internal/ports/ratelimit_test.go
test/
  architecture/
    ports_purity_test.go             # was internal/ports/purity_test.go
```

Package renames:
- `package ports_test` → `package audit_test` in `audit/contract_test.go`
- `package ports_test` → `package otel_test` in `otel/instrument_option_test.go`
- `package ports_test` → `package ratelimit_test` in `ratelimit/contract_test.go`
- `package ports_test` → `package architecture_test` in `test/architecture/ports_purity_test.go`

All four files keep their existing `"github.com/vibewarden/vibewarden/internal/ports"`
external import. No logic, assertion, or test-name change is permitted
in the move — only package header and (where needed) import path.

The `stubLimiter` / `stubFactory` types in `ratelimit_test.go` are
file-local and travel with the file. They must not collide with any
existing identifier in `internal/adapters/ratelimit`; a quick `grep`
during implementation confirms the names are unused in the adapter
package.

### Sequence for the implementing PR

1. Create `test/architecture/` directory.
2. `git mv internal/ports/purity_test.go test/architecture/ports_purity_test.go`;
   rewrite package header to `architecture_test`. Verify
   `go test -tags=purity ./test/architecture/...` passes.
3. `git mv internal/ports/audit_test.go internal/adapters/audit/contract_test.go`;
   rewrite package header to `audit_test`; prepend godoc banner.
4. `git mv internal/ports/otel_test.go internal/adapters/otel/instrument_option_test.go`;
   rewrite package header to `otel_test`.
5. `git mv internal/ports/ratelimit_test.go internal/adapters/ratelimit/contract_test.go`;
   rewrite package header to `ratelimit_test`; prepend godoc banner;
   verify `stubLimiter`/`stubFactory` names do not collide.
6. Grep the repo for any documentation or script that invokes
   `go test -tags=purity ./internal/ports/...` and update to
   `./test/architecture/...`. Candidate targets: `Makefile`,
   `docs/**`, `.github/**`.
7. `make check` — must pass.

### Error cases

- **Package-name collision** in target package — unlikely for these four
  files, but if it occurs the mover must rename the local identifier
  (test, type) rather than re-export new helpers from `internal/ports/`
  (no new port exports are permitted; see issue #717 scope).
- **Coverage drift** — aggregated coverage must not decrease. Coverage
  on `internal/ports/` will drop because its tests leave; that is the
  point. The missing coverage reappears on the destination packages.
- **Purity tag invocation drift** — if a script still invokes
  `-tags=purity ./internal/ports/...`, it becomes a silent no-op after
  the move (no files match the tag in that path). The grep step above
  is the mitigation.
- **Test discovers a latent bug mid-move** — per issue #717 scope, do
  not fix in this PR; open a follow-up issue and preserve the existing
  behaviour.

### Test strategy

No new test logic is introduced by this refactor. The correctness bar is:

- All four files continue to assert what they asserted before the move.
- `make check` is green.
- `go test -tags=purity ./test/architecture/...` exits 0.
- `ls internal/ports/*_test.go 2>/dev/null` returns empty.

### New dependencies

None.

### Consequences

**Positive:**
- `internal/ports/` becomes a pure interface-and-value-object package;
  routine port edits no longer surface test-file diffs.
- The three test shapes have explicit, reusable homes; future tests of
  the same shape have a single correct destination without re-litigation.
- Shared contract tests are now visible to every adapter maintainer in
  the package — newcomers see the invariants their adapter must honour.
- The ports-purity invariant is promoted to a first-class cross-cutting
  concern alongside e2e, integration, and benchmarks.

**Negative:**
- Adding a third shared adapter to `internal/adapters/audit` or
  `internal/adapters/ratelimit` means the `contract_test.go` file's
  assertions may need to grow — this is the cost of co-location vs. a
  dedicated `test/contract/` tree. Accepted for current scale (two
  such tests).
- `test/architecture/` is a new subtree. Its first inhabitant is one
  file; the tree is justified only if future architectural invariants
  are added (the ports-purity pattern is likely to be repeated for
  `internal/domain/` purity, for example). If after 12 months the tree
  still contains one file, consider inlining back into
  `test/integration/` and retiring this ADR.
- Build-tag invocation path changes. Any undocumented script invoking
  the old path silently becomes a no-op. The grep step in the sequence
  mitigates, but no linter enforces it.

### Limitations (v1)

- No linter rule blocks a future `*_test.go` from reappearing in
  `internal/ports/`. Code review is the enforcement mechanism. If
  drift recurs, a follow-up ADR can add a `make check` guard that
  `ls internal/ports/*_test.go` returns empty.
- This ADR does not cover tests for `internal/domain/` purity, or
  any other future architectural invariant; the `test/architecture/`
  pattern it establishes is available for those, but each new
  invariant needs its own PM/architect pass to define what it asserts.
