# ADR-497: Graceful Shutdown / Connection Draining


**Issue:** #497
**Status:** CLOSED — already implemented
**Date:** 2026-03-28

### Context

Issue #497 asked whether the VibeWarden sidecar handles SIGTERM/SIGINT gracefully
(i.e., drains in-flight connections before exiting) and, if not, to add proper
signal handling with a shutdown timeout.

### Decision

No code changes required. The full graceful-shutdown pipeline was already in place
before this issue was raised:

1. **Signal capture** (`cmd/vibewarden/serve.go`): A goroutine calls
   `signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)` and, on receipt of
   either signal, calls `cancel()` on the root `context.WithCancel` context.
   Signal handling is set up *before* `registry.InitAll` so that even a slow
   plugin initialisation can be interrupted.

2. **Proxy service shutdown** (`internal/app/proxy/service.go`): `Service.Run`
   blocks on `ctx.Done()`. When the context is cancelled it calls
   `server.Stop(shutdownCtx)` with a dedicated `context.WithTimeout` set to
   `30 seconds` (`defaultShutdownTimeout`). This gives in-flight HTTP requests
   up to 30 seconds to complete.

3. **Caddy adapter** (`internal/adapters/caddy/adapter.go`): `Stop` delegates
   directly to `caddy.Stop()`. Caddy's own shutdown logic drains active
   connections and releases listeners before returning.

4. **Plugin registry cleanup** (`cmd/vibewarden/serve.go`): A `defer` block
   calls `registry.StopAll(stopCtx)` with a `10-second` timeout, ensuring that
   background plugins (metrics exporter, egress proxy, etc.) are stopped cleanly
   after the main proxy has drained.

The shutdown sequence is therefore:

```
SIGTERM / SIGINT
  └─> context cancel
        └─> proxy.Service.Run exits select
              └─> caddy.Stop() drains connections (30 s budget)
                    └─> registry.StopAll() stops plugins (10 s budget)
```

### Verification

Existing unit tests in `internal/app/proxy/service_test.go` cover:

- `TestService_Run_ContextCancellation` — verifies `Stop` is called on context
  cancellation and the service returns `context.Canceled`.
- `TestService_Run_ServerError` — verifies early server errors propagate.
- `TestService_Run_StopError` — verifies stop errors propagate correctly.

### Consequences

**Positive:**

- Zero downtime deploys and container restarts are safe: Kubernetes / systemd
  both send SIGTERM and wait for the process to exit.
- Active connections complete normally within the 30-second drain window.
- Plugin teardown (flush metrics, close DB connections) runs within 10 seconds
  even if the proxy drain is slow or completes early.

**No new dependencies.** The implementation uses only stdlib (`os/signal`,
`syscall`, `context`) and existing Caddy APIs.
- JWKS URL must be HTTPS in production (not enforced in code for local dev).

---
