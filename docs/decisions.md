# VibeWarden — Decisions Log

This file is the living record of all architectural decisions and agent activity.
Updated by the architect agent (ADRs) and PM agent (planning log).
Never delete entries — mark superseded decisions as `Superseded by ADR-N`.

---

## Locked decisions (from project inception)

| # | Decision | Status |
|---|---|---|
| L-01 | Language: Go | Locked |
| L-02 | Reverse proxy: Caddy embedded as library (Apache 2.0) | Locked |
| L-03 | Identity: Ory Kratos (Apache 2.0) | Locked |
| L-04 | Architecture: Hexagonal + DDD | Locked |
| L-05 | DB migrations: golang-migrate | Locked |
| L-06 | CLI framework: cobra (Apache 2.0) | Locked |
| L-07 | Metrics: prometheus/client_golang (Apache 2.0) | Locked |
| L-08 | Logging: log/slog stdlib | Locked |
| L-09 | Infrastructure: Hetzner | Locked |
| L-10 | Billing: Stripe | Locked |
| L-11 | OSS license: Apache 2.0 for core | Locked |
| L-12 | Proprietary: dashboard UI, AI log schema impl, MCP server, cloud tier | Locked |
| L-13 | Distribution: Docker image + OS installers (pre-built binary, user never builds) | Locked |
| L-14 | Plugin activation: config-driven YAML (all plugins compiled in, enabled/disabled per config) | Locked for v1, revisit for v2 |
| L-15 | Sidecar is always local — runs next to the app, localhost only | Locked |
| L-16 | No hosted VibeWarden instance — hosting a sidecar doesn't make sense | Locked |
| L-17 | Commercial product = fleet dashboard (aggregates logs + metrics from N local instances) | Locked |
| L-18 | Commercial tier name: TBD (placeholder: "VibeWarden Pro") — targeting small businesses, not enterprise | Open |

---

## ADRs

## ADR-001: Plugin architecture — config-driven, compiled-in (v1)
**Date**: 2026-03-20
**Status**: Accepted

### Context
VibeWarden targets vibe coders who need zero-to-secure in minutes. The question was
whether plugins should be installed via CLI (`vibewarden plugin install x`) or
compiled into the binary and activated via config.

A CLI install model requires network access at install time, introduces plugin versioning
complexity, and adds friction for the target user. A build-tags model requires Go toolchain
on the user's machine, which contradicts the distribution model.

### Decision
All plugins are compiled into the official Docker image and OS installer binaries.
Users activate plugins via `vibewarden.yaml` — no install step, no network call, no
version mismatch between plugin and core.

Plugin config pattern:
```yaml
plugins:
  tls:
    enabled: true
    provider: letsencrypt   # or: external (user manages certs), self-signed (dev)
  user-management:
    enabled: true
    adapter: postgres
  rate-limiting:
    enabled: true
  grafana:
    enabled: false
```

### Consequences
- Binary is larger (contains all plugin code) — acceptable tradeoff for v1 simplicity
- CLI install model deferred to v2 if community demand justifies it
- `provider: external` handles users who already manage TLS via Cloudflare, registrar, etc.

---

## ADR-002: Commercial product is a fleet dashboard, not hosted VibeWarden
**Date**: 2026-03-20
**Status**: Accepted

### Context
VibeWarden is a sidecar — it must run next to the app, on localhost. Hosting a sidecar
as a service doesn't make sense architecturally. The question was: what is the commercial
product then?

### Decision
The sidecar is always self-hosted (OSS, free forever). The commercial product is a
**fleet dashboard**: a cloud service at `app.vibewarden.dev` that aggregates logs,
metrics, and health data from multiple local VibeWarden instances.

Tier model:
| Tier | What it is | Target |
|---|---|---|
| OSS | Local sidecar, config-driven, single-app embedded dashboard | Individual vibe coders |
| Pro (name TBD) | Fleet dashboard at app.vibewarden.dev, multi-instance observability | Small businesses, indie devs with multiple apps |
| Enterprise (future) | Self-hosted fleet dashboard, SSO, compliance | Larger teams |

Commercial tier name is TBD — "VibeWarden Pro" is a placeholder. Targeting small
businesses, not enterprise. Final name to be decided later.

### Consequences
- Each local VibeWarden instance optionally phones home to the fleet dashboard
- Phone-home is strictly opt-in, configured in vibewarden.yaml
- This model mirrors Grafana, Netdata, Prometheus — agent free and local, aggregation is the product
- MCP server (v2) integrates with the fleet dashboard for AI-driven observability

---

## PM Log

### 2026-03-20 - Initial Epic Creation

**Created 9 epic issues** for the VibeWarden v1 roadmap.

| Issue | Title | Epic Label |
|-------|-------|------------|
| #1 | Epic: Project Scaffold | `epic:scaffold` |
| #2 | Epic: Request Routing (Caddy Embedding) | `epic:routing` |
| #3 | Epic: Auth (Ory Kratos Integration) | `epic:auth` |
| #4 | Epic: Rate Limiting | `epic:rate-limiting` |
| #5 | Epic: AI-readable Structured Logs | `epic:structured-logs` |
| #6 | Epic: CLI (cobra) | `epic:cli` |
| #7 | Epic: Observability (Prometheus Metrics) | `epic:observability` |
| #8 | Epic: User Management (Admin API) | `epic:user-management` |
| #9 | Epic: Grafana Observability Stack | `epic:grafana-stack` |

**Recommended implementation order:**
1 → 5 → 6 → 2 → 3 → 4 → 7 → 8 → 9

**Note:** Run `gh auth refresh -s read:project` to enable adding issues to the project board.

---

## ADR-003: Project Scaffold Technical Design

**Status:** READY_FOR_DEV
**Issue:** #1
**Date:** 2026-03-20

### Context

This is the foundational epic. Before any business logic can be implemented, we need:
- Go module initialized with correct module path
- Directory structure matching the hexagonal architecture from CLAUDE.md
- Development tooling (Makefile, linting, CI)
- Local dev environment (Docker Compose with Postgres, Kratos)
- Configuration loading infrastructure

All subsequent epics depend on this scaffold being complete and correct.

### Decision

Implement the project scaffold with the following specifications:

#### Go Module

- Module path: `github.com/vibewarden/vibewarden`
- Minimum Go version: 1.26 (specified in go.mod)
- Use latest stable Go (1.26.1) per project policy

#### Dependencies (all licenses verified)

| Dependency | Version | License | Purpose |
|------------|---------|---------|---------|
| github.com/spf13/cobra | latest | Apache 2.0 | CLI framework (locked decision L-06) |
| github.com/spf13/viper | latest | MIT | Config loading (YAML + env vars) |

Note: golangci-lint (GPL-3.0) is used as a development tool only, not linked into the binary.
This is standard practice and does not trigger copyleft requirements.

### File Layout

The dev agent must create exactly this structure:

```
vibewarden/
├── .github/
│   └── workflows/
│       └── ci.yml                    # GitHub Actions CI pipeline
├── .claude/
│   ├── agents/                       # (empty, placeholder for subagent definitions)
│   │   └── .gitkeep
│   └── decisions.md                  # (already exists)
├── cmd/
│   └── vibewarden/
│       └── main.go                   # CLI entrypoint with cobra
├── internal/
│   ├── domain/
│   │   └── .gitkeep                  # placeholder — no domain logic in this epic
│   ├── ports/
│   │   └── .gitkeep                  # placeholder — no ports in this epic
│   ├── adapters/
│   │   ├── caddy/
│   │   │   └── .gitkeep
│   │   ├── kratos/
│   │   │   └── .gitkeep
│   │   ├── postgres/
│   │   │   └── .gitkeep
│   │   └── log/
│   │       └── .gitkeep
│   ├── app/
│   │   └── .gitkeep                  # placeholder — no app services in this epic
│   ├── config/
│   │   └── config.go                 # Config struct and loading logic
│   └── plugins/
│       └── .gitkeep                  # placeholder — plugin registry in future epic
├── migrations/
│   └── .gitkeep                      # placeholder — no migrations in this epic
├── dev/
│   ├── kratos/
│   │   ├── kratos.yml                # Kratos config for local dev
│   │   └── identity.schema.json      # Minimal identity schema
│   └── .gitkeep
├── .gitignore
├── .golangci.yml
├── docker-compose.yml
├── go.mod
├── go.sum
├── Makefile
├── vibewarden.example.yaml
├── CLAUDE.md                         # (already exists — do not modify)
└── LICENSE                           # Apache 2.0
```

### Interfaces & Types

#### Config struct (`internal/config/config.go`)

```go
// Package config provides configuration loading and validation for VibeWarden.
package config

import (
    "fmt"
    "strings"

    "github.com/spf13/viper"
)

// Config holds all configuration for VibeWarden.
// Fields are loaded from vibewarden.yaml and can be overridden by environment variables.
type Config struct {
    // Server configuration
    Server ServerConfig `mapstructure:"server"`

    // Upstream application configuration
    Upstream UpstreamConfig `mapstructure:"upstream"`

    // TLS configuration
    TLS TLSConfig `mapstructure:"tls"`

    // Kratos (identity) configuration
    Kratos KratosConfig `mapstructure:"kratos"`

    // Rate limiting configuration
    RateLimit RateLimitConfig `mapstructure:"rate_limit"`

    // Logging configuration
    Log LogConfig `mapstructure:"log"`

    // Admin API configuration
    Admin AdminConfig `mapstructure:"admin"`
}

// ServerConfig holds server-related settings.
type ServerConfig struct {
    // Host to bind to (default: "127.0.0.1")
    Host string `mapstructure:"host"`
    // Port to listen on (default: 8080)
    Port int `mapstructure:"port"`
}

// UpstreamConfig holds settings for the upstream application being protected.
type UpstreamConfig struct {
    // Host of the upstream application (default: "127.0.0.1")
    Host string `mapstructure:"host"`
    // Port of the upstream application (default: 3000)
    Port int `mapstructure:"port"`
}

// TLSConfig holds TLS-related settings.
type TLSConfig struct {
    // Enabled toggles TLS (default: false for local dev)
    Enabled bool `mapstructure:"enabled"`
    // Domain for TLS certificate (required if enabled)
    Domain string `mapstructure:"domain"`
    // Provider: "letsencrypt", "self-signed", or "external"
    Provider string `mapstructure:"provider"`
}

// KratosConfig holds Ory Kratos connection settings.
type KratosConfig struct {
    // PublicURL is the Kratos public API URL
    PublicURL string `mapstructure:"public_url"`
    // AdminURL is the Kratos admin API URL
    AdminURL string `mapstructure:"admin_url"`
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
    // Enabled toggles rate limiting (default: true)
    Enabled bool `mapstructure:"enabled"`
    // RequestsPerSecond is the default rate limit (default: 100)
    RequestsPerSecond int `mapstructure:"requests_per_second"`
    // BurstSize is the maximum burst size (default: 50)
    BurstSize int `mapstructure:"burst_size"`
}

// LogConfig holds logging settings.
type LogConfig struct {
    // Level: "debug", "info", "warn", "error" (default: "info")
    Level string `mapstructure:"level"`
    // Format: "json" or "text" (default: "json")
    Format string `mapstructure:"format"`
}

// AdminConfig holds admin API settings.
type AdminConfig struct {
    // Enabled toggles the admin API (default: false)
    Enabled bool `mapstructure:"enabled"`
    // Token is the bearer token for admin API authentication
    // Can be set via VIBEWARDEN_ADMIN_TOKEN env var
    Token string `mapstructure:"token"`
}

// Load reads configuration from file and environment variables.
// Config file path can be specified; defaults to "./vibewarden.yaml".
// Environment variables override file values using VIBEWARDEN_ prefix.
// Example: VIBEWARDEN_SERVER_PORT=9090 overrides server.port.
func Load(configPath string) (*Config, error) {
    v := viper.New()

    // Set defaults
    v.SetDefault("server.host", "127.0.0.1")
    v.SetDefault("server.port", 8080)
    v.SetDefault("upstream.host", "127.0.0.1")
    v.SetDefault("upstream.port", 3000)
    v.SetDefault("tls.enabled", false)
    v.SetDefault("tls.provider", "self-signed")
    v.SetDefault("kratos.public_url", "http://127.0.0.1:4433")
    v.SetDefault("kratos.admin_url", "http://127.0.0.1:4434")
    v.SetDefault("rate_limit.enabled", true)
    v.SetDefault("rate_limit.requests_per_second", 100)
    v.SetDefault("rate_limit.burst_size", 50)
    v.SetDefault("log.level", "info")
    v.SetDefault("log.format", "json")
    v.SetDefault("admin.enabled", false)
    v.SetDefault("admin.token", "")

    // Config file
    if configPath != "" {
        v.SetConfigFile(configPath)
    } else {
        v.SetConfigName("vibewarden")
        v.SetConfigType("yaml")
        v.AddConfigPath(".")
        v.AddConfigPath("/etc/vibewarden")
    }

    // Environment variables
    v.SetEnvPrefix("VIBEWARDEN")
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
    v.AutomaticEnv()

    // Read config file (ignore "not found" error — env vars may be sufficient)
    if err := v.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            return nil, fmt.Errorf("reading config file: %w", err)
        }
    }

    var cfg Config
    if err := v.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("unmarshaling config: %w", err)
    }

    return &cfg, nil
}
```

#### CLI entrypoint (`cmd/vibewarden/main.go`)

```go
// Package main is the entrypoint for the VibeWarden security sidecar.
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
    rootCmd := &cobra.Command{
        Use:   "vibewarden",
        Short: "VibeWarden - Security sidecar for vibe-coded apps",
        Long: `VibeWarden is an open-source security sidecar that handles
TLS, authentication, rate limiting, and AI-readable structured logs.

Zero-to-secure in minutes.`,
        Version: version,
        Run: func(cmd *cobra.Command, args []string) {
            // Default behavior: print help
            cmd.Help()
        },
    }

    // Configure version template
    rootCmd.SetVersionTemplate("vibewarden {{.Version}}\n")

    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

### Makefile Specification

```makefile
# VibeWarden Makefile

.PHONY: build test lint run docker-up docker-down clean

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
BINARY := bin/vibewarden

# Build the binary
build:
	@mkdir -p bin
	go build $(LDFLAGS) -o $(BINARY) ./cmd/vibewarden

# Run tests
test:
	go test -race -v ./...

# Run linter
lint:
	golangci-lint run

# Build and run
run: build
	./$(BINARY)

# Start dev environment
docker-up:
	docker compose up -d

# Stop dev environment
docker-down:
	docker compose down

# Clean build artifacts
clean:
	rm -rf bin/
```

### Docker Compose Specification (`docker-compose.yml`)

```yaml
version: "3.8"

services:
  postgres:
    image: postgres:18-alpine
    container_name: vibewarden-postgres
    environment:
      POSTGRES_USER: vibewarden
      POSTGRES_PASSWORD: vibewarden
      POSTGRES_DB: vibewarden
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U vibewarden -d vibewarden"]
      interval: 5s
      timeout: 5s
      retries: 5

  kratos-migrate:
    image: oryd/kratos:v25.4.0
    container_name: vibewarden-kratos-migrate
    environment:
      DSN: postgres://vibewarden:vibewarden@postgres:5432/vibewarden?sslmode=disable
    volumes:
      - ./dev/kratos:/etc/config/kratos:ro
    command: migrate sql -e --yes
    depends_on:
      postgres:
        condition: service_healthy

  kratos:
    image: oryd/kratos:v25.4.0
    container_name: vibewarden-kratos
    environment:
      DSN: postgres://vibewarden:vibewarden@postgres:5432/vibewarden?sslmode=disable
      SERVE_PUBLIC_BASE_URL: http://127.0.0.1:4433/
      SERVE_ADMIN_BASE_URL: http://127.0.0.1:4434/
    ports:
      - "4433:4433"
      - "4434:4434"
    volumes:
      - ./dev/kratos:/etc/config/kratos:ro
    command: serve --config /etc/config/kratos/kratos.yml --dev --watch-courier
    depends_on:
      kratos-migrate:
        condition: service_completed_successfully
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://127.0.0.1:4434/admin/health/ready"]
      interval: 10s
      timeout: 5s
      retries: 5

  mailslurper:
    image: oryd/mailslurper:latest-smtps
    container_name: vibewarden-mailslurper
    ports:
      - "4436:4436"  # SMTP
      - "4437:4437"  # Web UI

volumes:
  postgres_data:
```

### Kratos Local Dev Config (`dev/kratos/kratos.yml`)

```yaml
version: v25.4.0

dsn: memory

serve:
  public:
    base_url: http://127.0.0.1:4433/
    cors:
      enabled: true
  admin:
    base_url: http://127.0.0.1:4434/

selfservice:
  default_browser_return_url: http://127.0.0.1:3000/
  allowed_return_urls:
    - http://127.0.0.1:3000

  methods:
    password:
      enabled: true

  flows:
    registration:
      enabled: true
      ui_url: http://127.0.0.1:3000/auth/registration
    login:
      ui_url: http://127.0.0.1:3000/auth/login
    logout:
      after:
        default_browser_return_url: http://127.0.0.1:3000/
    verification:
      enabled: true
      ui_url: http://127.0.0.1:3000/auth/verification
    recovery:
      enabled: true
      ui_url: http://127.0.0.1:3000/auth/recovery

log:
  level: debug

identity:
  default_schema_id: default
  schemas:
    - id: default
      url: file:///etc/config/kratos/identity.schema.json

courier:
  smtp:
    connection_uri: smtp://mailslurper:4436/?skip_ssl_verify=true
```

### Kratos Identity Schema (`dev/kratos/identity.schema.json`)

```json
{
  "$id": "https://schemas.vibewarden.dev/identity.schema.json",
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "VibeWarden Identity",
  "type": "object",
  "properties": {
    "traits": {
      "type": "object",
      "properties": {
        "email": {
          "type": "string",
          "format": "email",
          "title": "Email",
          "ory.sh/kratos": {
            "credentials": {
              "password": {
                "identifier": true
              }
            },
            "verification": {
              "via": "email"
            },
            "recovery": {
              "via": "email"
            }
          }
        }
      },
      "required": ["email"],
      "additionalProperties": false
    }
  }
}
```

### golangci-lint Configuration (`.golangci.yml`)

```yaml
run:
  timeout: 5m
  go: "1.26"

linters:
  enable:
    - gofmt
    - goimports
    - govet
    - staticcheck
    - errcheck
    - revive
    - gosec

linters-settings:
  goimports:
    local-prefixes: github.com/vibewarden/vibewarden

  revive:
    rules:
      - name: exported
        severity: warning
      - name: blank-imports
        severity: warning
      - name: context-as-argument
        severity: warning
      - name: error-return
        severity: warning
      - name: error-strings
        severity: warning
      - name: errorf
        severity: warning
      - name: increment-decrement
        severity: warning
      - name: var-naming
        severity: warning

  gosec:
    severity: medium
    confidence: medium

issues:
  exclude-use-default: false
  max-issues-per-linter: 0
  max-same-issues: 0
```

### GitHub Actions CI (`.github/workflows/ci.yml`)

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - uses: actions/setup-go@v6
        with:
          go-version: "1.26"
          cache: true

      - name: golangci-lint
        uses: golangci/golangci-lint-action@v9
        with:
          version: v2.11.3

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - uses: actions/setup-go@v6
        with:
          go-version: "1.26"
          cache: true

      - name: Run tests
        run: go test -race -v ./...

  build:
    name: Build
    runs-on: ubuntu-latest
    needs: [lint, test]
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0  # Needed for git describe

      - uses: actions/setup-go@v6
        with:
          go-version: "1.26"
          cache: true

      - name: Build
        run: make build

      - name: Verify version flag
        run: ./bin/vibewarden --version
```

### Example Config (`vibewarden.example.yaml`)

```yaml
# VibeWarden Configuration
# Copy this file to vibewarden.yaml and customize as needed.
# Environment variables can override any setting using VIBEWARDEN_ prefix.
# Example: VIBEWARDEN_SERVER_PORT=9090 overrides server.port

# Server settings
server:
  # Host to bind to (use 0.0.0.0 to expose externally)
  host: "127.0.0.1"
  # Port to listen on
  port: 8080

# Upstream application being protected
upstream:
  # Host of your application
  host: "127.0.0.1"
  # Port of your application
  port: 3000

# TLS configuration
tls:
  # Enable TLS (set to true in production)
  enabled: false
  # Domain for certificate (required if enabled)
  domain: ""
  # Provider: "letsencrypt", "self-signed", or "external"
  provider: "self-signed"

# Ory Kratos (identity/auth) settings
kratos:
  # Kratos public API (user-facing flows)
  public_url: "http://127.0.0.1:4433"
  # Kratos admin API (internal use)
  admin_url: "http://127.0.0.1:4434"

# Rate limiting
rate_limit:
  # Enable rate limiting
  enabled: true
  # Requests per second per client
  requests_per_second: 100
  # Maximum burst size
  burst_size: 50

# Logging
log:
  # Level: debug, info, warn, error
  level: "info"
  # Format: json (recommended) or text
  format: "json"

# Admin API (for user management, metrics, etc.)
admin:
  # Enable admin API
  enabled: false
  # Bearer token for authentication (use env var VIBEWARDEN_ADMIN_TOKEN in production)
  token: ""
```

### .gitignore

```
# Binaries
/bin/
*.exe

# Environment and secrets
.env
.env.*
!.env.example

# IDE
.idea/
.vscode/
*.swp
*.swo

# Logs
*.log

# OS
.DS_Store
Thumbs.db

# Go
vendor/

# Test artifacts
coverage.out
coverage.html
```

### LICENSE (Apache 2.0)

Standard Apache 2.0 license text with:
- Copyright 2024 VibeWarden Contributors

### Sequence / Wiring

At startup (when a `serve` command is added in a future epic), the wiring will be:

1. `main.go` invokes cobra root command
2. For `--version` flag, cobra handles it and prints `vibewarden <version>`
3. For `serve` subcommand (future epic):
   - Load config via `config.Load(configPath)`
   - Initialize logger (slog) based on config
   - Initialize adapters with config values
   - Start the server

For this scaffold epic, the only flow is:
1. User runs `vibewarden --version`
2. Cobra prints `vibewarden v0.1.0` (or whatever version)
3. Exit 0

### Consequences

**Positive:**
- Clean separation of concerns from day one
- Placeholder directories guide future development
- Local dev environment ready with single `docker compose up`
- CI pipeline catches issues before merge
- Config struct ready for all future epics

**Negative:**
- Many placeholder `.gitkeep` files (minor clutter)
- Config struct has fields for features not yet implemented

**Trade-offs:**
- golangci-lint version pinned to avoid drift (must update periodically)

**Follow-up work:**
- Epic 2 (Routing) will add the Caddy adapter
- Epic 5 (Structured Logs) will implement log schema
- Epic 6 (CLI) will add `serve` subcommand

### Out of Scope

- Actual reverse proxy implementation (Epic 2)
- Business logic or domain entities
- Production deployment configs (Helm, Dockerfile for release)
- Kratos identity schema customization beyond minimal local dev
- Test coverage reporting
- `serve` command (will be added in Epic 6 CLI)
- Any adapters or ports beyond the empty placeholder directories

---

## ADR-004: Request Routing Architecture (Caddy Embedding)

**Status:** Accepted
**Issue:** #2
**Date:** 2026-03-20

### Context

VibeWarden needs to proxy HTTP/HTTPS traffic from clients to the upstream application. The locked decision L-02 mandates embedding Caddy as a library (not subprocess). This epic delivers the core reverse proxy functionality including:

- Caddy embedded with programmatic JSON config
- Reverse proxying to upstream application
- Automatic TLS via Let's Encrypt (for non-localhost domains)
- Security headers middleware
- Health check endpoint

This is a large epic with several distinct components. To enable focused reviews and incremental delivery, it will be split into sub-issues.

### Decision

#### Epic Split Strategy

Split Epic #2 into four focused sub-issues:

| Sub-Issue | Title | Dependencies |
|-----------|-------|--------------|
| #2.1 | Core Caddy Embedding and Reverse Proxy | Epic #1 |
| #2.2 | Security Headers Middleware | #2.1 |
| #2.3 | Health Check Endpoint | #2.1 |
| #2.4 | TLS Automation (Let's Encrypt) | #2.1 |

Each sub-issue is independently testable and deployable.

#### Dependencies (License Verified)

| Dependency | Version | License | Purpose |
|------------|---------|---------|---------|
| github.com/caddyserver/caddy/v2 | latest (v2.10+) | Apache 2.0 | Reverse proxy engine (locked decision L-02) |

Caddy v2 brings in many transitive dependencies, but Caddy itself is Apache 2.0 licensed.

#### Architecture Overview

```
                    ┌─────────────────────────────────────────────────────┐
                    │                    VibeWarden                       │
                    │                                                     │
 Incoming ──────────┼──► ┌─────────────────┐    ┌───────────────────┐    │
 HTTP(S)            │    │  Caddy Adapter  │───►│ Upstream App      │    │
 Requests           │    │  (implements    │    │ (localhost:3000)  │    │
                    │    │   ProxyServer)  │◄───│                   │    │
                    │    └────────┬────────┘    └───────────────────┘    │
                    │             │                                       │
                    │    ┌────────▼────────┐                              │
                    │    │  Middleware     │                              │
                    │    │  Chain:         │                              │
                    │    │  - Security Hdr │                              │
                    │    │  - Health Check │                              │
                    │    │  - (future:auth)│                              │
                    │    │  - (future:rate)│                              │
                    │    └─────────────────┘                              │
                    └─────────────────────────────────────────────────────┘
```

#### Hexagonal Architecture Mapping

**Domain Layer** (`internal/domain/`)
- No domain entities in this epic (pure infrastructure)
- Value objects for configuration validation will be added if needed

**Ports Layer** (`internal/ports/`)
- `ProxyServer` interface - abstraction for the reverse proxy
- `Middleware` interface - abstraction for HTTP middleware chain

**Adapters Layer** (`internal/adapters/caddy/`)
- `CaddyAdapter` - implements `ProxyServer` using embedded Caddy
- Builds Caddy JSON config programmatically
- Manages Caddy lifecycle (start, stop, reload)

**Application Layer** (`internal/app/`)
- `ProxyService` - orchestrates proxy startup and middleware registration

### File Layout

New/modified files for this epic:

```
internal/
├── ports/
│   ├── proxy.go              # ProxyServer interface
│   └── middleware.go         # Middleware interface
├── adapters/
│   └── caddy/
│       ├── adapter.go        # CaddyAdapter implementation
│       ├── adapter_test.go   # Unit tests
│       ├── config.go         # Caddy JSON config builder
│       ├── config_test.go    # Config builder tests
│       └── middleware.go     # Caddy middleware integration
├── app/
│   └── proxy/
│       ├── service.go        # ProxyService
│       └── service_test.go   # Unit tests
├── middleware/
│   ├── security_headers.go   # Security headers middleware
│   ├── security_headers_test.go
│   ├── health.go             # Health check handler
│   └── health_test.go
└── config/
    └── config.go             # (existing - extend with security_headers config)
```

### Interface Definitions

#### ProxyServer Port (`internal/ports/proxy.go`)

```go
// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import "context"

// ProxyServer defines the interface for the reverse proxy server.
// Implementations handle incoming HTTP(S) requests and forward them to upstream.
type ProxyServer interface {
    // Start begins listening for incoming requests.
    // Blocks until the context is cancelled or an error occurs.
    Start(ctx context.Context) error

    // Stop gracefully shuts down the proxy server.
    // The provided context controls the shutdown timeout.
    Stop(ctx context.Context) error

    // Reload applies configuration changes without dropping connections.
    // Not all implementations may support reload; they should return an error if not.
    Reload(ctx context.Context) error
}

// ProxyConfig holds configuration for the proxy server.
// This is a domain-agnostic configuration that ports can depend on.
type ProxyConfig struct {
    // ListenAddr is the address to bind to (e.g., "127.0.0.1:8080")
    ListenAddr string

    // UpstreamAddr is the address of the upstream application (e.g., "127.0.0.1:3000")
    UpstreamAddr string

    // TLS configuration
    TLS TLSConfig

    // SecurityHeaders configuration
    SecurityHeaders SecurityHeadersConfig
}

// TLSConfig holds TLS-specific settings.
type TLSConfig struct {
    // Enabled toggles TLS termination
    Enabled bool

    // Domain for certificate provisioning (required if Enabled && AutoCert)
    Domain string

    // AutoCert enables automatic certificate provisioning via ACME (Let's Encrypt)
    AutoCert bool

    // CertPath is the path to a custom certificate file (if not using AutoCert)
    CertPath string

    // KeyPath is the path to the private key file (if not using AutoCert)
    KeyPath string

    // StoragePath is where Caddy stores certificates (default: system-specific)
    StoragePath string
}

// SecurityHeadersConfig holds security header settings.
type SecurityHeadersConfig struct {
    // Enabled toggles security headers middleware
    Enabled bool

    // HSTS settings
    HSTSMaxAge            int  // max-age in seconds (default: 31536000 = 1 year)
    HSTSIncludeSubDomains bool // includeSubDomains directive
    HSTSPreload           bool // preload directive

    // Content-Type-Options
    ContentTypeNosniff bool // X-Content-Type-Options: nosniff

    // Frame options
    FrameOption string // X-Frame-Options value: "DENY", "SAMEORIGIN", or "" (disabled)

    // CSP
    ContentSecurityPolicy string // Content-Security-Policy value (empty = disabled)

    // Referrer Policy
    ReferrerPolicy string // Referrer-Policy value (empty = disabled)

    // Permissions Policy (formerly Feature-Policy)
    PermissionsPolicy string // Permissions-Policy value (empty = disabled)
}
```

#### Middleware Port (`internal/ports/middleware.go`)

```go
package ports

import "net/http"

// Middleware defines an HTTP middleware that can be applied to requests.
// Middleware wraps an http.Handler and returns a new http.Handler.
type Middleware func(next http.Handler) http.Handler

// MiddlewareChain is an ordered list of middleware to apply.
type MiddlewareChain []Middleware

// Apply wraps the given handler with all middleware in the chain.
// Middleware is applied in reverse order so the first middleware in the chain
// is the outermost wrapper (executes first on request, last on response).
func (c MiddlewareChain) Apply(h http.Handler) http.Handler {
    for i := len(c) - 1; i >= 0; i-- {
        h = c[i](h)
    }
    return h
}
```

#### CaddyAdapter (`internal/adapters/caddy/adapter.go`)

```go
// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"

    "github.com/caddyserver/caddy/v2"
    "github.com/caddyserver/caddy/v2/caddyconfig"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// Adapter implements ports.ProxyServer using embedded Caddy.
type Adapter struct {
    config *ports.ProxyConfig
    logger *slog.Logger
}

// NewAdapter creates a new Caddy adapter with the given configuration.
func NewAdapter(cfg *ports.ProxyConfig, logger *slog.Logger) *Adapter {
    return &Adapter{
        config: cfg,
        logger: logger,
    }
}

// Start implements ports.ProxyServer.Start.
// It builds Caddy JSON configuration and runs Caddy.
func (a *Adapter) Start(ctx context.Context) error {
    cfg, err := a.buildConfig()
    if err != nil {
        return fmt.Errorf("building caddy config: %w", err)
    }

    cfgJSON, err := json.Marshal(cfg)
    if err != nil {
        return fmt.Errorf("marshaling caddy config: %w", err)
    }

    a.logger.Info("starting caddy proxy",
        slog.String("listen", a.config.ListenAddr),
        slog.String("upstream", a.config.UpstreamAddr),
    )

    // Load and run Caddy with the configuration
    err = caddy.Load(cfgJSON, true)
    if err != nil {
        return fmt.Errorf("loading caddy config: %w", err)
    }

    // Wait for context cancellation
    <-ctx.Done()

    return nil
}

// Stop implements ports.ProxyServer.Stop.
func (a *Adapter) Stop(ctx context.Context) error {
    a.logger.Info("stopping caddy proxy")
    return caddy.Stop()
}

// Reload implements ports.ProxyServer.Reload.
func (a *Adapter) Reload(ctx context.Context) error {
    cfg, err := a.buildConfig()
    if err != nil {
        return fmt.Errorf("building caddy config: %w", err)
    }

    cfgJSON, err := json.Marshal(cfg)
    if err != nil {
        return fmt.Errorf("marshaling caddy config: %w", err)
    }

    a.logger.Info("reloading caddy configuration")

    return caddy.Load(cfgJSON, true)
}

// buildConfig constructs the Caddy JSON configuration.
func (a *Adapter) buildConfig() (map[string]any, error) {
    // Implementation in config.go
    return BuildCaddyConfig(a.config)
}
```

#### Caddy Config Builder (`internal/adapters/caddy/config.go`)

```go
package caddy

import (
    "fmt"
    "net"
    "strings"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// BuildCaddyConfig constructs the Caddy JSON configuration from ProxyConfig.
// Uses Caddy's native JSON config format (not Caddyfile).
func BuildCaddyConfig(cfg *ports.ProxyConfig) (map[string]any, error) {
    // Determine if this is a local address (skip TLS for localhost)
    isLocal := isLocalAddress(cfg.UpstreamAddr) || isLocalAddress(cfg.ListenAddr)

    // Build the reverse proxy handler
    reverseProxyHandler := map[string]any{
        "handler": "reverse_proxy",
        "upstreams": []map[string]any{
            {"dial": cfg.UpstreamAddr},
        },
    }

    // Build route handlers (middleware chain + reverse proxy)
    handlers := []map[string]any{}

    // Add security headers handler if enabled
    if cfg.SecurityHeaders.Enabled {
        handlers = append(handlers, buildSecurityHeadersHandler(cfg.SecurityHeaders))
    }

    // Add reverse proxy as final handler
    handlers = append(handlers, reverseProxyHandler)

    // Build routes
    routes := []map[string]any{
        {
            "handle": handlers,
        },
    }

    // Build the server configuration
    server := map[string]any{
        "listen": []string{cfg.ListenAddr},
        "routes": routes,
    }

    // Configure TLS if enabled and not local
    if cfg.TLS.Enabled && !isLocal {
        server["tls_connection_policies"] = buildTLSPolicy(cfg.TLS)

        // Add automatic HTTPS redirect
        server["automatic_https"] = map[string]any{
            "disable": false,
        }
    } else {
        // Disable automatic HTTPS for local development
        server["automatic_https"] = map[string]any{
            "disable": true,
        }
    }

    // Build apps configuration
    apps := map[string]any{
        "http": map[string]any{
            "servers": map[string]any{
                "vibewarden": server,
            },
        },
    }

    // Configure TLS automation if enabled
    if cfg.TLS.Enabled && cfg.TLS.AutoCert && !isLocal {
        apps["tls"] = buildTLSAutomation(cfg.TLS)
    }

    return map[string]any{
        "apps": apps,
    }, nil
}

// buildSecurityHeadersHandler creates the Caddy headers handler for security headers.
func buildSecurityHeadersHandler(cfg ports.SecurityHeadersConfig) map[string]any {
    headers := map[string][]string{}

    // HSTS
    if cfg.HSTSMaxAge > 0 {
        hsts := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAge)
        if cfg.HSTSIncludeSubDomains {
            hsts += "; includeSubDomains"
        }
        if cfg.HSTSPreload {
            hsts += "; preload"
        }
        headers["Strict-Transport-Security"] = []string{hsts}
    }

    // X-Content-Type-Options
    if cfg.ContentTypeNosniff {
        headers["X-Content-Type-Options"] = []string{"nosniff"}
    }

    // X-Frame-Options
    if cfg.FrameOption != "" {
        headers["X-Frame-Options"] = []string{cfg.FrameOption}
    }

    // Content-Security-Policy
    if cfg.ContentSecurityPolicy != "" {
        headers["Content-Security-Policy"] = []string{cfg.ContentSecurityPolicy}
    }

    // Referrer-Policy
    if cfg.ReferrerPolicy != "" {
        headers["Referrer-Policy"] = []string{cfg.ReferrerPolicy}
    }

    // Permissions-Policy
    if cfg.PermissionsPolicy != "" {
        headers["Permissions-Policy"] = []string{cfg.PermissionsPolicy}
    }

    return map[string]any{
        "handler": "headers",
        "response": map[string]any{
            "set": headers,
        },
    }
}

// buildTLSPolicy creates TLS connection policies for Caddy.
func buildTLSPolicy(cfg ports.TLSConfig) []map[string]any {
    return []map[string]any{
        {
            // Default policy - Caddy handles the rest
        },
    }
}

// buildTLSAutomation configures automatic certificate management.
func buildTLSAutomation(cfg ports.TLSConfig) map[string]any {
    automation := map[string]any{
        "automation": map[string]any{
            "policies": []map[string]any{
                {
                    "subjects": []string{cfg.Domain},
                    "issuers": []map[string]any{
                        {
                            "module": "acme",
                        },
                    },
                },
            },
        },
    }

    // Configure certificate storage if specified
    if cfg.StoragePath != "" {
        automation["storage"] = map[string]any{
            "module": "file_system",
            "root":   cfg.StoragePath,
        }
    }

    return automation
}

// isLocalAddress checks if the address is localhost or a loopback address.
func isLocalAddress(addr string) bool {
    host, _, err := net.SplitHostPort(addr)
    if err != nil {
        host = addr
    }

    host = strings.ToLower(host)

    if host == "localhost" || host == "" {
        return true
    }

    ip := net.ParseIP(host)
    if ip == nil {
        return false
    }

    return ip.IsLoopback()
}
```

#### Health Check Handler (`internal/middleware/health.go`)

```go
// Package middleware provides HTTP middleware for VibeWarden.
package middleware

import (
    "encoding/json"
    "net/http"
)

// HealthResponse is the JSON response from the health endpoint.
type HealthResponse struct {
    Status  string `json:"status"`
    Version string `json:"version"`
}

// HealthHandler returns an http.HandlerFunc for the health check endpoint.
// The health endpoint is served at /_vibewarden/health.
func HealthHandler(version string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        resp := HealthResponse{
            Status:  "ok",
            Version: version,
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)

        _ = json.NewEncoder(w).Encode(resp)
    }
}

// HealthMiddleware intercepts requests to /_vibewarden/health and serves
// the health response. All other requests pass through to the next handler.
func HealthMiddleware(version string) func(next http.Handler) http.Handler {
    healthHandler := HealthHandler(version)

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if r.URL.Path == "/_vibewarden/health" {
                healthHandler(w, r)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

#### Security Headers Middleware (`internal/middleware/security_headers.go`)

```go
package middleware

import (
    "fmt"
    "net/http"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// SecurityHeaders creates a middleware that adds security headers to responses.
// This middleware applies headers after the response from upstream but before
// sending to the client.
func SecurityHeaders(cfg ports.SecurityHeadersConfig) func(next http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Set security headers before calling next handler
            setSecurityHeaders(w, cfg)

            next.ServeHTTP(w, r)
        })
    }
}

// setSecurityHeaders applies all configured security headers to the response.
func setSecurityHeaders(w http.ResponseWriter, cfg ports.SecurityHeadersConfig) {
    // HSTS
    if cfg.HSTSMaxAge > 0 {
        hsts := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAge)
        if cfg.HSTSIncludeSubDomains {
            hsts += "; includeSubDomains"
        }
        if cfg.HSTSPreload {
            hsts += "; preload"
        }
        w.Header().Set("Strict-Transport-Security", hsts)
    }

    // X-Content-Type-Options
    if cfg.ContentTypeNosniff {
        w.Header().Set("X-Content-Type-Options", "nosniff")
    }

    // X-Frame-Options
    if cfg.FrameOption != "" {
        w.Header().Set("X-Frame-Options", cfg.FrameOption)
    }

    // Content-Security-Policy
    if cfg.ContentSecurityPolicy != "" {
        w.Header().Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
    }

    // Referrer-Policy
    if cfg.ReferrerPolicy != "" {
        w.Header().Set("Referrer-Policy", cfg.ReferrerPolicy)
    }

    // Permissions-Policy
    if cfg.PermissionsPolicy != "" {
        w.Header().Set("Permissions-Policy", cfg.PermissionsPolicy)
    }
}

// DefaultSecurityHeadersConfig returns sensible default security header settings.
func DefaultSecurityHeadersConfig() ports.SecurityHeadersConfig {
    return ports.SecurityHeadersConfig{
        Enabled:               true,
        HSTSMaxAge:            31536000, // 1 year
        HSTSIncludeSubDomains: true,
        HSTSPreload:           false, // Preload requires manual submission
        ContentTypeNosniff:    true,
        FrameOption:           "DENY",
        ContentSecurityPolicy: "default-src 'self'",
        ReferrerPolicy:        "strict-origin-when-cross-origin",
        PermissionsPolicy:     "",
    }
}
```

#### ProxyService (`internal/app/proxy/service.go`)

```go
// Package proxy provides the application service for the reverse proxy.
package proxy

import (
    "context"
    "fmt"
    "log/slog"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// Service orchestrates the reverse proxy lifecycle.
type Service struct {
    server ports.ProxyServer
    logger *slog.Logger
}

// NewService creates a new proxy service with the given server implementation.
func NewService(server ports.ProxyServer, logger *slog.Logger) *Service {
    return &Service{
        server: server,
        logger: logger,
    }
}

// Run starts the proxy server and blocks until the context is cancelled.
// On context cancellation, it initiates graceful shutdown.
func (s *Service) Run(ctx context.Context) error {
    // Create a child context for the server
    serverCtx, cancel := context.WithCancel(ctx)
    defer cancel()

    // Channel to receive server errors
    errCh := make(chan error, 1)

    // Start server in goroutine
    go func() {
        errCh <- s.server.Start(serverCtx)
    }()

    // Wait for context cancellation or server error
    select {
    case <-ctx.Done():
        s.logger.Info("shutdown signal received, stopping proxy")
        // Context for graceful shutdown
        shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer shutdownCancel()

        if err := s.server.Stop(shutdownCtx); err != nil {
            return fmt.Errorf("stopping proxy: %w", err)
        }
        return ctx.Err()

    case err := <-errCh:
        if err != nil {
            return fmt.Errorf("proxy server error: %w", err)
        }
        return nil
    }
}

// Reload reloads the proxy configuration without dropping connections.
func (s *Service) Reload(ctx context.Context) error {
    s.logger.Info("reloading proxy configuration")
    return s.server.Reload(ctx)
}
```

### Config Extension

Add to `internal/config/config.go`:

```go
// SecurityHeadersConfig holds security header settings.
type SecurityHeadersConfig struct {
    // Enabled toggles security headers (default: true)
    Enabled bool `mapstructure:"enabled"`

    // HSTS max-age in seconds (default: 31536000 = 1 year)
    HSTSMaxAge int `mapstructure:"hsts_max_age"`
    // Include subdomains in HSTS (default: true)
    HSTSIncludeSubDomains bool `mapstructure:"hsts_include_subdomains"`
    // HSTS preload (default: false - requires manual submission)
    HSTSPreload bool `mapstructure:"hsts_preload"`

    // X-Content-Type-Options: nosniff (default: true)
    ContentTypeNosniff bool `mapstructure:"content_type_nosniff"`

    // X-Frame-Options value: "DENY", "SAMEORIGIN", or "" to disable (default: "DENY")
    FrameOption string `mapstructure:"frame_option"`

    // Content-Security-Policy value (default: "default-src 'self'")
    ContentSecurityPolicy string `mapstructure:"content_security_policy"`

    // Referrer-Policy value (default: "strict-origin-when-cross-origin")
    ReferrerPolicy string `mapstructure:"referrer_policy"`

    // Permissions-Policy value (default: "")
    PermissionsPolicy string `mapstructure:"permissions_policy"`
}
```

And add to the Config struct:
```go
// Security headers configuration
SecurityHeaders SecurityHeadersConfig `mapstructure:"security_headers"`
```

### Request/Response Sequence

#### Proxy Request Flow (HTTP, no TLS)

1. Client sends HTTP request to `127.0.0.1:8080`
2. Caddy accepts connection on listen address
3. Health check middleware checks path:
   - If `/_vibewarden/health`: respond with JSON health status, return
   - Otherwise: continue chain
4. Security headers middleware adds headers to response writer
5. Reverse proxy handler forwards request to upstream (`127.0.0.1:3000`)
6. Upstream responds
7. Caddy forwards response (with security headers) to client

#### Proxy Request Flow (HTTPS with Let's Encrypt)

1. Client sends HTTPS request to `example.com:443`
2. Caddy performs TLS handshake (certificate from Let's Encrypt cache)
3. If certificate missing/expired: Caddy obtains new certificate via ACME
4. After TLS established, same flow as HTTP (steps 3-7)
5. HSTS header tells browser to use HTTPS in future

### Error Cases

| Error | Handling |
|-------|----------|
| Upstream unreachable | Caddy returns 502 Bad Gateway |
| TLS certificate failure | Caddy logs error, falls back to HTTP if possible |
| Invalid config | Return error from `Start()`, do not start server |
| Listen port in use | Return error from `Start()` with clear message |
| Graceful shutdown timeout | Force close connections after timeout |

### Test Strategy

**Unit Tests:**
- `internal/adapters/caddy/config_test.go` - Test JSON config generation for various scenarios
- `internal/middleware/security_headers_test.go` - Test header values for all configurations
- `internal/middleware/health_test.go` - Test health endpoint response format
- `internal/app/proxy/service_test.go` - Test lifecycle with mock ProxyServer

**Integration Tests:**
- `internal/adapters/caddy/adapter_integration_test.go`:
  - Start real Caddy with mock upstream (httptest.Server)
  - Make requests through proxy, verify they reach upstream
  - Verify security headers present in response
  - Verify health endpoint responds correctly
  - Test graceful shutdown

Integration tests will use build tag `//go:build integration` and can be run with `go test -tags=integration`.

### Consequences

**Positive:**
- Programmatic Caddy config enables dynamic configuration changes
- Caddy handles TLS complexity (ACME, renewal, OCSP stapling)
- Security headers are a single toggle with sensible defaults
- Health endpoint provides standard observability primitive
- Hexagonal architecture allows swapping Caddy for another proxy if needed

**Negative:**
- Caddy brings significant binary size increase (~15-20MB)
- Caddy's JSON config is verbose compared to Caddyfile
- Some Caddy modules may pull in unwanted dependencies

**Trade-offs:**
- Using Caddy's native handlers for security headers (via JSON config) vs custom Go middleware
  - Decision: Use both - Caddy handlers for production, Go middleware for tests
- Health endpoint under `/_vibewarden/` namespace to avoid conflicts

**Follow-up work:**
- Epic #3 (Auth) will add Kratos middleware to the chain
- Epic #4 (Rate Limiting) will add rate limiter middleware
- Epic #5 (Structured Logs) will add `proxy.started` event emission
- Epic #6 (CLI) will add full `serve` subcommand with config loading

---

## ADR-005: CLI Pivot — Project Scaffolding and Management Tool

**Date**: 2026-03-26
**Issue**: #6
**Status**: Accepted

### Context

Epic #6 originally conceived the CLI as a runtime tool (`vibewarden start`, `vibewarden version`). After re-evaluation, the CLI has been **pivoted** to be a **project scaffolding and management tool**.

The target user is a vibe coder — someone using AI coding agents (Claude Code, Cursor, Windsurf) to build apps. They delegate understanding to their AI agent. The AI agent needs context to work correctly with VibeWarden.

**The killer feature is AI agent context generation.** When you run `vibewarden init`, it generates:
- `CLAUDE.md` (or appends a VibeWarden section) for Claude Code users
- `.cursor/rules` for Cursor users
- `AGENTS.md` for generic AI tools

These files tell the AI: "This app is behind VibeWarden. Auth headers are X-User-Id, X-User-Email, X-User-Verified. Don't implement auth. Add public paths to vibewarden.yaml."

This means the vibe coder can say "add a protected endpoint" and the AI knows exactly how to do it without the user explaining the architecture.

### Decision

Implement the CLI as a scaffolding and management tool with the following command structure:

#### Command Tree

```
vibewarden
├── init                     # Initialize VibeWarden in a project
│   ├── --force             # Overwrite existing files
│   ├── --skip-docker       # Skip docker-compose generation
│   ├── --skip-agent        # Skip AI agent context generation
│   └── --app-port <port>   # Upstream app port (default: 3000)
├── add
│   ├── auth                # Add authentication (Kratos config)
│   ├── rate-limiting       # Enable rate limiting
│   ├── tls                 # Add TLS configuration
│   │   └── --provider <p>  # letsencrypt, self-signed, external
│   ├── admin               # Enable admin API
│   └── metrics             # Enable Prometheus metrics
├── dev                     # Start local dev environment (docker compose up)
│   └── --detach / -d       # Run in background
├── status                  # Show status of VibeWarden services
├── doctor                  # Diagnose common issues
├── logs                    # Pretty-print structured JSON logs
│   ├── --follow / -f       # Follow log output
│   ├── --filter <type>     # Filter by event type
│   └── --json              # Output raw JSON (no pretty-print)
├── secret
│   └── generate            # Generate cryptographically secure secrets
│       └── --length <n>    # Secret length (default: 32)
├── validate                # Validate vibewarden.yaml
├── context
│   └── refresh             # Regenerate AI agent context files
├── serve                   # (existing) Start the sidecar directly
└── version                 # (existing) Print version
```

#### Architecture Overview

The CLI commands fall into three categories:

1. **Scaffolding commands** (`init`, `add`, `context refresh`): Generate files from templates
2. **Operational commands** (`dev`, `status`, `doctor`, `logs`): Manage local dev environment
3. **Utility commands** (`secret generate`, `validate`, `serve`, `version`): Standalone utilities

#### Domain Model Changes

No domain entities needed. The CLI operates on files and Docker, not on domain concepts. However, we introduce:

**Value Objects** (in `internal/cli/scaffold/`):
- `ProjectConfig` — detected project state (language, framework, existing files)
- `ScaffoldOptions` — options for file generation
- `FeatureConfig` — configuration for a VibeWarden feature (auth, rate-limiting, etc.)

#### Ports (Interfaces)

New interfaces in `internal/ports/`:

```go
// internal/ports/scaffold.go

// TemplateRenderer renders templates to files.
type TemplateRenderer interface {
    // Render executes a template with the given data and returns the result.
    Render(templateName string, data any) ([]byte, error)

    // RenderToFile renders a template and writes it to the given path.
    // Creates parent directories if needed. Returns error if file exists and overwrite is false.
    RenderToFile(templateName string, data any, path string, overwrite bool) error
}

// ProjectDetector analyzes a project directory to detect its configuration.
type ProjectDetector interface {
    // Detect analyzes the directory and returns project configuration.
    Detect(dir string) (*ProjectConfig, error)
}

// FeatureToggler modifies vibewarden.yaml to enable/disable features.
type FeatureToggler interface {
    // Enable enables a feature in the config file.
    Enable(configPath string, feature string, opts map[string]any) error

    // IsEnabled checks if a feature is already enabled.
    IsEnabled(configPath string, feature string) (bool, error)
}
```

```go
// internal/ports/docker.go

// ComposeRunner manages Docker Compose operations.
type ComposeRunner interface {
    // Up starts services defined in docker-compose.yml.
    Up(ctx context.Context, projectDir string, detach bool) error

    // Down stops and removes containers.
    Down(ctx context.Context, projectDir string) error

    // Status returns the status of all services.
    Status(ctx context.Context, projectDir string) ([]ServiceStatus, error)

    // Logs streams logs from services.
    Logs(ctx context.Context, projectDir string, follow bool, service string) (io.ReadCloser, error)
}

// ServiceStatus represents the status of a Docker Compose service.
type ServiceStatus struct {
    Name    string
    State   string // running, stopped, etc.
    Health  string // healthy, unhealthy, starting
    Ports   []string
}
```

```go
// internal/ports/logprinter.go

// LogPrinter formats and prints structured log events.
type LogPrinter interface {
    // Print formats and prints a single log event.
    Print(event map[string]any) error

    // SetFilter sets event type filter.
    SetFilter(eventType string)
}
```

#### Adapters

**Template Adapter** (`internal/adapters/template/`):
- Uses Go's `text/template` (stdlib, no license concern)
- Templates embedded via `embed.FS`
- Implements `ports.TemplateRenderer`

**Docker Adapter** (`internal/adapters/docker/`):
- Shells out to `docker compose` CLI (no library dependency)
- Implements `ports.ComposeRunner`

**Log Printer Adapter** (`internal/adapters/logprint/`):
- Uses `github.com/fatih/color` for colorized output (MIT license)
- Implements `ports.LogPrinter`

**YAML Adapter** (`internal/adapters/yamlmod/`):
- Uses `gopkg.in/yaml.v3` (MIT license, already indirect dependency)
- Implements `ports.FeatureToggler`
- Preserves comments and formatting when modifying YAML

#### Application Services

**Scaffold Service** (`internal/app/scaffold/`):
```go
// Service orchestrates project scaffolding operations.
type Service struct {
    renderer  ports.TemplateRenderer
    detector  ports.ProjectDetector
    toggler   ports.FeatureToggler
}

// Init initializes VibeWarden in a project directory.
func (s *Service) Init(ctx context.Context, dir string, opts InitOptions) error

// AddFeature enables a feature in an existing VibeWarden project.
func (s *Service) AddFeature(ctx context.Context, dir string, feature string, opts map[string]any) error

// RefreshContext regenerates AI agent context files.
func (s *Service) RefreshContext(ctx context.Context, dir string) error
```

**DevEnv Service** (`internal/app/devenv/`):
```go
// Service manages the local development environment.
type Service struct {
    compose ports.ComposeRunner
}

// Start starts the dev environment.
func (s *Service) Start(ctx context.Context, dir string, detach bool) error

// Stop stops the dev environment.
func (s *Service) Stop(ctx context.Context, dir string) error

// Status returns the status of all services.
func (s *Service) Status(ctx context.Context, dir string) ([]ports.ServiceStatus, error)
```

**Doctor Service** (`internal/app/doctor/`):
```go
// Service runs diagnostic checks.
type Service struct {
    compose ports.ComposeRunner
}

// Check represents a single diagnostic check.
type Check struct {
    Name    string
    Status  CheckStatus // pass, warn, fail
    Message string
    Fix     string // suggested fix command or action
}

// Run executes all diagnostic checks.
func (s *Service) Run(ctx context.Context, dir string) ([]Check, error)
```

#### File Layout

```
internal/
├── cli/
│   ├── cmd/
│   │   ├── root.go           # Root cobra command
│   │   ├── init.go           # vibewarden init
│   │   ├── add.go            # vibewarden add (parent)
│   │   ├── add_auth.go       # vibewarden add auth
│   │   ├── add_ratelimit.go  # vibewarden add rate-limiting
│   │   ├── add_tls.go        # vibewarden add tls
│   │   ├── add_admin.go      # vibewarden add admin
│   │   ├── add_metrics.go    # vibewarden add metrics
│   │   ├── dev.go            # vibewarden dev
│   │   ├── status.go         # vibewarden status
│   │   ├── doctor.go         # vibewarden doctor
│   │   ├── logs.go           # vibewarden logs
│   │   ├── secret.go         # vibewarden secret (parent)
│   │   ├── secret_generate.go# vibewarden secret generate
│   │   ├── validate.go       # vibewarden validate
│   │   └── context.go        # vibewarden context refresh
│   └── templates/
│       ├── embed.go          # embed.FS for templates
│       ├── docker-compose.yml.tmpl
│       ├── vibewarden.yaml.tmpl
│       ├── env.example.tmpl
│       ├── claude.md.tmpl    # CLAUDE.md VibeWarden section
│       ├── cursor_rules.tmpl # .cursor/rules
│       └── agents.md.tmpl    # AGENTS.md
├── ports/
│   ├── scaffold.go           # TemplateRenderer, ProjectDetector, FeatureToggler
│   ├── docker.go             # ComposeRunner
│   └── logprinter.go         # LogPrinter
├── adapters/
│   ├── template/
│   │   ├── renderer.go       # Template rendering adapter
│   │   └── renderer_test.go
│   ├── docker/
│   │   ├── compose.go        # Docker Compose adapter
│   │   └── compose_test.go
│   ├── logprint/
│   │   ├── printer.go        # Log pretty-printer adapter
│   │   └── printer_test.go
│   └── yamlmod/
│       ├── toggler.go        # YAML modifier adapter
│       └── toggler_test.go
├── app/
│   ├── scaffold/
│   │   ├── service.go        # Scaffold service
│   │   ├── service_test.go
│   │   ├── detector.go       # Project detector implementation
│   │   └── detector_test.go
│   ├── devenv/
│   │   ├── service.go        # Dev environment service
│   │   └── service_test.go
│   └── doctor/
│       ├── service.go        # Doctor service
│       ├── service_test.go
│       └── checks.go         # Individual check implementations
```

Note: `cmd/vibewarden/main.go` remains the entrypoint but delegates to `internal/cli/cmd/root.go`. The existing `serve.go` moves into `internal/cli/cmd/serve.go`.

#### Template Content

**CLAUDE.md VibeWarden Section** (`claude.md.tmpl`):
```markdown
## VibeWarden Security Sidecar

This application is protected by VibeWarden, a security sidecar that handles:
- Authentication via Ory Kratos
- Rate limiting
- Security headers
- Structured logging

### Architecture

All requests go through VibeWarden. Your app receives authenticated requests with these headers:
- `X-User-Id`: Kratos identity ID (UUID)
- `X-User-Email`: User's email address
- `X-User-Verified`: "true" if email is verified

### What NOT to implement

- **Do NOT implement authentication** — VibeWarden handles it
- **Do NOT implement rate limiting** — VibeWarden handles it
- **Do NOT implement security headers** — VibeWarden handles it

### What to configure

When adding new endpoints:
1. **Protected endpoints**: No config needed — auth is enforced by default
2. **Public endpoints**: Add the path to `auth.public_paths` in `vibewarden.yaml`
3. **Exempt from rate limiting**: Add the path to `rate_limit.exempt_paths` in `vibewarden.yaml`

### Files

- `vibewarden.yaml` — VibeWarden configuration
- `docker-compose.yml` — Local dev environment (includes VibeWarden, Kratos, Postgres)

### Local development

```bash
vibewarden dev          # Start local dev environment
vibewarden status       # Check service status
vibewarden logs -f      # Stream logs
vibewarden doctor       # Diagnose issues
```

### Header contract

Your app should read these headers to identify the user:

```go
// Example: reading user from VibeWarden headers
userID := r.Header.Get("X-User-Id")
email := r.Header.Get("X-User-Email")
verified := r.Header.Get("X-User-Verified") == "true"
```
```

**Cursor Rules** (`.cursor/rules`):
```
# VibeWarden Security Sidecar Rules

## Authentication
- This app is behind VibeWarden — DO NOT implement authentication
- User identity comes from headers: X-User-Id, X-User-Email, X-User-Verified
- To make an endpoint public, add it to auth.public_paths in vibewarden.yaml

## Rate Limiting
- Rate limiting is handled by VibeWarden — DO NOT implement it
- To exempt a path, add it to rate_limit.exempt_paths in vibewarden.yaml

## Security Headers
- Security headers are added by VibeWarden — DO NOT add them in your app

## Configuration
- VibeWarden config is in vibewarden.yaml
- Local dev runs via docker-compose.yml
- Start dev: vibewarden dev
- Check status: vibewarden status
```

#### Sequence Diagrams

**Init Command Flow**:
1. User runs `vibewarden init`
2. Detector scans directory for existing files (docker-compose.yml, vibewarden.yaml, CLAUDE.md)
3. Detector infers app port from common patterns (package.json scripts, Dockerfile EXPOSE, etc.)
4. If files exist and --force not set, prompt or error
5. Render docker-compose.yml.tmpl with detected config
6. Render vibewarden.yaml.tmpl with defaults
7. Render .env.example with placeholder secrets
8. Detect AI tool (look for .cursor/, existing CLAUDE.md, etc.)
9. Generate appropriate AI context files
10. Print success message with next steps

**Add Feature Flow**:
1. User runs `vibewarden add auth`
2. Check vibewarden.yaml exists (error if not)
3. Check if feature already enabled (warn and exit if so)
4. Load vibewarden.yaml preserving comments
5. Modify YAML to enable feature with defaults
6. Write updated YAML
7. Regenerate AI context files (they reference enabled features)
8. Print success message

**Dev Command Flow**:
1. User runs `vibewarden dev`
2. Check docker-compose.yml exists
3. Shell out to `docker compose up` (with -d if --detach)
4. Stream output to terminal
5. On success, print service URLs

**Logs Command Flow**:
1. User runs `vibewarden logs -f`
2. Shell out to `docker compose logs -f vibewarden`
3. Parse each line as JSON
4. Pretty-print with colors: timestamp, level, event_type, ai_summary
5. If --filter set, skip non-matching event types
6. If --json set, pass through raw JSON

**Doctor Command Flow**:
1. User runs `vibewarden doctor`
2. Run checks in sequence:
   - Docker installed and running
   - docker-compose.yml exists
   - vibewarden.yaml valid
   - Services healthy (docker compose ps)
   - Upstream reachable
   - Kratos API responding
3. Print results with pass/warn/fail status
4. For failures, print suggested fix

#### Error Cases

| Error | Handling |
|-------|----------|
| `init` in non-empty dir without `--force` | Error: "Directory contains existing VibeWarden files. Use --force to overwrite." |
| `add` without prior `init` | Error: "vibewarden.yaml not found. Run 'vibewarden init' first." |
| `dev` without docker-compose.yml | Error: "docker-compose.yml not found. Run 'vibewarden init' first." |
| Docker not installed | Doctor check fails with: "Docker not found. Install from https://docker.com" |
| Docker daemon not running | Doctor check fails with: "Docker daemon not running. Start Docker Desktop." |
| Invalid vibewarden.yaml | `validate` prints YAML parse errors with line numbers |
| Service unhealthy | `status` shows health as "unhealthy", `doctor` suggests fix |

#### Test Strategy

**Unit Tests**:
- `internal/adapters/template/renderer_test.go` — template rendering with various data
- `internal/adapters/yamlmod/toggler_test.go` — YAML modification preserving comments
- `internal/adapters/logprint/printer_test.go` — log formatting
- `internal/app/scaffold/detector_test.go` — project detection from various project types
- `internal/app/scaffold/service_test.go` — scaffold service with mocked ports
- `internal/app/doctor/service_test.go` — doctor checks with mocked compose runner

**Integration Tests**:
- `internal/adapters/docker/compose_integration_test.go` — real Docker Compose operations
- End-to-end tests for `init` -> `dev` -> `status` -> `doctor` flow

Integration tests use build tag `//go:build integration`.

#### New Dependencies

| Library | Version | License | Purpose |
|---------|---------|---------|---------|
| github.com/fatih/color | latest | MIT | Colorized terminal output |

Note: `gopkg.in/yaml.v3` is already an indirect dependency via viper. We'll use it directly for YAML modification.

License verification for fatih/color:
```
$ curl -s https://raw.githubusercontent.com/fatih/color/master/LICENSE.md | head -5
The MIT License (MIT)

Copyright (c) 2013 Fatih Arslan
```

### Epic Split — Sub-Issues

This epic is split into 6 focused sub-issues:

| Issue | Title | Dependencies | Scope |
|-------|-------|--------------|-------|
| #6.1 | CLI scaffold + init command | None | Root command, init, templates, scaffold service |
| #6.2 | AI agent context generation | #6.1 | CLAUDE.md, .cursor/rules, AGENTS.md templates |
| #6.3 | Add commands for features | #6.1 | add auth/rate-limiting/tls/admin/metrics, YAML modifier |
| #6.4 | Dev + status + doctor commands | #6.1 | Docker Compose adapter, operational commands |
| #6.5 | Logs pretty-printer | #6.4 | Log printer adapter, logs command |
| #6.6 | Secret generate + validate + context refresh | #6.1, #6.2 | Utility commands |

### Consequences

**Positive:**
- CLI becomes the primary onboarding experience for vibe coders
- AI agent context is a unique differentiator — no other security tool does this
- Incremental `add` commands lower the barrier to adopting features
- `doctor` command reduces support burden by self-diagnosing issues
- Template-based generation allows easy updates to generated files

**Negative:**
- CLI is now a hard dependency for the recommended workflow (users can still manually write config)
- Shelling out to Docker Compose instead of using Docker API adds external dependency
- AI context files may drift from actual config if user edits vibewarden.yaml manually

**Trade-offs:**
- Using `docker compose` CLI vs Docker Go SDK: CLI is simpler, SDK would avoid shell dependency
  - Decision: Use CLI — target users already have Docker installed, SDK adds significant complexity
- Embedding templates vs external template files: Embedding ensures single-binary distribution
  - Decision: Embed via `embed.FS`
- Appending to existing CLAUDE.md vs separate file: Appending integrates better with existing context
  - Decision: Append a clearly-marked section, detect existing section to avoid duplicates

**Follow-up work:**
- Future: `vibewarden upgrade` command to update generated files when VibeWarden is updated
- Future: `vibewarden eject` to convert to manual configuration
- Future: Interactive `init` wizard with prompts
