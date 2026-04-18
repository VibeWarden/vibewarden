# ADR-069: Caddy Multi-Host Routes

**Date**: 2026-04-15
**Issue**: #871
**Status**: Accepted (implemented in PR #876)

### Context

With the Site domain model (ADR-068) in place, the Caddy adapter needs
to generate a single Caddy JSON configuration that serves multiple
sites on the same listen address, each with host-matched routes,
independent middleware chains, and per-domain TLS automation policies.
The existing single-site `buildConfigJSON` path must remain backward
compatible.

### Decision

1. **`BuildMultiSiteConfig` function** in
   `internal/adapters/caddy/multisite_config.go`. Accepts
   `[]*site.Site`, `site.GlobalConfig`, and an injected `*slog.Logger`.
   Iterates healthy sites, skipping those without a TLS domain or with
   errors. Produces a `map[string]any` Caddy JSON config with a single
   `vibewarden` HTTP server listening on `globalCfg.ListenHost:ListenPort`.

2. **Per-site route generation.** `buildSiteRoutes` converts each
   site's config into two Caddy routes scoped by host matcher: a
   `/_vibewarden/health` static response route (with site name in the
   JSON body) and a catch-all reverse proxy route. The middleware chain
   (security headers, rate limiting, compression) mirrors single-site
   mode but is independently configured per site.

3. **Per-domain TLS automation.** `buildMultiSiteTLSApp` generates
   one automation policy per domain. The issuer module is chosen by
   provider: `internal` for self-signed, `acme` for letsencrypt (with
   optional `acme_email`), and `load_files` for external cert/key
   pairs.

4. **Mode branching in adapter.** `NewMultiSiteAdapter` constructor in
   `internal/adapters/caddy/adapter.go` stores a `registry` field.
   `buildConfigJSON` branches: if `registry != nil`, it calls
   `BuildMultiSiteConfig` with the registry's healthy sites and global
   config; otherwise it falls back to the existing single-site path.

5. **Error isolation.** A site that fails route generation (e.g.,
   invalid upstream) is skipped with a warning log. Only if zero routes
   survive does `BuildMultiSiteConfig` return an error.

### Consequences

#### Positive

- Single Caddy instance serves all sites, sharing the listen port and
  TLS certificate cache.
- Each site has independent middleware chains; a misconfigured rate
  limit on site A does not affect site B.
- Backward compatible: single-site mode is unchanged.
- Per-domain TLS policies allow mixed providers (some sites ACME, some
  self-signed, some external certs).

#### Negative

- The `map[string]any` Caddy config construction is verbose and not
  type-safe. This matches the existing single-site pattern and is the
  pragmatic choice given Caddy's JSON API.
- All sites share a single Caddy server; a Caddy-level crash affects
  all sites. Acceptable for the sidecar model.
