# ADR-006: Add User App Service to Generated docker-compose.yml


**Date**: 2026-03-28
**Issue**: #279
**Status**: Accepted

### Context

The generated `docker-compose.yml` currently includes the VibeWarden sidecar and Kratos
(when auth is enabled), but not the user's own application. This forces users to manually
add their app service to the compose file, which defeats the "single config file" goal
of VibeWarden. The user should only need to maintain `vibewarden.yaml` and their app source.

This is part of Epic #277 (generate entire runtime stack from vibewarden.yaml).

### Decision

Add an `app` section to `vibewarden.yaml` that configures how the user's application is
included in the generated Docker Compose file. Support two modes:

1. **Dev mode** (`VIBEWARDEN_PROFILE=dev` or `tls`): Build the app from local Dockerfile
2. **Prod mode** (`VIBEWARDEN_PROFILE=prod`): Pull a pre-built image from a registry

#### Domain Model Changes

No new domain entities. This is a config-driven template enhancement.

#### Config Struct Changes

Add `AppConfig` struct to `internal/config/config.go`:

```go
// AppConfig configures the user's application in the generated Docker Compose.
// Either Build or Image should be set, depending on whether the user wants
// to build from source (dev) or use a pre-built image (prod).
type AppConfig struct {
    // Build is the Docker build context path (e.g., "." for current directory).
    // Used in dev/tls profiles.
    Build string `mapstructure:"build"`

    // Image is the Docker image reference (e.g., "ghcr.io/org/myapp:latest").
    // Used in prod profile. Can be overridden via VIBEWARDEN_APP_IMAGE env var.
    Image string `mapstructure:"image"`
}
```

Add `App AppConfig` field to the `Config` struct, after `Upstream`:

```go
type Config struct {
    Server   ServerConfig   `mapstructure:"server"`
    Upstream UpstreamConfig `mapstructure:"upstream"`
    App      AppConfig      `mapstructure:"app"`  // NEW
    TLS      TLSConfig      `mapstructure:"tls"`
    // ... rest unchanged
}
```

#### Ports (Interfaces)

No new interfaces required. The existing `ports.TemplateRenderer` interface is sufficient.

#### Adapters

No new adapters. The existing template adapter handles the rendering.

#### Application Service

The existing `internal/app/generate/Service.Generate()` method continues to pass the
full `*config.Config` to the template renderer. No changes to the service logic.

#### File Layout

Files to modify:

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `AppConfig` struct and `App` field to `Config` |
| `internal/config/templates/docker-compose.yml.tmpl` | Add app service with build/image logic |
| `internal/cli/templates/vibewarden.yaml.tmpl` | Add `app.build: .` section |
| `internal/app/generate/service_test.go` | Add tests for app service generation |

No new files required.

#### Template Changes

Update `internal/config/templates/docker-compose.yml.tmpl`:

1. Add the user's app service **before** the vibewarden service
2. Use Go template conditionals to select `build:` vs `image:` based on the configured values
3. Add `VIBEWARDEN_APP_IMAGE` env var override for prod mode
4. Add healthcheck to the app service
5. Update vibewarden service to `depends_on` the app with a healthcheck condition
6. Change `VIBEWARDEN_UPSTREAM_HOST` to point to the app container instead of `host.docker.internal`

The template logic for build vs image selection:

```
{{- if .App.Build }}
    build:
      context: {{ .App.Build }}
{{- else if .App.Image }}
    image: ${VIBEWARDEN_APP_IMAGE:-{{ .App.Image }}}
{{- end }}
```

Key design points:

- **Profile-agnostic template**: The template does not check `VIBEWARDEN_PROFILE`. Instead,
  it renders based on what is configured in the YAML. Users set either `app.build` (for dev)
  or `app.image` (for prod), or both if they want to support both modes.
- **Image override via env var**: In prod, `VIBEWARDEN_APP_IMAGE` env var overrides the
  configured image, enabling CI/CD to inject a specific image tag without modifying YAML.
- **Healthcheck**: The app service includes a default healthcheck that curls `localhost:<upstream_port>/health`.
  Users can override this by providing their own compose file via `overrides.compose_file`.
- **Network**: The app joins the `vibewarden` network so all services can communicate.

#### Sequence

1. User runs `vibewarden init`
2. `vibewarden.yaml` is generated with `app.build: .` (for dev workflow)
3. User runs `vibew dev` (which calls `vibewarden generate` internally)
4. `vibewarden generate` reads `vibewarden.yaml` and renders `docker-compose.yml.tmpl`
5. Template checks if `App.Build` is set:
   - If set: render `build: context: {{ .App.Build }}`
   - If not set but `App.Image` is set: render `image: ${VIBEWARDEN_APP_IMAGE:-{{ .App.Image }}}`
   - If neither set: no app service is rendered (graceful degradation)
6. Generated `docker-compose.yml` includes the app service before vibewarden
7. vibewarden service's `VIBEWARDEN_UPSTREAM_HOST` points to `app` container
8. `docker compose up` starts the full stack

#### Error Cases

| Error | Handling |
|-------|----------|
| Both `app.build` and `app.image` set | Valid — `app.build` takes precedence (dev mode) |
| Neither `app.build` nor `app.image` set | No app service rendered; vibewarden falls back to `host.docker.internal` (existing behavior) |
| `app.build` path does not exist | Docker Compose fails at build time with clear error |
| `app.image` not found in registry | Docker Compose fails at pull time with clear error |
| App container fails healthcheck | `depends_on` condition keeps vibewarden waiting; `docker compose logs app` shows failure |

#### Test Strategy

**Unit Tests** (in `internal/app/generate/service_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_AppService_BuildMode` | Config with `App.Build` renders app service with `build:` |
| `TestGenerate_AppService_ImageMode` | Config with `App.Image` renders app service with `image:` |
| `TestGenerate_AppService_BothSet` | Config with both set renders `build:` (build takes precedence) |
| `TestGenerate_AppService_NeitherSet` | Config with neither set does not render app service |
| `TestGenerate_AppService_DependsOn` | Vibewarden service `depends_on` app when app is rendered |
| `TestGenerate_AppService_UpstreamHost` | `VIBEWARDEN_UPSTREAM_HOST=app` when app service is rendered |

**Integration Tests** (in `internal/app/generate/service_integration_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Integration_AppService_DevMode` | Full render with app.build, validates compose YAML |
| `TestGenerate_Integration_AppService_ProdMode` | Full render with app.image, validates compose YAML |

Tests should parse the generated YAML and verify:
- The `app` service is present with correct build/image settings
- The `vibewarden` service has `depends_on.app.condition: service_healthy`
- The `VIBEWARDEN_UPSTREAM_HOST` environment variable is set to `app`

#### New Dependencies

None. This feature uses existing template rendering infrastructure.

### Consequences

**Positive:**
- User's app is now part of the generated stack — truly single-config workflow
- Dev mode builds from source, prod mode pulls from registry — both workflows supported
- `VIBEWARDEN_APP_IMAGE` env var enables CI/CD to inject image tags
- Backwards compatible — if `app` section is absent, behavior is unchanged

**Negative:**
- Users must have a `Dockerfile` for dev mode to work (reasonable assumption for Docker users)
- Default healthcheck assumes `/health` endpoint exists; users may need to override
- Adding more sections to `vibewarden.yaml` increases config surface area

**Trade-offs:**
- Using `build:` context vs `dockerfile:` path: context is simpler and matches most project layouts
- Healthcheck via curl vs custom command: curl is universal; users can override if needed
- Build precedence over image when both set: matches dev-first workflow expectation

---
