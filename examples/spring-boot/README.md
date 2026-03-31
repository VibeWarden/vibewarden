# VibeWarden + Spring Boot

Minimal Spring Boot REST API behind VibeWarden.

## Quick start

```bash
cd examples/spring-boot
vibew dev
# Open https://localhost:8443
```

VibeWarden adds HTTPS (self-signed), rate limiting, and security headers — the Spring Boot app just serves JSON.

## Endpoints

| Path | Method | Description |
|------|--------|-------------|
| `/` | GET | Welcome message |
| `/health` | GET | Health check |
| `/public` | GET | Public data |
| `/protected` | GET | Protected (reads X-User-Id header) |
| `/headers` | GET | Echo all request headers |

## Architecture

```
Internet → VibeWarden :8443 (HTTPS) → Spring Boot :3000 (HTTP)
           TLS, rate limiting,         Plain Java app,
           security headers            no security code needed
```

## Upgrading to JWT auth

```yaml
auth:
  enabled: true
  mode: jwt
  jwt:
    jwks_url: https://your-idp.com/.well-known/jwks.json
    issuer: https://your-idp.com
    audience: your-app
    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
```

Then read `X-User-Id` and `X-User-Email` from request headers in your Spring controllers — no Spring Security needed.
