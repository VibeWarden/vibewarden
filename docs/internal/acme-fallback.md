# ACME Fallback Chain — Internal Reference

> This file was relocated from `decisions/adr-079-acme-fallback-chain-multi-issuer.md`
> on 2026-05-04 as part of the ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`).
> The stub at that path remains stable; existing PR / commit references continue to resolve.
>
> **Partial supersession note**: ADR-083 partially superseded the content here by
> revising the default issuer chain (Buypass removed; ZeroSSL email preflight added).
> See `decisions/adr-083-acme-chain-hardening-email-preflight-buypass-removed.md` for
> the current chain specification.

## From ADR-079 — ACME Fallback Chain — Multi-Issuer Automatic Failover

**Status**: Accepted (partially superseded by ADR-083 — default chain revised; Buypass removed).

**Date**: 2026-04-20
**Issue**: #1026

### Context

Let's Encrypt occasionally suffers outages or rate-limits. Caddy natively supports multiple
ACME issuers per automation policy, trying each in sequence when the previous fails.
ADR-079 introduced a 3-issuer ACME fallback chain for `provider: letsencrypt`.

### Decision

Introduce a 3-issuer ACME fallback chain for the default `provider: letsencrypt`
configuration, and add first-class provider values for `zerossl`, `buypass`, and
`letsencrypt-staging`.

#### New provider constants (in `internal/ports/proxy.go`)

| Constant | Value | Behaviour |
|----------|-------|-----------|
| `TLSProviderZeroSSL` | `"zerossl"` | Single issuer: ZeroSSL ACME. Requires `email`. |
| `TLSProviderBuypass` | `"buypass"` | Single issuer: Buypass Go SSL ACME. |
| `TLSProviderLEStaging` | `"letsencrypt-staging"` | Single issuer: LE staging CA. |

#### Original 3-issuer chain (superseded by ADR-083)

The original default chain was: Let's Encrypt → ZeroSSL → Buypass.

**ADR-083 revised this**: Buypass was removed from the default chain (upstream reliability
issues); ZeroSSL now requires email preflight. See ADR-083 for the current chain.

#### Single-issuer providers

When `provider` is set to `zerossl` or `buypass`, Caddy uses only that issuer with no
fallback. `letsencrypt-staging` is for testing only.

#### `acme_ca` escape hatch

For any ACME CA not covered by the first-class provider values, users can set
`tls.acme_ca` to any ACME directory URL. This bypasses the multi-issuer chain.

### Current behaviour (post ADR-083)

See `decisions/adr-083-acme-chain-hardening-email-preflight-buypass-removed.md` for
the live specification of the fallback chain and the email preflight requirement.
