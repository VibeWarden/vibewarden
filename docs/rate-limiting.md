# Rate Limiting

VibeWarden enforces two independent token-bucket rate limits on every proxied request:

- **per_ip** — applied to every request, keyed by client IP address.
- **per_user** — applied only to authenticated requests, keyed by user ID.

Both limits must pass. A request failing either limit receives `429 Too Many Requests` with:

```
Retry-After: <seconds>

{"error":"rate_limit_exceeded","retry_after_seconds":<N>}
```

Rate limit activity is captured as structured events in the AI-readable log stream
(`event_type: rate_limit.blocked`).

---

## Backing stores

VibeWarden supports two backing stores, selected by `rate_limit.store`:

| Store      | State scope           | Restarts       | Multi-instance |
|------------|-----------------------|----------------|----------------|
| `memory`   | Per-process           | Resets counters | Not shared     |
| `redis`    | Redis server          | Preserved       | Shared         |

---

## Single instance: in-memory store (default)

No external dependencies. Counters live in process memory and reset when VibeWarden
restarts. This is the right choice for:

- Local development
- Single-instance deployments where occasional counter resets on redeploy are acceptable
- Any setup where you do not want to operate a Redis server

```yaml
rate_limit:
  enabled: true
  store: memory   # default; this line is optional

  per_ip:
    requests_per_second: 10
    burst: 20

  per_user:
    requests_per_second: 100
    burst: 200
```

**Consistency note:** In-memory counters are not shared across deployments. If you
restart VibeWarden (e.g. during a deploy) every client's counter resets to zero.
This is acceptable for single-instance workloads.

---

## Multi-instance: Redis store for shared counters

When you run more than one VibeWarden instance behind a load balancer, in-memory
counters allow clients to exceed their intended limit by routing requests across
instances. The Redis store solves this by keeping all counters in a shared Redis
server.

The token bucket logic runs as an atomic Lua script inside Redis, so no two
instances can race on the same key.

```yaml
rate_limit:
  enabled: true
  store: redis

  redis:
    address: "localhost:6379"   # or use url: (see below)
    password: ""
    db: 0
    pool_size: 10
    key_prefix: vibewarden
    fallback: true
    health_check_interval: 30s

  per_ip:
    requests_per_second: 10
    burst: 20

  per_user:
    requests_per_second: 100
    burst: 200
```

**Local Redis via Docker Compose:**

When `rate_limit.store` is `redis` and no `rate_limit.redis.url` is set,
`vibew init` adds a `redis` service to the generated `docker-compose.yml`
automatically. No manual configuration is needed for local dev.

---

## Config reference

### `rate_limit`

| Field                 | Type    | Default  | Description |
|-----------------------|---------|----------|-------------|
| `enabled`             | bool    | `true`   | Enable or disable rate limiting |
| `store`               | string  | `memory` | Backing store: `memory` or `redis` |
| `trust_proxy_headers` | bool    | `false`  | Read `X-Forwarded-For` for the real client IP. Enable only when VibeWarden is behind a trusted proxy. |
| `exempt_paths`        | list    | `[]`     | Glob patterns that bypass rate limiting. `/_vibewarden/*` is always exempt. |

### `rate_limit.per_ip` and `rate_limit.per_user`

| Field                  | Type    | Default (`per_ip`) | Default (`per_user`) | Description |
|------------------------|---------|--------------------|----------------------|-------------|
| `requests_per_second`  | float   | `10`               | `100`                | Sustained token refill rate |
| `burst`                | int     | `20`               | `200`                | Maximum tokens that can accumulate |

`burst` should always be >= `requests_per_second`. Setting `burst` to
`requests_per_second` disables any burst tolerance — every request must arrive no
faster than the sustained rate.

### `rate_limit.redis`

Only read when `store` is `redis`.

| Field                   | Type   | Default             | Description |
|-------------------------|--------|---------------------|-------------|
| `url`                   | string | `""`                | Full Redis URL (`redis://` or `rediss://` for TLS). Takes precedence over all other fields when set. |
| `address`               | string | `localhost:6379`    | Redis server address in `host:port` form. Used when `url` is empty. |
| `password`              | string | `""`                | Redis `AUTH` password. Leave empty for no-auth Redis. Ignored when `url` is set. |
| `db`                    | int    | `0`                 | Logical database index (0–15). Ignored when `url` is set. |
| `pool_size`             | int    | `0` (auto)          | Maximum number of socket connections in the pool. `0` lets go-redis choose based on CPU count. |
| `key_prefix`            | string | `vibewarden`        | Namespace prefix for all Redis keys. Full key format: `<key_prefix>:ratelimit:<n>:<identifier>`. |
| `fallback`              | bool   | `true`              | Fail-open (`true`): fall back to in-memory on Redis failure. Fail-closed (`false`): deny all requests when Redis is unreachable. |
| `health_check_interval` | string | `30s`               | How often the background goroutine probes Redis for recovery after a failure. Go duration string (`"30s"`, `"1m"`). |

**URL format:**

```
redis://[:<password>@]<host>:<port>[/<db>]
rediss://[:<password>@]<host>:<port>[/<db>]    # TLS
```

Using `url` is the recommended approach for external providers because it
embeds all credentials in a single string that can be stored in a secret
manager and passed via environment variable:

```bash
VIBEWARDEN_RATE_LIMIT_REDIS_URL=rediss://:mypassword@redis.example.com:6380/0
```

---

## External Redis providers

### Upstash

Upstash provides a serverless Redis compatible with standard clients. It is a good
choice for apps deployed on platforms where running a sidecar Redis container is
not practical.

1. Create a Redis database at [console.upstash.com](https://console.upstash.com).
2. Copy the **Endpoint** and **Password** from the database details page.
3. Configure VibeWarden using the TLS URL (`rediss://`):

```yaml
rate_limit:
  store: redis
  redis:
    url: "rediss://default:${UPSTASH_REDIS_PASSWORD}@<endpoint>.upstash.io:6380"
```

Set `UPSTASH_REDIS_PASSWORD` as an environment variable. Never commit the
password in `vibewarden.yaml`.

**Pool size note:** Upstash free tier limits concurrent connections. Set
`pool_size: 5` (or lower) to stay within the limit:

```yaml
rate_limit:
  store: redis
  redis:
    url: "rediss://default:${UPSTASH_REDIS_PASSWORD}@<endpoint>.upstash.io:6380"
    pool_size: 5
```

### AWS ElastiCache

ElastiCache runs inside your VPC. VibeWarden must run in the same VPC (or be
connected via VPC Peering or Transit Gateway) to reach it.

**ElastiCache Serverless (recommended for new workloads):**

ElastiCache Serverless supports TLS and IAM-based auth. Use a `rediss://` URL:

```yaml
rate_limit:
  store: redis
  redis:
    url: "rediss://:${ELASTICACHE_PASSWORD}@<cluster-endpoint>.cache.amazonaws.com:6379"
```

**ElastiCache Cluster Mode Disabled (single shard):**

For non-cluster mode, use the Primary Endpoint:

```yaml
rate_limit:
  store: redis
  redis:
    address: "<primary-endpoint>.cache.amazonaws.com:6379"
    password: "${ELASTICACHE_AUTH_TOKEN}"   # requires transit encryption
    pool_size: 20
```

Enable **in-transit encryption** on the ElastiCache cluster and set an **Auth
token** in the cluster settings. Without transit encryption, the `password` field
and `AUTH` are not available.

### Redis Cloud

Redis Cloud (Redislabs) requires TLS and provides a connection URL in the database
dashboard.

```yaml
rate_limit:
  store: redis
  redis:
    url: "rediss://default:${REDIS_CLOUD_PASSWORD}@redis-<id>.c<num>.eu-west-1-1.ec2.redns.redis-cloud.com:12345"
```

Copy the full endpoint from the **Connect** button in the Redis Cloud console and
substitute the password via an environment variable.

---

## How rate limits work with circuit breakers and retries

### Retry-After and client back-off

When VibeWarden returns `429`, the response always includes:

```
Retry-After: <seconds>
```

Well-behaved clients (and many HTTP libraries) honour `Retry-After`. If your
upstream app or a client library retries automatically on `429`, make sure the
retry logic reads `Retry-After` and backs off accordingly. Blind retries on `429`
waste burst budget and make throttling worse.

### Interaction with per-IP and per-user limits

Both limits are checked independently. A request can be blocked by either:

- `per_ip` — the client IP has exhausted its token bucket.
- `per_user` — the authenticated user has exhausted their token bucket.

The response body's `error` field is always `rate_limit_exceeded`. Check the
structured log event (`event_type: rate_limit.blocked`, `payload.limit_type:
"ip"` or `"user"`) to determine which limit fired.

### Redis fallback and circuit breaking

The Redis store is wrapped in a `FallbackStore` that acts as a circuit breaker:

1. VibeWarden assumes Redis is healthy at startup.
2. A background goroutine probes Redis every `health_check_interval` (default: 30s).
3. If the probe fails, the store is marked unhealthy and a
   `rate_limit.store_fallback` structured event is emitted.
4. **Fail-open (`fallback: true`, default):** rate limiting continues using the
   in-memory store. Counters are no longer shared across instances, but requests
   are not blocked.
5. **Fail-closed (`fallback: false`):** all requests are denied with `429` until
   Redis recovers.
6. When Redis recovers, the store switches back automatically and a
   `rate_limit.store_recovered` event is emitted.

Choose `fallback: false` when rate limiting correctness (e.g. financial or
compliance workloads) outweighs availability concerns.

---

## Troubleshooting: why am I getting 429s?

### 1. Identify which limit fired

Check the structured logs for `rate_limit.blocked` events:

```json
{
  "event_type": "rate_limit.blocked",
  "payload": {
    "limit_type": "ip",
    "identifier": "203.0.113.42",
    "remaining": 0,
    "retry_after_seconds": 1
  }
}
```

`limit_type` is `"ip"` or `"user"`. `identifier` is the IP address or user ID.

### 2. Check Prometheus metrics

```
vibewarden_rate_limit_hits_total{limit_type="ip"}
vibewarden_rate_limit_hits_total{limit_type="user"}
```

A sudden spike in these counters indicates a traffic burst or a misbehaving client.

### 3. Verify the IP VibeWarden sees

If `trust_proxy_headers: false` (the default) and VibeWarden is behind a load
balancer, every request appears to come from the load balancer's IP. All traffic
then shares a single per-IP bucket and the limit is exhausted immediately.

Fix: enable `trust_proxy_headers: true` **only** when you trust all upstream proxies:

```yaml
rate_limit:
  trust_proxy_headers: true
```

If `trust_proxy_headers: true` and you are still seeing `429`, check that the load
balancer is actually setting `X-Forwarded-For` and that the header reaches
VibeWarden without being stripped.

### 4. Requests hitting `per_ip` when you expect `per_user`

`per_user` only applies to authenticated requests. If your auth mode is `none`,
or the request does not carry a valid session/token, VibeWarden cannot extract a
user ID and falls back to `per_ip` only.

Verify the request carries a valid credential:

- **Kratos mode:** session cookie (`ory_kratos_session`) must be present and valid.
- **JWT mode:** `Authorization: Bearer <token>` must be present and validate against JWKS.
- **API key mode:** the configured header (default: `X-API-Key`) must contain a
  valid key.

### 5. Limits are too low for your traffic pattern

Increase `burst` to absorb legitimate traffic spikes without changing the
sustained `requests_per_second` rate. For example, an API used by a single-page
app that fires many requests on page load may need:

```yaml
per_ip:
  requests_per_second: 20
  burst: 100
```

### 6. Redis fallback is active

Check logs for `rate_limit.store_fallback` events. If present, counters are no
longer shared across instances. Identify and resolve the Redis connectivity issue
(network policy, auth failure, cluster restart), then wait for the background
probe to detect recovery (up to `health_check_interval`).

Force a restart of VibeWarden to reconnect immediately if needed.

### 7. Exempting specific paths

Add paths that should never be rate limited to `exempt_paths`:

```yaml
rate_limit:
  exempt_paths:
    - "/health"
    - "/public/*"
    - "/static/*"
```

The `/_vibewarden/*` prefix (health check, metrics) is always exempt regardless
of this setting.
