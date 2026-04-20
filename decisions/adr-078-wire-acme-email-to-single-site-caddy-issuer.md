## ADR-078: Wire acme_email to Single-Site Caddy ACME Issuer
**Date**: 2026-04-20
**Issue**: #1027
**Status**: Accepted

### Context

The `acme_email` field is accepted in config and correctly wired to the Caddy
ACME issuer in **multi-site** mode (`multisite_config.go`) but completely
ignored in **single-site** mode (`config.go`). This causes two problems:

1. ZeroSSL ACME fails in single-site mode because it requires an email for
   automatic EAB registration.
2. Let's Encrypt certificate expiry notifications are never sent because no
   contact email is registered with the ACME account.

The demo deploy agent had to PATCH the Caddy admin API directly to inject the
email — a workaround that is lost on container restart.

### Decision

Add an `Email` field to the `ports.TLSConfig` struct and wire it into the ACME
issuer JSON object in `buildLetsEncryptTLSApp()`, matching the existing pattern
in `buildMultiSiteTLSApp()`.

#### Modified files

| File | Change |
|------|--------|
| `internal/ports/proxy.go` | Add `Email string` field to `TLSConfig` struct |
| `internal/config/config.go` | Add `Email string` with `mapstructure:"email"` tag |
| `internal/adapters/caddy/config.go` | Wire `cfg.Email` into ACME issuer in `buildLetsEncryptTLSApp` |
| `cmd/vibewarden/wiring_serve_helpers.go` | Pass `cfg.TLS.Email` through to `ports.TLSConfig` |
| `internal/app/eject/eject.go` | Pass `cfg.TLS.Email` through to `ports.TLSConfig` |
| `internal/adapters/caddy/config_test.go` | Table-driven test for email presence in ACME issuer |

#### YAML config example

```yaml
tls:
  enabled: true
  provider: letsencrypt
  domain: myapp.example.com
  email: admin@example.com        # <-- new field
  acme_ca: https://acme.zerossl.com/v2/DV90  # optional
```

#### Behaviour

- When `email` is non-empty, `"email": "<value>"` is added to the ACME issuer
  JSON object in the Caddy TLS automation policy.
- When `email` is empty (default), the field is omitted — backward compatible
  with Let's Encrypt which does not require it.

### Consequences

- ZeroSSL works in single-site mode without any workaround.
- Let's Encrypt sends expiry notifications to the configured email.
- No breaking changes — existing configs without `email` continue to work.
- The single-site and multi-site code paths now have consistent ACME email
  handling.
