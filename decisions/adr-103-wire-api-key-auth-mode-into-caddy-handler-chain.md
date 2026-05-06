# ADR-103: Wire `auth: api-key` mode into the Caddy handler chain

**Date**: 2026-05-05
**Issue**: #1302
**Status**: Accepted

## Context

`auth.mode: api-key` is documented in `vibewarden.reference.yaml`,
advertised in `internal/plugins/catalog.go` meta, and accepted by config
validation (`internal/config/auth.go`), yet it has never enforced anything
at the HTTP boundary. The reason:

- `internal/plugins/auth/plugin.go:ContributeCaddyHandlers` returns `nil`
  for `ModeAPIKey` with a comment "those modes handle auth elsewhere".
- No Caddy handler module for API-key validation has ever been written.
- `internal/middleware/APIKeyMiddleware` exists and is well-tested, but
  nothing constructs or registers it.
- `internal/adapters/apikey/{config_validator,openbao_validator}.go`
  implement `ports.APIKeyValidator` but are never instantiated outside
  their own test files.

A user who configures `auth.mode: api-key` in `vibewarden.yaml` today gets
**zero authentication enforcement** on proxied requests. This violates the
project's core promise ("zero-to-secure in minutes") and constitutes a
security defect.

The fix is a wire-up, not a redesign. Every required component exists; the
missing link is:
1. A Caddy handler module (`APIKeyHandler`) that wraps
   `middleware.APIKeyMiddleware`.
2. Registration of that handler in `RuntimeServices` so the composition
   root can inject the configured `ports.APIKeyValidator`.
3. `ContributeCaddyHandlers` returning the handler when `ModeAPIKey` is
   active.
4. Composition-root code that constructs the correct validator (config-based
   or OpenBao) and injects it.

The second part of the issue — the orphaned `internal/adapters/logprint/`
package — is a straightforward DELETE with no architectural implications.

## Decision

### What to implement (api-key wire-up)

#### New file: `internal/adapters/caddy/api_key_handler.go`

A Caddy module `http.handlers.api_key_auth` following the exact same
pattern as `jwt_bearer_handler.go` and `ratelimit_handler.go`:

- Struct `APIKeyHandler` embeds `APIKeyHandlerConfig` (header name, scope
  rules serialised to JSON).
- `Provision` retrieves `APIKeyValidator` from `RuntimeServices` (nil-safe
  — logs a one-time Error and rejects all requests if missing, because a
  configured-but-broken api-key handler must fail closed, not open).
- `ServeHTTP` delegates entirely to `middleware.APIKeyMiddleware` invoked
  on the fly with the provisioned validator, the event logger, the audit
  logger, and the drop counter (all from RuntimeServices).
- `init()` registers the module with Caddy.
- Priority 35 — sits between rate-limit (20) and Kratos/JWT handlers (40+).

#### `RuntimeServices` addition

Add one field to `internal/adapters/caddy/runtime_services.go`:

```go
// APIKeyValidator validates API key header values. Required when
// auth.mode is "api-key" — APIKeyHandler fails closed (rejects all
// requests) when this is nil and the mode is active.
APIKeyValidator ports.APIKeyValidator
```

No other RuntimeServices fields change.

#### `internal/plugins/auth/plugin.go` change

`ContributeCaddyHandlers` currently returns `nil` for `ModeAPIKey`.
Change it to return an `APIKeyHandler` Caddy handler JSON blob, constructed
from `p.cfg.APIKey` (header, scope rules). The handler JSON is serialised
the same way `JWTBearerHandler` is today.

#### Composition root (`cmd/vibewarden/main.go` or the wiring layer it delegates to)

After loading config and before calling `caddy.SetRuntimeServices`:

```
if cfg.Auth.Mode == AuthModeAPIKey {
    switch {
    case cfg.Auth.APIKey.OpenBaoPath != "":
        svc.APIKeyValidator = apikey.NewOpenBaoValidator(...)
    default:
        svc.APIKeyValidator = apikey.NewConfigValidator(cfg.Auth.APIKey.Keys)
    }
}
```

The `apikey` import is already in `go.mod`; no new module required.

#### CHANGELOG

Add a breaking-change-fix note under the next release heading:
`auth.mode: api-key` was documented but not enforced since introduction.
This release closes the gap. No config changes required for existing users
with `mode: api-key`.

### What to delete (logprint)

- Delete `internal/adapters/logprint/printer.go` and
  `internal/adapters/logprint/printer_test.go` (entire directory).
- Run `go mod tidy` — `github.com/fatih/color` should be removed if
  nothing else imports it. Verify with `grep -r fatih/color --include="*.go"`.
- If any future CLI command needs colour output, add `fatih/color` back
  with a comment on why it is used.

### Invariant test (same PR)

Per the issue's suggested fix, add one case to
`test/architecture/ports_purity_test.go` (or a new file
`test/architecture/auth_modes_test.go`):

`TestAuthModes_AllModesHaveHandlerOrAreNone` — parse
`internal/plugins/auth/config.go`, collect all `Mode` constants, then call
`plugin.ContributeCaddyHandlers()` for each mode and fail if the result is
`nil` for any mode other than `ModeNone`. This pins the invariant so a
future mode added to `config.go` without a handler fails CI immediately.

Because this test instantiates a real `auth.Plugin`, it lives in
`test/architecture/` (cross-cutting concern) not in `internal/plugins/auth`
(which would be a unit test).

## File layout

New file:
- `internal/adapters/caddy/api_key_handler.go`

Modified files:
- `internal/adapters/caddy/runtime_services.go` — add `APIKeyValidator` field
- `internal/plugins/auth/plugin.go` — `ContributeCaddyHandlers` returns handler for `ModeAPIKey`
- `cmd/vibewarden/main.go` (or its composition sub-file) — construct validator, inject into RuntimeServices
- `CHANGELOG.md` — fix note

Deleted files:
- `internal/adapters/logprint/printer.go`
- `internal/adapters/logprint/printer_test.go`

New test file:
- `test/architecture/auth_modes_test.go`

## Sequence (api-key request flow after wire-up)

1. Config loads `auth.mode: api-key`.
2. Composition root constructs `apikey.NewConfigValidator(cfg.Auth.APIKey.Keys)` (or OpenBao variant).
3. Composition root stores validator in `RuntimeServices.APIKeyValidator`.
4. `SetRuntimeServices(svc)` publishes to the atomic registry.
5. `auth.Plugin.ContributeCaddyHandlers()` returns `APIKeyHandler` JSON.
6. Caddy loads the handler chain; `APIKeyHandler.Provision` retrieves validator from registry.
7. Incoming request hits `APIKeyHandler.ServeHTTP`.
8. Handler delegates to `middleware.APIKeyMiddleware` (validator, config, event/audit loggers, drop counter).
9. Middleware enforces key presence, validity, and scope rules.
10. On success: downstream handler chain continues. On failure: 401/403 with structured error + audit event.

## Error cases

| Situation | Behaviour |
|---|---|
| `APIKeyValidator` is nil at Provision time | `APIKeyHandler.ServeHTTP` returns HTTP 500 with body `{"error":"auth misconfigured"}` and logs an Error. Fails closed. |
| Header absent | HTTP 401, `auth.api_key.failed` event emitted |
| Invalid / inactive key | HTTP 401, `auth.api_key.failed` event emitted |
| Scope rule not satisfied | HTTP 403, `auth.api_key.forbidden` event emitted |
| OpenBao unreachable | validator returns error → HTTP 401 (same path as invalid key) |

## Test strategy

- **Unit test** `api_key_handler_test.go` (next to the handler, package `caddy_test`):
  - `Provision` with nil validator → error.
  - `ServeHTTP` with nil validator → 500 (fail-closed).
  - `ServeHTTP` with valid validator stub → 200.
  - `ServeHTTP` with invalid key stub → 401.
  - `ServeHTTP` with forbidden scope stub → 403.
- **Architecture invariant** `test/architecture/auth_modes_test.go`:
  - Every non-None mode returns a non-nil `ContributeCaddyHandlers` result.
- Existing `internal/middleware/api_key_test.go` tests remain unchanged — they cover the middleware logic; the handler test covers the Caddy module wiring.

## New dependencies

None. All required packages (`internal/middleware`, `internal/adapters/apikey`,
`internal/ports`) are already in the module. `github.com/fatih/color` is
removed (no replacement needed — logprint was unused).

## Consequences

- Users who configured `auth.mode: api-key` and expected it to work will now
  have enforcement. This is the intended state; it is not a breaking change
  for correctly-written apps.
- Users who configured `auth.mode: api-key` and relied on the fact it was
  silently a no-op (i.e., used it as an alias for `mode: none`) will see
  their requests start being rejected. This is correct security behaviour.
  The CHANGELOG note is the mitigation.
- `fatih/color` leaves `go.mod`; reduces binary size slightly.
- The architecture invariant test locks `ContributeCaddyHandlers` for all
  future modes — any mode added without a handler fails CI.
