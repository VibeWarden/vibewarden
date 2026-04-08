# VibeWarden Event Schema

The VibeWarden event schema defines the structure of all structured log events
emitted by the VibeWarden security sidecar. Every security-relevant action
produces a JSON event conforming to this schema.

## What the schema is

The schema is a JSON Schema (draft 2020-12) document that describes the shape
of every log event. It is published at `vibewarden.dev/schema/v1/event.json`
and embedded in the VibeWarden binary.

Each event is a flat JSON object with seven required top-level fields:

| Field | Type | Description |
|---|---|---|
| `schema_version` | string | Always `"v1"` for this release. |
| `event_type` | string | Dot-separated event identifier (e.g. `auth.success`). |
| `timestamp` | string | RFC 3339 UTC timestamp. |
| `severity` | string | Severity level: `info`, `low`, `medium`, `high`, `critical`. |
| `category` | string | Event category: `auth`, `network`, `policy`, `resilience`, `secret`, `user`, `audit`. |
| `ai_summary` | string | Concise human- and AI-readable description (max 200 chars). |
| `payload` | object | Event-specific structured data, shape depends on `event_type`. |

Optional enrichment fields (`actor`, `resource`, `outcome`, `risk_signals`,
`request_id`, `triggered_by`, `trace_id`, `span_id`) are present when relevant
context is available.

## Design principles

1. **AI-readable by default.** Every event includes an `ai_summary` field that
   an LLM can understand without parsing the payload. The `severity` and
   `category` fields enable automated triage and filtering.

2. **Forward-compatible.** Unknown `event_type` values must be accepted and
   passed through. Consumers should dispatch on known types and gracefully
   ignore unknown ones.

3. **Strict payloads.** Each known event type defines a payload shape with
   `additionalProperties: false`. This prevents accidental schema drift.

4. **Flat envelope.** The top-level structure is intentionally flat (no nesting)
   to simplify log pipeline parsing and reduce JSON path depth.

5. **Zero-ambiguity timestamps.** All timestamps are UTC, RFC 3339 format.

## Versioning policy

The v1 schema is frozen. See [docs/schema-evolution.md](../docs/schema-evolution.md)
for the full policy. Key rules:

- No existing field may be removed, renamed, or have its type changed.
- No new required fields may be added to an existing payload.
- New `event_type` values may be added (this is backward-compatible).
- Breaking changes require `schema_version: "v2"`.

When v2 is introduced, v1 events continue to be emitted for at least two major
releases.

## How to consume the schema

### Validating events

Use any JSON Schema validator that supports draft 2020-12:

```bash
# Using ajv-cli (Node.js)
npx ajv validate -s schema/v1/event.json -d my_event.json

# Using check-jsonschema (Python)
pip install check-jsonschema
check-jsonschema --schemafile schema/v1/event.json my_event.json
```

### In Go code

The schema is embedded in the binary. Tests in `schema/v1/schema_validate_test.go`
demonstrate validation using `santhosh-tekuri/jsonschema` (Apache 2.0).

### Dispatching on event_type

Consumers should:

1. Check `schema_version` first -- reject unknown versions.
2. Use `event_type` to route to type-specific parsing logic.
3. Pass through events with unknown `event_type` values without error.
4. Use `severity` and `category` for filtering and prioritisation.

## Examples

The `schema/v1/examples/` directory contains sample events for common event
types. Each example validates against the schema and demonstrates the expected
field values for its event type.

## Directory structure

```
schema/
  README.md               # This file
  v1/
    event.json            # The v1 JSON Schema
    examples/             # Example events (one per file)
      auth_success.json
      auth_failed.json
      rate_limit_hit.json
      request_blocked.json
      ...
```
