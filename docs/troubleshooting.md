# Troubleshooting

This guide explains the `vibew doctor` command, how to interpret its output, and how to
resolve the most common issues.

---

## `vibew doctor`

`vibew doctor` is a first-aid command. It validates static configuration and prints a
report so you can see exactly what is wrong before filing a bug or spending time
searching logs. For runtime upstream health, `curl https://<your-domain>/_vibewarden/health`
after `vibew dev` is up — doctor only validates static config.

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
| `--skip-le-preflight` | `false` | Skip the Let's Encrypt rate-limit preflight check for this run |

### Checks performed (in order)

#### Layer 1: Config and Docker

| # | Check name | What it tests |
|---|------------|---------------|
| 1 | **Config file** | `vibewarden.yaml` exists and parses without errors |
| 2 | **Docker daemon** | `docker info` succeeds within 5 s |
| 3 | **Docker Compose** | `docker compose version` returns a v2+ version string within 5 s |
| 4 | **Proxy port** | The port configured in `server.port` (default `8443`) is not already bound |
| 5 | **ACME email** | `tls.email` is set when `tls.acme_ca` contains "zerossl" (fails if missing) |
| 6 | **Image tag** | `app.image` matches a locally available Docker image (skipped when unset) |
| 7 | **LE rate-limit: `<domain>`** | Queries the public crt.sh CT log to count Let's Encrypt certificates issued for the registered domain in the last 168 hours. WARN at 4/5, FAIL at 5/5. Skipped when `tls.provider` is not `letsencrypt`, when `tls.acme_ca` overrides the default ACME endpoint, or when `tls.skip_rate_limit_check: true` / `--skip-le-preflight` is set. See [LE rate-limit preflight](#le-rate-limit-preflight) below. |

#### When the dev stack is running

These checks are skipped silently when `vibew dev` has not been started yet — they require
live containers to produce meaningful results. Start the stack first, then re-run `vibew doctor`.

| # | Check name | What it tests |
|---|------------|---------------|
| 8 | **Generated files** | `.vibewarden/generated/docker-compose.yml` is present on disk. Skipped when no compose containers are detected — run `vibew dev` first. |
| 9 | **TLS cert valid** | Performs a live TLS handshake against the sidecar, reads the leaf certificate from the handshake, and verifies it is not expired or expiring within 7 days. Skipped pre-stack. For runtime container health, query `_vibewarden/health` after `vibew dev` is up (see ADR-084). |

### Severity levels

| Badge | Meaning |
|-------|---------|
| `[OK]` | Check passed — nothing to do |
| `[WARN]` | Something worth noting, but not blocking (e.g., stack not started yet) |
| `[FAIL]` | Critical problem that will prevent VibeWarden from functioning |

---

## Sample output

### All checks pass (stack running)

```
VibeWarden Doctor
─────────────────────────────────────────
  [OK]            Config file            vibewarden.yaml — valid
  [OK]            Docker daemon          running
  [OK]            Docker Compose         Docker Compose version v2.27.0
  [OK]            Proxy port             port 8443 is available
  [OK]            ACME email             not using ZeroSSL — email not required
  [OK]            Generated files        .vibewarden/generated/docker-compose.yml
  [OK]            TLS certificate        valid until 2026-07-28
```

### Stack not yet started

Before `vibew dev` is run, the generated-files and TLS certificate checks are silently
skipped — they require a live stack to produce meaningful results. The output shows only
static-config and environment checks.

```
VibeWarden Doctor
─────────────────────────────────────────
  [OK]            Config file            vibewarden.yaml — valid
  [OK]            Docker daemon          running
  [OK]            Docker Compose         Docker Compose version v2.27.0
  [OK]            Proxy port             port 8443 is available
  [OK]            ACME email             not using ZeroSSL — email not required
```

### Proxy port already owned by a running `vibew dev`

When `vibew dev` is already running locally, port 8443 is expected to be in use.
`vibew doctor` probes `/_vibewarden/health` and recognises the sidecar as the owner,
so the check is `[OK]` — not `[FAIL]` — per ADR-084.

```
VibeWarden Doctor
─────────────────────────────────────────
  [OK]            Config file            vibewarden.yaml — valid
  [OK]            Docker daemon          running
  [OK]            Docker Compose         Docker Compose version v2.27.0
  [OK]            Proxy port             in use by local vibew dev (expected)
  [OK]            ACME email             not using ZeroSSL — email not required
  [OK]            Generated files        .vibewarden/generated/docker-compose.yml
  [OK]            TLS certificate        valid until 2026-07-28
```

### Port conflict (foreign process) + Docker not running

The `[FAIL] Proxy port` line only fires when the port is owned by a **non-VibeWarden**
process (i.e., the health probe does not find a `vibewarden` signature on the port).
A running `vibew dev` never triggers this FAIL — see the sample above. With Docker
unavailable, stack detection fails, so generated-files and TLS rows are silently absent.

```
VibeWarden Doctor
─────────────────────────────────────────
  [OK]            Config file            vibewarden.yaml — valid
  [FAIL]          Docker daemon          not running — start Docker Desktop or the Docker service
  [FAIL]          Docker Compose         not available — install Docker Compose v2
  [FAIL]          Proxy port             port 8443 is already in use
  [OK]            ACME email             not using ZeroSSL — email not required
```

### JSON output

`vibew doctor --json` is useful when another tool (a script, a CI step, or an AI agent)
needs to consume the results programmatically:

```json
[
  {
    "name": "Config file",
    "severity": "OK",
    "detail": "vibewarden.yaml — valid",
    "section": "Config & Docker"
  },
  {
    "name": "Docker daemon",
    "severity": "OK",
    "detail": "running",
    "section": "Config & Docker"
  },
  {
    "name": "Docker Compose",
    "severity": "OK",
    "detail": "Docker Compose version v2.27.0",
    "section": "Config & Docker"
  },
  {
    "name": "Proxy port",
    "severity": "OK",
    "detail": "port 8443 is available",
    "section": "Config & Docker"
  },
  {
    "name": "ACME email",
    "severity": "OK",
    "detail": "not using ZeroSSL — email not required",
    "section": "Config & Docker"
  },
  {
    "name": "Generated files",
    "severity": "OK",
    "detail": ".vibewarden/generated/docker-compose.yml",
    "section": "Config & Docker"
  },
  {
    "name": "TLS certificate",
    "severity": "OK",
    "detail": "valid until 2026-07-28",
    "section": "Local Runtime"
  }
]
```
Note: `Generated files` and `TLS certificate` only appear when `vibew dev` is running.
Pre-stack, those rows are silently absent — no `Container health` row ever appears
(that check was removed in v0.18.3; runtime health is at `/_vibewarden/health`).

---

## LE rate-limit preflight

When `tls.provider: letsencrypt` is set and no `tls.acme_ca` override is present,
`vibew doctor` queries the public [crt.sh](https://crt.sh) Certificate Transparency log to
count certificates issued for your registered domain in the last 168 hours (the
Let's Encrypt rate-limit window).

### Severity thresholds

| Certificates issued (168h) | Remaining | Result |
|------|------|------|
| 0–3 | 5–2 | `[OK]` — enough budget |
| 4 | 1 | `[WARN]` — "1 of 5 slots remaining this week for `<domain>`" |
| 5 | 0 | `[FAIL]` — "LE rate limit exhausted for `<domain>`; next slot at `<time>`; use --skip-le-preflight to bypass" |
| crt.sh unreachable / throttled | — | `[WARN]` — check degraded, not blocking |

A `[FAIL]` result causes `vibew doctor` to exit 1.

### Opt-out

If you are intentionally re-issuing a certificate within the same 168-hour window
(e.g. after revocation), suppress the check for one run:

```bash
vibew doctor --skip-le-preflight
```

Or set it permanently in `vibewarden.yaml`:

```yaml
tls:
  provider: letsencrypt
  skip_rate_limit_check: true
```

### Privacy note

The check sends your domain name to crt.sh — a public service operated by Sectigo.
The certificate, once issued, will be publicly visible in CT logs anyway. If this is
not acceptable, use `--skip-le-preflight` or `tls.skip_rate_limit_check: true`.

### Limitations

- **CT log propagation delay**: a certificate issued in the last 1–3 minutes may not
  yet appear in CT logs. At 4/5 budget the WARN threshold absorbs this lag.
- **Wildcard certificates**: `*.example.com` certs have a separate LE limit
  (10 per 7 days). This check counts only the registered-domain budget.

---

## Common issues and fixes

### Port conflict — proxy port already in use

**Symptom**

```
[FAIL]  Proxy port  port 8443 is already in use
```

> This `[FAIL]` no longer fires for a running `vibew dev`. Since ADR-084 the doctor
> probes `/_vibewarden/health` and reports
> `[OK] Proxy port  in use by local vibew dev (expected)` when the owner is the
> local sidecar. The FAIL below only applies when a **foreign** process owns the
> port.

**Cause**

A non-VibeWarden process is listening on the port configured in `server.port`
(default `8443`).

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

### Unknown configuration key(s)

**Symptom**

```
Configuration invalid: config vibewarden.yaml: unknown key(s): tls.dmain
Error: loading config: ...
```

**Cause**

`vibew validate` and `vibew bundle` run the strict loader (ADR-082): every key in
your `vibewarden.yaml` and sibling `vibewarden.production.yaml` must map to a field
in the schema. A typo, a removed key, or a rename — such as `docker_compose`
(the old name) instead of `compose_file` — is reported instead of silently dropped.

**Fix**

Open the offending file and correct the key. The authoritative schema lives in
[`internal/config/config.go`](https://github.com/vibewarden/vibewarden/blob/main/internal/config/config.go);
user-facing docs in [`docs/configuration.md`](configuration.md) and the fully
annotated `vibewarden.reference.yaml` mirror it. Every `mapstructure:"..."` tag in
`config.go` is a valid key.

The runtime path (`vibewarden serve`) stays lenient per ADR-065 so existing
deployments keep running across upgrades — strict rejection only fires at
validate/deploy time.

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

### Unhealthy containers (Kratos, Postgres)

Runtime container health is no longer checked by `vibew doctor` — that check was
removed in v0.18.3 (it produced misleading WARNs before `vibew dev` was started).
Use `_vibewarden/health` or `vibew logs` to diagnose container issues after the
stack is running.

```bash
# Check runtime health after vibew dev is up
curl https://localhost:8443/_vibewarden/health

# Stream logs for a specific service
vibew logs kratos --tail 50
vibew logs postgres --tail 100
```

**Common Kratos issues:**

| Cause | Fix |
|-------|-----|
| Postgres is not yet ready when Kratos starts | Wait 15–30 s; the `depends_on: condition: service_healthy` guard retries automatically |
| Kratos config points at the wrong DSN | Check `server.database.url` in `vibewarden.yaml` and ensure it matches the Postgres container credentials |
| Port 4433 / 4434 bound by another process | `lsof -i :4433` — stop the conflicting process |
| Kratos schema migration failed | `vibew logs kratos` — look for migration errors; run `vibew generate` then restart |
| Insufficient memory | Docker Desktop defaults to 2 GB RAM; increase to at least 4 GB in Docker Desktop → Settings → Resources |

**Postgres stuck in a restart loop:**

```bash
# Check the logs for the failing container
vibew logs postgres --tail 100

# Common fix: wipe the volume and let Postgres reinitialise
docker compose -f .vibewarden/generated/docker-compose.yml down -v
vibew dev
```

!!! warning "Data loss"
    `down -v` removes Docker volumes. Only use this in a **development environment**.
    In production, investigate the log output before taking destructive action.

---

### Image identity check failed (v0.18.3+)

Since v0.18.3, `vibew dev` verifies that the app image was built for the
current project before starting the stack. Two failure variants:

**Variant 1 — image built by a different project (same tag, different directory)**

```
Error: app image <tag> was built from a different project.
  Built from: /Users/foo/old-project
  Current:    /Users/you/current-project

Rebuild with: vibew dev --rebuild
```

**Cause:** Two projects with the same directory name share the same
`<name>-app:latest` tag. Docker reused the existing image without warning.

**Variant 2 — image has no project-root label (pre-v0.18.3 or external build)**

```
Error: app image <tag> is missing the vibew project-root label.
  This image was built before VibeWarden v0.18.3 OR by something other than vibew build.
  Current project: /Users/you/current-project

Rebuild with: vibew dev --rebuild
```

**Cause:** Images built by VibeWarden ≤ v0.18.2 carry no identity label.
Every project hits Variant 2 on the first `vibew dev` after upgrading to v0.18.3.

**Fix — both variants**

```bash
vibew dev --rebuild
```

`--rebuild` stops the stack, removes the app image, rebuilds via `vibew build`, and starts
the stack. Volumes are preserved. To also reset named volumes (Postgres data, LE certs),
pass `--rebuild --volumes`.

**Images set via `app.image:` in vibewarden.yaml are skipped automatically** — the
check only runs on the vibew-derived canonical tag. An INFO line is written to
stderr so you can confirm the skip occurred.

**After recovery, verify with logs:**

```bash
vibew logs --since 2m vibewarden    # confirm the sidecar started cleanly
vibew logs --follow                 # stream all services to watch for restart loops
```

---

### `vibew bundle` reports STALE

**Symptom**

```
Freshness:    STALE
  - modified: Dockerfile
  - modified: vibewarden.production.yaml
  - added:    src/handler.go
  (and 2 more)
```

**Cause**

One or more source files changed content since the last successful `vibew bundle` run.
The freshness check compares a per-file SHA-256 digest of all non-ignored project files
against the stored baseline at `.vibewarden/.input-digest`. A STALE verdict means the
image was built before those changes — the bundle may not reflect the current source.

**Fix — rebuild the image and re-bundle**

```bash
vibew bundle --build
```

`--build` runs `vibew build --platform <target>` before bundling. The freshness baseline
is updated after a successful bundle.

**Fix — suppress (when you know the image is correct)**

```bash
vibew bundle --allow-stale
```

Use `--allow-stale` when the changed files are irrelevant to the build (for example,
documentation or test fixtures committed after the image was built intentionally).
The STALE warning is suppressed; the bundle proceeds; the baseline is updated.

**What triggers STALE**

Files under the project root that are not excluded by `.gitignore`, `.dockerignore`, or
the built-in ignore list (`.git`, `.vibewarden`, `node_modules`, `vendor`, `dist`,
`build`, `target`, `.venv`, `__pycache__`, `bin`, `.next`). Content changes, file
additions, and file removals all trip the check. Renaming a file appears as one removal
and one addition.

**First bundle after upgrading to v0.19.0+**

The digest schema changed from v1 to v2 (per-file hashes) in v0.19.0. An existing v1
digest file is treated as missing — the first post-upgrade bundle is always FRESH
baseline, no false positive.

**`.vibewarden/.gitignore` generated file**

`vibew bundle` writes `.vibewarden/.gitignore` (containing `*`) so that git excludes
the entire `.vibewarden/` directory without touching your own `.gitignore`. This file is
generated and idempotent — do not edit it.

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

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | All checks passed (OK or WARN only) |
| `1` | At least one check failed (FAIL) |

This lets you gate a startup script on a clean doctor run:

```bash
vibew doctor && vibew dev
```
