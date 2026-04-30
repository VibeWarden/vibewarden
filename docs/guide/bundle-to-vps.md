# Bundle to VPS — end-to-end walkthrough

This guide walks through the canonical path from a scaffolded VibeWarden
project to a running stack on a VPS. It replaces the removed
`vibew deploy` command (see [ADR-086](../../decisions/adr-086-sunset-vibew-deploy.md)).

The flow is one command on your workstation, then a copy and a compose-up
on the host:

1. `vibew bundle` — produce `.vibewarden/bundle/` locally.
2. Copy the bundle directory to the host (path is up to you).
3. On the host, load `image.tar` (or pull from the registry) and run
   `docker compose up -d`.
4. Verify with `https://yourdomain/_vibewarden/health` — port **443** in
   production, not the dev port 8443.

The bundle's `README.md` is the canonical deploy contract. It lists what
each file is and the two non-obvious traps (the remote directory must
exist before you copy; the healthcheck port). There is no shell script
shipped in the bundle — operators and AI agents own the
`tar`/`ssh`/`docker compose` chain.

Day-two ops — restarts, log inspection, updates — are standard
`docker compose` commands against the copied bundle directory.

---

## Prerequisites

On your workstation:

- A scaffolded VibeWarden project (`vibew init` run at least once).
- A built app image (`vibew build` run at least once when `app.build` is
  set; skip if you pull from a registry via `app.image`).
- `ssh` and `tar` in `PATH` — required for the tar pipe transfer. Both
  are POSIX baseline tools present on all supported platforms.

On the VPS:

- A Linux host reachable over SSH.
- Docker Engine 20.10+ and the Docker Compose v2 plugin installed and
  runnable by the SSH user.
- A directory writable by the SSH user where the bundle will land. Create
  it before copying — `ssh <your-ssh-user>@<your-ssh-host> mkdir -p /path/to/bundle` works.

---

## 1. Build the deployment artifact

```bash
vibew bundle
```

This writes a self-contained tree under `.vibewarden/bundle/`:

```
.vibewarden/bundle/
  docker-compose.yml    # image: pinned, never build:
  vibewarden.yaml       # merged base + production override, strict-validated
  sample.env            # regenerated every run — commit this
  .env                  # first-run only; --overwrite to replace
  image.tar             # omit with --skip-image for registry-pull flows
  README.md             # the deploy contract — read this on the host
  kratos/               # anything the generator produces (auth, credentials…)
  .credentials          # stable across re-runs
```

Flags you may want:

| Flag | Purpose |
|------|---------|
| `--output <dir>` | Override the default `.vibewarden/bundle/`. |
| `--overwrite` | Replace an existing `.env` in the output dir. First run writes it; subsequent runs preserve the user's edits unless `--overwrite` is set. |
| `--skip-image` | Do not package `image.tar`. Useful when `app.image` points at a registry the VPS can pull from. |
| `--image <tag>` | Override the packaged image tag. Defaults to `<project>-app:latest`. |

The command never opens an SSH connection, never calls docker on a remote
host, and never touches files outside `--output`. Rerunning it with the
same inputs produces byte-identical output (deterministic).

---

## 2. Deploy

The bundle is just files. The contract is described in
`.vibewarden/bundle/README.md` and reproduced here:

1. Make sure the remote directory exists (e.g. `ssh <your-ssh-user>@<your-ssh-host> mkdir -p
   /path/to/bundle`). The transfer step below cannot create missing
   parent directories.
2. Copy the bundle to the host using the tar pipe form — it transfers
   dotfiles (`.env`, `.credentials`) that `scp -r bundle/*` silently
   drops due to POSIX glob expansion:
   ```bash
   tar -czf - -C .vibewarden/bundle . | ssh <your-ssh-user>@<your-ssh-host> 'tar -xzf - -C /path/to/bundle/'
   ```
   For redeploys with delta transfer, `rsync -av --delete .vibewarden/bundle/ <your-ssh-user>@<your-ssh-host>:/path/to/bundle/` also works (rsync is dotfile-safe by default).
3. On the host, in the bundle directory: load `image.tar` into Docker
   (or, in registry-pull mode built with `--skip-image`, ensure the
   image referenced by `docker-compose.yml` is published and reachable).
4. Bring the stack up with `docker compose up -d`.
5. Verify against the public URL: `curl https://yourdomain/_vibewarden/health`.
   Port **443** in production (TLS), not the dev port 8443.

If anything fails, `docker compose logs --tail=50` on the host is the
canonical diagnostic. There is no script that wraps these steps —
orchestrators (systemd, Ansible, Kubernetes manifests) and AI agents
own the chain.

The transfer assumes your `~/.ssh/config` already works for the target
host (keys, agent forwarding, host-key acceptance).

---

## Day-two operations

Once the bundle is on the VPS, all subsequent operations use standard
`docker compose`:

```bash
ssh <your-ssh-user>@<your-ssh-host> 'cd ~/vibewarden-bundle && docker compose ps'                 # status
ssh <your-ssh-user>@<your-ssh-host> 'cd ~/vibewarden-bundle && docker compose logs --tail=100 -f' # logs
ssh <your-ssh-user>@<your-ssh-host> 'cd ~/vibewarden-bundle && docker compose restart vibewarden' # restart
ssh <your-ssh-user>@<your-ssh-host> 'cd ~/vibewarden-bundle && docker compose pull && docker compose up -d'  # update
```

Redeploys are `vibew bundle` → repeat the copy + `docker compose up -d`. The bundle
is deterministic so the redeploy diff is limited to what you actually
changed (config, image tag, overlay).

---

## Troubleshooting

- **Image not loading on the VPS.** Check that the host architecture
  matches the image's target arch. `docker inspect --format='{{.Architecture}}' <image>`
  on both sides. `vibew bundle` does not auto-detect cross-arch mismatches;
  rebuild with `docker buildx build --platform linux/amd64` if needed.

- **TLS certificates.** `vibew bundle` emits whatever TLS config the
  merged `vibewarden.yaml` specifies. Let's Encrypt flows run on first
  container start. Verify with
  `ssh <your-ssh-user>@<your-ssh-host> 'cd ~/vibewarden-bundle && docker compose logs vibewarden | grep -i acme'`.

- **Strict config validation failures before any files are written.**
  `vibew bundle` calls `config.LoadStrict`, so unknown keys abort the
  command before the bundle directory is touched.

- **Health check failures on the VPS.** `curl -k https://localhost:$PORT/_vibewarden/health`
  over SSH is the canonical probe. The sidecar's own `docker compose
  logs vibewarden` usually pinpoints the failing plugin.

---

## See also

- [Deploy reference (removed-command landing)](../deploy-reference.md)
- [ADR-086: sunset `vibew deploy`](../../decisions/adr-086-sunset-vibew-deploy.md)
- [Production hardening](../production-hardening.md)
- [Troubleshooting](../troubleshooting.md)
