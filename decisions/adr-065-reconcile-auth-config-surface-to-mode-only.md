# ADR-065: Reconcile `auth` config surface to mode-only

**Date**: 2026-04-15
**Status**: Accepted
**Issue**: #816

### Context

User and writer audits (2026-04-16) surfaced a real ambiguity in the
authentication configuration surface. The `auth` block in
`vibewarden.yaml` exposed two overlapping on/off axes:

- `auth.enabled: true | false`
- `auth.mode: "none" | "kratos" | "jwt" | "api-key"`

Across the repo they were mixed inconsistently: the demo-app used both
(`enabled: true` *and* `mode: "kratos"`), `vibewarden.example.yaml` and
`vibewarden.reference.yaml` used `mode` only, the newer
nextjs/node-express/python-flask/spring-boot examples used `mode` only,
and `docs/configuration.md` documented both. The runtime truth table
depended on both fields — `cfg.Auth.Enabled && cfg.Auth.Mode ==
AuthModeKratos` — so a vibe coder could not tell from the docs which
form was canonical.

The validator already rejected `enabled: true + mode: "none"` but
silently accepted the legacy shape, `mode`-only shape, and the mixed
shape the demo-app used. A new user reading three docs and two examples
would see three different conventions.

### Decision

**`auth.mode` is the single source of truth.** `auth.enabled` is
removed from the config schema. The `mode` enum already contains an off
sentinel (`none`), so the on/off axis is unambiguously expressed by
`mode`.

**Canonical form:**

```yaml
auth:
  mode: "none"          # disable auth (also the default)
  # mode: "kratos"      # Ory Kratos session auth
  # mode: "jwt"         # JWT / OIDC bearer auth
  # mode: "api-key"     # API key header auth
```

Runtime derivation collapses to a new `AuthConfig.Active()` helper:

```go
func (a AuthConfig) Active() bool {
    return a.Mode != "" && a.Mode != AuthModeNone
}
```

Every consumer that previously read `cfg.Auth.Enabled` now calls
`cfg.Auth.Active()`. The helper centralises the "is auth on" derivation
so future code does not drift back into ad-hoc `mode == "kratos" ||
mode == "jwt" || ...` checks.

### Ruling on the three open questions from the PM

1. **Option A — mode-only.** `auth.enabled` is deleted. `auth.mode` is
   the only on/off axis for auth. The enum already contains `none` as
   an off sentinel, so a separate boolean is redundant.

2. **Hard break — no deprecation path.** The validator rejects any
   presence of `auth.enabled` (true *or* false) with a single,
   actionable error. Justifications: repo is pre-1.0 (current release
   v0.10.0), zero stars, zero forks, no semver promise for the YAML
   schema. A deprecation path costs validator complexity and prolongs
   the ambiguity in docs for months. A clean hard-error with a message
   that names the replacement inline is strictly better UX than a silent
   acceptance that later surprises.

3. **Auth-only — do not touch other plugins.** `rate_limit.enabled`,
   `security_headers.enabled`, `cors.enabled`, `fleet.enabled`, and
   `admin.enabled` are single-axis booleans with no `mode` companion —
   not ambiguous. `waf.mode` (`block | detect`) is orthogonal to
   `waf.enabled`: `mode` describes the enforcement action when the
   plugin is on, not whether the plugin is on — different semantics.
   `tls.provider` is orthogonal to `tls.enabled`: `provider` selects
   the certificate source when TLS is on, it is not an off sentinel.
   `auth` is the only plugin whose `mode` enum contains an off sentinel
   (`none`), which is why the fold is valid here and not elsewhere.

### Validator error message (verbatim)

The `Load` path inspects `viper.AllSettings()["auth"]` for an `enabled`
key. When present, `Load` returns:

```
auth.enabled is no longer a recognised config key (removed in v0.11.0; see ADR-065). Use auth.mode as the single source of truth: set auth.mode: "none" to disable auth, or auth.mode: "kratos" | "jwt" | "api-key" to enable a strategy.

Canonical form:

  auth:
    mode: "none"          # disable auth
    # mode: "kratos"      # enable Ory Kratos session auth
    # mode: "jwt"         # enable JWT / OIDC bearer auth
    # mode: "api-key"     # enable API key header auth
```

The existing `enabled: true + mode: "none"` validator error is removed
— subsumed by the new rule, which rejects the field unconditionally.

### Why the raw-key check lives in `Load`, not `Validate`

`Validate` operates on the already-unmarshalled `*Config` and so cannot
distinguish "user set `auth.enabled: false`" from "user did not set
`auth.enabled` at all" once the struct field is removed. The raw YAML
map from `viper.AllSettings()` is the only source of truth for key
presence, and it is only available inside `Load`. Placing the check
there is correct — `Validate` continues to operate on typed fields as
before.

### Why `DecoderConfig.ErrorUnused` stays `false`

Viper's `Unmarshal` accepts extra keys by default. If a future change
flipped `ErrorUnused: true`, the unknown `auth.enabled` key would be
rejected a second time by the decoder, with a generic error message
("has invalid keys: enabled") that is strictly worse than the ADR-065
message. The explicit raw-key check in `Load` is the sole authoritative
rejector for `auth.enabled`. A test in
`internal/config/config_test.go` locks in that no decoder setting
shadows the explicit message.

### "This file intentionally left unchanged"

The following plugin config surfaces are **not** touched by this ADR:

- `waf.mode` (`block | detect`) — semantically orthogonal to
  `waf.enabled`.
- `tls.provider` (`letsencrypt | self-signed | external`) — selects a
  strategy when TLS is on, not an off sentinel.
- `rate_limit.enabled`, `cors.enabled`, `security_headers.enabled`,
  `fleet.enabled`, `admin.enabled` — no `mode` companion.

The reviewer must reject any PR under this ADR that changes these
surfaces. Scope creep here was the PM's first open question and the
answer was explicit: auth only.

### Migration order

To keep the tree green at each commit:

1. **Schema.** Remove `config.AuthConfig.Enabled`, add
   `AuthConfig.Active()`, delete the `v.SetDefault("auth.enabled",
   false)` call, and delete the
   `if c.Auth.Enabled && c.Auth.Mode == AuthModeNone` validator rule.
2. **Runtime call sites (12).** `adapters/caddy/config.go` (godoc
   only — the runtime reads `ports.AuthConfig.Enabled`, which is
   populated from `cfg.Auth.Active()` at the boundary),
   `plugins/builtin.go`, `mcp/tools.go` (two sites, including the
   error message), `cli/cmd/generate.go`, `cli/cmd/plugins.go`,
   `cli/cmd/validate.go`, `app/eject/eject.go`,
   `app/generate/service.go`, `app/serve/config.go`,
   `app/ops/status.go` (two sites), `config/generator_input.go`.
3. **Templates.** Six template branches in
   `config/templates/docker-compose.yml.tmpl` that gate Kratos
   services; the `wrap` template in `cli/templates/vibewarden.yaml.tmpl`
   gains a `mode: "kratos"` line so `vibew wrap --auth` produces a
   canonical config.
4. **YAML examples (2).** `examples/demo-app/vibewarden.yaml` drops
   `enabled: true`; `examples/vibewarden.prod.yaml` drops `enabled: true`.
   The other five example YAMLs are already canonical.
5. **Docs (6).** `docs/configuration.md` drops the `auth.enabled` row
   and adds a "Canonical auth config" section; the four example-app
   READMEs and `examples/demo-app/README.md` drop the `enabled: true`
   line; `llms-full.txt` updates the `auth.enabled` bullet to
   `auth.mode`.
6. **CHANGELOG.** A `### Breaking` entry under the Unreleased
   heading names the removed field, the replacement, the error
   message preview, and this ADR.
7. **Raw-key check and hard-break test.** Last because it actively
   rejects shapes that earlier commits may still carry.

### Behavioural-equivalence criteria

A byte-identical guarantee is impossible because YAML line numbers
shift when the `enabled` field is removed. Instead:

- **Semantic equivalence on canonical configs.** Every YAML in the
  repo that is already `mode`-only (`vibewarden.example.yaml`,
  `vibewarden.reference.yaml`, `examples/nextjs/`,
  `examples/node-express/`, `examples/python-flask/`,
  `examples/spring-boot/`) is a pure no-op: `Load` and `Validate`
  succeed with zero behavioural change.
- **Semantic equivalence for the two fixed configs.**
  `examples/demo-app/vibewarden.yaml` (was `enabled: true + mode:
  "kratos"`) becomes `mode: "kratos"` — `cfg.Auth.Active()` returns
  `true` before and after. `examples/vibewarden.prod.yaml` (was
  `enabled: true + mode: "jwt"`) becomes `mode: "jwt"` —
  `cfg.Auth.Active()` returns `true` before and after.
- **Generator output stability.** `TestGenerate_Compose_*` tests
  already exist and assert the presence/absence of Kratos services
  based on mode. They pass unchanged after the runtime derivation
  switches to `Active()`.

### Test strategy

New / updated tests:

- `TestValidate_AuthEnabledModeNone` — deleted. The tests encoded the
  old ambiguous-pair rule that no longer exists. The file-level
  rejection is covered by the new hard-break test below.
- `TestLoad_RejectsAuthEnabled` — new. Table-driven over five inputs:
  `auth.enabled: true`, `auth.enabled: false`, both with and without a
  valid `auth.mode`, and a control row that has `auth.mode: "kratos"`
  only and must succeed. Every failing row must match the exact error
  text from the ADR.
- `TestLoad_AuthModeOnly_BehavioralEquivalence` — new. Canonical
  `mode: "kratos"` config must `Load` successfully and produce
  `cfg.Auth.Active() == true`; `mode: "none"` config must produce
  `cfg.Auth.Active() == false`; omitted `auth` section must produce
  `cfg.Auth.Active() == false`.
- `TestLoad_ErrorUnusedStaysFalse` — new, documentary. Asserts via
  the rejected-shape test that when `auth.enabled` is absent but
  another unknown key like `auth.bogus` is present, `Load` succeeds —
  proving the decoder is not in `ErrorUnused: true` mode. Guards
  against a future loader change accidentally shadowing the ADR-065
  message.

### Consequences

**Positive:**
- One canonical shape for the auth block. Docs, examples, validator,
  and runtime all agree.
- `AuthConfig.Active()` is the single derivation for "is auth on" —
  future callers cannot drift into ad-hoc `mode` string comparisons.
- The error message tells the user exactly what to change, inline.
- Eliminates the bug-shaped "enabled: true + mode: none" misconfig
  that the validator half-rejected.

**Negative:**
- Hard break for the two configs in the repo that carried `enabled:
  true`. Any external user with the same shape sees the actionable
  error on the next load. Pre-1.0 this is the right call but it is
  still a break.
- `ports.AuthConfig.Enabled`, `authplugin.Config.Enabled`, and the
  Caddy adapter's read of `cfg.Auth.Enabled` (the *ports* value, not
  the *config* value) keep their boolean field. The field is populated
  from `cfg.Auth.Active()` at the `app/serve/config.go` and
  `app/eject/eject.go` boundaries. Renaming the port field to `Active`
  was considered and rejected: the port is an adapter-facing DTO, not a
  user-facing schema, and the rename would ripple into three packages
  for no behavioural gain.

### Limitations

- The raw-key check is exact-match on `"enabled"` inside the `auth`
  mapping. It does not catch other legacy spellings (there are none)
  or case variants — viper is case-insensitive for YAML key lookup, so
  `Auth.Enabled` and `AUTH.ENABLED` would both land under `enabled` in
  `AllSettings`. This is the correct behaviour.
- Migration is one-way: once a user's config is updated, they cannot
  downgrade to v0.10.0 without reintroducing `auth.enabled`. Acceptable
  pre-1.0.
