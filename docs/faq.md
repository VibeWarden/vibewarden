# FAQ

## Is VibeWarden only for "vibe-coded" apps?

No. VibeWarden is a production-grade security sidecar that works with any HTTP application — Spring Boot, Django, Rails, Express, FastAPI, Next.js, or anything else that listens on a port.

We say "vibe-coded" because that's where the pain is sharpest: developers shipping fast with AI tools often skip security entirely. But the features themselves — TLS termination, JWT validation, rate limiting, WAF, egress control, audit logging — are the same ones you'd want in front of any application.

If it's good enough for an app built in an afternoon with an AI assistant, it's more than good enough for your carefully architected production service.

## Does it replace nginx / Traefik / Caddy?

VibeWarden embeds Caddy as its reverse proxy engine. It replaces the manual configuration of nginx or Traefik with a single `vibewarden.yaml` file that handles TLS, auth, rate limiting, WAF, and security headers together. You don't need to stitch together separate tools.

## Does it work with my existing identity provider?

Yes. VibeWarden supports JWT/OIDC validation from any standard provider — Auth0, Keycloak, Firebase, Cognito, Okta, Supabase, or your own. It also supports Ory Kratos for self-hosted identity and simple API key auth for machine-to-machine communication. See the [identity providers guide](identity-providers.md).

## Do I need to change my application code?

Not for ingress (inbound traffic). VibeWarden sits in front of your app and handles security transparently. Your app receives clean, authenticated requests with user identity in headers like `X-User-Id`.

For egress (outbound traffic), your app points its HTTP calls at the egress proxy (`localhost:8081`) instead of directly at external APIs. This is the only code change, and it's optional.

## Is it production-ready?

VibeWarden is pre-1.0. The core features (TLS, auth, rate limiting, security headers, WAF, egress proxy) are stable and tested, but the API may evolve. We recommend it for new projects and non-critical production workloads. Pin your version via `.vibewarden-version` to avoid surprises.

## What's the performance overhead?

Under 3 microseconds per request with all middleware enabled (security headers + rate limiting + WAF). See the [performance benchmarks](performance.md).

## Is it free?

The open-source core is Apache 2.0 — free forever. A Pro tier (fleet dashboard for managing multiple instances) is planned but not yet available.

## Can I use it in Kubernetes?

Yes, as a sidecar container in the same pod as your application. A Helm chart is planned. For now, deploy the Docker image directly.

## Where do I report security vulnerabilities?

See [SECURITY.md](https://github.com/vibewarden/vibewarden/blob/main/SECURITY.md). Use GitHub Security Advisories or email security@vibewarden.dev.
