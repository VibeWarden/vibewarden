# Social Login (OIDC / OAuth 2.0)

VibeWarden delegates social login entirely to [Ory Kratos](https://www.ory.sh/kratos/).
Kratos supports every major OAuth 2.0 / OIDC provider through its `oidc` self-service
method. This document shows you how to configure each supported provider.

## How the redirect URL is constructed

Every OAuth provider requires you to register a callback (redirect) URL.
The URL always follows this pattern:

```
https://<your-domain>/self-service/methods/oidc/callback/<provider-id>
```

`<provider-id>` is the `id` field you set in the Kratos `oidc` method config.
VibeWarden proxies the `/self-service/` path prefix directly to Kratos, so the
callback URL above resolves correctly through VibeWarden.

**Examples** (replace `your-domain` with your actual domain):

| Provider | Callback URL |
|----------|-------------|
| Google | `https://your-domain/self-service/methods/oidc/callback/google` |
| GitHub | `https://your-domain/self-service/methods/oidc/callback/github` |
| Apple | `https://your-domain/self-service/methods/oidc/callback/apple` |
| Facebook | `https://your-domain/self-service/methods/oidc/callback/facebook` |
| Microsoft | `https://your-domain/self-service/methods/oidc/callback/microsoft` |
| Generic OIDC | `https://your-domain/self-service/methods/oidc/callback/<your-id>` |

---

## Where the config lives

Social providers are configured in your **Kratos config file** (`kratos.yml`),
not in `vibewarden.yaml`. VibeWarden reads no provider credentials directly —
it simply proxies traffic to Kratos.

The general structure in `kratos.yml`:

```yaml
selfservice:
  methods:
    oidc:
      enabled: true
      config:
        providers:
          - id: google          # must match the callback URL segment
            provider: google
            client_id: ${GOOGLE_CLIENT_ID}
            client_secret: ${GOOGLE_CLIENT_SECRET}
            scope:
              - email
              - profile
            mapper_url: "base64://<base64-encoded-jsonnet>"
```

Kratos expands `${ENV_VAR}` syntax at startup, so credentials are never stored
in the config file.

---

## Google

### 1. Create OAuth 2.0 credentials

1. Open [Google Cloud Console](https://console.cloud.google.com/) and select your project
   (or create one).
2. Go to **APIs & Services > Credentials**.
3. Click **Create Credentials > OAuth client ID**.
4. Choose **Web application** as the application type.
5. Under **Authorized redirect URIs** add:
   ```
   https://your-domain/self-service/methods/oidc/callback/google
   ```
6. Click **Create**. Copy the **Client ID** and **Client secret**.

### 2. Environment variables

```bash
GOOGLE_CLIENT_ID=<client-id-from-console>
GOOGLE_CLIENT_SECRET=<client-secret-from-console>
```

### 3. Kratos config snippet

```yaml
selfservice:
  methods:
    oidc:
      enabled: true
      config:
        providers:
          - id: google
            provider: google
            client_id: ${GOOGLE_CLIENT_ID}
            client_secret: ${GOOGLE_CLIENT_SECRET}
            scope:
              - email
              - profile
            mapper_url: "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dEZpbGUoInBheWxvYWQuanNvbiIpOwp7CiAgaWRlbnRpdHk6IHsKICAgIHRyYWl0czogewogICAgICBlbWFpbDogY2xhaW1zLmVtYWlsLAogICAgICBuYW1lOiBjbGFpbXMubmFtZSwKICAgIH0sCiAgfSwKfQ=="
```

The `mapper_url` value above is a base64-encoded [Jsonnet](https://jsonnet.org/) snippet
that maps the provider's claims to Kratos identity traits. Decode it to inspect or
customise the mapping:

```bash
echo "<base64-value>" | base64 -d
```

A minimal mapper that copies `email` and `name`:

```jsonnet
local claims = std.extVar("claims");
{
  identity: {
    traits: {
      email: claims.email,
      name: claims.name,
    },
  },
}
```

---

## GitHub

### 1. Create an OAuth App

1. Go to **GitHub Settings > Developer settings > OAuth Apps**.
2. Click **New OAuth App**.
3. Fill in the form:
   - **Application name**: any name (e.g. "MyApp via VibeWarden")
   - **Homepage URL**: your app's public URL
   - **Authorization callback URL**:
     ```
     https://your-domain/self-service/methods/oidc/callback/github
     ```
4. Click **Register application**.
5. On the next page, copy the **Client ID** and generate a **Client secret**.

### 2. Environment variables

```bash
GITHUB_CLIENT_ID=<client-id>
GITHUB_CLIENT_SECRET=<client-secret>
```

### 3. Kratos config snippet

```yaml
selfservice:
  methods:
    oidc:
      enabled: true
      config:
        providers:
          - id: github
            provider: github
            client_id: ${GITHUB_CLIENT_ID}
            client_secret: ${GITHUB_CLIENT_SECRET}
            scope:
              - user:email
            mapper_url: "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dEZpbGUoInBheWxvYWQuanNvbiIpOwp7CiAgaWRlbnRpdHk6IHsKICAgIHRyYWl0czogewogICAgICBlbWFpbDogY2xhaW1zLmVtYWlsLAogICAgfSwKICB9LAp9"
```

Note: request the `user:email` scope so that GitHub includes the user's email
in the token claims. GitHub does not expose the email by default when it is set
to private.

---

## Apple

Apple Sign In has additional requirements compared to other providers.

### 1. Create a Services ID in Apple Developer

1. Sign in to [Apple Developer](https://developer.apple.com/) and go to
   **Certificates, Identifiers & Profiles > Identifiers**.
2. Click **+** and select **Services IDs**, then click **Continue**.
3. Enter a description and a unique **Identifier** (e.g. `dev.your-company.app.social`).
   This becomes your `client_id`.
4. Click **Continue**, then **Register**.
5. Back on the Identifiers list, click your new Services ID.
6. Enable **Sign In with Apple**, then click **Configure**.
7. Add your domain under **Domains and Subdomains** and add the callback URL
   under **Return URLs**:
   ```
   https://your-domain/self-service/methods/oidc/callback/apple
   ```
8. Click **Save**, then **Continue**, then **Register**.

### 2. Create a private key

1. Go to **Keys** in the Apple Developer portal.
2. Click **+**, give the key a name, and enable **Sign In with Apple**.
3. Click **Configure** and select your primary App ID.
4. Click **Save**, then **Continue**, then **Register**.
5. Download the `.p8` key file. **This is the only time you can download it.**
6. Note the **Key ID** shown on the confirmation page.

### 3. Gather required values

| Value | Where to find it |
|-------|-----------------|
| `client_id` | The Services ID Identifier you created (e.g. `dev.your-company.app.social`) |
| `team_id` | Your 10-character Apple Developer Team ID, shown in the top-right of the developer portal |
| `key_id` | The Key ID from the key you downloaded |
| `private_key` | Contents of the `.p8` file (the private key itself, not the file path) |

### 4. Environment variables

```bash
APPLE_CLIENT_ID=dev.your-company.app.social
APPLE_TEAM_ID=XXXXXXXXXX
APPLE_KEY_ID=YYYYYYYYYY
# Inline the .p8 contents, preserving newlines:
APPLE_PRIVATE_KEY="-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQg...
-----END PRIVATE KEY-----"
```

### 5. Kratos config snippet

```yaml
selfservice:
  methods:
    oidc:
      enabled: true
      config:
        providers:
          - id: apple
            provider: apple
            client_id: ${APPLE_CLIENT_ID}
            apple_team_id: ${APPLE_TEAM_ID}
            apple_private_key_id: ${APPLE_KEY_ID}
            apple_private_key: ${APPLE_PRIVATE_KEY}
            scope:
              - name
              - email
            mapper_url: "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dEZpbGUoInBheWxvYWQuanNvbiIpOwp7CiAgaWRlbnRpdHk6IHsKICAgIHRyYWl0czogewogICAgICBlbWFpbDogY2xhaW1zLmVtYWlsLAogICAgfSwKICB9LAp9"
```

**Why `apple_team_id` and `apple_private_key_id`?** Apple does not issue a standard
`client_secret`. Instead, Kratos generates a short-lived JWT signed with your private
key. The `team_id` and `key_id` fields are used to construct the JWT header and claims
as specified in Apple's documentation.

---

## Facebook

### 1. Create an App in Meta Developer Portal

1. Go to [Meta for Developers](https://developers.facebook.com/) and click
   **My Apps > Create App**.
2. Choose **Consumer** as the use case and click **Next**.
3. Enter an app name and click **Create App**.
4. In the app dashboard, find **Facebook Login** and click **Set up**.
5. Choose **Web** as the platform.
6. Under **Facebook Login > Settings**, add the callback URL to
   **Valid OAuth Redirect URIs**:
   ```
   https://your-domain/self-service/methods/oidc/callback/facebook
   ```
7. Click **Save Changes**.
8. Go to **Settings > Basic** and copy the **App ID** and **App Secret**.

### 2. Environment variables

```bash
FACEBOOK_CLIENT_ID=<app-id>
FACEBOOK_CLIENT_SECRET=<app-secret>
```

### 3. Kratos config snippet

```yaml
selfservice:
  methods:
    oidc:
      enabled: true
      config:
        providers:
          - id: facebook
            provider: facebook
            client_id: ${FACEBOOK_CLIENT_ID}
            client_secret: ${FACEBOOK_CLIENT_SECRET}
            scope:
              - email
              - public_profile
            mapper_url: "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dEZpbGUoInBheWxvYWQuanNvbiIpOwp7CiAgaWRlbnRpdHk6IHsKICAgIHRyYWl0czogewogICAgICBlbWFpbDogY2xhaW1zLmVtYWlsLAogICAgfSwKICB9LAp9"
```

Note: Facebook requires your app to pass a review before the `email` permission
is available for users outside your developer team. For testing, add test users
in the Meta Developer Portal under **Roles > Test Users**.

---

## Microsoft (Azure AD)

### 1. Register an app in Azure AD

1. Open [Azure Portal](https://portal.azure.com/) and navigate to
   **Azure Active Directory > App registrations**.
2. Click **New registration**.
3. Enter a name for the app.
4. Under **Supported account types**, choose the appropriate option:
   - **Single tenant** — only users in your organisation
   - **Multitenant** — users from any Azure AD organisation
   - **Multitenant + personal Microsoft accounts** — broadest support
5. Under **Redirect URI**, select **Web** and enter:
   ```
   https://your-domain/self-service/methods/oidc/callback/microsoft
   ```
6. Click **Register**.
7. On the overview page, copy the **Application (client) ID** and
   **Directory (tenant) ID**.
8. Go to **Certificates & secrets > Client secrets > New client secret**.
   Set an expiry, click **Add**, and copy the **Value** (you cannot retrieve it later).

### 2. Environment variables

```bash
MICROSOFT_CLIENT_ID=<application-client-id>
MICROSOFT_CLIENT_SECRET=<client-secret-value>
MICROSOFT_TENANT=<directory-tenant-id>
# Use "common" for multitenant + personal accounts,
# or your tenant ID for single-tenant.
```

### 3. Kratos config snippet

```yaml
selfservice:
  methods:
    oidc:
      enabled: true
      config:
        providers:
          - id: microsoft
            provider: microsoft
            client_id: ${MICROSOFT_CLIENT_ID}
            client_secret: ${MICROSOFT_CLIENT_SECRET}
            microsoft_tenant: ${MICROSOFT_TENANT}
            scope:
              - profile
              - email
              - openid
            mapper_url: "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dEZpbGUoInBheWxvYWQuanNvbiIpOwp7CiAgaWRlbnRpdHk6IHsKICAgIHRyYWl0czogewogICAgICBlbWFpbDogY2xhaW1zLmVtYWlsLAogICAgfSwKICB9LAp9"
```

Set `microsoft_tenant` to `common` to allow both organisational and personal
Microsoft accounts, or to your specific tenant ID to restrict login to your
organisation only.

---

## Generic OIDC

Any identity provider that exposes an OIDC discovery document
(i.e. `<issuer>/.well-known/openid-configuration`) can be configured using
Kratos's `generic` provider. This covers Keycloak, Auth0, Okta, Dex, GitLab,
and any other standards-compliant IdP.

### 1. Register a client in your IdP

The exact steps depend on your IdP, but you will always need to:

1. Create an OAuth 2.0 / OIDC client (also called an "application").
2. Set the redirect / callback URI to:
   ```
   https://your-domain/self-service/methods/oidc/callback/<your-provider-id>
   ```
   Replace `<your-provider-id>` with the `id` you will use in the Kratos config.
3. Note the **client ID**, **client secret**, and the **issuer URL**
   (e.g. `https://sso.your-company.com/realms/myrealm` for Keycloak).

### 2. Environment variables

```bash
OIDC_CLIENT_ID=<client-id>
OIDC_CLIENT_SECRET=<client-secret>
OIDC_ISSUER_URL=https://sso.your-company.com/realms/myrealm
```

### 3. Kratos config snippet

```yaml
selfservice:
  methods:
    oidc:
      enabled: true
      config:
        providers:
          - id: my-oidc-provider       # choose any slug; appears in the callback URL
            provider: generic
            client_id: ${OIDC_CLIENT_ID}
            client_secret: ${OIDC_CLIENT_SECRET}
            issuer_url: ${OIDC_ISSUER_URL}
            scope:
              - openid
              - email
              - profile
            mapper_url: "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dEZpbGUoInBheWxvYWQuanNvbiIpOwp7CiAgaWRlbnRpdHk6IHsKICAgIHRyYWl0czogewogICAgICBlbWFpbDogY2xhaW1zLmVtYWlsLAogICAgfSwKICB9LAp9"
```

Kratos fetches `<issuer_url>/.well-known/openid-configuration` automatically to
discover all endpoints, so you do not need to specify the authorization, token,
or userinfo endpoints manually.

#### Keycloak example

For a Keycloak realm named `myrealm` running at `https://sso.example.com`:

```yaml
- id: keycloak
  provider: generic
  client_id: ${KEYCLOAK_CLIENT_ID}
  client_secret: ${KEYCLOAK_CLIENT_SECRET}
  issuer_url: https://sso.example.com/realms/myrealm
  scope:
    - openid
    - email
    - profile
  mapper_url: "base64://..."
```

Callback URL to register in Keycloak:
```
https://your-domain/self-service/methods/oidc/callback/keycloak
```

#### Auth0 example

For an Auth0 tenant `your-tenant.us.auth0.com`:

```yaml
- id: auth0
  provider: generic
  client_id: ${AUTH0_CLIENT_ID}
  client_secret: ${AUTH0_CLIENT_SECRET}
  issuer_url: https://your-tenant.us.auth0.com/
  scope:
    - openid
    - email
    - profile
  mapper_url: "base64://..."
```

Callback URL to register in Auth0:
```
https://your-domain/self-service/methods/oidc/callback/auth0
```

---

## Enabling the OIDC self-service method in vibewarden.yaml

Add a `social_providers` block under the `kratos` section in your
`vibewarden.yaml`. This block tells VibeWarden which providers are active
so it can surface provider names in structured log events.

```yaml
kratos:
  public_url: "http://127.0.0.1:4433"
  admin_url:  "http://127.0.0.1:4434"
  social_providers:
    - google
    - github
    - microsoft
```

The actual OAuth credentials are always configured in `kratos.yml`, not here.

---

## Claim mappers reference

Every provider entry requires a `mapper_url`. The mapper is a
[Jsonnet](https://jsonnet.org/) snippet that transforms the provider's JWT claims
into Kratos identity traits.

You can supply the mapper as:

- **base64 inline** (`base64://<base64-encoded-jsonnet>`) — simplest, no extra files.
- **file path** (`file:///path/to/mapper.jsonnet`) — easier to read and edit.
- **HTTP URL** (`https://...`) — useful for centralised multi-provider setups.

A mapper that works for most providers:

```jsonnet
local claims = std.extVar("claims");
{
  identity: {
    traits: {
      email: claims.email,
      name: claims["name"],
    },
  },
}
```

Encode it for inline use:

```bash
cat mapper.jsonnet | base64 | tr -d '\n'
```

Then prefix the result with `base64://` in the config.

---

## Troubleshooting

**"redirect_uri_mismatch"** — The callback URL registered with the provider does
not exactly match the URL Kratos sent. Check for trailing slashes, `http` vs
`https`, and that the provider `id` in the Kratos config matches the URL segment.

**"invalid_client"** — The client ID or secret is wrong, or the environment
variable was not exported before starting Kratos.

**"access_denied"** — The user denied the consent screen, or the app has not been
approved for the requested scopes by the provider (common with Facebook).

**Kratos returns a 500 during the callback** — Enable debug logging in
`kratos.yml` (`log.level: debug`) and inspect the Kratos container logs.
The most common cause is a misconfigured claim mapper (Jsonnet syntax error or
missing claim field).

**Apple private key format errors** — Ensure the `APPLE_PRIVATE_KEY` environment
variable preserves the literal newlines of the `.p8` file. When using Docker
Compose, pass the variable through a `.env` file rather than inlining it in the
compose file to avoid YAML escaping issues.
