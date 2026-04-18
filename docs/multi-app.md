# Multi-App Deployment

Deploy multiple apps to a single VM, each on its own subdomain, behind one
shared VibeWarden sidecar. One VM, multiple apps, independent TLS certs.

---

## When to use this

- **Demo hosting** -- deploy `app1.demo.example.com` and `app2.demo.example.com`
  to one Hetzner VM instead of two.
- **Early-stage startup** -- run 2-3 microservices on one box while budget is
  tight; split to dedicated VMs later.
- **Side projects** -- one VPS, multiple projects, each with full security.

---

## How it works

Each app keeps its own `vibewarden.yaml` -- the same file you use locally with
`vibew dev`. When you run `vibew deploy`, the CLI detects whether the target
already has a VibeWarden sidecar:

- **First deploy with a `tls.domain`**: creates the sidecar + the first site.
- **Subsequent deploys**: adds the new app alongside existing ones.

There is no config merging. Each app's `vibewarden.yaml` is copied as-is to a
per-site directory on the server. The sidecar reads all site configs and
generates one Caddy route block per app, with host-based routing.

```
                    +----------------------------------+
  app1.example.com -->|                                  |--> app1 container :3000
  app2.example.com -->|  Single VibeWarden sidecar       |--> app2 container :8080
  app3.example.com -->|  TLS + routing + per-app config  |--> app3 container :3000
                    +----------------------------------+
```

---

## Server-side directory layout

```
~/vibewarden/
  .sidecar/
    global.yaml              # VM-wide settings (listen port, log level, ACME email)
    docker-compose.yml       # sidecar container
  sites/
    blog/
      vibewarden.yaml        # same file as local project
      docker-compose.yml     # per-app container (generated)
    api/
      vibewarden.yaml
      docker-compose.yml
```

- `global.yaml` is generated automatically on the first multi-app deploy.
- Each `sites/<name>/vibewarden.yaml` is your project's config, copied verbatim.
- Each `sites/<name>/docker-compose.yml` is generated from your config.

---

## Quick start

### Prerequisites

- A VM with Docker installed (see [Deploy to VPS](deploy-to-vps.md) steps 1-3)
- DNS A records pointing each subdomain to the VM's IP
- `vibew` CLI installed locally

### 1. Deploy the first app

From your first project's directory:

```bash
vibew deploy \
  --target ssh://root@203.0.113.10 \
  --config vibewarden.yaml
```

Your `vibewarden.yaml` must have `tls.domain` set. This is what triggers
multi-app mode:

```yaml
# vibewarden.yaml (blog project)
app:
  image: ghcr.io/your-org/blog:latest

upstream:
  host: app
  port: 3000

tls:
  enabled: true
  provider: letsencrypt
  domain: "blog.example.com"

auth:
  mode: jwt
  jwt:
    jwks_url: "https://your-idp/.well-known/jwks.json"
    issuer: "https://your-idp/"
    audience: "https://api.example.com"

rate_limit:
  enabled: true

waf:
  enabled: true
```

Expected output:

```
Creating multi-app directory layout...
Creating shared Docker network...
Writing global.yaml...
Writing sidecar docker-compose.yml...
Deploying site "blog"...
Starting sidecar...
Waiting for sidecar health check at http://localhost:443/_vibewarden/health (via SSH)...
Bootstrap complete.
```

### 2. Deploy a second app

From your second project's directory:

```bash
vibew deploy \
  --target ssh://root@203.0.113.10 \
  --config vibewarden.yaml
```

This time the CLI detects the existing sidecar and adds the new site:

```
Deploying site "api" to existing sidecar...
Restarting sidecar to load new site...
Waiting for sidecar health check at http://localhost:443/_vibewarden/health (via SSH)...
Site deployed.
```

### 3. Verify both apps

```bash
curl https://blog.example.com/_vibewarden/health
# {"status":"healthy"}

curl https://api.example.com/_vibewarden/health
# {"status":"healthy"}
```

---

## Per-site configuration

Each app's `vibewarden.yaml` is independent and self-contained. The file on
the server is identical to the file in your local project. Each site can have
its own:

- Auth mode and settings
- Rate limiting configuration
- WAF mode (`block` or `detect`)
- Security headers
- Upstream host and port

Changing one site's config does not affect other sites.

---

## Hot reload

The sidecar watches `sites/*/vibewarden.yaml` for changes. When a config file
is added, modified, or removed, the sidecar reloads the affected site's Caddy
routes without dropping connections to other sites.

Changes are debounced (500ms per site) to handle editors that write files in
multiple steps.

---

## Error isolation

A broken config in one site does not take down other sites:

- If `sites/api/vibewarden.yaml` has a syntax error, the `api` site is marked
  as errored. The `blog` site continues serving normally.
- The errored site appears with `Error` status in `vibew deploy status`.
- Fix the config file and the sidecar automatically reloads the corrected site.

---

## Managing sites

### Check status of all sites

```bash
vibew deploy status --target ssh://root@203.0.113.10
```

Output:

```
=== Sidecar ===
NAME              STATUS
vibewarden        Up (healthy)

=== Site: api ===
NAME                    STATUS
vibewarden-api-app      Up (healthy)

=== Site: blog ===
NAME                    STATUS
vibewarden-blog-app     Up (healthy)
```

### Check status of a single site

```bash
vibew deploy status --target ssh://root@203.0.113.10 --app blog
```

### View sidecar logs

```bash
vibew deploy logs --target ssh://root@203.0.113.10
```

### View a specific site's logs

```bash
vibew deploy logs --target ssh://root@203.0.113.10 --app blog --follow
```

Press Ctrl-C to stop streaming.

---

## TLS

Caddy auto-provisions a TLS certificate for each site's domain via ACME
(Let's Encrypt). Each site can use a different TLS provider:

```yaml
# Site A: Let's Encrypt
tls:
  enabled: true
  provider: letsencrypt
  domain: "blog.example.com"

# Site B: self-signed (for staging)
tls:
  enabled: true
  provider: self-signed
  domain: "staging.example.com"
```

DNS A records for all subdomains must point to the VM's IP before deploying.

---

## Global configuration

The `global.yaml` file is generated automatically. It controls VM-wide settings:

| Field | Default | Description |
|-------|---------|-------------|
| `listen_port` | `443` | Port the sidecar binds to for HTTPS |
| `log_level` | `info` | Sidecar log verbosity (`debug`, `info`, `warn`, `error`) |
| `admin_token` | (empty) | Bearer token for the sidecar admin API |
| `acme_email` | (empty) | Email for ACME certificate registration |
| `listen_host` | `0.0.0.0` | Address the sidecar binds to |

To customize, edit `~/vibewarden/.sidecar/global.yaml` on the server:

```yaml
# global.yaml
listen_port: 443
log_level: info
acme_email: "ops@example.com"
```

---

## Limitations

These are planned for future phases:

- **Shared Kratos** -- each site runs its own auth config. A shared Kratos
  instance across sites is deferred to Phase 2.
- **Migration tooling** -- no automated conversion from a single-app deployment
  to multi-app layout. Phase 3.
- **App removal** -- no `vibew deploy remove` command yet. To remove a site
  manually, delete its directory (`rm -rf ~/vibewarden/sites/<name>`) and
  restart the sidecar.
- **Local multi-app** -- `vibew dev` remains single-app. Multi-app is
  deploy-time only.

---

## Related

- [Deploy to VPS](deploy-to-vps.md) -- single-app deploy walkthrough
- [Deploy Reference](deploy-reference.md) -- full flag reference for all
  `vibew deploy` commands
- [Configuration](configuration.md) -- `vibewarden.yaml` field reference
