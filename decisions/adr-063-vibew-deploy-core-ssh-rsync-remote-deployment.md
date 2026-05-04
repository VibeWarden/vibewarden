# ADR-063: vibew deploy core — SSH + rsync remote deployment

**Date**: 2026-04-03
**Status**: Obsolete — `vibew deploy` core was sunset by ADR-086 (#1138). Kept as historical record of the SSH-based deploy design.

### Context

VibeWarden targets vibe coders who need zero-to-secure in minutes. After a project
is scaffolded and tested locally, the user needs a simple way to deploy the
generated Docker Compose stack to a remote server. The common deployment target
is a Linux VPS (Hetzner, DigitalOcean, etc.) accessible via SSH.

The question: should we use a Go SSH library (e.g. `golang.org/x/crypto/ssh`) or
shell out to the system `ssh` and `rsync` binaries?

### Decision

Shell out to the system `ssh` and `rsync` binaries. No Go SSH library.

Reasons:
1. **SSH agent forwarding**: system ssh automatically uses the user's running
   ssh-agent, so key-based auth just works without any credential management in Go.
2. **SSH config**: system ssh reads `~/.ssh/config` (ProxyJump, IdentityFile,
   ServerAliveInterval, etc.) — all of this is free when shelling out.
3. **Simplicity**: no additional Go dependency, no license concern, no TLS
   handshake implementation to maintain.
4. **rsync is the right tool for file sync**: delta transfer, permission
   preservation, exclude patterns — reinventing this in Go would be worse.

Target URL format: `ssh://user@host[:port]` — parsed with `net/url` from stdlib.
The remote directory is hardcoded to `~/vibewarden/<project-name>/` for
predictability and simplicity in v1.

The deploy flow:
1. Load and validate config from `--config` path
2. Run `generate` internally (calls `generateapp.Service.Generate`)
3. SSH to remote, verify Docker and Docker Compose are installed
4. `rsync` the `.vibewarden/generated/` directory + `vibewarden.yaml` to
   `~/vibewarden/<project-name>/`
5. On remote: `docker compose pull && docker compose up -d`
6. Health check: HTTP GET `/_vibewarden/health` via SSH port-forward with
   60-second timeout and exponential backoff retries
7. Report success or failure

### Architecture

```
internal/ports/remote.go           — RemoteExecutor interface
internal/adapters/ssh/executor.go  — SSH adapter (os/exec shell-out)
internal/app/deploy/service.go     — Deploy application service
internal/cli/cmd/deploy.go         — cobra command + subcommands
```

CLI surface:
```
vibew deploy --config vibewarden.prod.yaml --target ssh://user@host
vibew deploy status --target ssh://user@host
vibew deploy logs --target ssh://user@host [--lines 50]
```

### Consequences

**Positive:**
- Zero new Go dependencies
- SSH agent / `~/.ssh/config` work transparently
- rsync handles incremental updates efficiently
- Simple, auditable implementation

**Negative:**
- Requires `ssh` and `rsync` to be installed on the developer's machine
  (both are available by default on macOS and most Linux distros)
- No Windows support in v1 (Windows ships neither ssh nor rsync by default in
  all versions); documented limitation, deferred to v2
- Integration tests require a real SSH target; unit tests use a mock executor

**Limitations (v1):**
- Remote directory is `~/vibewarden/<project-name>/` — not configurable
- No rollback on failure — the operator must redeploy manually
- Health check polls `/_vibewarden/health` via a direct HTTP call over an
  SSH tunnel; does not validate TLS certificate in v1
