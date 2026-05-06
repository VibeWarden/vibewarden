# Secret CLI Commands — Internal Reference

> This file was relocated from `decisions/adr-010-add-vibew-secret-get-and-vibew-secret-list-commands.md`
> on 2026-05-04 as part of the ADR audit (audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`).
> The stub at that path remains stable; existing PR / commit references continue to resolve.

## From ADR-010 — Add `vibew secret get` and `vibew secret list` Commands

**Date**: 2026-03-28 | **Issue**: #286

With the no-secrets-on-disk approach (ADR-009), credentials are randomly generated per
`vibewarden generate` run and seeded into OpenBao. These two subcommands let users retrieve
them for debugging or connecting external tools.

### Commands

- `vibew secret get <alias-or-path>` — retrieve credentials for a service or path
- `vibew secret list` — show all managed secret paths

#### Domain types (in `internal/domain/secret/`)

```go
type RetrievedSecret struct {
    Path   string
    Alias  string
    Data   map[string]string
    Source SecretSource
}

type SecretSource string

const (
    SourceOpenBao        SecretSource = "openbao"
    SourceCredentialsFile SecretSource = "credentials_file"
)
```

#### Port (in `internal/ports/secrets.go`)

```go
type SecretRetriever interface {
    Get(ctx context.Context, aliasOrPath string) (*secret.RetrievedSecret, error)
    List(ctx context.Context) ([]string, error)
}
```

#### Well-known aliases

| Alias | OpenBao Static Path | Dynamic Role | .credentials Keys |
|-------|---------------------|--------------|-------------------|
| `postgres` | `infra/postgres` | `app-readwrite` | `POSTGRES_PASSWORD` |
| `kratos` | `infra/kratos` | — | `KRATOS_SECRETS_COOKIE`, `KRATOS_SECRETS_CIPHER` |
| `grafana` | `infra/grafana` | — | `GRAFANA_ADMIN_PASSWORD` |
| `openbao` | — | — | `OPENBAO_ROOT_TOKEN` |

#### Fallback logic

1. If OpenBao is available: query OpenBao first
2. If OpenBao unavailable (or not configured): fall back to `.credentials` file
3. Arbitrary paths (non-aliases): require OpenBao; no `.credentials` fallback

#### Output formats

- **Default**: human-readable, labeled output
- **`--json`**: machine-readable JSON object
- **`--env`**: `export KEY=VALUE` lines for shell sourcing
