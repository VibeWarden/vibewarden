# VibeWarden — Project Constitution

## What this project is

VibeWarden is an open-source security sidecar for vibe-coded apps.
Single Go binary embedding Caddy. Handles TLS, auth (Ory Kratos), user management,
rate limiting, security headers, and AI-readable structured logs.
Target: vibe coders who need zero-to-secure in minutes.

**The sidecar always runs locally, next to the app, on localhost. It is never hosted.**

Business model:
- **OSS core** (Apache 2.0) — local sidecar, free forever
- **Pro tier** (name TBD, placeholder "VibeWarden Pro") — fleet dashboard at `app.vibewarden.dev`,
  aggregates logs + metrics from multiple local instances. Target: small businesses and
  indie devs running multiple apps.
- **Enterprise** (future) — self-hosted fleet dashboard, SSO, compliance

Romanian SRL, VAT registered. Solo founder.

---

## Locked decisions (do not relitigate)

| Decision | Choice | Reason |
|---|---|---|
| Language | Go | Single binary distribution |
| Reverse proxy | Caddy embedded as library | Apache 2.0, programmatic config |
| Identity | Ory Kratos | Apache 2.0, battle-tested |
| Architecture | Hexagonal (ports & adapters) + DDD | Clean boundaries, testable |
| DB migrations | golang-migrate | Apache 2.0 |
| CLI framework | cobra | Apache 2.0 |
| Metrics | prometheus/client_golang | Apache 2.0 |
| Log package | log/slog (stdlib) | No license concern |
| Infrastructure | Hetzner | Cost-effective EU |
| Billing | Stripe | Standard |
| Containers | Docker Compose primary, Helm v2 | |
| Domain | vibewarden.dev | Registered |
| Logo mark | Sierpiński hexagon fractal | Purple #7C3AED → Cyan #06B6D4 |
| Distribution | Docker image + OS installers (pre-built) | User never builds the binary |
| Plugin activation | Config-driven YAML, compiled-in (v1) | Zero friction for target user |
| Sidecar locality | Always local, localhost only | Architectural requirement |
| No hosted sidecar | Hosting a sidecar makes no sense | Sidecar must run next to the app |
| Commercial product | Fleet dashboard, not hosted sidecar | Aggregation layer is the product |
| Commercial tier name | TBD (placeholder: "VibeWarden Pro") | Targeting small businesses |

---

## Plugin model

All plugins ship inside the official binary. Users activate them in `vibewarden.yaml`:

```yaml
plugins:
  tls:
    enabled: true
    provider: letsencrypt   # or: external (Cloudflare, registrar, etc.), self-signed (dev)
  user-management:
    enabled: true
    adapter: postgres
  rate-limiting:
    enabled: true
  grafana:
    enabled: false
  fleet:
    enabled: false          # opt-in: send telemetry to app.vibewarden.dev (Pro feature)
    endpoint: https://app.vibewarden.dev
    api_key: ${VIBEWARDEN_FLEET_KEY}
```

`provider: external` is the escape hatch for users who already manage TLS via Cloudflare,
their domain registrar, AWS ACM, etc.

`fleet` plugin is the bridge to the Pro tier — always opt-in, never on by default.

---

## Architecture principles

- **Hexagonal architecture**: domain layer has zero external dependencies.
  All I/O goes through ports (interfaces). Adapters implement ports.
- **DDD**: model the domain explicitly. Entities, value objects, aggregates, domain events.
- **SOLID**: single responsibility per type, dependency inversion via interfaces.
- **Functional where Go allows**: prefer pure functions, immutable value objects,
  explicit error handling over panics.
- **No global state**: everything passed via dependency injection.

### Directory layout

```
cmd/
  vibewarden/         # main entrypoint
internal/
  domain/             # entities, value objects, domain events — zero external deps
  ports/              # interfaces (inbound + outbound)
  adapters/
    caddy/            # Caddy embedding adapter
    kratos/           # Ory Kratos adapter
    postgres/         # DB adapter
    log/              # log sink adapters
  app/                # application services (use cases)
  config/             # config loading and validation
  plugins/            # plugin registry and lifecycle
migrations/           # golang-migrate SQL files
.claude/
  agents/             # subagent definitions
docs/
  decisions.md        # living architectural decisions log (ADRs)
```

---

## Dependency rules

- **Always use the latest stable versions** of languages, libraries, tools, and base images.
  Do not pin to older versions without an explicit reason documented in an ADR.
- **Always check license before adding a dependency**
- Approved licenses: Apache 2.0, MIT, BSD-2, BSD-3
- Rejected: GPL, AGPL, LGPL, CC-BY-SA, proprietary
- If unsure: ask the architect agent, do not add speculatively

---

## Code style

- Go standard formatting (`gofmt`, `goimports`)
- Error wrapping: `fmt.Errorf("context: %w", err)` — never swallow errors
- No `panic` in library code — only in `main()` for unrecoverable startup failures
- Table-driven tests preferred
- Every exported type and function must have a godoc comment
- Interfaces defined in `ports/`, not next to their implementations

---

## Testing

- Unit tests for all domain logic and middleware
- Integration tests for adapters (use `testcontainers-go` — Apache 2.0)
- Tests live next to the code they test (`foo_test.go`)
- Minimum coverage target: 80% on `internal/domain/` and `internal/app/`

---

## Agent pipeline

The standard flow for any GitHub issue:

```
PM Agent → Architect Agent → Dev Agent → Reviewer Agent → (your PR review) → repeat until merged
```

Status values used in `docs/decisions.md` and issue comments:
- `READY_FOR_ARCH` — PM done, architect should pick up
- `READY_FOR_DEV` — Architect done, dev should pick up
- `READY_FOR_REVIEW` — Dev done, reviewer should pick up
- `CHANGES_REQUESTED` — Reviewer or human requested changes
- `APPROVED` — Ready to merge

---

## GitHub conventions

- Org: `vibewarden`
- Repo: `vibewarden`
- Branch naming: `feat/<issue-number>-<short-slug>`, `fix/<issue-number>-<short-slug>`
- Commit style: conventional commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`)
- PR title = conventional commit style
- Labels: `epic:*` for epics, `status:*` for pipeline status

---

## Sub-agent routing rules

**Sequential dispatch** (this project always uses sequential):
- PM → Architect → Dev → Reviewer pipeline is always sequential
- Each stage depends on output of previous

**Do not parallelize** stages — shared files and state between stages.

**Background dispatch** is fine for:
- Research tasks (looking up library docs, license checks)
- Codebase exploration that doesn't modify files

---

## Key differentiator

The AI-readable structured log schema is VibeWarden's most original idea.
Every log event has a `schema_version`, `event_type`, `ai_summary`, and `payload`.
Schema published at `vibewarden.dev/schema/v1/event.json`.
Treat schema stability with the same care as a public API.
