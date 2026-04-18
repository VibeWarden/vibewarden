# ADR-002: Commercial product is a fleet dashboard, not hosted VibeWarden

**Date**: 2026-03-20
**Status**: Accepted

### Context
VibeWarden is a sidecar — it must run next to the app, on localhost. Hosting a sidecar
as a service doesn't make sense architecturally. The question was: what is the commercial
product then?

### Decision
The sidecar is always self-hosted (OSS, free forever). The commercial product is a
**fleet dashboard**: a cloud service at `app.vibewarden.dev` that aggregates logs,
metrics, and health data from multiple local VibeWarden instances.

Tier model:
| Tier | What it is | Target |
|---|---|---|
| OSS | Local sidecar, config-driven, single-app embedded dashboard | Individual vibe coders |
| Pro (name TBD) | Fleet dashboard at app.vibewarden.dev, multi-instance observability | Small businesses, indie devs with multiple apps |
| Enterprise (future) | Self-hosted fleet dashboard, SSO, compliance | Larger teams |

Commercial tier name is TBD — "VibeWarden Pro" is a placeholder. Targeting small
businesses, not enterprise. Final name to be decided later.

### Consequences
- Each local VibeWarden instance optionally phones home to the fleet dashboard
- Phone-home is strictly opt-in, configured in vibewarden.yaml
- This model mirrors Grafana, Netdata, Prometheus — agent free and local, aggregation is the product
- MCP server (v2) integrates with the fleet dashboard for AI-driven observability

---

## PM Log

### 2026-03-20 - Initial Epic Creation

**Created 9 epic issues** for the VibeWarden v1 roadmap.

| Issue | Title | Epic Label |
|-------|-------|------------|
| #1 | Epic: Project Scaffold | `epic:scaffold` |
| #2 | Epic: Request Routing (Caddy Embedding) | `epic:routing` |
| #3 | Epic: Auth (Ory Kratos Integration) | `epic:auth` |
| #4 | Epic: Rate Limiting | `epic:rate-limiting` |
| #5 | Epic: AI-readable Structured Logs | `epic:structured-logs` |
| #6 | Epic: CLI (cobra) | `epic:cli` |
| #7 | Epic: Observability (Prometheus Metrics) | `epic:observability` |
| #8 | Epic: User Management (Admin API) | `epic:user-management` |
| #9 | Epic: Grafana Observability Stack | `epic:grafana-stack` |

**Recommended implementation order:**
1 → 5 → 6 → 2 → 3 → 4 → 7 → 8 → 9

**Note:** Run `gh auth refresh -s read:project` to enable adding issues to the project board.

---
