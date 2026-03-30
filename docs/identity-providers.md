# Identity Providers and JWT/OIDC Setup

VibeWarden supports four authentication modes. You pick one per deployment by setting
`auth.mode` in `vibewarden.yaml`. Each mode is described below, followed by
step-by-step configuration guides for common providers.

---

## Auth modes

| Mode | When to use |
|------|-------------|
| `none` | Fully public apps, or when auth is handled by the upstream |
| `jwt` | Any OIDC-compatible provider (Auth0, Keycloak, Firebase, Cognito, Okta, Supabase, …) |
| `kratos` | Self-hosted identity with full UI flows (login, registration, recovery, …) |
| `api-key` | Machine-to-machine or service-account requests |

### `none` — no authentication

All requests are forwarded to the upstream without any identity check. Use this
only for fully public apps or when a downstream component handles authentication.

```yaml
auth:
  mode: none
```

### `jwt` — JWT/OIDC bearer token

VibeWarden validates the `Authorization: Bearer <token>` header on every
protected request. The token must be a signed JWT whose public keys are
discoverable from the configured JWKS endpoint. Verified claims are injected
into the upstream request as HTTP headers.

```yaml
auth:
  mode: jwt
  jwt:
    jwks_url: "https://your-provider/.well-known/jwks.json"
    issuer: "https://your-provider/"
    audience: "your-api-identifier"
    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      email_verified: X-User-Verified
```

This is the **recommended default** for most applications. It works with every
major identity provider without requiring any additional infrastructure.

### `kratos` — Ory Kratos session-cookie

VibeWarden validates Ory Kratos session cookies. Kratos handles all identity
flows: login, registration, MFA, account recovery, and email verification.
All `/self-service/` paths are automatically proxied to Kratos.

```yaml
auth:
  mode: kratos

kratos:
  public_url: "http://127.0.0.1:4433"
  admin_url: "http://127.0.0.1:4434"
```

Use this mode when you need a self-hosted identity layer with full UI flows.
The Kratos plugin starts a Kratos instance for you when `kratos.external` is
`false` (the default in the generated config).

See the [social login guide](social-login.md) for configuring OAuth 2.0
providers inside Kratos.

### `api-key` — API key header

VibeWarden validates an API key passed in a request header (default:
`X-API-Key`). Keys are stored as SHA-256 hashes in the config, optionally in
OpenBao for production.

```yaml
auth:
  mode: api-key
  api_key:
    header: X-API-Key
    keys:
      - name: ci-deploy
        hash: "e3b0c44298fc1c149afb..."   # sha256 of the plaintext key
        scopes: [deploy]
      - name: monitoring
        hash: "a665a45920422f9d417e..."
        scopes: [read]
    scope_rules:
      - path: "/admin/*"
        required_scopes: [deploy]
```

See [API key auth](#api-key-auth-for-machine-to-machine) for full details.

---

## JWT/OIDC configuration reference

### Required fields

| Field | Description |
|-------|-------------|
| `auth.jwt.issuer` | Expected `iss` claim value. Must match exactly what the provider puts in the token. |
| `auth.jwt.audience` | Expected `aud` claim value. Usually your API identifier or client ID. |

### JWKS discovery

Provide **one** of the following:

| Field | Description |
|-------|-------------|
| `auth.jwt.jwks_url` | Direct URL to the JWKS endpoint. Takes precedence over `issuer_url`. |
| `auth.jwt.issuer_url` | OIDC issuer base URL. VibeWarden appends `/.well-known/openid-configuration` and fetches `jwks_uri` from the discovery document. |

When `jwks_url` is set, `issuer_url` is ignored. If neither is set but
`issuer` is set, VibeWarden attempts auto-discovery from `issuer` as the base
URL.

### `claims_to_headers` mapping

Every claim in this map is extracted from the validated JWT and injected as an
HTTP header on the upstream request. The upstream app reads these headers like
any other.

Default mapping (applied when the field is empty):

```yaml
claims_to_headers:
  sub: X-User-Id
  email: X-User-Email
  email_verified: X-User-Verified
```

Extended example:

```yaml
claims_to_headers:
  sub: X-User-Id
  email: X-User-Email
  email_verified: X-User-Verified
  name: X-User-Name
  roles: X-User-Roles        # array claim — joined with comma
  org_id: X-Org-Id
  custom_claim: X-Custom
```

Array-valued claims (e.g. `roles: ["admin", "read"]`) are joined with a comma
before being set as a header value. The upstream receives
`X-User-Roles: admin,read`.

### Algorithm and cache settings

```yaml
auth:
  jwt:
    allowed_algorithms: [RS256, ES256]   # default — do not add HS256 in production
    cache_ttl: 1h                        # how long JWKS keys are cached locally
```

`allowed_algorithms` defaults to `[RS256, ES256]`. Never include `none` or
symmetric algorithms (`HS256`) in production — a compromised secret allows
arbitrary token forgery.

---

## Provider examples

### Auth0

1. In the Auth0 dashboard, create an **API** (Applications → APIs). Note the
   **Identifier** — this is your `audience`.
2. Your Auth0 domain is shown in the dashboard (e.g. `dev-abc123.us.auth0.com`).

```yaml
auth:
  mode: jwt
  jwt:
    jwks_url: "https://dev-abc123.us.auth0.com/.well-known/jwks.json"
    issuer: "https://dev-abc123.us.auth0.com/"
    audience: "https://api.your-app.com"
    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      email_verified: X-User-Verified
      name: X-User-Name
```

The trailing slash on `issuer` is significant — Auth0 includes it in the `iss`
claim.

### Keycloak

1. In Keycloak Admin Console, create a **Realm** and a **Client** with
   `Client authentication` enabled.
2. The issuer follows the pattern `https://<host>/realms/<realm>`.

```yaml
auth:
  mode: jwt
  jwt:
    # Keycloak publishes the JWKS under /protocol/openid-connect/certs
    jwks_url: "https://keycloak.example.com/realms/myrealm/protocol/openid-connect/certs"
    issuer: "https://keycloak.example.com/realms/myrealm"
    audience: "your-client-id"
    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      email_verified: X-User-Verified
      preferred_username: X-Username
      realm_access_roles: X-Roles    # custom Keycloak claim mapper required
```

To expose `realm_access.roles` as a flat claim, add a **Protocol Mapper** of
type `User Realm Role` to your client, set `Token Claim Name` to
`realm_access_roles`, and enable `Add to access token`.

### Firebase Auth

Firebase issues tokens signed with RSA. The public keys rotate every hour.
VibeWarden caches the JWKS according to `cache_ttl` and refreshes
automatically.

1. In the Firebase Console, find your **Project ID** under Project Settings.

```yaml
auth:
  mode: jwt
  jwt:
    jwks_url: "https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com"
    issuer: "https://securetoken.google.com/<your-project-id>"
    audience: "<your-project-id>"
    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      email_verified: X-User-Verified
      name: X-User-Name
      picture: X-User-Picture
    # Firebase rotates keys frequently; keep cache short
    cache_ttl: 30m
```

Replace `<your-project-id>` with the value shown in the Firebase Console
(e.g. `my-app-prod`).

### Amazon Cognito

1. In the AWS Console, find your **User Pool ID** (format: `<region>_<id>`)
   and your **App Client ID**.

```yaml
auth:
  mode: jwt
  jwt:
    jwks_url: "https://cognito-idp.<region>.amazonaws.com/<user-pool-id>/.well-known/jwks.json"
    issuer: "https://cognito-idp.<region>.amazonaws.com/<user-pool-id>"
    audience: "<app-client-id>"
    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      email_verified: X-User-Verified
      cognito:username: X-Username
      cognito:groups: X-User-Groups
```

Replace `<region>`, `<user-pool-id>`, and `<app-client-id>` with your values.
Cognito issues tokens with the `sub` claim set to a UUID.

### Okta

1. In the Okta Admin Console, create an **Authorization Server** (or use the
   default `default` server).
2. The issuer is visible under Security → API → Authorization Servers.

```yaml
auth:
  mode: jwt
  jwt:
    # OIDC auto-discovery — Okta supports RFC 8414
    issuer_url: "https://dev-abc123.okta.com/oauth2/default"
    issuer: "https://dev-abc123.okta.com/oauth2/default"
    audience: "api://default"
    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      name: X-User-Name
      groups: X-User-Groups
```

### Supabase Auth

Supabase uses its own JWT secret (symmetric, HS256 by default in older
versions). **Do not use HS256 in production** — use Supabase's asymmetric
JWKS endpoint instead.

Supabase provides an OIDC discovery endpoint since early 2024. Use it:

```yaml
auth:
  mode: jwt
  jwt:
    jwks_url: "https://<project-ref>.supabase.co/auth/v1/.well-known/jwks.json"
    issuer: "https://<project-ref>.supabase.co/auth/v1"
    audience: "authenticated"
    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      role: X-User-Role
      app_metadata: X-App-Metadata
```

Replace `<project-ref>` with your Supabase project reference (shown in the
project URL, e.g. `abcdefghijklmnop`).

---

## Kratos as optional self-hosted identity

Ory Kratos is the right choice when you need full self-hosted identity flows:
browser-based login/registration UI, MFA, password recovery, email
verification, social login, and WebAuthn.

The Kratos plugin is **opt-in** — you activate it by setting `auth.mode:
kratos`. VibeWarden then starts and manages a Kratos instance for you in the
generated Docker Compose stack.

### When to choose Kratos

- You want a complete login/registration UI without writing any auth code.
- You need password-based accounts, not just token validation.
- You need session management (logout, session revocation).
- You need MFA, email verification, or account recovery flows.
- You want social login (Google, GitHub, Apple, …) through a single
  self-hosted system.

### When to choose `jwt` instead

- You already have an identity provider (Auth0, Cognito, Firebase, …).
- You need to validate tokens issued by a third-party provider.
- You are building an API consumed by mobile clients or single-page apps.
- You want minimal infrastructure — no Postgres, no Kratos container.

### External Kratos for production scaling

For high-traffic production deployments, you can manage Kratos yourself and
point VibeWarden at it:

```yaml
auth:
  mode: kratos

kratos:
  public_url: "https://auth.internal.example.com"
  admin_url: "https://auth-admin.internal.example.com"
  external: true    # prevents VibeWarden from trying to start Kratos
```

When `external: true`, VibeWarden skips generating the Kratos config and
Docker Compose service. You are responsible for running and scaling Kratos.

See [docs/social-login.md](social-login.md) for configuring OAuth 2.0 / OIDC
social login providers inside Kratos.

---

## API key auth for machine-to-machine

Use `api-key` mode for service-to-service calls, CI/CD pipelines, and
monitoring agents.

### Generating and storing keys

1. Generate a secure random key:

   ```bash
   openssl rand -hex 32
   # outputs e.g.: a3f2c1...
   ```

2. Hash it with SHA-256:

   ```bash
   echo -n "a3f2c1..." | sha256sum
   # outputs: e3b0c4... -
   ```

3. Store only the hash in `vibewarden.yaml`; distribute the plaintext key to
   the calling service via a secret manager.

### Static keys in config

```yaml
auth:
  mode: api-key
  api_key:
    header: X-API-Key        # header name the client sends
    keys:
      - name: ci-pipeline
        hash: "e3b0c44298fc1c149afb4c8996fb92427ae41e4649b934ca495991b7852b855"
        scopes: [deploy, read]
      - name: monitoring-agent
        hash: "a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3"
        scopes: [read]
```

### Keys stored in OpenBao

For production, store key hashes in OpenBao instead of the config file:

```yaml
auth:
  mode: api-key
  api_key:
    header: X-API-Key
    openbao_path: "auth/api-keys"   # KV path inside OpenBao
    cache_ttl: 5m                   # how long to cache keys locally
```

The KV secret at `auth/api-keys` must contain string fields where each key is
the key name and each value is the SHA-256 hash:

```
bao kv put secret/auth/api-keys ci-pipeline=e3b0c4...
```

### Scope-based authorization

Scope rules restrict which keys can access which paths:

```yaml
auth:
  api_key:
    scope_rules:
      - path: "/api/v1/*"
        methods: [GET, HEAD]
        required_scopes: [read]
      - path: "/admin/*"
        required_scopes: [admin]
      - path: "/deploy"
        methods: [POST]
        required_scopes: [deploy]
```

Rules are evaluated in order. The first matching rule determines the required
scopes. If no rule matches, the request is allowed (provided the key itself is
valid).

---

## Migration guide: Kratos-only to JWT/OIDC mode

If you started with `auth.mode: kratos` and want to move to JWT/OIDC:

1. **Choose a provider** — pick from the examples above or use any
   OIDC-compliant provider.

2. **Update `vibewarden.yaml`**:

   ```yaml
   # Before
   auth:
     mode: kratos

   kratos:
     public_url: "http://127.0.0.1:4433"
     admin_url: "http://127.0.0.1:4434"

   # After
   auth:
     mode: jwt
     jwt:
       jwks_url: "https://your-provider/.well-known/jwks.json"
       issuer: "https://your-provider/"
       audience: "your-api-identifier"
       claims_to_headers:
         sub: X-User-Id
         email: X-User-Email
         email_verified: X-User-Verified
   ```

3. **Update your upstream** — the upstream no longer receives Kratos session
   headers. If your app reads `X-User-Id`, `X-User-Email`, and
   `X-User-Verified`, the default `claims_to_headers` mapping produces
   identical header names so no upstream changes are needed (assuming your
   JWT provider includes `sub`, `email`, and `email_verified` claims).

4. **Handle existing sessions** — Kratos sessions are cookie-based and will
   stop working after the mode switch. Coordinate with your users or run both
   modes in parallel on separate VibeWarden instances during migration.

5. **Remove Kratos infrastructure** — once traffic has fully switched, remove
   the Kratos and Postgres services from your Docker Compose stack to reduce
   operational overhead.

---

## AI-agent-readable summary

```json
{
  "schema_version": "v1",
  "topic": "identity-providers",
  "modes": ["none", "jwt", "kratos", "api-key"],
  "recommended_mode": "jwt",
  "jwt_required_fields": ["auth.jwt.issuer", "auth.jwt.audience"],
  "jwks_discovery": ["auth.jwt.jwks_url", "auth.jwt.issuer_url"],
  "default_claims_to_headers": {
    "sub": "X-User-Id",
    "email": "X-User-Email",
    "email_verified": "X-User-Verified"
  },
  "provider_examples": ["auth0", "keycloak", "firebase", "cognito", "okta", "supabase"],
  "kratos_use_cases": ["browser_flows", "mfa", "social_login", "self_hosted_accounts"],
  "api_key_use_cases": ["machine_to_machine", "ci_cd", "service_accounts"]
}
```
