# Troubleshooting

This guide explains the `vibew doctor` command, how to interpret its output, and how to
resolve the most common issues.

---

## `vibew doctor`

`vibew doctor` is a first-aid command. It validates static configuration and prints a
report so you can see exactly what is wrong before filing a bug or spending time
searching logs. For runtime upstream health, run `vibew probe` after `vibew dev` is up
— it uses Go's TLS stack and works on macOS without LibreSSL friction. Doctor only
validates static config.

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
| `--preflight <env>` | _(unset)_ | Run pre-deploy validation against a named env. Reads `vibewarden.<env>.yaml`, merges with `vibewarden.yaml`, then appends five preflight checks. See [Pre-deploy preflight](#pre-deploy-preflight) below. |

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

#### Layer 3: Pre-deploy preflight (only with `--preflight <env>`)

These checks run only when `--preflight <env>` is passed. They append after all static and Dockerfile checks. If `vibewarden.<env>.yaml` does not exist, `vibew doctor` exits 1 immediately without running any checks.

| # | Check name | What it tests |
|---|------------|---------------|
| P1 | **DNS** | `tls.domain` resolves to at least one A or AAAA record. Uses the same env-resolver introduced in #1233 (ADR-102). |
| P2 | **Port 443** | `server.port` equals 443. WARN if set to any other value (non-blocking). |
| P3 | **Target platform** | `deploy.target_platform` is set in the merged config. FAIL if unset. |
| P4 | **Image arch** | The local app image architecture matches `deploy.target_platform`. Uses the Docker label-inspection path from #1219 (ADR-100). |
| P5 | **TLS email** | `tls.email` is non-empty. WARN (non-blocking) if missing — required by Let's Encrypt but not enforced pre-deploy. |

The merged config (base `vibewarden.yaml` + env overlay) is used for **all** checks, including the static Layer 1 checks, so the doctor report reflects exactly what will be deployed.

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

## Pre-deploy preflight

Run `vibew doctor --preflight production` before `vibew bundle` to catch DNS, port, architecture, and TLS-email mistakes before a deploy attempt.

```bash
vibew doctor --preflight production
```

This reads `vibewarden.production.yaml`, merges it with `vibewarden.yaml`, and runs the standard static + Dockerfile checks against the merged config plus five additional preflight checks. Exit code is `1` only when a FAIL-severity check is encountered. P3 (`deploy.target_platform` unset) and P4 (image arch mismatch) are FAIL-severity; P1 (DNS), P2 (port 443), and P5 (TLS email) are WARN-only and do not produce exit 1.

### Usage in the deploy flow

```bash
# Optional gate before building the bundle:
vibew doctor --preflight production
vibew build --platform linux/amd64
vibew bundle
```

`vibew doctor --preflight production` is not required — `vibew bundle` will still hard-fail on arch mismatch (check P4). However, running doctor first surfaces DNS and TLS-email issues before a multi-minute build.

### What each preflight check catches

| Check | Typical failure cause | Fix |
|-------|----------------------|-----|
| **P1 DNS** | `tls.domain` not yet pointing at the server | Update DNS A record; wait for TTL propagation |
| **P2 Port 443** | `server.port` left at dev default (8443) | Set `server.port: 443` in `vibewarden.production.yaml` |
| **P3 Target platform** | `deploy.target_platform` not set | Add `deploy.target_platform: linux/amd64` to `vibewarden.production.yaml` |
| **P4 Image arch** | Built on Apple Silicon without `--platform linux/amd64` | Run `vibew build --platform linux/amd64` before `vibew bundle` |
| **P5 TLS email** | `tls.email` missing | Add `tls.email: you@example.com` to `vibewarden.yaml` or the production overlay |

### Error: env file not found

```
Error: config file not found: vibewarden.production.yaml
```

The named env file must exist. Create it or check the spelling:

```bash
vibew add env production   # if this command is available, or create manually
```

The file only needs the fields that differ from `vibewarden.yaml`. Minimal example:

```yaml
# vibewarden.production.yaml
server:
  port: 443
tls:
  domain: myapp.example.com
  provider: letsencrypt
  email: you@example.com
deploy:
  target_platform: linux/amd64
```

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

### Docker socket permission denied or daemon not reachable (vibew dev / bundle / logs)

`vibew dev`, `vibew bundle`, and `vibew logs` shell out to Docker. When the Docker
socket is inaccessible or the daemon is not running, these commands exit with code `3`
and print an operator-friendly block instead of the raw Docker error.

**Symptom — daemon not running**

```
Error: Docker is unavailable.

  Ensure Docker Desktop is running and your user has access to
  the socket.

  On macOS:  open Docker Desktop
  On Linux:  sudo usermod -aG docker $USER && newgrp docker

Underlying error:
  Cannot connect to the Docker daemon at unix:///var/run/docker.sock.
  Is the docker daemon running?
```

Exit code: `3`

**Symptom — socket permission denied (Linux)**

```
Error: Docker is unavailable.

  Ensure Docker Desktop is running and your user has access to
  the socket.

  On macOS:  open Docker Desktop
  On Linux:  sudo usermod -aG docker $USER && newgrp docker

Underlying error:
  permission denied while trying to connect to the Docker API socket at
  unix:///var/run/docker.sock: dial unix /var/run/docker.sock: connect:
  permission denied
```

Exit code: `3`

**Fix — macOS: daemon not running**

```bash
open -a Docker
# wait for the whale icon in the menu bar to stop animating
docker info
```

**Fix — Linux: socket permission denied**

Add your user to the `docker` group:

```bash
sudo usermod -aG docker $USER
newgrp docker
# verify
docker info
```

The `newgrp docker` activates the new group membership for the current shell session.
A full logout/login also works.

**Fix — Linux: daemon not running**

```bash
sudo systemctl start docker
sudo systemctl enable docker   # start on boot
docker info
```

**Distinguishing from `vibew doctor` output**

`vibew doctor` prints `[FAIL] Docker daemon not running — start Docker Desktop or the
Docker service` in its check table. The hint block above is produced by `vibew dev`,
`vibew bundle`, and `vibew logs` — not by `vibew doctor`. Both indicate the same root
cause; the fix is the same.

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

### macOS: system curl fails handshake on dev cert

**Symptom**

```
LibreSSL/3.3.6: error:06FFF064:digital envelope routines:CONF_modules_load:bad decrypt
```

`curl https://localhost:8443/` from a default macOS shell fails the TLS handshake against the Caddy local-CA dev certificate. Same command from Linux works.

**Cause**

macOS ships a system `curl` linked against LibreSSL, which is stricter than OpenSSL on certain self-signed certificate profiles. Caddy's local CA issues an ECC intermediate that triggers this. Not a vibew bug — works around it client-side.

**Primary fix — `vibew probe` (no external tools required)**

```bash
vibew probe
```

`vibew probe` uses Go's stdlib TLS stack and bypasses LibreSSL entirely. It is the canonical health check after `vibew dev` on macOS. With `--env <name>` it reads `tls.domain` from the merged config and probes the production endpoint with full cert verification.

**Fallback — Homebrew curl (when non-vibew tooling must hit the endpoint)**

```bash
brew install curl
/opt/homebrew/opt/curl/bin/curl --insecure https://localhost:8443/_vibewarden/health
```

The Homebrew bottle is linked against OpenSSL and accepts the dev cert with `--insecure`. Do **not** alias `curl` to the Homebrew binary system-wide — only use it for sidecar testing.

**Fallback — Python `ssl` module (no Homebrew required)**

```bash
python3 -c '
import ssl, urllib.request
ctx = ssl._create_unverified_context()
print(urllib.request.urlopen("https://localhost:8443/_vibewarden/health", context=ctx).read().decode())
'
```

Python's bundled OpenSSL accepts the cert when verification is disabled. Useful when you cannot install Homebrew and do not have the `vibew` binary available.

**Note**

This applies to **dev / self-signed** certificates only. Production certs from Let's Encrypt or ZeroSSL handshake fine with macOS system curl.

---

### TLS handshake error immediately after deploy (ACME issuance in progress)

**Symptom**

`vibew probe --env production` exits 1 with output like:

```
ERROR: TLS handshake failed for 30s.

Likely ACME (Let's Encrypt) issuance still in progress. Check:
  ssh <host> docker compose logs vibewarden | grep -i acme
If the cert hasn't been issued yet, retry `vibew probe --env production`
in another minute.
```

Or — before the 30s budget is exhausted — progress lines on stderr:

```
Waiting for ACME issuance... (TLS handshake failed; retrying 30s)
Waiting for ACME issuance... (2s elapsed)
Waiting for ACME issuance... (4s elapsed)
...
```

**Cause**

Let's Encrypt (or ZeroSSL) ACME certificate issuance typically takes 5–30 s
after `docker compose up -d`. During that window the sidecar serves a
self-signed fallback that the strict prober (used by `--env`) rejects.

**Fix**

`vibew probe --env <name>` handles this automatically. It retries every 2s
for up to 30s. If the cert is issued within that window, the probe falls
through to the normal boot-gap check and exits 0 on healthy.

If 30s is not enough:

```bash
# Check whether ACME is still working
ssh <your-ssh-user>@<your-ssh-host> 'docker compose logs vibewarden | grep -i acme'

# Then retry
vibew probe --env production
```

**Conditions required for the TLS retry loop to engage:**

- `--env <name>` must be set. Default mode (no `--env`) treats TLS errors as
  immediate failures — a TLS error against the localhost dev cert is a real
  config bug (wrong CA, wrong port), not a transient.
- The error must match a known TLS handshake substring:
  `tls: internal error`, `tls: handshake failure`, `bad certificate`,
  `tls: protocol version not supported`.
- Connection-refused errors skip the TLS retry loop and use the normal
  per-env error message.

**If ACME keeps failing**

```bash
ssh <your-ssh-user>@<your-ssh-host> 'docker compose logs vibewarden --tail 100 | grep -i acme'
```

Common causes: port 80 blocked by the host firewall (ACME HTTP-01 requires
inbound TCP 80), DNS not yet propagated (A record points to the wrong IP),
or LE rate limit exceeded (run `vibew doctor --skip-le-preflight` to bypass
the local check and inspect the sidecar logs directly).

---

### Unhealthy containers (Kratos, Postgres)

Runtime container health is no longer checked by `vibew doctor` — that check was
removed in v0.18.3 (it produced misleading WARNs before `vibew dev` was started).
Use `_vibewarden/health` or `vibew logs` to diagnose container issues after the
stack is running.

```bash
# Check runtime health after vibew dev is up (vibew probe preferred on macOS)
vibew probe

# Or with curl (works on Linux; on macOS requires Homebrew curl or Python workaround)
curl https://localhost:8443/_vibewarden/health

# Stream logs for a specific service
vibew logs kratos --tail 50
vibew logs kratos-db --tail 100
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
vibew logs kratos-db --tail 100

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

### OpenBao: unseal key missing after host reboot or bundle re-run

**Symptom** (from `docker compose logs seed-secrets`):

```
ERROR: OPENBAO_UNSEAL_KEY not found in /.credentials — vault is initialized but unseal key is missing. Recover manually.
```

**Cause**

`seed-secrets.sh` writes `OPENBAO_UNSEAL_KEY` into `.credentials` on first boot (v0.19+,
ADR-104). The key is missing because either:

- `.credentials` was overwritten by re-running `vibew bundle` after the initial prod deploy.
  `vibew bundle` regenerates `.credentials` with a new placeholder token and does not
  include the `OPENBAO_UNSEAL_KEY` produced at boot time.
- `.credentials` was not restored after a host migration.

**Fix — backup exists**

1. Restore `.credentials` from your backup (the version containing `OPENBAO_UNSEAL_KEY`).
2. Restart the stack:

    ```bash
    ssh <your-ssh-user>@<your-ssh-host> 'cd /path/to/bundle && docker compose restart'
    ```

**Fix — backup lost (destructive)**

If the unseal key backup is gone, the vault must be re-initialised. All secrets stored
in the vault will be lost.

```bash
ssh <your-ssh-user>@<your-ssh-host> 'cd /path/to/bundle && docker compose down -v && docker compose up -d'
```

After the stack restarts, `seed-secrets` re-initialises the vault and writes a new
`OPENBAO_UNSEAL_KEY` to `.credentials`. Re-seed any secrets with `vibew secret set`.
Back up `.credentials` immediately.

---

## Exit codes

### `vibew doctor`

| Code | Meaning |
|------|---------|
| `0` | All checks passed (OK or WARN only) |
| `1` | At least one check failed (FAIL) |

This lets you gate a startup script on a clean doctor run:

```bash
vibew doctor && vibew dev
```

### `vibew dev`, `vibew bundle`, `vibew logs`

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error (config, image identity mismatch, unknown service, etc.) |
| `2` | Config or flag validation error |
| `3` | Docker unavailable — socket permission denied or daemon not running. The command prints an operator-friendly hint block before exiting. See [Docker socket permission denied or daemon not reachable](#docker-socket-permission-denied-or-daemon-not-reachable-vibew-dev--bundle--logs) above. |
