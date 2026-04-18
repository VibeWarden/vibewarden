# ADR-064: Ports-layer consolidation — remove config, adapter, and app-service leaks from internal/ports

**Date**: 2026-04-15
**Status**: Accepted
**Issue**: #818

### Context

A 2026-04-16 architect audit found three independent violations of the
hexagonal invariant locked in `CLAUDE.md` ("domain layer has zero external
dependencies… interfaces defined in `ports/`, not next to their
implementations"):

1. `internal/ports/generate.go` imported `internal/config`, reaching from the
   ports package back into the config package.
2. `internal/adapters/apikey/openbao_validator.go` declared a local
   `KeyStore` interface that was really a generic outbound secret-KV-read
   port, co-located with its sole consumer instead of living in
   `internal/ports/`.
3. `internal/adapters/http/admin_proposals.go` depended on
   `*proposalapp.Service` directly, so the HTTP inbound adapter was coupled
   to a concrete application type rather than an interface.

Each finding is small on its own; together they show the ports layer is
leaking. This ADR captures the cleanup and states the ports-layer import
policy end-to-end.

### Decision

The `internal/ports/` package imports stdlib and `internal/domain/*` only.
No import of `internal/config`, `internal/adapters/*`, or `internal/app/*`
is permitted.

To restore the invariant, three surgical changes land together:

1. **F1 — `ConfigGenerator` takes a port-owned DTO.** A new
   `ports.GeneratorInput` struct carries typed decision fields plus an
   opaque `TemplateData any` payload. `ports.ConfigGenerator.Generate` is
   retyped to `Generate(ctx, GeneratorInput, outputDir) error`. A new
   mapper `(*config.Config).ToGeneratorInput()` lives in `internal/config`
   so the ports package does not import config.

   The typed decision fields (`Profile`, `AuthEnabled`, `AuthMode`,
   `KratosExternal`, `SecretsEnabled`, `ObservabilityEnabled`) are
   declared for the ports contract but **intentionally unread by the v1
   service body**. The generator recovers the concrete `*config.Config`
   from `TemplateData` via a type assertion and continues to branch on it.
   This keeps template output byte-identical to main and leaves a clean
   future path: the Generate body can migrate off the assertion in a later
   PR by consuming the typed fields and narrowing `TemplateData` to a
   named template model.

2. **F2 — Promote `apikey.KeyStore` to `ports.SecretKVReader`.**
   `ports.SecretKVReader` is the minimal outbound port for reading a
   slice of a KV secret store (`Get(ctx, path) (map[string]string, error)`).
   `ports.SecretStore` embeds `SecretKVReader` so every existing
   implementation (including `*openbao.Adapter`) still satisfies the full
   store interface. The local `apikey.KeyStore` declaration is deleted;
   `OpenBaoValidator` consumes `ports.SecretKVReader`.

3. **F3 — Introduce `ports.ProposalService` aggregate inbound port.**
   The port aggregates the full `Create`/`List`/`Get`/`Approve`/`Dismiss`
   surface because the HTTP handler — the only consumer today — exercises
   every operation; splitting into reader/writer buys no ISP benefit and
   forces shim boilerplate. `*proposalapp.Service` satisfies the port,
   enforced by a `var _ ports.ProposalService = (*Service)(nil)`
   compile-time assertion in the application package. The alias
   `proposalapp.CreateParams = ports.ProposalCreateParams` keeps every
   existing caller (HTTP handler, integration tests, unit tests) compiling
   unchanged.

### Ruling on the three open questions from the PM

1. **`ProposalService` — single aggregate interface.** One port, five
   methods. Future narrow consumers can embed a sub-interface locally
   via Go structural typing.
2. **`SecretKVReader`, not `KeyStore`.** The original name leaked the
   apikey domain into a generic port. `SecretKVReader` describes what
   the port *is*: a read-only slice of a secret KV store. Contract stays
   minimal at `Get(ctx, path)`; no `Health` method.
3. **Narrow DTO + opaque `TemplateData any`.** Typed decision fields
   for the service-side branching layer; opaque payload for the template
   renderer (which already takes `any`). Minimal surface, byte-identical
   template output, clear future migration path.

### Migration order

To keep the tree green at each commit:

1. Add the three port types without removing anything
   (`SecretKVReader`, `GeneratorInput`, `ProposalService`,
   `ProposalCreateParams`).
2. Migrate the generate path — signature change, config mapper, three
   callers (`cli/cmd/generate`, `app/ops/dev` x2, `app/deploy`), and
   their test fakes.
3. Migrate the adapter + HTTP — delete `apikey.KeyStore`, retype the
   admin-proposals handler to `ports.ProposalService`, add the
   compile-time assertion.

### Behavioural equivalence guards

- HTTP admin-proposals endpoints exercise identical status codes, bodies,
  and error mappings under both the real `*proposalapp.Service` and a
  hand-rolled stub that only satisfies `ports.ProposalService`
  (`internal/adapters/http/admin_proposals_port_test.go`).
- Generator output is byte-identical whether the input is built by
  `cfg.ToGeneratorInput()` or by constructing `ports.GeneratorInput`
  directly with the same `*config.Config` as `TemplateData`
  (`internal/app/generate/service_port_test.go`).
- A rejected-payload test confirms the Generate body's type assertion
  errors cleanly when `TemplateData` is not `*config.Config`.
- An advisory `//go:build purity` test in `internal/ports/purity_test.go`
  asserts `internal/ports` imports only stdlib and `internal/domain/*`.
  It is not part of the default `make check` gate — operators or CI can
  opt in with `go test -tags=purity ./internal/ports/...`.

### Consequences

**Positive:**
- `internal/ports/` is now a pure interface package — stdlib plus
  `internal/domain/*` only.
- HTTP admin-proposals handler is testable against any implementation of
  `ports.ProposalService`, not just `*proposalapp.Service`.
- `apikey.OpenBaoValidator` composes with any implementation of
  `ports.SecretKVReader`, not just the colocated interface.
- A compile-time assertion enforces that the app service and the inbound
  port stay in sync.

**Negative:**
- The generator service body still carries a `TemplateData.(*config.Config)`
  cast — this is intentional for v1 but is technical debt that must be
  retired when the body migrates to consume the typed decision fields.
  Follow-up work tracked outside this ADR.
- `proposalapp.CreateParams` is now a type alias; this is load-bearing
  public API to keep existing callers compiling. Renaming or removing
  the alias requires a full caller audit.

### Limitations (v1)

- The purity test is advisory (build-tagged). No linter rule enforces
  the ports import policy in CI. If the policy is re-violated, reviewers
  and the advisory test are the safety net.
- The `GeneratorInput` typed fields are not yet consumed by the service
  body. Adding a new decision field requires coordinating the DTO, the
  mapper, and — in a later PR — the service body.
