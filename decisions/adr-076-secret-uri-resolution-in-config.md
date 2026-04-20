# ADR-076: secret:// URI resolution in vibewarden.yaml config

## Status

Accepted

## Context

Users need a way to reference secrets stored in the built-in encrypted secret
store directly from `vibewarden.yaml` config fields. Currently, secrets can
only be injected into the upstream app via `secrets.inject.*` headers or env
vars, but config fields themselves (e.g. `auth.social_providers[].client_secret`,
`database.url`, `admin.token`) cannot reference the secret store.

This forces users to either:
- Hardcode secrets in plaintext in `vibewarden.yaml`
- Use environment variable interpolation (`${VAR}`), which requires managing
  env vars separately

## Decision

Add a `secret://` URI scheme that can be used in any string field in the
Config struct. When a field value starts with `secret://`, it is resolved from
the built-in encrypted secret store before the config is validated or used.

### URI format

```
secret://path/key
```

The last segment is the key within the secret, everything before it is the
store path. For example:

```
secret://auth/google/client_id
```

Resolves to: `store.Get(ctx, "auth/google")["client_id"]`

### Resolution mechanism

- Uses Go reflection to walk all exported string fields in the Config struct
- For each string starting with `secret://`, parses the URI, calls
  `SecretKVReader.Get(ctx, path)`, and extracts the key
- Replaces the field value with the resolved plaintext
- Fails fast on the first resolution error with a descriptive message
  including the struct field path

### Bootstrap constraint

The `secrets.*` config section itself cannot use `secret://` URIs because the
secret store is initialised from that section. This is enforced by skipping the
`Secrets` field during resolution.

### Config loading sequence

The existing `config.Load()` (which validates immediately) is preserved for
backward compatibility. A new `config.LoadRaw()` function loads and unmarshals
without validation. The CLI commands that need secret resolution use:

```go
cfg, err := config.LoadRaw(configPath)
config.ResolveSecrets(ctx, cfg, store)
cfg.Validate()
```

## Consequences

- Users can store sensitive values in the encrypted secret store and reference
  them from config without plaintext exposure
- Any string field in Config automatically supports `secret://` URIs without
  requiring an allowlist
- The reflection-based approach means new config fields are automatically
  supported
- Missing secrets produce clear, actionable error messages with field paths
- The `secrets.*` bootstrap section remains free of circular dependencies
