# Secret Management

VibeWarden provides built-in encrypted secret storage by default. No external containers needed -- secrets are stored locally in an AES-256-GCM encrypted file. For advanced use cases (dynamic credentials, lease rotation), you can opt into [OpenBao](https://openbao.org/).

**What this gives you:**
- Store API keys, database passwords, and other secrets encrypted at rest instead of `.env` files.
- Inject secrets as HTTP request headers or as a `.env` file the app reads at startup.
- Short-lived, auto-rotating Postgres credentials via OpenBao's database engine (opt-in).
- Periodic health checks: detects weak, short, and stale secrets.

**What this does not do:**
- VibeWarden does not replace a full secrets management policy -- it is a sidecar integration layer.
- The built-in store does not support dynamic credentials (use OpenBao for that).

---

## Choosing a Backend

| | Built-in (default) | OpenBao (opt-in) |
|--|--|--|
| External dependencies | None | OpenBao container |
| Encryption | AES-256-GCM | OpenBao seal/unseal |
| Static secrets | Yes | Yes |
| Dynamic Postgres credentials | No | Yes |
| Lease rotation | No | Yes |
| Secret metadata/staleness checks | No | Yes |
| Config key | `secrets.store: builtin` | `secrets.store: openbao` |

### Migration note for existing OpenBao users

If you already deployed with `secrets.store: openbao` (or the deprecated `secrets.provider: openbao`), nothing changes -- your configuration continues to work as before. Migrating secrets from OpenBao to the built-in store is a manual process: re-set each secret using `vibew secret set`. There is no automated migration tool. You can run both backends in different environments if needed (e.g. builtin for dev, OpenBao for production).

---

## Architecture

```
vibewarden.yaml
       |
       v
  VibeWarden (sidecar)
       |
       |--- Built-in store (AES-256-GCM encrypted file)
       |        .vibewarden/secrets.enc
       |
       |--- OR: OpenBao (KV v2 + database engine)
       |        |
       |        v
       |     Postgres (dynamic credentials, OpenBao only)
       |
       v
  upstream app (receives secrets via headers or .env file)
```

---

## Quick Start -- Built-in (default)

The built-in store requires a 32-byte master key. Provide it via environment variable or key file.

### 1. Generate and set the master key

```bash
export VIBEWARDEN_SECRETS_MASTER_KEY=$(openssl rand -hex 32)
```

Save this key somewhere safe -- you need it every time VibeWarden starts. Alternatively, write the hex key to a file and reference it in config:

```yaml
secrets:
  enabled: true
  store: builtin
  builtin:
    key_file: /path/to/master.key
```

### 2. Store a secret

```bash
vibew secret set app/stripe api_key=sk_live_abc123
```

Store multiple keys at once:

```bash
vibew secret set app/database password=s3cr3t! host=db.example.com
```

### 3. Configure injection

```yaml
# vibewarden.yaml
secrets:
  enabled: true
  store: builtin
  inject:
    headers:
      - secret_path: app/stripe
        secret_key: api_key
        header: X-Stripe-Key
    env_file: /run/secrets/.env.secrets
    env:
      - secret_path: app/database
        secret_key: password
        env_var: DATABASE_PASSWORD
```

### 4. Start VibeWarden

```bash
vibew dev
```

VibeWarden decrypts the secrets file, writes `/run/secrets/.env.secrets`, and injects the header on every proxied request.

### Worked example: DB_PASSWORD

```bash
export VIBEWARDEN_SECRETS_MASTER_KEY=$(openssl rand -hex 32)

vibew secret set app/database password=my-secure-db-password

cat <<'EOF' >> vibewarden.yaml
secrets:
  enabled: true
  store: builtin
  inject:
    env_file: .env.secrets
    env:
      - secret_path: app/database
        secret_key: password
        env_var: DB_PASSWORD
EOF

vibew dev
```

Your app reads `DB_PASSWORD` from `.env.secrets` at startup.

---

## Quick Start -- OpenBao (opt-in)

Use OpenBao when you need dynamic credentials, lease rotation, or metadata-based staleness checks.

The OpenBao path looks like this:

```
vibewarden.yaml
       |
       v
  VibeWarden (sidecar)
       |
       | HTTP API (no SDK)
       v
    OpenBao (KV v2 + database engine)
       |
       v
  Postgres (dynamic credentials)
       |
       v
  upstream app (receives secrets via headers or .env file)
```

### 1. Start the stack

```bash
docker compose up -d
```

OpenBao starts in dev mode (auto-unsealed, in-memory). The `openbao-bootstrap` container enables the KV v2 engine, creates a policy, and creates an AppRole for VibeWarden.

```bash
docker compose logs openbao-bootstrap
```

Copy the printed `role_id` and `secret_id`.

### 2. Store a secret

Use the OpenBao CLI (`bao`) or `curl` to write secrets directly to OpenBao:

```bash
# Store your Stripe API key
bao kv put secret/app/stripe api_key=sk_live_abc123

# Store your internal API token
bao kv put secret/app/internal token=bearer-xyz
```

### 3. Configure injection

```yaml
# vibewarden.yaml
secrets:
  enabled: true
  store: openbao
  openbao:
    address: http://openbao:8200
    auth:
      method: approle
      role_id: ${OPENBAO_ROLE_ID}
      secret_id: ${OPENBAO_SECRET_ID}
  inject:
    # Inject as HTTP request headers (received by the upstream on every request)
    headers:
      - secret_path: app/internal
        secret_key: token
        header: X-Internal-Token
    # Write a .env file the upstream reads at startup
    env_file: /run/secrets/.env.secrets
    env:
      - secret_path: app/stripe
        secret_key: api_key
        env_var: STRIPE_API_KEY
```

### 4. Restart VibeWarden

```bash
docker compose restart vibewarden
```

VibeWarden fetches the secrets, writes `/run/secrets/.env.secrets`, and injects the header on every proxied request.

---

## Static Secrets

Static secrets are key/value pairs stored in the configured backend and refreshed on a configurable interval.

### Storing secrets

**Built-in store:**

```bash
# Single key
vibew secret set app/database password=s3cr3t!

# Multiple keys at once
vibew secret set app/stripe \
  api_key=sk_live_abc \
  webhook_secret=whsec_xyz
```

**OpenBao:**

```bash
# Single key
bao kv put secret/app/database password=s3cr3t!

# Multiple keys at once
bao kv put secret/app/stripe \
  api_key=sk_live_abc \
  webhook_secret=whsec_xyz
```

### Listing secrets

```bash
vibew secret list           # list all managed paths
```

### Viewing secrets

```bash
vibew secret get app/stripe              # human-readable output
vibew secret get app/stripe --json       # JSON output
vibew secret get app/stripe --env        # export KEY=value lines
```

### Injection modes

**Header injection** -- VibeWarden adds a header to every proxied request. The upstream app reads it like any other HTTP header. Best for API tokens that the app needs per-request.

```yaml
inject:
  headers:
    - secret_path: app/internal
      secret_key: token
      header: X-Internal-Token
```

**Env file injection** -- VibeWarden writes a `.env` file. The upstream reads it at startup. Best for connection strings and other startup-time config.

```yaml
inject:
  env_file: /run/secrets/.env.secrets
  env:
    - secret_path: app/database
      secret_key: password
      env_var: DATABASE_PASSWORD
    - secret_path: app/stripe
      secret_key: api_key
      env_var: STRIPE_API_KEY
```

The upstream app reads the file:

```bash
# Node.js (dotenv)
require('dotenv').config({ path: '/run/secrets/.env.secrets' })

# Python (python-dotenv)
from dotenv import load_dotenv
load_dotenv('/run/secrets/.env.secrets')

# Shell
source /run/secrets/.env.secrets
```

### Cache TTL

Secrets are cached in memory. The default TTL is 5 minutes -- VibeWarden re-fetches in the background and serves the stale value if the refresh fails.

```yaml
secrets:
  cache_ttl: "10m"   # increase to reduce store load
```

---

## Dynamic Postgres Credentials

**OpenBao only.** OpenBao's database engine generates short-lived Postgres credentials with a configurable TTL. When credentials are within 25% of their TTL, VibeWarden automatically renews (or regenerates) them.

The built-in store does not support dynamic credentials. Use OpenBao if you need this feature.

### Setup

**1. Configure OpenBao's database engine** (done by the bootstrap script in dev mode):

The bootstrap script sets up the database engine mount. For production, add the Postgres connection:

```bash
# Connect OpenBao to Postgres
curl -X POST http://openbao:8200/v1/database/config/postgres \
  -H "X-Vault-Token: $BAO_TOKEN" \
  -d '{
    "plugin_name": "postgresql-database-plugin",
    "connection_url": "postgresql://{{username}}:{{password}}@postgres:5432/app_db",
    "allowed_roles": ["app-readwrite"],
    "username": "postgres_admin",
    "password": "admin_password"
  }'

# Create a role that generates credentials
curl -X POST http://openbao:8200/v1/database/roles/app-readwrite \
  -H "X-Vault-Token: $BAO_TOKEN" \
  -d '{
    "db_name": "postgres",
    "creation_statements": [
      "CREATE ROLE \"{{name}}\" WITH LOGIN PASSWORD '\''{{password}}'\'' VALID UNTIL '\''{{expiration}}'\'';",
      "GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO \"{{name}}\";"
    ],
    "default_ttl": "1h",
    "max_ttl": "24h"
  }'
```

**2. Enable in VibeWarden config:**

```yaml
secrets:
  enabled: true
  store: openbao
  openbao:
    address: http://openbao:8200
    auth:
      method: approle
      role_id: ${OPENBAO_ROLE_ID}
      secret_id: ${OPENBAO_SECRET_ID}
  dynamic:
    postgres:
      enabled: true
      roles:
        - name: app-readwrite
          env_var_user: DATABASE_USER
          env_var_password: DATABASE_PASSWORD
  inject:
    env_file: /run/secrets/.env.secrets
```

VibeWarden requests credentials at startup, writes them to the env file, and rotates them automatically before expiry.

### What the upstream app must handle

Dynamic credentials change on rotation. Your app must be able to re-establish database connections with the new credentials. Most connection pool libraries support this:

- **Node.js (pg):** Create a new `Pool` when the env file changes.
- **Python (psycopg2/asyncpg):** Re-read the env vars and reconnect.
- **Go (pgx):** `pgxpool` can be configured with a `BeforeAcquire` hook that re-reads credentials.

For apps that cannot handle rotation, set a very long `max_ttl` in the OpenBao role:

```json
"max_ttl": "8760h"
```

**This is a security trade-off** -- a one-year TTL is significantly better than a static password, but you lose the rotation benefit. VibeWarden logs a health warning when TTL > 24 hours.

### Rotation events

Every rotation emits a structured domain event:

```json
{
  "schema_version": "v1",
  "event_type": "secret.rotated",
  "ai_summary": "dynamic credential for role \"app-readwrite\" rotated (new user: v-app-Abc123)",
  "payload": {
    "role": "app-readwrite",
    "new_username": "v-app-Abc123"
  }
}
```

Rotation failures emit `secret.rotation_failed`.

---

## Secret Health Checks

VibeWarden periodically checks secret hygiene and emits structured `secret.health_check` events.

### Checks performed

| Check | Severity | Condition |
|-------|----------|-----------|
| Weak secret | critical | Value matches a known default (`password`, `changeme`, `secret`, `123456`, `admin`, `letmein`) |
| Short secret | warning | Value is shorter than 16 characters |
| Stale secret | warning | Not updated in longer than `max_static_age` (default: 90 days) -- OpenBao only |
| Expiring lease | warning | Dynamic credential TTL is less than 10% remaining -- OpenBao only |
| Missing creds | critical | No dynamic credentials available for a configured role -- OpenBao only |

### Configuration

```yaml
secrets:
  health:
    check_interval: "6h"       # how often to run checks (default: 6h)
    max_static_age: "2160h"    # 90 days (default)
    weak_patterns:             # additional patterns to flag as weak
      - "password"
      - "changeme"
      - "letmein"
      - "mycompanyname"
```

### Viewing findings

```bash
vibew doctor      # includes secrets health in the output
```

Health events are also delivered to configured webhooks (Slack, Discord, etc.) when severity is `critical`.

### Event format

```json
{
  "schema_version": "v1",
  "event_type": "secret.health_check",
  "ai_summary": "secret health check: 2 finding(s)",
  "payload": {
    "finding_count": 2,
    "findings": [
      {
        "path": "app/creds",
        "check": "weak",
        "severity": "critical",
        "message": "secret at \"app/creds\" (key: \"api_key\") matches a known weak pattern"
      },
      {
        "path": "app/database",
        "check": "stale",
        "severity": "warning",
        "message": "secret at \"app/database\" (key: \"password\") has not been updated in 91 days"
      }
    ]
  }
}
```

---

## CLI Commands

```bash
# Write a secret to the secret store
vibew secret set <path> <key=value>...
# Examples:
#   vibew secret set app/stripe api_key=sk_live_abc123
#   vibew secret set app/db password=s3cret host=db.example.com

# Read a secret from the configured store (human-readable, JSON, or shell-sourceable env output)
vibew secret get <alias-or-path>
vibew secret get <alias-or-path> --json
vibew secret get <alias-or-path> --env

# List all managed secret paths
vibew secret list

# Generate a cryptographically secure random secret
vibew secret generate
vibew secret generate --length 64
```

---

## Production Considerations

### Built-in store

**Master key backup.** The `VIBEWARDEN_SECRETS_MASTER_KEY` (or the contents of `secrets.builtin.key_file`) is the only way to decrypt your secrets. If you lose it, your secrets are unrecoverable. Store it in a password manager or a separate key management system.

**File permissions.** The encrypted secrets file at `.vibewarden/secrets.enc` is written with `0600` permissions. Ensure the directory `.vibewarden/` is not world-readable on your server. VibeWarden creates the directory with `0700` permissions.

**No dynamic credentials.** The built-in store provides only static secrets. If you need short-lived, auto-rotating database credentials, use OpenBao.

### OpenBao seal/unseal

In production, OpenBao starts **sealed**. It cannot serve requests until unsealed with Shamir keys (or an auto-unseal provider like AWS KMS or cloud HSM).

**First-time initialisation:**

```bash
# Initialize OpenBao (generates unseal keys and root token)
bao operator init -key-shares=5 -key-threshold=3

# Unseal with 3 of the 5 shares
bao operator unseal <key-1>
bao operator unseal <key-2>
bao operator unseal <key-3>
```

Store each unseal key with a different team member. Never store them together or in plaintext.

**Auto-unseal** is strongly recommended for production:

```hcl
# openbao.hcl
seal "awskms" {
  region     = "eu-west-1"
  kms_key_id = "your-kms-key-id"
}
```

### Root token revocation

The root token generated during `bao operator init` is extremely powerful. After bootstrapping:

1. Create a long-lived service token with only the `vibewarden` policy.
2. Revoke the root token: `bao token revoke <root-token>`.
3. Root tokens can be regenerated using the unseal keys when needed.

### AppRole secret_id rotation

The AppRole `secret_id` is a credential -- rotate it periodically:

```bash
# Generate a new secret_id
bao write -f auth/approle/role/vibewarden/secret-id

# Update OPENBAO_SECRET_ID in your environment and restart VibeWarden
# Revoke the old secret_id (optional -- it expires automatically after secret_id_ttl)
bao write auth/approle/role/vibewarden/secret-id/destroy secret_id=<old-id>
```

### Backup and restore

OpenBao's Postgres storage backend (`storage "postgresql"`) is backed by a single table. Back it up with your normal Postgres backup process (`pg_dump`).

For Raft storage (the other common backend): `bao operator raft snapshot save backup.snap`.

### High availability

OpenBao supports Raft HA out of the box. For a single VibeWarden sidecar, a single-node OpenBao is sufficient. For multi-instance setups, configure Raft clustering or use a managed PostgreSQL-backed OpenBao.

---

## Troubleshooting

### Missing master key (built-in store)

**Symptom:** `secrets plugin: builtin store: master key not configured`

**Fix:** Set the `VIBEWARDEN_SECRETS_MASTER_KEY` environment variable or configure `secrets.builtin.key_file` in `vibewarden.yaml`:

```bash
export VIBEWARDEN_SECRETS_MASTER_KEY=$(openssl rand -hex 32)
```

### OpenBao sealed

**Symptom:** `secrets plugin: openbao unhealthy: unhealthy (status 503)`

**Fix:** Unseal OpenBao with `bao operator unseal <key>` (3 of 5 keys by default).

### Connection refused

**Symptom:** `http GET /v1/sys/health: dial tcp: connection refused`

**Fix:** Verify OpenBao is running and `secrets.openbao.address` is correct. Inside Docker, use the container name: `http://openbao:8200`.

### Permission denied

**Symptom:** `openbao: get "app/stripe" returned 403`

**Fix:** The VibeWarden policy does not grant access to that path. Update the policy:

```bash
bao policy write vibewarden - <<EOF
path "secret/data/*" { capabilities = ["create", "read", "update", "delete", "list"] }
path "secret/metadata/*" { capabilities = ["read", "list", "delete"] }
path "database/creds/*" { capabilities = ["read"] }
path "sys/leases/renew" { capabilities = ["update"] }
path "sys/leases/revoke" { capabilities = ["update"] }
EOF
```

### Lease expired

**Symptom:** Dynamic credentials stop working and `secret.rotation_failed` events appear.

**Fix:** Check that VibeWarden can reach OpenBao and that the database engine is configured correctly. Force a refresh by restarting VibeWarden.

### Secret not found

**Symptom:** `builtin: secret not found at "app/stripe"` or `openbao: secret not found at "app/stripe"`

**Fix:** The secret does not exist in the store yet. Write it:

```bash
# Built-in store
vibew secret set app/stripe api_key=your-api-key

# OpenBao
bao kv put secret/app/stripe api_key=your-api-key
```

---

## Full Example Configuration

### Built-in store (default)

```yaml
# vibewarden.yaml -- built-in encrypted secret store

secrets:
  enabled: true
  store: builtin

  # Master key: set VIBEWARDEN_SECRETS_MASTER_KEY env var,
  # or provide a key file:
  # builtin:
  #   key_file: /path/to/master.key

  inject:
    headers:
      - secret_path: app/internal-api
        secret_key: token
        header: X-Internal-Token

    env_file: /run/secrets/.env.secrets
    env:
      - secret_path: app/database
        secret_key: password
        env_var: DATABASE_PASSWORD
      - secret_path: app/stripe
        secret_key: api_key
        env_var: STRIPE_API_KEY

  cache_ttl: "5m"

  health:
    check_interval: "6h"
    max_static_age: "2160h"   # 90 days
    weak_patterns:
      - "password"
      - "changeme"
      - "secret"
      - "123456"
      - "admin"
      - "letmein"
```

### OpenBao (opt-in)

```yaml
# vibewarden.yaml -- OpenBao secret store with dynamic credentials

secrets:
  enabled: true
  store: openbao

  openbao:
    address: http://openbao:8200
    auth:
      # Use AppRole in production; token is fine for development.
      method: approle
      role_id: ${OPENBAO_ROLE_ID}
      secret_id: ${OPENBAO_SECRET_ID}
    mount_path: secret    # default KV v2 mount path

  inject:
    headers:
      - secret_path: app/internal-api
        secret_key: token
        header: X-Internal-Token

    env_file: /run/secrets/.env.secrets
    env:
      - secret_path: app/database
        secret_key: password
        env_var: DATABASE_PASSWORD
      - secret_path: app/stripe
        secret_key: api_key
        env_var: STRIPE_API_KEY

  dynamic:
    postgres:
      enabled: true
      roles:
        - name: app-readwrite
          env_var_user: DATABASE_USER
          env_var_password: DATABASE_PASSWORD

  cache_ttl: "5m"

  health:
    check_interval: "6h"
    max_static_age: "2160h"   # 90 days
    weak_patterns:
      - "password"
      - "changeme"
      - "secret"
      - "123456"
      - "admin"
      - "letmein"
```
