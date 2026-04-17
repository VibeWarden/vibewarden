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

#### What happens on first deploy

1. Generates runtime config (Docker Compose files, Kratos config) locally.
2. Transfers files to the remote via `rsync`.
3. Runs `docker compose up -d` on the remote.
4. When `secrets.enabled: true` in vibewarden.yaml, bootstraps OpenBao:
   initialises, unseals, enables KV v2 and AppRole, creates the vibewarden
   policy and role, and seeds secrets from `--secrets-from` if provided.
5. Prints the unseal key, root token, role ID, and secret ID. Save the unseal
   key -- it is not shown again.

#### What happens on subsequent deploys

1. Regenerates and transfers updated config.
2. Restarts Docker Compose on the remote.
3. Uses the stored unseal key from `~/vibewarden/<project>/.openbao-credentials`
   unless `--unseal-key` is provided explicitly.
4. Secrets are not re-seeded unless `--rotate-secrets` is passed.

---

### `vibew deploy status`

Show Docker Compose service status on the remote.

```bash
vibew deploy status --target ssh://user@host
```

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--target` | Yes | -- | Remote target in `ssh://user@host[:port]` format |
| `--config` | No | `./vibewarden.yaml` | Path to vibewarden.yaml (used to derive the remote project directory) |
| `--ssh-key` | No | SSH agent / `~/.ssh/config` | Path to the SSH private key file |

---

### `vibew deploy logs`

Fetch Docker Compose logs from the remote.

```bash
vibew deploy logs --target ssh://user@host
```

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--target` | Yes | -- | Remote target in `ssh://user@host[:port]` format |
| `--config` | No | `./vibewarden.yaml` | Path to vibewarden.yaml (used to derive the remote project directory) |
| `--ssh-key` | No | SSH agent / `~/.ssh/config` | Path to the SSH private key file |
| `--lines` | No | `50` | Number of log lines to fetch (`0` = all) |
| `--follow` / `-f` | No | `false` | Stream log output continuously until cancelled (Ctrl-C) |

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

---

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `--target is required (e.g. ssh://user@host)` | Missing `--target` flag | Add `--target ssh://user@host` |
| `invalid --target: scheme must be "ssh"` | Wrong URL scheme (e.g. `http://`) | Use `ssh://user@host` format |
| `openbao bootstrap: ...` | OpenBao initialisation failed | Check remote Docker logs; verify port 8200 is reachable on the remote |
| `loading config: ...` | Invalid or missing vibewarden.yaml | Run `vibew validate --config <path>` locally |
| `rsync: connection refused` | SSH not reachable | Verify `ssh user@host echo OK` works first |

---

## Related

- [Deploy to VPS](deploy-to-vps.md) -- full first-deploy walkthrough
- [Secret Management](secret-management.md) -- OpenBao configuration details
- [Production Hardening](production-hardening.md) -- post-deploy security checklist
