# ADR-010: Add `vibew secret get` and `vibew secret list` Commands


**Status:** Accepted
**Issue:** #286
**Date:** 2026-03-28

### Context

With the no-secrets-on-disk approach from ADR-009, credentials are randomly generated per
`vibewarden generate` run and seeded into OpenBao. Users need a simple way to retrieve
them for debugging or connecting external tools (e.g. database GUIs, API testing).

The existing `vibew secret` command group only has `vibew secret generate` for creating
new random tokens. We need retrieval commands that:

1. Support well-known aliases for common services (postgres, kratos, grafana, openbao)
2. Allow arbitrary OpenBao path queries (e.g. `demo/api-key`)
3. List all managed secret paths
4. Output in human-readable, JSON, or env-export formats
5. Fall back to the `.credentials` file when OpenBao is not running

### Decision

Add two new subcommands to the existing `vibew secret` command group:

- `vibew secret get <alias-or-path>` — retrieve credentials for a service or path
- `vibew secret list` — show all managed secret paths

#### Domain Model Changes

Add a new value object to `internal/domain/secret/`:

```go
// Package secret contains domain types for secret retrieval.
package secret

// RetrievedSecret holds the key/value pairs retrieved from a secret path.
// It is a value object — immutable after construction.
type RetrievedSecret struct {
    // Path is the original path or resolved alias that was queried.
    Path string

    // Alias is the well-known alias if one was used, or empty string.
    Alias string

    // Data holds the key/value pairs of the secret.
    Data map[string]string

    // Source indicates where the secret was retrieved from.
    Source SecretSource
}

// SecretSource indicates the origin of a retrieved secret.
type SecretSource string

const (
    // SourceOpenBao indicates the secret was retrieved from OpenBao.
    SourceOpenBao SecretSource = "openbao"

    // SourceCredentialsFile indicates the secret was retrieved from .credentials.
    SourceCredentialsFile SecretSource = "credentials_file"
)
```

Add a well-known alias resolver:

```go
// WellKnownAlias maps user-friendly names to OpenBao paths and credential file keys.
type WellKnownAlias struct {
    // Name is the alias (e.g. "postgres", "kratos").
    Name string

    // OpenBaoPath is the static KV path in OpenBao (e.g. "infra/postgres").
    OpenBaoPath string

    // DynamicRole is the database secret engine role name for dynamic credentials.
    // Empty string if this alias does not support dynamic credentials.
    DynamicRole string

    // CredentialsFileKeys maps .credentials file keys to output field names.
    // e.g. {"POSTGRES_PASSWORD": "password"}
    CredentialsFileKeys map[string]string

    // EnvPrefix is the prefix for --env output (e.g. "POSTGRES_").
    EnvPrefix string
}

// ResolveAlias returns the WellKnownAlias for the given name, or nil if not found.
func ResolveAlias(name string) *WellKnownAlias

// ListAliases returns all well-known aliases.
func ListAliases() []WellKnownAlias
```

#### Ports (Interfaces)

Add a new outbound port to `internal/ports/secrets.go` (extend existing file):

```go
// SecretRetriever provides read-only access to secrets from multiple sources.
// It tries OpenBao first, then falls back to the credentials file.
type SecretRetriever interface {
    // Get retrieves a secret by alias or path. Tries OpenBao first, then
    // falls back to the credentials file. Returns ErrSecretNotFound when
    // neither source has the secret.
    Get(ctx context.Context, aliasOrPath string) (*secret.RetrievedSecret, error)

    // List returns all managed secret paths from both sources.
    List(ctx context.Context) ([]string, error)
}
```

#### Adapters

**No new adapter files needed.** The application service will compose the existing adapters:

- `internal/adapters/openbao/adapter.go` — already implements `ports.SecretStore`
- `internal/adapters/credentials/store.go` — already implements `ports.CredentialStore`

The secret retrieval logic will live in the application service, which orchestrates
these two adapters with the fallback logic.

#### Application Service

Create `internal/app/secret/service.go`:

```go
// Package secret provides the application service for retrieving secrets.
package secret

import (
    "context"
    "errors"
    "fmt"
    "os"

    "github.com/vibewarden/vibewarden/internal/domain/secret"
    "github.com/vibewarden/vibewarden/internal/ports"
)

// ErrSecretNotFound is returned when a secret cannot be found in any source.
var ErrSecretNotFound = errors.New("secret not found")

// ErrNoSourceAvailable is returned when neither OpenBao nor the credentials file is available.
var ErrNoSourceAvailable = errors.New("no secret source available: OpenBao is not running and .credentials file not found")

// Service implements secret retrieval with OpenBao-first, credentials-file fallback.
type Service struct {
    secretStore   ports.SecretStore    // may be nil if OpenBao is not configured
    credStore     ports.CredentialStore
    outputDir     string               // directory containing .credentials
}

// NewService creates a secret retrieval service.
// secretStore may be nil; in that case, only the credentials file is used.
func NewService(
    secretStore ports.SecretStore,
    credStore ports.CredentialStore,
    outputDir string,
) *Service {
    return &Service{
        secretStore: secretStore,
        credStore:   credStore,
        outputDir:   outputDir,
    }
}

// Get retrieves a secret by alias or path.
func (s *Service) Get(ctx context.Context, aliasOrPath string) (*secret.RetrievedSecret, error)

// List returns all managed secret paths.
func (s *Service) List(ctx context.Context) ([]string, error)

// tryOpenBao attempts to retrieve a secret from OpenBao.
// Returns nil, nil when OpenBao is not available (not configured or health check fails).
func (s *Service) tryOpenBao(ctx context.Context, path string) (map[string]string, error)

// tryCredentialsFile attempts to retrieve a secret from the .credentials file.
// Returns nil, nil when the file does not exist.
func (s *Service) tryCredentialsFile(ctx context.Context, alias *secret.WellKnownAlias) (map[string]string, error)
```

#### File Layout

New files to create:

```
internal/
  domain/
    secret/
      secret.go              # RetrievedSecret, SecretSource value objects
      alias.go               # WellKnownAlias, ResolveAlias, ListAliases
      alias_test.go          # Unit tests for alias resolution
  app/
    secret/
      service.go             # Secret retrieval application service
      service_test.go        # Unit tests with mocked ports
  cli/
    cmd/
      secret_get.go          # `vibew secret get` command implementation
      secret_get_test.go     # CLI tests
      secret_list.go         # `vibew secret list` command implementation
      secret_list_test.go    # CLI tests
```

Modified files:

```
internal/
  ports/
    secrets.go               # Add SecretRetriever interface
  cli/
    cmd/
      secret.go              # Add get and list subcommands
```

#### Sequence

**`vibew secret get postgres` flow:**

1. CLI parses arguments, resolves output format (default/json/env)
2. CLI creates the SecretService with available adapters
3. CLI calls `service.Get(ctx, "postgres")`
4. Service checks if "postgres" is a well-known alias → yes, resolves to `WellKnownAlias`
5. Service checks if OpenBao is available:
   a. If secretStore is nil → skip to step 7
   b. Call `secretStore.Health(ctx)` → if error, skip to step 7
6. OpenBao available:
   a. If alias has DynamicRole, try `database/creds/<role>` first
   b. If dynamic fails or no DynamicRole, try static path `infra/postgres`
   c. Return `RetrievedSecret{Source: SourceOpenBao, Data: ...}`
7. OpenBao not available, try credentials file:
   a. Call `credStore.Read(ctx, outputDir)`
   b. If `os.ErrNotExist` → return `ErrNoSourceAvailable`
   c. Map `.credentials` keys to output fields using alias.CredentialsFileKeys
   d. Return `RetrievedSecret{Source: SourceCredentialsFile, Data: ...}`
8. CLI formats output based on --json or --env flag

**`vibew secret get demo/api-key` flow (arbitrary path):**

1. CLI parses arguments
2. Service checks if "demo/api-key" is a well-known alias → no
3. Service checks if OpenBao is available → if no, return `ErrNoSourceAvailable`
   (arbitrary paths cannot be resolved from .credentials)
4. Service calls `secretStore.Get(ctx, "demo/api-key")`
5. Return `RetrievedSecret{Source: SourceOpenBao, Data: ...}`

**`vibew secret list` flow:**

1. CLI calls `service.List(ctx)`
2. Service collects paths from both sources:
   a. All well-known alias paths
   b. If OpenBao available: `secretStore.List(ctx, "infra/")` and `secretStore.List(ctx, "app/")`
3. Deduplicate and sort paths
4. CLI prints paths (one per line, or JSON array with --json)

#### Well-Known Aliases

| Alias | OpenBao Static Path | Dynamic Role | .credentials Keys | Env Prefix |
|-------|---------------------|--------------|-------------------|------------|
| `postgres` | `infra/postgres` | `app-readwrite` | `POSTGRES_PASSWORD` | `POSTGRES_` |
| `kratos` | `infra/kratos` | — | `KRATOS_SECRETS_COOKIE`, `KRATOS_SECRETS_CIPHER` | `KRATOS_` |
| `grafana` | `infra/grafana` | — | `GRAFANA_ADMIN_PASSWORD` | `GRAFANA_` |
| `openbao` | — | — | `OPENBAO_DEV_ROOT_TOKEN` | `OPENBAO_` |

Note: `openbao` alias only reads from .credentials file (the root token is not stored in OpenBao itself).

#### Output Format Implementations

**Default (human-readable):**
```
postgres credentials (source: openbao):
  username: v-app-kX9mNp2q
  password: A3bC7dE9fG1hJ2kL4mN6pQ8rS0tU
  host:     localhost:5432
  database: vibewarden
```

**`--json`:**
```json
{"username":"v-app-kX9mNp2q","password":"A3bC7dE9fG1hJ2kL4mN6pQ8rS0tU","host":"localhost:5432","database":"vibewarden"}
```

**`--env`:**
```bash
export POSTGRES_USER=v-app-kX9mNp2q
export POSTGRES_PASSWORD=A3bC7dE9fG1hJ2kL4mN6pQ8rS0tU
```

#### Error Cases

| Error | When | User message |
|-------|------|--------------|
| `ErrNoSourceAvailable` | OpenBao not running AND .credentials not found | "No secret source available. Run 'vibewarden generate' to create credentials, or start the stack with 'vibewarden dev'." |
| `ErrSecretNotFound` | Path/alias not found in any available source | "Secret '<path>' not found." |
| OpenBao connection error | Network/auth failure | "Failed to connect to OpenBao: <error>. Falling back to .credentials file." (warn, then try fallback) |
| Invalid alias | User types unknown alias-like string | Treated as OpenBao path, then "Secret '<path>' not found in OpenBao. Use 'vibew secret list' to see available secrets." |

#### Test Strategy

**Unit tests (mocked ports):**

| Test | What it verifies |
|------|------------------|
| `TestResolveAlias_WellKnown` | All 4 aliases resolve correctly |
| `TestResolveAlias_Unknown` | Unknown returns nil |
| `TestService_Get_OpenBaoFirst` | OpenBao is queried before .credentials |
| `TestService_Get_FallbackToCredentials` | Falls back when OpenBao health fails |
| `TestService_Get_ArbitraryPath` | Non-alias paths go directly to OpenBao |
| `TestService_Get_ErrNoSourceAvailable` | Error when both sources unavailable |
| `TestService_List_MergesSources` | Combines aliases + OpenBao paths |
| `TestFormatHuman` | Human output formatting |
| `TestFormatJSON` | JSON output formatting |
| `TestFormatEnv` | Env export formatting |

**CLI integration tests:**

| Test | What it verifies |
|------|------------------|
| `TestSecretGet_HelpOutput` | Help text shows aliases and examples |
| `TestSecretGet_UnknownAlias` | Appropriate error message |
| `TestSecretList_HelpOutput` | Help text is correct |

#### New Dependencies

None. Uses only existing adapters and Go standard library.

### Consequences

**Positive:**
- Users can retrieve credentials without inspecting `.credentials` or OpenBao UI
- Machine-readable formats (`--json`, `--env`) enable scripting and tool integration
- Well-known aliases abstract away OpenBao path structure
- Fallback to .credentials works before `docker compose up`
- Consistent with the secret management model from ADR-009

**Negative:**
- Arbitrary OpenBao paths require OpenBao to be running (no fallback)
- `openbao` alias only works from .credentials (root token not in OpenBao)

**Trade-offs:**
- OpenBao-first vs credentials-first: OpenBao-first ensures dynamic credentials are fresh
- Alias abstraction vs direct paths: aliases are user-friendly but hide implementation details

---
