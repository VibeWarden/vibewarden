# ADR-104: OpenBao prod init+unseal via seed-secrets init container

**Date**: 2026-05-05
**Issue**: [#1345](https://github.com/vibewarden/vibewarden/issues/1345)
**Status**: Accepted

---

## Context

Fresh production deploys fail permanently because the Docker Compose healthcheck for
the `openbao` service uses `bao status`, which exits with code 2 whenever the vault
is uninitialised or sealed. On every first-run prod deploy:

- OpenBao starts in `server` mode (persistent storage, no `-dev` flag).
- It is uninitialized and sealed by default.
- `bao status` exits 2 → the healthcheck never passes.
- All downstream services (`seed-secrets`, `vibewarden`) have
  `condition: service_healthy` on `openbao` → they never start.
- The stack is permanently broken with no operator intervention path.

The dev profile is unaffected: `server -dev` mode auto-inits, auto-unseals, and
exits `bao status` with code 0. The bug is exclusively in the prod code path.

### Additional failure: seed-secrets.sh in prod context

`seed-secrets.sh.tmpl` (line 21) does:
```sh
until bao status >/dev/null 2>&1; do sleep 1; done
```
This waits for exit 0, which requires the vault to be initialized AND unsealed.
Additionally, `seed-secrets` uses `BAO_TOKEN=${OPENBAO_DEV_ROOT_TOKEN}` — a randomly
generated token written to `.credentials` at bundle time. This token is never
registered with OpenBao during prod bootstrap, so even if the vault were reachable,
authentication would fail with a 403.

### Why OPENBAO_DEV_ROOT_TOKEN exists in prod

`OPENBAO_DEV_ROOT_TOKEN` was introduced for dev mode where OpenBao accepts any
pre-configured token set via `BAO_DEV_ROOT_TOKEN_ID`. In prod mode this variable
is still written to `.credentials` and `.env`, but it has no relationship to the
actual root token produced by `bao operator init`. The vibewarden process also reads
it via `VIBEWARDEN_SECRETS_OPENBAO_AUTH_TOKEN=${OPENBAO_DEV_ROOT_TOKEN}`, which
means vibewarden itself would also fail to authenticate to a prod vault.

### Options considered

**a. HTTP healthcheck with permissive status codes** — change `bao status` to
`wget -q --spider 'http://127.0.0.1:8200/v1/sys/health?uninitcode=200&sealedcode=200'`.
The vault HTTP API returns the right code for each state; by passing `uninitcode=200`
and `sealedcode=200` we accept all states and just verify the process is up.
Healthcheck passes as soon as the HTTP listener is bound. `seed-secrets` can then
run and perform init+unseal.

**b. Init container pattern** — a dedicated `openbao-init` Compose service with
`restart: "no"` and `depends_on: openbao: condition: service_started` that runs
`bao operator init` + `bao operator unseal` before `seed-secrets` starts.

**c. Drop OpenBao as prod default** — make `secrets.provider: env` the default
for prod; mark OpenBao as opt-in advanced.

**d. openbao-init shell script generated alongside seed-secrets.sh** — generate
`openbao-init.sh.tmpl` that performs init+unseal+token-write and is run by the
existing `seed-secrets` Compose service as its first step.

### Chosen approach: (a) + extend seed-secrets.sh to own init+unseal

Option (a) alone fixes the healthcheck liveness gate. The real work is option (d)
folded into `seed-secrets.sh`: when on the prod profile the script must also
initialize OpenBao, unseal it, and capture the real root token for subsequent steps.

The unseal key and root token produced by `bao operator init` must persist across
container restarts. They are stored in a Docker named volume
(`openbao-data:/openbao/file` — the path expected by the OpenBao image's built-in
storage backend) and in the `.credentials` file that is bind-mounted read-write into
`seed-secrets`. The `seed-secrets` service already has `./.credentials:/.credentials`
as a writable bind mount — we use this to persist `OPENBAO_ROOT_TOKEN` and
`OPENBAO_UNSEAL_KEY` back to the host filesystem so subsequent restarts can re-unseal
without re-initializing.

> **Implementation note (fix #1351):** the initial ADR draft used `/openbao/data` as
> the storage path. Smoke test #3 revealed the OpenBao image pre-configures its file
> storage backend at `/openbao/file`. The generated `openbao-config.hcl` and the
> docker-compose volume mount were updated to `/openbao/file` in the same PR.

This approach:
- Requires no new Compose services.
- Requires no new Go code — only template changes.
- Is idempotent: the script checks init status before initializing.
- Keeps the operator flow identical: `vibew bundle` + `docker compose up -d`. No manual
  `bao operator init` step.

**Security note**: storing the unseal key in `.credentials` on the host filesystem has
the same threat model as storing any other secret in that file. For the target user
(vibe coder on a single-node VPS) this is appropriate. Users with stronger requirements
are directed to OpenBao's auto-unseal (KMS) feature, which is outside the scope of
VibeWarden v1.

---

## Decision

### Changes to `internal/config/templates/docker-compose.yml.tmpl`

**Healthcheck** — replace `bao status` with an HTTP check that accepts all vault
states (uninitialised, sealed, and active). The HTTP API is the correct readiness
signal; `bao status` couples readiness to vault state.

Old:
```yaml
healthcheck:
  test: ["CMD-SHELL", "BAO_ADDR=http://127.0.0.1:8200 bao status"]
  interval: 10s
  timeout: 5s
  retries: 5
  start_period: 5s
```

New (both dev and prod share the same definition — the openbao block is not
profile-split for healthcheck):
```yaml
healthcheck:
  test: ["CMD-SHELL", "wget -q --spider 'http://127.0.0.1:8200/v1/sys/health?uninitcode=200&sealedcode=200&standbyok=true' || exit 1"]
  interval: 10s
  timeout: 5s
  retries: 12
  start_period: 10s
```

`uninitcode=200` and `sealedcode=200` tell the sys/health endpoint to return HTTP 200
for all non-error vault states. `standbyok=true` includes HA standby. retries increased
to 12 to allow 2 minutes of startup time on slow hosts.

**No change** to `depends_on` wiring — `seed-secrets` still depends on
`openbao: condition: service_healthy`, which now passes as soon as the HTTP listener
is bound (within seconds of container start).

**`seed-secrets` environment in prod** — the service must know the profile to branch
its behaviour. Expose `VIBEWARDEN_PROFILE=${VIBEWARDEN_PROFILE}` as an environment
variable in the `seed-secrets` Compose service definition.

### Changes to `internal/config/templates/seed-secrets.sh.tmpl`

The script gains a prod-mode init+unseal preamble, conditioned on
`VIBEWARDEN_PROFILE=prod`. The changes are:

1. **Wait loop** — replace `until bao status` with `until wget`, matching the
   healthcheck semantics (HTTP 200 = API is up).

2. **Prod init block** (runs only when `VIBEWARDEN_PROFILE=prod`):
   - Check if vault is already initialized: `bao operator init -status` (exits 0 =
     initialized, 2 = not).
   - If not initialized: run `bao operator init -key-shares=1 -key-threshold=1
     -format=json`, parse the output, write `OPENBAO_ROOT_TOKEN` and
     `OPENBAO_UNSEAL_KEY` back to `$CREDS_FILE`.
   - Unseal: run `bao operator unseal <key>`.
   - Re-source `$CREDS_FILE` so `BAO_TOKEN` reflects the real root token.
   - On subsequent runs (already initialized): read `OPENBAO_ROOT_TOKEN` from
     `$CREDS_FILE`, check seal status, unseal if sealed.

3. **Token** — after the init block, set `BAO_TOKEN` from the `OPENBAO_ROOT_TOKEN`
   credential (which is now the real root token in prod, not the pre-generated dev
   token).

### Changes to `internal/domain/generate/credentials.go`

Rename the field `OpenBaoDevRootToken` to `OpenBaoProdToken` for clarity. This is a
Go-internal rename only. The env variable name in `.credentials` / `.env` changes from
`OPENBAO_DEV_ROOT_TOKEN` to `OPENBAO_ROOT_TOKEN` to remove the misleading `DEV_` prefix
that implies the token is only for dev use.

**Migration**: All references to `OPENBAO_DEV_ROOT_TOKEN` in templates and Go code must
be updated atomically in the same PR. A `grep` check must be added to the test suite
to assert the old name no longer appears in any template.

### Changes to `internal/app/generate/service.go`

The `.credentials` writer at line 170 produces the env file. Update the key name from
`OPENBAO_DEV_ROOT_TOKEN` to `OPENBAO_ROOT_TOKEN`.

### Changes to `internal/config/templates/docker-compose.yml.tmpl` (seed-secrets env)

The `seed-secrets` Compose service needs `VIBEWARDEN_PROFILE` available:
```yaml
  seed-secrets:
    ...
    environment:
      BAO_ADDR: http://openbao:8200
      BAO_TOKEN: ${OPENBAO_ROOT_TOKEN}
      VIBEWARDEN_PROFILE: ${VIBEWARDEN_PROFILE}
```

The `vibewarden` service already has `VIBEWARDEN_SECRETS_OPENBAO_AUTH_TOKEN` wired
to the token. In prod this must point to `${OPENBAO_ROOT_TOKEN}` (the real root token).
The variable name change above means the template line updates automatically.

### Changes to `internal/config/templates/openbao-config.hcl.tmpl`

The HCL config sets `storage "file"` and `listener "tcp"`. The storage `path` must
match the mount point in the Compose volume definition.

**Fix #1351:** the initial draft set `path = "/openbao/data"`. The OpenBao image
pre-configures its storage backend at `/openbao/file`. Both the HCL template and the
docker-compose volume mount were corrected to `/openbao/file`.

### File layout — files that change

```
internal/config/templates/docker-compose.yml.tmpl     # healthcheck, seed-secrets env, volume path
internal/config/templates/openbao-config.hcl.tmpl     # storage path /openbao/data → /openbao/file
internal/config/templates/seed-secrets.sh.tmpl        # init+unseal preamble, wait loop
internal/domain/generate/credentials.go               # rename field
internal/app/generate/service.go                      # rename env key in .env writer
internal/app/generate/service_test.go                 # update fixtures/assertions
internal/app/secret/service_test.go                   # update fixture
```

No new files are needed.

### Sequence (fresh prod deploy)

1. `docker compose up -d` starts all services.
2. `openbao` container starts, binds TCP :8200 within ~2 seconds.
3. Healthcheck fires: `wget .../v1/sys/health?uninitcode=200` → 200 OK → healthy.
4. `seed-secrets` starts (depends_on healthcheck passed).
5. `seed-secrets.sh` detects `VIBEWARDEN_PROFILE=prod`.
6. `bao operator init -status` exits 2 → vault not initialized.
7. `bao operator init -key-shares=1 -key-threshold=1 -format=json` runs, outputs JSON.
8. Script parses unseal key + root token, writes them to `$CREDS_FILE` (bind-mounted
   from host at `.credentials`).
9. `bao operator unseal <key>` runs → vault unsealed.
10. `BAO_TOKEN` is set to the real root token.
11. KV v2 engine is enabled, infra secrets seeded, user-defined secrets seeded.
12. `seed-secrets` container exits 0.
13. `vibewarden` starts (depends_on `seed-secrets: condition: service_completed_successfully`).
14. `vibewarden` reads `OPENBAO_ROOT_TOKEN` from environment → authenticates to vault.

### Sequence (subsequent restart of sealed vault, e.g. host reboot)

1. `openbao` starts, vault data persists in `openbao-data` volume → initialized but sealed.
2. Healthcheck passes immediately (HTTP listener up, `uninitcode=200` / `sealedcode=200`).
3. `seed-secrets` starts.
4. `bao operator init -status` exits 0 → already initialized.
5. Script reads `OPENBAO_UNSEAL_KEY` from `$CREDS_FILE` (written during step 8 above).
6. `bao operator unseal <key>` runs → vault unsealed.
7. Seeds are skipped (or run with `--rotate` flag if operator passes it).
8. `seed-secrets` exits 0.
9. `vibewarden` starts.

### Error cases

| Error | Behaviour |
|-------|-----------|
| `bao operator init` fails | Script exits non-zero; `seed-secrets` container exits with failure; vibewarden never starts; operator sees compose logs with error |
| `OPENBAO_UNSEAL_KEY` missing from `.credentials` on re-run (lost) | Script exits with clear message: "OPENBAO_UNSEAL_KEY not found — vault is initialized but unseal key is missing. Recover manually." |
| Vault already initialized (re-run scenario) | Init is skipped; unseal is attempted with stored key |
| Unseal with wrong key | `bao operator unseal` exits non-zero; script exits; operator sees error in logs |
| `.credentials` bind mount missing | Script already exits at existing "ERROR: $CREDS_FILE not found" guard |

### Test strategy

All changes are in templates and credential wiring — no new Go ports or adapters.

**Unit tests** (table-driven, in existing test files):

- `TestGenerate_ProdProfile_WithSecretsEnabled_Succeeds` — assert generated compose
  contains `uninitcode=200` in the healthcheck, does NOT contain `bao status`.
- `TestGenerate_ProdProfile_WithSecretsEnabled_Succeeds` — assert `seed-secrets` env
  contains `VIBEWARDEN_PROFILE`.
- `TestGenerate_SeedSecrets_DevProfile_NoInitBlock` — assert generated `seed-secrets.sh`
  does NOT contain `bao operator init` for dev profile.
- `TestGenerate_SeedSecrets_ProdProfile_HasInitBlock` — assert generated `seed-secrets.sh`
  contains `bao operator init` for prod profile.
- `TestGenerate_CredentialsEnvFile_ContainsOPENBAO_ROOT_TOKEN` — assert `.env` writer
  uses `OPENBAO_ROOT_TOKEN` not `OPENBAO_DEV_ROOT_TOKEN`.
- `TestGenerate_NoStaleDevTokenName` — grep the rendered templates for
  `OPENBAO_DEV_ROOT_TOKEN` and assert zero hits.

**Integration tests**: not required — template rendering is unit-testable without
containers.

### New dependencies

None. `wget` is available in the `quay.io/openbao/openbao:2.2.0` image (Alpine-based).
`jq` is NOT used; JSON parsing is done with `grep`/`sed` (POSIX sh, no external deps).

---

## Consequences

- Fresh prod deploys succeed without operator interaction.
- Container restarts after host reboots succeed automatically (vault unseals from
  stored key in `.credentials`).
- The unseal key lives in `.credentials` on the host filesystem. This is the same
  threat model as all other secrets in that file. Documented in user-facing docs as
  "treat `.credentials` as a private key — never commit it".
- `OPENBAO_DEV_ROOT_TOKEN` → `OPENBAO_ROOT_TOKEN` rename is a breaking change for
  any user who has pinned the old variable name in their own scripts. Mitigated by:
  shipping a deprecation warning in `vibew bundle` when the old name is detected in
  an existing `.credentials` file; the warning is emitted for one minor release then
  the old name is dropped.
- The generated `seed-secrets.sh` grows by ~30 lines but remains a real artifact
  (validatable, testable) per the artifact policy.
- KMS-based auto-unseal (AWS KMS, GCP KMS, Azure Key Vault) remains out of scope for
  v1. Users who require it are directed to the OpenBao documentation.
