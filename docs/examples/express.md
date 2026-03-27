# VibeWarden + Express / Node.js

This guide shows how to put VibeWarden in front of an Express (Node.js) application
running in Docker Compose. VibeWarden handles TLS, authentication (Ory Kratos), rate
limiting, and security headers. Express receives only authenticated, validated requests
with identity injected as HTTP headers.

---

## Architecture

```
Internet
  │
  ▼ :443 (HTTPS)
VibeWarden (Caddy embedded)
  │  validates session cookie → Kratos
  │  injects X-User-* headers
  ▼ :3000 (HTTP, internal)
Express app
```

Your Express app listens on port 3000 on the internal Docker network. It is never
directly reachable from the internet — VibeWarden is the sole entry point.

---

## vibewarden.yaml

```yaml
server:
  host: "0.0.0.0"
  port: 443

upstream:
  host: "express"   # Docker Compose service name
  port: 3000

tls:
  enabled: true
  provider: letsencrypt
  domain: "myapp.example.com"

kratos:
  public_url: "http://kratos:4433"
  admin_url:  "http://kratos:4434"

auth:
  public_paths:
    - "/login"
    - "/register"
    - "/recovery"
    - "/verification"
    - "/public/*"
    - "/static/*"
    - "/favicon.ico"
    - "/robots.txt"
  session_cookie_name: "ory_kratos_session"
  login_url: "/login"

body_size:
  max: "10MB"
  overrides:
    - path: /api/upload
      max: "50MB"

rate_limit:
  enabled: true
  per_ip:
    requests_per_second: 20
    burst: 40
  per_user:
    requests_per_second: 200
    burst: 400
  trust_proxy_headers: false

log:
  level: "info"
  format: "json"

admin:
  enabled: true
  token: ""   # set via VIBEWARDEN_ADMIN_TOKEN env var

metrics:
  enabled: true
  path_patterns:
    - "/api/:resource"
    - "/api/:resource/:id"
    - "/api/v1/:resource"
    - "/api/v1/:resource/:id"

security_headers:
  enabled: true
  hsts_max_age: 31536000
  hsts_include_subdomains: true
  content_type_nosniff: true
  frame_option: "DENY"
  content_security_policy: "default-src 'self'"
  referrer_policy: "strict-origin-when-cross-origin"
  cross_origin_opener_policy: "same-origin"
  cross_origin_resource_policy: "same-origin"
  suppress_via_header: true
```

---

## docker-compose.yml

```yaml
services:
  postgres:
    image: postgres:17-alpine
    container_name: myapp-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER:     ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB:       ${POSTGRES_DB}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - myapp
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 5s
      timeout: 5s
      retries: 10

  kratos-migrate:
    image: oryd/kratos:v1.3.1
    container_name: myapp-kratos-migrate
    restart: on-failure
    environment:
      DSN: postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
    volumes:
      - ./config/kratos:/etc/config/kratos:ro
    command: migrate sql -e --yes
    depends_on:
      postgres:
        condition: service_healthy
    networks:
      - myapp

  kratos:
    image: oryd/kratos:v1.3.1
    container_name: myapp-kratos
    restart: unless-stopped
    environment:
      DSN: postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
      SERVE_PUBLIC_BASE_URL: https://${DOMAIN}/
      SERVE_ADMIN_BASE_URL: http://kratos:4434/
    volumes:
      - ./config/kratos:/etc/config/kratos:ro
    command: serve --config /etc/config/kratos/kratos.yml --watch-courier
    depends_on:
      kratos-migrate:
        condition: service_completed_successfully
    networks:
      - myapp
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://kratos:4434/admin/health/ready"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 10s

  vibewarden:
    image: ghcr.io/vibewarden/vibewarden:latest
    container_name: myapp-vibewarden
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./vibewarden.yaml:/vibewarden.yaml:ro
      - caddy_data:/root/.local/share/caddy
    environment:
      VIBEWARDEN_KRATOS_PUBLIC_URL: http://kratos:4433
      VIBEWARDEN_KRATOS_ADMIN_URL:  http://kratos:4434
      VIBEWARDEN_UPSTREAM_HOST:     express
      VIBEWARDEN_UPSTREAM_PORT:     "3000"
      VIBEWARDEN_SERVER_HOST:       "0.0.0.0"
      VIBEWARDEN_ADMIN_TOKEN:       ${VIBEWARDEN_ADMIN_TOKEN}
    depends_on:
      kratos:
        condition: service_healthy
      express:
        condition: service_healthy
    networks:
      - myapp

  express:
    image: your-registry/your-express-app:latest
    container_name: myapp-express
    restart: unless-stopped
    environment:
      NODE_ENV: production
      PORT: "3000"
    expose:
      - "3000"
    networks:
      - myapp
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:3000/health"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
  caddy_data:

networks:
  myapp:
    driver: bridge
```

---

## Reading X-User-* headers in Express

VibeWarden injects identity headers on every authenticated request before forwarding
to the upstream. Your Express app reads these headers to identify the current user
without making any additional auth calls.

### Available headers

| Header | Description |
|---|---|
| `X-User-ID` | Kratos identity UUID |
| `X-User-Email` | Primary email address from the identity traits |
| `X-User-Verified` | `"true"` if the email address has been verified |
| `X-Session-ID` | Kratos session UUID |

### Middleware to extract user identity

Create a middleware that reads the VibeWarden-injected headers and attaches the user
to `req.user`:

```javascript
// middleware/vibewarden.js

/**
 * Reads the X-User-* headers injected by VibeWarden and attaches
 * a user object to req.user. Must run after VibeWarden has validated
 * the session — never trust these headers if the request did not come
 * through VibeWarden.
 */
function vibewardenAuth(req, res, next) {
  const userId    = req.headers["x-user-id"];
  const userEmail = req.headers["x-user-email"];
  const verified  = req.headers["x-user-verified"] === "true";
  const sessionId = req.headers["x-session-id"];

  if (!userId) {
    // VibeWarden should have redirected unauthenticated requests.
    // This is a defensive fallback for routes not covered by VibeWarden.
    return res.status(401).json({ error: "unauthorized" });
  }

  req.user = { id: userId, email: userEmail, verified, sessionId };
  next();
}

module.exports = { vibewardenAuth };
```

TypeScript version:

```typescript
// middleware/vibewarden.ts
import { Request, Response, NextFunction } from "express";

export interface VibeWardenUser {
  id: string;
  email: string;
  verified: boolean;
  sessionId: string;
}

declare global {
  namespace Express {
    interface Request {
      user?: VibeWardenUser;
    }
  }
}

export function vibewardenAuth(
  req: Request,
  res: Response,
  next: NextFunction
): void {
  const id        = req.headers["x-user-id"] as string | undefined;
  const email     = (req.headers["x-user-email"] as string) ?? "";
  const verified  = req.headers["x-user-verified"] === "true";
  const sessionId = (req.headers["x-session-id"] as string) ?? "";

  if (!id) {
    res.status(401).json({ error: "unauthorized" });
    return;
  }

  req.user = { id, email, verified, sessionId };
  next();
}
```

### Applying the middleware

Apply globally on all protected routes:

```javascript
// app.js
const express = require("express");
const { vibewardenAuth } = require("./middleware/vibewarden");

const app = express();
app.use(express.json());

// Health check — public, no auth required.
// Add this path to auth.public_paths in vibewarden.yaml.
app.get("/health", (req, res) => res.json({ status: "ok" }));

// All routes below require authentication via VibeWarden.
app.use(vibewardenAuth);

app.get("/api/me", (req, res) => {
  res.json({
    id:    req.user.id,
    email: req.user.email,
  });
});

app.get("/api/dashboard", (req, res) => {
  res.json({ message: `Hello, ${req.user.email}` });
});

app.listen(3000);
```

Apply per-router for fine-grained control:

```javascript
const apiRouter = express.Router();
apiRouter.use(vibewardenAuth);

apiRouter.get("/profile", (req, res) => {
  res.json({ userId: req.user.id });
});

app.use("/api", apiRouter);
```

### Health check route

Add a `/health` endpoint that does not require authentication. Register it in
`auth.public_paths` in `vibewarden.yaml` so VibeWarden passes it through without a
session check:

```javascript
app.get("/health", (req, res) => {
  res.json({ status: "ok" });
});
```

```yaml
# vibewarden.yaml
auth:
  public_paths:
    - "/health"
    - "/static/*"
```

---

## Logging

VibeWarden emits structured JSON logs. Configure Express to also emit JSON logs so
your log aggregation pipeline receives a consistent format:

```javascript
// Using pino (recommended for production)
const pino = require("pino");
const pinoHttp = require("pino-http");

const logger = pino({ level: "info" });

app.use(
  pinoHttp({
    logger,
    customProps: (req) => ({
      userId:    req.headers["x-user-id"],
      sessionId: req.headers["x-session-id"],
    }),
  })
);
```

This enriches every HTTP log entry with the user and session IDs injected by
VibeWarden, making it easy to correlate VibeWarden logs with Express logs in your log
aggregation platform.
