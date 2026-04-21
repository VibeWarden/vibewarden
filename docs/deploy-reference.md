# Deploy Command Reference

`vibew deploy` deploys the VibeWarden stack to a remote server over SSH. It
generates runtime configuration locally, transfers files via `rsync`, and starts
Docker Compose on the remote host.

For a full walkthrough of deploying to a VPS for the first time, see
[Deploy to VPS](deploy-to-vps.md). This page is the command reference.

---

## Architecture

All operations use your system `ssh` and `rsync` binaries. Your SSH agent and
`~/.ssh/config` settings (IdentityFile, ProxyJump, etc.) are honoured
automatically. No SSH libraries are embedded in the binary.

Remote directory layout: `~/vibewarden/<project-name>/`

The project name is derived from the absolute path of the `--config` file. When
`--config` is omitted, the current directory name is used.

---

## Commands

### `vibew deploy`

Deploy (or redeploy) the stack to a remote server.

```bash
vibew deploy --target ssh://user@host --config vibewarden.prod.yaml
```

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--target` | Yes | -- | Remote target in `ssh://user@host[:port]` format |
| `--config` | No | `./vibewarden.yaml` | Path to the vibewarden.yaml config file |
| `--ssh-key` | No | SSH agent / `~/.ssh/config` | Path to the SSH private key file |
| `--secrets-from` | No | -- | Path to a `.env`-format file whose KEY=VALUE pairs are seeded into OpenBao |
| `--rotate-secrets` | No | `false` | Re-seed secrets from `--secrets-from` on subsequent deploys |
| `--unseal-key` | No | stored key | OpenBao unseal key; overrides the key stored in `~/vibewarden/<project>/.openbao-credentials` on the remote |
| `--force` | No | `false` | Overwrite remote files even if they have been modified since last deploy (skips drift detection) |
| `--env` | No | `production` | Deployment environment name; reads `vibewarden.<env>.yaml` as the production override |
| `--dry-run` | No | `false` | Generate the deploy bundle and list its contents without actually deploying (does not require `--target`) |

#### Deploy mode detection

`vibew deploy` automatically detects whether the target has an existing
VibeWarden sidecar and selects the right strategy:

| Existing sidecar? | `tls.domain` set? | Behavior |
|--------------------|-------------------|----------|
| No | Yes | **Bootstrap**: creates sidecar + first site (multi-app) |
| No | No | **Legacy deploy**: single-app mode (backward compatible) |
| Yes | Yes | **Add site**: adds app alongside existing sites |
| Yes | No | **Error**: `cannot add a site without a TLS domain` |

Detection works by checking for `~/vibewarden/.sidecar/global.yaml` on the
remote host via SSH (single round-trip).

#### What happens on first deploy (single-app, no domain)

1. Generates runtime config (Docker Compose files, Kratos config) locally.
2. Transfers files to the remote via `rsync`.
3. Runs `docker compose up -d` on the remote.
4. When `secrets.enabled: true` in vibewarden.yaml, bootstraps OpenBao:
   initialises, unseals, enables KV v2 and AppRole, creates the vibewarden
   policy and role, and seeds secrets from `--secrets-from` if provided.
5. Prints the unseal key, root token, role ID, and secret ID. Save the unseal
   key -- it is not shown again.

#### What happens on first deploy (multi-app, with domain)

1. Creates the directory layout: `~/vibewarden/.sidecar/` and
   `~/vibewarden/sites/<project>/`.
2. Creates the shared `vibewarden-multiapp` Docker network.
3. Writes `global.yaml` and the sidecar `docker-compose.yml`.
4. Copies your `vibewarden.yaml` to `sites/<project>/vibewarden.yaml`.
5. Renders and starts the per-app Docker Compose stack.
6. Starts the sidecar container.
7. Runs a health check via SSH.

#### What happens when adding a site

1. Copies your `vibewarden.yaml` to `sites/<project>/vibewarden.yaml`.
2. Renders and starts the per-app Docker Compose stack.
3. Restarts the sidecar to pick up the new site configuration.
4. Runs a health check via SSH.

#### What happens on subsequent deploys (single-app)

1. Regenerates and transfers updated config.
2. Restarts Docker Compose on the remote.
3. Uses the stored unseal key from `~/vibewarden/<project>/.openbao-credentials`
   unless `--unseal-key` is provided explicitly.
4. Secrets are not re-seeded unless `--rotate-secrets` is passed.

---

### `vibew bundle`

Produce a self-contained Docker Compose deployment bundle under
`--output` without opening an SSH connection. The command writes
every file needed to deploy on a VPS (merged docker-compose.yml with
`image:` pinned, merged vibewarden.yaml, a `.env` preserved across
runs, a reference `deploy.sh`, a README, and optionally an
`image.tar`) and lists them on stdout.

Use this when you want to run the deploy manually (your own `scp` /
`rsync` / CI pipeline) instead of letting `vibew deploy` drive SSH.
The bundle is local-only — no network calls, no changes outside
`--output`.

```bash
vibew bundle
vibew bundle --output build/deploy
vibew bundle --skip-image
vibew bundle --image ghcr.io/acme/myapp:v1.2.3
vibew bundle --overwrite
```

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--output` | No | `.vibewarden/bundle` | Output directory for all bundle files |
| `--overwrite` | No | `false` | Replace an existing `.env` in `--output` (otherwise preserved across runs) |
| `--image` | No | `<project>-app:latest` | Docker image tag to package via `docker save` |
| `--skip-image` | No | `false` | Do not package `image.tar` (use this when pulling from a registry) |

#### Output layout

```
.vibewarden/bundle/
  docker-compose.yml     # image: pinned, never build:
  vibewarden.yaml        # merged base + prod override, strict-validated
  sample.env             # regenerated every run; template for operators
  .env                   # first-run only; --overwrite to replace
  deploy.sh              # mode 0o750, 10-line reference script
  image.tar              # omitted with --skip-image
  README.md              # 3-paragraph manual-deploy guide
  kratos/, .credentials  # whatever the generator produces
```

Five of those files (`sample.env`, `.env`, `deploy.sh`, `README.md`,
`image.tar`) are unique to `vibew bundle`. The rest are the same
artifacts `vibew deploy` produces internally; ADR-085 §4 guarantees
`docker-compose.yml` and `vibewarden.yaml` are byte-identical between
the two commands for the same input.

#### Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `multi-site bundle is not yet supported; use vibew deploy` | Project has `sites/<name>/vibewarden.yaml` | Use `vibew deploy` until multi-site bundling lands (tracked under ADR-085 follow-ups) |
| `Configuration invalid: unknown key ...` | Typo in `vibewarden.yaml` or `vibewarden.production.yaml` | Remove the unknown key; see ADR-082 |
| `creating output directory: ...` | `--output` points at a read-only path | Choose a writable directory |
| `saving image ... : no such image` | Image not yet built | Run `vibew build` (or pass `--skip-image` for registry-pull) |

---

### `vibew deploy status`

Show Docker Compose service status on the remote.

```bash
vibew deploy status --target ssh://user@host
```

In multi-app mode (auto-detected), shows the sidecar status plus all sites.
Use `--app` to target a specific site.

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--target` | Yes | -- | Remote target in `ssh://user@host[:port]` format |
| `--config` | No | `./vibewarden.yaml` | Path to vibewarden.yaml (used to derive the remote project directory) |
| `--ssh-key` | No | SSH agent / `~/.ssh/config` | Path to the SSH private key file |
| `--app` | No | (all sites) | Target a specific site in multi-app mode (e.g. `--app blog`) |

---

### `vibew deploy logs`

Fetch Docker Compose logs from the remote.

```bash
vibew deploy logs --target ssh://user@host
```

In multi-app mode (auto-detected), shows sidecar logs by default. Use `--app`
to view a specific site's logs.

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--target` | Yes | -- | Remote target in `ssh://user@host[:port]` format |
| `--config` | No | `./vibewarden.yaml` | Path to vibewarden.yaml (used to derive the remote project directory) |
| `--ssh-key` | No | SSH agent / `~/.ssh/config` | Path to the SSH private key file |
| `--lines` | No | `50` | Number of log lines to fetch (`0` = all) |
| `--follow` / `-f` | No | `false` | Stream log output continuously until cancelled (Ctrl-C) |
| `--app` | No | (sidecar) | Target a specific site in multi-app mode (e.g. `--app blog`) |

---

## Examples

### First deploy with secrets

```bash
vibew deploy \
  --config vibewarden.prod.yaml \
  --target ssh://ubuntu@203.0.113.10 \
  --secrets-from .env.prod
```

Save the unseal key from the output immediately.

### Subsequent deploy (config change, no secret rotation)

```bash
vibew deploy \
  --config vibewarden.prod.yaml \
  --target ssh://ubuntu@203.0.113.10
```

### Rotate secrets on redeploy

```bash
vibew deploy \
  --config vibewarden.prod.yaml \
  --target ssh://ubuntu@203.0.113.10 \
  --rotate-secrets \
  --secrets-from .env.prod
```

### Deploy with explicit unseal key

Use this when redeploying to a sealed OpenBao instance (e.g. after a server
restart) if the stored credentials file was lost:

```bash
vibew deploy \
  --config vibewarden.prod.yaml \
  --target ssh://ubuntu@203.0.113.10 \
  --unseal-key "YOUR_UNSEAL_KEY_HERE"
```

### Deploy via non-standard SSH port with explicit key

```bash
vibew deploy \
  --config vibewarden.prod.yaml \
  --target ssh://deploy@myserver.example.com:2222 \
  --ssh-key ~/.ssh/deploy_ed25519
```

### Check service status

```bash
vibew deploy status --target ssh://ubuntu@203.0.113.10
```

### Tail last 100 log lines

```bash
vibew deploy logs --target ssh://ubuntu@203.0.113.10 --lines 100
```

### Stream logs in real-time

```bash
vibew deploy logs --target ssh://ubuntu@203.0.113.10 --follow
```

Press Ctrl-C to stop streaming.

### Multi-app: deploy a second app to the same VM

```bash
vibew deploy \
  --config vibewarden.yaml \
  --target ssh://ubuntu@203.0.113.10
```

The CLI detects the existing sidecar and adds the new site. The config must
have `tls.domain` set.

### Multi-app: check all sites

```bash
vibew deploy status --target ssh://ubuntu@203.0.113.10
```

### Multi-app: view a specific site's logs

```bash
vibew deploy logs --target ssh://ubuntu@203.0.113.10 --app blog --follow
```

---

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `--target is required (e.g. ssh://user@host)` | Missing `--target` flag | Add `--target ssh://user@host` |
| `invalid --target: scheme must be "ssh"` | Wrong URL scheme (e.g. `http://`) | Use `ssh://user@host` format |
| `openbao bootstrap: ...` | OpenBao initialisation failed | Check remote Docker logs; verify port 8200 is reachable on the remote |
| `loading config: ...` | Invalid or missing vibewarden.yaml | Run `vibew validate --config <path>` locally |
| `cannot add a site without a TLS domain` | Adding a site to a multi-app host without `tls.domain` | Set `tls.domain` in vibewarden.yaml |
| `rsync: connection refused` | SSH not reachable | Verify `ssh user@host echo OK` works first |
| `remote files modified since last deploy` | Files on remote were edited manually (drift detected) | Use `--force` to overwrite, or inspect with `vibew deploy status` first |
| `architecture mismatch: image is arm64, remote is amd64` | Image built for the wrong platform | Rebuild with `vibew build --platform linux/amd64` |
| `health check failed after deploy` | App container did not become healthy in time | Check `vibew deploy logs --target ...`; ensure `/health` returns 200 |
| `docker load: no such file` | Image not built before deploy | Run `vibew build` before `vibew deploy` |

---

## Related

- [Deploy to VPS](deploy-to-vps.md) -- full first-deploy walkthrough
- [Multi-App Deployment](multi-app.md) -- multiple apps on one VM
- [Secret Management](secret-management.md) -- OpenBao configuration details
- [Production Hardening](production-hardening.md) -- post-deploy security checklist
