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
| `vibew deploy --target ssh://user@host --config vibewarden.production.yaml` | `vibew bundle` → copy `.vibewarden/bundle/` to the host → `docker compose up -d` (see the bundle's `README.md` for the full contract) |
| `vibew deploy status --target ssh://user@host` | `ssh user@host 'cd ~/vibewarden-bundle && docker compose ps'` |
| `vibew deploy logs --target ssh://user@host --lines 50` | `ssh user@host 'cd ~/vibewarden-bundle && docker compose logs --tail=50'` |
| `vibew deploy --dry-run` | `vibew bundle` (bundle is always written locally; inspect `.vibewarden/bundle/`) |

---

## Migration — two steps

See [Bundle to VPS](guide/bundle-to-vps.md) for the end-to-end walkthrough.
In short:

1. Run `vibew bundle --output .vibewarden/bundle/` to build the deployment
   artifact locally.
2. Copy the contents of that directory to the host, load the image (or
   pull from the registry), bring the stack up with `docker compose up -d`,
   and verify with `https://yourdomain/_vibewarden/health` (port **443**
   in production, not the dev port 8443). The bundle's `README.md`
   describes the contract — there are no shell scripts to run.

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
