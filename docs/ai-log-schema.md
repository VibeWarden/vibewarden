# AI-Readable Log Schema

VibeWarden emits every security-relevant event as a strongly-typed JSON log
entry. The schema is designed so that both humans and AI agents can understand
what happened without needing to parse free-form text.

The schema is published at `vibewarden.dev/schema/v1/event.json`.
Changes to the schema are treated with the same care as a public API:
backwards-incompatible changes increment the schema version and are documented
in [Schema Evolution](schema-evolution.md).

---

## Why "AI-readable"?

Traditional application logs are optimised for grep and sed — plain text lines
where meaning is embedded in an ad-hoc string format.  AI agents work better
with structured, typed, semantically-labelled data.

VibeWarden logs are AI-readable because:

1. **Every field has a stable name.** `actor.id`, `resource.path`, `outcome` —
   an LLM can refer to field names directly rather than parsing sentence structure.

2. **Enumerations are typed.** `outcome` is one of `allowed`, `blocked`,
   `rate_limited`, or `failed` — not "user was blocked" buried in a string.

3. **Semantic models are reused.** The same `Actor`, `Resource`, and
   `RiskSignal` shapes appear across all event types, so agents need to learn
   only one vocabulary.

4. **`ai_summary` provides a pre-computed sentence.** Every event carries a
   concise human-language summary (≤ 200 characters) so an LLM can answer
   "what happened?" without deserialising the full payload.

5. **Causality fields are explicit.** `request_id`, `trace_id`, and
   `triggered_by` let agents reconstruct causal chains across multiple events.

---

## Envelope fields

Every event carries these top-level fields regardless of type.

| Field | Type | Description |
|---|---|---|
| `schema_version` | `string` | Always `"v1"` for the current schema. |
| `event_type` | `string` | Stable identifier for the event (e.g. `"auth.success"`). |
| `timestamp` | RFC3339 string | When the event occurred, always in UTC. |
| `ai_summary` | `string` | One-sentence human+AI-readable description (≤ 200 chars). |
| `actor` | `Actor` | Entity that initiated the action. See [Actor model](#actor-model). |
| `resource` | `Resource` | Target of the action. See [Resource model](#resource-model). |
| `outcome` | `Outcome` | Enforcement result. See [Outcome enum](#outcome-enum). |
| `risk_signals` | `[]RiskSignal` | Machine-detectable risk indicators. See [Risk signals](#risk-signals). |
| `request_id` | `string` | Value of the `X-Request-ID` header. Empty when absent. |
| `trace_id` | `string` | W3C trace-id of the active OpenTelemetry span. Empty when absent. |
| `triggered_by` | `string` | Internal component that raised the event (e.g. `"auth_middleware"`). |
| `payload` | `object` | Event-specific fields. Keys and types are defined per event type. |

---

## Actor model

The `actor` field describes the entity that initiated the security-relevant
action.

```json
{
  "type": "user",
  "id":   "a1b2c3d4-...",
  "ip":   "203.0.113.42"
}
```

| Field | Type | Description |
|---|---|---|
| `type` | `string` | `ip`, `user`, `api_key`, or `system`. |
| `id` | `string` | Unique identifier within the type namespace: user ID, API key name, IP address, or empty for `system`. |
| `ip` | `string` | Client IP address. Present for `ip` (equals `id`), and optionally for `user` and `api_key`. Omitted for `system`. |

**Actor types:**

- `ip` — anonymous client identified only by remote address.
- `user` — authenticated identity from Ory Kratos (session-based auth) or a JWT.
- `api_key` — request authenticated via an API key (key name in `id`).
- `system` — action initiated internally by the sidecar itself (health probe, certificate renewal, config reload).

---

## Resource model

The `resource` field describes what was acted on.

```json
{
  "type":   "http_endpoint",
  "path":   "/api/orders",
  "method": "POST"
}
```

| Field | Type | Description |
|---|---|---|
| `type` | `string` | `http_endpoint`, `egress_route`, or `config`. |
| `path` | `string` | URL path, egress route name, or file path. |
| `method` | `string` | HTTP method. Only present for `http_endpoint`. |

**Resource types:**

- `http_endpoint` — an inbound HTTP request handled by the sidecar proxy.
- `egress_route` — a named outbound route to an external service via the egress proxy.
- `config` — the VibeWarden configuration file.

---

## Outcome enum

The `outcome` field is an enforcement decision made by the sidecar.

| Value | Meaning |
|---|---|
| `allowed` | The request or action was permitted and forwarded. |
| `blocked` | The request was rejected by a security policy. |
| `rate_limited` | The request was rejected because the caller exceeded a rate limit. |
| `failed` | The action failed due to an internal or transport error. |
| _(empty)_ | The event is informational — no enforcement decision was made. |

---

## Risk signals

The `risk_signals` array contains zero or more machine-detectable threat
indicators. Each signal is independent; multiple signals may be present on a
single event.

```json
"risk_signals": [
  {
    "signal":  "prompt_injection",
    "score":   1.0,
    "details": "pattern \"ignore previous instructions\" matched at .messages[0].content"
  }
]
```

| Field | Type | Description |
|---|---|---|
| `signal` | `string` | Stable identifier for the risk pattern (e.g. `"rate_limit_exceeded"`, `"prompt_injection"`). |
| `score` | `float64` | Normalised risk score in `[0.0, 1.0]`. Higher = higher confidence this is a genuine threat. |
| `details` | `string` | Human-readable explanation of why the signal was raised. |

**Known signals:**

| Signal | When |
|---|---|
| `rate_limit_exceeded` | Emitted on `rate_limit.hit` and `egress.rate_limit_hit` events. Score: `0.5`. |
| `prompt_injection` | Emitted on `llm.prompt_injection_blocked` (score `1.0`) and `llm.prompt_injection_detected` (score `0.9`). |

---

## Causal chain fields

These envelope fields let agents and observability tools link related events:

| Field | How to use |
|---|---|
| `request_id` | Groups all events emitted during a single inbound HTTP request. Set from the `X-Request-ID` header; generated by VibeWarden when absent. |
| `trace_id` | W3C trace context. Correlates events to distributed traces in Jaeger, Tempo, etc. |
| `triggered_by` | Names the middleware or subsystem that emitted the event. Useful for understanding the enforcement layer. |

---

## Event types

This section lists every event type. Use the MCP tool
`vibewarden_schema_describe` with `event_type=<type>` for full field listings,
or with no arguments to see a condensed version of this table.

### Proxy

| Event type | Description |
|---|---|
| `proxy.started` | Reverse proxy started and is ready to accept connections. |
| `proxy.kratos_flow` | Request routed to Ory Kratos self-service flow API. |

### Auth — Kratos sessions

| Event type | Description |
|---|---|
| `auth.success` | Valid Kratos session — request allowed through to the upstream. |
| `auth.failed` | Missing, invalid, or expired Kratos session — request rejected. |
| `auth.provider_unavailable` | Ory Kratos is unreachable (emitted once per transition). |
| `auth.provider_recovered` | Ory Kratos is reachable again after unavailability. |

### Auth — API keys

| Event type | Description |
|---|---|
| `auth.api_key.success` | Request authenticated via a valid API key. |
| `auth.api_key.failed` | API key missing, invalid, or inactive. |
| `auth.api_key.forbidden` | Valid key lacks required scopes for the requested path/method. |

### Auth — JWT

| Event type | Description |
|---|---|
| `auth.jwt_valid` | JWT token passed all validation checks. |
| `auth.jwt_invalid` | JWT failed validation (bad signature, wrong issuer/audience, parse error). |
| `auth.jwt_expired` | JWT is structurally valid but past its expiry time. |
| `auth.jwks_refresh` | JWKS cache successfully refreshed. |
| `auth.jwks_error` | Fetching or parsing the JWKS failed. |

### Rate limiting

| Event type | Description |
|---|---|
| `rate_limit.hit` | Per-IP or per-user rate limit exceeded — request rejected. |
| `rate_limit.unidentified_client` | Rate limit check failed because client IP could not be determined. |
| `rate_limit.store_fallback` | Redis unavailable — rate limiter switched to in-memory store. |
| `rate_limit.store_recovered` | Redis available again — rate limiter switched back from in-memory. |

### Request blocking

| Event type | Description |
|---|---|
| `request.blocked` | Request blocked by a middleware policy (not auth or rate limiting). |

### TLS

| Event type | Description |
|---|---|
| `tls.certificate_issued` | TLS certificate obtained or renewed. |
| `tls.cert_expiry_warning` | Certificate expires within 30 days. |
| `tls.cert_expiry_critical` | Certificate expires within 7 days. |

### User management

| Event type | Description |
|---|---|
| `user.created` | New user identity created. |
| `user.deleted` | User identity deleted. |
| `user.deactivated` | User identity deactivated (auth prevented, record retained). |

### Audit

| Event type | Description |
|---|---|
| `audit.log_failure` | Audit entry could not be persisted to the backing store. |

### IP filter

| Event type | Description |
|---|---|
| `ip_filter.blocked` | Request rejected by IP filter (allowlist or blocklist). |

### Secrets

| Event type | Description |
|---|---|
| `secret.rotated` | Dynamic secret rotated successfully. |
| `secret.rotation_failed` | Dynamic secret rotation failed; old credentials remain active. |
| `secret.health_check` | Scheduled secret health check run completed. |

### Upstream

| Event type | Description |
|---|---|
| `upstream.timeout` | Upstream did not respond within configured timeout; 504 returned. |
| `upstream.retry` | Retry middleware re-sending a failed upstream request. |
| `upstream.health_changed` | Upstream health status changed (unknown → healthy → unhealthy). |

### Circuit breaker (inbound)

| Event type | Description |
|---|---|
| `circuit_breaker.opened` | Circuit tripped — upstream blocked until probe succeeds. |
| `circuit_breaker.half_open` | Circuit in half-open state — probe request allowed through. |
| `circuit_breaker.closed` | Circuit closed — upstream recovered, normal traffic resumed. |

### Config

| Event type | Description |
|---|---|
| `config.reloaded` | Configuration reloaded and applied. |
| `config.reload_failed` | Configuration reload failed; old config remains active. |

### Maintenance mode

| Event type | Description |
|---|---|
| `maintenance.request_blocked` | Request rejected because maintenance mode is enabled. |

### Webhooks

| Event type | Description |
|---|---|
| `webhook.signature_valid` | Inbound webhook request passed signature verification. |
| `webhook.signature_invalid` | Inbound webhook request rejected due to invalid/missing signature. |

### Egress proxy

| Event type | Description |
|---|---|
| `egress.request` | Egress proxy started forwarding an outbound request. |
| `egress.response` | Egress proxy received a complete response from the external service. |
| `egress.blocked` | Egress proxy blocked an outbound request (policy or security rule). |
| `egress.error` | Egress proxy encountered a transport-level error. |
| `egress.circuit_breaker.opened` | Per-route egress circuit breaker tripped. |
| `egress.circuit_breaker.closed` | Per-route egress circuit breaker recovered. |
| `egress.response_invalid` | Upstream egress response failed per-route validation rules. |
| `egress.rate_limit_hit` | Per-route egress rate limit exceeded. |
| `egress.sanitized` | PII redaction applied to an outbound egress request. |

### LLM safety

| Event type | Description |
|---|---|
| `llm.prompt_injection_blocked` | Prompt injection pattern detected and request blocked (action: block). |
| `llm.prompt_injection_detected` | Prompt injection pattern detected, request forwarded (action: detect). |
| `llm.response_invalid` | Upstream LLM response failed JSON Schema validation. |

### Agent proposals

| Event type | Description |
|---|---|
| `agent.proposal_created` | MCP agent created a configuration-change proposal pending human review. |
| `agent.proposal_approved` | Human admin approved a proposal; change applied. |
| `agent.proposal_dismissed` | Human admin dismissed a proposal; change not applied. |

---

## Example events

### auth.success

```json
{
  "schema_version": "v1",
  "event_type":     "auth.success",
  "timestamp":      "2026-04-03T14:32:10.123Z",
  "ai_summary":     "Authenticated request allowed: GET /api/orders (identity a1b2c3d4-e5f6-...)",
  "actor": {
    "type": "user",
    "id":   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "ip":   "203.0.113.42"
  },
  "resource": {
    "type":   "http_endpoint",
    "path":   "/api/orders",
    "method": "GET"
  },
  "outcome":     "allowed",
  "request_id":  "req_01HZ123ABC",
  "trace_id":    "4bf92f3577b34da6a3ce929d0e0e4736",
  "triggered_by": "auth_middleware",
  "payload": {
    "method":      "GET",
    "path":        "/api/orders",
    "session_id":  "sess_01HZ456DEF",
    "identity_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "email":       "alice@example.com"
  }
}
```

### rate_limit.hit

```json
{
  "schema_version": "v1",
  "event_type":     "rate_limit.hit",
  "timestamp":      "2026-04-03T14:32:11.456Z",
  "ai_summary":     "Rate limit exceeded for ip 203.0.113.99: 10 requests/second limit reached",
  "actor": {
    "type": "ip",
    "id":   "203.0.113.99",
    "ip":   "203.0.113.99"
  },
  "resource": {
    "type":   "http_endpoint",
    "path":   "/api/login",
    "method": "POST"
  },
  "outcome": "rate_limited",
  "risk_signals": [
    {
      "signal":  "rate_limit_exceeded",
      "score":   0.5,
      "details": "ip 203.0.113.99 exceeded 10 req/s"
    }
  ],
  "triggered_by": "rate_limit_middleware",
  "payload": {
    "limit_type":          "ip",
    "identifier":          "203.0.113.99",
    "requests_per_second": 10,
    "burst":               20,
    "retry_after_seconds": 5,
    "path":                "/api/login",
    "method":              "POST"
  }
}
```

### egress.request

```json
{
  "schema_version": "v1",
  "event_type":     "egress.request",
  "timestamp":      "2026-04-03T14:32:12.789Z",
  "ai_summary":     "Egress request started: POST https://api.openai.com/v1/chat/completions via route \"openai\"",
  "actor": { "type": "system" },
  "resource": {
    "type":   "egress_route",
    "path":   "openai",
    "method": "POST"
  },
  "triggered_by": "egress_proxy",
  "payload": {
    "route":    "openai",
    "method":   "POST",
    "url":      "https://api.openai.com/v1/chat/completions",
    "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
  }
}
```

### llm.prompt_injection_blocked

```json
{
  "schema_version": "v1",
  "event_type":     "llm.prompt_injection_blocked",
  "timestamp":      "2026-04-03T14:32:13.100Z",
  "ai_summary":     "Prompt injection blocked on route \"openai\": pattern \"ignore_previous\" matched in .messages[0].content",
  "actor": { "type": "system" },
  "resource": {
    "type":   "egress_route",
    "path":   "openai",
    "method": "POST"
  },
  "outcome": "blocked",
  "risk_signals": [
    {
      "signal":  "prompt_injection",
      "score":   1.0,
      "details": "pattern \"ignore_previous\" matched at .messages[0].content"
    }
  ],
  "triggered_by": "prompt_injection_middleware",
  "payload": {
    "route":        "openai",
    "method":       "POST",
    "url":          "https://api.openai.com/v1/chat/completions",
    "pattern":      "ignore_previous",
    "content_path": ".messages[0].content",
    "action":       "block"
  }
}
```

### config.reload_failed

```json
{
  "schema_version": "v1",
  "event_type":     "config.reload_failed",
  "timestamp":      "2026-04-03T14:45:00.000Z",
  "ai_summary":     "Configuration reload failed for /etc/vibewarden/vibewarden.yaml (source: file_watcher): validation error",
  "actor": { "type": "system" },
  "resource": {
    "type": "config",
    "path": "/etc/vibewarden/vibewarden.yaml"
  },
  "outcome":      "failed",
  "triggered_by": "file_watcher",
  "payload": {
    "config_path":       "/etc/vibewarden/vibewarden.yaml",
    "trigger_source":    "file_watcher",
    "reason":            "validation error",
    "validation_errors": [
      "rate_limit.per_ip.requests_per_second must be greater than zero"
    ]
  }
}
```

### agent.proposal_created

```json
{
  "schema_version": "v1",
  "event_type":     "agent.proposal_created",
  "timestamp":      "2026-04-03T15:00:00.000Z",
  "ai_summary":     "Proposal 7f3e1b2a-... created by mcp_agent: block_ip — 203.0.113.99 triggered 47 rate limit events in 60s",
  "actor": {
    "type": "system",
    "id":   "mcp_agent"
  },
  "resource": { "type": "config" },
  "outcome":      "allowed",
  "triggered_by": "mcp_agent",
  "payload": {
    "proposal_id": "7f3e1b2a-c4d5-6789-abcd-ef0123456789",
    "action_type": "block_ip",
    "reason":      "203.0.113.99 triggered 47 rate limit events in 60s",
    "source":      "mcp_agent"
  }
}
```

---

## How to adopt this schema in your app

VibeWarden logs events from the proxy layer. If your application emits its own
logs and you want them to be machine-readable alongside VibeWarden events:

1. **Use the same top-level envelope.** Add `schema_version`, `event_type`,
   `timestamp`, and `ai_summary` to every log entry.

2. **Reuse the `actor`, `resource`, and `outcome` shapes.** An AI agent that
   understands VibeWarden logs will immediately understand your logs too.

3. **Pick meaningful, stable `event_type` values.** Use dot-separated
   namespaces (`order.created`, `payment.failed`) and never change them.

4. **Write `ai_summary` as a complete sentence** that includes the most
   important identifiers so an LLM can answer "what happened?" from the
   summary alone.

5. **Use structured payloads, not format strings.** Put each data point in its
   own key rather than interpolating it into a message string.

There is no library to install — the schema is a convention, not a framework.
