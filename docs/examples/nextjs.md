# VibeWarden + Next.js

This guide shows how to put VibeWarden in front of a Next.js application running in
Docker Compose. VibeWarden handles TLS, authentication (Ory Kratos), rate limiting,
and security headers. Next.js receives only authenticated, validated requests.

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
Next.js app
```

Your Next.js app listens on port 3000 on the internal Docker network. It is never
directly reachable from the internet — VibeWarden is the sole entry point.

---

## vibewarden.yaml

```yaml
server:
  host: "0.0.0.0"
  port: 443

upstream:
  host: "nextjs"   # Docker Compose service name
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
    - "/_next/static/*"
    - "/_next/image"
    - "/favicon.ico"
    - "/robots.txt"
    - "/api/public/*"
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

security_headers:
  enabled: true
  hsts_max_age: 31536000
  hsts_include_subdomains: true
  content_type_nosniff: true
  frame_option: "DENY"
  # Next.js uses inline scripts for hydration — adjust CSP accordingly.
  content_security_policy: >-
    default-src 'self';
    script-src 'self' 'unsafe-inline';
    style-src 'self' 'unsafe-inline';
    img-src 'self' data: https:;
    font-src 'self' data:
  referrer_policy: "strict-origin-when-cross-origin"
  cross_origin_opener_policy: "same-origin"
  cross_origin_resource_policy: "same-origin"
  suppress_via_header: true
```

> Next.js requires `'unsafe-inline'` for its runtime hydration scripts unless you
> implement per-request nonces. Tighten the CSP after testing with your specific pages.

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
      VIBEWARDEN_UPSTREAM_HOST:     nextjs
      VIBEWARDEN_UPSTREAM_PORT:     "3000"
      VIBEWARDEN_SERVER_HOST:       "0.0.0.0"
      VIBEWARDEN_ADMIN_TOKEN:       ${VIBEWARDEN_ADMIN_TOKEN}
    depends_on:
      kratos:
        condition: service_healthy
      nextjs:
        condition: service_healthy
    networks:
      - myapp

  nextjs:
    image: your-registry/your-nextjs-app:latest
    container_name: myapp-nextjs
    restart: unless-stopped
    environment:
      NODE_ENV: production
      PORT: "3000"
    expose:
      - "3000"
    networks:
      - myapp
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:3000/api/health"]
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

## Reading X-User-* headers in Next.js

VibeWarden injects identity headers on every authenticated request before forwarding
to the upstream. Your Next.js app reads these headers to identify the current user
without making any additional auth calls.

### Available headers

| Header | Description |
|---|---|
| `X-User-ID` | Kratos identity UUID |
| `X-User-Email` | Primary email address from the identity traits |
| `X-User-Verified` | `"true"` if the email address has been verified |
| `X-Session-ID` | Kratos session UUID |

### App Router (Next.js 13+)

In a Server Component or Route Handler, read headers via the `headers()` API:

```typescript
// app/dashboard/page.tsx
import { headers } from "next/headers";

export default async function DashboardPage() {
  const headersList = await headers();
  const userId    = headersList.get("x-user-id");
  const userEmail = headersList.get("x-user-email");
  const verified  = headersList.get("x-user-verified") === "true";

  if (!userId) {
    // VibeWarden should have redirected unauthenticated requests to /login.
    // This branch is a defensive fallback.
    return <p>Not authenticated</p>;
  }

  return (
    <main>
      <h1>Welcome, {userEmail}</h1>
      {!verified && <p>Please verify your email address.</p>}
    </main>
  );
}
```

In a Route Handler:

```typescript
// app/api/me/route.ts
import { headers } from "next/headers";
import { NextResponse } from "next/server";

export async function GET() {
  const headersList = await headers();
  const userId    = headersList.get("x-user-id");
  const userEmail = headersList.get("x-user-email");

  if (!userId) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  return NextResponse.json({ userId, userEmail });
}
```

### Pages Router (Next.js 12 and earlier)

In `getServerSideProps`:

```typescript
// pages/dashboard.tsx
import { GetServerSideProps } from "next";

interface Props {
  userId: string;
  userEmail: string;
}

export const getServerSideProps: GetServerSideProps<Props> = async ({ req }) => {
  const userId    = req.headers["x-user-id"] as string | undefined;
  const userEmail = req.headers["x-user-email"] as string | undefined;

  if (!userId) {
    return {
      redirect: { destination: "/login", permanent: false },
    };
  }

  return {
    props: { userId, userEmail: userEmail ?? "" },
  };
};

export default function Dashboard({ userId, userEmail }: Props) {
  return <h1>Welcome, {userEmail}</h1>;
}
```

In an API route:

```typescript
// pages/api/me.ts
import { NextApiRequest, NextApiResponse } from "next";

export default function handler(req: NextApiRequest, res: NextApiResponse) {
  const userId    = req.headers["x-user-id"];
  const userEmail = req.headers["x-user-email"];

  if (!userId) {
    return res.status(401).json({ error: "unauthorized" });
  }

  res.json({ userId, userEmail });
}
```

### Middleware (edge runtime)

If you use Next.js Middleware for additional routing logic, read the headers there too:

```typescript
// middleware.ts
import { NextRequest, NextResponse } from "next/server";

export function middleware(request: NextRequest) {
  const userId = request.headers.get("x-user-id");

  // VibeWarden has already enforced auth; this is an extra guard.
  if (!userId && !request.nextUrl.pathname.startsWith("/login")) {
    return NextResponse.redirect(new URL("/login", request.url));
  }

  return NextResponse.next();
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
```

---

## Login and registration pages

The Kratos self-service UI flows are proxied by VibeWarden at `/self-service/*`. Your
Next.js app renders the login and registration pages — they fetch the flow data from
Kratos via the browser.

Minimal login page (using the Kratos browser flow API):

```typescript
// app/login/page.tsx
"use client";

import { useEffect, useState } from "react";

export default function LoginPage() {
  const [flowId, setFlowId] = useState<string | null>(null);
  const [csrfToken, setCsrfToken] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const id = params.get("flow");
    if (!id) {
      // Initiate a new login flow via Kratos
      window.location.href = "/self-service/login/browser";
      return;
    }
    setFlowId(id);
    fetch(`/self-service/login/flows?id=${id}`, { credentials: "include" })
      .then((r) => r.json())
      .then((flow) => {
        const csrfNode = flow.ui?.nodes?.find(
          (n: { attributes?: { name?: string; value?: string } }) =>
            n.attributes?.name === "csrf_token"
        );
        setCsrfToken(csrfNode?.attributes?.value ?? "");
      });
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    await fetch(`/self-service/login?flow=${flowId}`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        method: "password",
        csrf_token: csrfToken,
        identifier: email,
        password,
      }),
    });
    window.location.href = "/";
  };

  return (
    <form onSubmit={handleSubmit}>
      <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" />
      <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Password" />
      <button type="submit">Sign in</button>
    </form>
  );
}
```

Refer to the [Ory Kratos documentation](https://www.ory.sh/docs/kratos/self-service)
for complete flow implementations including CSRF handling and error display.
