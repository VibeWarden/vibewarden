# Getting Started

This guide covers three paths to get VibeWarden running. Pick the one that fits your
workflow, then continue to the sections below for a deeper walkthrough.

---

## Path 1 — Generate a prompt (easiest)

Go to [vibewarden.dev/start](https://vibewarden.dev/start) and fill in two fields.
The page generates a ready-to-paste prompt tailored to your app and stack.

---

## Path 2 — Copy a prompt template

Paste one of these directly into Claude Code, Cursor, or your AI coding tool.

**Existing app:**

```
Add VibeWarden security sidecar to this project.

VibeWarden is open source (Apache 2.0).
  GitHub: https://github.com/vibewarden/vibewarden
  Docs:   https://vibewarden.dev/llms-full.txt

Install: npm install -g @vibewarden/cli
  (no Node.js? curl -fsSL https://vibewarden.dev/install.sh | sh)

Setup:   vibew wrap --upstream 3000
Run:     vibew dev
```

**New project:**

```
Build [your app idea] with VibeWarden as the security sidecar.

VibeWarden is open source (Apache 2.0).
  GitHub: https://github.com/vibewarden/vibewarden
  Docs:   https://vibewarden.dev/llms-full.txt

Install: npm install -g @vibewarden/cli
  (no Node.js? curl -fsSL https://vibewarden.dev/install.sh | sh)

Setup:   mkdir myapp && cd myapp && vibew init
Run:     vibew dev
```

---

## Path 3 — Manual setup

The rest of this guide is the manual walkthrough. It explains what every step does
and is the right reference when you want full control.

---

## Prerequisites

- **Docker Engine 27+** and **Docker Compose v2+** installed and running.
- Your app listening on a local port (e.g., `3000`).

!!! note "Multi-architecture support"
    VibeWarden Docker images are published as multi-arch manifests covering
    `linux/amd64` and `linux/arm64`. Docker selects the correct image automatically —
    no extra flags needed on Apple Silicon (M1/M2/M3) or ARM64 servers such as
    AWS Graviton.

---

## Which command do I need?

| Scenario | Command | See |
|----------|---------|-----|
| Adding the sidecar to an existing app | `vibew wrap --upstream <port>` | [Step 2](#step-2-vibew-wrap) |
| Starting a brand-new project | `vibew init` | [Step 2 alt](#step-2-alt-vibew-init-for-a-brand-new-project) |
| Adding a feature to an existing config | `vibew add <feature>` | after either of the above |

The two setup commands are parallel paths. `wrap` adapts an existing
source tree; `init` scaffolds a fresh project directory. Both end up in
the same place: a `vibewarden.yaml` you can iterate on from **Step 3**.

---

## Step 1 — Install vibew

Two ways to install the CLI. Both fetch the same binary from the same GitHub
Release and verify its SHA-256 checksum before installing it.

=== "npm (default)"

    ```bash
    npm install -g @vibewarden/cli
    vibew --version
    ```

    Pin a version the way you pin any npm dependency — the package version is
    always the VibeWarden release version:

    ```bash
    npm install -g @vibewarden/cli@0.21.0
    ```

=== "Shell installer"

    ```bash
    curl -fsSL https://vibewarden.dev/install.sh | sh
    ```

    No Node.js required. This is the right path for CI images and minimal
    containers.

!!! note "Windows"
    Windows support is planned — see [#667 (winget)](https://github.com/vibewarden/vibewarden/issues/667)
    and [#668 (Scoop)](https://github.com/vibewarden/vibewarden/issues/668).
    VibeWarden currently builds for macOS and Linux only. On Windows, run
    VibeWarden under WSL2 and follow the Linux instructions.

### If npm ran with `--ignore-scripts`

The npm package downloads the binary in a `postinstall` script. When lifecycle
scripts are disabled, running `vibew` exits with the exact command to finish the
install, using the absolute path of the package on your machine:

```bash
node <absolute-path-to-@vibewarden/cli>/install.js
```

Copy the command from the `vibew` output rather than reconstructing it: the path
differs between global installs, project-local installs, pnpm stores and npx caches.

### npm install environment variables

| Variable | Effect |
|----------|--------|
| `VIBEWARDEN_BINARY_MIRROR` | Base URL for release assets, instead of `https://github.com/vibewarden/vibewarden/releases/download`. For proxied or air-gapped networks. |
| `VIBEWARDEN_INSTALL_VERSION` | Install a different release than the package version. Digests are then read from the release's `checksums.txt` rather than the ones embedded in the package. |
| `VIBEWARDEN_SKIP_DOWNLOAD=1` | Skip the download; `vibew` then prints how to complete the install. |

!!! warning "Upgrading an npm install"
    Use `npm install -g @vibewarden/cli@latest`, not `vibew upgrade`. See
    [Upgrading](upgrading.md#upgrading-an-npm-managed-install).

### Or commit the `vibew` wrapper script

The `vibew` script is a thin shell wrapper that downloads the correct VibeWarden
binary for your platform and delegates all commands to it. Commit it to your repo —
it pins the version your team uses, with no global install.

=== "macOS / Linux"

    ```bash
    curl -fsSL https://vibewarden.dev/vibew > vibew && chmod +x vibew
    ```

You can also install that wrapper globally:

```bash
sudo mv vibew /usr/local/bin/vibew
```

---

## Step 2 — `vibew wrap`

Run `vibew wrap` inside your project directory. Pass `--upstream` with the port
your app listens on. Add feature flags for the security plugins you want enabled.

```bash
vibew wrap --upstream 3000 --auth --rate-limit
```

!!! note "If you committed the `vibew` wrapper script"
    The wrapper-script install puts `vibew` in the current directory, not on your
    `PATH`. Run `./vibew wrap --upstream 3000 --auth --rate-limit` instead.

Common flags:

| Flag | Description |
|------|-------------|
| `--upstream <port>` | Port your app listens on (default: auto-detected or 3000) |
| `--auth` | Enable authentication (Ory Kratos) |
| `--rate-limit` | Enable rate limiting |
| `--tls --domain app.yourcompany.com` | Enable TLS (requires `--domain`; use a real domain you control — Let's Encrypt rejects `example.com`) |
| `--force` | Overwrite existing files |

### What `wrap` generates

```
vibewarden.yaml          # Main config — commit this
vibew                    # Wrapper script (macOS/Linux)
vibew.ps1                # Wrapper script (Windows — PowerShell)
vibew.cmd                # Wrapper script (Windows — batch/cmd.exe)
.gitignore               # Created if absent; .vibewarden/ entry appended if missing
AGENTS.md                # AI agent context (created or updated)
AGENTS-VIBEWARDEN.md     # VibeWarden-specific instructions for agents (always overwritten)
```

Wrapper scripts are omitted when `--skip-wrapper` is given. `vibew wrap` does **not** generate `Dockerfile`, `.dockerignore`, or `vibewarden.production.yaml`.

!!! tip "AI agent context"
    `vibew wrap` generates context files for your AI coding assistant. When you
    ask Claude or Cursor to "add a login page," the AI knows to use Kratos flows
    instead of building auth from scratch. Regenerate after config changes with
    `./vibew context refresh`.

---

## Step 2 alt — `vibew init` (for a brand-new project)

Use `vibew init` when you are starting from an empty directory and want
VibeWarden wired up before you write any app code. The typical flow:

```bash
mkdir myapp && cd myapp
vibew init
# ...your AI agent reads AGENTS-VIBEWARDEN.md and writes app code that
#    fits the pre-configured security layer.
```

Common flags:

| Flag | Description |
|------|-------------|
| `--port <port>` | Port your app will listen on (default: `3000`) |
| `--name <name>` | Project name for Docker Compose and image tags (default: directory name) |
| `--describe "<text>"` | One-line project description; written to `PROJECT.md` and injected into agent files |
| `--force` | Overwrite existing files |

### What `init` generates

```
./                                # current directory (you mkdir + cd first)
  vibewarden.yaml                 # Local dev config (TLS self-signed, port 8443)
  vibewarden.production.yaml      # Production overrides (letsencrypt, port 443)
  .gitignore
  PROJECT.md                      # Project description (only when --describe is given)
  AGENTS.md                       # AI agent context (created or updated)
  AGENTS-VIBEWARDEN.md            # VibeWarden-specific instructions for agents (always overwritten)
```

`Dockerfile` and `.dockerignore` are **not** generated by `vibew init`. Write your own Dockerfile after init — `AGENTS-VIBEWARDEN.md` §Dockerfile contract lists the required invariants.

`vibew init` also runs `git init` and creates an initial commit.

!!! note "Environment separation"
    `vibewarden.yaml` is your local dev config. `vibewarden.production.yaml`
    contains production overrides. `vibew bundle` deep-merges the production
    overrides automatically. Never put production-only settings in
    `vibewarden.yaml`.

`init` does **not** generate app source code. It sets up the sidecar
and the agent context files so that your AI assistant can write the app
code inside a project that already has TLS, rate limiting, and the
security posture nailed down.

!!! tip "init vs. wrap"
    - `vibew init` — empty directory, no app code yet.
    - `vibew wrap` — existing source tree. Leaves your code alone and
      adds only the sidecar config.

    If you prefer a browser-driven flow, the prompt generator at
    [vibewarden.dev/start](https://vibewarden.dev/start) produces a
    ready-to-paste prompt that uses `vibew init` under the hood.

After `vibew init`, continue from **Step 3** below.

---

## Step 3 — Build and start

There are two development modes. Choose based on where you are in your workflow.

---

### Mode 1 — First run (just works)

Write a `Dockerfile` before running `vibew dev` (see `AGENTS-VIBEWARDEN.md` §Dockerfile contract for the required invariants). A multi-stage build is recommended for compiled languages — it compiles your app inside Docker so you do not need a local toolchain. Run:

```bash
./vibew dev
```

This command:

1. Runs `vibew generate` to produce `.vibewarden/generated/docker-compose.yml`
   from your `vibewarden.yaml`.
2. Runs `docker build` (multi-stage: compiles your app inside the container).
3. Starts the full stack with `docker compose up`.

Your app is protected at `https://localhost:8443`. Nothing else is required for the
first run.

    !!! tip "Health check after first run"
        Verify the stack is healthy:

        ```bash
        vibew probe
        ```

        `vibew probe` uses Go's TLS stack and works on macOS without LibreSSL issues.
        Expected output ends with `OK — dev stack healthy.` If you see
        `upstream: unknown`, the boot probe has not completed yet — wait 10s and retry
        (vibew probe retries automatically for up to 10s).
        On non-macOS or when non-vibew tooling is needed:
        ```bash
        curl --insecure https://localhost:8443/_vibewarden/health
        ```

!!! warning "If your app image hasn't been built yet"
    `vibew dev` checks for the app Docker image before starting. If the image
    is missing you'll see an error like:

    ```
    app image "myapp:latest" not found in the local Docker daemon.
    Build the image first, then run `vibew dev` again.

    Build steps:
      1. Build your application binary / artifact.
      2. Run `vibew build` to build the Docker image.
    ```

    Run `vibew build` first, then retry `vibew dev`. If something else is
    wrong, `vibew doctor` gives a full diagnostic:

    ```bash
    vibew doctor       # human-readable
    vibew doctor --json  # machine-readable (for AI agents)
    ```

---

### Mode 2 — Iterative development (faster rebuilds)

The multi-stage Docker build recompiles from source every time, which can take
30–60 seconds. For fast iteration, build locally with your language tool and then
package the resulting artifact into a thin Docker image. This build takes only a
few seconds.

**Step 3a — Build the app artifact locally**

=== "Go"

    ```bash
    go build -o bin/myapp ./cmd/myapp
    ```

=== "Gradle (Kotlin / Java)"

    ```bash
    ./gradlew build
    ```

=== "npm (TypeScript / Node.js)"

    ```bash
    npm run build
    ```

**Step 3b — Package into a thin Docker image**

```bash
./vibew build
```

This runs `docker build` using your `Dockerfile`. It copies the pre-built artifact instead of
recompiling, so the image builds in seconds. You can also run
`docker build -t myapp .` directly if you prefer.

**Step 3c — Start or restart the stack**

On first start:

```bash
./vibew dev
```

After subsequent code changes — rebuild the image and restart the stack:

```bash
./vibew build && ./vibew dev
```

---

### When to use which mode

| Situation | Command |
|-----------|---------|
| First run, no local toolchain needed | `vibew dev` |
| Code change, want fast feedback | build locally, then `vibew build && vibew dev` |
| Added a new service or changed `vibewarden.yaml` | `vibew dev` (full recreate) |
| Upgrading from ≤ v0.18.2 — image blocks on first `vibew dev` | `vibew dev --rebuild` |
| Image-identity mismatch error (different project same tag) | `vibew dev --rebuild` |

!!! tip "Trust the self-signed certificate"
    On first run, VibeWarden generates a self-signed CA certificate so your browser
    can open `https://localhost:8443` without TLS errors. Export and trust it with:

    ```bash
    ./vibew cert export > vibewarden-ca.pem
    ```

    Then import `vibewarden-ca.pem` into your browser's or OS's trusted certificate
    store (or pass `--cacert vibewarden-ca.pem` to `curl`).

---

## Stopping the stack

Stop all containers while keeping data volumes (Kratos DB, secrets, etc.):

```bash
vibew down
```

To also remove named volumes and destroy persisted state, pass `-v`:

```bash
vibew down -v
```

---

## What just happened

### The stack

`vibew dev` starts several containers:

| Container | Purpose |
|-----------|---------|
| `vibewarden` | The security sidecar — Caddy embedding all middleware |
| `kratos` | Identity server (only when `auth.mode: kratos`) |
| `kratos-db` | Postgres for Kratos (only when `auth.mode: kratos` and no external DB) |
| `openbao` | Secrets manager (only when `secrets.enabled: true` and `secrets.store: openbao`) |

Your app runs outside Docker and is reached from the container network via
`host.docker.internal`. Alternatively, set `app.build` or `app.image` in
`vibewarden.yaml` to include your app in the Compose stack.

### The middleware chain

Every inbound request passes through this ordered chain before reaching your app:

```
Request
   │
   ▼
 IP filter (if enabled)
   │
   ▼
 Rate limiter — per-IP token bucket
   │
   ▼
 Body size limit
   │
   ▼
 WAF — SQLi / XSS / path traversal detection (enabled by default in `detect` mode)
   │
   ▼
 Authentication — JWT / Kratos / API key
   │
   ▼
 Rate limiter — per-user token bucket
   │
   ▼
 Secret injection into request headers
   │
   ▼
 Upstream (your app)
   │
   ▼
 Security headers added to response
   │
   ▼
 Audit log event emitted
   │
   ▼
Response
```

### Generated files

Runtime files land under `.vibewarden/generated/` (add this to `.gitignore`):

```
.vibewarden/generated/
  docker-compose.yml           # Full stack
  kratos/kratos.yml            # Ory Kratos config
  kratos/identity.schema.json  # Identity schema
  observability/               # Grafana/Prometheus/Loki (always generated; activate with vibew obs up)
```

Do not edit generated files. Re-run `vibew generate` after changing
`vibewarden.yaml`.

---

## Next steps

### Enable authentication

The default JWT mode works with any OIDC provider. Edit `vibewarden.yaml`:

```yaml
auth:
  mode: jwt
  jwt:
    jwks_url: "https://your-provider/.well-known/jwks.json"
    issuer:   "https://your-provider/"
    audience: "your-api-identifier"
  public_paths:
    - /static/*
    - /health
```

See the [Identity Providers guide](identity-providers.md) for step-by-step
examples with Auth0, Keycloak, Firebase, Cognito, Okta, Supabase, and Kratos.

### Add observability

```bash
./vibew add metrics
./vibew build
./vibew dev
./vibew obs up
```

Open Grafana at `http://localhost:3001` to see request rate, latency percentiles,
rate limit hits, and auth decisions in real time.

!!! tip "Generate a dev JWT"
    Use `vibew token` to mint a signed JWT for local testing without an external
    OIDC provider:

    ```bash
    curl https://localhost:8443/api/me \
      --cacert vibewarden-ca.pem \
      -H "Authorization: Bearer $(./vibew token --json)"
    ```

See the [Observability guide](observability.md) for details.

### Enable TLS for production

```bash
./vibew add tls --domain app.yourcompany.com --email ops@yourcompany.com
```

This sets `tls.provider: letsencrypt` and `tls.domain` in `vibewarden.yaml` and
writes the following fields to `vibewarden.production.yaml`:

| Field | Value | Purpose |
|-------|-------|---------|
| `profile` | `prod` | Marks the file as a production override |
| `server.port` | `443` | Standard HTTPS port |
| `deploy.target_platform` | `linux/amd64` | Prevents bracketed arch placeholder in `vibew bundle` output |
| `tls.domain` | the `--domain` value | Real domain for Let's Encrypt and healthcheck URL |
| `tls.provider` | `letsencrypt` | ACME provider (auto-derived for public domains) |

After running `vibew add tls`, set `deploy.host` in `vibewarden.production.yaml`
before running `vibew bundle`. Without it, `vibew bundle` prints SSH commands with
`<your-ssh-user>@<your-ssh-host>` bracketed placeholders — copy-pasting them
verbatim fails:

```yaml
# vibewarden.production.yaml  (edit after vibew add tls)
deploy:
  # Replace placeholders with your real SSH target before vibew bundle.
  target_platform: linux/amd64
  host: <your-ssh-user>@<your-server-ip>
```

When `deploy.host` is set, `vibew bundle` substitutes it into all three SSH
command lines, producing output that is ready to run without any edits.

See the [Production Deployment guide](production-deployment.md) for the full
production checklist.

### Pre-deploy validation

Before bundling, run the preflight check against your production environment:

```bash
./vibew doctor --preflight production
```

This reads `vibewarden.production.yaml`, merges it with `vibewarden.yaml`, and
runs the standard static checks plus five additional pre-deploy checks:

1. DNS resolves `tls.domain`
2. `server.port` is 443
3. `deploy.target_platform` is set
4. App image architecture matches `deploy.target_platform`
5. `tls.email` is configured for Let's Encrypt

Exit code is `1` only when a FAIL-severity check is encountered. Of the five preflight
checks, P3 (`deploy.target_platform` unset) and P4 (image arch mismatch) are
FAIL-severity; P1 (DNS), P2 (port 443), and P5 (TLS email) are WARN-only and do not
produce exit 1. The env file (`vibewarden.production.yaml`) must exist — if it is
missing, doctor exits 1 immediately without running any checks.

Run `vibew doctor --preflight production` before `vibew bundle` to surface DNS,
port, architecture, and TLS-email issues before a multi-minute build attempt.

Then produce the deployment bundle:

```bash
./vibew bundle
```

The bundle is written to `.vibewarden/bundle/`. When `deploy.host` is set in
`vibewarden.production.yaml`, the "Next: deploy" block printed to stdout contains the
exact `ssh` + `tar` + `docker compose` commands ready to run. When `deploy.host` is
unset (the default at this point in setup), the block prints bracketed placeholders
(`<your-ssh-user>@<your-ssh-host>`) with a configuration hint — substitute the values
before running.

For one-off deploys or CI pipelines where you want to supply the SSH target without
setting `deploy.host` in config, use `--print-deploy`:

```bash
./vibew bundle --print-deploy --host <your-ssh-host> --user <your-ssh-user> --path /opt/myapp
```

All three sub-flags (`--host`, `--user`, `--path`) are required when `--print-deploy`
is set. This overrides `deploy.host` from `vibewarden.production.yaml` for the printed
block only — the bundle README and all bundle files are unaffected.

See [Pre-deploy preflight](troubleshooting.md#pre-deploy-preflight) for the full preflight check reference.

### Validate your config

```bash
./vibew validate
```

Reports all validation errors in `vibewarden.yaml` before you start the stack.

---

## Troubleshooting

### Port already in use

VibeWarden defaults to port `8443`. Change it in `vibewarden.yaml`:

```yaml
server:
  port: 9443
```

### App not reachable

If your app does not run inside Docker, verify it is listening on `0.0.0.0`
(not `127.0.0.1`), or override the upstream host:

```yaml
upstream:
  host: host.docker.internal
  port: 3000
```

### Containers not starting

```bash
# Check container health
./vibew status

# Show detailed logs for all containers
./vibew logs

# Diagnose common issues automatically
./vibew doctor
```
