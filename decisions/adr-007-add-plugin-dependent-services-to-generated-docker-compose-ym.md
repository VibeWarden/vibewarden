# ADR-007: Add Plugin-Dependent Services to Generated docker-compose.yml (OpenBao, Redis)


**Date**: 2026-03-28
**Issue**: #281
**Status**: Accepted

### Context

Epic #277 establishes that `vibewarden generate` should produce the entire runtime stack from
`vibewarden.yaml`. Currently, when plugins like `secrets` (OpenBao) or `rate-limiting` with
Redis backend are enabled, users must manually add those infrastructure services to their
compose file. This defeats the single-config-file goal.

This ADR designs the automatic inclusion of plugin-dependent services:
- **OpenBao** when `secrets.enabled: true`
- **Redis** when `rate_limit.store: redis`

Key requirements from Epic #277:
- No secrets on disk — dev mode uses hardcoded defaults, prod mode uses OpenBao
- Dev credentials are embedded in templates; prod credentials come from OpenBao
- Seed containers in dev mode populate secrets for testing

### Decision

Extend the `docker-compose.yml.tmpl` template to conditionally include OpenBao and Redis
services based on the `secrets` and `rate_limit` config sections. Add seed containers for
dev mode that populate OpenBao with the secrets defined in `secrets.inject`.

#### Domain Model Changes

No new domain entities. This is a config-driven template enhancement.

#### Ports (Interfaces)

No new interfaces required. The existing `ports.TemplateRenderer` interface is sufficient.

#### Adapters

No new adapters. The existing template adapter handles the rendering.

#### Application Service

The existing `internal/app/generate/Service.Generate()` method continues to pass the
full `*config.Config` to the template renderer. Two new helper functions are added to
support the template:

```go
// internal/app/generate/helpers.go

// NeedsOpenBao returns true if the config requires an OpenBao service in the generated compose.
func NeedsOpenBao(cfg *config.Config) bool {
    return cfg.Secrets.Enabled
}

// NeedsRedis returns true if the config requires a Redis service in the generated compose.
func NeedsRedis(cfg *config.Config) bool {
    return cfg.RateLimit.Store == "redis"
}

// NeedsSeedSecrets returns true if dev mode should seed OpenBao with demo secrets.
// This is true when secrets.enabled is true AND secrets.inject has at least one entry.
func NeedsSeedSecrets(cfg *config.Config) bool {
    if !cfg.Secrets.Enabled {
        return false
    }
    return len(cfg.Secrets.Inject.Headers) > 0 || len(cfg.Secrets.Inject.Env) > 0
}
```

These helpers are registered as template functions so the template can call them:

```go
// In the template adapter, register these as FuncMap entries
funcMap := template.FuncMap{
    "needsOpenBao":     func(cfg *config.Config) bool { return generate.NeedsOpenBao(cfg) },
    "needsRedis":       func(cfg *config.Config) bool { return generate.NeedsRedis(cfg) },
    "needsSeedSecrets": func(cfg *config.Config) bool { return generate.NeedsSeedSecrets(cfg) },
}
```

#### File Layout

Files to modify:

| File | Change |
|------|--------|
| `internal/app/generate/helpers.go` | New file with helper functions |
| `internal/app/generate/helpers_test.go` | Unit tests for helper functions |
| `internal/adapters/template/renderer.go` | Register helper functions in FuncMap |
| `internal/config/templates/docker-compose.yml.tmpl` | Add OpenBao, Redis, seed-secrets services |
| `internal/config/templates/seed-secrets.sh.tmpl` | New embedded script template for seeding |
| `internal/app/generate/service.go` | Generate seed-secrets.sh when needed |
| `internal/app/generate/service_test.go` | Add tests for new service generation |

New files:

| File | Purpose |
|------|---------|
| `internal/app/generate/helpers.go` | Helper functions for template logic |
| `internal/app/generate/helpers_test.go` | Tests for helpers |
| `internal/config/templates/seed-secrets.sh.tmpl` | Script to seed demo secrets into OpenBao |

#### Template Changes

##### OpenBao Service

Add to `docker-compose.yml.tmpl` when `secrets.enabled: true`:

```yaml
{{- if .Secrets.Enabled }}
  openbao:
    image: quay.io/openbao/openbao:2.2.0
    restart: unless-stopped
    cap_add:
      - IPC_LOCK
    environment:
      # Dev mode: in-memory storage, root token generated per run
      BAO_DEV_ROOT_TOKEN_ID: ${OPENBAO_DEV_ROOT_TOKEN:-dev-root-token}
      BAO_DEV_LISTEN_ADDRESS: "0.0.0.0:8200"
    ports:
      - "8200:8200"
    networks:
      - vibewarden
    healthcheck:
      test: ["CMD-SHELL", "BAO_ADDR=http://127.0.0.1:8200 bao status"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 5s
{{- end }}
```

##### Seed Secrets Service

Add when `secrets.enabled: true` AND `secrets.inject` has entries:

```yaml
{{- if and .Secrets.Enabled (or (len .Secrets.Inject.Headers) (len .Secrets.Inject.Env)) }}
  seed-secrets:
    image: quay.io/openbao/openbao:2.2.0
    environment:
      BAO_ADDR: http://openbao:8200
      BAO_TOKEN: ${OPENBAO_DEV_ROOT_TOKEN:-dev-root-token}
    volumes:
      - ./.vibewarden/generated/seed-secrets.sh:/seed-secrets.sh:ro
    entrypoint: sh
    command: /seed-secrets.sh
    depends_on:
      openbao:
        condition: service_healthy
    networks:
      - vibewarden
    restart: "no"
{{- end }}
```

##### Redis Service

Add when `rate_limit.store: redis`:

```yaml
{{- if eq .RateLimit.Store "redis" }}
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: ["redis-server", "--appendonly", "yes"]
    volumes:
      - redis-data:/data
    networks:
      - vibewarden
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
{{- end }}
```

##### VibeWarden Service Updates

Update the `vibewarden` service's `depends_on` to include the new services:

```yaml
  vibewarden:
    # ... existing config ...
    depends_on:
{{- if or .App.Build .App.Image }}
      app:
        condition: service_healthy
{{- end }}
{{- if .Auth.Enabled }}
      kratos:
        condition: service_healthy
{{- end }}
{{- if and .Secrets.Enabled (or (len .Secrets.Inject.Headers) (len .Secrets.Inject.Env)) }}
      seed-secrets:
        condition: service_completed_successfully
{{- end }}
{{- if .Secrets.Enabled }}
{{- if not (or (len .Secrets.Inject.Headers) (len .Secrets.Inject.Env)) }}
      openbao:
        condition: service_healthy
{{- end }}
{{- end }}
{{- if eq .RateLimit.Store "redis" }}
      redis:
        condition: service_healthy
{{- end }}
    environment:
      # ... existing env vars ...
{{- if .Secrets.Enabled }}
      VIBEWARDEN_SECRETS_OPENBAO_ADDRESS: http://openbao:8200
      VIBEWARDEN_SECRETS_OPENBAO_AUTH_TOKEN: ${OPENBAO_DEV_ROOT_TOKEN:-dev-root-token}
{{- end }}
{{- if eq .RateLimit.Store "redis" }}
      VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS: redis:6379
{{- end }}
```

##### Volumes Section

Add to volumes section:

```yaml
volumes:
{{- if .Auth.Enabled }}
  kratos-db-data:
{{- end }}
{{- if .TLS.Enabled }}
  vibewarden-data:
{{- end }}
{{- if eq .RateLimit.Store "redis" }}
  redis-data:
{{- end }}
```

#### Seed Script Template

Create `internal/config/templates/seed-secrets.sh.tmpl`:

```bash
#!/usr/bin/env sh
# seed-secrets.sh — Generated by VibeWarden to seed demo secrets into OpenBao.
# Do not edit manually — re-run `vibewarden generate` to regenerate.

set -eu

echo "Waiting for OpenBao to be ready..."
until bao status >/dev/null 2>&1; do
  sleep 1
done

echo "Enabling KV v2 secrets engine at secret/ ..."
bao secrets enable -path=secret -version=2 kv 2>/dev/null || true

echo "Seeding demo secrets..."

{{- range .Secrets.Inject.Headers }}
# Header injection: {{ .Header }}
bao kv put {{ $.Secrets.OpenBao.MountPath }}/{{ .SecretPath }} \
  {{ .SecretKey }}="demo-value-for-{{ .SecretKey }}"
{{- end }}

{{- range .Secrets.Inject.Env }}
# Env injection: {{ .EnvVar }}
bao kv put {{ $.Secrets.OpenBao.MountPath }}/{{ .SecretPath }} \
  {{ .SecretKey }}="demo-value-for-{{ .SecretKey }}"
{{- end }}

echo "Done — OpenBao secrets seeded successfully."
```

#### Generate Service Changes

Update `internal/app/generate/Service.Generate()` to also generate `seed-secrets.sh` when needed:

```go
// After generating docker-compose.yml...

// Generate seed-secrets.sh if secrets plugin is enabled and has inject entries.
if cfg.Secrets.Enabled && (len(cfg.Secrets.Inject.Headers) > 0 || len(cfg.Secrets.Inject.Env) > 0) {
    seedPath := filepath.Join(outputDir, "seed-secrets.sh")
    if err := s.renderer.RenderToFile("seed-secrets.sh.tmpl", cfg, seedPath, true); err != nil {
        return fmt.Errorf("rendering seed-secrets.sh: %w", err)
    }
    // Make the script executable
    if err := os.Chmod(seedPath, 0o750); err != nil {
        return fmt.Errorf("setting seed-secrets.sh permissions: %w", err)
    }
}
```

#### Sequence

1. User configures `secrets.enabled: true` and/or `rate_limit.store: redis` in `vibewarden.yaml`
2. User optionally configures `secrets.inject.headers` or `secrets.inject.env` entries
3. User runs `vibewarden generate`
4. Generate service reads config and renders templates:
   - `docker-compose.yml.tmpl` with conditional OpenBao/Redis/seed services
   - `seed-secrets.sh.tmpl` if inject entries exist
5. Generated files written to `.vibewarden/generated/`:
   - `docker-compose.yml` (includes openbao, redis, seed-secrets as needed)
   - `seed-secrets.sh` (if inject entries exist, executable)
6. User runs `docker compose up`
7. Startup order enforced by `depends_on`:
   - postgres, kratos-db (if auth enabled)
   - kratos, openbao, redis (parallel, with healthchecks)
   - seed-secrets (waits for openbao healthy)
   - vibewarden (waits for seed-secrets completed, or openbao healthy if no seed)
   - app (parallel with vibewarden)
8. VibeWarden connects to OpenBao/Redis using container DNS names

#### Error Cases

| Error | Handling |
|-------|----------|
| OpenBao fails to start | `depends_on` blocks vibewarden; logs show OpenBao error |
| Redis fails to start | `depends_on` blocks vibewarden; logs show Redis error |
| seed-secrets fails | `depends_on` with `service_completed_successfully` blocks vibewarden |
| OpenBao unavailable at runtime | Secrets plugin logs error; behavior depends on `secrets.health` config |
| Redis unavailable at runtime | Rate limiter falls back to memory if `rate_limit.redis.fallback: true` |
| `secrets.inject` empty but `secrets.enabled: true` | No seed-secrets service; openbao still runs; vibewarden depends on openbao directly |

#### Prod Mode Considerations

The design above focuses on dev mode (in-memory OpenBao, root token). For prod mode:

1. **AppRole auth**: The `secrets.openbao.auth.method: approle` config is already supported
   in the existing secrets plugin. The generated compose uses `${OPENBAO_DEV_ROOT_TOKEN}`
   env var, which can be overridden for prod via `.env` file or environment.

2. **External OpenBao**: If the user has an existing OpenBao cluster, they can:
   - Set `secrets.openbao.address` to point to their cluster
   - Use `overrides.compose_file` to provide a custom compose without the openbao service
   - Or simply not use `vibewarden generate` and manage compose manually

3. **Persistent storage**: The dev-mode OpenBao is in-memory. For prod persistence:
   - Add a volume mount for OpenBao data (future enhancement)
   - Or use external OpenBao/Vault (recommended for prod)

4. **Seed script**: The seed-secrets service is dev-mode only. It seeds demo values.
   In prod, secrets should be provisioned via Terraform, CI/CD, or manual `bao` commands.

The template does not currently differentiate between dev and prod profiles. This is
intentional — the same compose works for both, with env var overrides controlling behavior.
A future enhancement could add profile-aware templates if needed.

#### Test Strategy

**Unit Tests** (in `internal/app/generate/helpers_test.go`):

| Test | Description |
|------|-------------|
| `TestNeedsOpenBao_Enabled` | Returns true when `secrets.enabled: true` |
| `TestNeedsOpenBao_Disabled` | Returns false when `secrets.enabled: false` |
| `TestNeedsRedis_StoreRedis` | Returns true when `rate_limit.store: redis` |
| `TestNeedsRedis_StoreMemory` | Returns false when `rate_limit.store: memory` |
| `TestNeedsRedis_StoreEmpty` | Returns false when `rate_limit.store` is empty |
| `TestNeedsSeedSecrets_WithHeaders` | Returns true when inject.headers is non-empty |
| `TestNeedsSeedSecrets_WithEnv` | Returns true when inject.env is non-empty |
| `TestNeedsSeedSecrets_NoInject` | Returns false when inject is empty |
| `TestNeedsSeedSecrets_SecretsDisabled` | Returns false when secrets.enabled is false |

**Template Tests** (in `internal/app/generate/service_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_OpenBaoService_WhenSecretsEnabled` | OpenBao service present in compose |
| `TestGenerate_OpenBaoService_WhenSecretsDisabled` | OpenBao service absent |
| `TestGenerate_RedisService_WhenStoreRedis` | Redis service present |
| `TestGenerate_RedisService_WhenStoreMemory` | Redis service absent |
| `TestGenerate_SeedSecrets_WhenInjectConfigured` | seed-secrets service present |
| `TestGenerate_SeedSecrets_WhenNoInject` | seed-secrets service absent |
| `TestGenerate_SeedSecretsScript_Created` | seed-secrets.sh file created |
| `TestGenerate_SeedSecretsScript_Executable` | seed-secrets.sh has 0750 permissions |
| `TestGenerate_DependsOn_OpenBao` | vibewarden depends_on openbao when secrets enabled |
| `TestGenerate_DependsOn_SeedSecrets` | vibewarden depends_on seed-secrets when inject configured |
| `TestGenerate_DependsOn_Redis` | vibewarden depends_on redis when store is redis |
| `TestGenerate_VibewardenEnv_OpenBao` | VIBEWARDEN_SECRETS_OPENBAO_* env vars set |
| `TestGenerate_VibewardenEnv_Redis` | VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS env var set |
| `TestGenerate_Volumes_Redis` | redis-data volume present when redis enabled |

**Integration Tests** (in `internal/app/generate/service_integration_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Integration_FullStack` | All plugins enabled, validate complete compose |
| `TestGenerate_Integration_SecretsOnly` | Only secrets enabled, validate compose structure |
| `TestGenerate_Integration_RedisOnly` | Only redis enabled, validate compose structure |

Tests should parse the generated YAML and verify:
- Correct services are present/absent based on config
- `depends_on` chains are correct
- Environment variables point to correct container names
- Volumes are declared when needed

#### New Dependencies

None. This feature uses existing template rendering infrastructure and the OpenBao/Redis
images are pulled at runtime by Docker Compose.

### Consequences

**Positive:**
- Plugin-dependent infrastructure is now part of the generated stack
- Zero manual compose editing for common use cases
- Dev mode "just works" with demo secrets seeded automatically
- Dependency ordering ensures correct startup sequence
- Environment variable overrides enable prod mode without template changes

**Negative:**
- Generated compose grows more complex with more conditional services
- Seed script embeds "demo-value-for-X" placeholders that are meaningless in prod
- No differentiation between dev/prod profiles in the template itself

**Trade-offs:**
- Dev-mode root token vs AppRole: Root token is simpler for dev; prod should use AppRole
- In-memory OpenBao vs persistent: Acceptable for dev; prod should use external cluster
- Single compose vs profile-separated: Single is simpler; profiles could be added later
- Seed script templated vs static: Templated allows customization based on inject config

---
