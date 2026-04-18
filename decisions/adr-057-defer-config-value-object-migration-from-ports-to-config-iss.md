# ADR-057: Defer config value-object migration from ports/ to config/ (issue #527)

**Date**: 2026-03-28
**Status**: Deferred — post-1.0

### Context

Issue #527 identified that `internal/ports/` contains plain data structs that carry
no interface behavior and arguably belong in `internal/config/` or a dedicated
`internal/config/proxy/` package. Examples:

| Type | File |
|---|---|
| `ProxyConfig`, `TLSConfig`, `TLSProvider*` constants | `ports/proxy.go` |
| `SecurityHeadersConfig` | `ports/proxy.go` |
| `ResilienceConfig`, `RetryConfig` | `ports/proxy.go` |
| `IPFilterConfig`, `BodySizeConfig`, `BodySizeOverride` | `ports/proxy.go` |
| `AdminProxyConfig`, `MetricsProxyConfig`, `ReadinessProxyConfig` | `ports/proxy.go` |
| `AdminAuthConfig` | `ports/admin_auth.go` |
| `AuthConfig`, `KratosUnavailableBehavior` | `ports/auth.go` |
| `MetricsConfig` | `ports/metrics.go` |
| `RateLimitConfig`, `RateLimitRule`, `RateLimitResult` | `ports/ratelimit.go` |
| `CircuitBreakerConfig` | `ports/circuit_breaker.go` |

The root cause is that `internal/config/config.go` already defines structurally
equivalent types (`config.TLSConfig`, `config.RateLimitConfig`, etc.) for YAML
deserialization. `cmd/vibewarden/serve_config.go` then performs explicit field-by-field
copies from `config.*` into `ports.*` when building `ports.ProxyConfig`. The `ports`
versions carry no `mapstructure` tags and exist purely to keep the adapters and
middleware decoupled from `internal/config`.

The ideal end state would be one of:
- A dedicated `internal/config/proxy/` package holding the ports-facing config
  structs, imported by both the YAML loader and the adapters (eliminating the
  field-copy boilerplate).
- Or simply re-using `config.*` types directly in the ports layer after verifying
  the import graph remains acyclic.

### Why deferred

A grep across the repository identified **55 unique Go files** referencing config
types from the `ports` package (`ports.SecurityHeadersConfig`, `ports.RateLimitConfig`,
`ports.ResilienceConfig`, etc.), spread across:

- `internal/adapters/caddy/` — 14 files (including integration tests)
- `internal/adapters/ratelimit/` — 8 files
- `internal/adapters/resilience/` — 3 files
- `internal/middleware/` — 11 files
- `internal/plugins/` — 3 files
- `cmd/vibewarden/` — 2 files
- `test/benchmarks/` — 1 file

This exceeds the threshold for a safe pre-1.0 refactor. Touching 55 files across
adapters, middleware, and tests in a single PR creates significant merge risk and a
large surface for subtle regressions with no user-visible benefit.

### Decision

Defer the migration to post-1.0. No code changes are made in this ADR.

The current design — duplicate config structs in `ports/` with explicit mapping in
`serve_config.go` — is verbose but correct. It keeps the import graph clean and the
adapters free of `mapstructure` tags.

### Post-1.0 plan

When tackling this after v1 ships:

1. Create `internal/config/proxy/` package with the ports-facing structs (no
   `mapstructure` tags, no viper dependency).
2. Delete the duplicate definitions from `internal/ports/`.
3. Update all 55 callers in a single atomic commit.
4. Remove the field-copy boilerplate in `serve_config.go` by sharing the struct
   directly between loader and adapter.
5. Ensure `go vet ./...` and all integration tests pass before merging.

---
