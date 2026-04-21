# Deploy — removed (breaking change)

The `vibew deploy` command was removed in the release referenced by
[ADR-086](../decisions/adr-086-sunset-vibew-deploy.md). It has been replaced
by `vibew bundle` plus a manual `scp` / `ssh` / `docker compose up -d` flow.

`vibew deploy` is no longer a registered command. Invoking it prints
cobra's default `unknown command "deploy"` error and exits non-zero. No
files are transferred, no SSH connection is opened, and no remote state
is touched.

---

## What changed

| Before (removed) | After (supported) |
|------------------|-------------------|
| `vibew deploy --target ssh://user@host --config vibewarden.prod.yaml` | `vibew bundle` + `cd .vibewarden/bundle && ./deploy.sh user@host` |
| `vibew deploy status --target ssh://user@host` | `ssh user@host 'cd ~/vibewarden-bundle && docker compose ps'` |
| `vibew deploy logs --target ssh://user@host --lines 50` | `ssh user@host 'cd ~/vibewarden-bundle && docker compose logs --tail=50'` |
| `vibew deploy --dry-run` | `vibew bundle` (bundle is always written locally; inspect `.vibewarden/bundle/`) |

---

## Migration — two steps

See [Bundle to VPS](guide/bundle-to-vps.md) for the end-to-end walkthrough.
In short:

```bash
# 1. Build the deployment artifact locally.
vibew bundle --output .vibewarden/bundle/

# 2. Ship it to the VPS (scp + docker load + compose up + healthcheck).
cd .vibewarden/bundle && ./deploy.sh user@host
```

`deploy.sh` runs **locally** — it opens a single `scp` + two `ssh`
sessions, probes `/_vibewarden/health` on the remote, and dumps the last
50 log lines to stderr on failure. Pass `user@host:/remote/path` to
override the default remote path (`~/vibewarden-bundle`).

The bundle is self-contained and deterministic: same inputs, same bytes.
See [`vibew bundle`](../README.md#bundle) for the flag reference.

---

## Why the change

Four retrospectives converged on the same finding: `vibew deploy`'s remote
SSH orchestration — rsync, remote docker compose, remote OpenBao bootstrap,
remote health probes, remote arch checks — was the single largest source of
bugs and user friction in the product (16 bugs across 3 retro cycles).
ADR-086 retires the feature in favour of a thin, purely local bundle
pipeline. The user owns the transport.

---

## Rollback

This is a one-way change. There is no deprecation stub — `vibew deploy` is
not a registered command. New scripts and agent prompts must target
`vibew bundle` from day one.

---

## See also

- [`vibew bundle` reference](../README.md#bundle)
- [Bundle to VPS walkthrough](guide/bundle-to-vps.md)
- [ADR-086](../decisions/adr-086-sunset-vibew-deploy.md)
- [CHANGELOG — Breaking changes](../CHANGELOG.md)
