# ADR-009: Generate .env Template with Secure Credential Management


**Status:** Accepted
**Issue:** #283
**Date:** 2026-03-28

### Context

VibeWarden generates a Docker Compose stack that includes services requiring credentials:
Postgres (for Kratos), Kratos secrets (cookie + cipher), Grafana admin password, and
OpenBao dev root token. The current implementation references environment variables
in docker-compose.yml.tmpl (e.g., `${KRATOS_DB_PASSWORD}`) but does not generate these
values or any `.env` file.

The security requirements from issue #283 are strict:

1. **No secrets in .env, ever** — environment files are too easily committed to git
2. **Every `vibewarden generate` run creates fresh random credentials** — no hardcoded defaults
3. **Credentials stored in a sealed file** (`.credentials`, mode 0600, gitignored)
4. **Init container seeds OpenBao from .credentials** — services read from OpenBao at runtime
5. **Prod compose generation fails** if `secrets.enabled: false` — production requires OpenBao

This design eliminates the risk of accidentally committing credentials while still providing
a zero-friction dev experience.

### Decision

Implement secure credential generation with the following architecture:

#### Domain Model Changes

Add a new value object to `internal/domain/generate/`:

```go
// Package generate contains domain types for the stack generation subsystem.
package generate

// GeneratedCredentials holds the randomly generated credentials for a single
// `vibewarden generate` run. It is a value object — immutable after construction.
// The domain layer does not know how these are persisted or consumed.
type GeneratedCredentials struct {
    // PostgresPassword is the password for the Kratos Postgres database (32 chars).
    PostgresPassword string

    // KratosCookieSecret is the Kratos session cookie signing secret (32 chars).
    KratosCookieSecret string

    // KratosCipherSecret is the Kratos data encryption secret (32 chars).
    KratosCipherSecret string

    // GrafanaAdminPassword is the Grafana admin password (24 chars).
    GrafanaAdminPassword string

    // OpenBaoDevRootToken is the OpenBao dev mode root token (32 chars).
    OpenBaoDevRootToken string
}
```

#### Ports (Interfaces)

Add a new outbound port to `internal/ports/credentials.go`:

```go
// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
    "context"

    "github.com/vibewarden/vibewarden/internal/domain/generate"
)

// CredentialGenerator generates cryptographically secure random credentials.
// Implementations must use crypto/rand for all randomness.
type CredentialGenerator interface {
    // Generate creates a new set of random credentials.
    Generate(ctx context.Context) (*generate.GeneratedCredentials, error)
}

// CredentialStore persists and retrieves generated credentials.
// The store is responsible for file permissions and atomic writes.
type CredentialStore interface {
    // Write persists credentials to the backing store. Overwrites any existing data.
    Write(ctx context.Context, creds *generate.GeneratedCredentials, outputDir string) error

    // Read loads previously generated credentials. Returns os.ErrNotExist if none.
    Read(ctx context.Context, outputDir string) (*generate.GeneratedCredentials, error)
}
```

#### Adapters

**CredentialGenerator adapter** (`internal/adapters/credentials/generator.go`):

```go
// Package credentials provides adapters for credential generation and storage.
package credentials

import (
    "context"
    "crypto/rand"
    "encoding/base64"
    "fmt"

    "github.com/vibewarden/vibewarden/internal/domain/generate"
)

// Generator implements ports.CredentialGenerator using crypto/rand.
type Generator struct{}

// NewGenerator creates a Generator.
func NewGenerator() *Generator {
    return &Generator{}
}

// Generate creates cryptographically secure random credentials.
func (g *Generator) Generate(ctx context.Context) (*generate.GeneratedCredentials, error) {
    postgres, err := randomAlphanumeric(32)
    if err != nil {
        return nil, fmt.Errorf("generating postgres password: %w", err)
    }

    cookie, err := randomAlphanumeric(32)
    if err != nil {
        return nil, fmt.Errorf("generating kratos cookie secret: %w", err)
    }

    cipher, err := randomAlphanumeric(32)
    if err != nil {
        return nil, fmt.Errorf("generating kratos cipher secret: %w", err)
    }

    grafana, err := randomAlphanumeric(24)
    if err != nil {
        return nil, fmt.Errorf("generating grafana admin password: %w", err)
    }

    bao, err := randomAlphanumeric(32)
    if err != nil {
        return nil, fmt.Errorf("generating openbao root token: %w", err)
    }

    return &generate.GeneratedCredentials{
        PostgresPassword:     postgres,
        KratosCookieSecret:   cookie,
        KratosCipherSecret:   cipher,
        GrafanaAdminPassword: grafana,
        OpenBaoDevRootToken:  bao,
    }, nil
}

// randomAlphanumeric generates a random alphanumeric string of the specified length.
func randomAlphanumeric(length int) (string, error) {
    // Generate extra bytes to account for base64 expansion, then trim.
    bytes := make([]byte, length)
    if _, err := rand.Read(bytes); err != nil {
        return "", err
    }
    // Use URL-safe base64 without padding for alphanumeric-ish output.
    encoded := base64.RawURLEncoding.EncodeToString(bytes)
    if len(encoded) < length {
        return "", fmt.Errorf("encoded string too short: got %d, want %d", len(encoded), length)
    }
    return encoded[:length], nil
}
```

**CredentialStore adapter** (`internal/adapters/credentials/store.go`):

```go
// Package credentials provides adapters for credential generation and storage.
package credentials

import (
    "bufio"
    "context"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/vibewarden/vibewarden/internal/domain/generate"
)

const (
    credentialsFileName = ".credentials"
    // permSecretFile is the permission mode for the credentials file.
    // Only owner can read/write — group and world have no access.
    permSecretFile = os.FileMode(0o600)
    permDir        = os.FileMode(0o750)
)

// Store implements ports.CredentialStore using a dotenv-formatted file.
type Store struct{}

// NewStore creates a Store.
func NewStore() *Store {
    return &Store{}
}

// Write persists credentials to .credentials in dotenv format.
// The file is created with mode 0600 (owner read/write only).
func (s *Store) Write(ctx context.Context, creds *generate.GeneratedCredentials, outputDir string) error {
    if err := os.MkdirAll(outputDir, permDir); err != nil {
        return fmt.Errorf("creating output directory: %w", err)
    }

    path := filepath.Join(outputDir, credentialsFileName)

    content := fmt.Sprintf(`# Generated credentials — do not commit to version control.
# Re-run 'vibewarden generate' to regenerate with fresh values.
# Mode: 0600 (owner read/write only)

POSTGRES_PASSWORD=%s
KRATOS_SECRETS_COOKIE=%s
KRATOS_SECRETS_CIPHER=%s
GRAFANA_ADMIN_PASSWORD=%s
OPENBAO_DEV_ROOT_TOKEN=%s
`, creds.PostgresPassword, creds.KratosCookieSecret, creds.KratosCipherSecret,
        creds.GrafanaAdminPassword, creds.OpenBaoDevRootToken)

    if err := os.WriteFile(path, []byte(content), permSecretFile); err != nil {
        return fmt.Errorf("writing credentials file: %w", err)
    }

    return nil
}

// Read loads credentials from .credentials file.
func (s *Store) Read(ctx context.Context, outputDir string) (*generate.GeneratedCredentials, error) {
    path := filepath.Join(outputDir, credentialsFileName)

    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    values := make(map[string]string)
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        parts := strings.SplitN(line, "=", 2)
        if len(parts) == 2 {
            values[parts[0]] = parts[1]
        }
    }
    if err := scanner.Err(); err != nil {
        return nil, fmt.Errorf("reading credentials file: %w", err)
    }

    return &generate.GeneratedCredentials{
        PostgresPassword:     values["POSTGRES_PASSWORD"],
        KratosCookieSecret:   values["KRATOS_SECRETS_COOKIE"],
        KratosCipherSecret:   values["KRATOS_SECRETS_CIPHER"],
        GrafanaAdminPassword: values["GRAFANA_ADMIN_PASSWORD"],
        OpenBaoDevRootToken:  values["OPENBAO_DEV_ROOT_TOKEN"],
    }, nil
}
```

#### Application Service Changes

Update `internal/app/generate/service.go` to:

1. Accept `CredentialGenerator` and `CredentialStore` as dependencies
2. Generate credentials on every run
3. Write `.credentials` file
4. Write `.env.template` file (non-secret config only)
5. Update `seed-secrets.sh.tmpl` to read credentials from `.credentials`
6. Validate prod profile requires `secrets.enabled: true`

The service constructor becomes:

```go
// Service implements ports.ConfigGenerator using a ports.TemplateRenderer.
type Service struct {
    renderer   ports.TemplateRenderer
    credGen    ports.CredentialGenerator
    credStore  ports.CredentialStore
}

// NewService creates a generate Service with all dependencies.
func NewService(
    renderer ports.TemplateRenderer,
    credGen ports.CredentialGenerator,
    credStore ports.CredentialStore,
) *Service {
    return &Service{
        renderer:  renderer,
        credGen:   credGen,
        credStore: credStore,
    }
}
```

Add prod profile validation in `Generate()`:

```go
// Validate prod profile requirements.
if cfg.Profile == "prod" && !cfg.Secrets.Enabled {
    return fmt.Errorf("prod profile requires secrets.enabled: true (OpenBao is mandatory for production)")
}
```

#### Config Changes

Add `Profile` field to `internal/config/config.go`:

```go
type Config struct {
    // Profile selects the deployment profile: "dev", "tls", or "prod".
    // Affects TLS settings, credential handling, and validation rules.
    Profile string `mapstructure:"profile"`

    // ... existing fields ...
}
```

Set default in `Load()`:

```go
v.SetDefault("profile", "dev")
```

Add validation in `Validate()`:

```go
validProfiles := map[string]bool{"dev": true, "tls": true, "prod": true}
if !validProfiles[cfg.Profile] {
    return fmt.Errorf("profile must be 'dev', 'tls', or 'prod', got %q", cfg.Profile)
}
```

#### File Layout

New files:

```
internal/
  domain/
    generate/
      credentials.go              # GeneratedCredentials value object
  ports/
    credentials.go                # CredentialGenerator, CredentialStore interfaces
  adapters/
    credentials/
      generator.go                # crypto/rand implementation
      generator_test.go           # unit tests
      store.go                    # file-based storage
      store_test.go               # unit tests
  config/
    templates/
      env.template.tmpl           # .env.template template
      seed-secrets.sh.tmpl        # (update existing)
```

Generated output:

```
.vibewarden/generated/
  .credentials                    # mode 0600, contains all secrets
  .env.template                   # non-secret config, safe to commit
  docker-compose.yml              # references env vars from .credentials
  seed-secrets.sh                 # reads .credentials, seeds OpenBao
  kratos/...                      # unchanged
  observability/...               # unchanged
```

#### Templates

**.env.template.tmpl** (`internal/config/templates/env.template.tmpl`):

```
# VibeWarden Environment Template
# Generated by `vibewarden generate` — safe to commit.
# Copy to .env and customize values before running docker compose.
#
# IMPORTANT: Credentials are NOT stored here.
# They are in .credentials (mode 0600, gitignored).
# Run `vibew secret get <name>` to retrieve them.

# --------------------------------------------------------------------------
# Profile selection
# --------------------------------------------------------------------------

# Deployment profile: dev | tls | prod
VIBEWARDEN_PROFILE={{ .Profile }}

# --------------------------------------------------------------------------
# App image (prod profile only — dev uses build context)
# --------------------------------------------------------------------------
{{- if .App.Image }}

VIBEWARDEN_APP_IMAGE={{ .App.Image }}
{{- else }}

# VIBEWARDEN_APP_IMAGE=ghcr.io/your-org/your-app:latest
{{- end }}

# --------------------------------------------------------------------------
# Compose profiles (uncomment to enable optional services)
# --------------------------------------------------------------------------

# Enable observability stack (Prometheus, Grafana, Loki, Promtail)
# COMPOSE_PROFILES=observability
```

**seed-secrets.sh.tmpl** (update existing):

```bash
#!/usr/bin/env sh
# seed-secrets.sh — Generated by VibeWarden to seed credentials into OpenBao.
# Do not edit manually — re-run `vibewarden generate` to regenerate.

set -eu

CREDS_FILE="$(dirname "$0")/.credentials"

# Load credentials from .credentials file
if [ ! -f "$CREDS_FILE" ]; then
  echo "ERROR: $CREDS_FILE not found. Run 'vibewarden generate' first." >&2
  exit 1
fi

# Source the credentials file (dotenv format)
set -a
. "$CREDS_FILE"
set +a

echo "Waiting for OpenBao to be ready..."
until bao status >/dev/null 2>&1; do
  sleep 1
done

echo "Enabling KV v2 secrets engine at {{ .Secrets.OpenBao.MountPath }}/ ..."
bao secrets enable -path={{ .Secrets.OpenBao.MountPath }} -version=2 kv 2>/dev/null || true

echo "Seeding infrastructure credentials..."

# Postgres credentials
bao kv put {{ .Secrets.OpenBao.MountPath }}/infra/postgres \
  password="$POSTGRES_PASSWORD"

# Kratos secrets
bao kv put {{ .Secrets.OpenBao.MountPath }}/infra/kratos \
  cookie_secret="$KRATOS_SECRETS_COOKIE" \
  cipher_secret="$KRATOS_SECRETS_CIPHER"

# Grafana credentials
bao kv put {{ .Secrets.OpenBao.MountPath }}/infra/grafana \
  admin_password="$GRAFANA_ADMIN_PASSWORD"

# OpenBao root token (for reference)
bao kv put {{ .Secrets.OpenBao.MountPath }}/infra/openbao \
  root_token="$OPENBAO_DEV_ROOT_TOKEN"
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

**docker-compose.yml.tmpl changes**:

Update the kratos-db service to source password from env var (already done, but ensure the
seed-secrets container mounts .credentials):

```yaml
  seed-secrets:
    image: quay.io/openbao/openbao:2.2.0
    environment:
      BAO_ADDR: http://openbao:8200
      BAO_TOKEN: ${OPENBAO_DEV_ROOT_TOKEN}
    volumes:
      - ./.vibewarden/generated/seed-secrets.sh:/seed-secrets.sh:ro
      - ./.vibewarden/generated/.credentials:/.credentials:ro
    entrypoint: sh
    command: /seed-secrets.sh
    depends_on:
      openbao:
        condition: service_healthy
    networks:
      - vibewarden
    restart: "no"
```

Update kratos-db to read from .credentials via env_file:

```yaml
  kratos-db:
    image: postgres:17-alpine
    restart: unless-stopped
    env_file:
      - ./.vibewarden/generated/.credentials
    environment:
      POSTGRES_DB: kratos
      POSTGRES_USER: kratos
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    # ... rest unchanged
```

Similarly for kratos and grafana services.

#### Sequence

1. User runs `vibewarden generate`
2. Service loads `vibewarden.yaml` config
3. If `profile == "prod" && !secrets.enabled`, return error immediately
4. CredentialGenerator creates fresh random credentials (crypto/rand)
5. CredentialStore writes `.credentials` to `.vibewarden/generated/.credentials` (mode 0600)
6. TemplateRenderer writes `.env.template` to `.vibewarden/generated/.env.template`
7. TemplateRenderer writes `docker-compose.yml` with env_file references
8. TemplateRenderer writes `seed-secrets.sh` that sources `.credentials`
9. TemplateRenderer writes Kratos configs, observability configs (unchanged)
10. User runs `docker compose up`
11. OpenBao starts
12. seed-secrets container starts, waits for OpenBao, sources `.credentials`, seeds all values
13. kratos-db, kratos, grafana start with credentials from env_file
14. vibewarden starts after dependencies are healthy

#### Error Cases

| Error | Condition | Handling |
|-------|-----------|----------|
| `ErrProdRequiresSecrets` | `profile == "prod" && !secrets.enabled` | Return error, do not generate any files |
| `ErrCredentialGeneration` | crypto/rand failure | Return error with wrapped cause |
| `ErrCredentialWrite` | Filesystem error on .credentials | Return error, partial generation may occur |
| `ErrTemplateRender` | Template parsing/execution error | Return error with template name |

#### Test Strategy

**Unit Tests** (in `internal/adapters/credentials/generator_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerator_Generate_ReturnsUniqueValues` | Two calls return different credentials |
| `TestGenerator_Generate_CorrectLengths` | Each field has correct character length |
| `TestGenerator_Generate_Alphanumeric` | Output contains only URL-safe base64 chars |

**Unit Tests** (in `internal/adapters/credentials/store_test.go`):

| Test | Description |
|------|-------------|
| `TestStore_Write_CreatesFile` | File created at correct path |
| `TestStore_Write_FilePermissions` | File has mode 0600 |
| `TestStore_Write_DotenvFormat` | File contains valid KEY=VALUE lines |
| `TestStore_Read_ParsesCorrectly` | Roundtrip write then read matches |
| `TestStore_Read_NotExist` | Returns os.ErrNotExist when file missing |
| `TestStore_Read_IgnoresComments` | Comment lines are skipped |

**Unit Tests** (in `internal/app/generate/service_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_ProdProfile_RequiresSecrets` | Returns error when profile=prod, secrets.enabled=false |
| `TestGenerate_DevProfile_AllowsNoSecrets` | Succeeds when profile=dev, secrets.enabled=false |
| `TestGenerate_CredentialsWritten` | .credentials file created on every run |
| `TestGenerate_EnvTemplateWritten` | .env.template file created |
| `TestGenerate_EnvTemplateNoSecrets` | .env.template contains no passwords or tokens |
| `TestGenerate_SeedSecretsSourcesCredentials` | seed-secrets.sh includes sourcing logic |

**Integration Tests** (in `internal/app/generate/service_integration_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Integration_CredentialLifecycle` | Full generate, verify .credentials mode, verify compose refs |
| `TestGenerate_Integration_FreshCredentialsPerRun` | Two generate runs produce different credentials |

#### New Dependencies

None. Uses only Go standard library:
- `crypto/rand` (stdlib) — cryptographically secure random number generation
- `encoding/base64` (stdlib) — encoding random bytes to alphanumeric

### Consequences

**Positive:**
- Credentials are never stored in `.env` — eliminates accidental commit risk
- Fresh credentials on every `vibewarden generate` — no stale/shared secrets
- `.credentials` file mode 0600 — not readable by other users on the system
- Prod profile validation catches misconfiguration early
- `.env.template` provides documentation without exposing secrets
- Seamless dev experience — `docker compose up` just works after `vibewarden generate`

**Negative:**
- Additional complexity in the generate flow
- Users must re-run `vibewarden generate` after `docker compose down -v` to get fresh credentials
- `.credentials` file is plain text on disk — adequate for dev, not for prod (OpenBao handles prod)

**Trade-offs:**
- dotenv format for `.credentials` vs JSON/YAML: dotenv is simpler and compatible with shell sourcing
- Storing root token in `.credentials`: necessary for dev mode, prod uses AppRole auth instead
- env_file in compose vs environment: env_file keeps compose cleaner and avoids duplication

---
