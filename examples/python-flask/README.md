# VibeWarden — Python / Flask Example

A minimal Flask app with `/health`, `/public`, and `/protected` endpoints,
secured by the VibeWarden sidecar.

The app itself performs no authentication. VibeWarden sits in front, enforces
rate limiting and security headers, and forwards user identity via `X-User-*`
headers when auth is enabled.

## Quick start

Step 1 — move into this directory:

```bash
cd examples/python-flask
```

Step 2 — generate the runtime stack from `vibewarden.yaml`:

```bash
vibew generate
```

Step 3 — start everything:

```bash
vibew dev
```

Visit http://localhost:8080. VibeWarden is now protecting your Flask app.

## Endpoints

| Method | Path | Auth required | Description |
|--------|------|---------------|-------------|
| GET | `/health` | No | Liveness probe — returns `{"status":"ok"}` |
| GET | `/public` | No | Public data with a timestamp |
| GET | `/protected` | Yes (when auth enabled) | Echoes `X-User-Id` and `X-User-Email` headers |

## Architecture

```
curl / browser
      |
      | :8080
      v
+------------------+
|   VibeWarden     |  rate limiting, security headers, optional auth
+------------------+
      |
      | :5000 (internal)
      v
+------------------+
|   Flask app      |  your code — trusts sidecar headers
+------------------+
```

## Enabling JWT auth

Edit `vibewarden.yaml` and change the `auth` block:

```yaml
auth:
  mode: jwt
  jwt:
    jwks_url: "https://your-provider/.well-known/jwks.json"
    issuer:   "https://your-provider/"
    audience: "your-api-identifier"
  public_paths:
    - /health
    - /public
```

Then run `vibew generate` again. Requests to `/protected` without a valid JWT
will receive a `401 Unauthorized` response from VibeWarden before reaching the
Flask app.

## Development without VibeWarden

The app runs standalone on port 5000:

```bash
pip install -r requirements.txt
python app.py
```
