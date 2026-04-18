# ADR-001: Plugin architecture — config-driven, compiled-in (v1)

**Date**: 2026-03-20
**Status**: Accepted

### Context
VibeWarden targets vibe coders who need zero-to-secure in minutes. The question was
whether plugins should be installed via CLI (`vibewarden plugin install x`) or
compiled into the binary and activated via config.

A CLI install model requires network access at install time, introduces plugin versioning
complexity, and adds friction for the target user. A build-tags model requires Go toolchain
on the user's machine, which contradicts the distribution model.

### Decision
All plugins are compiled into the official Docker image and OS installer binaries.
Users activate plugins via `vibewarden.yaml` — no install step, no network call, no
version mismatch between plugin and core.

Plugin config pattern:
```yaml
plugins:
  tls:
    enabled: true
    provider: letsencrypt   # or: external (user manages certs), self-signed (dev)
  user-management:
    enabled: true
    adapter: postgres
  rate-limiting:
    enabled: true
  grafana:
    enabled: false
```

### Consequences
- Binary is larger (contains all plugin code) — acceptable tradeoff for v1 simplicity
- CLI install model deferred to v2 if community demand justifies it
- `provider: external` handles users who already manage TLS via Cloudflare, registrar, etc.

---
