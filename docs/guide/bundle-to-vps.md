# Bundle to VPS — end-to-end walkthrough

This guide walks through the canonical path from a scaffolded VibeWarden
project to a running stack on a VPS. It replaces the removed
`vibew deploy` command (see [ADR-086](../../decisions/adr-086-sunset-vibew-deploy.md)).

The whole flow is two commands from your workstation:

```bash
vibew bundle
cd .vibewarden/bundle && ./deploy.sh user@host
```

`deploy.sh` runs **locally**. It `scp`s the bundle directory to the host,
loads the image, brings the stack up with `docker compose up -d`, and
probes `/_vibewarden/health` on the remote. On failure it dumps the last
50 log lines to stderr and exits non-zero.

Everything after that — restarts, log inspection, updates — is done with
standard `docker compose` commands against the copied bundle directory.

---

## Prerequisites

On your workstation:

- A scaffolded VibeWarden project (`vibew init` run at least once).
- A built app image (`vibew build` run at least once when `app.build` is
  set; skip if you pull from a registry via `app.image`).
- `scp` and `ssh` in `PATH` — the bundle uses whatever transport you
  configure in `~/.ssh/config`.

On the VPS:

- A Linux host reachable over SSH.
- Docker Engine 20.10+ and the Docker Compose v2 plugin installed and
  runnable by the SSH user.
- The user's home directory writable (the bundle lands under
  `~/vibewarden-bundle/` by default; override by passing
  `user@host:/remote/path` to `deploy.sh`).

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
  deploy.sh             # reference script, mode 0o750
  image.tar             # omit with --skip-image for registry-pull flows
  README.md             # per-bundle 3-paragraph cheat sheet
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

## 2. Deploy with `deploy.sh`

From the bundle directory on your workstation:

```bash
cd .vibewarden/bundle
./deploy.sh user@host                        # default remote path: ~/vibewarden-bundle
./deploy.sh user@host:/srv/myapp             # custom remote path
```

`deploy.sh` runs locally. It:

1. `scp -r`s the bundle directory to the remote path.
2. Runs `docker load -i image.tar` (or `docker compose pull` when the
   bundle was built with `--skip-image`) over a single `ssh`.
3. Runs `docker compose up -d` in the same `ssh` session.
4. Probes `http://localhost:<port>/_vibewarden/health` from the remote
   via a second `ssh`.
5. On failure, dumps `docker compose logs --tail 50` to stderr and
   exits 1.

The script assumes your `~/.ssh/config` already works for the target
host (keys, agent forwarding, host-key acceptance). There is no SSH key
setup automation.

You are free to replace `deploy.sh` with your own orchestration (systemd
unit, Ansible play, Kubernetes manifest) — the bundle is just files.

If you prefer `rsync` for delta transfers, use it directly in place of
the script's `scp`:

```bash
rsync -av --delete .vibewarden/bundle/ user@host:~/vibewarden-bundle/
ssh user@host 'cd ~/vibewarden-bundle && docker load -i image.tar && docker compose up -d'
```

---

## Day-two operations

Once the bundle is on the VPS, all subsequent operations use standard
`docker compose`:

```bash
ssh user@host 'cd ~/vibewarden-bundle && docker compose ps'                 # status
ssh user@host 'cd ~/vibewarden-bundle && docker compose logs --tail=100 -f' # logs
ssh user@host 'cd ~/vibewarden-bundle && docker compose restart vibewarden' # restart
ssh user@host 'cd ~/vibewarden-bundle && docker compose pull && docker compose up -d'  # update
```

Redeploys are `vibew bundle` → `./deploy.sh user@host` again. The bundle
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
  `ssh user@host 'cd ~/vibewarden-bundle && docker compose logs vibewarden | grep -i acme'`.

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
