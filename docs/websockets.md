# WebSocket Pass-Through

VibeWarden proxies WebSocket connections transparently. No configuration is
required — it just works.

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

plugins:
  rate-limiting:
    enabled: true
  user-management:
    enabled: true
```

With the config above, WebSocket connections to VibeWarden are automatically
proxied to `http://localhost:3000`. The upgrade handshake is authenticated and
rate-limited; individual frames are not.

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
| Config changes needed | None |
