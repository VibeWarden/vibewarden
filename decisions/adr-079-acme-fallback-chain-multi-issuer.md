## ADR-079: ACME Fallback Chain — Multi-Issuer Automatic Failover
**Date**: 2026-04-20
**Issue**: #1026
**Status**: Accepted

### Context

Let's Encrypt occasionally suffers outages or rate-limits that prevent
certificate issuance. When this happens, a VibeWarden deployment cannot obtain
or renew TLS certificates, causing downtime.

Caddy natively supports multiple ACME issuers per automation policy. When the
first issuer fails, Caddy automatically tries the next one. This is a
zero-configuration reliability improvement.

Additionally, users may want to target a specific ACME CA (ZeroSSL, Buypass,
Let's Encrypt staging) without using the raw `acme_ca` escape hatch.

### Decision

Introduce a 3-issuer ACME fallback chain for the default `provider: letsencrypt`
configuration, and add first-class provider values for `zerossl`, `buypass`, and
`letsencrypt-staging`.

#### New provider constants (in `internal/ports/proxy.go`)

| Constant | Value | Behaviour |
|----------|-------|-----------|
| `TLSProviderZeroSSL` | `"zerossl"` | Single issuer: ZeroSSL ACME. Requires `email`. |
| `TLSProviderBuypass` | `"buypass"` | Single issuer: Buypass Go SSL ACME. |
| `TLSProviderLetsEncryptStaging` | `"letsencrypt-staging"` | Single issuer: LE staging. |

#### Fallback chain (default)

When `provider: letsencrypt` is used **without** an `acme_ca` override:

1. Let's Encrypt production
2. ZeroSSL
3. Buypass Go SSL

When `acme_ca` is set, the fallback chain is disabled (single issuer with the
custom URL) for backward compatibility.

#### Shared helper

`buildACMEIssuers(cfg ports.TLSConfig) []map[string]any` is the single source
of truth for issuer chain construction, used by:
- `internal/adapters/caddy/acme_issuers.go` (adapter-level)
- `internal/plugins/tls/plugin.go` (plugin-level)
- `internal/adapters/caddy/multisite_config.go` (multi-site mode)

#### Email requirement

- `zerossl` provider: `tls.email` is **required** (for automatic EAB).
- All other ACME providers: `tls.email` is **recommended** (for expiry notifications).

#### CLI

`vibew add tls --email` flag registered for ACME account email.

### Consequences

- Default `provider: letsencrypt` deployments gain automatic failover across 3
  CAs with zero configuration change.
- Users who need a specific CA can use `provider: zerossl`, `provider: buypass`,
  or `provider: letsencrypt-staging` as first-class values.
- The `acme_ca` escape hatch remains for arbitrary ACME directories.
- No new external dependencies.
- Existing configs are backward-compatible: the only observable change is that
  the ACME issuer now always has an explicit `ca` field in the generated Caddy
  JSON (previously omitted when using the default LE directory).
