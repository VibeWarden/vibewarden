# ADR-018: Telemetry Documentation and Configuration Guide

**Date**: 2026-03-28
**Issue**: #292
**Status**: Accepted

### Context

Epic #280 ("Switch telemetry from Prometheus to OpenTelemetry") established a comprehensive
OTel-based telemetry pipeline through six implementation stories (ADR-012 through ADR-017):

- ADR-012: OTel SDK integration and MetricsCollector port/adapter refactoring
- ADR-013: OTLP exporter configuration and TelemetryConfig refactor
- ADR-014: Prometheus fallback exporter for backward compatibility
- ADR-015: Bridge slog structured events to OTel logs
- ADR-016: OTel Collector in Docker Compose observability stack
- ADR-017: Update Grafana dashboards for OTel-sourced metrics

Issue #292 is the documentation capstone for this epic. Users need clear documentation
explaining:

1. The new `telemetry:` config section and all its options
2. The three export modes: Prometheus-only (default), OTLP-only, dual-export
3. How the OTel Collector fits into the observability stack
4. How slog structured events are bridged to OTel logs
5. Migration path from legacy `metrics:` config to new `telemetry:` config
6. Example configurations for common backends

**Target audience:** Vibe coders who want zero-to-secure in minutes. Documentation must be
concise and practical, not an OTel tutorial.

### Decision

Create comprehensive telemetry documentation by:

1. Updating `docs/observability.md` with a new "Telemetry Configuration" section
2. Updating `vibewarden.example.yaml` with the full `telemetry:` section and comments
3. Adding migration guidance for users with existing `metrics:` config

This is a docs-only story. No Go code changes are expected.

#### Domain Model Changes

None. Documentation only.

#### Ports (Interfaces)

None. Documentation only.

#### Adapters

None. Documentation only.

#### Application Service

None. Documentation only.

#### File Layout

**Modified files:**

| File | Changes |
|------|---------|
| `docs/observability.md` | Add "Telemetry Configuration" section covering all export modes, OTel Collector architecture, slog-to-OTel bridge, migration guide |
| `vibewarden.example.yaml` | Add `telemetry:` section with all options and inline documentation |

**No new files.** All documentation is consolidated into existing files.

#### Documentation Structure

**1. Update `docs/observability.md`**

Add new sections after the existing "Quick Start" section:

```markdown
## Telemetry Configuration

VibeWarden uses OpenTelemetry as its telemetry foundation, supporting both pull-based
Prometheus scraping and push-based OTLP export. The `telemetry:` section in
`vibewarden.yaml` controls all telemetry behavior.

### Export Modes

VibeWarden supports three telemetry export modes:

| Mode | Prometheus | OTLP | Use Case |
|------|------------|------|----------|
| **Prometheus-only** (default) | Enabled | Disabled | Local development, single-instance deployments |
| **OTLP-only** | Disabled | Enabled | Cloud backends (Grafana Cloud, Datadog), fleet deployments |
| **Dual-export** | Enabled | Enabled | Migration, local + central collection |

### Prometheus-Only Mode (Default)

This is the zero-config default. VibeWarden exposes metrics at `/_vibewarden/metrics`
in Prometheus text format. No outbound connections are made.

```yaml
telemetry:
  enabled: true
  prometheus:
    enabled: true
  otlp:
    enabled: false
```

Or simply omit the `telemetry:` block entirely — the defaults match this configuration.

**When to use:** Local development, single-instance production where you run your own
Prometheus and scrape VibeWarden directly.

### OTLP-Only Mode

Metrics are pushed to an OTLP-compatible collector or backend. The `/_vibewarden/metrics`
endpoint is disabled. All telemetry flows outbound.

```yaml
telemetry:
  enabled: true
  prometheus:
    enabled: false
  otlp:
    enabled: true
    endpoint: https://otlp-gateway.example.com/otlp
    headers:
      Authorization: "Bearer ${OTLP_API_KEY}"
    interval: 30s
```

**When to use:** Cloud observability backends (Grafana Cloud, Datadog, Honeycomb, etc.),
fleet deployments where a central collector aggregates telemetry from multiple instances.

### Dual-Export Mode

Both Prometheus and OTLP exporters run simultaneously. Use this for gradual migration
or when you need both local scraping and central collection.

```yaml
telemetry:
  enabled: true
  prometheus:
    enabled: true
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
    interval: 15s
```

**When to use:** Migration from Prometheus-only to OTLP, or hybrid setups where local
dashboards coexist with central fleet observability.

### Configuration Reference

#### telemetry.enabled
**Type:** boolean
**Default:** `true`

Master switch for all telemetry collection. When `false`, no metrics are collected or
exported, and the `/_vibewarden/metrics` endpoint returns 404.

#### telemetry.path_patterns
**Type:** list of strings
**Default:** `[]`

URL path normalization patterns using colon-param syntax. Without patterns, all paths
are recorded as `"other"`. Configure the routes your app exposes to prevent
high-cardinality metric labels.

```yaml
telemetry:
  path_patterns:
    - "/users/:id"
    - "/api/v1/items/:item_id/comments/:comment_id"
```

#### telemetry.prometheus.enabled
**Type:** boolean
**Default:** `true`

Enables the Prometheus pull-based exporter. When enabled, metrics are served at
`/_vibewarden/metrics` in Prometheus text format with OpenMetrics compatibility.

#### telemetry.otlp.enabled
**Type:** boolean
**Default:** `false`

Enables the OTLP push-based exporter. Requires `telemetry.otlp.endpoint` to be set.

#### telemetry.otlp.endpoint
**Type:** string
**Default:** `""`

OTLP HTTP endpoint URL. Required when `telemetry.otlp.enabled` is `true`.

Examples:
- Local OTel Collector: `http://localhost:4318`
- Docker Compose: `http://otel-collector:4318`
- Grafana Cloud: `https://otlp-gateway-prod-us-central-0.grafana.net/otlp`

#### telemetry.otlp.headers
**Type:** map of string to string
**Default:** `{}`

HTTP headers to include with OTLP requests. Use for authentication.

```yaml
telemetry:
  otlp:
    headers:
      Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
      X-Custom-Header: "value"
```

#### telemetry.otlp.interval
**Type:** duration string
**Default:** `"30s"`

How often metrics are batched and pushed to the OTLP endpoint. Shorter intervals
reduce telemetry lag but increase network overhead.

Valid formats: `"15s"`, `"1m"`, `"30s"`.

#### telemetry.otlp.protocol
**Type:** string
**Default:** `"http"`

OTLP transport protocol. Only `"http"` is supported in this version. `"grpc"` is
reserved for future use.

#### telemetry.logs.otlp
**Type:** boolean
**Default:** `false`

Enables OTLP log export. When enabled, structured events (the AI-readable logs) are
exported to the same OTLP endpoint as metrics. Requires `telemetry.otlp.endpoint`
to be configured.

Logs are exported in addition to stdout JSON output — existing log collection via
stdout remains unchanged.

### Structured Event Log Export

VibeWarden's structured event logs (with `schema_version`, `event_type`, `ai_summary`,
and `payload` fields) can be exported via OTLP alongside metrics. Enable with:

```yaml
telemetry:
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
  logs:
    otlp: true
```

**How it works:**

1. Events are logged to stdout as JSON (existing behavior, always active)
2. Events are simultaneously sent to the OTel LoggerProvider
3. The LoggerProvider batches and pushes logs to the OTLP endpoint
4. OTel Collector receives logs and routes them to Loki (or any configured backend)

**OTel log record mapping:**

| Event field | OTel log record field |
|-------------|----------------------|
| `Timestamp` | `Timestamp` |
| `EventType` | Attribute: `event.type` |
| `SchemaVersion` | Attribute: `vibewarden.schema_version` |
| `AISummary` | `Body` (string) |
| `Payload.*` | Attributes: `vibewarden.payload.<key>` |

**Severity mapping:** Event types are mapped to OTel severity levels:

| Event type pattern | OTel Severity |
|-------------------|---------------|
| `*.failed`, `*.blocked`, `*.hit` | WARN |
| `*.unavailable`, `*_failed` | ERROR |
| All others | INFO |

### OTel Collector Architecture

When the observability profile is enabled (`docker compose --profile observability up`),
VibeWarden generates an OTel Collector configuration that acts as a telemetry hub:

```
VibeWarden --OTLP--> OTel Collector --metrics--> Prometheus (scrapes :8889)
                              |
                              +--logs--> Loki
```

The collector:
- Receives OTLP on port 4318 (HTTP)
- Exports metrics via Prometheus exporter on port 8889 (Prometheus scrapes this)
- Exports logs to Loki via the Loki exporter

**Collector config location:** `.vibewarden/generated/observability/otel-collector/config.yaml`

**Why a collector?**

- Decouples VibeWarden from backend details
- Enables batching, retry, and buffering
- Standard OTel pipeline that works with any OTLP-compatible backend
- Future-proof for distributed tracing

### Migrating from metrics: to telemetry:

The legacy `metrics:` config section is deprecated. VibeWarden automatically migrates
settings at startup and logs a warning.

**Before (deprecated):**

```yaml
metrics:
  enabled: true
  path_patterns:
    - "/users/:id"
```

**After (recommended):**

```yaml
telemetry:
  enabled: true
  path_patterns:
    - "/users/:id"
  prometheus:
    enabled: true
```

**Migration behavior:**

1. If `metrics:` is present but `telemetry:` is not, settings are copied automatically
2. A deprecation warning is logged at startup
3. The `/_vibewarden/metrics` endpoint works unchanged
4. Existing Prometheus scrapers and Grafana dashboards continue working

**When to migrate:** Update your config before the next major version. The `metrics:`
section will be removed in a future release.

### Example Configurations

#### Local Development (default)

No config needed. The defaults enable Prometheus-only mode:

```yaml
# Nothing required — defaults are:
# telemetry.enabled: true
# telemetry.prometheus.enabled: true
# telemetry.otlp.enabled: false
```

#### Grafana Cloud

Push metrics and logs to Grafana Cloud OTLP gateway:

```yaml
telemetry:
  enabled: true
  path_patterns:
    - "/api/v1/users/:id"
    - "/api/v1/orders/:order_id"
  prometheus:
    enabled: false  # Use OTLP instead
  otlp:
    enabled: true
    endpoint: https://otlp-gateway-prod-us-central-0.grafana.net/otlp
    headers:
      Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
    interval: 30s
  logs:
    otlp: true
```

Set `GRAFANA_OTLP_TOKEN` in your environment (base64-encoded `instanceId:apiKey`).

#### Self-Hosted OTel Collector

Push to your own OTel Collector while keeping local Prometheus scraping:

```yaml
telemetry:
  enabled: true
  path_patterns:
    - "/users/:id"
  prometheus:
    enabled: true  # Keep local /_vibewarden/metrics
  otlp:
    enabled: true
    endpoint: http://otel-collector.monitoring.svc:4318
    interval: 15s
  logs:
    otlp: true
```

#### Docker Compose Observability Stack

When using `docker compose --profile observability up`, the generated compose file
automatically sets these environment variables:

```
VIBEWARDEN_TELEMETRY_OTLP_ENABLED=true
VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT=http://otel-collector:4318
VIBEWARDEN_TELEMETRY_LOGS_OTLP=true
```

No manual config changes needed — just enable the observability profile.
```

**2. Update `vibewarden.example.yaml`**

Add new `telemetry:` section after the `metrics:` section (which will be marked deprecated):

```yaml
# Telemetry configuration
# VibeWarden uses OpenTelemetry as its telemetry foundation. This section controls
# all metrics and log export behavior.
#
# Export modes:
#   - Prometheus-only (default): Metrics scraped at /_vibewarden/metrics
#   - OTLP-only: Metrics pushed to OTLP endpoint (no local metrics endpoint)
#   - Dual-export: Both Prometheus scraping and OTLP push active
#
# See docs/observability.md for full configuration guide.
telemetry:
  # Master switch for telemetry collection (default: true)
  enabled: true

  # URL path normalization patterns using :param syntax.
  # Configure the routes your app exposes to prevent high-cardinality labels.
  # Example:
  #   path_patterns:
  #     - "/users/:id"
  #     - "/api/v1/items/:item_id/comments/:comment_id"
  path_patterns: []

  # Prometheus pull-based exporter (metrics served at /_vibewarden/metrics)
  prometheus:
    # Enable Prometheus exporter (default: true)
    # When enabled, metrics are available at /_vibewarden/metrics in Prometheus format.
    enabled: true

  # OTLP push-based exporter (metrics pushed to collector/backend)
  otlp:
    # Enable OTLP exporter (default: false)
    # Requires endpoint to be set.
    enabled: false

    # OTLP HTTP endpoint URL.
    # Examples:
    #   Local collector: http://localhost:4318
    #   Docker Compose:  http://otel-collector:4318
    #   Grafana Cloud:   https://otlp-gateway-prod-us-central-0.grafana.net/otlp
    endpoint: ""

    # HTTP headers for OTLP requests (e.g., authentication).
    # Example:
    #   headers:
    #     Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
    headers: {}

    # Export interval — how often metrics are batched and pushed (default: 30s).
    # Shorter intervals reduce telemetry lag but increase network overhead.
    interval: "30s"

    # Transport protocol: "http" (supported) or "grpc" (reserved for future).
    protocol: "http"

  # Structured event log export
  logs:
    # Enable OTLP log export (default: false).
    # When enabled, structured events (AI-readable logs) are exported to the same
    # OTLP endpoint as metrics. Logs are also written to stdout (unchanged behavior).
    # Requires telemetry.otlp.endpoint to be configured.
    otlp: false
```

**3. Add deprecation notice to existing metrics: section**

Update the `metrics:` section comment in `vibewarden.example.yaml`:

```yaml
# DEPRECATED: Use telemetry: section instead.
# This section remains for backward compatibility. Settings are automatically
# migrated to telemetry: at startup with a deprecation warning.
# This section will be removed in a future major version.
#
# Prometheus metrics
# VibeWarden exposes a Prometheus-compatible metrics endpoint at /_vibewarden/metrics.
metrics:
  # Enable metrics endpoint at /_vibewarden/metrics (recommended: true)
  enabled: true
  # Path normalization patterns (moved to telemetry.path_patterns)
  path_patterns: []
```

#### Sequence

Not applicable. This is a documentation story with no runtime changes.

#### Error Cases

Not applicable. Documentation only.

#### Test Strategy

**Manual verification:**

1. Read through updated `docs/observability.md` and verify clarity
2. Read through updated `vibewarden.example.yaml` and verify comments are accurate
3. Verify all code snippets in documentation match actual config struct fields
4. Verify example configurations are valid YAML and match TelemetryConfig schema
5. Cross-reference with ADR-012 through ADR-017 to ensure consistency

**Automated checks:**

1. YAML lint on `vibewarden.example.yaml` (existing CI)
2. Markdown lint on `docs/observability.md` (existing CI)

No new Go tests required — this is documentation only.

#### New Dependencies

None. Documentation only.

### Consequences

**Positive:**

- **Complete documentation:** Users have a single reference for all telemetry config
- **Clear migration path:** Existing users know how to migrate from `metrics:` to `telemetry:`
- **Example configs:** Practical examples for common backends reduce trial-and-error
- **Architecture clarity:** OTel Collector role is documented for users who want to understand the stack
- **Consistent with code:** Documentation reflects the actual config structs and behavior

**Negative:**

- **Documentation maintenance:** Must be updated when telemetry features change
- **Length:** The observability.md file grows significantly

**Trade-offs:**

- **Single file vs. multiple:** Chose to extend `docs/observability.md` rather than create
  a separate `docs/telemetry.md`. The telemetry config is part of the observability story,
  and splitting would fragment related content.

- **Depth vs. brevity:** Chose comprehensive coverage over minimal docs. Target users are
  vibe coders, but those who do read docs want complete information.

- **Code examples vs. full config:** Chose snippets over full files. Users can reference
  `vibewarden.example.yaml` for the complete structure.

---
