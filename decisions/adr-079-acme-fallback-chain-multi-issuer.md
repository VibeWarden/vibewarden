# ADR-079: ACME Fallback Chain (Multi-Issuer)

## Status

Accepted

## Context

Let's Encrypt occasionally experiences outages or rate limits that prevent
certificate issuance. When this happens, VibeWarden users are left without
valid TLS certificates until the CA recovers. Caddy natively supports multiple
ACME issuers in a fallback chain, but VibeWarden previously only configured a
single issuer.

Additionally, users may want to use alternative ACME CAs (ZeroSSL, Buypass)
as their primary issuer for business or compliance reasons.

## Decision

1. **Default fallback chain**: When `tls.provider: letsencrypt` and no
   `acme_ca` override is set, configure 3 ACME issuers in order:
   - Let's Encrypt (production)
   - ZeroSSL
   - Buypass Go SSL

   Caddy tries them in order; if the first fails, it falls over to the next.

2. **New provider constants**: Add `zerossl`, `buypass`, and
   `letsencrypt-staging` as first-class provider values. Each configures a
   single issuer targeting that specific CA.

3. **Email field**: Add `tls.email` for ACME account registration. Required
   for `zerossl` (EAB credential auto-registration), recommended for others
   (cert expiry warnings).

4. **Backward compatibility**: Setting `acme_ca` explicitly disables the
   fallback chain and uses a single issuer with the specified CA URL. This
   preserves the behavior for users who already set `acme_ca`.

5. **Shared helper**: `buildACMEIssuers(cfg ports.TLSConfig)` is the single
   source of truth for issuer chain construction, used by both single-site
   and multi-site config builders.

## Consequences

- Users get automatic failover across CAs with zero configuration change.
- ZeroSSL and Buypass are now first-class providers for users who need them.
- The `letsencrypt-staging` provider replaces the need to manually set
  `acme_ca` to the staging URL for testing.
- All ACME issuers now include explicit `ca` URLs (previously the default LE
  issuer had no `ca` field). This is a non-breaking change for Caddy.
