# VibeWarden Security Challenge

Want to put VibeWarden through its paces? We encourage security-minded people to
probe the demo app and try to find gaps in the sidecar's defences. This is the
best way to harden the project — and we genuinely enjoy hearing about what you
find.

## What to attack

The demo app is an intentionally minimal Go server that trusts VibeWarden
completely. The interesting surface is the **sidecar**, not the app behind it.

Things we want you to try:

| Attack class | What to look for |
|---|---|
| **Auth bypass** | Can you reach a protected endpoint without a valid Kratos session? Can you forge or replay session cookies? |
| **Rate limiting** | Can you exceed the configured request budget without triggering a 429? Any per-IP bypass (header spoofing, IP rotation via local proxy, chunked requests)? |
| **Header forging** | Can a client inject `X-User-Id`, `X-User-Email`, or `X-User-Verified` so that the app trusts a fake identity? |
| **Security header gaps** | Are any of the expected response headers (`Strict-Transport-Security`, `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`, `Referrer-Policy`) missing on any endpoint or response code? |
| **Misconfiguration** | Does any combination of `vibewarden.yaml` settings leave a door open that should be closed? |

## What is out of scope

Please do not:

- **DDoS the demo VM or infrastructure** — bandwidth costs money and
  disrupts other users. Rate-limit bypass testing should stay local (`docker compose up`).
- **Attack the VM operating system, SSH daemon, or host networking** — we care
  about the sidecar, not the underlying server.
- **Attack Grafana, Prometheus, or Loki** — those are not part of VibeWarden's
  security surface. Report those to their respective projects instead.
- **DNS hijacking or BGP manipulation** — out of scope for a localhost sidecar.
- **Social engineering** — phishing, pretexting, or impersonating team members.

If you are unsure whether something is in scope, run it locally and open a
discussion issue before touching shared infrastructure.

## This is not a bug bounty

VibeWarden does not currently offer financial rewards for security findings. We
are an open-source project run by a solo founder. What we do offer:

- Public credit in [`ACKNOWLEDGMENTS.md`](../../ACKNOWLEDGMENTS.md) and in the
  release notes for any fix (with your permission).
- Our sincere gratitude and a mention on the challenge page at
  [vibewarden.dev](https://vibewarden.dev) once it launches.

## Found something?

Please follow the responsible disclosure process in
[`SECURITY.md`](../../SECURITY.md):

- **Critical findings** (auth bypass, RCE, privilege escalation, data exposure)
  — email **security@vibewarden.dev** privately. Do not open a public issue
  until a fix is released.
- **Lower-severity findings** (hardening improvements, missing headers, minor
  information leaks) — open a public GitHub issue or use
  [GitHub private vulnerability reporting](https://github.com/vibewarden/vibewarden/security/advisories/new).

We aim to acknowledge every report within 48 hours.

## Running the demo locally

The safest way to probe VibeWarden is against your own local instance:

```bash
cd examples/demo-app
docker compose up -d
# VibeWarden is now listening on https://localhost:8443
```

See [`README.md`](README.md) for the full list of endpoints and what each one
demonstrates.

Happy hacking — and thank you for helping make VibeWarden more secure.
