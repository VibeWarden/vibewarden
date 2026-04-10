# VibeWarden + Rails

This guide shows how to put VibeWarden in front of a Ruby on Rails application running
in Docker Compose. VibeWarden handles TLS, authentication (Ory Kratos), rate limiting,
and security headers. Rails receives only authenticated, validated requests with
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
  ▼ :3000 (HTTP, internal, via Puma)
Rails app
```

Your Rails app listens on port 3000 on the internal Docker network via Puma.
It is never directly reachable from the internet — VibeWarden is the sole entry point.

---

## vibewarden.yaml

```yaml
server:
  host: "0.0.0.0"
  port: 443

upstream:
  host: "rails"   # Docker Compose service name
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
    - "/assets/*"
    - "/packs/*"
    - "/favicon.ico"
    - "/robots.txt"
    - "/api/public/*"
    - "/up"          # Rails 7.1+ health check
  session_cookie_name: "ory_kratos_session"
  login_url: "/login"

body_size:
  max: "10MB"
  overrides:
    - path: /uploads
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
  # Rails uses inline scripts for UJS and Turbo — adjust CSP per your stack.
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

  # App-level Postgres (separate DB for your Rails app)
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
      VIBEWARDEN_UPSTREAM_HOST:     rails
      VIBEWARDEN_UPSTREAM_PORT:     "3000"
      VIBEWARDEN_SERVER_HOST:       "0.0.0.0"
      VIBEWARDEN_ADMIN_TOKEN:       ${VIBEWARDEN_ADMIN_TOKEN}
    depends_on:
      kratos:
        condition: service_healthy
      rails:
        condition: service_healthy
    networks:
      - myapp

  rails:
    image: your-registry/your-rails-app:latest
    container_name: myapp-rails
    restart: unless-stopped
    environment:
      RAILS_ENV: production
      RAILS_LOG_TO_STDOUT: "true"
      DATABASE_URL: postgres://${APP_DB_USER}:${APP_DB_PASSWORD}@app-postgres:5432/${APP_DB_NAME}
      SECRET_KEY_BASE: ${RAILS_SECRET_KEY_BASE}
      RAILS_SERVE_STATIC_FILES: "true"
    expose:
      - "3000"
    networks:
      - myapp
    depends_on:
      app-postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:3000/up"]
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

> Rails has its own application database (`app-postgres`) separate from the Kratos
> database (`postgres`). This separation keeps concerns clean.

---

## Reading X-User-* headers in Rails

VibeWarden injects identity headers on every authenticated request before forwarding
to the upstream. Rack (the underlying interface for Rails) transforms HTTP header names
to uppercase with an `HTTP_` prefix and hyphens replaced by underscores.

### Available headers

| HTTP Header | Rack env key | Description |
|---|---|---|
| `X-User-ID` | `HTTP_X_USER_ID` | Kratos identity UUID |
| `X-User-Email` | `HTTP_X_USER_EMAIL` | Primary email address |
| `X-User-Verified` | `HTTP_X_USER_VERIFIED` | `"true"` if email is verified |
| `X-Session-ID` | `HTTP_X_SESSION_ID` | Kratos session UUID |

### Middleware

Create a Rack middleware that reads the VibeWarden headers and makes them available
via `request.env`:

```ruby
# app/middleware/vibewarden_identity.rb

# VibeWardenIdentity is a Rack middleware that reads the X-User-* headers
# injected by VibeWarden and makes them available as request.env keys.
#
# Only trust these headers when all requests come through VibeWarden.
# Never expose your Rails app directly to the internet.
class VibeWardenIdentity
  USER_ID_HEADER    = "HTTP_X_USER_ID".freeze
  EMAIL_HEADER      = "HTTP_X_USER_EMAIL".freeze
  VERIFIED_HEADER   = "HTTP_X_USER_VERIFIED".freeze
  SESSION_ID_HEADER = "HTTP_X_SESSION_ID".freeze

  def initialize(app)
    @app = app
  end

  def call(env)
    env[:vw_user_id]    = env[USER_ID_HEADER]
    env[:vw_user_email] = env[EMAIL_HEADER]
    env[:vw_verified]   = env[VERIFIED_HEADER] == "true"
    env[:vw_session_id] = env[SESSION_ID_HEADER]

    @app.call(env)
  end
end
```

Register the middleware in `config/application.rb`:

```ruby
# config/application.rb
require_relative "../app/middleware/vibewarden_identity"

module MyApp
  class Application < Rails::Application
    # Insert after the standard middleware stack so logging and
    # exception handling middleware run first.
    config.middleware.use VibeWardenIdentity
  end
end
```

### Concern for controllers

Create an `ActionController` concern that provides a `current_user` helper and a
`require_vibewarden_user` before action:

```ruby
# app/controllers/concerns/vibewarden_authenticated.rb
module VibeWardenAuthenticated
  extend ActiveSupport::Concern

  included do
    helper_method :current_vw_user
  end

  # current_vw_user returns a frozen hash with the authenticated user's
  # attributes, or nil if the request is unauthenticated.
  def current_vw_user
    return nil unless request.env[:vw_user_id].present?

    @current_vw_user ||= {
      id:         request.env[:vw_user_id],
      email:      request.env[:vw_user_email],
      verified:   request.env[:vw_verified],
      session_id: request.env[:vw_session_id],
    }.freeze
  end

  # require_vibewarden_user halts the request with 401 if VibeWarden did
  # not inject a user identity. Use as a before_action.
  def require_vibewarden_user
    return if current_vw_user

    render json: { error: "unauthorized" }, status: :unauthorized
  end
end
```

Include the concern in `ApplicationController`:

```ruby
# app/controllers/application_controller.rb
class ApplicationController < ActionController::Base
  include VibeWardenAuthenticated

  # Protect all actions by default. Override in specific controllers.
  before_action :require_vibewarden_user
end
```

### Controllers

```ruby
# app/controllers/api/me_controller.rb
module Api
  class MeController < ApplicationController
    def show
      render json: {
        id:       current_vw_user[:id],
        email:    current_vw_user[:email],
        verified: current_vw_user[:verified],
      }
    end
  end
end
```

Skip authentication for public controllers:

```ruby
# app/controllers/pages_controller.rb
class PagesController < ApplicationController
  skip_before_action :require_vibewarden_user, only: %i[login register]

  def login
    # Render your login page. The form submits to /self-service/login
    # which VibeWarden proxies to Kratos.
  end

  def register
    # Render your registration page.
  end
end
```

### Views — accessing the current user

Because `current_vw_user` is declared as a `helper_method`, it is available in views:

```erb
<%# app/views/dashboard/index.html.erb %>
<h1>Welcome, <%= current_vw_user[:email] %></h1>

<% unless current_vw_user[:verified] %>
  <div class="alert">Please verify your email address.</div>
<% end %>
```

---

## Rails configuration for production behind a proxy

Since VibeWarden terminates TLS and forwards HTTP to Rails, configure Rails to trust
the forwarded headers:

```ruby
# config/environments/production.rb
Rails.application.configure do
  # Trust the X-Forwarded-Proto header set by VibeWarden.
  # This makes request.ssl? return true for HTTPS requests.
  config.force_ssl = false   # VibeWarden handles the redirect; do not double-redirect
  config.assume_ssl = true   # tell Rails it is behind an SSL-terminating proxy

  # VibeWarden sets security headers — disable Rails' own to avoid duplicates.
  config.action_dispatch.default_headers = {
    # Keep only headers that VibeWarden does not set.
    # Remove entries that VibeWarden already handles.
  }

  # Log to stdout for Docker log collection.
  config.logger = ActiveSupport::Logger.new($stdout)
  config.log_level = :info
end
```

### CSRF protection

Rails' built-in CSRF protection applies to all state-mutating requests (POST, PUT,
PATCH, DELETE) with `Content-Type: application/x-www-form-urlencoded` or
`multipart/form-data`. VibeWarden does not bypass CSRF protection — Rails still
validates the CSRF token.

For JSON API endpoints that use VibeWarden for authentication, you may use:

```ruby
class ApiController < ApplicationController
  protect_from_forgery with: :null_session   # or :exception for strict mode
end
```

### Health check (Rails 7.1+)

Rails 7.1 adds a built-in health check at `/up` that returns `200 OK` when the app
is ready. Register `/up` in `auth.public_paths` in `vibewarden.yaml` so VibeWarden
passes it through without a session check:

```yaml
# vibewarden.yaml
auth:
  public_paths:
    - "/up"
```

For older Rails versions, add a minimal health route:

```ruby
# config/routes.rb
Rails.application.routes.draw do
  get "/health", to: proc { [200, {}, [{ status: "ok" }.to_json]] }
  # ...
end
```

---

## Logging

Rails logs to stdout in the Docker setup. VibeWarden enriches the request with user
headers, which you can add to Rails' log tags for correlation:

```ruby
# config/environments/production.rb
config.log_tags = [
  :request_id,
  ->(req) { req.env[:vw_user_id]   || "anonymous" },
  ->(req) { req.env[:vw_session_id] || "no-session" },
]
```

This prepends `[request_id] [user_id] [session_id]` to every Rails log line,
making it easy to trace a user's requests across VibeWarden and Rails logs.
