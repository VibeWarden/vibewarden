# VibeWarden + Django

This guide shows how to put VibeWarden in front of a Django application running in
Docker Compose. VibeWarden handles TLS, authentication (Ory Kratos), rate limiting,
and security headers. Django receives only authenticated, validated requests with
identity injected as HTTP headers.

---

## Architecture

```
Internet
  │
  ▼ :443 (HTTPS)
VibeWarden (Caddy embedded)
  │  validates session cookie → Kratos
  │  injects X-User-* headers
  ▼ :8000 (HTTP, internal, via Gunicorn)
Django app
```

Your Django app listens on port 8000 on the internal Docker network via Gunicorn.
It is never directly reachable from the internet — VibeWarden is the sole entry point.

---

## vibewarden.yaml

```yaml
server:
  host: "0.0.0.0"
  port: 443

upstream:
  host: "django"   # Docker Compose service name
  port: 8000

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
    - "/static/*"
    - "/media/*"
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

  # App-level Postgres (separate DB for your Django app)
  app-postgres:
    image: postgres:17-alpine
    container_name: myapp-app-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER:     ${APP_DB_USER}
      POSTGRES_PASSWORD: ${APP_DB_PASSWORD}
      POSTGRES_DB:       ${APP_DB_NAME}
    volumes:
      - app_postgres_data:/var/lib/postgresql/data
    networks:
      - myapp
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${APP_DB_USER} -d ${APP_DB_NAME}"]
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
      VIBEWARDEN_UPSTREAM_HOST:     django
      VIBEWARDEN_UPSTREAM_PORT:     "8000"
      VIBEWARDEN_SERVER_HOST:       "0.0.0.0"
      VIBEWARDEN_ADMIN_TOKEN:       ${VIBEWARDEN_ADMIN_TOKEN}
    depends_on:
      kratos:
        condition: service_healthy
      django:
        condition: service_healthy
    networks:
      - myapp

  django:
    image: your-registry/your-django-app:latest
    container_name: myapp-django
    restart: unless-stopped
    environment:
      DJANGO_SETTINGS_MODULE: myapp.settings.production
      DATABASE_URL: postgres://${APP_DB_USER}:${APP_DB_PASSWORD}@app-postgres:5432/${APP_DB_NAME}
      SECRET_KEY: ${DJANGO_SECRET_KEY}
    expose:
      - "8000"
    networks:
      - myapp
    depends_on:
      app-postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8000/health/"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
  app_postgres_data:
  caddy_data:

networks:
  myapp:
    driver: bridge
```

> Django has its own application database (`app-postgres`) separate from the Kratos
> database (`postgres`). This separation keeps concerns clean.

---

## Reading X-User-* headers in Django

VibeWarden injects identity headers on every authenticated request before forwarding
to the upstream. Django receives them as HTTP headers; the WSGI/ASGI interface
transforms header names to uppercase with an `HTTP_` prefix.

### Available headers

| HTTP Header | Django `request.META` key | Description |
|---|---|---|
| `X-User-ID` | `HTTP_X_USER_ID` | Kratos identity UUID |
| `X-User-Email` | `HTTP_X_USER_EMAIL` | Primary email address |
| `X-User-Verified` | `HTTP_X_USER_VERIFIED` | `"true"` if email is verified |
| `X-Session-ID` | `HTTP_X_SESSION_ID` | Kratos session UUID |

### Middleware

Create a Django middleware that reads the VibeWarden headers and attaches a user
object to `request.vw_user`:

```python
# myapp/middleware.py
from dataclasses import dataclass


@dataclass(frozen=True)
class VibeWardenUser:
    """Represents the authenticated user as injected by VibeWarden."""

    id: str
    email: str
    verified: bool
    session_id: str


class VibeWardenMiddleware:
    """
    Reads X-User-* headers injected by VibeWarden and attaches a
    VibeWardenUser to request.vw_user.

    Only trust these headers when all requests come through VibeWarden.
    Never expose your Django app directly to the internet.
    """

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        user_id    = request.META.get("HTTP_X_USER_ID", "")
        user_email = request.META.get("HTTP_X_USER_EMAIL", "")
        verified   = request.META.get("HTTP_X_USER_VERIFIED", "false") == "true"
        session_id = request.META.get("HTTP_X_SESSION_ID", "")

        if user_id:
            request.vw_user = VibeWardenUser(
                id=user_id,
                email=user_email,
                verified=verified,
                session_id=session_id,
            )
        else:
            request.vw_user = None

        return self.get_response(request)
```

Register the middleware in `settings.py`:

```python
# settings/production.py
MIDDLEWARE = [
    "django.middleware.security.SecurityMiddleware",
    "django.contrib.sessions.middleware.SessionMiddleware",
    "django.middleware.common.CommonMiddleware",
    "django.middleware.csrf.CsrfViewMiddleware",
    "myapp.middleware.VibeWardenMiddleware",   # add after standard middleware
    # ...
]
```

### Views

```python
# views.py
from django.http import JsonResponse
from django.views import View


class MeView(View):
    def get(self, request):
        user = request.vw_user
        if not user:
            return JsonResponse({"error": "unauthorized"}, status=401)
        return JsonResponse({
            "id":       user.id,
            "email":    user.email,
            "verified": user.verified,
        })


class DashboardView(View):
    def get(self, request):
        user = request.vw_user
        if not user:
            return JsonResponse({"error": "unauthorized"}, status=401)
        return JsonResponse({"message": f"Hello, {user.email}"})
```

### Decorator for protected views

```python
# myapp/decorators.py
from functools import wraps
from django.http import JsonResponse


def vibewarden_required(view_func):
    """
    Decorator that rejects requests where VibeWarden did not inject
    a user identity (i.e. the request is unauthenticated).
    """
    @wraps(view_func)
    def wrapper(request, *args, **kwargs):
        if not getattr(request, "vw_user", None):
            return JsonResponse({"error": "unauthorized"}, status=401)
        return view_func(request, *args, **kwargs)
    return wrapper
```

Usage:

```python
from myapp.decorators import vibewarden_required

@vibewarden_required
def profile(request):
    return JsonResponse({
        "id":    request.vw_user.id,
        "email": request.vw_user.email,
    })
```

### Django REST Framework integration

If you use DRF, create a custom authentication class:

```python
# myapp/authentication.py
from rest_framework.authentication import BaseAuthentication
from rest_framework.exceptions import AuthenticationFailed
from dataclasses import dataclass


@dataclass
class VibeWardenIdentity:
    """DRF-compatible user object populated from VibeWarden headers."""

    pk: str      # required by DRF's is_authenticated logic
    email: str
    verified: bool
    session_id: str

    @property
    def is_authenticated(self):
        return True

    @property
    def is_anonymous(self):
        return False


class VibeWardenAuthentication(BaseAuthentication):
    """
    DRF authentication backend that reads X-User-* headers injected
    by VibeWarden. Returns (user, None) on success.
    """

    def authenticate(self, request):
        user_id = request.META.get("HTTP_X_USER_ID")
        if not user_id:
            return None   # unauthenticated; let DRF handle it

        user = VibeWardenIdentity(
            pk=user_id,
            email=request.META.get("HTTP_X_USER_EMAIL", ""),
            verified=request.META.get("HTTP_X_USER_VERIFIED", "false") == "true",
            session_id=request.META.get("HTTP_X_SESSION_ID", ""),
        )
        return (user, None)
```

Register in `settings.py`:

```python
REST_FRAMEWORK = {
    "DEFAULT_AUTHENTICATION_CLASSES": [
        "myapp.authentication.VibeWardenAuthentication",
    ],
    "DEFAULT_PERMISSION_CLASSES": [
        "rest_framework.permissions.IsAuthenticated",
    ],
}
```

DRF views then use `request.user` as normal:

```python
from rest_framework.views import APIView
from rest_framework.response import Response

class ProfileView(APIView):
    def get(self, request):
        return Response({
            "id":    request.user.pk,
            "email": request.user.email,
        })
```

---

## Django settings for production behind a proxy

Since VibeWarden terminates TLS and forwards HTTP to Django, configure Django to trust
the forwarded headers:

```python
# settings/production.py

# VibeWarden is the only trusted proxy; it runs on the same Docker network.
# Set this to the VibeWarden container's IP or the Docker network CIDR.
ALLOWED_HOSTS = ["myapp.example.com"]

# Tell Django that HTTPS is being handled by VibeWarden upstream.
SECURE_PROXY_SSL_HEADER = ("HTTP_X_FORWARDED_PROTO", "https")

# Django's own HSTS and security headers — disable these because VibeWarden
# already adds them. Letting both add headers results in duplicates.
SECURE_HSTS_SECONDS = 0
SECURE_CONTENT_TYPE_NOSNIFF = False
SECURE_BROWSER_XSS_FILTER = False
X_FRAME_OPTIONS = ""   # VibeWarden sets X-Frame-Options

# Do not redirect HTTP to HTTPS in Django — VibeWarden handles that.
SECURE_SSL_REDIRECT = False

# CSRF — Django still validates CSRF tokens for state-mutating requests.
# VibeWarden does not bypass CSRF protection.
CSRF_TRUSTED_ORIGINS = ["https://myapp.example.com"]
```

### Health check endpoint

Add a lightweight health endpoint that does not touch the database:

```python
# urls.py
from django.urls import path
from django.http import JsonResponse

def health(request):
    return JsonResponse({"status": "ok"})

urlpatterns = [
    path("health/", health),
    # ... your other URL patterns
]
```

Register `/health/` in `auth.public_paths` in `vibewarden.yaml` so VibeWarden
passes it through without a session check.
