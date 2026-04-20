## ADR-077: Placeholder Substitution for Composite Secret Values
**Date**: 2026-04-18
**Issue**: #994
**Status**: Accepted

### Context

ADR-076 introduced `secret://` URI resolution in `vibewarden.yaml`, allowing
any string field to be replaced entirely by a secret from the encrypted store.
However, this only supports full-field replacement -- a field value is either
entirely a secret or entirely literal.

Users need to embed secrets inside larger strings (composite values):

```yaml
app:
  environment:
    DATABASE_URL: "postgres://user:${secret://db/password}@host:5432/db"
    AUTH_HEADER: "Bearer ${secret://auth/api/token}"
```

Today this requires making the entire connection string a secret (losing
visibility into the non-secret parts) or templating outside VibeWarden
(defeating the sidecar model).

### Decision

Extend the existing `secret://` URI scheme with a `${secret://path/key}`
placeholder syntax. Placeholders can appear anywhere inside a string value
and multiple placeholders can coexist in a single field. The existing
full-field `secret://` resolution (ADR-076) continues to work unchanged.

#### Syntax

- **Placeholder**: `${secret://path/key}` -- the existing `secret://` URI
  wrapped in `${}` delimiters.
- **Escape**: `$${secret://path/key}` -- a literal `$` before `{` produces
  the literal string `${secret://path/key}` with no resolution.
- **Full-field** (unchanged): `secret://path/key` -- the entire field value
  is a secret URI (ADR-076 behavior).

Resolution priority:
1. If a string starts with `secret://` -- full-field resolution (existing).
2. If a string contains `${secret://...}` -- placeholder substitution (new).
3. If a string contains `$${secret://...}` -- unescape to literal (no resolution).
4. Otherwise -- literal passthrough.

#### Domain Model Changes

New types and functions in `internal/domain/secret/uri.go`:

```go
// Placeholder represents a single ${secret://path/key} occurrence in a string.
type Placeholder struct {
    Raw string // full match including ${...} delimiters
    URI URI    // parsed secret URI
}

// ContainsPlaceholder reports whether s contains at least one ${secret://...} placeholder.
func ContainsPlaceholder(s string) bool

// FindPlaceholders extracts all ${secret://...} placeholders from s.
// Escaped placeholders ($${secret://...}) are not included.
func FindPlaceholders(s string) []Placeholder

// UnescapePlaceholders converts $${secret://...} to literal ${secret://...}.
func UnescapePlaceholders(s string) string
```

These are pure functions with zero external dependencies, appropriate for the
domain layer.

#### Config Changes

Add `ValueTemplate` field to `SecretsHeaderInjection` and `SecretsEnvInjection`
in `internal/config/config.go`:

```go
type SecretsHeaderInjection struct {
    SecretPath    string `mapstructure:"secret_path"`
    SecretKey     string `mapstructure:"secret_key"`
    Header        string `mapstructure:"header"`
    ValueTemplate string `mapstructure:"value_template"`
}

type SecretsEnvInjection struct {
    SecretPath    string `mapstructure:"secret_path"`
    SecretKey     string `mapstructure:"secret_key"`
    EnvVar        string `mapstructure:"env_var"`
    ValueTemplate string `mapstructure:"value_template"`
}
```

When `ValueTemplate` is set, the resolved secret value is substituted into
the template at `${secret://...}` positions. When empty, behavior is
unchanged (the raw secret value is used directly).

Mirror in `internal/plugins/secrets/config.go`.

#### Resolution Changes

Extend `resolveStringField` in `internal/config/resolve_secrets.go`:

1. If the string starts with `secret://` -- existing full-field resolution.
2. If the string contains `${secret://...}` -- find all placeholders, resolve
   each, replace in the string, then unescape any `$${...}`.
3. Otherwise -- no-op.

#### Plugin Changes

Extend the secrets plugin header handler and env file writer to support
`ValueTemplate`:

- When `ValueTemplate` is non-empty, resolve `${secret://...}` placeholders
  in it using the cache, then use the result as the header/env value.
- When empty, use the direct secret value (existing behavior).

#### File Layout

Modified files:
```
internal/domain/secret/uri.go            # add Placeholder, ContainsPlaceholder, FindPlaceholders, UnescapePlaceholders
internal/domain/secret/uri_test.go       # placeholder parsing tests
internal/config/resolve_secrets.go       # extend resolveStringField, resolveMap for composite values
internal/config/resolve_secrets_test.go  # composite resolution tests
internal/config/config.go               # add ValueTemplate to SecretsHeaderInjection, SecretsEnvInjection
internal/plugins/secrets/config.go      # add ValueTemplate to HeaderInjection, EnvInjection
internal/plugins/secrets/plugin.go      # extend header handler and env file writer
internal/plugins/secrets/plugin_test.go # composite injection tests
internal/plugins/builtin_helpers.go     # wire ValueTemplate from config to plugin config
```

#### New Dependencies

None. Uses only Go stdlib (`regexp`, `strings`).

### Consequences

**Benefits:**

- Users can embed secrets inside connection strings, authorization headers,
  and other composite values without making the entire string opaque.
- The `${secret://...}` syntax reuses the existing URI scheme, so users do
  not need to learn a new vocabulary.
- Backward-compatible: existing full-field `secret://` resolution is unchanged.
- Escape hatch (`$${...}`) allows literal `${secret://...}` strings when needed.

**Trade-offs:**

- Adds regex-based placeholder detection. Contained in domain-layer pure
  functions with comprehensive tests.
- The `ValueTemplate` field is an additional config surface. However, it is
  optional and backward-compatible.
