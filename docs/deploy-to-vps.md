# Deploy to VPS

This guide walks you through deploying a VibeWarden-protected application to a
fresh VPS — from DNS setup through a running HTTPS endpoint. The commands are
copy-pasteable. Replace `demo.yourdomain.com` and `<server-ip>` with your own
values throughout.

VibeWarden is a sidecar. It always runs on the same machine as your app. You are
not deploying VibeWarden to a remote server — you are deploying your whole stack
(app + sidecar) together.

---

## Prerequisites

| Requirement | Details |
|---|---|
| VPS | Ubuntu 24.04 LTS, 2 vCPU, 4 GB RAM (Hetzner CX23 or equivalent) |
| Local machine | macOS or Linux with `ssh`, `dig`, `curl` installed |
| Domain name | A registered domain you control |
| SSH key | `~/.ssh/id_ed25519` (or RSA) already added to the VPS |

!!! note "Why Hetzner CX23?"
    The CX23 (2 vCPU, 4 GB RAM, 40 GB NVMe) is the minimum comfortable size for
    the full stack: your app + VibeWarden + Ory Kratos + Postgres. At roughly
    €5–6/month it is the most cost-effective option for a production workload in
    the EU.

---

## Step 1 — Provision the server

### Create the VPS

In the Hetzner Cloud console:

1. Choose **CX23** (x86, Ubuntu 24.04 LTS).
2. Add your SSH public key.
3. Enable the built-in **Hetzner Firewall** with these rules:

| Direction | Protocol | Port | Source |
|---|---|---|---|
| Inbound | TCP | 22 | Your IP (or `0.0.0.0/0` if dynamic) |
| Inbound | TCP | 80 | `0.0.0.0/0` — required for Let's Encrypt ACME |
| Inbound | TCP | 443 | `0.0.0.0/0` |
| Outbound | Any | Any | `0.0.0.0/0` |

Note the public IP address — you will need it in the next step.

### Verify SSH access

```bash
ssh root@<server-ip> echo "OK"
```

---

## Step 2 — Point DNS

Add an **A record** at your DNS provider:

| Type | Name | Value | TTL |
|---|---|---|---|
| A | `demo` | `<server-ip>` | 300 |

This creates `demo.yourdomain.com → <server-ip>`.

Wait for DNS to propagate, then verify:

```bash
dig +short demo.yourdomain.com
# must return <server-ip> before you continue
```

!!! warning "DNS must resolve before first start"
    Let's Encrypt's ACME HTTP-01 challenge connects to your domain on port 80 to
    verify ownership. If DNS is not yet pointing at your server, certificate
    issuance will fail. Wait until `dig` returns the correct IP.

---

## Step 3 — Install Docker on the server

```bash
ssh root@<server-ip> 'curl -fsSL https://get.docker.com | sh'
```

Verify:

```bash
ssh root@<server-ip> 'docker --version && docker compose version'
# Docker version 27.x.x
# Docker Compose version v2.x.x
```

---

## Step 4 — Set up your project locally

### Install `vibew`

```bash
curl -fsSL https://vibewarden.dev/vibew > vibew && chmod +x vibew
```

### Initialize a new project (or wrap an existing one)

**New project:**

```bash
mkdir myapp && cd myapp
./vibew init myapp --upstream 3000 --auth --rate-limit
```

**Existing app:**

```bash
cd /path/to/your/app
./vibew wrap --upstream 3000 --auth --rate-limit
```

Both commands generate `vibewarden.yaml`, a `Dockerfile`, and AI agent context
files. Commit `vibewarden.yaml` and `Dockerfile` to version control. Do not commit
`.env` files.

---

## Step 5 — Create the production config

Create `vibewarden.prod.yaml` in your project root (see the annotated example
at `examples/vibewarden.prod.yaml` in this repository):

```yaml
# vibewarden.prod.yaml
profile: prod

app:
  image: ghcr.io/your-org/myapp:latest   # your production image

server:
  host: "0.0.0.0"
  port: 443

upstream:
  host: app
  port: 3000

tls:
  enabled: true
  provider: letsencrypt
  domain: "demo.yourdomain.com"           # must match your DNS A record

auth:
  enabled: true
  mode: jwt
  jwt:
    jwks_url: "https://your-idp/.well-known/jwks.json"
    issuer:   "https://your-idp/"
    audience: "https://api.yourdomain.com"
  public_paths:
    - /health
    - /static/*

rate_limit:
  enabled: true
  store: memory
  per_ip:
    requests_per_second: 20
    burst: 40
  per_user:
    requests_per_second: 200
    burst: 400

waf:
  enabled: true
  mode: block

security_headers:
  enabled: true
  hsts_max_age: 31536000
  hsts_include_subdomains: true
  content_type_nosniff: true
  frame_option: "DENY"
  content_security_policy: "default-src 'self'; style-src 'self' 'unsafe-inline'"
  referrer_policy: "strict-origin-when-cross-origin"

log:
  level: "info"
  format: "json"

admin:
  enabled: true
  token: "${VIBEWARDEN_ADMIN_TOKEN}"
```

Generate the Docker Compose stack from this config:

```bash
./vibew generate --config vibewarden.prod.yaml
```

This writes `.vibewarden/generated/docker-compose.yml` (do not edit — regenerate
after config changes).

---

## Step 6 — Deploy

### Copy files to the server

```bash
rsync -av \
  vibewarden.prod.yaml \
  .vibewarden/generated/docker-compose.yml \
  root@<server-ip>:/opt/myapp/
```

Create the `.env` file on the server:

```bash
ssh root@<server-ip> bash -s <<'EOF'
mkdir -p /opt/myapp
cat > /opt/myapp/.env <<ENV
VIBEWARDEN_ADMIN_TOKEN=$(openssl rand -hex 32)
ENV
chmod 600 /opt/myapp/.env
EOF
```

### Start the stack

```bash
ssh root@<server-ip> 'cd /opt/myapp && docker compose -f docker-compose.yml up -d'
```

Docker pulls the VibeWarden image and your app image, then starts the stack.
On first boot Caddy contacts Let's Encrypt and obtains a certificate — this
takes up to 30 seconds.

---

## Step 7 — Verify

Check container health:

```bash
ssh root@<server-ip> 'cd /opt/myapp && docker compose ps'
```

Expected output (all healthy):

```
NAME                    STATUS
myapp-vibewarden        Up (healthy)
myapp-app               Up (healthy)
```

Hit the health endpoint:

```bash
curl -I https://demo.yourdomain.com/_vibewarden/healthz
# HTTP/2 200
```

Open your browser and navigate to `https://demo.yourdomain.com`. You should see
your app served over HTTPS with a valid Let's Encrypt certificate.

Check logs if anything looks wrong:

```bash
ssh root@<server-ip> 'cd /opt/myapp && docker compose logs -f --tail 50'
```

---

## Step 8 — Subsequent deploys

After changing config or code:

1. Rebuild and push your app image:
   ```bash
   docker build -t ghcr.io/your-org/myapp:latest .
   docker push ghcr.io/your-org/myapp:latest
   ```

2. If `vibewarden.prod.yaml` changed, regenerate:
   ```bash
   ./vibew generate --config vibewarden.prod.yaml
   rsync -av .vibewarden/generated/docker-compose.yml root@<server-ip>:/opt/myapp/
   ```

3. Pull and restart on the server:
   ```bash
   ssh root@<server-ip> 'cd /opt/myapp && docker compose pull && docker compose up -d'
   ```

Zero-downtime: Docker Compose replaces containers one at a time and waits for
health checks to pass before stopping the old container.

---

## Troubleshooting

### DNS not propagated

```
Error: error obtaining certificate: ...no such host...
```

Run `dig +short demo.yourdomain.com` from your local machine. If it does not
return `<server-ip>`, wait and try again. TTL of 300 seconds means propagation
takes up to 5 minutes.

### Port 80 blocked by firewall

```
Error: failed to connect to the ACME server on port 80
```

Check your Hetzner firewall rules: inbound TCP port 80 from `0.0.0.0/0` must be
allowed. Even if your app only serves HTTPS, Let's Encrypt needs port 80 open
for the HTTP-01 challenge.

Verify from your local machine:

```bash
curl -v http://demo.yourdomain.com/_vibewarden/healthz
```

If the connection is refused or times out, the firewall is blocking port 80.

### Docker not running

```bash
ssh root@<server-ip> 'systemctl status docker'
# if inactive:
ssh root@<server-ip> 'systemctl start docker && systemctl enable docker'
```

### Let's Encrypt rate limits

Let's Encrypt allows 5 failed certificate requests per hour per domain. If you
exceeded this limit you will see:

```
Error: too many certificates already issued for ...
```

Wait at least one hour before retrying. In the meantime, use
`tls.provider: self-signed` to test the rest of the stack.

### Container fails to start

```bash
ssh root@<server-ip> 'cd /opt/myapp && docker compose logs vibewarden --tail 100'
```

Look for `level=error` lines. The most common causes are:

| Error message | Fix |
|---|---|
| `cannot bind to port 443: permission denied` | Run as root or use `setcap cap_net_bind_service` on the Docker binary |
| `upstream connect error` | Verify the `upstream.host` and `upstream.port` match your app container's name and port |
| `config validation failed` | Run `./vibew validate --config vibewarden.prod.yaml` locally |

### Inspect the full doctor output

```bash
ssh root@<server-ip> 'cd /opt/myapp && docker run --rm \
  -v /opt/myapp/vibewarden.prod.yaml:/vibewarden.yaml:ro \
  ghcr.io/vibewarden/vibewarden:latest \
  vibew doctor --json'
```

---

## Next steps

- **Production hardening**: see [Production Hardening Checklist](production-hardening.md)
  for a full checklist of security settings to review before going live.
- **Backups**: see [Production Deployment](production-deployment.md) for Postgres
  backup scripts and TLS certificate backup procedures.
- **Observability**: enable Prometheus metrics and Grafana dashboards —
  see [Observability](observability.md).
- **Fleet dashboard** (Pro): connect multiple instances to the VibeWarden fleet
  dashboard at `app.vibewarden.dev` for aggregated logs and metrics.
