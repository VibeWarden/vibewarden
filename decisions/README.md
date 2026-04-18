# Architectural Decision Records

Each ADR documents a significant architectural decision. ADRs are immutable records —
once accepted, they are not edited. If a decision is superseded, a new ADR references the old one.

## Index

| # | Title | PR |
|---|-------|-----|
| [001](adr-001-plugin-architecture-config-driven-compiled-in-v1.md) | Plugin architecture — config-driven, compiled-in (v1) | — |
| [002](adr-002-commercial-product-is-a-fleet-dashboard-not-hosted-vibewarde.md) | Commercial product is a fleet dashboard, not hosted VibeWarden | — |
| [003](adr-003-project-scaffold-technical-design.md) | Project Scaffold Technical Design | — |
| [004](adr-004-request-routing-architecture-caddy-embedding.md) | Request Routing Architecture (Caddy Embedding) | — |
| [005](adr-005-cli-pivot-project-scaffolding-and-management-tool.md) | CLI Pivot — Project Scaffolding and Management Tool | — |
| [006](adr-006-add-user-app-service-to-generated-docker-compose-yml.md) | Add User App Service to Generated docker-compose.yml | — |
| [007](adr-007-add-plugin-dependent-services-to-generated-docker-compose-ym.md) | Add Plugin-Dependent Services to Generated docker-compose.yml | — |
| [008](adr-008-add-observability-profile-to-generated-docker-compose-yml.md) | Add Observability Profile to Generated docker-compose.yml | — |
| [009](adr-009-generate-env-template-with-secure-credential-management.md) | Generate .env Template with Secure Credential Management | — |
| [010](adr-010-add-vibew-secret-get-and-vibew-secret-list-commands.md) | Add `vibew secret get` and `vibew secret list` Commands | — |
| [011](adr-011-migrate-demo-app-to-use-generated-docker-compose-yml.md) | Migrate Demo App to Use Generated docker-compose.yml | — |
| [012](adr-012-otel-sdk-integration-and-metricscollector-port-adapter-refac.md) | OTel SDK Integration and MetricsCollector Port/Adapter Refactoring | — |
| [013](adr-013-otlp-exporter-configuration-and-telemetry-plugin-refactor.md) | OTLP Exporter Configuration and Telemetry Plugin Refactor | — |
| [014](adr-014-prometheus-fallback-exporter-for-backward-compatibility.md) | Prometheus Fallback Exporter for Backward Compatibility | — |
| [015](adr-015-bridge-slog-structured-events-to-otel-logs.md) | Bridge slog Structured Events to OTel Logs | — |
| [016](adr-016-otel-collector-in-docker-compose-observability-stack.md) | OTel Collector in Docker Compose Observability Stack | — |
| [017](adr-017-update-grafana-dashboards-for-otel-sourced-metrics.md) | Update Grafana Dashboards for OTel-Sourced Metrics | — |
| [018](adr-018-telemetry-documentation-and-configuration-guide.md) | Telemetry Documentation and Configuration Guide | — |
| [019](adr-019-tracerprovider-initialization-and-http-tracing-middleware.md) | TracerProvider Initialization and HTTP Tracing Middleware | — |
| [020](adr-020-inject-trace-id-and-span-id-into-slog-context.md) | Inject trace_id and span_id into slog context | — |
| [021](adr-021-include-trace-id-in-json-error-responses.md) | Include trace_id in JSON Error Responses | — |
| [022](adr-022-identity-provider-port-abstraction.md) | Identity Provider Port Abstraction | — |
| [023](adr-023-jwt-oidc-identity-adapter.md) | JWT/OIDC Identity Adapter | — |
| [057](adr-057-defer-config-value-object-migration-from-ports-to-config-iss.md) | Defer config value-object migration from ports/ to config/ | — |
| [058](adr-058-plugin-extension-point-for-external-plugin-registration-pro.md) | Plugin extension point for external plugin registration (Pro) | — |
| [059](adr-059-rename-vibew-init-to-vibew-wrap-for-sidecar-only-scaffolding.md) | Rename `vibew init` to `vibew wrap` for sidecar-only scaffolding | — |
| [060](adr-060-vibew-init-lang-go-core-scaffold-command-and-go-language-pac.md) | `vibew init --lang go` — Core scaffold command and Go language pack | — |
| [061](adr-061-generate-agents-vibewarden-md-instead-of-claude-agents.md) | Generate AGENTS-VIBEWARDEN.md instead of .claude/agents/ | — |
| [062](adr-062-hot-config-reload-file-watcher-and-admin-api-endpoint.md) | Hot Config Reload — File Watcher and Admin API Endpoint | — |
| [063](adr-063-vibew-deploy-core-ssh-rsync-remote-deployment.md) | vibew deploy core — SSH + rsync remote deployment | — |
| [064](adr-064-ports-layer-consolidation-remove-config-adapter-and-app-serv.md) | Ports-layer consolidation | #843 |
| [065](adr-065-reconcile-auth-config-surface-to-mode-only.md) | Reconcile `auth` config surface to mode-only | #845 |
| [066](adr-066-scaffold-test-isolation-and-repo-safety-check.md) | Scaffold test isolation and repo safety check | #846 |
| [067](adr-067-move-composition-roots-out-of-internal-app.md) | Move composition roots out of internal/app/ | #867 |
| [068](adr-068-site-domain-model-for-multi-app.md) | Site domain model for multi-app | #875 |
| [069](adr-069-caddy-multi-host-routes.md) | Caddy multi-host routes | #876 |
| [070](adr-070-deploy-detection.md) | Deploy detection | #877 |
| [071](adr-071-multi-site-directory-watcher.md) | Multi-site directory watcher | #878 |
| [072](adr-072-cli-ux-wiring-for-multi-app.md) | CLI UX wiring for multi-app | #879 |
| [497](adr-497-graceful-shutdown-connection-draining.md) | Graceful Shutdown / Connection Draining | — |

## Numbering

ADR numbers are sequential but not contiguous. Gaps (024–048, 049–056) are historical —
those numbers were never assigned. Do not renumber; existing issue and PR references
depend on stable ADR numbers.
