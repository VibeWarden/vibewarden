## ADR-076: secret:// URI Resolution in vibewarden.yaml Config
**Date**: 2026-04-18
**Issue**: #1008
**Status**: Accepted

### Context

VibeWarden has a built-in AES-256-GCM encrypted secret store (ADR-074,
`internal/adapters/builtin/store.go`) that users populate via
`vibew secret set auth/google client_id=xxx client_secret=xxx`. The store
persists to `.vibewarden/secrets.enc`.

Currently, sensitive values in `vibewarden.yaml` are specified as plaintext
strings or environment variable references (`${VAR_NAME}`). This forces users
to either commit plaintext secrets in YAML or manage parallel env var setups.
The encrypted store already exists but has no integration point with the
config file.

Users need a way to reference secrets stored in the encrypted store directly
from `vibewarden.yaml`. The `secret://` URI scheme provides a clear, typed
reference that the config loader can resolve before any downstream consumer
(template rendering, deploy bundling, sidecar startup) sees the value.

### Decision

Introduce a `secret://path/key` URI scheme and a **SecretResolver** that
post-processes the loaded `Config` struct, replacing all `secret://` string
values with their resolved plaintext equivalents from the `SecretStore`.

#### URI Syntax

```
secret://auth/google/client_id
         └──────────┘ └────────┘
           path          key
```

- Scheme: `secret://` (literal, case-sensitive)
- Path: everything up to and including the last `/`-delimited segment before
  the final segment. In the example, path = `auth/google`.
- Key: the final `/`-delimited segment. In the example, key = `client_id`.
- The minimum valid URI is `secret://path/key` (at least one `/` after `://`).
- No query parameters, fragments, or authority component.

Parsing rule: split the string after `secret://` on `/`. The last element is
the key; everything before it, joined by `/`, is the path. This aligns with
the existing `SecretStore.Get(ctx, path) -> map[string]string` contract
where `path` maps to a key/value map.

Example YAML:

```yaml
auth:
  social_providers:
    - provider: google
      client_id: secret://auth/google/client_id
      client_secret: secret://auth/google/client_secret
```

After resolution, the Config struct contains the plaintext values as if the
user had written them directly.

#### Resolution Point

**After `config.Load()`, before validation.** A new `ResolveSecrets` function
takes a `*Config` and a `SecretStore`, walks all string fields that may
contain `secret://` URIs, resolves them, and writes the plaintext back into
the struct. The resolved `*Config` is then validated and passed to downstream
consumers (template renderer, deploy bundler, etc.).

This is the correct point because:

1. **Before validation** -- validation checks that `client_secret` is non-empty.
   If we resolved after validation, `secret://...` strings would fail the
   "required field" check or worse, pass validation as a non-empty string that
   is not actually a secret.
2. **After Load** -- `config.Load()` uses viper to unmarshal the YAML. The
   `secret://` strings are just opaque string values to viper. Resolution is
   a separate concern that requires the SecretStore, which has its own
   initialization (master key, file path).
3. **Exactly once** -- resolving in the config pipeline (not at template time
   or runtime) means every consumer of `*Config` gets resolved values. There
   is no risk of templates rendering literal `secret://` URIs.

The sequence in every CLI command becomes:

```
config.Load(path) -> config.ResolveSecrets(cfg, store) -> cfg.Validate() -> use cfg
```

Today, `config.Load()` calls `cfg.Validate()` internally. To support
resolution before validation, `Load` is split: a new `LoadRaw` function
returns the config without validation, and `Load` remains as a convenience
that calls `LoadRaw` + `Validate` for callers that do not use secret URIs.
Commands that support `secret://` call `LoadRaw`, then `ResolveSecrets`,
then `Validate`.

#### Which Fields Support It

All string fields in the Config struct support `secret://` URIs. Rather than
maintaining a curated allowlist (which would need updating every time a new
sensitive field is added), the resolver walks the struct recursively and
resolves any string value that starts with `secret://`.

This is safe because:
- The `secret://` prefix is syntactically unambiguous -- no legitimate config
  value would start with `secret://`.
- The resolver skips non-string fields, slice elements that are not strings or
  structs, and map values that are not strings.
- If a `secret://` URI cannot be resolved, the resolver returns an error
  immediately (fail-fast).

Primary use cases (the fields users will most commonly reference):
- `auth.social_providers[].client_id`
- `auth.social_providers[].client_secret`
- `auth.jwt.jwks_url` (when stored as a secret)
- `secrets.openbao.auth.token`
- `secrets.openbao.auth.secret_id`
- `admin.token`
- `database.url`
- `database.external_url`
- `webhooks.endpoints[].url`
- Any future field that holds a sensitive value

#### Domain Model Changes

**New value object in `internal/domain/secret/uri.go`:**

```go
// SecretURI is a parsed secret:// URI.
// It is a value object — immutable after construction.
type SecretURI struct {
    Path string // e.g. "auth/google"
    Key  string // e.g. "client_secret"
}
```

**New pure functions:**

```go
// ParseSecretURI parses a secret:// URI string.
// Returns (SecretURI, true) if the string is a valid secret:// URI,
// or (SecretURI{}, false) if not.
func ParseSecretURI(s string) (SecretURI, bool)

// IsSecretURI reports whether s starts with "secret://".
func IsSecretURI(s string) bool
```

These functions live in the domain layer (`internal/domain/secret/`) because
they are pure parsing logic with zero external dependencies. The URI format
is part of the domain vocabulary.

No new entities, aggregates, or domain events.

#### Ports (interfaces)

**New interface in `internal/ports/secrets.go`:**

```go
// SecretResolver resolves secret:// URIs in a Config struct by replacing
// them with plaintext values from the secret store.
type SecretResolver interface {
    // ResolveConfig walks all string fields of cfg. For any field whose
    // value starts with "secret://", it parses the URI, fetches the
    // corresponding value from the secret store, and replaces the field
    // in-place. Returns an error if any secret:// URI cannot be resolved.
    ResolveConfig(ctx context.Context, cfg *config.Config) error
}
```

Note: This interface imports `internal/config`, which is acceptable because
`internal/ports/` already imports `internal/domain/*` and config is a
neighbouring internal package. However, to avoid a circular dependency (since
`config` does not import `ports`), the `SecretResolver` interface will be
defined in a new file `internal/ports/secret_resolver.go` and the concrete
implementation lives in `internal/config/`. The `Config` type is passed as
`any` to avoid the import cycle:

```go
// SecretResolver resolves secret:// URIs in configuration values.
type SecretResolver interface {
    // ResolveConfig walks all string fields in the config struct and
    // replaces secret:// URI values with plaintext from the secret store.
    // The cfg parameter must be a *config.Config.
    ResolveConfig(ctx context.Context, cfg any) error
}
```

On reflection, this introduces an `any` parameter which is not type-safe.
A cleaner approach: the resolution function is a **standalone function** in
`internal/config/` (not behind an interface) because it operates directly on
the `Config` struct and is always called in the config loading pipeline. The
function accepts a `ports.SecretKVReader` (the narrow read-only port) as its
store dependency:

```go
// internal/config/resolve_secrets.go
func ResolveSecrets(ctx context.Context, cfg *Config, store ports.SecretKVReader) error
```

This avoids a new port interface entirely. The `SecretKVReader` port already
exists and is the right abstraction -- we only need `Get(ctx, path)`.

#### Adapters

No new adapters. The existing `builtin.Store` and `openbao.Adapter` both
implement `ports.SecretKVReader` (via `SecretStore` embedding). The
`ResolveSecrets` function accepts the narrow `SecretKVReader` interface.

#### Application Service

No new application service. Secret resolution is a config-loading concern,
not a use case. The `ResolveSecrets` function lives in `internal/config/`
alongside `Load` and `Validate`.

The calling sites (CLI commands, plugin init) wire the resolution:

```go
cfg, err := config.LoadRaw(configPath)
if err != nil { return err }

store, err := buildSecretStore(cfg) // uses cfg.Secrets.* to build the store
if err != nil { return err }

if store != nil {
    if err := config.ResolveSecrets(ctx, cfg, store); err != nil {
        return fmt.Errorf("resolving secret:// URIs: %w", err)
    }
}

if err := cfg.Validate(); err != nil {
    return fmt.Errorf("invalid config: %w", err)
}
```

Note the ordering: `cfg.Secrets.*` fields themselves must NOT be `secret://`
URIs, because the store needs to be constructed before resolution. This is
a documented constraint: the `secrets.*` config section cannot use `secret://`
references (it would be circular -- you need the store to resolve the store's
own config). This is acceptable because `secrets.builtin.key_file` is a file
path (not a secret value) and `secrets.builtin.path` is a file path.

#### File Layout

New files:

```
internal/domain/secret/uri.go          # SecretURI value object, ParseSecretURI, IsSecretURI
internal/domain/secret/uri_test.go     # table-driven tests for URI parsing
internal/config/resolve_secrets.go     # ResolveSecrets function
internal/config/resolve_secrets_test.go # tests for struct walking and resolution
internal/config/load_raw.go            # LoadRaw function (Load without Validate)
```

Modified files:

```
internal/config/config.go              # extract Validate call from Load into LoadRaw+Load
internal/cli/cmd/dev.go                # wire ResolveSecrets after LoadRaw, before use
internal/cli/cmd/generate.go           # wire ResolveSecrets after LoadRaw, before use
internal/cli/cmd/deploy.go             # wire ResolveSecrets in deploy pipeline
internal/cli/cmd/secret_set.go         # no change (secret set does not read config values)
internal/cli/cmd/secret_get.go         # buildSecretService may use LoadRaw
```

#### Sequence

**Config loading with secret:// resolution (e.g. `vibew dev`):**

1. CLI calls `config.LoadRaw(configPath)` -- viper reads YAML, env vars,
   unmarshals into `*Config`. No validation yet.
2. CLI calls `buildSecretStore(cfg)` -- reads `cfg.Secrets.Store`,
   `cfg.Secrets.Builtin.*` to construct the appropriate `SecretStore`.
   Returns nil if no master key is available.
3. If store is non-nil, CLI calls `config.ResolveSecrets(ctx, cfg, store)`.
4. `ResolveSecrets` uses reflection to walk all exported string fields of
   `*Config` recursively (including slices of structs, nested structs).
5. For each string field whose value starts with `secret://`:
   a. Parse the URI via `secret.ParseSecretURI(value)` -- extract path and key.
   b. Call `store.Get(ctx, path)` to fetch the key/value map.
   c. Look up `key` in the returned map.
   d. If found, replace the struct field value with the plaintext.
   e. If not found (path missing or key missing), return an error immediately.
6. `ResolveSecrets` caches `store.Get` results per path to avoid redundant
   decryptions when multiple fields reference the same path (e.g. both
   `client_id` and `client_secret` from `auth/google`).
7. CLI calls `cfg.Validate()` on the resolved config.
8. CLI proceeds with the fully-resolved, validated `*Config`.

**Deploy bundle with secret:// resolution:**

1. `vibew deploy` calls `config.LoadRaw(configPath)`.
2. Builds the secret store from `cfg.Secrets.*`.
3. Calls `config.ResolveSecrets(ctx, cfg, store)`.
4. Calls `cfg.Validate()`.
5. Passes the resolved `*Config` to `deploy.Bundle(ctx, opts)`.
6. The deploy bundle writes `vibewarden.yaml` with resolved values (no
   `secret://` URIs in the bundle). The bundle is self-contained.

**Template rendering (Kratos config):**

- No changes needed. The Kratos template already reads `{{ .ClientSecret }}`
  from `SocialProviderConfig`. After resolution, `ClientSecret` contains the
  plaintext value. The template is unaware of `secret://`.

#### Error Cases

| Error | Cause | Handling |
|---|---|---|
| Invalid secret URI format | `secret://` with no path or key (e.g. `secret://`, `secret://foo`) | `ResolveSecrets` returns `fmt.Errorf("invalid secret:// URI %q: must have at least path/key", uri)` |
| Secret path not found | `store.Get` returns `ErrSecretNotFound` | `ResolveSecrets` returns `fmt.Errorf("resolving %q: secret path %q not found in store", fieldPath, uri.Path)` with the YAML field path for context |
| Secret key not found | Path exists but key is not in the map | `ResolveSecrets` returns `fmt.Errorf("resolving %q: key %q not found at path %q", fieldPath, uri.Key, uri.Path)` |
| Master key not available | No env var, no key file | `buildSecretStore` returns nil. `ResolveSecrets` is skipped. If any field contains `secret://`, it will fail at `Validate()` (e.g. empty `client_secret` after the `secret://` string is not resolved). This is intentional -- the user must set up the master key. |
| Store decryption failure | Wrong master key or corrupt file | `store.Get` returns an error, propagated by `ResolveSecrets` |
| Circular reference | `secrets.builtin.*` fields use `secret://` | Not possible by construction: `buildSecretStore` reads `cfg.Secrets.*` before `ResolveSecrets` runs. Document this constraint. |

#### Test Strategy

**Unit tests** (`internal/domain/secret/uri_test.go`):

- Table-driven tests for `ParseSecretURI`:
  - Valid: `secret://auth/google/client_id` -> path=`auth/google`, key=`client_id`
  - Valid: `secret://a/b` -> path=`a`, key=`b`
  - Valid: `secret://deep/nested/path/key` -> path=`deep/nested/path`, key=`key`
  - Invalid: `secret://` (no path/key)
  - Invalid: `secret://onlyone` (no slash after scheme)
  - Invalid: `secrets://wrong-scheme` (wrong scheme)
  - Invalid: `plain-string` (no scheme)
  - Invalid: `secret://path/` (trailing slash, empty key)
- `IsSecretURI` returns true only for `secret://` prefix.

**Unit tests** (`internal/config/resolve_secrets_test.go`):

- Mock `SecretKVReader` (simple in-memory map).
- Test that a Config with `secret://` values in various fields is resolved.
- Test nested struct resolution (e.g. `SocialProviderConfig.ClientSecret`).
- Test slice-of-struct resolution (multiple social providers).
- Test caching: two fields referencing same path cause only one `Get` call.
- Test error: missing path returns descriptive error.
- Test error: missing key at existing path returns descriptive error.
- Test error: invalid URI format returns descriptive error.
- Test passthrough: fields without `secret://` are unchanged.
- Test that `secrets.*` fields are not resolved (skipped by field path).
- All tests use in-memory mock store; no disk I/O needed.

**Unit tests** (`internal/config/load_raw_test.go`):

- `LoadRaw` returns config without calling `Validate`.
- Verify that invalid config values pass through `LoadRaw` without error.

**Integration tests**: not needed. The resolution logic is pure struct
walking + `SecretKVReader.Get` calls. The store adapters are already tested
(ADR-074 unit tests, OpenBao integration tests).

#### New Dependencies

**None.** The implementation uses only Go stdlib:
- `reflect` for struct walking in `ResolveSecrets`
- `strings` for URI parsing
- `context` for the `Get` call
- `fmt` for error formatting

### Consequences

**Benefits:**

- Users can store all sensitive config values in the encrypted store and
  reference them from `vibewarden.yaml`, eliminating plaintext secrets in
  config files.
- The resolution is transparent to all downstream consumers (templates,
  deploy bundle, sidecar). No changes needed in template rendering or
  deploy bundling.
- The `secret://` scheme is unambiguous and self-documenting in YAML.
- Compatible with both `builtin` and `openbao` stores -- any
  `SecretKVReader` implementation works.

**Trade-offs:**

- Uses `reflect` for struct walking. This adds complexity but is contained
  in a single function with comprehensive tests. The alternative (a manually
  maintained list of resolvable fields) would be error-prone and require
  changes every time a new sensitive field is added.
- The `secrets.*` config section cannot itself use `secret://` URIs (bootstrap
  circularity). This is an acceptable constraint -- the secret store config
  is bootstrapped from env vars and file paths, not from itself.
- `LoadRaw` + `ResolveSecrets` + `Validate` is a three-step pipeline instead
  of the current one-step `Load`. This adds wiring complexity to CLI commands
  but is necessary for correct ordering.

**Migration path:**

- Existing configs with plaintext or `${VAR}` values continue to work
  unchanged. `secret://` is purely opt-in.
- Users can migrate incrementally: replace one field at a time with a
  `secret://` URI after storing the value via `vibew secret set`.
- A future `vibew config check` command could warn about plaintext secrets
  and suggest `secret://` replacements.
