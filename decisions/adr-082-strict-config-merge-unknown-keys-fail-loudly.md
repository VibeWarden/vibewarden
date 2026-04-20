# ADR-082: Strict config merge — route prod-override through YAML deep-merge, fail loudly on unknown keys in validate

**Date**: 2026-04-20
**Issue**: #1053
**Status**: Accepted

## Context

`vibew deploy` loads a production override file (`vibewarden.production.yaml`)
and overlays it on top of the base `vibewarden.yaml`. Two merge paths exist in
`internal/app/deploy/`:

1. **Struct overlay** — `overlayProdConfig(base, prod *config.Config)` in
   `resolve.go` lines 183–204. A hand-written allow-list that copies a handful
   of fields (`Server.Port`, `TLS.Enabled`, `TLS.Provider`, `TLS.Domain`,
   `Log.Level`, `WAF.Mode`) and silently drops everything else.
2. **Raw YAML deep-merge** — `MergeConfigYAML` in `resolve.go` lines 87–114.
   Operates on `map[string]any`, preserves arbitrary keys, works correctly.

The allow-list is the bug site. `TLSConfig.Email` (added by ADR-078) and
`TLSConfig.ACMECA` (added by ADR-079) are valid schema fields loaded by
`config.Load`, but they are absent from the allow-list. When a user sets
`tls.email` only in the production override, the field is dropped from the
merged `*config.Config` that feeds:

- the Docker Compose template,
- the generator input (`resolved.ToGeneratorInput()`),
- the deploy health-check TLS state,
- the runtime `ports.TLSConfig` observed by the Caddy ACME issuer.

The on-disk bundle YAML (written via `buildMergedConfigYAML` → raw YAML
deep-merge) does carry the override, but the runtime and template-driven code
paths read the broken `*config.Config`. The user sees TLS handshake failures in
production and no clear error.

ADR-078 promised `tls.email` wires to the single-site ACME issuer. This bug is
a direct regression of that promise when the field is set only in the
production override.

### Related constraint: ADR-065

ADR-065 documented why `DecoderConfig.ErrorUnused` stays `false` in
`config.Load`. The goal is forward-compat: a config file that contains a
pre-release key (e.g. `new_plugin.enabled`) must still load on an older binary
that does not know that key. Flipping `ErrorUnused: true` globally would break
that property. Any "unknown key" check must therefore live outside the viper
decoder path.

### Related constraint: `vibew deploy` sunset

Issue #1051 sunsets `vibew deploy`. The generator logic (including the merge
path that contains this bug) is being lifted into `internal/bundle/` as part
of #1044. Any fix must survive that move — i.e. must not entrench the
`internal/app/deploy/` location or the `deploy`-specific semantics of
`overlayProdConfig`.

## Decision

Two separate, minimal changes. Each is independent.

### Decision 1 — replace `overlayProdConfig` with a YAML round-trip

Delete the hand-written allow-list. Route the struct overlay through the
already-correct YAML deep-merge path. Concretely, `LoadMergedConfig` becomes:

```
1. Read baseYAML   from ConfigPath (or marshal cfg when ConfigPath is empty).
2. Read prodYAML   from ProdConfigPath.
3. merged map := MergeConfigYAML(baseYAML, prodYAML).
4. Marshal merged map back to YAML bytes.
5. Write bytes to a tempfile, call config.Load(tempfile), return the Config.
```

Result: a single canonical merge path (the YAML deep-merge) feeds both:

- the on-disk `vibewarden.yaml` shipped in the bundle, and
- the typed `*config.Config` used for template rendering and runtime checks.

Every schema-valid field in the production override now reaches the runtime
struct, including `tls.email`, `tls.acme_ca`, `tls.cert_monitoring.*`,
`server.host`, `rate_limit.enabled`, and every future field added under any
plugin section. The allow-list is replaced by the full viper decode, which
already knows every field.

Environment-variable precedence (`VIBEWARDEN_TLS_EMAIL`) is preserved because
`config.Load` applies env-var overrides on top of the merged file — identical
to the base-file-only path.

Naming and package placement: the new helper (`MergeConfigToStruct` or
equivalent; dev picks the exact name) moves alongside `MergeConfigYAML`. When
`internal/app/deploy/` is renamed to `internal/bundle/` per #1044, the helper
moves with it. Nothing in the helper references `deploy`-specific types.

### Decision 2 — strict unknown-key check in `vibew validate` only

`vibew validate` gains an additional pass that walks the raw
`viper.AllSettings()` tree of both the base file AND the production override
(when both are provided) and reports any key that does not map to a struct
field in `*config.Config`. The check uses reflection over the mapstructure
tags on the `Config` type — the same metadata viper already consumes — so it
cannot drift from the schema.

`config.Load` stays unchanged: `ErrorUnused: false`, unknown keys silently
accepted. ADR-065's forward-compat property is preserved for the runtime path.

The loudness of the failure is therefore:

- **`vibew validate`** — unknown keys are a hard error. Exit code 1. The
  error names each offending key and the file it came from.
- **`vibew deploy` / `vibew bundle`** — runs `vibew validate` as a
  prerequisite. If validate fails, the deploy/bundle does not proceed.
- **`vibewarden serve` (runtime)** — unknown keys continue to be silently
  accepted. A running instance must not be killed by a forward-compat key
  that was valid in a newer release.

This split is deliberate. The target user is a vibe coder. They run
`vibew validate` (or `vibew deploy`, which wraps it) before shipping. A typo
(`tls.dmain: foo`) fails there with a clear message naming the file and the
key — the single point in the workflow where human attention is present. The
runtime path, where a silent forward-compat load matters, is untouched.

### Error shape

The check returns a structured error:

```go
// internal/config/strict.go

// UnknownKeyError reports keys present in a YAML source that do not map to
// any field in the Config schema. File is the path the keys were read from
// (empty when loaded via env / defaults only). Keys is the list of dotted
// paths (e.g. "tls.dmain", "unknown_plugin.enabled").
type UnknownKeyError struct {
    File string
    Keys []string
}

func (e *UnknownKeyError) Error() string { /* … */ }
```

New public function:

```go
// LoadStrict behaves like Load but additionally rejects any key present in
// the YAML file(s) that does not map to a field in Config. It is the loader
// used by `vibew validate` and by any caller that explicitly opts in to
// strict-schema enforcement. Load, and therefore the runtime, are unchanged.
func LoadStrict(configPath, prodConfigPath string) (*Config, error)
```

Callers:

- `vibew validate` — switches from `config.Load` to `config.LoadStrict`.
- `vibew deploy` and future `vibew bundle` — already call validate-equivalent
  checks; they additionally call `LoadStrict` before proceeding. When validate
  fails they print the structured error and exit 1.

## Consequences

### Positive

- The struct overlay and the YAML overlay converge on a single merge path.
  The allow-list disappears. Every schema field in a production override
  reaches the runtime struct.
- Regression guard for ADR-078 (`tls.email`) and ADR-079 (`tls.acme_ca`)
  restored.
- `vibew validate` catches typos under any plugin section — not only under
  `auth`. The user sees an actionable error with file and key.
- ADR-065's forward-compat property preserved: the runtime loader still
  accepts unknown keys silently.
- Fix survives the `deploy → bundle` package move. `MergeConfigYAML` and
  `LoadStrict` are both package-agnostic.

### Negative / breaking

- **`vibew validate` becomes stricter.** Users who had typos in their
  `vibewarden.yaml` or `vibewarden.production.yaml` that were being silently
  ignored (e.g. `tls.dmain: example.com`) now see a validate error. This is
  the intended failure — silent drop of typos was the root cause of #1053 —
  but it is a breaking change relative to v0.15.0 behaviour.
- Users who relied on the silent-drop to keep their own scratch annotations
  inside the YAML (e.g. `_notes: "staging cutover 2026-04-18"`) must move
  those annotations to YAML comments (`# staging cutover …`). The migration
  is trivial.
- `LoadMergedConfig` gains a tempfile write in the round-trip. The write goes
  to `os.TempDir()` and is removed before return. This is acceptable for a
  one-shot deploy/bundle command but would be unacceptable in a hot-path
  runtime — it is not used in the runtime.

### Migration note (for the CHANGELOG)

> **Breaking (`vibew validate`)**: `vibew validate` now rejects unknown keys
> under any plugin section with an actionable error naming the file and the
> key. Previously, unknown keys were silently dropped, which masked typos and
> caused silent misconfiguration in production (#1053). The runtime loader
> (`vibewarden serve`) is unchanged — it continues to accept unknown keys for
> forward-compat per ADR-065. If you have scratch annotations in your YAML,
> move them to YAML comments (`# …`).

## Related ADRs

- **ADR-065** — reconciles auth config surface, documents why `ErrorUnused`
  stays `false` on the runtime `Load`. This ADR preserves that decision and
  adds a strict sibling loader used only by `vibew validate`.
- **ADR-078** — wires `tls.email` to the single-site Caddy ACME issuer. The
  regression fixed here is specifically that ADR-078's promise breaks when
  `tls.email` is set only in the prod-override file.
- **ADR-079** — adds `tls.acme_ca` and the ACME fallback chain. Same
  regression class.
- **Issue #1044 / #1051** — `vibew deploy` sunset. The fix is package-move
  safe: the merge helper lives in `internal/config/` and the
  `LoadMergedConfig` helper moves with whatever package owns the bundle logic.
