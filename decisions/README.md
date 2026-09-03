# Architectural Decision Records

Each ADR documents a significant architectural decision. ADRs are immutable records —
once accepted, they are not edited. If a decision is superseded, a new ADR references the old one.

## ADR Audit 2026-05-03

An audit of all ADRs was performed on 2026-05-03 against the ADR threshold (memory
`feedback_adr_threshold`): ADRs only for architecturally significant changes (new domain,
new port, wire-format, new CLI verb); not for bug fixes or chores.

Full report: `~/notes/vibewarden/audit-adr-2026-05-03.md`

Results applied 2026-05-04. The audit covered the 69 ADRs that existed on that date
(ADR-001–ADR-023, ADR-057–ADR-073, ADR-075–ADR-102, ADR-497); rows added to the index
afterwards (ADR-103 and up) are outside its scope.
- **45 KEEP** — load-bearing, no change to file content (other than explicit banner additions); 69 − 13 demoted − 11 tombstoned
- **13 DEMOTE** — content moved to `docs/internal/` or `docs/observability.md`; stubs remain at original paths
- **11 TOMBSTONE** — replaced with tombstones (files remain, content replaced; includes ADR-497 anomaly)
- **3 STATUS BANNERS added** — ADR-058 (composition-root note), ADR-063 (Obsolete), ADR-070 (Obsolete); ADR-080, ADR-081, ADR-088 already had banners pre-audit

Demoted and tombstoned ADRs that also carry supersession notes (for example ADR-079,
partially superseded by ADR-083) are recorded in their index rows below. They belong to
the DEMOTE and TOMBSTONE buckets, not to the KEEP banner set above.

## Index

| # | Title | PR | Notes |
|---|-------|-----|-------|
| [001](adr-001-plugin-architecture-config-driven-compiled-in-v1.md) | Plugin architecture — config-driven, compiled-in (v1) | — | |
| [002](adr-002-commercial-product-is-a-fleet-dashboard-not-hosted-vibewarde.md) | Commercial product is a fleet dashboard, not hosted VibeWarden | — | |
| [003](adr-003-project-scaffold-technical-design.md) | Project Scaffold Technical Design | — | Demoted to `docs/internal/scaffold-bootstrap.md` 2026-05-04 |
| [004](adr-004-request-routing-architecture-caddy-embedding.md) | Request Routing Architecture (Caddy Embedding) | — | |
| [005](adr-005-cli-pivot-project-scaffolding-and-management-tool.md) | CLI Pivot — Project Scaffolding and Management Tool | — | |
| [006](adr-006-add-user-app-service-to-generated-docker-compose-yml.md) | Add User App Service to Generated docker-compose.yml | — | Demoted to `docs/internal/compose-generation.md` 2026-05-04 |
| [007](adr-007-add-plugin-dependent-services-to-generated-docker-compose-ym.md) | Add Plugin-Dependent Services to Generated docker-compose.yml | — | Demoted to `docs/internal/compose-generation.md` 2026-05-04 |
| [008](adr-008-add-observability-profile-to-generated-docker-compose-yml.md) | Add Observability Profile to Generated docker-compose.yml | — | Demoted to `docs/internal/compose-generation.md` 2026-05-04 |
| [009](adr-009-generate-env-template-with-secure-credential-management.md) | Generate .env Template with Secure Credential Management | — | |
| [010](adr-010-add-vibew-secret-get-and-vibew-secret-list-commands.md) | Add `vibew secret get` and `vibew secret list` Commands | — | Demoted to `docs/internal/secret-cli.md` 2026-05-04 |
| [011](adr-011-migrate-demo-app-to-use-generated-docker-compose-yml.md) | Migrate Demo App to Use Generated docker-compose.yml | — | Removed 2026-05-04 — see tombstone |
| [012](adr-012-otel-sdk-integration-and-metricscollector-port-adapter-refac.md) | OTel SDK Integration and MetricsCollector Port/Adapter Refactoring | — | |
| [013](adr-013-otlp-exporter-configuration-and-telemetry-plugin-refactor.md) | OTLP Exporter Configuration and Telemetry Plugin Refactor | — | Demoted to `docs/observability.md` 2026-05-04 |
| [014](adr-014-prometheus-fallback-exporter-for-backward-compatibility.md) | Prometheus Fallback Exporter for Backward Compatibility | — | Demoted to `docs/internal/telemetry-migration.md` 2026-05-04 |
| [015](adr-015-bridge-slog-structured-events-to-otel-logs.md) | Bridge slog Structured Events to OTel Logs | — | |
| [016](adr-016-otel-collector-in-docker-compose-observability-stack.md) | OTel Collector in Docker Compose Observability Stack | — | Demoted to `docs/observability.md` 2026-05-04 |
| [017](adr-017-update-grafana-dashboards-for-otel-sourced-metrics.md) | Update Grafana Dashboards for OTel-Sourced Metrics | — | Removed 2026-05-04 — see tombstone |
| [018](adr-018-telemetry-documentation-and-configuration-guide.md) | Telemetry Documentation and Configuration Guide | — | Removed 2026-05-04 — see tombstone |
| [019](adr-019-tracerprovider-initialization-and-http-tracing-middleware.md) | TracerProvider Initialization and HTTP Tracing Middleware | — | |
| [020](adr-020-inject-trace-id-and-span-id-into-slog-context.md) | Inject trace_id and span_id into slog context | — | Demoted to `docs/internal/tracing.md` 2026-05-04 |
| [021](adr-021-include-trace-id-in-json-error-responses.md) | Include trace_id in JSON Error Responses | — | Demoted to `docs/internal/tracing.md` 2026-05-04 |
| [022](adr-022-identity-provider-port-abstraction.md) | Identity Provider Port Abstraction | — | |
| [023](adr-023-jwt-oidc-identity-adapter.md) | JWT/OIDC Identity Adapter | — | |
| [057](adr-057-defer-config-value-object-migration-from-ports-to-config-iss.md) | Defer config value-object migration from ports/ to config/ | — | |
| [058](adr-058-plugin-extension-point-for-external-plugin-registration-pro.md) | Plugin extension point for external plugin registration (Pro) | — | Status note added: composition root relocated per ADR-067 |
| [059](adr-059-rename-vibew-init-to-vibew-wrap-for-sidecar-only-scaffolding.md) | Rename `vibew init` to `vibew wrap` for sidecar-only scaffolding | — | Removed 2026-05-04 — see tombstone |
| [060](adr-060-vibew-init-lang-go-core-scaffold-command-and-go-language-pac.md) | `vibew init --lang go` — Core scaffold command and Go language pack | — | Removed 2026-05-04 — see tombstone |
| [061](adr-061-generate-agents-vibewarden-md-instead-of-claude-agents.md) | Generate AGENTS-VIBEWARDEN.md instead of .claude/agents/ | — | Demoted to `docs/internal/agents-file-conventions.md` 2026-05-04 |
| [062](adr-062-hot-config-reload-file-watcher-and-admin-api-endpoint.md) | Hot Config Reload — File Watcher and Admin API Endpoint | — | |
| [063](adr-063-vibew-deploy-core-ssh-rsync-remote-deployment.md) | vibew deploy core — SSH + rsync remote deployment | — | Obsolete — `vibew deploy` sunset by ADR-086 |
| [064](adr-064-ports-layer-consolidation-remove-config-adapter-and-app-serv.md) | Ports-layer consolidation | #843 | |
| [065](adr-065-reconcile-auth-config-surface-to-mode-only.md) | Reconcile `auth` config surface to mode-only | #845 | |
| [066](adr-066-scaffold-test-isolation-and-repo-safety-check.md) | Scaffold test isolation and repo safety check | #846 | |
| [067](adr-067-move-composition-roots-out-of-internal-app.md) | Move composition roots out of internal/app/ | #867 | |
| [068](adr-068-site-domain-model-for-multi-app.md) | Site domain model for multi-app | #875 | |
| [069](adr-069-caddy-multi-host-routes.md) | Caddy multi-host routes | #876 | |
| [070](adr-070-deploy-detection.md) | Deploy detection | #877 | Obsolete — `vibew deploy` sunset by ADR-086 |
| [071](adr-071-multi-site-directory-watcher.md) | Multi-site directory watcher | #878 | |
| [072](adr-072-cli-ux-wiring-for-multi-app.md) | CLI UX wiring for multi-app | #879 | Demoted to `docs/internal/multi-site-serve.md` 2026-05-04 |
| [073](adr-073-init-cwd-only.md) | Make `vibew init` scaffold in current directory only | — | Removed 2026-05-04 — see tombstone |
| [075](adr-075-redesign-vibew-deploy-local-resolution-no-remote-patching.md) | Redesign vibew deploy -- local resolution, no remote patching | #948 | Historical — `vibew deploy` sunset by ADR-086; lineage note: the local-resolution principle is preserved in ADR-085 |
| [076](adr-076-secret-uri-resolution-in-config.md) | secret:// URI resolution in vibewarden.yaml config | #1008 | |
| [077](adr-077-placeholder-substitution-for-composite-secret-values.md) | Placeholder substitution for composite secret values | #994 | |
| [078](adr-078-wire-acme-email-to-single-site-caddy-issuer.md) | Wire acme_email to single-site Caddy ACME issuer | #1027 | Removed 2026-05-04 — see tombstone |
| [079](adr-079-acme-fallback-chain-multi-issuer.md) | ACME fallback chain — multi-issuer automatic failover | #1026 | Demoted to `docs/internal/acme-fallback.md` 2026-05-04; partially superseded by ADR-083 |
| [080](adr-080-deploy-health-check-diagnostic-classification.md) | Deploy health-check diagnostic classification | — | Obsolete (Historical) — `vibew deploy` sunset by ADR-086 |
| [081](adr-081-auto-detect-arch-mismatch-during-deploy-prerequisites.md) | Auto-detect arch mismatch during deploy prerequisites | — | Obsolete (Historical) — `vibew deploy` sunset by ADR-086 |
| [082](adr-082-strict-config-merge-unknown-keys-fail-loudly.md) | Strict config merge — unknown keys fail loudly | #1053 | |
| [083](adr-083-acme-chain-hardening-email-preflight-buypass-removed.md) | ACME chain hardening — email preflight for ZeroSSL, Buypass removed from default chain | #1055 | |
| [084](adr-084-doctor-port-ownership-via-vibewarden-health-signature.md) | Doctor port ownership via VibeWarden health-signature probe | #1054 | |
| [085](adr-085-vibew-bundle-compose-only.md) | `vibew bundle` — compose-only deployment artifact generator | #1044 | |
| [086](adr-086-sunset-vibew-deploy.md) | Sunset `vibew deploy` — bundle-and-deploy-manually is the canonical path | #1051 | |
| [087](adr-087-test-placement-contract-tests-and-architectural-invariants.md) | Test placement — contract tests live with their adapter, architectural invariants live in test/architecture | — | |
| [088](adr-088-deploy-sh-local-run-convention.md) | deploy.sh runs locally — scp + ssh + healthcheck in one script *(superseded by #1138)* | #1087 | Superseded — see tombstone/banner in file |
| [089](adr-089-bundle-image-health-tag-scoping-freshness-arch.md) | Bundle image health — tag scoping, freshness, and arch warning | — | |
| [090](adr-090-le-rate-limit-preflight.md) | Let's Encrypt rate-limit preflight via Certificate Transparency | #1057 | |
| [091](adr-091-ports-hygiene-delete-dead-session-checker-adapter-move-outbound-ports.md) | Ports hygiene — delete dead SessionChecker adapter; move three outbound ports to `internal/ports/`; rename `AdminServerIface` | #1106, #1107 | |
| [092](adr-092-caddy-handler-dependency-injection.md) | Caddy handler dependency injection via composition-root-populated services registry | #1102 | |
| [093](adr-093-bundle-image-name-cwd-basename-fallback.md) | bundle image-name resolution — cwd-basename fallback unified across `vibew bundle` and `--build` | #1141 | Removed 2026-05-04 — see tombstone |
| [094](adr-094-bundle-sensitive-files-awareness-block.md) | vibew bundle sensitive-file awareness block | #1142 | Removed 2026-05-04 — see tombstone |
| [095](adr-095-status-three-state-ok-off-fail.md) | `vibew status` three-state model — OK / OFF / FAIL | #1143 | |
| [096](adr-096-vibew-eject-keep-and-clarify-non-docker-escape-hatch.md) | `vibew eject` — keep-and-clarify as the non-Docker escape hatch | #1147 | |
| [097](adr-097-obs-up-down-fix.md) | Fix `vibew obs up` no-op and `vibew obs down` nuking the main stack | — | Removed 2026-05-04 — see tombstone |
| [098](adr-098-upstream-health-probe.md) | Upstream health probe — wire the cached background probe into `/_vibewarden/health` | #1197 | |
| [099](adr-099-vibew-prompt-template-canonical-agent-kickoff.md) | `vibew prompt-template` — canonical agent kickoff prompt owned by the binary | — | |
| [100](adr-100-image-identity-via-project-root-label.md) | Image identity via `org.vibewarden.project-root-hash` label | — | |
| [101](adr-101-agent-kickoff-release-artifacts.md) | Agent-kickoff release artifacts — main repo emits canonical kickoff prompts as release assets | #1232 | |
| [102](adr-102-vibew-probe-go-stdlib-health-probe-and-env-resolver.md) | `vibew probe [--env <name>]` — Go-stdlib HTTPS probe of `/_vibewarden/health` and a generalisable env-resolver | #1233 | |
| [103](adr-103-wire-api-key-auth-mode-into-caddy-handler-chain.md) | Wire `api-key` auth mode into Caddy handler chain | #1302 | |
| [104](adr-104-openbao-prod-init-unseal-via-seed-secrets.md) | OpenBao prod init+unseal via seed-secrets init container | #1345 | |
| [105](adr-105-remove-grafana-plugin-name-constant.md) | Remove unused grafana plugin-name constant; grafana is a compose service | #1371 | |
| [106](adr-106-sidecar-image-pinning-cli-version.md) | Sidecar image pinning — thread CLI version into compose render | #1385 | |
| [107](adr-107-embedded-admin-ui.md) | Embedded user-management admin UI at /_vibewarden/admin/ui | #1391 | |
| [108](adr-108-no-authz-policy-engine-in-the-sidecar.md) | No authz policy engine (Casbin or similar) in the sidecar — authorization stays at path × method × identity attribute | #1461 | |
| [109](adr-109-license-waivers-mpl-2-mysql-driver-cc0-blake3.md) | License waivers — MPL-2.0 (`go-sql-driver/mysql`) and CC0-1.0 (`zeebo/blake3`), both transitive via Caddy | #1347 | Reconstructs the waiver lost as ADR-104; consolidates #1292 + #1293 |
| [110](adr-110-server-max-connections-listener-wrapper.md) | `server.max_connections` — concurrent-connection cap via a custom Caddy listener wrapper placed before TLS | #1311 | |
| [111](adr-111-sidecar-container-resource-limits.md) | Sidecar container resource limits — `mem_limit`/`cpus`/`pids_limit` in both compose templates, config-driven under `server:`, with a derived `GOMEMLIMIT` | #1306 | |
| [497](adr-497-graceful-shutdown-connection-draining.md) | Graceful Shutdown / Connection Draining | — | Removed 2026-05-04 — see tombstone (anomalous number, paste of issue #497) |

## Numbering

ADR numbers are sequential but not contiguous. Gaps (024–048, 049–056) are historical —
those numbers were never assigned. Do not renumber; existing issue and PR references
depend on stable ADR numbers.

**ADR-074 missing**: ADR-074 was reserved for the encrypted secret store decision but
never written. ADR-076 references it (`(ADR-074, internal/adapters/builtin/store.go)`) for
context only. The decision is inlined in `internal/adapters/builtin/store.go` code comments.

**ADR-497 anomaly**: Number 497 is almost certainly a mis-paste of issue #497. The file
was tombstoned in the 2026-05-03 audit. Sequential numbering resumes at 104.

**ADR-104 double-assignment**: number 104 was first assigned to the MPL-2.0 license
waiver when #1292 was closed, but that file was never committed. The empty slot was
later taken by the OpenBao prod-init decision, which is the only ADR-104. The license
waiver was reconstructed as ADR-109 (#1347). Any reference to "ADR-104" in or before
#1292 means ADR-109.

**ADR-108**: was reserved for the authz-engine decision while ADR-109 was being written;
it is now written (#1461) and the reservation is discharged.
