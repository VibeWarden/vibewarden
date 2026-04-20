# Troubleshooting

This guide explains the `vibew doctor` command, how to interpret its output, and how to
resolve the most common issues.

---

## `vibew doctor`

`vibew doctor` is a first-aid command. It runs a series of independent diagnostics and
prints a report so you can see exactly what is wrong before filing a bug or spending time
searching logs.

```bash
vibew doctor
```

Every check runs regardless of whether an earlier check failed. When at least one check
reports **FAIL**, the command exits with status code `1`, so you can use it in CI or
pre-flight scripts.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config <path>` | `./vibewarden.yaml` | Path to a non-default config file |
| `--json` | `false` | Emit results as a JSON array instead of the human-readable table |
| `--target <ssh://...>` | (none) | Remote target for production checks; when omitted, only local checks run |

### Checks performed (in order)

#### Layer 1: Config and Docker

| # | Check name | What it tests |
|---|------------|---------------|
| 1 | **Config file** | `vibewarden.yaml` exists and parses without errors |
| 2 | **Docker daemon** | `docker info` succeeds within 5 s |
| 3 | **Docker Compose** | `docker compose version` returns a v2+ version string within 5 s |
| 4 | **Proxy port** | The port configured in `server.port` (default `8443`) is not already bound |
| 5 | **Generated files** | `.vibewarden/generated/docker-compose.yml` is present on disk |
| 6 | **Container health** | `docker compose ps` shows all containers in `running` / `healthy` state |
| 7 | **ACME email** | `tls.email` is set when using `letsencrypt` provider (warns if missing) |
| 8 | **Image tag** | `app.image` matches a locally available Docker image (skipped when unset) |

#### Layer 2: Local Runtime

| # | Check name | What it tests |
|---|------------|---------------|
| 9 | **Upstream reachable** | The configured upstream port is listening locally |
| 10 | **TLS cert valid** | Local TLS certificate (when using `self-signed` or `external`) is not expired or expiring within 7 days |

#### Layer 3: Production (only with `--target`)

| # | Check name | What it tests |
|---|------------|---------------|
| 11 | **SSH connectivity** | SSH connection to the remote host succeeds |
| 12 | **Arch compatibility** | Local build architecture matches the remote server architecture |
| 13 | **Remote containers** | Remote Docker Compose services are healthy |
| 14 | **Domain DNS** | `tls.domain` resolves to the target host IP (only when domain is set) |
| 15 | **Remote TLS cert** | Remote certificate is valid and not expiring within 30 days (only when domain is set) |

### Severity levels

| Badge | Meaning |
|-------|---------|
| `[OK]` | Check passed — nothing to do |
| `[WARN]` | Something worth noting, but not blocking (e.g., stack not started yet) |
| `[FAIL]` | Critical problem that will prevent VibeWarden from functioning |

---

## Sample output

### All checks pass

```
VibeWarden Doctor
─────────────────────────────────────────
  [OK]            Config file            vibewarden.yaml — valid
  [OK]            Docker daemon          running
  [OK]            Docker Compose         Docker Compose version v2.27.0
  [OK]            Proxy port             port 8443 is available
  [OK]            Generated files        .vibewarden/generated/docker-compose.yml
  [OK]            Container health       4 container(s) running
```

### Stack not yet started

```
VibeWarden Doctor
─────────────────────────────────────────
  [OK]            Config file            vibewarden.yaml — valid
  [OK]            Docker daemon          running
  [OK]            Docker Compose         Docker Compose version v2.27.0
  [OK]            Proxy port             port 8443 is available
  [WARN]          Generated files        .vibewarden/generated/docker-compose.yml not found — run 'vibewarden generate' first
  [WARN]          Container health       no containers found — run 'vibewarden dev' to start the stack
```

### Port conflict + Docker not running

```
VibeWarden Doctor
─────────────────────────────────────────
  [OK]            Config file            vibewarden.yaml — valid
  [FAIL]          Docker daemon          not running — start Docker Desktop or the Docker service
  [FAIL]          Docker Compose         not available — install Docker Compose v2
  [FAIL]          Proxy port             port 8443 is already in use
  [WARN]          Generated files        .vibewarden/generated/docker-compose.yml not found — run 'vibewarden generate' first
  [WARN]          Container health       could not query containers — stack may not be running
```

### JSON output

`vibew doctor --json` is useful when another tool (a script, a CI step, or an AI agent)
needs to consume the results programmatically:

```json
[
  {
    "name": "Config file",
    "severity": "OK",
    "detail": "vibewarden.yaml — valid"
  },
  {
    "name": "Docker daemon",
    "severity": "OK",
    "detail": "running"
  },
  {
    "name": "Docker Compose",
    "severity": "OK",
    "detail": "Docker Compose version v2.27.0"
  },
  {
    "name": "Proxy port",
    "severity": "OK",
    "detail": "port 8443 is available"
  },
  {
    "name": "Generated files",
    "severity": "OK",
    "detail": ".vibewarden/generated/docker-compose.yml"
  },
  {
    "name": "Container health",
    "severity": "OK",
    "detail": "4 container(s) running"
  }
]
```

---

## Common issues and fixes

### Port conflict — proxy port already in use

**Symptom**

```
[FAIL]  Proxy port  port 8443 is already in use
```

**Cause**

Another process is listening on the port configured in `server.port` (default `8443`).

**Fix — option 1: change VibeWarden's port**

In `vibewarden.yaml`:

```yaml
server:
  port: 9443
```

**Fix — option 2: stop the conflicting process**

Find the PID that owns the port and stop it:

```bash
# macOS / Linux
lsof -i :8443
# or
ss -tlnp | grep 8443

# then kill the process
kill <PID>
```

---

### Docker not running

**Symptom**

```
[FAIL]  Docker daemon  not running — start Docker Desktop or the Docker service
[FAIL]  Docker Compose  not available — install Docker Compose v2
```

**Cause**

The Docker daemon is not reachable. VibeWarden needs Docker to manage the Kratos,
Postgres, and other sidecar containers.

**Fix — macOS**

Open Docker Desktop from the Applications folder, or:

```bash
open -a Docker
# wait for the whale icon in the menu bar to stop animating
docker info
```

**Fix — Linux (systemd)**

```bash
sudo systemctl start docker
sudo systemctl enable docker   # make it start on boot
docker info
```

**Fix — Docker not installed**

Follow the official Docker Engine install guide for your OS:
<https://docs.docker.com/engine/install/>

VibeWarden requires Docker Compose v2 (the `docker compose` subcommand, not the
standalone `docker-compose` binary). Docker Desktop ships with Compose v2 by default.
On Linux you may need to install the `docker-compose-plugin` package separately.

---

### Config file not found or invalid

**Symptom**

```
[FAIL]  Config file  vibewarden.yaml not found or invalid
```

**Cause — file missing**

`vibew doctor` looks for `vibewarden.yaml` in the current directory. Either the file does
not exist or you are running the command from the wrong directory.

**Fix**

```bash
# Run from the directory that contains vibewarden.yaml
cd /path/to/your/project
vibew doctor

# Or point directly at the file
vibew doctor --config /path/to/vibewarden.yaml
```

If you have not created a config file yet, scaffold one:

```bash
vibew wrap --upstream 3000
```

**Cause — YAML parse error**

The config file exists but contains a syntax error.

**Fix**

Validate the YAML with a linter:

```bash
python3 -c "import sys, yaml; yaml.safe_load(open('vibewarden.yaml'))" && echo OK
```

Common mistakes: tabs instead of spaces, missing quotes around values that contain
colons, or misaligned indentation.

---

### Generated files missing

**Symptom**

```
[WARN]  Generated files  .vibewarden/generated/docker-compose.yml not found — run 'vibew generate' first
```

**Cause**

The `vibew generate` step has not been run, the `.vibewarden/` directory was deleted, or
the project was cloned without the generated directory (it is gitignored by default).

**Fix**

```bash
vibew generate
# then start the stack
vibew dev
```

---

### Kratos unreachable

Container health failures often manifest as a Kratos container in a non-healthy state:

**Symptom**

```
[FAIL]  Container health  unhealthy containers: [kratos (running/unhealthy)]
```

**Possible causes and fixes**

| Cause | Fix |
|-------|-----|
| Postgres is not yet ready when Kratos starts | Wait 15–30 s and run `vibew doctor` again; the `depends_on: condition: service_healthy` guard retries automatically |
| Kratos config points at the wrong DSN | Check `server.database.url` in `vibewarden.yaml` and ensure it matches the Postgres container credentials |
| Port 4433 / 4434 bound by another process | `lsof -i :4433` — stop the conflicting process |
| Kratos schema migration failed | `docker compose logs kratos` — look for migration errors; run `vibew generate` to regenerate the config and `docker compose up -d` to restart |
| Insufficient memory | Docker Desktop defaults to 2 GB RAM; increase to at least 4 GB in Docker Desktop → Settings → Resources |

View Kratos logs directly:

```bash
docker compose -f .vibewarden/generated/docker-compose.yml logs kratos --tail 50
```

---

### Containers stuck in a restart loop

**Symptom**

```
[FAIL]  Container health  unhealthy containers: [postgres (restarting/)]
```

**Fix**

```bash
# Check the logs for the failing container
docker compose -f .vibewarden/generated/docker-compose.yml logs postgres --tail 100

# Common fix: wipe the volume and let Postgres reinitialise
docker compose -f .vibewarden/generated/docker-compose.yml down -v
vibew dev
```

!!! warning "Data loss"
    `down -v` removes Docker volumes. Only use this in a **development environment**.
    In production, investigate the log output before taking destructive action.

---

### Secrets plugin fails to start -- missing master key

**Symptom**

```
secrets plugin: builtin store: master key not configured
```

**Cause**

The `VIBEWARDEN_SECRETS_MASTER_KEY` environment variable is not set and
`secrets.builtin.key_file` is not configured in `vibewarden.yaml`. The built-in
encrypted secret store requires a 32-byte master key to encrypt and decrypt secrets.

**Fix**

```bash
export VIBEWARDEN_SECRETS_MASTER_KEY=$(openssl rand -hex 32)
```

Or set `secrets.builtin.key_file` in `vibewarden.yaml` pointing to a file that contains the
hex-encoded 32-byte key:

```yaml
secrets:
  enabled: true
  store: builtin
  builtin:
    key_file: /path/to/master.key
```

Save the master key somewhere safe -- if you lose it, your secrets are unrecoverable.

---

### Architecture mismatch between build and deploy target

**Symptom**

```
[FAIL]  Arch compatibility  image is arm64, remote is amd64
```

**Cause**

You built the Docker image on Apple Silicon (arm64) without specifying the
target platform. The remote server runs amd64 and cannot execute the image.

**Fix**

Rebuild with the correct platform:

```bash
vibew build --platform linux/amd64
```

Then redeploy:

```bash
vibew deploy --target ssh://user@host
```

---

### Remote TLS certificate expiring or invalid

**Symptom**

```
[WARN]  Remote TLS cert  certificate expires in 12 days
[FAIL]  Remote TLS cert  certificate expired 2 days ago
```

**Cause**

The TLS certificate on the remote host is nearing expiry or has already expired.
If you are using `tls.provider: letsencrypt`, the ACME renewal may have failed.

**Fix**

Inspect the certificate details:

```bash
vibew tls status --domain example.com --target ssh://user@host
```

If renewal failed, check the sidecar logs for ACME errors:

```bash
vibew deploy logs --target ssh://user@host --lines 100
```

Common causes: DNS not pointing to the server, port 80 blocked (HTTP-01 challenge
requires it), or rate limits exhausted (switch to ZeroSSL via `tls.acme_ca`).

---

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | All checks passed (OK or WARN only) |
| `1` | At least one check failed (FAIL) |

This lets you gate a startup script on a clean doctor run:

```bash
vibew doctor && vibew dev
```
