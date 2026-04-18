# ADR-011: Migrate Demo App to Use Generated docker-compose.yml


**Date**: 2026-03-28
**Issue**: #284
**Status**: Accepted

### Context

This is the capstone story of Epic #277 (generate entire runtime stack from vibewarden.yaml).
The demo app at `examples/demo-app/` currently uses a hand-crafted `docker-compose.yml` with
~420 lines. All prerequisite stories are now complete:

- ADR-006 (#279): App service in generated compose
- ADR-007 (#281): Plugin-dependent services (OpenBao, Redis)
- ADR-008 (#282): Observability profile
- ADR-009 (#283): .env template with secure credential management
- ADR-010 (#286): `vibew secret get` command

The demo app should now showcase the intended workflow: commit only `vibewarden.yaml` and
let `vibewarden generate` produce everything else. This validates that the generation
system works end-to-end and serves as the canonical example for users.

### Decision

Migrate the demo app to use generated configuration:

1. **Update `vibewarden.yaml`** to exercise all features
2. **Remove hand-crafted `docker-compose.yml`** from version control
3. **Update Makefile** to generate before composing
4. **Keep seed scripts** (`seed-users.sh`, `seed-secrets.sh`) for demo data
5. **Deprecate the committed `observability/` directory** (configs are now generated)
6. **Update README** with new workflow

#### Domain Model Changes

No new domain entities. This is a configuration and workflow change.

#### Ports (Interfaces)

No new interfaces required.

#### Adapters

No new adapters required.

#### Application Service

No changes to the generate service. The existing `internal/app/generate/Service.Generate()`
handles all required functionality.

#### File Layout

**Files to delete:**

| File | Reason |
|------|--------|
| `examples/demo-app/docker-compose.yml` | Replaced by generated compose |
| `examples/demo-app/docker-compose.local-demo.yml` | Superseded by profile system |
| `examples/demo-app/docker-compose.prod.yml` | Superseded by profile system |
| `examples/demo-app/vibewarden.local-demo.yaml` | Superseded by single config |
| `examples/demo-app/vibewarden.prod.yaml` | Superseded by single config |
| `observability/prometheus/prometheus.yml` | Now generated from template |
| `observability/grafana/provisioning/datasources/prometheus.yml` | Now generated |
| `observability/grafana/provisioning/dashboards/dashboard.yml` | Now generated |
| `observability/grafana/dashboards/vibewarden.json` | Embedded in binary, generated on demand |
| `observability/loki/loki-config.yml` | Now generated |
| `observability/promtail/promtail-config.yml` | Now generated |

The entire `observability/` directory can be removed once the demo migration is complete.

**Files to keep:**

| File | Purpose |
|------|---------|
| `examples/demo-app/vibewarden.yaml` | Single source of truth for demo config |
| `examples/demo-app/.env.example` | Documents available env var overrides |
| `examples/demo-app/Dockerfile` | App build context |
| `examples/demo-app/main.go` | Demo app source |
| `examples/demo-app/main_test.go` | Demo app tests |
| `examples/demo-app/go.mod`, `go.sum` | Demo app dependencies |
| `examples/demo-app/static/` | Demo UI assets |
| `examples/demo-app/kratos/` | Kratos config overrides (identity schema, etc.) |
| `examples/demo-app/scripts/seed-users.sh` | Seeds demo identities into Kratos |
| `examples/demo-app/scripts/seed-secrets.sh` | Seeds demo secrets into OpenBao |
| `examples/demo-app/README.md` | Documentation |
| `examples/demo-app/CHALLENGE.md` | Challenge documentation |
| `examples/demo-app/MONITORING.md` | Monitoring documentation |
| `examples/demo-app/RECOVERY.md` | Recovery documentation |
| `examples/demo-app/.gitignore` | Ignores generated files |

**Files to modify:**

| File | Change |
|------|--------|
| `examples/demo-app/vibewarden.yaml` | Update to use new config structure with app, observability, etc. |
| `examples/demo-app/.env.example` | Update to match generated .env.template format |
| `examples/demo-app/.gitignore` | Add `.vibewarden/generated/` |
| `examples/demo-app/README.md` | Update with new workflow instructions |
| `Makefile` | Update `demo` and `demo-down` targets |

#### Updated vibewarden.yaml

The new `examples/demo-app/vibewarden.yaml` exercises all major features:

```yaml
# VibeWarden Demo App Configuration
# Single source of truth for the entire demo stack.
#
# Usage:
#   vibewarden generate
#   docker compose -f .vibewarden/generated/docker-compose.yml up
#
# Or simply: make demo

# Deployment profile: dev | tls | prod
profile: dev

# User application
app:
  # Build from local Dockerfile (dev mode)
  build: .
  # Image for production (overridable via VIBEWARDEN_APP_IMAGE)
  # image: ghcr.io/vibewarden/demo-app:latest

server:
  host: "0.0.0.0"
  port: 8080

upstream:
  host: app
  port: 3000

tls:
  enabled: false
  # For TLS profile, set via env vars:
  # VIBEWARDEN_TLS_ENABLED=true
  # VIBEWARDEN_TLS_PROVIDER=self-signed

kratos:
  public_url: "http://kratos:4433"
  admin_url: "http://kratos:4434"

auth:
  enabled: true
  public_paths:
    - "/"
    - "/public"
    - "/health"
    - "/profile"
    - "/static"
    - "/auth"
    - "/vuln"
  session_cookie_name: "ory_kratos_session"

rate_limit:
  enabled: true
  store: memory
  per_ip:
    requests_per_second: 5
    burst: 10
  per_user:
    requests_per_second: 10
    burst: 20
  trust_proxy_headers: false
  exempt_paths: []

log:
  level: "info"
  format: "json"

admin:
  enabled: false

metrics:
  enabled: true
  path_patterns:
    - "/"
    - "/public"
    - "/me"
    - "/headers"
    - "/spam"
    - "/health"

secrets:
  enabled: true
  provider: openbao
  openbao:
    address: http://openbao:8200
    auth:
      method: token
    mount_path: secret
  inject:
    headers:
      - secret_path: demo/api-key
        secret_key: token
        header: X-Demo-Api-Key
    env:
      - secret_path: demo/app-config
        secret_key: database_url
        env_var: DEMO_DATABASE_URL
      - secret_path: demo/app-config
        secret_key: session_secret
        env_var: DEMO_SESSION_SECRET
  cache_ttl: "5m"

security_headers:
  enabled: true
  hsts_max_age: 31536000
  hsts_include_subdomains: true
  hsts_preload: false
  content_type_nosniff: true
  frame_option: "DENY"
  content_security_policy: "default-src 'self'; style-src 'self'; script-src 'self' 'unsafe-inline'"
  referrer_policy: "strict-origin-when-cross-origin"

observability:
  enabled: true
  grafana_port: 3001
  prometheus_port: 9090
  loki_port: 3100
  retention_days: 7

# Override paths for demo-specific configs
overrides:
  # Use demo-specific Kratos config with seed users
  kratos_config: ""
  identity_schema: ""
```

#### Updated .gitignore

Add to `examples/demo-app/.gitignore`:

```
# Generated by vibewarden generate
.vibewarden/
```

#### Updated Makefile Targets

```makefile
# Start the full local demo stack
demo: ## Start the full local demo stack (https://localhost:8443, Grafana http://localhost:3001)
	cd examples/demo-app && \
	  ../../bin/vibewarden generate && \
	  COMPOSE_PROFILES=observability \
	  docker compose -f .vibewarden/generated/docker-compose.yml up -d
	@echo ""
	@echo "Demo stack is starting — wait ~30 s for all services to be healthy."
	@echo ""
	@echo "  App:        http://localhost:8080"
	@echo "  Grafana:    http://localhost:3001"
	@echo "  Prometheus: http://localhost:9090"
	@echo ""
	@echo "Demo credentials: demo@vibewarden.dev / demo1234"
	@echo "Run 'vibew secret get postgres' to retrieve generated credentials."

# Start demo with TLS
demo-tls: ## Start the full local demo stack with self-signed TLS
	cd examples/demo-app && \
	  VIBEWARDEN_TLS_ENABLED=true \
	  VIBEWARDEN_TLS_PROVIDER=self-signed \
	  VIBEWARDEN_SERVER_PORT=8443 \
	  ../../bin/vibewarden generate && \
	  COMPOSE_PROFILES=observability \
	  docker compose -f .vibewarden/generated/docker-compose.yml up -d
	@echo ""
	@echo "Demo stack is starting — wait ~30 s for all services to be healthy."
	@echo ""
	@echo "  App:        https://localhost:8443   (accept the self-signed cert warning)"
	@echo "  Grafana:    http://localhost:3001"
	@echo "  Prometheus: http://localhost:9090"
	@echo ""
	@echo "Demo credentials: demo@vibewarden.dev / demo1234"

# Stop the full local demo stack
demo-down: ## Stop the full local demo stack
	cd examples/demo-app && \
	  docker compose -f .vibewarden/generated/docker-compose.yml down

# Stop and remove volumes
demo-clean: ## Stop the demo stack and remove all volumes
	cd examples/demo-app && \
	  docker compose -f .vibewarden/generated/docker-compose.yml down -v && \
	  rm -rf .vibewarden/generated/
```

#### Seed Scripts Mounting

The generated `docker-compose.yml` template must mount the demo-specific seed scripts.
Update `internal/config/templates/docker-compose.yml.tmpl` to support user-provided seed
scripts via config or convention.

For the demo app, we use the `overrides` mechanism or a convention where seed scripts
in `scripts/` are automatically mounted. The simplest approach is to have the demo
`vibewarden.yaml` use relative paths that the generated compose references.

The existing seed containers in the template already mount `seed-secrets.sh` from the
generated directory. For the demo-specific `seed-users.sh`, add a `seed` service that
runs the Kratos user seeding.

Add to `docker-compose.yml.tmpl` a configurable seed container:

```yaml
{{- if .Auth.Enabled }}
  seed-users:
    image: curlimages/curl:8.12.1
    environment:
      KRATOS_ADMIN_URL: http://kratos:4434
    volumes:
      - ./scripts/seed-users.sh:/seed-users.sh:ro
    command: sh /seed-users.sh
    depends_on:
      kratos:
        condition: service_healthy
    networks:
      - vibewarden
    restart: "no"
{{- end }}
```

Note: The `./scripts/seed-users.sh` path is relative to where `docker compose` is run.
Since the demo runs `docker compose -f .vibewarden/generated/docker-compose.yml`, the
working directory is `examples/demo-app/`, so `./scripts/seed-users.sh` resolves correctly.

Alternatively, we can add a config option for custom seed scripts, but for the demo
we rely on the convention that `scripts/seed-users.sh` exists when auth is enabled.

#### Sequence

1. User runs `make demo` (or `make demo-tls`)
2. Makefile builds `bin/vibewarden` if needed (via dependency on `build`)
3. Makefile runs `vibewarden generate` in `examples/demo-app/`:
   - Generates `.vibewarden/generated/docker-compose.yml`
   - Generates `.vibewarden/generated/.credentials` (fresh random credentials)
   - Generates `.vibewarden/generated/.env.template`
   - Generates `.vibewarden/generated/kratos/kratos.yml`
   - Generates `.vibewarden/generated/kratos/identity.schema.json`
   - Generates `.vibewarden/generated/seed-secrets.sh`
   - Generates `.vibewarden/generated/observability/` configs
4. Makefile runs `docker compose -f .vibewarden/generated/docker-compose.yml up -d`
5. Docker Compose starts services in dependency order:
   - postgres (kratos-db)
   - openbao
   - seed-secrets (populates OpenBao from `.credentials`)
   - kratos (after postgres healthy)
   - seed-users (after kratos healthy, seeds demo identities)
   - app (after kratos healthy)
   - vibewarden (after app, kratos, seed-secrets healthy/complete)
   - prometheus, loki, promtail, grafana (observability profile)
6. User accesses demo at http://localhost:8080 (or https://localhost:8443 for TLS)
7. User can retrieve credentials via `vibew secret get postgres`

#### Error Cases

| Error | Handling |
|-------|----------|
| `vibewarden generate` fails | Makefile exits with error; no compose started |
| Docker image pull fails | Docker Compose reports pull error |
| Kratos healthcheck fails | Dependent services wait; eventually timeout |
| seed-users.sh not found | Docker Compose fails with mount error; user must create script or disable auth |
| Generated .credentials not found | seed-secrets.sh fails; vibewarden cannot connect |

#### Template Change: Add seed-users Service

The generated compose needs a `seed-users` service that mounts the user-provided seed script.
This is a demo-specific feature but can be generalized.

Add to `internal/config/templates/docker-compose.yml.tmpl` after the `seed-secrets` service:

```yaml
{{- if .Auth.Enabled }}
{{- /* seed-users is optional: only rendered if scripts/seed-users.sh exists.
       The template cannot check filesystem, so we always render it for auth-enabled configs.
       If the script doesn't exist, docker compose will fail with a clear error. */ -}}
  seed-users:
    image: curlimages/curl:8.12.1
    environment:
      KRATOS_ADMIN_URL: http://kratos:4434
    volumes:
      - ./scripts/seed-users.sh:/seed-users.sh:ro
    command: sh /seed-users.sh
    depends_on:
      kratos:
        condition: service_healthy
    networks:
      - vibewarden
    restart: "no"
{{- end }}
```

Note: This assumes a convention that projects with auth enabled provide a
`scripts/seed-users.sh` script. For projects without demo users to seed, they can
create an empty script or use `overrides.compose_file` to provide a custom compose.

For the demo app specifically, we already have `examples/demo-app/scripts/seed-users.sh`.

#### Deprecation of observability/ Directory

The `observability/` directory at the repository root contains hand-crafted configs that
are now superseded by the generated templates in `internal/config/templates/observability/`.

**Migration plan:**

1. This ADR marks the files as deprecated
2. The demo migration removes references to `../../observability/` paths
3. A follow-up PR can delete the `observability/` directory entirely

The generated configs in `internal/config/templates/observability/` are the source of truth.
The committed `observability/` directory is only useful for reference during the transition.

#### Test Strategy

**Manual Testing Checklist:**

| Test | Steps | Expected Result |
|------|-------|-----------------|
| `make demo` works | Run `make demo`, wait 30s | All services healthy, app at :8080 |
| Auth works | Login with demo@vibewarden.dev / demo1234 | Successful login, session cookie set |
| Protected routes work | Access /me when logged in | Returns user info |
| Rate limiting works | Run `for i in $(seq 1 20); do curl -X POST localhost:8080/spam; done` | 429 after burst |
| Security headers present | `curl -I localhost:8080/public` | HSTS, CSP, X-Frame-Options present |
| Secrets injection works | Check X-Demo-Api-Key header | Header present with demo value |
| Observability works | Access Grafana at :3001 | Dashboard shows metrics |
| `make demo-down` works | Run `make demo-down` | All containers stopped |
| `make demo-clean` works | Run `make demo-clean` | Containers stopped, volumes removed |
| TLS profile works | Run `make demo-tls` | HTTPS at :8443 with self-signed cert |
| Credentials retrieval | Run `vibew secret get postgres` | Shows generated password |

**Automated Tests:**

The existing tests in `internal/app/generate/service_test.go` cover the generation logic.
No new automated tests are required for this migration, as it is primarily a configuration
and documentation change.

**CI Considerations:**

The CI pipeline should verify that `make demo` succeeds:

```yaml
- name: Test demo workflow
  run: |
    make build
    cd examples/demo-app
    ../../bin/vibewarden generate
    # Verify generated files exist
    test -f .vibewarden/generated/docker-compose.yml
    test -f .vibewarden/generated/.credentials
    test -f .vibewarden/generated/kratos/kratos.yml
```

Full `docker compose up` is not tested in CI due to resource constraints.

#### New Dependencies

None. This migration uses existing functionality.

### Consequences

**Positive:**
- Demo now showcases the intended user workflow
- Single source of truth (`vibewarden.yaml`) for all demo configuration
- Generated files are gitignored — no duplication between template and demo
- Validates that the generate system works end-to-end
- Reduces maintenance burden — updating templates updates the demo automatically
- Users can copy the demo workflow for their own projects

**Negative:**
- `make demo` now requires a build step (`vibewarden generate`)
- Additional complexity in Makefile targets
- seed-users convention may surprise users who don't have a seed script

**Trade-offs:**
- Convention (scripts/seed-users.sh) vs configuration: Convention is simpler but less flexible
- Keeping observability/ vs deleting immediately: Keeping allows reference during transition
- Generated compose paths relative to working directory: Matches how users will run it

---
