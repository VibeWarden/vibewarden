# WebSocket and Streaming Pass-Through

VibeWarden proxies WebSocket connections and streaming HTTP responses
(Server-Sent Events, chunked LLM token streams) transparently. No configuration
is required — it just works.

---

## How it works

Caddy, the embedded reverse proxy, handles the HTTP `Upgrade` mechanism
automatically. When a client sends an HTTP request with:

```
Connection: Upgrade
Upgrade: websocket
```

Caddy forwards the upgrade handshake to the upstream application and then
bridges the resulting TCP connection bi-directionally. VibeWarden does not
inspect or buffer individual WebSocket frames.

---

## Security checks on the upgrade request

All security checks run on the **initial HTTP upgrade request** — the standard
HTTP `GET` that initiates the handshake. Once the connection is established,
the raw TCP stream is forwarded unchanged.

### Authentication

Auth is enforced on the upgrade request using the same mechanism as any other
request:

- **Kratos mode:** the `ory_kratos_session` cookie must be present and valid.
- **JWT mode:** `Authorization: Bearer <token>` must be present and pass JWKS
  validation.
- **API key mode:** the configured key header (default: `X-API-Key`) must
  contain a valid key.

If the upgrade request fails auth, VibeWarden returns `401 Unauthorized` and
the WebSocket connection is never opened. The upstream application never sees
the request.

### Rate limiting

Rate limiting applies to the upgrade request, not to individual WebSocket
frames. Each new WebSocket connection consumes one token from the per-IP
and per-user buckets. Messages sent over an already-established connection
are not counted.

This means a client that sends many messages over a single WebSocket
connection will not be rate limited on those messages. Rate limiting
remains effective at preventing a flood of new connection attempts.

---

## Timeouts

HTTP request timeouts configured in VibeWarden do **not** apply to established
WebSocket connections. Caddy keeps the connection alive until either side
closes it or a network error occurs. Your application controls the connection
lifetime.

The read/write timeout applies only to the upgrade handshake itself (the
initial HTTP round-trip). If your app needs idle timeouts on WebSocket
connections, implement them in the application layer (e.g. send periodic
pings and close on missed pongs).

---

## Example config

No special configuration is needed. A standard reverse-proxy setup works:

```yaml
upstream:
  url: "http://localhost:3000"

rate_limit:
  enabled: true

admin:
  enabled: true
  token: ${VIBEWARDEN_ADMIN_TOKEN}

kratos:
  admin_url: http://127.0.0.1:4434

database:
  url: postgres://<user>:<pass>@localhost:5432/vibewarden?sslmode=disable
```

With the config above, WebSocket connections to VibeWarden are automatically
proxied to `http://localhost:3000`. The upgrade handshake is authenticated and
rate-limited; individual frames are not.

---

## Server-Sent Events and streaming responses

Streaming HTTP responses are forwarded as they are produced. When your app
writes a chunk and flushes, the bytes leave VibeWarden immediately; nothing
waits for the response to complete. This covers `text/event-stream`, chunked
LLM token streams, and any other incremental response.

Internally every VibeWarden response wrapper (metrics, tracing, access log,
circuit breaker, timeout) forwards `Flush()` and exposes the writer underneath
through `Unwrap()`, so a flush survives the whole chain. Before this was fixed
(#1526) the wrappers hid `http.Flusher` and clients received nothing until the
connection closed.

What your app must do:

- Set `Content-Type: text/event-stream` (SSE) and do not set `Content-Length`.
- Flush after each event. Most frameworks buffer until you ask them not to.

### Settings that still buffer or cut a stream

| Setting | Effect on a streaming response |
|---|---|
| `resilience.timeout` (default `30s`) | Counts the **whole response** as one request. A stream held open longer than the timeout is cut when the deadline fires. Raise it, or set `"0"` to disable, if you serve long-lived SSE. |
| `resilience.retry.enabled` (default `false`) | Buffers the entire response in memory so a failed attempt can be discarded. A stream never "completes", so the client sees nothing and memory grows. Leave retry off when streaming. |
| egress `response_validation.enabled` (default `false`) | Buffers the upstream body to validate it against the JSON Schema. Applies to any egress route you enable it on, in both addressing modes: transparent (`HTTP_PROXY` + `X-Egress-URL`) and named (`/_egress/{route}`). See [Egress](egress.md). |

Timeouts, auth, and rate limiting otherwise behave exactly as for a normal
request: they are evaluated once, before the first byte is written.

---

## Structured log events

The upgrade request is logged like any other HTTP request
(`event_type: request.proxied`). There is no separate event type for
WebSocket frame activity because frames are not inspected.

A failed upgrade due to auth is logged as `event_type: auth.denied`.
A failed upgrade due to rate limiting is logged as
`event_type: rate_limit.blocked`.

---

## Summary

| Concern | Behaviour |
|---|---|
| Upgrade handshake | Caddy handles `Connection: Upgrade` automatically |
| Authentication | Checked on the upgrade request; deny = `401`, connection never opened |
| Rate limiting | One token consumed per new connection, not per frame |
| Timeouts | Not applied to established connections; only to the upgrade handshake |
| Frame inspection | Not performed; Caddy bridges the raw TCP stream |
| SSE / chunked streaming | Flushed through unbuffered; `resilience.retry` and egress `response_validation` buffer by design |
| Config changes needed | None (raise `resilience.timeout` for long-lived streams) |
