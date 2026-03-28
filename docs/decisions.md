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

---

## ADR-006: Add User App Service to Generated docker-compose.yml

**Date**: 2026-03-28
**Issue**: #279
**Status**: Accepted

### Context

The generated `docker-compose.yml` currently includes the VibeWarden sidecar and Kratos
(when auth is enabled), but not the user's own application. This forces users to manually
add their app service to the compose file, which defeats the "single config file" goal
of VibeWarden. The user should only need to maintain `vibewarden.yaml` and their app source.

This is part of Epic #277 (generate entire runtime stack from vibewarden.yaml).

### Decision

Add an `app` section to `vibewarden.yaml` that configures how the user's application is
included in the generated Docker Compose file. Support two modes:

1. **Dev mode** (`VIBEWARDEN_PROFILE=dev` or `tls`): Build the app from local Dockerfile
2. **Prod mode** (`VIBEWARDEN_PROFILE=prod`): Pull a pre-built image from a registry

#### Domain Model Changes

No new domain entities. This is a config-driven template enhancement.

#### Config Struct Changes

Add `AppConfig` struct to `internal/config/config.go`:

```go
// AppConfig configures the user's application in the generated Docker Compose.
// Either Build or Image should be set, depending on whether the user wants
// to build from source (dev) or use a pre-built image (prod).
type AppConfig struct {
    // Build is the Docker build context path (e.g., "." for current directory).
    // Used in dev/tls profiles.
    Build string `mapstructure:"build"`

    // Image is the Docker image reference (e.g., "ghcr.io/org/myapp:latest").
    // Used in prod profile. Can be overridden via VIBEWARDEN_APP_IMAGE env var.
    Image string `mapstructure:"image"`
}
```

Add `App AppConfig` field to the `Config` struct, after `Upstream`:

```go
type Config struct {
    Server   ServerConfig   `mapstructure:"server"`
    Upstream UpstreamConfig `mapstructure:"upstream"`
    App      AppConfig      `mapstructure:"app"`  // NEW
    TLS      TLSConfig      `mapstructure:"tls"`
    // ... rest unchanged
}
```

#### Ports (Interfaces)

No new interfaces required. The existing `ports.TemplateRenderer` interface is sufficient.

#### Adapters

No new adapters. The existing template adapter handles the rendering.

#### Application Service

The existing `internal/app/generate/Service.Generate()` method continues to pass the
full `*config.Config` to the template renderer. No changes to the service logic.

#### File Layout

Files to modify:

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `AppConfig` struct and `App` field to `Config` |
| `internal/config/templates/docker-compose.yml.tmpl` | Add app service with build/image logic |
| `internal/cli/templates/vibewarden.yaml.tmpl` | Add `app.build: .` section |
| `internal/app/generate/service_test.go` | Add tests for app service generation |

No new files required.

#### Template Changes

Update `internal/config/templates/docker-compose.yml.tmpl`:

1. Add the user's app service **before** the vibewarden service
2. Use Go template conditionals to select `build:` vs `image:` based on the configured values
3. Add `VIBEWARDEN_APP_IMAGE` env var override for prod mode
4. Add healthcheck to the app service
5. Update vibewarden service to `depends_on` the app with a healthcheck condition
6. Change `VIBEWARDEN_UPSTREAM_HOST` to point to the app container instead of `host.docker.internal`

The template logic for build vs image selection:

```
{{- if .App.Build }}
    build:
      context: {{ .App.Build }}
{{- else if .App.Image }}
    image: ${VIBEWARDEN_APP_IMAGE:-{{ .App.Image }}}
{{- end }}
```

Key design points:

- **Profile-agnostic template**: The template does not check `VIBEWARDEN_PROFILE`. Instead,
  it renders based on what is configured in the YAML. Users set either `app.build` (for dev)
  or `app.image` (for prod), or both if they want to support both modes.
- **Image override via env var**: In prod, `VIBEWARDEN_APP_IMAGE` env var overrides the
  configured image, enabling CI/CD to inject a specific image tag without modifying YAML.
- **Healthcheck**: The app service includes a default healthcheck that curls `localhost:<upstream_port>/health`.
  Users can override this by providing their own compose file via `overrides.compose_file`.
- **Network**: The app joins the `vibewarden` network so all services can communicate.

#### Sequence

1. User runs `vibewarden init`
2. `vibewarden.yaml` is generated with `app.build: .` (for dev workflow)
3. User runs `vibew dev` (which calls `vibewarden generate` internally)
4. `vibewarden generate` reads `vibewarden.yaml` and renders `docker-compose.yml.tmpl`
5. Template checks if `App.Build` is set:
   - If set: render `build: context: {{ .App.Build }}`
   - If not set but `App.Image` is set: render `image: ${VIBEWARDEN_APP_IMAGE:-{{ .App.Image }}}`
   - If neither set: no app service is rendered (graceful degradation)
6. Generated `docker-compose.yml` includes the app service before vibewarden
7. vibewarden service's `VIBEWARDEN_UPSTREAM_HOST` points to `app` container
8. `docker compose up` starts the full stack

#### Error Cases

| Error | Handling |
|-------|----------|
| Both `app.build` and `app.image` set | Valid — `app.build` takes precedence (dev mode) |
| Neither `app.build` nor `app.image` set | No app service rendered; vibewarden falls back to `host.docker.internal` (existing behavior) |
| `app.build` path does not exist | Docker Compose fails at build time with clear error |
| `app.image` not found in registry | Docker Compose fails at pull time with clear error |
| App container fails healthcheck | `depends_on` condition keeps vibewarden waiting; `docker compose logs app` shows failure |

#### Test Strategy

**Unit Tests** (in `internal/app/generate/service_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_AppService_BuildMode` | Config with `App.Build` renders app service with `build:` |
| `TestGenerate_AppService_ImageMode` | Config with `App.Image` renders app service with `image:` |
| `TestGenerate_AppService_BothSet` | Config with both set renders `build:` (build takes precedence) |
| `TestGenerate_AppService_NeitherSet` | Config with neither set does not render app service |
| `TestGenerate_AppService_DependsOn` | Vibewarden service `depends_on` app when app is rendered |
| `TestGenerate_AppService_UpstreamHost` | `VIBEWARDEN_UPSTREAM_HOST=app` when app service is rendered |

**Integration Tests** (in `internal/app/generate/service_integration_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Integration_AppService_DevMode` | Full render with app.build, validates compose YAML |
| `TestGenerate_Integration_AppService_ProdMode` | Full render with app.image, validates compose YAML |

Tests should parse the generated YAML and verify:
- The `app` service is present with correct build/image settings
- The `vibewarden` service has `depends_on.app.condition: service_healthy`
- The `VIBEWARDEN_UPSTREAM_HOST` environment variable is set to `app`

#### New Dependencies

None. This feature uses existing template rendering infrastructure.

### Consequences

**Positive:**
- User's app is now part of the generated stack — truly single-config workflow
- Dev mode builds from source, prod mode pulls from registry — both workflows supported
- `VIBEWARDEN_APP_IMAGE` env var enables CI/CD to inject image tags
- Backwards compatible — if `app` section is absent, behavior is unchanged

**Negative:**
- Users must have a `Dockerfile` for dev mode to work (reasonable assumption for Docker users)
- Default healthcheck assumes `/health` endpoint exists; users may need to override
- Adding more sections to `vibewarden.yaml` increases config surface area

**Trade-offs:**
- Using `build:` context vs `dockerfile:` path: context is simpler and matches most project layouts
- Healthcheck via curl vs custom command: curl is universal; users can override if needed
- Build precedence over image when both set: matches dev-first workflow expectation

---

## ADR-007: Add Plugin-Dependent Services to Generated docker-compose.yml (OpenBao, Redis)

**Date**: 2026-03-28
**Issue**: #281
**Status**: Accepted

### Context

Epic #277 establishes that `vibewarden generate` should produce the entire runtime stack from
`vibewarden.yaml`. Currently, when plugins like `secrets` (OpenBao) or `rate-limiting` with
Redis backend are enabled, users must manually add those infrastructure services to their
compose file. This defeats the single-config-file goal.

This ADR designs the automatic inclusion of plugin-dependent services:
- **OpenBao** when `secrets.enabled: true`
- **Redis** when `rate_limit.store: redis`

Key requirements from Epic #277:
- No secrets on disk — dev mode uses hardcoded defaults, prod mode uses OpenBao
- Dev credentials are embedded in templates; prod credentials come from OpenBao
- Seed containers in dev mode populate secrets for testing

### Decision

Extend the `docker-compose.yml.tmpl` template to conditionally include OpenBao and Redis
services based on the `secrets` and `rate_limit` config sections. Add seed containers for
dev mode that populate OpenBao with the secrets defined in `secrets.inject`.

#### Domain Model Changes

No new domain entities. This is a config-driven template enhancement.

#### Ports (Interfaces)

No new interfaces required. The existing `ports.TemplateRenderer` interface is sufficient.

#### Adapters

No new adapters. The existing template adapter handles the rendering.

#### Application Service

The existing `internal/app/generate/Service.Generate()` method continues to pass the
full `*config.Config` to the template renderer. Two new helper functions are added to
support the template:

```go
// internal/app/generate/helpers.go

// NeedsOpenBao returns true if the config requires an OpenBao service in the generated compose.
func NeedsOpenBao(cfg *config.Config) bool {
    return cfg.Secrets.Enabled
}

// NeedsRedis returns true if the config requires a Redis service in the generated compose.
func NeedsRedis(cfg *config.Config) bool {
    return cfg.RateLimit.Store == "redis"
}

// NeedsSeedSecrets returns true if dev mode should seed OpenBao with demo secrets.
// This is true when secrets.enabled is true AND secrets.inject has at least one entry.
func NeedsSeedSecrets(cfg *config.Config) bool {
    if !cfg.Secrets.Enabled {
        return false
    }
    return len(cfg.Secrets.Inject.Headers) > 0 || len(cfg.Secrets.Inject.Env) > 0
}
```

These helpers are registered as template functions so the template can call them:

```go
// In the template adapter, register these as FuncMap entries
funcMap := template.FuncMap{
    "needsOpenBao":     func(cfg *config.Config) bool { return generate.NeedsOpenBao(cfg) },
    "needsRedis":       func(cfg *config.Config) bool { return generate.NeedsRedis(cfg) },
    "needsSeedSecrets": func(cfg *config.Config) bool { return generate.NeedsSeedSecrets(cfg) },
}
```

#### File Layout

Files to modify:

| File | Change |
|------|--------|
| `internal/app/generate/helpers.go` | New file with helper functions |
| `internal/app/generate/helpers_test.go` | Unit tests for helper functions |
| `internal/adapters/template/renderer.go` | Register helper functions in FuncMap |
| `internal/config/templates/docker-compose.yml.tmpl` | Add OpenBao, Redis, seed-secrets services |
| `internal/config/templates/seed-secrets.sh.tmpl` | New embedded script template for seeding |
| `internal/app/generate/service.go` | Generate seed-secrets.sh when needed |
| `internal/app/generate/service_test.go` | Add tests for new service generation |

New files:

| File | Purpose |
|------|---------|
| `internal/app/generate/helpers.go` | Helper functions for template logic |
| `internal/app/generate/helpers_test.go` | Tests for helpers |
| `internal/config/templates/seed-secrets.sh.tmpl` | Script to seed demo secrets into OpenBao |

#### Template Changes

##### OpenBao Service

Add to `docker-compose.yml.tmpl` when `secrets.enabled: true`:

```yaml
{{- if .Secrets.Enabled }}
  openbao:
    image: quay.io/openbao/openbao:2.2.0
    restart: unless-stopped
    cap_add:
      - IPC_LOCK
    environment:
      # Dev mode: in-memory storage, root token generated per run
      BAO_DEV_ROOT_TOKEN_ID: ${OPENBAO_DEV_ROOT_TOKEN:-dev-root-token}
      BAO_DEV_LISTEN_ADDRESS: "0.0.0.0:8200"
    ports:
      - "8200:8200"
    networks:
      - vibewarden
    healthcheck:
      test: ["CMD-SHELL", "BAO_ADDR=http://127.0.0.1:8200 bao status"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 5s
{{- end }}
```

##### Seed Secrets Service

Add when `secrets.enabled: true` AND `secrets.inject` has entries:

```yaml
{{- if and .Secrets.Enabled (or (len .Secrets.Inject.Headers) (len .Secrets.Inject.Env)) }}
  seed-secrets:
    image: quay.io/openbao/openbao:2.2.0
    environment:
      BAO_ADDR: http://openbao:8200
      BAO_TOKEN: ${OPENBAO_DEV_ROOT_TOKEN:-dev-root-token}
    volumes:
      - ./.vibewarden/generated/seed-secrets.sh:/seed-secrets.sh:ro
    entrypoint: sh
    command: /seed-secrets.sh
    depends_on:
      openbao:
        condition: service_healthy
    networks:
      - vibewarden
    restart: "no"
{{- end }}
```

##### Redis Service

Add when `rate_limit.store: redis`:

```yaml
{{- if eq .RateLimit.Store "redis" }}
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: ["redis-server", "--appendonly", "yes"]
    volumes:
      - redis-data:/data
    networks:
      - vibewarden
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
{{- end }}
```

##### VibeWarden Service Updates

Update the `vibewarden` service's `depends_on` to include the new services:

```yaml
  vibewarden:
    # ... existing config ...
    depends_on:
{{- if or .App.Build .App.Image }}
      app:
        condition: service_healthy
{{- end }}
{{- if .Auth.Enabled }}
      kratos:
        condition: service_healthy
{{- end }}
{{- if and .Secrets.Enabled (or (len .Secrets.Inject.Headers) (len .Secrets.Inject.Env)) }}
      seed-secrets:
        condition: service_completed_successfully
{{- end }}
{{- if .Secrets.Enabled }}
{{- if not (or (len .Secrets.Inject.Headers) (len .Secrets.Inject.Env)) }}
      openbao:
        condition: service_healthy
{{- end }}
{{- end }}
{{- if eq .RateLimit.Store "redis" }}
      redis:
        condition: service_healthy
{{- end }}
    environment:
      # ... existing env vars ...
{{- if .Secrets.Enabled }}
      VIBEWARDEN_SECRETS_OPENBAO_ADDRESS: http://openbao:8200
      VIBEWARDEN_SECRETS_OPENBAO_AUTH_TOKEN: ${OPENBAO_DEV_ROOT_TOKEN:-dev-root-token}
{{- end }}
{{- if eq .RateLimit.Store "redis" }}
      VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS: redis:6379
{{- end }}
```

##### Volumes Section

Add to volumes section:

```yaml
volumes:
{{- if .Auth.Enabled }}
  kratos-db-data:
{{- end }}
{{- if .TLS.Enabled }}
  vibewarden-data:
{{- end }}
{{- if eq .RateLimit.Store "redis" }}
  redis-data:
{{- end }}
```

#### Seed Script Template

Create `internal/config/templates/seed-secrets.sh.tmpl`:

```bash
#!/usr/bin/env sh
# seed-secrets.sh — Generated by VibeWarden to seed demo secrets into OpenBao.
# Do not edit manually — re-run `vibewarden generate` to regenerate.

set -eu

echo "Waiting for OpenBao to be ready..."
until bao status >/dev/null 2>&1; do
  sleep 1
done

echo "Enabling KV v2 secrets engine at secret/ ..."
bao secrets enable -path=secret -version=2 kv 2>/dev/null || true

echo "Seeding demo secrets..."

{{- range .Secrets.Inject.Headers }}
# Header injection: {{ .Header }}
bao kv put {{ $.Secrets.OpenBao.MountPath }}/{{ .SecretPath }} \
  {{ .SecretKey }}="demo-value-for-{{ .SecretKey }}"
{{- end }}

{{- range .Secrets.Inject.Env }}
# Env injection: {{ .EnvVar }}
bao kv put {{ $.Secrets.OpenBao.MountPath }}/{{ .SecretPath }} \
  {{ .SecretKey }}="demo-value-for-{{ .SecretKey }}"
{{- end }}

echo "Done — OpenBao secrets seeded successfully."
```

#### Generate Service Changes

Update `internal/app/generate/Service.Generate()` to also generate `seed-secrets.sh` when needed:

```go
// After generating docker-compose.yml...

// Generate seed-secrets.sh if secrets plugin is enabled and has inject entries.
if cfg.Secrets.Enabled && (len(cfg.Secrets.Inject.Headers) > 0 || len(cfg.Secrets.Inject.Env) > 0) {
    seedPath := filepath.Join(outputDir, "seed-secrets.sh")
    if err := s.renderer.RenderToFile("seed-secrets.sh.tmpl", cfg, seedPath, true); err != nil {
        return fmt.Errorf("rendering seed-secrets.sh: %w", err)
    }
    // Make the script executable
    if err := os.Chmod(seedPath, 0o750); err != nil {
        return fmt.Errorf("setting seed-secrets.sh permissions: %w", err)
    }
}
```

#### Sequence

1. User configures `secrets.enabled: true` and/or `rate_limit.store: redis` in `vibewarden.yaml`
2. User optionally configures `secrets.inject.headers` or `secrets.inject.env` entries
3. User runs `vibewarden generate`
4. Generate service reads config and renders templates:
   - `docker-compose.yml.tmpl` with conditional OpenBao/Redis/seed services
   - `seed-secrets.sh.tmpl` if inject entries exist
5. Generated files written to `.vibewarden/generated/`:
   - `docker-compose.yml` (includes openbao, redis, seed-secrets as needed)
   - `seed-secrets.sh` (if inject entries exist, executable)
6. User runs `docker compose up`
7. Startup order enforced by `depends_on`:
   - postgres, kratos-db (if auth enabled)
   - kratos, openbao, redis (parallel, with healthchecks)
   - seed-secrets (waits for openbao healthy)
   - vibewarden (waits for seed-secrets completed, or openbao healthy if no seed)
   - app (parallel with vibewarden)
8. VibeWarden connects to OpenBao/Redis using container DNS names

#### Error Cases

| Error | Handling |
|-------|----------|
| OpenBao fails to start | `depends_on` blocks vibewarden; logs show OpenBao error |
| Redis fails to start | `depends_on` blocks vibewarden; logs show Redis error |
| seed-secrets fails | `depends_on` with `service_completed_successfully` blocks vibewarden |
| OpenBao unavailable at runtime | Secrets plugin logs error; behavior depends on `secrets.health` config |
| Redis unavailable at runtime | Rate limiter falls back to memory if `rate_limit.redis.fallback: true` |
| `secrets.inject` empty but `secrets.enabled: true` | No seed-secrets service; openbao still runs; vibewarden depends on openbao directly |

#### Prod Mode Considerations

The design above focuses on dev mode (in-memory OpenBao, root token). For prod mode:

1. **AppRole auth**: The `secrets.openbao.auth.method: approle` config is already supported
   in the existing secrets plugin. The generated compose uses `${OPENBAO_DEV_ROOT_TOKEN}`
   env var, which can be overridden for prod via `.env` file or environment.

2. **External OpenBao**: If the user has an existing OpenBao cluster, they can:
   - Set `secrets.openbao.address` to point to their cluster
   - Use `overrides.compose_file` to provide a custom compose without the openbao service
   - Or simply not use `vibewarden generate` and manage compose manually

3. **Persistent storage**: The dev-mode OpenBao is in-memory. For prod persistence:
   - Add a volume mount for OpenBao data (future enhancement)
   - Or use external OpenBao/Vault (recommended for prod)

4. **Seed script**: The seed-secrets service is dev-mode only. It seeds demo values.
   In prod, secrets should be provisioned via Terraform, CI/CD, or manual `bao` commands.

The template does not currently differentiate between dev and prod profiles. This is
intentional — the same compose works for both, with env var overrides controlling behavior.
A future enhancement could add profile-aware templates if needed.

#### Test Strategy

**Unit Tests** (in `internal/app/generate/helpers_test.go`):

| Test | Description |
|------|-------------|
| `TestNeedsOpenBao_Enabled` | Returns true when `secrets.enabled: true` |
| `TestNeedsOpenBao_Disabled` | Returns false when `secrets.enabled: false` |
| `TestNeedsRedis_StoreRedis` | Returns true when `rate_limit.store: redis` |
| `TestNeedsRedis_StoreMemory` | Returns false when `rate_limit.store: memory` |
| `TestNeedsRedis_StoreEmpty` | Returns false when `rate_limit.store` is empty |
| `TestNeedsSeedSecrets_WithHeaders` | Returns true when inject.headers is non-empty |
| `TestNeedsSeedSecrets_WithEnv` | Returns true when inject.env is non-empty |
| `TestNeedsSeedSecrets_NoInject` | Returns false when inject is empty |
| `TestNeedsSeedSecrets_SecretsDisabled` | Returns false when secrets.enabled is false |

**Template Tests** (in `internal/app/generate/service_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_OpenBaoService_WhenSecretsEnabled` | OpenBao service present in compose |
| `TestGenerate_OpenBaoService_WhenSecretsDisabled` | OpenBao service absent |
| `TestGenerate_RedisService_WhenStoreRedis` | Redis service present |
| `TestGenerate_RedisService_WhenStoreMemory` | Redis service absent |
| `TestGenerate_SeedSecrets_WhenInjectConfigured` | seed-secrets service present |
| `TestGenerate_SeedSecrets_WhenNoInject` | seed-secrets service absent |
| `TestGenerate_SeedSecretsScript_Created` | seed-secrets.sh file created |
| `TestGenerate_SeedSecretsScript_Executable` | seed-secrets.sh has 0750 permissions |
| `TestGenerate_DependsOn_OpenBao` | vibewarden depends_on openbao when secrets enabled |
| `TestGenerate_DependsOn_SeedSecrets` | vibewarden depends_on seed-secrets when inject configured |
| `TestGenerate_DependsOn_Redis` | vibewarden depends_on redis when store is redis |
| `TestGenerate_VibewardenEnv_OpenBao` | VIBEWARDEN_SECRETS_OPENBAO_* env vars set |
| `TestGenerate_VibewardenEnv_Redis` | VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS env var set |
| `TestGenerate_Volumes_Redis` | redis-data volume present when redis enabled |

**Integration Tests** (in `internal/app/generate/service_integration_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Integration_FullStack` | All plugins enabled, validate complete compose |
| `TestGenerate_Integration_SecretsOnly` | Only secrets enabled, validate compose structure |
| `TestGenerate_Integration_RedisOnly` | Only redis enabled, validate compose structure |

Tests should parse the generated YAML and verify:
- Correct services are present/absent based on config
- `depends_on` chains are correct
- Environment variables point to correct container names
- Volumes are declared when needed

#### New Dependencies

None. This feature uses existing template rendering infrastructure and the OpenBao/Redis
images are pulled at runtime by Docker Compose.

### Consequences

**Positive:**
- Plugin-dependent infrastructure is now part of the generated stack
- Zero manual compose editing for common use cases
- Dev mode "just works" with demo secrets seeded automatically
- Dependency ordering ensures correct startup sequence
- Environment variable overrides enable prod mode without template changes

**Negative:**
- Generated compose grows more complex with more conditional services
- Seed script embeds "demo-value-for-X" placeholders that are meaningless in prod
- No differentiation between dev/prod profiles in the template itself

**Trade-offs:**
- Dev-mode root token vs AppRole: Root token is simpler for dev; prod should use AppRole
- In-memory OpenBao vs persistent: Acceptable for dev; prod should use external cluster
- Single compose vs profile-separated: Single is simpler; profiles could be added later
- Seed script templated vs static: Templated allows customization based on inject config

---

## ADR-008: Add Observability Profile to Generated docker-compose.yml

**Date**: 2026-03-28
**Issue**: #282
**Status**: Accepted

### Context

Epic #277 establishes that `vibewarden generate` should produce the entire runtime stack from
`vibewarden.yaml`. The observability stack (Prometheus, Grafana, Loki, Promtail) is currently
hand-crafted in the `observability/` directory with configs that must be manually copied.

This ADR designs the automatic generation of observability infrastructure:
- Prometheus for metrics collection (scrapes VibeWarden's `/_vibewarden/metrics` endpoint)
- Grafana for visualization (pre-provisioned with datasources and VibeWarden dashboard)
- Loki for log aggregation
- Promtail for log collection (scrapes Docker container logs)

The observability services are placed under a Docker Compose profile (`observability`) so they
only start when the user explicitly requests them via `COMPOSE_PROFILES=observability`.

### Decision

Add an `observability` config section to `vibewarden.yaml` and generate all observability
config files from templates based on the existing working configs in `observability/`.

#### Domain Model Changes

No new domain entities. This is a config-driven template enhancement.

#### Ports (Interfaces)

No new interfaces required. The existing `ports.TemplateRenderer` interface is sufficient.

#### Adapters

No new adapters. The existing template adapter handles the rendering.

#### Config Additions

Add `ObservabilityConfig` to `internal/config/config.go`:

```go
// ObservabilityConfig holds settings for the optional observability stack.
// When enabled, vibewarden generate produces Prometheus, Grafana, Loki, and
// Promtail configs under .vibewarden/generated/observability/.
type ObservabilityConfig struct {
    // Enabled toggles generation of the observability stack (default: false).
    Enabled bool `mapstructure:"enabled"`

    // GrafanaPort is the host port Grafana binds to (default: 3001).
    // This avoids conflict with common app ports like 3000.
    GrafanaPort int `mapstructure:"grafana_port"`

    // PrometheusPort is the host port Prometheus binds to (default: 9090).
    PrometheusPort int `mapstructure:"prometheus_port"`

    // LokiPort is the host port Loki binds to (default: 3100).
    LokiPort int `mapstructure:"loki_port"`

    // RetentionDays is how long Loki retains log data (default: 7).
    RetentionDays int `mapstructure:"retention_days"`
}
```

Add to the main `Config` struct:

```go
// Observability configures the optional observability stack (Prometheus,
// Grafana, Loki, Promtail) generated under the "observability" compose profile.
Observability ObservabilityConfig `mapstructure:"observability"`
```

Add defaults in `Load()`:

```go
v.SetDefault("observability.enabled", false)
v.SetDefault("observability.grafana_port", 3001)
v.SetDefault("observability.prometheus_port", 9090)
v.SetDefault("observability.loki_port", 3100)
v.SetDefault("observability.retention_days", 7)
```

#### Application Service Changes

Add a new helper function in `internal/app/generate/helpers.go`:

```go
// NeedsObservability returns true if the config requires the observability
// stack (Prometheus, Grafana, Loki, Promtail) in the generated compose.
func NeedsObservability(cfg *config.Config) bool {
    return cfg.Observability.Enabled
}
```

Update `internal/app/generate/Service.Generate()` to generate observability configs:

```go
// Generate observability configs when enabled.
if cfg.Observability.Enabled {
    if err := s.generateObservability(cfg, outputDir); err != nil {
        return fmt.Errorf("generating observability configs: %w", err)
    }
}
```

Add a new method `generateObservability()`:

```go
// generateObservability writes all observability config files to
// <outputDir>/observability/.
func (s *Service) generateObservability(cfg *config.Config, outputDir string) error {
    obsDir := filepath.Join(outputDir, "observability")

    // Create directory structure
    dirs := []string{
        filepath.Join(obsDir, "prometheus"),
        filepath.Join(obsDir, "grafana", "provisioning", "datasources"),
        filepath.Join(obsDir, "grafana", "provisioning", "dashboards"),
        filepath.Join(obsDir, "grafana", "dashboards"),
        filepath.Join(obsDir, "loki"),
        filepath.Join(obsDir, "promtail"),
    }
    for _, dir := range dirs {
        if err := os.MkdirAll(dir, permDir); err != nil {
            return fmt.Errorf("creating directory %q: %w", dir, err)
        }
    }

    // Render Prometheus config
    if err := s.renderer.RenderToFile(
        "observability/prometheus.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "prometheus", "prometheus.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering prometheus.yml: %w", err)
    }

    // Render Grafana datasources
    if err := s.renderer.RenderToFile(
        "observability/grafana-datasources.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "grafana", "provisioning", "datasources", "datasources.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering grafana datasources: %w", err)
    }

    // Render Grafana dashboard provisioner
    if err := s.renderer.RenderToFile(
        "observability/grafana-dashboards.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "grafana", "provisioning", "dashboards", "dashboards.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering grafana dashboard provisioner: %w", err)
    }

    // Copy Grafana dashboard JSON (static, not a template)
    dashboardJSON, err := templates.FS.ReadFile("observability/vibewarden-dashboard.json")
    if err != nil {
        return fmt.Errorf("reading embedded dashboard JSON: %w", err)
    }
    dashboardPath := filepath.Join(obsDir, "grafana", "dashboards", "vibewarden.json")
    if err := os.WriteFile(dashboardPath, dashboardJSON, permConfig); err != nil {
        return fmt.Errorf("writing dashboard JSON: %w", err)
    }

    // Render Loki config
    if err := s.renderer.RenderToFile(
        "observability/loki-config.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "loki", "loki-config.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering loki-config.yml: %w", err)
    }

    // Render Promtail config
    if err := s.renderer.RenderToFile(
        "observability/promtail-config.yml.tmpl",
        cfg,
        filepath.Join(obsDir, "promtail", "promtail-config.yml"),
        true,
    ); err != nil {
        return fmt.Errorf("rendering promtail-config.yml: %w", err)
    }

    return nil
}
```

#### File Layout

Files to modify:

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `ObservabilityConfig` struct and field |
| `internal/app/generate/helpers.go` | Add `NeedsObservability()` function |
| `internal/app/generate/helpers_test.go` | Add tests for `NeedsObservability()` |
| `internal/app/generate/service.go` | Add `generateObservability()` method |
| `internal/app/generate/service_test.go` | Add tests for observability generation |
| `internal/config/templates/docker-compose.yml.tmpl` | Add observability services under profile |

New template files (in `internal/config/templates/`):

| File | Purpose |
|------|---------|
| `observability/prometheus.yml.tmpl` | Prometheus scrape config |
| `observability/grafana-datasources.yml.tmpl` | Grafana datasource provisioning |
| `observability/grafana-dashboards.yml.tmpl` | Grafana dashboard provisioner config |
| `observability/vibewarden-dashboard.json` | Pre-built VibeWarden dashboard (static) |
| `observability/loki-config.yml.tmpl` | Loki storage and retention config |
| `observability/promtail-config.yml.tmpl` | Promtail Docker log scraping config |

#### Template Specifications

##### prometheus.yml.tmpl

```yaml
# Prometheus configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.

global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'vibewarden'
    metrics_path: '/_vibewarden/metrics'
    static_configs:
      - targets: ['vibewarden:{{ .Server.Port }}']
        labels:
          instance: 'vibewarden-sidecar'

  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']
```

##### grafana-datasources.yml.tmpl

```yaml
# Grafana datasources — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.

apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    uid: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false

  - name: Loki
    type: loki
    uid: loki
    access: proxy
    url: http://loki:3100
    isDefault: false
    editable: false
    jsonData:
      maxLines: 1000
```

##### grafana-dashboards.yml.tmpl

```yaml
# Grafana dashboard provisioner — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.

apiVersion: 1

providers:
  - name: 'VibeWarden'
    orgId: 1
    folder: ''
    folderUid: ''
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards
```

##### loki-config.yml.tmpl

```yaml
# Loki configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.
#
# Storage: local filesystem (single-node, not for production clustering).
# Retention: {{ .Observability.RetentionDays }} days.

auth_enabled: false

server:
  http_listen_port: 3100
  grpc_listen_port: 9096
  log_level: warn

common:
  instance_addr: 127.0.0.1
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: "2020-10-24"
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

limits_config:
  retention_period: {{ mul .Observability.RetentionDays 24 }}h

compactor:
  working_directory: /loki/compactor
  retention_enabled: true
  delete_request_store: filesystem

ruler:
  alertmanager_url: http://localhost:9093
```

##### promtail-config.yml.tmpl

```yaml
# Promtail configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.
#
# Scrapes Docker container logs and ships them to Loki.
# VibeWarden's structured JSON logs are parsed so that each field becomes
# queryable in Grafana.

server:
  http_listen_port: 9080
  grpc_listen_port: 0
  log_level: warn

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://loki:3100/loki/api/v1/push

scrape_configs:
  - job_name: docker
    docker_sd_configs:
      - host: unix:///var/run/docker.sock
        refresh_interval: 10s
        filters:
          - name: status
            values: ["running"]

    relabel_configs:
      - source_labels: [__meta_docker_container_name]
        regex: "/(.*)"
        target_label: container
      - source_labels: [__meta_docker_container_label_com_docker_compose_service]
        target_label: service
      - source_labels: [__meta_docker_container_label_com_docker_compose_project]
        target_label: compose_project

    pipeline_stages:
      - json:
          expressions:
            schema_version: schema_version
            event_type: event_type
            ai_summary: ai_summary
            level: level
            time: time

      - labels:
          schema_version:
          event_type:
          level:

      - timestamp:
          source: time
          format: RFC3339Nano
          fallback_formats:
            - RFC3339
            - UnixMs

      - structured_metadata:
          ai_summary:
```

##### vibewarden-dashboard.json

This file is copied verbatim from `observability/grafana/dashboards/vibewarden.json`.
It is a static JSON file (not a template) containing the pre-built VibeWarden dashboard
with panels for:
- Request rate and latency
- Error rates by status code
- Rate limiting metrics
- Auth middleware metrics
- Log explorer with VibeWarden structured log fields

#### Docker Compose Template Changes

Add observability services to `docker-compose.yml.tmpl` under the `observability` profile:

```yaml
{{- if .Observability.Enabled }}

  prometheus:
    image: prom/prometheus:v3.2.1
    profiles:
      - observability
    restart: unless-stopped
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.console.libraries=/usr/share/prometheus/console_libraries'
      - '--web.console.templates=/usr/share/prometheus/consoles'
    ports:
      - "{{ .Observability.PrometheusPort }}:9090"
    volumes:
      - ./.vibewarden/generated/observability/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    networks:
      - vibewarden
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:9090/-/healthy"]
      interval: 10s
      timeout: 5s
      retries: 5

  loki:
    image: grafana/loki:3.4.3
    profiles:
      - observability
    restart: unless-stopped
    command: -config.file=/etc/loki/loki-config.yml
    ports:
      - "{{ .Observability.LokiPort }}:3100"
    volumes:
      - ./.vibewarden/generated/observability/loki/loki-config.yml:/etc/loki/loki-config.yml:ro
      - loki-data:/loki
    networks:
      - vibewarden
    healthcheck:
      test: ["CMD-SHELL", "wget -q --spider http://localhost:3100/ready || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 5

  promtail:
    image: grafana/promtail:3.4.3
    profiles:
      - observability
    restart: unless-stopped
    command: -config.file=/etc/promtail/promtail-config.yml
    volumes:
      - ./.vibewarden/generated/observability/promtail/promtail-config.yml:/etc/promtail/promtail-config.yml:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
    networks:
      - vibewarden
    depends_on:
      loki:
        condition: service_healthy

  grafana:
    image: grafana/grafana:11.5.2
    profiles:
      - observability
    restart: unless-stopped
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: "Admin"
      GF_AUTH_DISABLE_LOGIN_FORM: "true"
      GF_SECURITY_ADMIN_PASSWORD: "admin"
    ports:
      - "{{ .Observability.GrafanaPort }}:3000"
    volumes:
      - ./.vibewarden/generated/observability/grafana/provisioning:/etc/grafana/provisioning:ro
      - ./.vibewarden/generated/observability/grafana/dashboards:/var/lib/grafana/dashboards:ro
      - grafana-data:/var/lib/grafana
    networks:
      - vibewarden
    depends_on:
      prometheus:
        condition: service_healthy
      loki:
        condition: service_healthy
    healthcheck:
      test: ["CMD-SHELL", "wget -q --spider http://localhost:3000/api/health || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 5
{{- end }}
```

Add observability volumes to the volumes section:

```yaml
volumes:
{{- /* existing volumes */ -}}
{{- if .Observability.Enabled }}
  prometheus-data:
  loki-data:
  grafana-data:
{{- end }}
```

Update the header comment to include observability services:

```yaml
{{- if .Observability.Enabled }}
#   prometheus  — Metrics collection (profile: observability)
#   loki        — Log aggregation (profile: observability)
#   promtail    — Log collector (profile: observability)
#   grafana     — Visualization (profile: observability)
{{- end }}
```

#### Sequence

1. User sets `observability.enabled: true` in `vibewarden.yaml`
2. User optionally customizes ports: `grafana_port`, `prometheus_port`, `loki_port`
3. User optionally sets `retention_days` for log retention
4. User runs `vibewarden generate`
5. Generate service reads config and:
   - Creates `.vibewarden/generated/observability/` directory structure
   - Renders Prometheus config with correct VibeWarden port
   - Renders Grafana datasources pointing to prometheus/loki containers
   - Renders Grafana dashboard provisioner config
   - Copies the static VibeWarden dashboard JSON
   - Renders Loki config with retention period
   - Renders Promtail config for Docker log scraping
   - Renders docker-compose.yml with observability services under profile
6. User starts the stack:
   - Without observability: `docker compose up`
   - With observability: `COMPOSE_PROFILES=observability docker compose up`
7. When observability profile is active:
   - Prometheus scrapes VibeWarden at `http://vibewarden:8080/_vibewarden/metrics`
   - Promtail tails Docker container logs and ships to Loki
   - Loki ingests logs with VibeWarden structured metadata
   - Grafana serves the pre-provisioned dashboard at `http://localhost:3001`

#### Generated Output Structure

When `observability.enabled: true`:

```
.vibewarden/generated/
  docker-compose.yml
  kratos/
    ...
  observability/
    prometheus/
      prometheus.yml
    grafana/
      provisioning/
        datasources/
          datasources.yml
        dashboards/
          dashboards.yml
      dashboards/
        vibewarden.json
    loki/
      loki-config.yml
    promtail/
      promtail-config.yml
```

#### Error Cases

| Error | Handling |
|-------|----------|
| Port conflict on GrafanaPort | Docker reports port binding error; user adjusts `grafana_port` |
| Port conflict on PrometheusPort | Docker reports port binding error; user adjusts `prometheus_port` |
| Docker socket not accessible | Promtail fails to start; logs show permission error |
| Loki fails healthcheck | Promtail/Grafana `depends_on` keeps them waiting |
| Prometheus fails healthcheck | Grafana `depends_on` keeps it waiting |
| Invalid retention_days (0 or negative) | Loki config invalid; service fails to start |
| Template rendering fails | Generate returns error; no partial output |

#### Validation

Add validation in `Config.Validate()`:

```go
// observability validation
if c.Observability.Enabled {
    if c.Observability.GrafanaPort <= 0 || c.Observability.GrafanaPort > 65535 {
        errs = append(errs, fmt.Sprintf(
            "observability.grafana_port %d is invalid; must be 1-65535",
            c.Observability.GrafanaPort,
        ))
    }
    if c.Observability.PrometheusPort <= 0 || c.Observability.PrometheusPort > 65535 {
        errs = append(errs, fmt.Sprintf(
            "observability.prometheus_port %d is invalid; must be 1-65535",
            c.Observability.PrometheusPort,
        ))
    }
    if c.Observability.LokiPort <= 0 || c.Observability.LokiPort > 65535 {
        errs = append(errs, fmt.Sprintf(
            "observability.loki_port %d is invalid; must be 1-65535",
            c.Observability.LokiPort,
        ))
    }
    if c.Observability.RetentionDays <= 0 {
        errs = append(errs, fmt.Sprintf(
            "observability.retention_days %d is invalid; must be > 0",
            c.Observability.RetentionDays,
        ))
    }
}
```

#### Template Function for Multiplication

The Loki template needs `mul` to calculate retention hours from days. Add to the template
FuncMap in `internal/adapters/template/renderer.go`:

```go
funcMap := template.FuncMap{
    "mul": func(a, b int) int { return a * b },
}
```

#### Test Strategy

**Unit Tests** (in `internal/app/generate/helpers_test.go`):

| Test | Description |
|------|-------------|
| `TestNeedsObservability_Enabled` | Returns true when `observability.enabled: true` |
| `TestNeedsObservability_Disabled` | Returns false when `observability.enabled: false` |
| `TestNeedsObservability_Default` | Returns false when observability section missing |

**Config Validation Tests** (in `internal/config/config_test.go`):

| Test | Description |
|------|-------------|
| `TestValidate_Observability_InvalidGrafanaPort` | Catches port < 1 or > 65535 |
| `TestValidate_Observability_InvalidPrometheusPort` | Catches port < 1 or > 65535 |
| `TestValidate_Observability_InvalidLokiPort` | Catches port < 1 or > 65535 |
| `TestValidate_Observability_InvalidRetentionDays` | Catches retention <= 0 |
| `TestValidate_Observability_ValidConfig` | Passes with valid values |

**Template Tests** (in `internal/app/generate/service_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Observability_WhenEnabled` | Observability dir and files created |
| `TestGenerate_Observability_WhenDisabled` | No observability dir created |
| `TestGenerate_Observability_PrometheusConfig` | Prometheus targets VibeWarden port |
| `TestGenerate_Observability_LokiRetention` | Loki retention matches config |
| `TestGenerate_Observability_GrafanaDatasources` | Datasources point to correct URLs |
| `TestGenerate_Observability_Dashboard` | Dashboard JSON copied correctly |
| `TestGenerate_Observability_ComposeServices` | Prometheus/Loki/Promtail/Grafana present |
| `TestGenerate_Observability_ComposeProfiles` | Services have `profiles: [observability]` |
| `TestGenerate_Observability_ComposeVolumes` | prometheus-data/loki-data/grafana-data volumes |
| `TestGenerate_Observability_ComposePorts` | Ports match config values |
| `TestGenerate_Observability_ComposeDependsOn` | Dependency chain correct |

**Integration Tests** (in `internal/app/generate/service_integration_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Integration_Observability` | Full render, validate all observability files |
| `TestGenerate_Integration_ObservabilityWithAuth` | Observability + Auth, validate compose |

Tests should:
- Parse generated YAML configs and verify structure
- Verify ports are substituted correctly
- Verify retention calculation is correct
- Verify Grafana dashboard JSON is valid JSON
- Verify compose services have correct profile annotation

#### New Dependencies

None. All images are pulled at runtime by Docker Compose:

| Image | Version | License | Purpose |
|-------|---------|---------|---------|
| `prom/prometheus` | v3.2.1 | Apache 2.0 | Metrics collection |
| `grafana/grafana` | 11.5.2 | AGPL 3.0 (runtime only) | Visualization |
| `grafana/loki` | 3.4.3 | AGPL 3.0 (runtime only) | Log aggregation |
| `grafana/promtail` | 3.4.3 | Apache 2.0 | Log collection |

Note: Grafana and Loki are AGPL 3.0 licensed. Since VibeWarden does not embed or link
against these components (they are pulled as Docker images at runtime), the AGPL does
not apply to VibeWarden's codebase. This is the standard usage pattern documented by
Grafana Labs for self-hosted deployments.

### Consequences

**Positive:**
- Full observability stack generated from single config file
- Zero manual setup for metrics, logs, and dashboards
- Compose profile keeps observability optional (not started by default)
- Pre-provisioned Grafana means instant dashboard access on first run
- Retention configurable to manage disk usage
- Ports configurable to avoid conflicts

**Negative:**
- Additional complexity in the generate service
- Dashboard JSON is embedded and may drift from the `observability/` reference
- Promtail requires Docker socket access (security consideration)
- Generated configs are dev-focused; prod deployments may need tuning

**Trade-offs:**
- Compose profile vs separate compose file: Profile keeps single file; separation would be cleaner
- Embedded dashboard vs generated: Embedded is simpler; generated would allow customization
- Single-node Loki vs cluster: Single-node is sufficient for dev; prod should use external stack
- Anonymous Grafana auth vs login: Anonymous is simpler for dev; prod should enable auth
