# ADR-080: Deploy Health Check Diagnostic Classification

## Status

Accepted

## Context

When `vibew deploy` fails at the health check step, the user sees a generic
"health check failed" error with no indication of *why* it failed. The most
common failure modes are:

1. **Container crashed** (exited, OOM, bad config)
2. **TLS certificate error** (ACME challenge failed, DNS misconfigured)
3. **Upstream unreachable** (app not listening, wrong port)
4. **Timeout** (slow start, no specific signal)

Without classification, users resort to SSH-ing into the server and running
manual diagnostic commands. This defeats the zero-friction deploy experience.

## Decision

On health check failure, the deploy service runs SSH diagnostic commands to
classify the failure into one of five categories:

- `container_unhealthy` — container exited, restarting, or Docker reports unhealthy
- `tls_error` — TLS/ACME errors found in sidecar logs
- `upstream_unreachable` — sidecar running but upstream returns 502/503
- `timeout` — no specific signal detected
- `unknown` — catch-all

### Implementation

- **Domain value object**: `health.Diagnostic` with `health.FailureCategory`
  constants in `internal/domain/health/`
- **App-layer error type**: `HealthCheckError` wraps `ErrHealthCheck` with a
  `Diagnostic` field, preserving `errors.Is` compatibility
- **Diagnostic method**: `Service.diagnoseHealthFailure()` in
  `internal/app/deploy/health.go` runs SSH commands via the existing
  `ports.RemoteExecutor` — no new ports or adapters introduced
- **Classification logic**: check container status first (most severe), then
  TLS log patterns, then upstream probe, then fall back to timeout

### Diagnostic SSH commands

1. `docker compose ps` — container status
2. `docker compose logs vibewarden --tail=30` — recent sidecar logs
3. `docker compose ps vibewarden` — sidecar-specific status for upstream probe
4. `curl -sk -o /dev/null -w '%{http_code}' <url>` — HTTP code probe

## Consequences

- Deploy failures now include actionable output: category, detail, container
  status, and relevant log lines
- Existing `errors.Is(err, ErrHealthCheck)` checks continue to work via Unwrap
- No new dependencies, ports, or adapters — reuses existing RemoteExecutor
- Small additional latency on failure path only (3-4 SSH commands after timeout)
