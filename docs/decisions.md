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

---

## ADR-009: Generate .env Template with Secure Credential Management

**Status:** Accepted
**Issue:** #283
**Date:** 2026-03-28

### Context

VibeWarden generates a Docker Compose stack that includes services requiring credentials:
Postgres (for Kratos), Kratos secrets (cookie + cipher), Grafana admin password, and
OpenBao dev root token. The current implementation references environment variables
in docker-compose.yml.tmpl (e.g., `${KRATOS_DB_PASSWORD}`) but does not generate these
values or any `.env` file.

The security requirements from issue #283 are strict:

1. **No secrets in .env, ever** — environment files are too easily committed to git
2. **Every `vibewarden generate` run creates fresh random credentials** — no hardcoded defaults
3. **Credentials stored in a sealed file** (`.credentials`, mode 0600, gitignored)
4. **Init container seeds OpenBao from .credentials** — services read from OpenBao at runtime
5. **Prod compose generation fails** if `secrets.enabled: false` — production requires OpenBao

This design eliminates the risk of accidentally committing credentials while still providing
a zero-friction dev experience.

### Decision

Implement secure credential generation with the following architecture:

#### Domain Model Changes

Add a new value object to `internal/domain/generate/`:

```go
// Package generate contains domain types for the stack generation subsystem.
package generate

// GeneratedCredentials holds the randomly generated credentials for a single
// `vibewarden generate` run. It is a value object — immutable after construction.
// The domain layer does not know how these are persisted or consumed.
type GeneratedCredentials struct {
    // PostgresPassword is the password for the Kratos Postgres database (32 chars).
    PostgresPassword string

    // KratosCookieSecret is the Kratos session cookie signing secret (32 chars).
    KratosCookieSecret string

    // KratosCipherSecret is the Kratos data encryption secret (32 chars).
    KratosCipherSecret string

    // GrafanaAdminPassword is the Grafana admin password (24 chars).
    GrafanaAdminPassword string

    // OpenBaoDevRootToken is the OpenBao dev mode root token (32 chars).
    OpenBaoDevRootToken string
}
```

#### Ports (Interfaces)

Add a new outbound port to `internal/ports/credentials.go`:

```go
// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
    "context"

    "github.com/vibewarden/vibewarden/internal/domain/generate"
)

// CredentialGenerator generates cryptographically secure random credentials.
// Implementations must use crypto/rand for all randomness.
type CredentialGenerator interface {
    // Generate creates a new set of random credentials.
    Generate(ctx context.Context) (*generate.GeneratedCredentials, error)
}

// CredentialStore persists and retrieves generated credentials.
// The store is responsible for file permissions and atomic writes.
type CredentialStore interface {
    // Write persists credentials to the backing store. Overwrites any existing data.
    Write(ctx context.Context, creds *generate.GeneratedCredentials, outputDir string) error

    // Read loads previously generated credentials. Returns os.ErrNotExist if none.
    Read(ctx context.Context, outputDir string) (*generate.GeneratedCredentials, error)
}
```

#### Adapters

**CredentialGenerator adapter** (`internal/adapters/credentials/generator.go`):

```go
// Package credentials provides adapters for credential generation and storage.
package credentials

import (
    "context"
    "crypto/rand"
    "encoding/base64"
    "fmt"

    "github.com/vibewarden/vibewarden/internal/domain/generate"
)

// Generator implements ports.CredentialGenerator using crypto/rand.
type Generator struct{}

// NewGenerator creates a Generator.
func NewGenerator() *Generator {
    return &Generator{}
}

// Generate creates cryptographically secure random credentials.
func (g *Generator) Generate(ctx context.Context) (*generate.GeneratedCredentials, error) {
    postgres, err := randomAlphanumeric(32)
    if err != nil {
        return nil, fmt.Errorf("generating postgres password: %w", err)
    }

    cookie, err := randomAlphanumeric(32)
    if err != nil {
        return nil, fmt.Errorf("generating kratos cookie secret: %w", err)
    }

    cipher, err := randomAlphanumeric(32)
    if err != nil {
        return nil, fmt.Errorf("generating kratos cipher secret: %w", err)
    }

    grafana, err := randomAlphanumeric(24)
    if err != nil {
        return nil, fmt.Errorf("generating grafana admin password: %w", err)
    }

    bao, err := randomAlphanumeric(32)
    if err != nil {
        return nil, fmt.Errorf("generating openbao root token: %w", err)
    }

    return &generate.GeneratedCredentials{
        PostgresPassword:     postgres,
        KratosCookieSecret:   cookie,
        KratosCipherSecret:   cipher,
        GrafanaAdminPassword: grafana,
        OpenBaoDevRootToken:  bao,
    }, nil
}

// randomAlphanumeric generates a random alphanumeric string of the specified length.
func randomAlphanumeric(length int) (string, error) {
    // Generate extra bytes to account for base64 expansion, then trim.
    bytes := make([]byte, length)
    if _, err := rand.Read(bytes); err != nil {
        return "", err
    }
    // Use URL-safe base64 without padding for alphanumeric-ish output.
    encoded := base64.RawURLEncoding.EncodeToString(bytes)
    if len(encoded) < length {
        return "", fmt.Errorf("encoded string too short: got %d, want %d", len(encoded), length)
    }
    return encoded[:length], nil
}
```

**CredentialStore adapter** (`internal/adapters/credentials/store.go`):

```go
// Package credentials provides adapters for credential generation and storage.
package credentials

import (
    "bufio"
    "context"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/vibewarden/vibewarden/internal/domain/generate"
)

const (
    credentialsFileName = ".credentials"
    // permSecretFile is the permission mode for the credentials file.
    // Only owner can read/write — group and world have no access.
    permSecretFile = os.FileMode(0o600)
    permDir        = os.FileMode(0o750)
)

// Store implements ports.CredentialStore using a dotenv-formatted file.
type Store struct{}

// NewStore creates a Store.
func NewStore() *Store {
    return &Store{}
}

// Write persists credentials to .credentials in dotenv format.
// The file is created with mode 0600 (owner read/write only).
func (s *Store) Write(ctx context.Context, creds *generate.GeneratedCredentials, outputDir string) error {
    if err := os.MkdirAll(outputDir, permDir); err != nil {
        return fmt.Errorf("creating output directory: %w", err)
    }

    path := filepath.Join(outputDir, credentialsFileName)

    content := fmt.Sprintf(`# Generated credentials — do not commit to version control.
# Re-run 'vibewarden generate' to regenerate with fresh values.
# Mode: 0600 (owner read/write only)

POSTGRES_PASSWORD=%s
KRATOS_SECRETS_COOKIE=%s
KRATOS_SECRETS_CIPHER=%s
GRAFANA_ADMIN_PASSWORD=%s
OPENBAO_DEV_ROOT_TOKEN=%s
`, creds.PostgresPassword, creds.KratosCookieSecret, creds.KratosCipherSecret,
        creds.GrafanaAdminPassword, creds.OpenBaoDevRootToken)

    if err := os.WriteFile(path, []byte(content), permSecretFile); err != nil {
        return fmt.Errorf("writing credentials file: %w", err)
    }

    return nil
}

// Read loads credentials from .credentials file.
func (s *Store) Read(ctx context.Context, outputDir string) (*generate.GeneratedCredentials, error) {
    path := filepath.Join(outputDir, credentialsFileName)

    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    values := make(map[string]string)
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        parts := strings.SplitN(line, "=", 2)
        if len(parts) == 2 {
            values[parts[0]] = parts[1]
        }
    }
    if err := scanner.Err(); err != nil {
        return nil, fmt.Errorf("reading credentials file: %w", err)
    }

    return &generate.GeneratedCredentials{
        PostgresPassword:     values["POSTGRES_PASSWORD"],
        KratosCookieSecret:   values["KRATOS_SECRETS_COOKIE"],
        KratosCipherSecret:   values["KRATOS_SECRETS_CIPHER"],
        GrafanaAdminPassword: values["GRAFANA_ADMIN_PASSWORD"],
        OpenBaoDevRootToken:  values["OPENBAO_DEV_ROOT_TOKEN"],
    }, nil
}
```

#### Application Service Changes

Update `internal/app/generate/service.go` to:

1. Accept `CredentialGenerator` and `CredentialStore` as dependencies
2. Generate credentials on every run
3. Write `.credentials` file
4. Write `.env.template` file (non-secret config only)
5. Update `seed-secrets.sh.tmpl` to read credentials from `.credentials`
6. Validate prod profile requires `secrets.enabled: true`

The service constructor becomes:

```go
// Service implements ports.ConfigGenerator using a ports.TemplateRenderer.
type Service struct {
    renderer   ports.TemplateRenderer
    credGen    ports.CredentialGenerator
    credStore  ports.CredentialStore
}

// NewService creates a generate Service with all dependencies.
func NewService(
    renderer ports.TemplateRenderer,
    credGen ports.CredentialGenerator,
    credStore ports.CredentialStore,
) *Service {
    return &Service{
        renderer:  renderer,
        credGen:   credGen,
        credStore: credStore,
    }
}
```

Add prod profile validation in `Generate()`:

```go
// Validate prod profile requirements.
if cfg.Profile == "prod" && !cfg.Secrets.Enabled {
    return fmt.Errorf("prod profile requires secrets.enabled: true (OpenBao is mandatory for production)")
}
```

#### Config Changes

Add `Profile` field to `internal/config/config.go`:

```go
type Config struct {
    // Profile selects the deployment profile: "dev", "tls", or "prod".
    // Affects TLS settings, credential handling, and validation rules.
    Profile string `mapstructure:"profile"`

    // ... existing fields ...
}
```

Set default in `Load()`:

```go
v.SetDefault("profile", "dev")
```

Add validation in `Validate()`:

```go
validProfiles := map[string]bool{"dev": true, "tls": true, "prod": true}
if !validProfiles[cfg.Profile] {
    return fmt.Errorf("profile must be 'dev', 'tls', or 'prod', got %q", cfg.Profile)
}
```

#### File Layout

New files:

```
internal/
  domain/
    generate/
      credentials.go              # GeneratedCredentials value object
  ports/
    credentials.go                # CredentialGenerator, CredentialStore interfaces
  adapters/
    credentials/
      generator.go                # crypto/rand implementation
      generator_test.go           # unit tests
      store.go                    # file-based storage
      store_test.go               # unit tests
  config/
    templates/
      env.template.tmpl           # .env.template template
      seed-secrets.sh.tmpl        # (update existing)
```

Generated output:

```
.vibewarden/generated/
  .credentials                    # mode 0600, contains all secrets
  .env.template                   # non-secret config, safe to commit
  docker-compose.yml              # references env vars from .credentials
  seed-secrets.sh                 # reads .credentials, seeds OpenBao
  kratos/...                      # unchanged
  observability/...               # unchanged
```

#### Templates

**.env.template.tmpl** (`internal/config/templates/env.template.tmpl`):

```
# VibeWarden Environment Template
# Generated by `vibewarden generate` — safe to commit.
# Copy to .env and customize values before running docker compose.
#
# IMPORTANT: Credentials are NOT stored here.
# They are in .credentials (mode 0600, gitignored).
# Run `vibew secret get <name>` to retrieve them.

# --------------------------------------------------------------------------
# Profile selection
# --------------------------------------------------------------------------

# Deployment profile: dev | tls | prod
VIBEWARDEN_PROFILE={{ .Profile }}

# --------------------------------------------------------------------------
# App image (prod profile only — dev uses build context)
# --------------------------------------------------------------------------
{{- if .App.Image }}

VIBEWARDEN_APP_IMAGE={{ .App.Image }}
{{- else }}

# VIBEWARDEN_APP_IMAGE=ghcr.io/your-org/your-app:latest
{{- end }}

# --------------------------------------------------------------------------
# Compose profiles (uncomment to enable optional services)
# --------------------------------------------------------------------------

# Enable observability stack (Prometheus, Grafana, Loki, Promtail)
# COMPOSE_PROFILES=observability
```

**seed-secrets.sh.tmpl** (update existing):

```bash
#!/usr/bin/env sh
# seed-secrets.sh — Generated by VibeWarden to seed credentials into OpenBao.
# Do not edit manually — re-run `vibewarden generate` to regenerate.

set -eu

CREDS_FILE="$(dirname "$0")/.credentials"

# Load credentials from .credentials file
if [ ! -f "$CREDS_FILE" ]; then
  echo "ERROR: $CREDS_FILE not found. Run 'vibewarden generate' first." >&2
  exit 1
fi

# Source the credentials file (dotenv format)
set -a
. "$CREDS_FILE"
set +a

echo "Waiting for OpenBao to be ready..."
until bao status >/dev/null 2>&1; do
  sleep 1
done

echo "Enabling KV v2 secrets engine at {{ .Secrets.OpenBao.MountPath }}/ ..."
bao secrets enable -path={{ .Secrets.OpenBao.MountPath }} -version=2 kv 2>/dev/null || true

echo "Seeding infrastructure credentials..."

# Postgres credentials
bao kv put {{ .Secrets.OpenBao.MountPath }}/infra/postgres \
  password="$POSTGRES_PASSWORD"

# Kratos secrets
bao kv put {{ .Secrets.OpenBao.MountPath }}/infra/kratos \
  cookie_secret="$KRATOS_SECRETS_COOKIE" \
  cipher_secret="$KRATOS_SECRETS_CIPHER"

# Grafana credentials
bao kv put {{ .Secrets.OpenBao.MountPath }}/infra/grafana \
  admin_password="$GRAFANA_ADMIN_PASSWORD"

# OpenBao root token (for reference)
bao kv put {{ .Secrets.OpenBao.MountPath }}/infra/openbao \
  root_token="$OPENBAO_DEV_ROOT_TOKEN"
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

**docker-compose.yml.tmpl changes**:

Update the kratos-db service to source password from env var (already done, but ensure the
seed-secrets container mounts .credentials):

```yaml
  seed-secrets:
    image: quay.io/openbao/openbao:2.2.0
    environment:
      BAO_ADDR: http://openbao:8200
      BAO_TOKEN: ${OPENBAO_DEV_ROOT_TOKEN}
    volumes:
      - ./.vibewarden/generated/seed-secrets.sh:/seed-secrets.sh:ro
      - ./.vibewarden/generated/.credentials:/.credentials:ro
    entrypoint: sh
    command: /seed-secrets.sh
    depends_on:
      openbao:
        condition: service_healthy
    networks:
      - vibewarden
    restart: "no"
```

Update kratos-db to read from .credentials via env_file:

```yaml
  kratos-db:
    image: postgres:17-alpine
    restart: unless-stopped
    env_file:
      - ./.vibewarden/generated/.credentials
    environment:
      POSTGRES_DB: kratos
      POSTGRES_USER: kratos
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    # ... rest unchanged
```

Similarly for kratos and grafana services.

#### Sequence

1. User runs `vibewarden generate`
2. Service loads `vibewarden.yaml` config
3. If `profile == "prod" && !secrets.enabled`, return error immediately
4. CredentialGenerator creates fresh random credentials (crypto/rand)
5. CredentialStore writes `.credentials` to `.vibewarden/generated/.credentials` (mode 0600)
6. TemplateRenderer writes `.env.template` to `.vibewarden/generated/.env.template`
7. TemplateRenderer writes `docker-compose.yml` with env_file references
8. TemplateRenderer writes `seed-secrets.sh` that sources `.credentials`
9. TemplateRenderer writes Kratos configs, observability configs (unchanged)
10. User runs `docker compose up`
11. OpenBao starts
12. seed-secrets container starts, waits for OpenBao, sources `.credentials`, seeds all values
13. kratos-db, kratos, grafana start with credentials from env_file
14. vibewarden starts after dependencies are healthy

#### Error Cases

| Error | Condition | Handling |
|-------|-----------|----------|
| `ErrProdRequiresSecrets` | `profile == "prod" && !secrets.enabled` | Return error, do not generate any files |
| `ErrCredentialGeneration` | crypto/rand failure | Return error with wrapped cause |
| `ErrCredentialWrite` | Filesystem error on .credentials | Return error, partial generation may occur |
| `ErrTemplateRender` | Template parsing/execution error | Return error with template name |

#### Test Strategy

**Unit Tests** (in `internal/adapters/credentials/generator_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerator_Generate_ReturnsUniqueValues` | Two calls return different credentials |
| `TestGenerator_Generate_CorrectLengths` | Each field has correct character length |
| `TestGenerator_Generate_Alphanumeric` | Output contains only URL-safe base64 chars |

**Unit Tests** (in `internal/adapters/credentials/store_test.go`):

| Test | Description |
|------|-------------|
| `TestStore_Write_CreatesFile` | File created at correct path |
| `TestStore_Write_FilePermissions` | File has mode 0600 |
| `TestStore_Write_DotenvFormat` | File contains valid KEY=VALUE lines |
| `TestStore_Read_ParsesCorrectly` | Roundtrip write then read matches |
| `TestStore_Read_NotExist` | Returns os.ErrNotExist when file missing |
| `TestStore_Read_IgnoresComments` | Comment lines are skipped |

**Unit Tests** (in `internal/app/generate/service_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_ProdProfile_RequiresSecrets` | Returns error when profile=prod, secrets.enabled=false |
| `TestGenerate_DevProfile_AllowsNoSecrets` | Succeeds when profile=dev, secrets.enabled=false |
| `TestGenerate_CredentialsWritten` | .credentials file created on every run |
| `TestGenerate_EnvTemplateWritten` | .env.template file created |
| `TestGenerate_EnvTemplateNoSecrets` | .env.template contains no passwords or tokens |
| `TestGenerate_SeedSecretsSourcesCredentials` | seed-secrets.sh includes sourcing logic |

**Integration Tests** (in `internal/app/generate/service_integration_test.go`):

| Test | Description |
|------|-------------|
| `TestGenerate_Integration_CredentialLifecycle` | Full generate, verify .credentials mode, verify compose refs |
| `TestGenerate_Integration_FreshCredentialsPerRun` | Two generate runs produce different credentials |

#### New Dependencies

None. Uses only Go standard library:
- `crypto/rand` (stdlib) — cryptographically secure random number generation
- `encoding/base64` (stdlib) — encoding random bytes to alphanumeric

### Consequences

**Positive:**
- Credentials are never stored in `.env` — eliminates accidental commit risk
- Fresh credentials on every `vibewarden generate` — no stale/shared secrets
- `.credentials` file mode 0600 — not readable by other users on the system
- Prod profile validation catches misconfiguration early
- `.env.template` provides documentation without exposing secrets
- Seamless dev experience — `docker compose up` just works after `vibewarden generate`

**Negative:**
- Additional complexity in the generate flow
- Users must re-run `vibewarden generate` after `docker compose down -v` to get fresh credentials
- `.credentials` file is plain text on disk — adequate for dev, not for prod (OpenBao handles prod)

**Trade-offs:**
- dotenv format for `.credentials` vs JSON/YAML: dotenv is simpler and compatible with shell sourcing
- Storing root token in `.credentials`: necessary for dev mode, prod uses AppRole auth instead
- env_file in compose vs environment: env_file keeps compose cleaner and avoids duplication

---

## ADR-010: Add `vibew secret get` and `vibew secret list` Commands

**Status:** Accepted
**Issue:** #286
**Date:** 2026-03-28

### Context

With the no-secrets-on-disk approach from ADR-009, credentials are randomly generated per
`vibewarden generate` run and seeded into OpenBao. Users need a simple way to retrieve
them for debugging or connecting external tools (e.g. database GUIs, API testing).

The existing `vibew secret` command group only has `vibew secret generate` for creating
new random tokens. We need retrieval commands that:

1. Support well-known aliases for common services (postgres, kratos, grafana, openbao)
2. Allow arbitrary OpenBao path queries (e.g. `demo/api-key`)
3. List all managed secret paths
4. Output in human-readable, JSON, or env-export formats
5. Fall back to the `.credentials` file when OpenBao is not running

### Decision

Add two new subcommands to the existing `vibew secret` command group:

- `vibew secret get <alias-or-path>` — retrieve credentials for a service or path
- `vibew secret list` — show all managed secret paths

#### Domain Model Changes

Add a new value object to `internal/domain/secret/`:

```go
// Package secret contains domain types for secret retrieval.
package secret

// RetrievedSecret holds the key/value pairs retrieved from a secret path.
// It is a value object — immutable after construction.
type RetrievedSecret struct {
    // Path is the original path or resolved alias that was queried.
    Path string

    // Alias is the well-known alias if one was used, or empty string.
    Alias string

    // Data holds the key/value pairs of the secret.
    Data map[string]string

    // Source indicates where the secret was retrieved from.
    Source SecretSource
}

// SecretSource indicates the origin of a retrieved secret.
type SecretSource string

const (
    // SourceOpenBao indicates the secret was retrieved from OpenBao.
    SourceOpenBao SecretSource = "openbao"

    // SourceCredentialsFile indicates the secret was retrieved from .credentials.
    SourceCredentialsFile SecretSource = "credentials_file"
)
```

Add a well-known alias resolver:

```go
// WellKnownAlias maps user-friendly names to OpenBao paths and credential file keys.
type WellKnownAlias struct {
    // Name is the alias (e.g. "postgres", "kratos").
    Name string

    // OpenBaoPath is the static KV path in OpenBao (e.g. "infra/postgres").
    OpenBaoPath string

    // DynamicRole is the database secret engine role name for dynamic credentials.
    // Empty string if this alias does not support dynamic credentials.
    DynamicRole string

    // CredentialsFileKeys maps .credentials file keys to output field names.
    // e.g. {"POSTGRES_PASSWORD": "password"}
    CredentialsFileKeys map[string]string

    // EnvPrefix is the prefix for --env output (e.g. "POSTGRES_").
    EnvPrefix string
}

// ResolveAlias returns the WellKnownAlias for the given name, or nil if not found.
func ResolveAlias(name string) *WellKnownAlias

// ListAliases returns all well-known aliases.
func ListAliases() []WellKnownAlias
```

#### Ports (Interfaces)

Add a new outbound port to `internal/ports/secrets.go` (extend existing file):

```go
// SecretRetriever provides read-only access to secrets from multiple sources.
// It tries OpenBao first, then falls back to the credentials file.
type SecretRetriever interface {
    // Get retrieves a secret by alias or path. Tries OpenBao first, then
    // falls back to the credentials file. Returns ErrSecretNotFound when
    // neither source has the secret.
    Get(ctx context.Context, aliasOrPath string) (*secret.RetrievedSecret, error)

    // List returns all managed secret paths from both sources.
    List(ctx context.Context) ([]string, error)
}
```

#### Adapters

**No new adapter files needed.** The application service will compose the existing adapters:

- `internal/adapters/openbao/adapter.go` — already implements `ports.SecretStore`
- `internal/adapters/credentials/store.go` — already implements `ports.CredentialStore`

The secret retrieval logic will live in the application service, which orchestrates
these two adapters with the fallback logic.

#### Application Service

Create `internal/app/secret/service.go`:

```go
// Package secret provides the application service for retrieving secrets.
package secret

import (
    "context"
    "errors"
    "fmt"
    "os"

    "github.com/vibewarden/vibewarden/internal/domain/secret"
    "github.com/vibewarden/vibewarden/internal/ports"
)

// ErrSecretNotFound is returned when a secret cannot be found in any source.
var ErrSecretNotFound = errors.New("secret not found")

// ErrNoSourceAvailable is returned when neither OpenBao nor the credentials file is available.
var ErrNoSourceAvailable = errors.New("no secret source available: OpenBao is not running and .credentials file not found")

// Service implements secret retrieval with OpenBao-first, credentials-file fallback.
type Service struct {
    secretStore   ports.SecretStore    // may be nil if OpenBao is not configured
    credStore     ports.CredentialStore
    outputDir     string               // directory containing .credentials
}

// NewService creates a secret retrieval service.
// secretStore may be nil; in that case, only the credentials file is used.
func NewService(
    secretStore ports.SecretStore,
    credStore ports.CredentialStore,
    outputDir string,
) *Service {
    return &Service{
        secretStore: secretStore,
        credStore:   credStore,
        outputDir:   outputDir,
    }
}

// Get retrieves a secret by alias or path.
func (s *Service) Get(ctx context.Context, aliasOrPath string) (*secret.RetrievedSecret, error)

// List returns all managed secret paths.
func (s *Service) List(ctx context.Context) ([]string, error)

// tryOpenBao attempts to retrieve a secret from OpenBao.
// Returns nil, nil when OpenBao is not available (not configured or health check fails).
func (s *Service) tryOpenBao(ctx context.Context, path string) (map[string]string, error)

// tryCredentialsFile attempts to retrieve a secret from the .credentials file.
// Returns nil, nil when the file does not exist.
func (s *Service) tryCredentialsFile(ctx context.Context, alias *secret.WellKnownAlias) (map[string]string, error)
```

#### File Layout

New files to create:

```
internal/
  domain/
    secret/
      secret.go              # RetrievedSecret, SecretSource value objects
      alias.go               # WellKnownAlias, ResolveAlias, ListAliases
      alias_test.go          # Unit tests for alias resolution
  app/
    secret/
      service.go             # Secret retrieval application service
      service_test.go        # Unit tests with mocked ports
  cli/
    cmd/
      secret_get.go          # `vibew secret get` command implementation
      secret_get_test.go     # CLI tests
      secret_list.go         # `vibew secret list` command implementation
      secret_list_test.go    # CLI tests
```

Modified files:

```
internal/
  ports/
    secrets.go               # Add SecretRetriever interface
  cli/
    cmd/
      secret.go              # Add get and list subcommands
```

#### Sequence

**`vibew secret get postgres` flow:**

1. CLI parses arguments, resolves output format (default/json/env)
2. CLI creates the SecretService with available adapters
3. CLI calls `service.Get(ctx, "postgres")`
4. Service checks if "postgres" is a well-known alias → yes, resolves to `WellKnownAlias`
5. Service checks if OpenBao is available:
   a. If secretStore is nil → skip to step 7
   b. Call `secretStore.Health(ctx)` → if error, skip to step 7
6. OpenBao available:
   a. If alias has DynamicRole, try `database/creds/<role>` first
   b. If dynamic fails or no DynamicRole, try static path `infra/postgres`
   c. Return `RetrievedSecret{Source: SourceOpenBao, Data: ...}`
7. OpenBao not available, try credentials file:
   a. Call `credStore.Read(ctx, outputDir)`
   b. If `os.ErrNotExist` → return `ErrNoSourceAvailable`
   c. Map `.credentials` keys to output fields using alias.CredentialsFileKeys
   d. Return `RetrievedSecret{Source: SourceCredentialsFile, Data: ...}`
8. CLI formats output based on --json or --env flag

**`vibew secret get demo/api-key` flow (arbitrary path):**

1. CLI parses arguments
2. Service checks if "demo/api-key" is a well-known alias → no
3. Service checks if OpenBao is available → if no, return `ErrNoSourceAvailable`
   (arbitrary paths cannot be resolved from .credentials)
4. Service calls `secretStore.Get(ctx, "demo/api-key")`
5. Return `RetrievedSecret{Source: SourceOpenBao, Data: ...}`

**`vibew secret list` flow:**

1. CLI calls `service.List(ctx)`
2. Service collects paths from both sources:
   a. All well-known alias paths
   b. If OpenBao available: `secretStore.List(ctx, "infra/")` and `secretStore.List(ctx, "app/")`
3. Deduplicate and sort paths
4. CLI prints paths (one per line, or JSON array with --json)

#### Well-Known Aliases

| Alias | OpenBao Static Path | Dynamic Role | .credentials Keys | Env Prefix |
|-------|---------------------|--------------|-------------------|------------|
| `postgres` | `infra/postgres` | `app-readwrite` | `POSTGRES_PASSWORD` | `POSTGRES_` |
| `kratos` | `infra/kratos` | — | `KRATOS_SECRETS_COOKIE`, `KRATOS_SECRETS_CIPHER` | `KRATOS_` |
| `grafana` | `infra/grafana` | — | `GRAFANA_ADMIN_PASSWORD` | `GRAFANA_` |
| `openbao` | — | — | `OPENBAO_DEV_ROOT_TOKEN` | `OPENBAO_` |

Note: `openbao` alias only reads from .credentials file (the root token is not stored in OpenBao itself).

#### Output Format Implementations

**Default (human-readable):**
```
postgres credentials (source: openbao):
  username: v-app-kX9mNp2q
  password: A3bC7dE9fG1hJ2kL4mN6pQ8rS0tU
  host:     localhost:5432
  database: vibewarden
```

**`--json`:**
```json
{"username":"v-app-kX9mNp2q","password":"A3bC7dE9fG1hJ2kL4mN6pQ8rS0tU","host":"localhost:5432","database":"vibewarden"}
```

**`--env`:**
```bash
export POSTGRES_USER=v-app-kX9mNp2q
export POSTGRES_PASSWORD=A3bC7dE9fG1hJ2kL4mN6pQ8rS0tU
```

#### Error Cases

| Error | When | User message |
|-------|------|--------------|
| `ErrNoSourceAvailable` | OpenBao not running AND .credentials not found | "No secret source available. Run 'vibewarden generate' to create credentials, or start the stack with 'vibewarden dev'." |
| `ErrSecretNotFound` | Path/alias not found in any available source | "Secret '<path>' not found." |
| OpenBao connection error | Network/auth failure | "Failed to connect to OpenBao: <error>. Falling back to .credentials file." (warn, then try fallback) |
| Invalid alias | User types unknown alias-like string | Treated as OpenBao path, then "Secret '<path>' not found in OpenBao. Use 'vibew secret list' to see available secrets." |

#### Test Strategy

**Unit tests (mocked ports):**

| Test | What it verifies |
|------|------------------|
| `TestResolveAlias_WellKnown` | All 4 aliases resolve correctly |
| `TestResolveAlias_Unknown` | Unknown returns nil |
| `TestService_Get_OpenBaoFirst` | OpenBao is queried before .credentials |
| `TestService_Get_FallbackToCredentials` | Falls back when OpenBao health fails |
| `TestService_Get_ArbitraryPath` | Non-alias paths go directly to OpenBao |
| `TestService_Get_ErrNoSourceAvailable` | Error when both sources unavailable |
| `TestService_List_MergesSources` | Combines aliases + OpenBao paths |
| `TestFormatHuman` | Human output formatting |
| `TestFormatJSON` | JSON output formatting |
| `TestFormatEnv` | Env export formatting |

**CLI integration tests:**

| Test | What it verifies |
|------|------------------|
| `TestSecretGet_HelpOutput` | Help text shows aliases and examples |
| `TestSecretGet_UnknownAlias` | Appropriate error message |
| `TestSecretList_HelpOutput` | Help text is correct |

#### New Dependencies

None. Uses only existing adapters and Go standard library.

### Consequences

**Positive:**
- Users can retrieve credentials without inspecting `.credentials` or OpenBao UI
- Machine-readable formats (`--json`, `--env`) enable scripting and tool integration
- Well-known aliases abstract away OpenBao path structure
- Fallback to .credentials works before `docker compose up`
- Consistent with the secret management model from ADR-009

**Negative:**
- Arbitrary OpenBao paths require OpenBao to be running (no fallback)
- `openbao` alias only works from .credentials (root token not in OpenBao)

**Trade-offs:**
- OpenBao-first vs credentials-first: OpenBao-first ensures dynamic credentials are fresh
- Alias abstraction vs direct paths: aliases are user-friendly but hide implementation details

---

## ADR-011: Migrate Demo App to Use Generated docker-compose.yml

**Date**: 2026-03-28
**Issue**: #284
**Status**: Accepted

### Context

This is the capstone story of Epic #277 (generate entire runtime stack from vibewarden.yaml).
The demo app at `examples/demo-app/` currently uses a hand-crafted `docker-compose.yml` with
~420 lines. All prerequisite stories are now complete:

- ADR-006 (#279): App service in generated compose
- ADR-007 (#281): Plugin-dependent services (OpenBao, Redis)
- ADR-008 (#282): Observability profile
- ADR-009 (#283): .env template with secure credential management
- ADR-010 (#286): `vibew secret get` command

The demo app should now showcase the intended workflow: commit only `vibewarden.yaml` and
let `vibewarden generate` produce everything else. This validates that the generation
system works end-to-end and serves as the canonical example for users.

### Decision

Migrate the demo app to use generated configuration:

1. **Update `vibewarden.yaml`** to exercise all features
2. **Remove hand-crafted `docker-compose.yml`** from version control
3. **Update Makefile** to generate before composing
4. **Keep seed scripts** (`seed-users.sh`, `seed-secrets.sh`) for demo data
5. **Deprecate the committed `observability/` directory** (configs are now generated)
6. **Update README** with new workflow

#### Domain Model Changes

No new domain entities. This is a configuration and workflow change.

#### Ports (Interfaces)

No new interfaces required.

#### Adapters

No new adapters required.

#### Application Service

No changes to the generate service. The existing `internal/app/generate/Service.Generate()`
handles all required functionality.

#### File Layout

**Files to delete:**

| File | Reason |
|------|--------|
| `examples/demo-app/docker-compose.yml` | Replaced by generated compose |
| `examples/demo-app/docker-compose.local-demo.yml` | Superseded by profile system |
| `examples/demo-app/docker-compose.prod.yml` | Superseded by profile system |
| `examples/demo-app/vibewarden.local-demo.yaml` | Superseded by single config |
| `examples/demo-app/vibewarden.prod.yaml` | Superseded by single config |
| `observability/prometheus/prometheus.yml` | Now generated from template |
| `observability/grafana/provisioning/datasources/prometheus.yml` | Now generated |
| `observability/grafana/provisioning/dashboards/dashboard.yml` | Now generated |
| `observability/grafana/dashboards/vibewarden.json` | Embedded in binary, generated on demand |
| `observability/loki/loki-config.yml` | Now generated |
| `observability/promtail/promtail-config.yml` | Now generated |

The entire `observability/` directory can be removed once the demo migration is complete.

**Files to keep:**

| File | Purpose |
|------|---------|
| `examples/demo-app/vibewarden.yaml` | Single source of truth for demo config |
| `examples/demo-app/.env.example` | Documents available env var overrides |
| `examples/demo-app/Dockerfile` | App build context |
| `examples/demo-app/main.go` | Demo app source |
| `examples/demo-app/main_test.go` | Demo app tests |
| `examples/demo-app/go.mod`, `go.sum` | Demo app dependencies |
| `examples/demo-app/static/` | Demo UI assets |
| `examples/demo-app/kratos/` | Kratos config overrides (identity schema, etc.) |
| `examples/demo-app/scripts/seed-users.sh` | Seeds demo identities into Kratos |
| `examples/demo-app/scripts/seed-secrets.sh` | Seeds demo secrets into OpenBao |
| `examples/demo-app/README.md` | Documentation |
| `examples/demo-app/CHALLENGE.md` | Challenge documentation |
| `examples/demo-app/MONITORING.md` | Monitoring documentation |
| `examples/demo-app/RECOVERY.md` | Recovery documentation |
| `examples/demo-app/.gitignore` | Ignores generated files |

**Files to modify:**

| File | Change |
|------|--------|
| `examples/demo-app/vibewarden.yaml` | Update to use new config structure with app, observability, etc. |
| `examples/demo-app/.env.example` | Update to match generated .env.template format |
| `examples/demo-app/.gitignore` | Add `.vibewarden/generated/` |
| `examples/demo-app/README.md` | Update with new workflow instructions |
| `Makefile` | Update `demo` and `demo-down` targets |

#### Updated vibewarden.yaml

The new `examples/demo-app/vibewarden.yaml` exercises all major features:

```yaml
# VibeWarden Demo App Configuration
# Single source of truth for the entire demo stack.
#
# Usage:
#   vibewarden generate
#   docker compose -f .vibewarden/generated/docker-compose.yml up
#
# Or simply: make demo

# Deployment profile: dev | tls | prod
profile: dev

# User application
app:
  # Build from local Dockerfile (dev mode)
  build: .
  # Image for production (overridable via VIBEWARDEN_APP_IMAGE)
  # image: ghcr.io/vibewarden/demo-app:latest

server:
  host: "0.0.0.0"
  port: 8080

upstream:
  host: app
  port: 3000

tls:
  enabled: false
  # For TLS profile, set via env vars:
  # VIBEWARDEN_TLS_ENABLED=true
  # VIBEWARDEN_TLS_PROVIDER=self-signed

kratos:
  public_url: "http://kratos:4433"
  admin_url: "http://kratos:4434"

auth:
  enabled: true
  public_paths:
    - "/"
    - "/public"
    - "/health"
    - "/profile"
    - "/static"
    - "/auth"
    - "/vuln"
  session_cookie_name: "ory_kratos_session"

rate_limit:
  enabled: true
  store: memory
  per_ip:
    requests_per_second: 5
    burst: 10
  per_user:
    requests_per_second: 10
    burst: 20
  trust_proxy_headers: false
  exempt_paths: []

log:
  level: "info"
  format: "json"

admin:
  enabled: false

metrics:
  enabled: true
  path_patterns:
    - "/"
    - "/public"
    - "/me"
    - "/headers"
    - "/spam"
    - "/health"

secrets:
  enabled: true
  provider: openbao
  openbao:
    address: http://openbao:8200
    auth:
      method: token
    mount_path: secret
  inject:
    headers:
      - secret_path: demo/api-key
        secret_key: token
        header: X-Demo-Api-Key
    env:
      - secret_path: demo/app-config
        secret_key: database_url
        env_var: DEMO_DATABASE_URL
      - secret_path: demo/app-config
        secret_key: session_secret
        env_var: DEMO_SESSION_SECRET
  cache_ttl: "5m"

security_headers:
  enabled: true
  hsts_max_age: 31536000
  hsts_include_subdomains: true
  hsts_preload: false
  content_type_nosniff: true
  frame_option: "DENY"
  content_security_policy: "default-src 'self'; style-src 'self'; script-src 'self' 'unsafe-inline'"
  referrer_policy: "strict-origin-when-cross-origin"

observability:
  enabled: true
  grafana_port: 3001
  prometheus_port: 9090
  loki_port: 3100
  retention_days: 7

# Override paths for demo-specific configs
overrides:
  # Use demo-specific Kratos config with seed users
  kratos_config: ""
  identity_schema: ""
```

#### Updated .gitignore

Add to `examples/demo-app/.gitignore`:

```
# Generated by vibewarden generate
.vibewarden/
```

#### Updated Makefile Targets

```makefile
# Start the full local demo stack
demo: ## Start the full local demo stack (https://localhost:8443, Grafana http://localhost:3001)
	cd examples/demo-app && \
	  ../../bin/vibewarden generate && \
	  COMPOSE_PROFILES=observability \
	  docker compose -f .vibewarden/generated/docker-compose.yml up -d
	@echo ""
	@echo "Demo stack is starting — wait ~30 s for all services to be healthy."
	@echo ""
	@echo "  App:        http://localhost:8080"
	@echo "  Grafana:    http://localhost:3001"
	@echo "  Prometheus: http://localhost:9090"
	@echo ""
	@echo "Demo credentials: demo@vibewarden.dev / demo1234"
	@echo "Run 'vibew secret get postgres' to retrieve generated credentials."

# Start demo with TLS
demo-tls: ## Start the full local demo stack with self-signed TLS
	cd examples/demo-app && \
	  VIBEWARDEN_TLS_ENABLED=true \
	  VIBEWARDEN_TLS_PROVIDER=self-signed \
	  VIBEWARDEN_SERVER_PORT=8443 \
	  ../../bin/vibewarden generate && \
	  COMPOSE_PROFILES=observability \
	  docker compose -f .vibewarden/generated/docker-compose.yml up -d
	@echo ""
	@echo "Demo stack is starting — wait ~30 s for all services to be healthy."
	@echo ""
	@echo "  App:        https://localhost:8443   (accept the self-signed cert warning)"
	@echo "  Grafana:    http://localhost:3001"
	@echo "  Prometheus: http://localhost:9090"
	@echo ""
	@echo "Demo credentials: demo@vibewarden.dev / demo1234"

# Stop the full local demo stack
demo-down: ## Stop the full local demo stack
	cd examples/demo-app && \
	  docker compose -f .vibewarden/generated/docker-compose.yml down

# Stop and remove volumes
demo-clean: ## Stop the demo stack and remove all volumes
	cd examples/demo-app && \
	  docker compose -f .vibewarden/generated/docker-compose.yml down -v && \
	  rm -rf .vibewarden/generated/
```

#### Seed Scripts Mounting

The generated `docker-compose.yml` template must mount the demo-specific seed scripts.
Update `internal/config/templates/docker-compose.yml.tmpl` to support user-provided seed
scripts via config or convention.

For the demo app, we use the `overrides` mechanism or a convention where seed scripts
in `scripts/` are automatically mounted. The simplest approach is to have the demo
`vibewarden.yaml` use relative paths that the generated compose references.

The existing seed containers in the template already mount `seed-secrets.sh` from the
generated directory. For the demo-specific `seed-users.sh`, add a `seed` service that
runs the Kratos user seeding.

Add to `docker-compose.yml.tmpl` a configurable seed container:

```yaml
{{- if .Auth.Enabled }}
  seed-users:
    image: curlimages/curl:8.12.1
    environment:
      KRATOS_ADMIN_URL: http://kratos:4434
    volumes:
      - ./scripts/seed-users.sh:/seed-users.sh:ro
    command: sh /seed-users.sh
    depends_on:
      kratos:
        condition: service_healthy
    networks:
      - vibewarden
    restart: "no"
{{- end }}
```

Note: The `./scripts/seed-users.sh` path is relative to where `docker compose` is run.
Since the demo runs `docker compose -f .vibewarden/generated/docker-compose.yml`, the
working directory is `examples/demo-app/`, so `./scripts/seed-users.sh` resolves correctly.

Alternatively, we can add a config option for custom seed scripts, but for the demo
we rely on the convention that `scripts/seed-users.sh` exists when auth is enabled.

#### Sequence

1. User runs `make demo` (or `make demo-tls`)
2. Makefile builds `bin/vibewarden` if needed (via dependency on `build`)
3. Makefile runs `vibewarden generate` in `examples/demo-app/`:
   - Generates `.vibewarden/generated/docker-compose.yml`
   - Generates `.vibewarden/generated/.credentials` (fresh random credentials)
   - Generates `.vibewarden/generated/.env.template`
   - Generates `.vibewarden/generated/kratos/kratos.yml`
   - Generates `.vibewarden/generated/kratos/identity.schema.json`
   - Generates `.vibewarden/generated/seed-secrets.sh`
   - Generates `.vibewarden/generated/observability/` configs
4. Makefile runs `docker compose -f .vibewarden/generated/docker-compose.yml up -d`
5. Docker Compose starts services in dependency order:
   - postgres (kratos-db)
   - openbao
   - seed-secrets (populates OpenBao from `.credentials`)
   - kratos (after postgres healthy)
   - seed-users (after kratos healthy, seeds demo identities)
   - app (after kratos healthy)
   - vibewarden (after app, kratos, seed-secrets healthy/complete)
   - prometheus, loki, promtail, grafana (observability profile)
6. User accesses demo at http://localhost:8080 (or https://localhost:8443 for TLS)
7. User can retrieve credentials via `vibew secret get postgres`

#### Error Cases

| Error | Handling |
|-------|----------|
| `vibewarden generate` fails | Makefile exits with error; no compose started |
| Docker image pull fails | Docker Compose reports pull error |
| Kratos healthcheck fails | Dependent services wait; eventually timeout |
| seed-users.sh not found | Docker Compose fails with mount error; user must create script or disable auth |
| Generated .credentials not found | seed-secrets.sh fails; vibewarden cannot connect |

#### Template Change: Add seed-users Service

The generated compose needs a `seed-users` service that mounts the user-provided seed script.
This is a demo-specific feature but can be generalized.

Add to `internal/config/templates/docker-compose.yml.tmpl` after the `seed-secrets` service:

```yaml
{{- if .Auth.Enabled }}
{{- /* seed-users is optional: only rendered if scripts/seed-users.sh exists.
       The template cannot check filesystem, so we always render it for auth-enabled configs.
       If the script doesn't exist, docker compose will fail with a clear error. */ -}}
  seed-users:
    image: curlimages/curl:8.12.1
    environment:
      KRATOS_ADMIN_URL: http://kratos:4434
    volumes:
      - ./scripts/seed-users.sh:/seed-users.sh:ro
    command: sh /seed-users.sh
    depends_on:
      kratos:
        condition: service_healthy
    networks:
      - vibewarden
    restart: "no"
{{- end }}
```

Note: This assumes a convention that projects with auth enabled provide a
`scripts/seed-users.sh` script. For projects without demo users to seed, they can
create an empty script or use `overrides.compose_file` to provide a custom compose.

For the demo app specifically, we already have `examples/demo-app/scripts/seed-users.sh`.

#### Deprecation of observability/ Directory

The `observability/` directory at the repository root contains hand-crafted configs that
are now superseded by the generated templates in `internal/config/templates/observability/`.

**Migration plan:**

1. This ADR marks the files as deprecated
2. The demo migration removes references to `../../observability/` paths
3. A follow-up PR can delete the `observability/` directory entirely

The generated configs in `internal/config/templates/observability/` are the source of truth.
The committed `observability/` directory is only useful for reference during the transition.

#### Test Strategy

**Manual Testing Checklist:**

| Test | Steps | Expected Result |
|------|-------|-----------------|
| `make demo` works | Run `make demo`, wait 30s | All services healthy, app at :8080 |
| Auth works | Login with demo@vibewarden.dev / demo1234 | Successful login, session cookie set |
| Protected routes work | Access /me when logged in | Returns user info |
| Rate limiting works | Run `for i in $(seq 1 20); do curl -X POST localhost:8080/spam; done` | 429 after burst |
| Security headers present | `curl -I localhost:8080/public` | HSTS, CSP, X-Frame-Options present |
| Secrets injection works | Check X-Demo-Api-Key header | Header present with demo value |
| Observability works | Access Grafana at :3001 | Dashboard shows metrics |
| `make demo-down` works | Run `make demo-down` | All containers stopped |
| `make demo-clean` works | Run `make demo-clean` | Containers stopped, volumes removed |
| TLS profile works | Run `make demo-tls` | HTTPS at :8443 with self-signed cert |
| Credentials retrieval | Run `vibew secret get postgres` | Shows generated password |

**Automated Tests:**

The existing tests in `internal/app/generate/service_test.go` cover the generation logic.
No new automated tests are required for this migration, as it is primarily a configuration
and documentation change.

**CI Considerations:**

The CI pipeline should verify that `make demo` succeeds:

```yaml
- name: Test demo workflow
  run: |
    make build
    cd examples/demo-app
    ../../bin/vibewarden generate
    # Verify generated files exist
    test -f .vibewarden/generated/docker-compose.yml
    test -f .vibewarden/generated/.credentials
    test -f .vibewarden/generated/kratos/kratos.yml
```

Full `docker compose up` is not tested in CI due to resource constraints.

#### New Dependencies

None. This migration uses existing functionality.

### Consequences

**Positive:**
- Demo now showcases the intended user workflow
- Single source of truth (`vibewarden.yaml`) for all demo configuration
- Generated files are gitignored — no duplication between template and demo
- Validates that the generate system works end-to-end
- Reduces maintenance burden — updating templates updates the demo automatically
- Users can copy the demo workflow for their own projects

**Negative:**
- `make demo` now requires a build step (`vibewarden generate`)
- Additional complexity in Makefile targets
- seed-users convention may surprise users who don't have a seed script

**Trade-offs:**
- Convention (scripts/seed-users.sh) vs configuration: Convention is simpler but less flexible
- Keeping observability/ vs deleting immediately: Keeping allows reference during transition
- Generated compose paths relative to working directory: Matches how users will run it

---

## ADR-012: OTel SDK Integration and MetricsCollector Port/Adapter Refactoring
**Date**: 2026-03-28
**Issue**: #285
**Status**: Accepted

### Context

VibeWarden currently uses `prometheus/client_golang` directly for metrics collection (locked
decision L-07). The existing `MetricsCollector` port and `PrometheusAdapter` implementation
work well, but they are tightly coupled to Prometheus-specific types.

Epic #280 (OpenTelemetry Integration) requires VibeWarden to adopt the OpenTelemetry SDK as
the unified observability foundation. This enables:

1. **OTLP export** — Push metrics to any OTLP-compatible backend (future story #286)
2. **Unified SDK** — Single initialization path for metrics, traces, and logs
3. **Prometheus bridge** — Continue serving `/_vibewarden/metrics` via OTel's Prometheus exporter
4. **Fleet integration** — Future Pro tier can receive OTel-formatted telemetry

The OTel Go SDK packages are already transitive dependencies through Caddy:
- `go.opentelemetry.io/otel` v1.41.0
- `go.opentelemetry.io/otel/metric` v1.41.0
- `go.opentelemetry.io/otel/sdk` v1.41.0
- `go.opentelemetry.io/otel/sdk/metric` v1.41.0
- `go.opentelemetry.io/otel/exporters/prometheus` v0.62.0

All are licensed under **Apache 2.0** (verified), which is on the approved list.

This ADR covers the foundation story: OTel SDK initialization, updated port interface,
and the new OTel adapter that replaces the direct Prometheus implementation.

### Decision

Refactor the metrics subsystem to use OpenTelemetry SDK as the metrics API while
maintaining Prometheus export compatibility. The `MetricsCollector` port interface
remains stable; only the adapter implementation changes.

#### Domain Model Changes

No domain model changes. Metrics are infrastructure concerns, not domain entities.

#### Ports (Interfaces)

The existing `ports.MetricsCollector` interface **remains unchanged**:

```go
// internal/ports/metrics.go
type MetricsCollector interface {
    IncRequestTotal(method, statusCode, pathPattern string)
    ObserveRequestDuration(method, pathPattern string, duration time.Duration)
    IncRateLimitHit(limitType string)
    IncAuthDecision(decision string)
    IncUpstreamError()
    SetActiveConnections(n int)
}
```

This interface is deliberately backend-agnostic. Callers (middleware, plugins) do not
need to know whether the underlying implementation uses Prometheus directly or OTel.

**New port interface** for OTel lifecycle management:

```go
// internal/ports/otel.go
package ports

import (
    "context"
    "net/http"
)

// OTelProvider manages the OpenTelemetry SDK lifecycle.
// It initializes the MeterProvider and exposes an HTTP handler for Prometheus scraping.
// Implementations must be safe for concurrent use after Init returns.
type OTelProvider interface {
    // Init initializes the OTel SDK with the given service name and version.
    // It sets up the MeterProvider with a Prometheus exporter.
    // Must be called once before any other methods.
    Init(ctx context.Context, serviceName, serviceVersion string) error

    // Shutdown gracefully shuts down the OTel SDK, flushing any buffered data.
    // Must honour the context deadline.
    Shutdown(ctx context.Context) error

    // Handler returns an http.Handler that serves Prometheus metrics.
    // Returns nil if Init has not been called.
    Handler() http.Handler

    // Meter returns a named OTel Meter for creating instruments.
    // The scope name is "github.com/vibewarden/vibewarden".
    Meter() Meter
}

// Meter is a subset of the OTel metric.Meter interface, exposing only the
// instrument creation methods VibeWarden needs. This keeps the port layer
// decoupled from the full OTel API.
type Meter interface {
    // Int64Counter creates a Counter instrument for incrementing metrics.
    Int64Counter(name string, options ...InstrumentOption) (Int64Counter, error)

    // Float64Histogram creates a Histogram instrument for recording distributions.
    Float64Histogram(name string, options ...InstrumentOption) (Float64Histogram, error)

    // Int64UpDownCounter creates an UpDownCounter for gauge-like values that can
    // increase or decrease.
    Int64UpDownCounter(name string, options ...InstrumentOption) (Int64UpDownCounter, error)
}

// InstrumentOption configures an OTel instrument (description, unit, etc.).
// This is a placeholder type; the adapter translates to OTel SDK options.
type InstrumentOption interface {
    isInstrumentOption()
}

// Int64Counter is an OTel counter instrument for int64 increments.
type Int64Counter interface {
    Add(ctx context.Context, incr int64, attrs ...Attribute)
}

// Float64Histogram is an OTel histogram instrument for float64 observations.
type Float64Histogram interface {
    Record(ctx context.Context, value float64, attrs ...Attribute)
}

// Int64UpDownCounter is an OTel up-down counter for gauge-like int64 values.
type Int64UpDownCounter interface {
    Add(ctx context.Context, incr int64, attrs ...Attribute)
}

// Attribute is a key-value pair attached to metric observations.
type Attribute struct {
    Key   string
    Value string
}
```

**Why wrap OTel types?**

The ports layer must not import external packages (hexagonal architecture principle).
These thin wrapper types allow the domain/app layers to reference metric concepts
without depending on `go.opentelemetry.io/otel/metric`. The adapter layer performs
the type conversion.

#### Adapters

**New file:** `internal/adapters/otel/provider.go`

```go
// Package otel provides the OpenTelemetry SDK adapter for VibeWarden.
//
// It initializes the MeterProvider with a Prometheus exporter and implements
// ports.OTelProvider. The provider is the single source of truth for OTel
// SDK lifecycle management.
package otel

import (
    "context"
    "fmt"
    "net/http"
    "sync"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/vibewarden/vibewarden/internal/ports"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/prometheus"
    "go.opentelemetry.io/otel/metric"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Provider implements ports.OTelProvider using the OTel Go SDK.
type Provider struct {
    mu            sync.RWMutex
    meterProvider *sdkmetric.MeterProvider
    meter         metric.Meter
    handler       http.Handler
    registry      *prometheus.Registry
}

// NewProvider creates an uninitialized Provider.
// Call Init before using any other methods.
func NewProvider() *Provider {
    return &Provider{}
}

// Init initializes the OTel SDK with a Prometheus exporter.
func (p *Provider) Init(ctx context.Context, serviceName, serviceVersion string) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.meterProvider != nil {
        return fmt.Errorf("otel provider already initialized")
    }

    // Create a dedicated Prometheus registry (not the global default).
    p.registry = prometheus.NewRegistry()

    // Create Prometheus exporter with the isolated registry.
    exporter, err := prometheus.New(
        prometheus.WithRegisterer(p.registry),
        prometheus.WithoutScopeInfo(),
    )
    if err != nil {
        return fmt.Errorf("creating prometheus exporter: %w", err)
    }

    // Build resource with service identity.
    res, err := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName(serviceName),
            semconv.ServiceVersion(serviceVersion),
        ),
    )
    if err != nil {
        return fmt.Errorf("creating otel resource: %w", err)
    }

    // Create MeterProvider with the Prometheus exporter.
    p.meterProvider = sdkmetric.NewMeterProvider(
        sdkmetric.WithResource(res),
        sdkmetric.WithReader(exporter),
    )

    // Set as global provider for any code that uses otel.GetMeterProvider().
    otel.SetMeterProvider(p.meterProvider)

    // Create the application meter.
    p.meter = p.meterProvider.Meter("github.com/vibewarden/vibewarden")

    // Create the HTTP handler for Prometheus scraping.
    p.handler = promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
        EnableOpenMetrics: true,
    })

    return nil
}

// Shutdown shuts down the MeterProvider.
func (p *Provider) Shutdown(ctx context.Context) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.meterProvider == nil {
        return nil
    }
    return p.meterProvider.Shutdown(ctx)
}

// Handler returns the Prometheus metrics HTTP handler.
func (p *Provider) Handler() http.Handler {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.handler
}

// Meter returns a ports.Meter wrapping the OTel SDK meter.
func (p *Provider) Meter() ports.Meter {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return &meterAdapter{m: p.meter}
}
```

**New file:** `internal/adapters/otel/meter.go`

```go
package otel

import (
    "context"

    "github.com/vibewarden/vibewarden/internal/ports"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

// meterAdapter wraps an OTel metric.Meter to implement ports.Meter.
type meterAdapter struct {
    m metric.Meter
}

func (a *meterAdapter) Int64Counter(name string, opts ...ports.InstrumentOption) (ports.Int64Counter, error) {
    c, err := a.m.Int64Counter(name, translateOptions(opts)...)
    if err != nil {
        return nil, err
    }
    return &int64CounterAdapter{c: c}, nil
}

func (a *meterAdapter) Float64Histogram(name string, opts ...ports.InstrumentOption) (ports.Float64Histogram, error) {
    h, err := a.m.Float64Histogram(name, translateOptions(opts)...)
    if err != nil {
        return nil, err
    }
    return &float64HistogramAdapter{h: h}, nil
}

func (a *meterAdapter) Int64UpDownCounter(name string, opts ...ports.InstrumentOption) (ports.Int64UpDownCounter, error) {
    c, err := a.m.Int64UpDownCounter(name, translateOptions(opts)...)
    if err != nil {
        return nil, err
    }
    return &int64UpDownCounterAdapter{c: c}, nil
}

// translateOptions converts ports.InstrumentOption to OTel SDK options.
func translateOptions(opts []ports.InstrumentOption) []metric.Int64CounterOption {
    // Implementation translates Description, Unit options.
    // Simplified for ADR; full implementation in code.
    return nil
}

// Instrument adapters...
type int64CounterAdapter struct{ c metric.Int64Counter }

func (a *int64CounterAdapter) Add(ctx context.Context, incr int64, attrs ...ports.Attribute) {
    a.c.Add(ctx, incr, toOTelAttrs(attrs)...)
}

type float64HistogramAdapter struct{ h metric.Float64Histogram }

func (a *float64HistogramAdapter) Record(ctx context.Context, value float64, attrs ...ports.Attribute) {
    a.h.Record(ctx, value, toOTelAttrs(attrs)...)
}

type int64UpDownCounterAdapter struct{ c metric.Int64UpDownCounter }

func (a *int64UpDownCounterAdapter) Add(ctx context.Context, incr int64, attrs ...ports.Attribute) {
    a.c.Add(ctx, incr, toOTelAttrs(attrs)...)
}

func toOTelAttrs(attrs []ports.Attribute) []attribute.KeyValue {
    kvs := make([]attribute.KeyValue, len(attrs))
    for i, a := range attrs {
        kvs[i] = attribute.String(a.Key, a.Value)
    }
    return kvs
}
```

**Updated file:** `internal/adapters/metrics/otel.go` (replaces prometheus.go)

```go
// Package metrics provides metrics adapter implementations for VibeWarden.
package metrics

import (
    "context"
    "net/http"
    "time"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// OTelAdapter implements ports.MetricsCollector using an OTel MeterProvider.
// It creates counters and histograms via ports.Meter and records observations.
type OTelAdapter struct {
    requestsTotal     ports.Int64Counter
    requestDuration   ports.Float64Histogram
    rateLimitHits     ports.Int64Counter
    authDecisions     ports.Int64Counter
    upstreamErrors    ports.Int64Counter
    activeConnections ports.Int64UpDownCounter
    pathMatcher       *PathMatcher
    handler           http.Handler
}

// NewOTelAdapter creates a new OTel-backed MetricsCollector.
// The provider must be initialized before calling this function.
func NewOTelAdapter(provider ports.OTelProvider, pathPatterns []string) (*OTelAdapter, error) {
    meter := provider.Meter()

    requestsTotal, err := meter.Int64Counter("vibewarden_requests_total",
        ports.WithDescription("Total number of HTTP requests processed."),
    )
    if err != nil {
        return nil, err
    }

    requestDuration, err := meter.Float64Histogram("vibewarden_request_duration_seconds",
        ports.WithDescription("HTTP request duration in seconds."),
        ports.WithUnit("s"),
        ports.WithExplicitBuckets([]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),
    )
    if err != nil {
        return nil, err
    }

    rateLimitHits, err := meter.Int64Counter("vibewarden_rate_limit_hits_total",
        ports.WithDescription("Total number of rate limit hits."),
    )
    if err != nil {
        return nil, err
    }

    authDecisions, err := meter.Int64Counter("vibewarden_auth_decisions_total",
        ports.WithDescription("Total number of authentication decisions."),
    )
    if err != nil {
        return nil, err
    }

    upstreamErrors, err := meter.Int64Counter("vibewarden_upstream_errors_total",
        ports.WithDescription("Total number of upstream connection errors."),
    )
    if err != nil {
        return nil, err
    }

    activeConnections, err := meter.Int64UpDownCounter("vibewarden_active_connections",
        ports.WithDescription("Current number of active proxy connections."),
    )
    if err != nil {
        return nil, err
    }

    return &OTelAdapter{
        requestsTotal:     requestsTotal,
        requestDuration:   requestDuration,
        rateLimitHits:     rateLimitHits,
        authDecisions:     authDecisions,
        upstreamErrors:    upstreamErrors,
        activeConnections: activeConnections,
        pathMatcher:       NewPathMatcher(pathPatterns),
        handler:           provider.Handler(),
    }, nil
}

// Handler returns the Prometheus HTTP handler for scraping.
func (a *OTelAdapter) Handler() http.Handler { return a.handler }

// NormalizePath returns the matching pattern for a path.
func (a *OTelAdapter) NormalizePath(path string) string {
    return a.pathMatcher.Match(path)
}

// IncRequestTotal implements ports.MetricsCollector.
func (a *OTelAdapter) IncRequestTotal(method, statusCode, pathPattern string) {
    a.requestsTotal.Add(context.Background(), 1,
        ports.Attribute{Key: "method", Value: method},
        ports.Attribute{Key: "status_code", Value: statusCode},
        ports.Attribute{Key: "path_pattern", Value: pathPattern},
    )
}

// ObserveRequestDuration implements ports.MetricsCollector.
func (a *OTelAdapter) ObserveRequestDuration(method, pathPattern string, duration time.Duration) {
    a.requestDuration.Record(context.Background(), duration.Seconds(),
        ports.Attribute{Key: "method", Value: method},
        ports.Attribute{Key: "path_pattern", Value: pathPattern},
    )
}

// IncRateLimitHit implements ports.MetricsCollector.
func (a *OTelAdapter) IncRateLimitHit(limitType string) {
    a.rateLimitHits.Add(context.Background(), 1,
        ports.Attribute{Key: "limit_type", Value: limitType},
    )
}

// IncAuthDecision implements ports.MetricsCollector.
func (a *OTelAdapter) IncAuthDecision(decision string) {
    a.authDecisions.Add(context.Background(), 1,
        ports.Attribute{Key: "decision", Value: decision},
    )
}

// IncUpstreamError implements ports.MetricsCollector.
func (a *OTelAdapter) IncUpstreamError() {
    a.upstreamErrors.Add(context.Background(), 1)
}

// SetActiveConnections implements ports.MetricsCollector.
func (a *OTelAdapter) SetActiveConnections(n int) {
    // OTel doesn't have a "set" operation for UpDownCounter.
    // We need to track the previous value and add the delta.
    // For simplicity in this foundation story, we use a synchronous approach.
    // A production implementation would track state atomically.
    a.activeConnections.Add(context.Background(), int64(n))
}
```

**Note on SetActiveConnections:** OTel's UpDownCounter only supports Add, not Set.
The implementation must track the previous value and compute the delta. This is
a known OTel limitation. The adapter will maintain an atomic int64 to track current
value and add/subtract the difference.

#### Application Service

No new application service. The metrics plugin orchestrates the OTel provider and adapter.

**Updated metrics plugin:** `internal/plugins/metrics/plugin.go`

```go
package metrics

import (
    "context"
    "fmt"
    "log/slog"

    metricsadapter "github.com/vibewarden/vibewarden/internal/adapters/metrics"
    oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
    "github.com/vibewarden/vibewarden/internal/ports"
)

type Plugin struct {
    cfg          Config
    logger       *slog.Logger
    otelProvider *oteladapter.Provider
    adapter      *metricsadapter.OTelAdapter
    server       *metricsadapter.Server
    internalAddr string
    running      bool
}

func New(cfg Config, logger *slog.Logger) *Plugin {
    return &Plugin{cfg: cfg, logger: logger}
}

func (p *Plugin) Name() string { return "metrics" }
func (p *Plugin) Priority() int { return 30 }

func (p *Plugin) Init(ctx context.Context) error {
    if !p.cfg.Enabled {
        return nil
    }

    // Initialize OTel provider.
    p.otelProvider = oteladapter.NewProvider()
    if err := p.otelProvider.Init(ctx, "vibewarden", Version); err != nil {
        return fmt.Errorf("metrics plugin: initializing otel provider: %w", err)
    }

    // Create the OTel-backed metrics adapter.
    adapter, err := metricsadapter.NewOTelAdapter(p.otelProvider, p.cfg.PathPatterns)
    if err != nil {
        return fmt.Errorf("metrics plugin: creating otel adapter: %w", err)
    }
    p.adapter = adapter

    p.logger.Info("metrics plugin initialised with OTel SDK",
        slog.Int("path_patterns", len(p.cfg.PathPatterns)),
    )
    return nil
}

func (p *Plugin) Start(ctx context.Context) error {
    if !p.cfg.Enabled {
        return nil
    }
    p.server = metricsadapter.NewServer(p.adapter.Handler(), p.logger)
    if err := p.server.Start(); err != nil {
        return fmt.Errorf("metrics plugin: starting internal server: %w", err)
    }
    p.internalAddr = p.server.Addr()
    p.running = true
    p.logger.Info("metrics plugin started",
        slog.String("internal_addr", p.internalAddr),
    )
    return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
    if p.server != nil {
        p.running = false
        if err := p.server.Stop(ctx); err != nil {
            return fmt.Errorf("metrics plugin: stopping internal server: %w", err)
        }
    }
    if p.otelProvider != nil {
        if err := p.otelProvider.Shutdown(ctx); err != nil {
            return fmt.Errorf("metrics plugin: shutting down otel provider: %w", err)
        }
    }
    return nil
}

// Collector returns the MetricsCollector for use by middleware.
// Returns nil if the plugin is disabled or not initialized.
func (p *Plugin) Collector() ports.MetricsCollector {
    if p.adapter == nil {
        return metricsadapter.NoOpMetricsCollector{}
    }
    return p.adapter
}

// ... Health, ContributeCaddyRoutes, InternalAddr unchanged ...
```

#### File Layout

**New files:**

```
internal/
  ports/
    otel.go                    # OTelProvider, Meter, Instrument interfaces
  adapters/
    otel/
      provider.go              # Provider implementing ports.OTelProvider
      provider_test.go         # Unit tests for Provider
      meter.go                 # meterAdapter, instrument adapters
      meter_test.go            # Unit tests for meter adapters
    metrics/
      otel.go                  # OTelAdapter implementing ports.MetricsCollector
      otel_test.go             # Unit tests for OTelAdapter
```

**Deprecated files (to be removed in follow-up):**

```
internal/
  adapters/
    metrics/
      prometheus.go            # DEPRECATED: replaced by otel.go
      prometheus_test.go       # DEPRECATED: replaced by otel_test.go
```

**Unchanged files:**

```
internal/
  ports/
    metrics.go                 # MetricsCollector interface (unchanged)
  adapters/
    metrics/
      noop.go                  # NoOpMetricsCollector (unchanged)
      noop_test.go             # Tests (unchanged)
      path_matcher.go          # PathMatcher (unchanged, reused by OTelAdapter)
      path_matcher_test.go     # Tests (unchanged)
      server.go                # Internal HTTP server (unchanged)
      server_test.go           # Tests (unchanged)
  plugins/
    metrics/
      plugin.go                # Updated to use OTel provider
      plugin_test.go           # Updated tests
      config.go                # Unchanged
      meta.go                  # Unchanged
  middleware/
    metrics.go                 # Unchanged (uses ports.MetricsCollector)
    metrics_test.go            # Unchanged
```

#### Sequence

**Initialization flow:**

1. `main()` loads config, creates metrics plugin with `metrics.New(cfg, logger)`
2. Plugin registry calls `plugin.Init(ctx)`:
   a. Create `oteladapter.Provider` (uninitialized)
   b. Call `provider.Init(ctx, "vibewarden", version)`:
      - Create Prometheus registry
      - Create Prometheus exporter with registry
      - Create OTel Resource with service name/version
      - Create MeterProvider with exporter
      - Set global MeterProvider
      - Create Meter for scope "github.com/vibewarden/vibewarden"
      - Create promhttp.Handler for the registry
   c. Create `metricsadapter.OTelAdapter(provider, pathPatterns)`:
      - Get Meter from provider
      - Create Int64Counter for requests_total
      - Create Float64Histogram for request_duration
      - Create counters for rate_limit_hits, auth_decisions, upstream_errors
      - Create Int64UpDownCounter for active_connections
      - Store handler from provider
3. Plugin registry calls `plugin.Start(ctx)`:
   a. Create internal HTTP server with adapter.Handler()
   b. Server binds random localhost port
   c. Store internal address for Caddy reverse-proxy

**Request flow (unchanged from caller's perspective):**

1. HTTP request arrives at Caddy
2. `MetricsMiddleware` intercepts, records start time
3. Request proceeds through handler chain
4. On response, middleware calls:
   - `mc.IncRequestTotal(method, statusCode, pathPattern)`
   - `mc.ObserveRequestDuration(method, pathPattern, duration)`
5. OTelAdapter forwards to OTel instruments with attributes
6. OTel SDK aggregates observations in memory
7. Prometheus exporter exposes aggregated metrics at `/_vibewarden/metrics`

**Shutdown flow:**

1. Plugin registry calls `plugin.Stop(ctx)`
2. Plugin stops internal HTTP server
3. Plugin calls `provider.Shutdown(ctx)`
4. MeterProvider flushes any pending data
5. Exporter is released

#### Error Cases

| Error | Cause | Handling |
|-------|-------|----------|
| `otel provider already initialized` | Init called twice | Return error, log warning |
| `creating prometheus exporter` | Registry conflict | Return error, plugin fails to init |
| `creating otel resource` | Invalid resource attributes | Return error, plugin fails to init |
| Instrument creation fails | Invalid metric name | Return error from NewOTelAdapter |
| Nil provider.Handler() | Init not called | Return nil, internal server fails |
| Context cancelled during Init | Timeout | Return ctx.Err() |
| Shutdown with pending data | Exporter blocked | Honour context deadline, may lose data |

All errors are wrapped with context and propagated to the plugin registry, which
logs them and marks the plugin as unhealthy.

#### Test Strategy

**Unit tests:**

| File | Coverage |
|------|----------|
| `internal/adapters/otel/provider_test.go` | Init, Shutdown, Handler, Meter accessors |
| `internal/adapters/otel/meter_test.go` | Instrument creation, Add/Record calls, attribute conversion |
| `internal/adapters/metrics/otel_test.go` | All MetricsCollector methods, path normalization |
| `internal/plugins/metrics/plugin_test.go` | Updated for OTel provider lifecycle |

**Unit test approach:**

- Use `go.opentelemetry.io/otel/sdk/metric/metrictest` for in-memory reading of recorded values
- Verify correct metric names, descriptions, and attribute labels
- Test SetActiveConnections delta calculation
- Mock provider for adapter tests

**Integration tests:**

| Test | Coverage |
|------|----------|
| `internal/adapters/metrics/otel_integration_test.go` | Full stack: Provider → Adapter → HTTP scrape |

**Integration test approach:**

- Start real OTel provider with Prometheus exporter
- Record metrics through adapter
- Scrape `/_vibewarden/metrics` endpoint
- Verify Prometheus format output contains expected metrics
- Verify Go runtime metrics are still present

**What to mock vs. what to test real:**

- Mock: Nothing at unit level; OTel SDK is fast and deterministic
- Real: Full integration test with HTTP scraping
- Skip: External OTLP export (future story #286)

#### New Dependencies

| Package | Version | License | Reason |
|---------|---------|---------|--------|
| `go.opentelemetry.io/otel` | v1.41.0 | Apache 2.0 | Core OTel API |
| `go.opentelemetry.io/otel/sdk` | v1.41.0 | Apache 2.0 | MeterProvider implementation |
| `go.opentelemetry.io/otel/sdk/metric` | v1.41.0 | Apache 2.0 | Metric SDK |
| `go.opentelemetry.io/otel/exporters/prometheus` | v0.62.0 | Apache 2.0 | Prometheus exporter |
| `go.opentelemetry.io/otel/semconv/v1.26.0` | v1.41.0 | Apache 2.0 | Semantic conventions |

**License verification:** All packages are part of the `opentelemetry-go` repository,
which is licensed under Apache 2.0 (verified by reading LICENSE file from
https://github.com/open-telemetry/opentelemetry-go). This license is on the
approved list per CLAUDE.md.

**Note:** These packages are already transitive dependencies through Caddy.
Promoting them to direct dependencies does not increase the binary size.

### Consequences

**Positive:**

- **Unified SDK:** Single OTel MeterProvider for all metrics, enabling future
  traces and logs integration with consistent resource attributes.
- **OTLP-ready:** Adding OTLP export (story #286) becomes a one-line change
  to add another reader to the MeterProvider.
- **Prometheus compatible:** Existing scrapers and dashboards work unchanged.
- **No breaking changes:** The `MetricsCollector` port interface is stable;
  callers do not need modification.
- **Vendor-neutral:** OTel is CNCF graduated; no vendor lock-in.

**Negative:**

- **SetActiveConnections complexity:** OTel UpDownCounter lacks Set semantics,
  requiring delta tracking in the adapter. This adds state management overhead.
- **Transitive dependency size:** While already present via Caddy, the OTel SDK
  is larger than prometheus/client_golang alone. Acceptable trade-off for
  observability standardization.
- **Learning curve:** Developers must understand OTel concepts (MeterProvider,
  Meter, Instruments, Attributes) vs. simpler Prometheus registry model.

**Trade-offs:**

- **Port wrapper types vs. direct OTel imports:** Chose wrappers to maintain
  hexagonal purity. Cost: more adapter code. Benefit: domain/app layers remain
  decoupled from OTel specifics.
- **Global MeterProvider:** Setting OTel's global provider simplifies integration
  with any code that uses `otel.GetMeterProvider()`. Risk: potential conflicts
  if user code also sets globals. Mitigation: documented behavior.
- **Deprecate vs. delete prometheus.go:** Chose deprecation in this story,
  deletion in follow-up. Allows rollback if issues discovered.

**Migration path:**

1. This story implements OTel adapter alongside existing Prometheus adapter
2. Plugin switched to use OTel adapter (breaking change for plugin internals only)
3. Follow-up story deletes deprecated prometheus.go
4. Future story #286 adds OTLP exporter configuration

**Locked decision update:**

L-07 currently reads: "Metrics: prometheus/client_golang (Apache 2.0)".

This ADR does **not** change the locked decision. Prometheus remains the export
format; we're changing the internal SDK from prometheus/client_golang to OTel SDK
with Prometheus exporter. The user-facing `/_vibewarden/metrics` endpoint remains
Prometheus-compatible.

A future ADR may update L-07 to "Metrics: OpenTelemetry SDK with Prometheus export"
once this migration is complete and validated.

---

## ADR-013: OTLP Exporter Configuration and Telemetry Plugin Refactor
**Date**: 2026-03-28
**Issue**: #287
**Status**: Accepted

### Context

ADR-012 introduced the OpenTelemetry SDK as the metrics foundation, using the Prometheus
exporter for pull-based metrics at `/_vibewarden/metrics`. Epic #280 (OpenTelemetry
Integration) requires push-based OTLP export as the primary telemetry path.

The issue is that the current architecture:

1. Only supports Prometheus pull-based export (scraping)
2. Requires opening inbound endpoints, conflicting with localhost-only security model
3. Has a `MetricsConfig` that is too narrow for the broader telemetry scope

OTLP export is push-based: the sidecar initiates outbound connections to send telemetry
to a collector endpoint. This aligns with VibeWarden's security model (localhost-only,
no inbound ports beyond the reverse proxy).

The acceptance criteria from issue #287:
- Refactor config to support both OTLP and Prometheus exporters
- Configure OTLP HTTP exporter with endpoint, headers, and interval
- Allow both exporters to run simultaneously
- Graceful shutdown with pending telemetry flush
- Backward compatibility: map legacy `metrics:` config to `telemetry:`

### Decision

Add OTLP HTTP exporter support to the OTelProvider and introduce a new `TelemetryConfig`
configuration section that replaces the narrow `MetricsConfig`. The system supports
running both Prometheus and OTLP exporters simultaneously.

#### Domain Model Changes

No domain model changes. Telemetry configuration is infrastructure concern.

#### Ports (Interfaces)

**Update `internal/ports/otel.go`** to add telemetry configuration types:

```go
// TelemetryConfig holds all telemetry export settings.
// It is passed to OTelProvider.Init to configure exporters.
type TelemetryConfig struct {
    // Prometheus enables the Prometheus pull-based exporter.
    // When enabled, metrics are available at /_vibewarden/metrics.
    Prometheus PrometheusExporterConfig

    // OTLP enables the OTLP push-based exporter.
    // When enabled, metrics are pushed to the configured endpoint.
    OTLP OTLPExporterConfig
}

// PrometheusExporterConfig configures the Prometheus pull-based exporter.
type PrometheusExporterConfig struct {
    // Enabled toggles the Prometheus exporter (default: true).
    Enabled bool
}

// OTLPExporterConfig configures the OTLP push-based exporter.
type OTLPExporterConfig struct {
    // Enabled toggles the OTLP exporter (default: false).
    Enabled bool

    // Endpoint is the OTLP HTTP endpoint URL (e.g., "http://localhost:4318").
    // Required when Enabled is true.
    Endpoint string

    // Headers are optional HTTP headers for authentication (e.g., API keys).
    // Keys are header names, values are header values.
    Headers map[string]string

    // Interval is the export interval (default: 30s).
    // Metrics are batched and pushed at this interval.
    Interval time.Duration

    // Protocol specifies the OTLP protocol: "http" or "grpc" (default: "http").
    // This story only implements "http"; "grpc" is reserved for future use.
    Protocol string
}
```

**Update `OTelProvider` interface:**

```go
// OTelProvider manages the OpenTelemetry SDK lifecycle.
// It initializes the MeterProvider with configured exporters and exposes
// an HTTP handler for Prometheus scraping (when Prometheus exporter is enabled).
// Implementations must be safe for concurrent use after Init returns.
type OTelProvider interface {
    // Init initializes the OTel SDK with the given service identity and telemetry config.
    // It sets up the MeterProvider with the configured exporters (Prometheus, OTLP, or both).
    // Must be called once before any other methods.
    Init(ctx context.Context, serviceName, serviceVersion string, cfg TelemetryConfig) error

    // Shutdown gracefully shuts down the OTel SDK, flushing any buffered data.
    // For OTLP exporter, this flushes pending metrics to the endpoint.
    // Must honour the context deadline.
    Shutdown(ctx context.Context) error

    // Handler returns an http.Handler that serves Prometheus metrics.
    // Returns nil if Prometheus exporter is disabled or Init has not been called.
    Handler() http.Handler

    // Meter returns a named OTel Meter for creating instruments.
    // The scope name is "github.com/vibewarden/vibewarden".
    Meter() Meter

    // PrometheusEnabled returns true if the Prometheus exporter is active.
    PrometheusEnabled() bool

    // OTLPEnabled returns true if the OTLP exporter is active.
    OTLPEnabled() bool
}
```

#### Adapters

**Update `internal/adapters/otel/provider.go`:**

```go
package otel

import (
    "context"
    "fmt"
    "net/http"
    "sync"
    "time"

    prometheusclient "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/vibewarden/vibewarden/internal/ports"
    "go.opentelemetry.io/otel"
    otelprom "go.opentelemetry.io/otel/exporters/prometheus"
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
    otelmetric "go.opentelemetry.io/otel/metric"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Provider implements ports.OTelProvider using the OTel Go SDK.
// It supports both Prometheus and OTLP exporters, configured via Init.
type Provider struct {
    mu            sync.RWMutex
    meterProvider *sdkmetric.MeterProvider
    meter         otelmetric.Meter
    handler       http.Handler
    registry      *prometheusclient.Registry

    promEnabled bool
    otlpEnabled bool
}

// NewProvider creates an uninitialized Provider.
// Call Init before using any other methods.
func NewProvider() *Provider {
    return &Provider{}
}

// Init initializes the OTel SDK with configured exporters.
// serviceName and serviceVersion are recorded as OTel resource attributes.
// Returns an error if Init has already been called, if no exporters are enabled,
// or if OTLP is enabled without an endpoint.
func (p *Provider) Init(ctx context.Context, serviceName, serviceVersion string, cfg ports.TelemetryConfig) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.meterProvider != nil {
        return fmt.Errorf("otel provider already initialized")
    }

    // Validate config.
    if !cfg.Prometheus.Enabled && !cfg.OTLP.Enabled {
        return fmt.Errorf("at least one exporter must be enabled")
    }
    if cfg.OTLP.Enabled && cfg.OTLP.Endpoint == "" {
        return fmt.Errorf("OTLP endpoint required when OTLP exporter is enabled")
    }

    // Build resource with service identity.
    res, err := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName(serviceName),
            semconv.ServiceVersion(serviceVersion),
        ),
    )
    if err != nil {
        return fmt.Errorf("creating otel resource: %w", err)
    }

    // Collect readers for each enabled exporter.
    var readers []sdkmetric.Option

    // Prometheus exporter (pull-based).
    if cfg.Prometheus.Enabled {
        p.registry = prometheusclient.NewRegistry()
        promExporter, err := otelprom.New(
            otelprom.WithRegisterer(p.registry),
            otelprom.WithoutScopeInfo(),
        )
        if err != nil {
            return fmt.Errorf("creating prometheus exporter: %w", err)
        }
        readers = append(readers, sdkmetric.WithReader(promExporter))
        p.handler = promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
            EnableOpenMetrics: true,
        })
        p.promEnabled = true
    }

    // OTLP HTTP exporter (push-based).
    if cfg.OTLP.Enabled {
        interval := cfg.OTLP.Interval
        if interval == 0 {
            interval = 30 * time.Second
        }

        // Build OTLP HTTP exporter options.
        otlpOpts := []otlpmetrichttp.Option{
            otlpmetrichttp.WithEndpointURL(cfg.OTLP.Endpoint),
        }
        if len(cfg.OTLP.Headers) > 0 {
            otlpOpts = append(otlpOpts, otlpmetrichttp.WithHeaders(cfg.OTLP.Headers))
        }

        otlpExporter, err := otlpmetrichttp.New(ctx, otlpOpts...)
        if err != nil {
            return fmt.Errorf("creating otlp exporter: %w", err)
        }

        // Periodic reader pushes metrics at the configured interval.
        periodicReader := sdkmetric.NewPeriodicReader(otlpExporter,
            sdkmetric.WithInterval(interval),
        )
        readers = append(readers, sdkmetric.WithReader(periodicReader))
        p.otlpEnabled = true
    }

    // Create MeterProvider with all configured readers.
    opts := []sdkmetric.Option{sdkmetric.WithResource(res)}
    opts = append(opts, readers...)
    p.meterProvider = sdkmetric.NewMeterProvider(opts...)

    // Set as global provider.
    otel.SetMeterProvider(p.meterProvider)

    // Create the application meter.
    p.meter = p.meterProvider.Meter("github.com/vibewarden/vibewarden")

    return nil
}

// Shutdown gracefully shuts down the MeterProvider, flushing any buffered data.
// For OTLP exporter, this ensures pending metrics are pushed to the endpoint.
func (p *Provider) Shutdown(ctx context.Context) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.meterProvider == nil {
        return nil
    }
    return p.meterProvider.Shutdown(ctx)
}

// Handler returns the Prometheus metrics HTTP handler.
// Returns nil if Prometheus exporter is disabled or Init has not been called.
func (p *Provider) Handler() http.Handler {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.handler
}

// Meter returns a ports.Meter wrapping the OTel SDK meter.
// Returns nil if Init has not been called.
func (p *Provider) Meter() ports.Meter {
    p.mu.RLock()
    defer p.mu.RUnlock()
    if p.meter == nil {
        return nil
    }
    return &meterAdapter{m: p.meter}
}

// PrometheusEnabled returns true if the Prometheus exporter is active.
func (p *Provider) PrometheusEnabled() bool {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.promEnabled
}

// OTLPEnabled returns true if the OTLP exporter is active.
func (p *Provider) OTLPEnabled() bool {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.otlpEnabled
}
```

#### Application Service

**Update `internal/config/config.go`:**

Add new `TelemetryConfig` struct and deprecate `MetricsConfig`:

```go
// TelemetryConfig holds all telemetry export settings.
// This replaces the narrower MetricsConfig and supports both pull (Prometheus)
// and push (OTLP) export modes.
type TelemetryConfig struct {
    // Enabled toggles telemetry collection entirely (default: true).
    Enabled bool `mapstructure:"enabled"`

    // PathPatterns is a list of URL path normalization patterns using :param syntax.
    // Example: "/users/:id", "/api/v1/items/:item_id/comments/:comment_id"
    // Paths that don't match any pattern are recorded as "other".
    PathPatterns []string `mapstructure:"path_patterns"`

    // Prometheus configures the pull-based Prometheus exporter.
    Prometheus PrometheusExporterConfig `mapstructure:"prometheus"`

    // OTLP configures the push-based OTLP exporter.
    OTLP OTLPExporterConfig `mapstructure:"otlp"`
}

// PrometheusExporterConfig configures the Prometheus pull-based exporter.
type PrometheusExporterConfig struct {
    // Enabled toggles the Prometheus exporter (default: true).
    // When enabled, metrics are served at /_vibewarden/metrics.
    Enabled bool `mapstructure:"enabled"`
}

// OTLPExporterConfig configures the OTLP push-based exporter.
type OTLPExporterConfig struct {
    // Enabled toggles the OTLP exporter (default: false).
    Enabled bool `mapstructure:"enabled"`

    // Endpoint is the OTLP HTTP endpoint URL (e.g., "http://localhost:4318").
    // Required when Enabled is true.
    Endpoint string `mapstructure:"endpoint"`

    // Headers are optional HTTP headers for authentication.
    // Example: {"Authorization": "Bearer <token>"}
    Headers map[string]string `mapstructure:"headers"`

    // Interval is the export interval as a duration string (default: "30s").
    // Metrics are batched and pushed at this interval.
    Interval string `mapstructure:"interval"`

    // Protocol is "http" or "grpc" (default: "http").
    // Only "http" is supported in this version.
    Protocol string `mapstructure:"protocol"`
}

// MetricsConfig is DEPRECATED. Use TelemetryConfig instead.
// This struct remains for backward compatibility and is mapped to TelemetryConfig
// during config loading.
type MetricsConfig struct {
    // Enabled toggles metrics collection and the /_vibewarden/metrics endpoint (default: true).
    Enabled bool `mapstructure:"enabled"`

    // PathPatterns is a list of URL path normalization patterns using :param syntax.
    PathPatterns []string `mapstructure:"path_patterns"`
}
```

**Add to `Load()` function:**

```go
// Defaults for telemetry.
v.SetDefault("telemetry.enabled", true)
v.SetDefault("telemetry.prometheus.enabled", true)
v.SetDefault("telemetry.otlp.enabled", false)
v.SetDefault("telemetry.otlp.interval", "30s")
v.SetDefault("telemetry.otlp.protocol", "http")
```

**Add migration helper in `internal/config/migrate.go`:**

```go
// MigrateLegacyMetrics converts legacy metrics config to telemetry config.
// If the user has a metrics: section but no telemetry: section, this function
// copies settings and logs a deprecation warning.
func MigrateLegacyMetrics(cfg *Config, logger *slog.Logger) {
    // Only migrate if telemetry is at defaults and metrics is customized.
    if cfg.Metrics.Enabled == false || len(cfg.Metrics.PathPatterns) > 0 {
        // User has customized metrics config, migrate it.
        cfg.Telemetry.Enabled = cfg.Metrics.Enabled
        cfg.Telemetry.PathPatterns = cfg.Metrics.PathPatterns
        cfg.Telemetry.Prometheus.Enabled = cfg.Metrics.Enabled

        logger.Warn("DEPRECATED: 'metrics:' config section is deprecated, use 'telemetry:' instead",
            slog.Bool("metrics_enabled", cfg.Metrics.Enabled),
            slog.Int("path_patterns", len(cfg.Metrics.PathPatterns)),
        )
    }
}
```

#### File Layout

**New files:**

```
internal/
  config/
    migrate.go                 # Legacy config migration helpers
    migrate_test.go            # Tests for migration
  adapters/
    otel/
      otlp.go                  # OTLP exporter helpers (optional, if extraction needed)
      otlp_test.go             # OTLP-specific unit tests
```

**Modified files:**

```
internal/
  ports/
    otel.go                    # Add TelemetryConfig, update OTelProvider interface
  config/
    config.go                  # Add TelemetryConfig, deprecate MetricsConfig
  adapters/
    otel/
      provider.go              # Add OTLP exporter support, update Init signature
      provider_test.go         # Add tests for OTLP exporter, dual-exporter mode
  plugins/
    metrics/
      plugin.go                # Update to use TelemetryConfig, pass to provider
      config.go                # Update Config struct to use TelemetryConfig fields
      plugin_test.go           # Update tests
```

#### Sequence

**Initialization flow (updated):**

1. `main()` loads config with `config.Load()`
2. `config.MigrateLegacyMetrics()` checks for deprecated `metrics:` section
3. If `metrics:` found but no `telemetry:`, copy settings and log warning
4. Plugin registry creates metrics plugin with `metrics.New(cfg, logger)`
5. Plugin registry calls `plugin.Init(ctx)`:
   a. Build `ports.TelemetryConfig` from config
   b. Create `oteladapter.Provider`
   c. Call `provider.Init(ctx, "vibewarden", version, telemetryCfg)`:
      - Validate: at least one exporter enabled
      - Validate: OTLP endpoint present if OTLP enabled
      - Create OTel Resource with service name/version
      - **If Prometheus enabled:**
        - Create Prometheus registry
        - Create Prometheus exporter with registry
        - Add to readers list
        - Create promhttp.Handler
      - **If OTLP enabled:**
        - Parse interval duration
        - Build OTLP HTTP exporter with endpoint, headers
        - Create PeriodicReader with exporter and interval
        - Add to readers list
      - Create MeterProvider with all readers
      - Set global MeterProvider
      - Create Meter for scope
   d. Create `metricsadapter.OTelAdapter(provider, pathPatterns)`
6. Plugin registry calls `plugin.Start(ctx)`:
   a. **If Prometheus enabled:**
      - Create internal HTTP server with adapter.Handler()
      - Server binds random localhost port
      - Store internal address for Caddy reverse-proxy
   b. **If only OTLP enabled:**
      - No internal server needed (push-based)
      - Plugin still contributes no Caddy routes

**OTLP push flow:**

1. HTTP request arrives at Caddy
2. `MetricsMiddleware` intercepts, records start time
3. Request proceeds through handler chain
4. On response, middleware calls MetricsCollector methods
5. OTelAdapter forwards to OTel instruments with attributes
6. OTel SDK aggregates observations in memory
7. **PeriodicReader** (every 30s by default):
   - Collects aggregated metrics from MeterProvider
   - Pushes to OTLP endpoint via HTTP POST
   - Endpoint returns 200 OK on success
8. On shutdown, `provider.Shutdown()` forces final flush

**Shutdown flow (updated):**

1. Plugin registry calls `plugin.Stop(ctx)`
2. **If Prometheus enabled:** Plugin stops internal HTTP server
3. Plugin calls `provider.Shutdown(ctx)`
4. MeterProvider triggers final export on all readers:
   - Prometheus exporter: no-op (pull-based)
   - OTLP exporter: flush pending metrics to endpoint
5. Wait for flush or context deadline
6. Exporters released

#### Error Cases

| Error | Cause | Handling |
|-------|-------|----------|
| `at least one exporter must be enabled` | Both Prometheus and OTLP disabled | Return error from Init |
| `OTLP endpoint required when OTLP exporter is enabled` | OTLP enabled but endpoint empty | Return error from Init |
| `creating otlp exporter` | Invalid endpoint URL | Return error from Init |
| `invalid interval duration` | Malformed interval string | Return error during config parsing |
| OTLP push fails (network error) | Collector unreachable | OTel SDK retries with backoff, logs warning |
| OTLP push fails (auth error) | Invalid headers/API key | OTel SDK logs error, continues trying |
| Shutdown timeout | Flush takes too long | Context deadline exceeded, may lose pending data |
| `unsupported protocol: grpc` | Protocol set to grpc | Return error from Init (grpc not implemented) |

**OTLP error handling philosophy:**

The OTel SDK handles transient OTLP export failures gracefully:
- Automatic retry with exponential backoff
- Logs export failures but does not crash
- Continues collecting metrics locally
- Next push interval attempts again

This matches the sidecar's resilience requirements: telemetry loss is acceptable,
crashes are not.

#### Test Strategy

**Unit tests:**

| File | Coverage |
|------|----------|
| `internal/adapters/otel/provider_test.go` | Init with various TelemetryConfig combinations |
| `internal/adapters/otel/otlp_test.go` | OTLP exporter creation, option translation |
| `internal/config/config_test.go` | TelemetryConfig parsing, defaults |
| `internal/config/migrate_test.go` | Legacy metrics config migration |
| `internal/plugins/metrics/plugin_test.go` | Updated plugin lifecycle with TelemetryConfig |

**Unit test cases for provider:**

1. Init with Prometheus only (current behavior, regression test)
2. Init with OTLP only (new behavior)
3. Init with both Prometheus and OTLP (dual-exporter)
4. Init with neither (error case)
5. Init with OTLP enabled but no endpoint (error case)
6. Init with custom OTLP headers
7. Init with custom OTLP interval
8. Shutdown flushes OTLP (verify call to exporter.Shutdown)
9. PrometheusEnabled/OTLPEnabled return correct values

**Integration tests:**

| Test | Coverage |
|------|----------|
| `internal/adapters/otel/provider_integration_test.go` | Full OTLP export to mock server |

**Integration test approach for OTLP:**

1. Start mock OTLP HTTP server (net/http/httptest)
2. Configure provider with mock server endpoint
3. Record metrics through adapter
4. Trigger manual flush via shutdown or short interval
5. Verify mock server received expected OTLP payload
6. Verify metric names, labels, values in payload

**What to mock vs. what to test real:**

- Mock: OTLP collector endpoint (httptest server)
- Real: Full OTel SDK stack, Prometheus exporter
- Skip: Real OTel Collector (tested in Docker Compose story #290)

#### New Dependencies

| Package | Version | License | Reason |
|---------|---------|---------|--------|
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` | v1.40.0 | Apache 2.0 | OTLP HTTP exporter for push-based metrics |

**License verification:** This package is part of the `opentelemetry-go` repository,
which is licensed under Apache 2.0 (verified in ADR-012). The package is already
a transitive dependency through Caddy (line 185 of go.mod), so promoting it to
a direct dependency does not increase binary size.

**No new transitive dependencies** are introduced; all required packages are
already in the dependency tree.

### Consequences

**Positive:**

- **Push-based export:** OTLP enables outbound-only telemetry, aligning with
  localhost-only security model. No inbound scrape ports needed.
- **Dual-mode support:** Users can run Prometheus (for local /metrics) AND OTLP
  (for central collection) simultaneously. Gradual migration path.
- **Fleet-ready:** Future Pro tier fleet dashboard can receive OTLP directly
  from local instances without scraping.
- **Vendor-neutral:** OTLP is CNCF standard; works with any OTLP-compatible backend
  (Grafana Cloud, Datadog, Honeycomb, self-hosted OTel Collector).
- **Backward compatible:** Legacy `metrics:` config continues to work with
  deprecation warning. No breaking changes for existing users.

**Negative:**

- **Configuration complexity:** TelemetryConfig has more options than MetricsConfig.
  Mitigated by sensible defaults (Prometheus enabled, OTLP disabled).
- **Network dependency:** OTLP requires network access to collector. If collector
  is down, metrics are lost after SDK buffer fills. Acceptable for observability.
- **No gRPC support yet:** Only HTTP protocol implemented. gRPC requires additional
  dependency (`otlpmetricgrpc`). Can be added in follow-up if needed.

**Trade-offs:**

- **Immediate flush vs. batching:** Chose batching with PeriodicReader (default 30s)
  for efficiency. Trade-off: up to 30s telemetry lag. Users can configure shorter
  intervals if needed.
- **Prometheus as default:** Kept Prometheus enabled by default for backward
  compatibility. New users may prefer OTLP-only, but this requires explicit opt-in.
- **Single OTLP endpoint:** No support for multiple OTLP endpoints. Users needing
  fan-out should use OTel Collector as aggregator.

**Migration path:**

1. **This story (ADR-013):** Add OTLP support, TelemetryConfig, deprecate MetricsConfig
2. **Story #288:** Prometheus fallback (ensure Prometheus still works as expected)
3. **Story #290:** OTel Collector in Docker Compose (OTLP receiver)
4. **Future:** Remove deprecated MetricsConfig after 2 minor versions

**Example `vibewarden.yaml` configurations:**

```yaml
# Prometheus only (current default, backward compatible)
telemetry:
  enabled: true
  prometheus:
    enabled: true
  otlp:
    enabled: false

# OTLP only (push to Grafana Cloud)
telemetry:
  enabled: true
  prometheus:
    enabled: false
  otlp:
    enabled: true
    endpoint: https://otlp-gateway-prod-us-central-0.grafana.net/otlp
    headers:
      Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
    interval: 30s

# Dual-mode (local scraping + central push)
telemetry:
  enabled: true
  path_patterns:
    - "/users/:id"
    - "/api/v1/items/:item_id"
  prometheus:
    enabled: true
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
    interval: 15s

# Legacy config (will be migrated with warning)
metrics:
  enabled: true
  path_patterns:
    - "/users/:id"
```

---

## ADR-014: Prometheus Fallback Exporter for Backward Compatibility
**Date**: 2026-03-28
**Issue**: #288
**Status**: Accepted

### Context

ADR-012 and ADR-013 established the OpenTelemetry SDK as the metrics foundation, with both
Prometheus (pull-based) and OTLP (push-based) export capabilities. The implementation uses
`go.opentelemetry.io/otel/exporters/prometheus` as the OTel-to-Prometheus bridge.

Issue #288 requires completing this migration by:

1. Removing the deprecated `prometheus/client_golang` direct adapter
2. Ensuring `/_vibewarden/metrics` continues to work as before
3. Validating that Prometheus is the automatic fallback when OTLP is not configured
4. Verifying metric names and labels remain identical (no breaking changes for dashboards)

**Current state after ADR-012/013:**

- OTel provider at `internal/adapters/otel/provider.go` already supports Prometheus export
- Metrics plugin already uses `OTelAdapter` for metric collection
- Config defaults: `prometheus.enabled = true`, `otlp.enabled = false`
- Legacy `prometheus.go` adapter still exists but is unused by the plugin

The old `PrometheusAdapter` in `internal/adapters/metrics/prometheus.go` is now dead code.
Some tests still reference it, creating maintenance burden and potential confusion.

### Decision

Complete the OTel migration by removing deprecated prometheus/client_golang adapter code
and updating all tests to use the OTel-based implementation. Add explicit integration tests
verifying backward compatibility of the `/_vibewarden/metrics` endpoint.

#### Domain Model Changes

None. This is a cleanup story with no domain impact.

#### Ports (Interfaces)

No changes. The existing `ports.MetricsCollector` and `ports.OTelProvider` interfaces
remain stable.

#### Adapters

**Files to delete:**

```
internal/adapters/metrics/prometheus.go           # REMOVE: replaced by otel.go
internal/adapters/metrics/prometheus_test.go      # REMOVE: replaced by otel_test.go
internal/adapters/metrics/prometheus_integration_test.go  # REMOVE: consolidate into otel tests
```

**Files to update:**

```
internal/adapters/metrics/server_test.go          # UPDATE: use OTelAdapter instead of PrometheusAdapter
internal/adapters/caddy/metrics_integration_test.go  # UPDATE: use OTelAdapter
```

#### Application Service

No changes to application services.

#### File Layout

**Files to delete:**

| File | Reason |
|------|--------|
| `internal/adapters/metrics/prometheus.go` | Replaced by `otel.go` |
| `internal/adapters/metrics/prometheus_test.go` | Replaced by `otel_test.go` |
| `internal/adapters/metrics/prometheus_integration_test.go` | Tests migrated to `otel_integration_test.go` |

**Files to modify:**

| File | Changes |
|------|---------|
| `internal/adapters/metrics/server_test.go` | Replace `NewPrometheusAdapter` with `NewOTelAdapter` using test provider |
| `internal/adapters/caddy/metrics_integration_test.go` | Replace `PrometheusAdapter` with `OTelAdapter` |
| `internal/adapters/otel/provider.go` | Add doc comment clarifying fallback behavior |
| `internal/config/config.go` | Add doc comment about Prometheus as default fallback |

**Files unchanged:**

| File | Status |
|------|--------|
| `internal/adapters/metrics/otel.go` | Already implements MetricsCollector via OTel |
| `internal/adapters/metrics/otel_test.go` | Already tests OTelAdapter |
| `internal/adapters/metrics/otel_integration_test.go` | Already tests HTTP scrape |
| `internal/adapters/metrics/noop.go` | Unchanged |
| `internal/adapters/metrics/path_matcher.go` | Unchanged, used by OTelAdapter |
| `internal/adapters/metrics/server.go` | Unchanged |
| `internal/plugins/metrics/plugin.go` | Already uses OTelAdapter |

#### Sequence

This story does not change any runtime sequences. The flows established in ADR-012
and ADR-013 remain unchanged:

1. On startup, metrics plugin builds `TelemetryConfig` from config
2. If `prometheus.enabled = true` (default), OTelProvider creates Prometheus exporter
3. If `otlp.enabled = true` AND endpoint provided, OTelProvider also creates OTLP exporter
4. If neither enabled, Init returns error "at least one exporter must be enabled"
5. Internal HTTP server serves `/metrics` via OTel Prometheus handler
6. Caddy reverse-proxies `/_vibewarden/metrics` to internal server

**Fallback behavior (clarified):**

With default config (no explicit `telemetry:` block):
- `prometheus.enabled = true` (default)
- `otlp.enabled = false` (default)
- Result: Prometheus-only export, same behavior as pre-OTel migration

This ensures zero-config backward compatibility.

#### Error Cases

No new error cases. Existing validation:

| Error | When | Handling |
|-------|------|----------|
| `at least one exporter must be enabled` | Both exporters explicitly disabled | Error from provider.Init |
| Invalid OTLP endpoint | OTLP enabled, bad URL | Error from provider.Init |

The key guarantee: users cannot accidentally end up with no metrics export unless
they explicitly disable both exporters, which is a conscious choice.

#### Test Strategy

**Unit tests to update:**

| File | Changes |
|------|---------|
| `internal/adapters/metrics/server_test.go` | Create test OTelProvider, pass to NewOTelAdapter |

The server tests need a handler; currently they use `NewPrometheusAdapter(nil).Handler()`.
After this change, they will use an OTel-backed handler via a test helper.

**Test helper to add in `internal/adapters/otel/testing.go`:**

```go
// NewTestProvider creates an OTelProvider with Prometheus enabled for testing.
// It initializes the provider and returns it ready for use.
func NewTestProvider(ctx context.Context) (*Provider, error) {
    p := NewProvider()
    cfg := ports.TelemetryConfig{
        Prometheus: ports.PrometheusExporterConfig{Enabled: true},
        OTLP:       ports.OTLPExporterConfig{Enabled: false},
    }
    if err := p.Init(ctx, "vibewarden-test", "0.0.0-test", cfg); err != nil {
        return nil, err
    }
    return p, nil
}
```

**Integration tests to update:**

| File | Changes |
|------|---------|
| `internal/adapters/caddy/metrics_integration_test.go` | Use OTelAdapter with test provider |

**Integration tests to verify (already exist in otel_integration_test.go):**

1. `/_vibewarden/metrics` returns HTTP 200
2. Response is valid Prometheus text format
3. Response contains expected metric names: `vibewarden_requests_total`, `vibewarden_request_duration_seconds`, etc.
4. Metric labels match expected format (method, status_code, path_pattern, etc.)
5. Go runtime metrics present (go_goroutines, go_memstats_*, etc.)

**New test case to add in otel_integration_test.go:**

```go
func TestOTelAdapter_MetricNamesMatchLegacyPrometheus(t *testing.T) {
    // Verify that all metric names exported via OTel Prometheus bridge
    // match the names that were exported by the direct Prometheus adapter.
    // This ensures dashboard compatibility.
    expectedMetrics := []string{
        "vibewarden_requests_total",
        "vibewarden_request_duration_seconds",
        "vibewarden_rate_limit_hits_total",
        "vibewarden_auth_decisions_total",
        "vibewarden_upstream_errors_total",
        "vibewarden_active_connections",
    }
    // ... scrape /_vibewarden/metrics and verify all expected metrics present
}
```

**What to mock vs. what to test real:**

- Real: Full OTel SDK, Prometheus exporter, HTTP scraping
- Mock: Nothing at unit level (OTel SDK is fast and deterministic)

#### New Dependencies

None. All required dependencies are already present:

| Package | Status | License |
|---------|--------|---------|
| `go.opentelemetry.io/otel/exporters/prometheus` | Already in go.mod | Apache 2.0 |
| `prometheus/client_golang` | Remains as transitive dep of OTel exporter | Apache 2.0 |

Note: `prometheus/client_golang` will remain in go.mod as a transitive dependency
of the OTel Prometheus exporter. We are removing direct usage, not the dependency itself.

### Consequences

**Positive:**

- **Cleaner codebase:** Remove ~300 lines of dead code (prometheus.go + tests)
- **Single path:** All metrics flow through OTel SDK, simplifying debugging
- **Consistent testing:** All tests use the same adapter type
- **Dashboard compatibility:** Metric names and labels unchanged
- **Fallback guaranteed:** Default config always enables Prometheus

**Negative:**

- **Test churn:** Several test files need updates (one-time cost)
- **Transitive dependency:** `prometheus/client_golang` remains in dependency tree
  via OTel exporter (unavoidable; OTel bridge requires it)

**Trade-offs:**

- **Keep vs. delete prometheus.go:** Chose deletion. Keeping dead code creates
  maintenance burden and confusion. The OTel bridge provides identical functionality.
- **Test helper vs. inline setup:** Chose helper (`NewTestProvider`) for DRY tests
  and clearer intent. Trade-off: one more file to maintain.

**Migration complete:**

After this story, the Prometheus export path is:

```
MetricsCollector interface
    -> OTelAdapter (internal/adapters/metrics/otel.go)
        -> OTel Meter instruments
            -> OTel MeterProvider
                -> Prometheus exporter (go.opentelemetry.io/otel/exporters/prometheus)
                    -> promhttp.Handler
                        -> /_vibewarden/metrics
```

The old path (`PrometheusAdapter -> prometheus.Registry -> promhttp.Handler`) is removed.

**Locked decision impact:**

CLAUDE.md line 33 states: "Metrics: prometheus/client_golang (Apache 2.0)"

This locked decision refers to the export format and backward compatibility, not the
internal SDK. The change is compliant because:

1. `/_vibewarden/metrics` still serves Prometheus format
2. `prometheus/client_golang` remains in the dependency tree (via OTel bridge)
3. Existing Prometheus scrapers and dashboards continue working

A future ADR may update the locked decision text to "Metrics: OpenTelemetry SDK
with Prometheus export" for accuracy, but this is documentation, not a breaking change.

---

## ADR-015: Bridge slog Structured Events to OTel Logs
**Date**: 2026-03-28
**Issue**: #289
**Status**: Accepted

### Context

VibeWarden's structured event logging follows a v1 schema with `schema_version`, `event_type`,
`timestamp`, `ai_summary`, and `payload` fields. This schema is the project's key differentiator
for AI-readable logs. Currently, events are emitted via the `ports.EventLogger` interface,
implemented by `SlogEventLogger` which writes JSON to stdout.

Issue #289 (part of epic #280 "Switch telemetry from Prometheus to OpenTelemetry") adds the
ability to export these structured events to an OpenTelemetry Collector via OTLP. This enables
users to:

1. Centralize logs alongside metrics in their observability backend (Grafana Cloud, Datadog, etc.)
2. Correlate log events with traces (future: when distributed tracing is added)
3. Use OTel's standard log pipeline for filtering, sampling, and routing

**Design constraints from the epic:**

- slog stays as the primary logging interface (locked decision L-08)
- OTel is an export path, not a replacement for stdout logging
- Use the OTel log bridge for slog (`go.opentelemetry.io/contrib/bridges/otelslog`)
- Bridge structured events to OTel log records, preserving the full schema
- OTLP log exporter shares the same endpoint config as metrics

**Current state:**

- `internal/adapters/log/slog_adapter.go` implements `ports.EventLogger` using a `slog.JSONHandler`
- `internal/adapters/otel/provider.go` initializes the `MeterProvider` for metrics
- OTel log SDK packages are already transitive dependencies (via Caddy):
  - `go.opentelemetry.io/otel/log v0.16.0`
  - `go.opentelemetry.io/otel/sdk/log v0.16.0`
  - `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.16.0`

### Decision

Add OTel log export as an optional, additive feature alongside stdout JSON logging.
When `telemetry.logs.otlp: true` is configured, structured events are written to both
stdout (existing behavior) and to the OTel Collector (new behavior).

#### Domain Model Changes

None. The `events.Event` struct remains unchanged. The domain layer has no knowledge of
OTel or log export mechanisms.

#### Ports (Interfaces)

**New interface in `internal/ports/otel.go`:**

```go
// LoggerProvider manages the OTel Log SDK lifecycle.
// It creates LoggerProviders that bridge slog events to OTel log records.
type LoggerProvider interface {
    // Handler returns an slog.Handler that bridges log records to OTel.
    // The handler emits logs with the configured service identity and resource attributes.
    // Returns nil if log export is disabled or Init has not been called.
    Handler() slog.Handler

    // Shutdown gracefully shuts down the LoggerProvider, flushing any buffered logs.
    Shutdown(ctx context.Context) error
}
```

**Extend `TelemetryConfig` in `internal/ports/otel.go`:**

```go
// TelemetryConfig holds all telemetry export settings.
type TelemetryConfig struct {
    // Prometheus enables the Prometheus pull-based exporter for metrics.
    Prometheus PrometheusExporterConfig

    // OTLP enables the OTLP push-based exporter for metrics.
    OTLP OTLPExporterConfig

    // Logs configures log export settings.
    Logs LogExportConfig
}

// LogExportConfig configures log export via OTLP.
type LogExportConfig struct {
    // OTLPEnabled toggles OTLP log export (default: false).
    // When enabled, logs are exported to the same OTLP endpoint as metrics.
    OTLPEnabled bool
}
```

**Note:** `EventLogger` interface remains unchanged. The bridging happens at the adapter level,
not the port level.

#### Adapters

**New file: `internal/adapters/otel/log_provider.go`**

Implements `ports.LoggerProvider`. Initializes the OTel `LoggerProvider` with an OTLP HTTP
exporter using the same endpoint as metrics. Creates an `otelslog.Handler` that bridges
slog records to OTel log records.

```go
// LogProvider implements ports.LoggerProvider using the OTel Log SDK.
type LogProvider struct {
    mu             sync.RWMutex
    loggerProvider *sdklog.LoggerProvider
    handler        slog.Handler
}

// NewLogProvider creates an uninitialized LogProvider.
func NewLogProvider() *LogProvider

// Init initializes the OTel LoggerProvider with an OTLP HTTP exporter.
// serviceName and serviceVersion are recorded as OTel resource attributes.
// otlpEndpoint must be provided when OTLPEnabled is true in cfg.
func (p *LogProvider) Init(ctx context.Context, serviceName, serviceVersion, otlpEndpoint string, cfg ports.LogExportConfig) error

// Handler returns the otelslog.Handler, or nil if Init has not been called or logs disabled.
func (p *LogProvider) Handler() slog.Handler

// Shutdown gracefully shuts down the LoggerProvider.
func (p *LogProvider) Shutdown(ctx context.Context) error
```

**Modify: `internal/adapters/log/slog_adapter.go`**

Add a multi-handler variant that fans out to multiple slog handlers:

```go
// MultiHandler is an slog.Handler that dispatches to multiple handlers.
// All handlers receive every log record. Errors are silently ignored
// (best-effort logging).
type MultiHandler struct {
    handlers []slog.Handler
}

// NewMultiHandler creates a handler that dispatches to all given handlers.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler

// Enabled returns true if any underlying handler is enabled for the level.
func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool

// Handle dispatches the record to all handlers.
func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error

// WithAttrs returns a new MultiHandler with the given attrs added to each handler.
func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler

// WithGroup returns a new MultiHandler with the given group name.
func (h *MultiHandler) WithGroup(name string) slog.Handler
```

**Modify: `internal/adapters/log/slog_adapter.go`**

Update `SlogEventLogger` to accept an optional list of additional handlers:

```go
// NewSlogEventLogger creates a SlogEventLogger that writes JSON to w.
// Additional handlers (e.g., OTel bridge) can be provided; events are
// dispatched to all handlers.
func NewSlogEventLogger(w io.Writer, additionalHandlers ...slog.Handler) *SlogEventLogger
```

When additional handlers are provided, the logger uses a `MultiHandler` combining the
JSON handler with the additional handlers.

#### Application Service

No changes to application services.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/adapters/otel/log_provider.go` | OTel LoggerProvider adapter |
| `internal/adapters/otel/log_provider_test.go` | Unit tests for LogProvider |
| `internal/adapters/log/multi_handler.go` | MultiHandler implementation |
| `internal/adapters/log/multi_handler_test.go` | Unit tests for MultiHandler |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/ports/otel.go` | Add `LoggerProvider` interface, `LogExportConfig` struct, extend `TelemetryConfig` |
| `internal/adapters/log/slog_adapter.go` | Accept additional handlers in constructor |
| `internal/adapters/log/slog_adapter_test.go` | Test multi-handler dispatch |
| `internal/config/config.go` | Add `Logs` field to `TelemetryConfig`, add config defaults |
| `internal/plugins/metrics/plugin.go` | Initialize LogProvider when logs.otlp enabled |
| `internal/plugins/metrics/config.go` | Add `LogsOTLPEnabled` field |
| `cmd/vibewarden/serve.go` | Pass OTel handler to event logger when enabled |

#### Sequence

**Startup (logs.otlp enabled):**

1. Config loads `telemetry.logs.otlp: true`
2. Metrics plugin `Init`:
   a. Creates OTel MeterProvider (existing)
   b. Creates OTel LoggerProvider with OTLP HTTP exporter (new)
   c. LoggerProvider uses same endpoint as metrics OTLP exporter
3. `serve.go` retrieves OTel log handler from metrics plugin
4. Creates `SlogEventLogger` with JSON handler + OTel handler (multi-handler)
5. Events logged via `EventLogger.Log()` are dispatched to both handlers

**Runtime (event emitted):**

```
1. app/middleware calls EventLogger.Log(event)
2. SlogEventLogger.Log() serializes event to slog.LogAttrs()
3. MultiHandler.Handle() dispatches to:
   a. JSON handler -> stdout (existing behavior)
   b. OTel handler -> LoggerProvider -> BatchProcessor -> OTLP exporter
4. OTLP exporter batches and pushes to collector endpoint
```

**Shutdown:**

1. Plugin Stop called
2. LoggerProvider.Shutdown() flushes pending log batches
3. MeterProvider.Shutdown() flushes pending metrics (existing)

#### OTel Log Record Mapping

Each VibeWarden event maps to an OTel log record as follows:

| Event field | OTel log record field |
|-------------|----------------------|
| `Timestamp` | `Timestamp` |
| `EventType` | Attribute: `event.type` |
| `SchemaVersion` | Attribute: `vibewarden.schema_version` |
| `AISummary` | `Body` (string) |
| `Payload.*` | Attributes: `vibewarden.payload.<key>` |

**Severity mapping:**

Event types are mapped to OTel severity levels based on their semantic meaning:

| Event type pattern | OTel Severity |
|-------------------|---------------|
| `*.failed`, `*.blocked`, `*.hit` | WARN (13) |
| `*.unavailable`, `*_failed` | ERROR (17) |
| `*.success`, `*.created`, `*.started`, `*.recovered` | INFO (9) |
| Default | INFO (9) |

The severity mapping is implemented as a pure function in `log_provider.go`:

```go
func severityForEventType(eventType string) log.Severity {
    switch {
    case strings.HasSuffix(eventType, ".failed"),
         strings.HasSuffix(eventType, ".blocked"),
         strings.HasSuffix(eventType, ".hit"):
        return log.SeverityWarn
    case strings.HasSuffix(eventType, ".unavailable"),
         strings.HasSuffix(eventType, "_failed"):
        return log.SeverityError
    default:
        return log.SeverityInfo
    }
}
```

#### Error Cases

| Error | When | Handling |
|-------|------|----------|
| OTLP endpoint missing | `logs.otlp: true` but no `otlp.endpoint` | Error from LogProvider.Init |
| Collector unreachable | Network failure | OTLP exporter retries with backoff; logs are dropped after retry exhaustion (best-effort) |
| Invalid log record | Malformed event payload | OTel SDK logs warning; record skipped |

**Graceful degradation:**

- Stdout logging always works (direct I/O, no network)
- OTel log export is best-effort; failures do not block request processing
- If LogProvider fails to initialize, serve.go falls back to stdout-only logging

#### Test Strategy

**Unit tests:**

| File | Tests |
|------|-------|
| `internal/adapters/otel/log_provider_test.go` | Init with valid config; Init fails without endpoint; Shutdown idempotent |
| `internal/adapters/log/multi_handler_test.go` | Dispatches to all handlers; WithAttrs/WithGroup propagate; Enabled returns true if any enabled |
| `internal/adapters/log/slog_adapter_test.go` | Log with additional handlers; verify both handlers receive records |

**Integration tests:**

| File | Tests |
|------|-------|
| `internal/adapters/otel/log_provider_integration_test.go` | Full roundtrip: emit event -> verify log record attributes |

**What to mock vs. real:**

- Real: OTel LoggerProvider, MultiHandler, SlogEventLogger
- Mock: OTLP endpoint (use `httptest.Server` to capture exported logs)

**Test helper (add to `internal/adapters/otel/testing.go`):**

```go
// NewTestLogProvider creates a LoggerProvider with an in-memory exporter for testing.
// Returns the provider and a function to retrieve exported log records.
func NewTestLogProvider(ctx context.Context) (*LogProvider, func() []sdklog.ReadOnlyLogRecord, error)
```

#### New Dependencies

**Direct dependency to add:**

| Package | Version | License | Purpose |
|---------|---------|---------|---------|
| `go.opentelemetry.io/contrib/bridges/otelslog` | latest | Apache 2.0 | Bridge slog to OTel log SDK |

**License verification:**

The `opentelemetry-go-contrib` repository is licensed under Apache 2.0:
https://github.com/open-telemetry/opentelemetry-go-contrib/blob/main/LICENSE

All packages in the contrib repository share this license, including `bridges/otelslog`.

**Already transitive dependencies (no action needed):**

| Package | Version | License |
|---------|---------|---------|
| `go.opentelemetry.io/otel/log` | v0.16.0 | Apache 2.0 |
| `go.opentelemetry.io/otel/sdk/log` | v0.16.0 | Apache 2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp` | v0.16.0 | Apache 2.0 |

#### Configuration

**New config fields in `vibewarden.yaml`:**

```yaml
telemetry:
  # Existing fields...
  prometheus:
    enabled: true
  otlp:
    enabled: true
    endpoint: http://localhost:4318
  # New field:
  logs:
    otlp: false  # default: false (opt-in)
```

**Config struct changes in `internal/config/config.go`:**

```go
type TelemetryConfig struct {
    Enabled      bool                     `mapstructure:"enabled"`
    PathPatterns []string                 `mapstructure:"path_patterns"`
    Prometheus   PrometheusExporterConfig `mapstructure:"prometheus"`
    OTLP         OTLPExporterConfig       `mapstructure:"otlp"`
    Logs         LogsConfig               `mapstructure:"logs"`
}

type LogsConfig struct {
    // OTLP toggles OTLP log export (default: false).
    // When enabled, structured events are exported to the same OTLP endpoint as metrics.
    // Requires telemetry.otlp.endpoint to be configured.
    OTLP bool `mapstructure:"otlp"`
}
```

**Defaults:**

```go
v.SetDefault("telemetry.logs.otlp", false)
```

**Validation:**

```go
// If logs.otlp enabled, otlp.endpoint must be set
if c.Telemetry.Logs.OTLP && c.Telemetry.OTLP.Endpoint == "" {
    errs = append(errs, "telemetry.logs.otlp requires telemetry.otlp.endpoint")
}
```

### Consequences

**Positive:**

- **Unified observability:** Logs, metrics, and (future) traces all flow through OTel
- **AI-readable logs preserved:** Schema unchanged; OTel is purely an export path
- **Stdout always works:** OTel export is additive; existing behavior unchanged
- **Shared config:** Logs use same OTLP endpoint as metrics (DRY)
- **Future-proof:** When distributed tracing is added, logs can be correlated via trace context

**Negative:**

- **New dependency:** `otelslog` bridge adds to binary size (~small)
- **Complexity:** Multi-handler dispatch adds a layer of indirection
- **Batch delay:** OTLP export is batched; logs appear with ~1-30s delay in collector

**Trade-offs:**

- **Multi-handler vs. separate loggers:** Chose multi-handler. Alternative was to have
  two separate `EventLogger` implementations called sequentially. Multi-handler is more
  composable and follows slog idioms.

- **Severity in event vs. derived:** Chose derived from event_type. Alternative was to
  add a Severity field to `events.Event`. Derived keeps domain layer clean and works
  well for the current event type taxonomy.

- **Same endpoint vs. separate:** Chose same endpoint. Alternative was separate
  `telemetry.logs.endpoint`. Shared endpoint is simpler and matches how most users
  deploy OTel Collector (single receiver for all signals).

**Limitations:**

- Log export is push-only (no pull equivalent like Prometheus metrics)
- OTel log SDK is still maturing (v0.x); API may change in future OTel releases
- No trace correlation yet (requires #293: distributed tracing setup)

---

## ADR-016: OTel Collector in Docker Compose Observability Stack
**Date**: 2026-03-28
**Issue**: #290
**Status**: Accepted

### Context

Epic #280 ("Switch telemetry from Prometheus to OpenTelemetry") transitions VibeWarden from
pull-based Prometheus scraping to push-based OTLP. Previous stories in this epic added:

- ADR-012: OTel SDK integration and MetricsCollector port/adapter refactoring
- ADR-013: OTLP exporter configuration
- ADR-014: Prometheus fallback exporter for backward compatibility
- ADR-015: Bridge slog structured events to OTel logs

The current observability stack (docker-compose observability profile) has:

- **Prometheus**: Scrapes `/_vibewarden/metrics` from the sidecar (pull)
- **Promtail**: Scrapes Docker container logs and pushes to Loki
- **Loki**: Receives logs from Promtail
- **Grafana**: Visualizes Prometheus metrics and Loki logs

With OTel, the sidecar now pushes metrics and logs via OTLP. The stack needs an
**OTel Collector** to receive OTLP from the sidecar and export to Prometheus and Loki.
This replaces the direct Prometheus scraping model.

**Goals from issue #290:**

1. Add `otel-collector` service to Docker Compose observability profile
2. Collector receives OTLP HTTP on port 4318 from VibeWarden
3. Collector exports metrics to Prometheus (via Prometheus exporter for scraping)
4. Collector exports logs to Loki via loki exporter
5. Remove or deprecate Promtail (OTel Collector replaces its function for VibeWarden logs)
6. Keep Grafana dashboards working with minimal changes
7. Healthcheck on collector service

### Decision

Add the OpenTelemetry Collector Contrib as the telemetry hub in the Docker Compose
observability stack. The collector acts as a central aggregation point:

```
VibeWarden --OTLP--> OTel Collector --metrics--> Prometheus --scrape--> Prometheus
                                    --logs--> Loki
```

**Architecture change:**

| Before (pull) | After (push via collector) |
|---------------|---------------------------|
| VibeWarden exposes `/_vibewarden/metrics` | VibeWarden pushes OTLP to collector |
| Prometheus scrapes VibeWarden directly | Prometheus scrapes collector's Prometheus exporter |
| Promtail scrapes Docker logs | OTel Collector receives OTLP logs from VibeWarden |
| | Collector pushes logs to Loki |

**Key decisions:**

1. **Use `otel/opentelemetry-collector-contrib` image**: The contrib distribution includes
   the `lokiexporter` required for Loki integration. License: Apache 2.0.

2. **Prometheus exporter (not remote write)**: The collector exposes a `/metrics` endpoint
   that Prometheus scrapes. This keeps Prometheus in its natural pull mode and requires
   minimal Prometheus config changes. Alternative was `prometheusremotewrite` exporter,
   but that requires enabling remote-write receiver in Prometheus and adds complexity.

3. **Keep Promtail for non-VibeWarden logs**: Promtail continues to scrape Docker logs
   for other containers (app, kratos, etc.). The OTel Collector handles only VibeWarden's
   structured event logs via OTLP. This avoids losing logs from services that do not
   speak OTLP.

4. **Collector port 4318 (OTLP HTTP)**: Standard OTLP HTTP port. The collector binds to
   4318 inside the Docker network; VibeWarden's default `telemetry.otlp.endpoint` in dev
   compose points to `http://otel-collector:4318`.

5. **Collector metrics endpoint on 8889**: The Prometheus exporter exposes metrics at
   `otel-collector:8889/metrics`. Prometheus scrapes this instead of VibeWarden directly.

#### Domain Model Changes

None. This story is pure infrastructure (Docker Compose and config templates). No changes
to domain entities, value objects, or events.

#### Ports (Interfaces)

None. No new Go interfaces. The OTel Collector is an external container, not embedded
in the VibeWarden binary.

#### Adapters

None. No Go adapter changes. The existing OTLP exporter in `internal/adapters/otel/`
already supports pushing to any OTLP endpoint.

#### Application Service

**Modify: `internal/app/generate/service.go`**

Add rendering of the OTel Collector config template. Update `generateObservability()` to:

1. Create `observability/otel-collector/` directory
2. Render `otel-collector-config.yml.tmpl` to `observability/otel-collector/config.yaml`

```go
// In generateObservability():

// Create otel-collector directory
dirs := []string{
    // ... existing dirs ...
    filepath.Join(obsDir, "otel-collector"),
}

// Render OTel Collector config
if err := s.renderer.RenderToFile(
    "observability/otel-collector-config.yml.tmpl",
    cfg,
    filepath.Join(obsDir, "otel-collector", "config.yaml"),
    true,
); err != nil {
    return fmt.Errorf("rendering otel-collector config: %w", err)
}
```

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/config/templates/observability/otel-collector-config.yml.tmpl` | OTel Collector YAML config template |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/config/templates/docker-compose.yml.tmpl` | Add `otel-collector` service under observability profile |
| `internal/config/templates/observability/prometheus.yml.tmpl` | Scrape `otel-collector:8889` instead of `vibewarden` |
| `internal/app/generate/service.go` | Add otel-collector directory creation and config rendering |
| `internal/app/generate/service_test.go` | Add test for otel-collector config generation |

#### New Template: otel-collector-config.yml.tmpl

```yaml
# OTel Collector configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.
#
# Receives OTLP from VibeWarden sidecar, exports to Prometheus and Loki.

receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
    timeout: 10s
    send_batch_size: 1024

exporters:
  # Prometheus exporter: exposes /metrics for Prometheus to scrape
  prometheus:
    endpoint: 0.0.0.0:8889
    namespace: vibewarden
    const_labels:
      source: otel_collector

  # Loki exporter: pushes logs to Loki
  loki:
    endpoint: http://loki:3100/loki/api/v1/push
    default_labels_enabled:
      exporter: false
      job: true
    labels:
      attributes:
        event.type: "event_type"
        vibewarden.schema_version: "schema_version"
      resource:
        service.name: "service"

  # Debug exporter for troubleshooting (logs to stdout)
  debug:
    verbosity: basic

service:
  telemetry:
    logs:
      level: warn
    metrics:
      level: none
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [prometheus]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [loki]
```

#### Modified Template: docker-compose.yml.tmpl

Add under observability services (after promtail, before grafana):

```yaml
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.123.0
    profiles:
      - observability
    restart: unless-stopped
    command: ["--config=/etc/otelcol-contrib/config.yaml"]
    volumes:
      - ./.vibewarden/generated/observability/otel-collector/config.yaml:/etc/otelcol-contrib/config.yaml:ro
    networks:
      - vibewarden
    depends_on:
      loki:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:13133/"]
      interval: 10s
      timeout: 5s
      retries: 5
```

**Notes:**
- Port 13133 is the OTel Collector's default health check extension port
- Port 4318 (OTLP HTTP) is exposed only within the Docker network (no host binding needed)
- Port 8889 (Prometheus exporter) is exposed only within the Docker network
- Collector depends on Loki being healthy (for log export)

**Update vibewarden service environment:**

When observability is enabled, set the OTLP endpoint to point to the collector:

```yaml
{{- if .Observability.Enabled }}
      - VIBEWARDEN_TELEMETRY_OTLP_ENABLED=true
      - VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT=http://otel-collector:4318
      - VIBEWARDEN_TELEMETRY_LOGS_OTLP=true
{{- end }}
```

**Update Grafana depends_on:**

Grafana should also depend on otel-collector for a clean startup sequence:

```yaml
    depends_on:
      prometheus:
        condition: service_healthy
      loki:
        condition: service_healthy
      otel-collector:
        condition: service_healthy
```

#### Modified Template: prometheus.yml.tmpl

Change the vibewarden scrape target to scrape the OTel Collector's Prometheus exporter:

```yaml
# Prometheus configuration — Generated by VibeWarden
# Do not edit manually — re-run `vibewarden generate` to regenerate.

global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'otel-collector'
    metrics_path: '/metrics'
    static_configs:
      - targets: ['otel-collector:8889']
        labels:
          instance: 'vibewarden-sidecar'

  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']
```

**Note:** The job_name remains descriptive. The `instance` label preserves dashboard
compatibility. Metric names remain unchanged because the OTel SDK produces the same
metric names as before.

#### Sequence

**Startup sequence (docker compose --profile observability up):**

1. Loki starts and becomes healthy
2. OTel Collector starts, depends on Loki healthy
3. Prometheus starts, scrapes OTel Collector
4. Promtail starts, scrapes Docker logs (non-VibeWarden containers)
5. Grafana starts, depends on Prometheus and Loki healthy
6. VibeWarden starts, pushes OTLP to OTel Collector

**Runtime telemetry flow:**

```
VibeWarden sidecar
    |
    +-- OTLP HTTP (metrics + logs) --> otel-collector:4318
                                              |
                                              +-- metrics --> :8889/metrics
                                              |                   |
                                              |                   v
                                              |            Prometheus scrapes
                                              |                   |
                                              |                   v
                                              |              Grafana
                                              |
                                              +-- logs --> loki:3100
                                                               |
                                                               v
                                                           Grafana
```

**Promtail parallel flow (unchanged):**

```
Docker containers (app, kratos, etc.)
    |
    +-- Docker logs --> Promtail --> Loki --> Grafana
```

#### Error Cases

| Error | When | Handling |
|-------|------|----------|
| Collector not reachable | Network partition, collector down | OTLP exporter retries with exponential backoff; sidecar continues operating (telemetry is best-effort) |
| Loki not reachable | Loki down | Collector buffers logs, retries; eventually drops if buffer full |
| Prometheus not scraping | Prometheus down | Metrics accumulate in collector; no data loss until collector restart |
| Invalid OTLP payload | Bug in sidecar | Collector logs error, drops invalid records |
| Collector unhealthy | Crash loop | Grafana won't start (depends_on); Docker restarts collector |

**Graceful degradation:**

- VibeWarden sidecar continues operating even if collector is unreachable
- Prometheus fallback exporter (`/_vibewarden/metrics`) remains available for direct scraping
- Promtail continues collecting non-VibeWarden logs independently

#### Test Strategy

**Unit tests:**

| File | Tests |
|------|-------|
| `internal/app/generate/service_test.go` | Verify otel-collector directory created; verify config.yaml rendered; verify docker-compose includes otel-collector service |

**Integration tests (manual or CI):**

| Test | Verification |
|------|--------------|
| `docker compose --profile observability up` | All services start and become healthy |
| Send request through sidecar | Metrics appear in Prometheus via collector |
| Trigger structured event | Log appears in Loki via collector |
| Grafana dashboard | Existing dashboards show data |

**What to mock vs. real:**

- Real: Template rendering, file system operations
- Mock: None needed for unit tests (template rendering is deterministic)

#### New Dependencies

**Docker image (not a Go dependency):**

| Image | Version | License | Purpose |
|-------|---------|---------|---------|
| `otel/opentelemetry-collector-contrib` | 0.123.0 | Apache 2.0 | OTel Collector with Loki exporter |

**License verification:**

The OpenTelemetry Collector Contrib repository is licensed under Apache 2.0:
https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/LICENSE

Verified via:
```bash
curl -s https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/LICENSE | head -5
```

Output confirms Apache License Version 2.0.

**No new Go dependencies.** The OTel Collector runs as a separate container.

#### Configuration

**No new config fields in vibewarden.yaml.** The OTLP endpoint is set via environment
variables in docker-compose.yml when the observability profile is active.

**Environment variables set by docker-compose (observability profile):**

| Variable | Value | Purpose |
|----------|-------|---------|
| `VIBEWARDEN_TELEMETRY_OTLP_ENABLED` | `true` | Enable OTLP export |
| `VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT` | `http://otel-collector:4318` | Collector endpoint |
| `VIBEWARDEN_TELEMETRY_LOGS_OTLP` | `true` | Enable log export via OTLP |

Users can override these in their own compose files or environment if they want to
point to a different collector.

### Consequences

**Positive:**

- **Unified telemetry hub:** Metrics and logs flow through a single collector
- **Push-based model:** No need for sidecar to expose metrics endpoint to external scrapers
- **Vendor-neutral:** Users can swap the collector's exporters to send data anywhere
- **Standard OTel pipeline:** Follows industry best practices for observability
- **Backward compatible:** Grafana dashboards work unchanged (metric names preserved)
- **Promtail coexists:** Non-VibeWarden logs still flow via Promtail

**Negative:**

- **Additional container:** One more service to run (small resource footprint)
- **Complexity:** Adds a hop between sidecar and backends
- **Version management:** Must track OTel Collector Contrib releases

**Trade-offs:**

- **Prometheus exporter vs. remote write:** Chose exporter. Remote write requires
  Prometheus config changes (enable receiver) and is less common in local dev setups.
  Exporter keeps Prometheus in its natural pull mode.

- **Keep Promtail vs. remove:** Chose keep. Removing Promtail would lose logs from
  other containers (app, kratos) that do not speak OTLP. Promtail is lightweight and
  handles non-VibeWarden logs.

- **Single collector vs. per-signal:** Chose single collector with multiple pipelines.
  Simpler to operate than separate collectors for metrics and logs.

**Future considerations:**

- When distributed tracing is added (#293), the collector will handle traces too
- Fleet dashboard (Pro tier) can point to a cloud-hosted OTel Collector instead of local
- Users with existing collectors can disable the bundled one and point sidecar directly

## ADR-017: Update Grafana Dashboards for OTel-Sourced Metrics
**Date**: 2026-03-28
**Issue**: #291
**Status**: Accepted

### Context

Epic #280 ("Switch telemetry from Prometheus to OpenTelemetry") transitions VibeWarden from
pull-based Prometheus scraping to push-based OTLP. Previous stories in this epic added:

- ADR-012: OTel SDK integration and MetricsCollector port/adapter refactoring
- ADR-016: OTel Collector in Docker Compose observability stack

The existing Grafana dashboard (`vibewarden-dashboard.json`) has PromQL and LogQL queries
designed for the original telemetry path:

1. **Metrics**: Prometheus scraped VibeWarden directly at `/_vibewarden/metrics`
2. **Logs**: Promtail scraped Docker container logs with label `container="vibewarden"`

With the new OTel pipeline:

1. **Metrics**: VibeWarden pushes OTLP to OTel Collector, which exports to Prometheus
2. **Logs**: VibeWarden pushes OTLP logs to OTel Collector, which exports to Loki

**Issue #1: Double namespace prefix**

The OTel Collector config (`otel-collector-config.yml.tmpl`) has:

```yaml
exporters:
  prometheus:
    namespace: vibewarden
```

Meanwhile, the OTel adapter (`internal/adapters/metrics/otel.go`) already names metrics with
the `vibewarden_` prefix:

- `vibewarden_requests_total`
- `vibewarden_request_duration_seconds`
- `vibewarden_rate_limit_hits_total`
- etc.

The combination produces double-prefixed metric names like `vibewarden_vibewarden_requests_total`,
which breaks all dashboard PromQL queries.

**Issue #2: Loki label changes**

The dashboard LogQL queries use `{container="vibewarden"}`:

```
{container="vibewarden"} | json
{container="vibewarden"} | json | event_type =~ "auth.*"
```

This relies on Promtail's automatic `container` label from Docker. With OTel Collector's Loki
exporter, logs arrive with different labels. Per ADR-016, the Loki exporter is configured to
use `service.name` resource attribute mapped to `service` label:

```yaml
exporters:
  loki:
    labels:
      resource:
        service.name: "service"
```

Logs from VibeWarden will have `{service="vibewarden"}`, not `{container="vibewarden"}`.

**Issue #3: OTel scope labels**

The OTel Prometheus exporter may add `otel_scope_name` and `otel_scope_version` labels to
metrics. Dashboard queries should be resilient to these additional labels.

### Decision

Fix the OTel Collector configuration and update the Grafana dashboard to work with
OTel-sourced metrics and logs. This is a configuration-only change (no Go code changes).

#### Domain Model Changes

None. This story is pure infrastructure (config templates and dashboard JSON).

#### Ports (Interfaces)

None. No Go code changes.

#### Adapters

None. No Go code changes.

#### Application Service

None. No Go code changes.

#### File Layout

**Modified files:**

| File | Changes |
|------|---------|
| `internal/config/templates/observability/otel-collector-config.yml.tmpl` | Remove `namespace: vibewarden` from Prometheus exporter |
| `internal/config/templates/observability/vibewarden-dashboard.json` | Update Loki queries from `container="vibewarden"` to `service="vibewarden"` |

No new files.

#### Changes

**1. Fix OTel Collector Prometheus exporter (remove double prefix)**

In `internal/config/templates/observability/otel-collector-config.yml.tmpl`, change:

```yaml
exporters:
  prometheus:
    endpoint: 0.0.0.0:8889
    namespace: vibewarden  # REMOVE THIS LINE
    const_labels:
      source: otel_collector
```

To:

```yaml
exporters:
  prometheus:
    endpoint: 0.0.0.0:8889
    const_labels:
      source: otel_collector
```

**Why:** The OTel adapter already names metrics with `vibewarden_` prefix. The Prometheus
exporter's `namespace` option prepends another prefix, resulting in `vibewarden_vibewarden_*`.
Removing the namespace preserves the original metric names that the dashboard expects.

**2. Update Loki queries in dashboard**

In `internal/config/templates/observability/vibewarden-dashboard.json`, update all Loki
queries from `{container="vibewarden"}` to `{service="vibewarden"}`:

| Panel ID | Panel Title | Old Query | New Query |
|----------|-------------|-----------|-----------|
| 20 | Log Stream | `{container="vibewarden"} \| json` | `{service="vibewarden"} \| json` |
| 21 | Auth Events | `{container="vibewarden"} \| json \| event_type =~ "auth.*"` | `{service="vibewarden"} \| json \| event_type =~ "auth.*"` |
| 22 | Rate Limit Events | `{container="vibewarden"} \| json \| event_type =~ "rate_limit.*"` | `{service="vibewarden"} \| json \| event_type =~ "rate_limit.*"` |
| 23 | Security Events | `{container="vibewarden"} \| json \| event_type =~ "security.*" or ...` | `{service="vibewarden"} \| json \| event_type =~ "security.*" or ...` |

**Why:** OTel Collector's Loki exporter uses the `service.name` resource attribute (mapped to
`service` label) instead of Docker's `container` label. VibeWarden sets `service.name` to
`"vibewarden"` in the OTel provider initialization.

**3. PromQL queries remain unchanged**

The existing PromQL queries do not need changes:

- `sum(rate(vibewarden_requests_total[5m])) by (status_code)` — works as-is
- `histogram_quantile(0.50, sum(rate(vibewarden_request_duration_seconds_bucket[5m])) by (le))` — works as-is
- etc.

The OTel Prometheus exporter may add `otel_scope_name` and `otel_scope_version` labels, but
these are stripped by the `sum(...) by (label)` aggregations in all queries. No changes needed.

#### Sequence

No runtime sequence changes. This is a configuration fix.

**Verification sequence:**

1. Run `vibewarden generate` to regenerate observability configs
2. Start the observability stack: `docker compose --profile observability up -d`
3. Send traffic through the sidecar to generate metrics and logs
4. Open Grafana at `http://localhost:3000`
5. Navigate to the VibeWarden dashboard
6. Verify all 8 metric panels render data (panels 1-7)
7. Verify all 4 log panels show logs (panels 20-23)

#### Error Cases

| Error | When | Handling |
|-------|------|----------|
| Metrics missing in Grafana | OTel Collector not running or unhealthy | Check `docker compose ps` for collector health |
| Logs missing in Grafana | Loki exporter misconfigured | Check collector logs for Loki export errors |
| No data in dashboard | Sidecar not sending OTLP | Verify `VIBEWARDEN_TELEMETRY_OTLP_ENABLED=true` |

#### Test Strategy

**Unit tests:**

None. Config file changes; no Go code.

**Integration tests (manual or CI):**

| Test | Verification |
|------|--------------|
| Start observability stack | `docker compose --profile observability up -d` succeeds |
| Send requests | `curl http://localhost:8080/health` generates metrics |
| Check Prometheus | Query `vibewarden_requests_total` returns data (not `vibewarden_vibewarden_requests_total`) |
| Check Grafana metrics | All 7 metric panels (1-7) render charts with data |
| Check Grafana logs | All 4 log panels (20-23) show log entries |

**What to mock vs. real:**

- Real: Full observability stack (Docker Compose)
- Mock: None (end-to-end verification required)

#### New Dependencies

None. No new Go dependencies or Docker images.

### Consequences

**Positive:**

- **Dashboard works with OTel pipeline:** All panels render correctly after the fix
- **No metric name changes:** Preserves original metric names for backward compatibility
- **Clean label scheme:** Logs use semantic `service` label instead of Docker-specific `container`
- **No code changes:** Pure configuration fix, minimal risk

**Negative:**

- **Promtail logs incompatible:** If users also run Promtail for VibeWarden logs (not recommended),
  they will see `container="vibewarden"` labels while OTel logs have `service="vibewarden"`.
  This is acceptable because ADR-016 established that VibeWarden logs should flow via OTLP,
  not Promtail.

**Trade-offs:**

- **Dual label compatibility vs. clean break:** Could have made Loki queries match both
  `{container="vibewarden"} or {service="vibewarden"}`, but this adds complexity and the
  Promtail path is deprecated for VibeWarden logs. Clean break is simpler.

---

## ADR-018: Telemetry Documentation and Configuration Guide
**Date**: 2026-03-28
**Issue**: #292
**Status**: Accepted

### Context

Epic #280 ("Switch telemetry from Prometheus to OpenTelemetry") established a comprehensive
OTel-based telemetry pipeline through six implementation stories (ADR-012 through ADR-017):

- ADR-012: OTel SDK integration and MetricsCollector port/adapter refactoring
- ADR-013: OTLP exporter configuration and TelemetryConfig refactor
- ADR-014: Prometheus fallback exporter for backward compatibility
- ADR-015: Bridge slog structured events to OTel logs
- ADR-016: OTel Collector in Docker Compose observability stack
- ADR-017: Update Grafana dashboards for OTel-sourced metrics

Issue #292 is the documentation capstone for this epic. Users need clear documentation
explaining:

1. The new `telemetry:` config section and all its options
2. The three export modes: Prometheus-only (default), OTLP-only, dual-export
3. How the OTel Collector fits into the observability stack
4. How slog structured events are bridged to OTel logs
5. Migration path from legacy `metrics:` config to new `telemetry:` config
6. Example configurations for common backends

**Target audience:** Vibe coders who want zero-to-secure in minutes. Documentation must be
concise and practical, not an OTel tutorial.

### Decision

Create comprehensive telemetry documentation by:

1. Updating `docs/observability.md` with a new "Telemetry Configuration" section
2. Updating `vibewarden.example.yaml` with the full `telemetry:` section and comments
3. Adding migration guidance for users with existing `metrics:` config

This is a docs-only story. No Go code changes are expected.

#### Domain Model Changes

None. Documentation only.

#### Ports (Interfaces)

None. Documentation only.

#### Adapters

None. Documentation only.

#### Application Service

None. Documentation only.

#### File Layout

**Modified files:**

| File | Changes |
|------|---------|
| `docs/observability.md` | Add "Telemetry Configuration" section covering all export modes, OTel Collector architecture, slog-to-OTel bridge, migration guide |
| `vibewarden.example.yaml` | Add `telemetry:` section with all options and inline documentation |

**No new files.** All documentation is consolidated into existing files.

#### Documentation Structure

**1. Update `docs/observability.md`**

Add new sections after the existing "Quick Start" section:

```markdown
## Telemetry Configuration

VibeWarden uses OpenTelemetry as its telemetry foundation, supporting both pull-based
Prometheus scraping and push-based OTLP export. The `telemetry:` section in
`vibewarden.yaml` controls all telemetry behavior.

### Export Modes

VibeWarden supports three telemetry export modes:

| Mode | Prometheus | OTLP | Use Case |
|------|------------|------|----------|
| **Prometheus-only** (default) | Enabled | Disabled | Local development, single-instance deployments |
| **OTLP-only** | Disabled | Enabled | Cloud backends (Grafana Cloud, Datadog), fleet deployments |
| **Dual-export** | Enabled | Enabled | Migration, local + central collection |

### Prometheus-Only Mode (Default)

This is the zero-config default. VibeWarden exposes metrics at `/_vibewarden/metrics`
in Prometheus text format. No outbound connections are made.

```yaml
telemetry:
  enabled: true
  prometheus:
    enabled: true
  otlp:
    enabled: false
```

Or simply omit the `telemetry:` block entirely — the defaults match this configuration.

**When to use:** Local development, single-instance production where you run your own
Prometheus and scrape VibeWarden directly.

### OTLP-Only Mode

Metrics are pushed to an OTLP-compatible collector or backend. The `/_vibewarden/metrics`
endpoint is disabled. All telemetry flows outbound.

```yaml
telemetry:
  enabled: true
  prometheus:
    enabled: false
  otlp:
    enabled: true
    endpoint: https://otlp-gateway.example.com/otlp
    headers:
      Authorization: "Bearer ${OTLP_API_KEY}"
    interval: 30s
```

**When to use:** Cloud observability backends (Grafana Cloud, Datadog, Honeycomb, etc.),
fleet deployments where a central collector aggregates telemetry from multiple instances.

### Dual-Export Mode

Both Prometheus and OTLP exporters run simultaneously. Use this for gradual migration
or when you need both local scraping and central collection.

```yaml
telemetry:
  enabled: true
  prometheus:
    enabled: true
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
    interval: 15s
```

**When to use:** Migration from Prometheus-only to OTLP, or hybrid setups where local
dashboards coexist with central fleet observability.

### Configuration Reference

#### telemetry.enabled
**Type:** boolean
**Default:** `true`

Master switch for all telemetry collection. When `false`, no metrics are collected or
exported, and the `/_vibewarden/metrics` endpoint returns 404.

#### telemetry.path_patterns
**Type:** list of strings
**Default:** `[]`

URL path normalization patterns using colon-param syntax. Without patterns, all paths
are recorded as `"other"`. Configure the routes your app exposes to prevent
high-cardinality metric labels.

```yaml
telemetry:
  path_patterns:
    - "/users/:id"
    - "/api/v1/items/:item_id/comments/:comment_id"
```

#### telemetry.prometheus.enabled
**Type:** boolean
**Default:** `true`

Enables the Prometheus pull-based exporter. When enabled, metrics are served at
`/_vibewarden/metrics` in Prometheus text format with OpenMetrics compatibility.

#### telemetry.otlp.enabled
**Type:** boolean
**Default:** `false`

Enables the OTLP push-based exporter. Requires `telemetry.otlp.endpoint` to be set.

#### telemetry.otlp.endpoint
**Type:** string
**Default:** `""`

OTLP HTTP endpoint URL. Required when `telemetry.otlp.enabled` is `true`.

Examples:
- Local OTel Collector: `http://localhost:4318`
- Docker Compose: `http://otel-collector:4318`
- Grafana Cloud: `https://otlp-gateway-prod-us-central-0.grafana.net/otlp`

#### telemetry.otlp.headers
**Type:** map of string to string
**Default:** `{}`

HTTP headers to include with OTLP requests. Use for authentication.

```yaml
telemetry:
  otlp:
    headers:
      Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
      X-Custom-Header: "value"
```

#### telemetry.otlp.interval
**Type:** duration string
**Default:** `"30s"`

How often metrics are batched and pushed to the OTLP endpoint. Shorter intervals
reduce telemetry lag but increase network overhead.

Valid formats: `"15s"`, `"1m"`, `"30s"`.

#### telemetry.otlp.protocol
**Type:** string
**Default:** `"http"`

OTLP transport protocol. Only `"http"` is supported in this version. `"grpc"` is
reserved for future use.

#### telemetry.logs.otlp
**Type:** boolean
**Default:** `false`

Enables OTLP log export. When enabled, structured events (the AI-readable logs) are
exported to the same OTLP endpoint as metrics. Requires `telemetry.otlp.endpoint`
to be configured.

Logs are exported in addition to stdout JSON output — existing log collection via
stdout remains unchanged.

### Structured Event Log Export

VibeWarden's structured event logs (with `schema_version`, `event_type`, `ai_summary`,
and `payload` fields) can be exported via OTLP alongside metrics. Enable with:

```yaml
telemetry:
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
  logs:
    otlp: true
```

**How it works:**

1. Events are logged to stdout as JSON (existing behavior, always active)
2. Events are simultaneously sent to the OTel LoggerProvider
3. The LoggerProvider batches and pushes logs to the OTLP endpoint
4. OTel Collector receives logs and routes them to Loki (or any configured backend)

**OTel log record mapping:**

| Event field | OTel log record field |
|-------------|----------------------|
| `Timestamp` | `Timestamp` |
| `EventType` | Attribute: `event.type` |
| `SchemaVersion` | Attribute: `vibewarden.schema_version` |
| `AISummary` | `Body` (string) |
| `Payload.*` | Attributes: `vibewarden.payload.<key>` |

**Severity mapping:** Event types are mapped to OTel severity levels:

| Event type pattern | OTel Severity |
|-------------------|---------------|
| `*.failed`, `*.blocked`, `*.hit` | WARN |
| `*.unavailable`, `*_failed` | ERROR |
| All others | INFO |

### OTel Collector Architecture

When the observability profile is enabled (`docker compose --profile observability up`),
VibeWarden generates an OTel Collector configuration that acts as a telemetry hub:

```
VibeWarden --OTLP--> OTel Collector --metrics--> Prometheus (scrapes :8889)
                              |
                              +--logs--> Loki
```

The collector:
- Receives OTLP on port 4318 (HTTP)
- Exports metrics via Prometheus exporter on port 8889 (Prometheus scrapes this)
- Exports logs to Loki via the Loki exporter

**Collector config location:** `.vibewarden/generated/observability/otel-collector/config.yaml`

**Why a collector?**

- Decouples VibeWarden from backend details
- Enables batching, retry, and buffering
- Standard OTel pipeline that works with any OTLP-compatible backend
- Future-proof for distributed tracing

### Migrating from metrics: to telemetry:

The legacy `metrics:` config section is deprecated. VibeWarden automatically migrates
settings at startup and logs a warning.

**Before (deprecated):**

```yaml
metrics:
  enabled: true
  path_patterns:
    - "/users/:id"
```

**After (recommended):**

```yaml
telemetry:
  enabled: true
  path_patterns:
    - "/users/:id"
  prometheus:
    enabled: true
```

**Migration behavior:**

1. If `metrics:` is present but `telemetry:` is not, settings are copied automatically
2. A deprecation warning is logged at startup
3. The `/_vibewarden/metrics` endpoint works unchanged
4. Existing Prometheus scrapers and Grafana dashboards continue working

**When to migrate:** Update your config before the next major version. The `metrics:`
section will be removed in a future release.

### Example Configurations

#### Local Development (default)

No config needed. The defaults enable Prometheus-only mode:

```yaml
# Nothing required — defaults are:
# telemetry.enabled: true
# telemetry.prometheus.enabled: true
# telemetry.otlp.enabled: false
```

#### Grafana Cloud

Push metrics and logs to Grafana Cloud OTLP gateway:

```yaml
telemetry:
  enabled: true
  path_patterns:
    - "/api/v1/users/:id"
    - "/api/v1/orders/:order_id"
  prometheus:
    enabled: false  # Use OTLP instead
  otlp:
    enabled: true
    endpoint: https://otlp-gateway-prod-us-central-0.grafana.net/otlp
    headers:
      Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
    interval: 30s
  logs:
    otlp: true
```

Set `GRAFANA_OTLP_TOKEN` in your environment (base64-encoded `instanceId:apiKey`).

#### Self-Hosted OTel Collector

Push to your own OTel Collector while keeping local Prometheus scraping:

```yaml
telemetry:
  enabled: true
  path_patterns:
    - "/users/:id"
  prometheus:
    enabled: true  # Keep local /_vibewarden/metrics
  otlp:
    enabled: true
    endpoint: http://otel-collector.monitoring.svc:4318
    interval: 15s
  logs:
    otlp: true
```

#### Docker Compose Observability Stack

When using `docker compose --profile observability up`, the generated compose file
automatically sets these environment variables:

```
VIBEWARDEN_TELEMETRY_OTLP_ENABLED=true
VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT=http://otel-collector:4318
VIBEWARDEN_TELEMETRY_LOGS_OTLP=true
```

No manual config changes needed — just enable the observability profile.
```

**2. Update `vibewarden.example.yaml`**

Add new `telemetry:` section after the `metrics:` section (which will be marked deprecated):

```yaml
# Telemetry configuration
# VibeWarden uses OpenTelemetry as its telemetry foundation. This section controls
# all metrics and log export behavior.
#
# Export modes:
#   - Prometheus-only (default): Metrics scraped at /_vibewarden/metrics
#   - OTLP-only: Metrics pushed to OTLP endpoint (no local metrics endpoint)
#   - Dual-export: Both Prometheus scraping and OTLP push active
#
# See docs/observability.md for full configuration guide.
telemetry:
  # Master switch for telemetry collection (default: true)
  enabled: true

  # URL path normalization patterns using :param syntax.
  # Configure the routes your app exposes to prevent high-cardinality labels.
  # Example:
  #   path_patterns:
  #     - "/users/:id"
  #     - "/api/v1/items/:item_id/comments/:comment_id"
  path_patterns: []

  # Prometheus pull-based exporter (metrics served at /_vibewarden/metrics)
  prometheus:
    # Enable Prometheus exporter (default: true)
    # When enabled, metrics are available at /_vibewarden/metrics in Prometheus format.
    enabled: true

  # OTLP push-based exporter (metrics pushed to collector/backend)
  otlp:
    # Enable OTLP exporter (default: false)
    # Requires endpoint to be set.
    enabled: false

    # OTLP HTTP endpoint URL.
    # Examples:
    #   Local collector: http://localhost:4318
    #   Docker Compose:  http://otel-collector:4318
    #   Grafana Cloud:   https://otlp-gateway-prod-us-central-0.grafana.net/otlp
    endpoint: ""

    # HTTP headers for OTLP requests (e.g., authentication).
    # Example:
    #   headers:
    #     Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
    headers: {}

    # Export interval — how often metrics are batched and pushed (default: 30s).
    # Shorter intervals reduce telemetry lag but increase network overhead.
    interval: "30s"

    # Transport protocol: "http" (supported) or "grpc" (reserved for future).
    protocol: "http"

  # Structured event log export
  logs:
    # Enable OTLP log export (default: false).
    # When enabled, structured events (AI-readable logs) are exported to the same
    # OTLP endpoint as metrics. Logs are also written to stdout (unchanged behavior).
    # Requires telemetry.otlp.endpoint to be configured.
    otlp: false
```

**3. Add deprecation notice to existing metrics: section**

Update the `metrics:` section comment in `vibewarden.example.yaml`:

```yaml
# DEPRECATED: Use telemetry: section instead.
# This section remains for backward compatibility. Settings are automatically
# migrated to telemetry: at startup with a deprecation warning.
# This section will be removed in a future major version.
#
# Prometheus metrics
# VibeWarden exposes a Prometheus-compatible metrics endpoint at /_vibewarden/metrics.
metrics:
  # Enable metrics endpoint at /_vibewarden/metrics (recommended: true)
  enabled: true
  # Path normalization patterns (moved to telemetry.path_patterns)
  path_patterns: []
```

#### Sequence

Not applicable. This is a documentation story with no runtime changes.

#### Error Cases

Not applicable. Documentation only.

#### Test Strategy

**Manual verification:**

1. Read through updated `docs/observability.md` and verify clarity
2. Read through updated `vibewarden.example.yaml` and verify comments are accurate
3. Verify all code snippets in documentation match actual config struct fields
4. Verify example configurations are valid YAML and match TelemetryConfig schema
5. Cross-reference with ADR-012 through ADR-017 to ensure consistency

**Automated checks:**

1. YAML lint on `vibewarden.example.yaml` (existing CI)
2. Markdown lint on `docs/observability.md` (existing CI)

No new Go tests required — this is documentation only.

#### New Dependencies

None. Documentation only.

### Consequences

**Positive:**

- **Complete documentation:** Users have a single reference for all telemetry config
- **Clear migration path:** Existing users know how to migrate from `metrics:` to `telemetry:`
- **Example configs:** Practical examples for common backends reduce trial-and-error
- **Architecture clarity:** OTel Collector role is documented for users who want to understand the stack
- **Consistent with code:** Documentation reflects the actual config structs and behavior

**Negative:**

- **Documentation maintenance:** Must be updated when telemetry features change
- **Length:** The observability.md file grows significantly

**Trade-offs:**

- **Single file vs. multiple:** Chose to extend `docs/observability.md` rather than create
  a separate `docs/telemetry.md`. The telemetry config is part of the observability story,
  and splitting would fragment related content.

- **Depth vs. brevity:** Chose comprehensive coverage over minimal docs. Target users are
  vibe coders, but those who do read docs want complete information.

- **Code examples vs. full config:** Chose snippets over full files. Users can reference
  `vibewarden.example.yaml` for the complete structure.

---

## ADR-019: TracerProvider Initialization and HTTP Tracing Middleware
**Date**: 2026-03-28
**Issue**: #307
**Status**: Accepted

### Context

Epic #306 ("Distributed tracing with request correlation") establishes the need for OTel
tracing in VibeWarden. Currently, the sidecar has metrics (MeterProvider) and logs
(LoggerProvider) but no tracing. Requests pass through without generating trace context,
making it impossible to correlate logs and metrics with specific request flows.

Issue #307 is the first story in this epic. It introduces:
1. TracerProvider initialization in the OTel provider adapter
2. HTTP middleware that wraps each request in a span
3. Config toggle `telemetry.traces.enabled`

The middleware must be the outermost middleware so it captures the full request lifecycle
including auth, rate limiting, and proxy latency. The span context must be stored in the
request context for downstream use (log correlation in #308, error responses in #309).

**Constraints:**
- Must reuse the existing OTLP endpoint configuration (same as metrics/logs)
- TracerProvider lifecycle must integrate with existing Shutdown path
- Middleware must not break existing tests or integration tests
- Default `traces.enabled: false` for backward compatibility

### Decision

Extend the existing OTel adapter with TracerProvider support and add tracing middleware
as a new Caddy handler contributed by the metrics plugin. The tracing middleware is
integrated into the Caddy catch-all handler chain at the outermost position (lowest
priority number).

#### Domain Model Changes

None. Tracing is an infrastructure concern, not a domain concept. The trace_id and span_id
are observability context, not domain entities.

#### Ports (Interfaces)

**1. Extend `ports.TelemetryConfig` with traces config**

Add a new field to `ports.TelemetryConfig` in `internal/ports/otel.go`:

```go
// TelemetryConfig holds all telemetry export settings.
type TelemetryConfig struct {
    // ... existing fields ...

    // Traces configures distributed tracing settings.
    Traces TraceExportConfig
}

// TraceExportConfig configures OTel tracing.
type TraceExportConfig struct {
    // Enabled toggles tracing (default: false).
    // When enabled, a span is created for each HTTP request.
    Enabled bool
}
```

**2. Extend `ports.OTelProvider` interface**

Add a method to expose the tracer:

```go
type OTelProvider interface {
    // ... existing methods ...

    // Tracer returns an OTel Tracer for creating spans.
    // Returns nil if tracing is disabled or Init has not been called.
    Tracer() Tracer

    // TracingEnabled returns true if the tracing exporter is active.
    TracingEnabled() bool
}
```

**3. New `ports.Tracer` interface**

Create a minimal tracer interface that decouples application code from the full OTel API:

```go
// Tracer is a subset of the OTel trace.Tracer interface.
// It exposes only the span creation method VibeWarden needs.
type Tracer interface {
    // Start creates a span and a context containing the newly-created span.
    // The span must be ended by calling span.End() when the operation completes.
    Start(ctx context.Context, spanName string, opts ...SpanStartOption) (context.Context, Span)
}

// Span represents a single operation within a trace.
type Span interface {
    // End marks the span as complete. Must be called exactly once.
    End()

    // SetStatus sets the span status.
    SetStatus(code SpanStatusCode, description string)

    // SetAttributes sets attributes on the span.
    SetAttributes(attrs ...Attribute)

    // RecordError records an error as a span event.
    RecordError(err error)
}

// SpanStartOption configures span creation.
type SpanStartOption interface {
    isSpanStartOption()
}

// SpanStatusCode represents the status of a span.
type SpanStatusCode int

const (
    SpanStatusUnset SpanStatusCode = iota
    SpanStatusOK
    SpanStatusError
)

// WithSpanKind returns a SpanStartOption that sets the span kind.
func WithSpanKind(kind SpanKind) SpanStartOption

// SpanKind is the type of span.
type SpanKind int

const (
    SpanKindInternal SpanKind = iota
    SpanKindServer
    SpanKindClient
)
```

#### Adapters

**1. Extend `internal/adapters/otel/provider.go`**

Add TracerProvider initialization alongside MeterProvider:

```go
type Provider struct {
    mu            sync.RWMutex
    meterProvider *sdkmetric.MeterProvider
    tracerProvider *sdktrace.TracerProvider  // NEW
    meter         otelmetric.Meter
    tracer        trace.Tracer              // NEW
    handler       http.Handler
    registry      *prometheusclient.Registry

    promEnabled bool
    otlpEnabled bool
    traceEnabled bool                        // NEW
}
```

In `Init()`, when `cfg.Traces.Enabled` is true and OTLP is configured:
1. Create an OTLP trace exporter using `otlptracehttp.New()`
2. Create a BatchSpanProcessor with the exporter
3. Create a TracerProvider with the resource and processor
4. Set as global tracer provider via `otel.SetTracerProvider()`
5. Create the application tracer via `tracerProvider.Tracer("github.com/vibewarden/vibewarden")`

In `Shutdown()`, shut down TracerProvider before MeterProvider (traces may reference metrics).

**2. New `internal/adapters/otel/tracer.go`**

Implement `ports.Tracer` by wrapping the OTel SDK tracer:

```go
// tracerAdapter wraps an OTel trace.Tracer to implement ports.Tracer.
type tracerAdapter struct {
    t trace.Tracer
}

func (a *tracerAdapter) Start(ctx context.Context, spanName string, opts ...ports.SpanStartOption) (context.Context, ports.Span) {
    // Convert ports.SpanStartOption to trace.SpanStartOption
    var traceOpts []trace.SpanStartOption
    for _, opt := range opts {
        if k, ok := opt.(spanKindOption); ok {
            traceOpts = append(traceOpts, trace.WithSpanKind(convertSpanKind(k.kind)))
        }
    }
    ctx, span := a.t.Start(ctx, spanName, traceOpts...)
    return ctx, &spanAdapter{s: span}
}

// spanAdapter wraps an OTel trace.Span to implement ports.Span.
type spanAdapter struct {
    s trace.Span
}

func (a *spanAdapter) End() {
    a.s.End()
}

func (a *spanAdapter) SetStatus(code ports.SpanStatusCode, description string) {
    a.s.SetStatus(convertStatusCode(code), description)
}

func (a *spanAdapter) SetAttributes(attrs ...ports.Attribute) {
    otelAttrs := make([]attribute.KeyValue, len(attrs))
    for i, attr := range attrs {
        otelAttrs[i] = attribute.String(attr.Key, attr.Value)
    }
    a.s.SetAttributes(otelAttrs...)
}

func (a *spanAdapter) RecordError(err error) {
    a.s.RecordError(err)
}
```

**3. New `internal/middleware/tracing.go`**

HTTP middleware that creates a span for each request:

```go
// TracingMiddleware returns HTTP middleware that creates an OTel span for each request.
// It must be the outermost middleware (first in, last out) to capture the full
// request lifecycle including auth, rate limiting, and proxy latency.
//
// The middleware sets standard HTTP span attributes:
//   - http.request.method
//   - url.path
//   - http.response.status_code
//   - http.route (normalized path pattern)
//
// The span context is stored in the request context for downstream use
// (log correlation, error responses).
//
// Requests to /_vibewarden/* paths are NOT traced to avoid self-referential noise.
func TracingMiddleware(
    tracer ports.Tracer,
    normalizePathFn func(string) string,
) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip tracing for internal endpoints.
            if strings.HasPrefix(r.URL.Path, "/_vibewarden/") {
                next.ServeHTTP(w, r)
                return
            }

            // Create span with server kind.
            ctx, span := tracer.Start(r.Context(), "HTTP "+r.Method,
                ports.WithSpanKind(ports.SpanKindServer))
            defer span.End()

            // Set initial attributes.
            route := normalizePathFn(r.URL.Path)
            span.SetAttributes(
                ports.Attribute{Key: "http.request.method", Value: r.Method},
                ports.Attribute{Key: "url.path", Value: r.URL.Path},
                ports.Attribute{Key: "http.route", Value: route},
            )

            // Wrap response writer to capture status code.
            rw := &tracingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

            // Serve with span context.
            next.ServeHTTP(rw, r.WithContext(ctx))

            // Set final attributes.
            span.SetAttributes(
                ports.Attribute{Key: "http.response.status_code", Value: strconv.Itoa(rw.statusCode)},
            )

            // Set span status based on HTTP status.
            if rw.statusCode >= 500 {
                span.SetStatus(ports.SpanStatusError, http.StatusText(rw.statusCode))
            } else {
                span.SetStatus(ports.SpanStatusOK, "")
            }
        })
    }
}

// tracingResponseWriter wraps http.ResponseWriter to capture the status code.
type tracingResponseWriter struct {
    http.ResponseWriter
    statusCode int
    written    bool
}

func (rw *tracingResponseWriter) WriteHeader(code int) {
    if !rw.written {
        rw.statusCode = code
        rw.written = true
    }
    rw.ResponseWriter.WriteHeader(code)
}

func (rw *tracingResponseWriter) Write(b []byte) (int, error) {
    if !rw.written {
        rw.statusCode = http.StatusOK
        rw.written = true
    }
    return rw.ResponseWriter.Write(b)
}
```

#### Application Service

No application service changes. Tracing is infrastructure-level, handled by middleware and adapters.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/adapters/otel/tracer.go` | `tracerAdapter` and `spanAdapter` implementations |
| `internal/adapters/otel/tracer_test.go` | Unit tests for tracer adapter |
| `internal/middleware/tracing.go` | `TracingMiddleware` implementation |
| `internal/middleware/tracing_test.go` | Unit tests for tracing middleware |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/ports/otel.go` | Add `TraceExportConfig`, `Tracer`, `Span` interfaces |
| `internal/adapters/otel/provider.go` | Add TracerProvider initialization and shutdown |
| `internal/adapters/otel/provider_test.go` | Add tests for TracerProvider lifecycle |
| `internal/config/config.go` | Add `TracesConfig` struct to `TelemetryConfig` |
| `internal/plugins/metrics/config.go` | Add `TracesEnabled` field |
| `internal/plugins/metrics/plugin.go` | Initialize tracer, contribute tracing handler to Caddy |

#### Sequence

**Request flow with tracing enabled:**

1. HTTP request arrives at Caddy
2. Caddy handler chain starts; tracing handler is first (priority 5)
3. Tracing handler creates span via `tracer.Start()`
4. Span context stored in request context
5. Next handlers execute (security headers, auth, rate limit, proxy)
6. Response written by reverse proxy
7. Tracing handler captures status code from wrapped ResponseWriter
8. Tracing handler sets final attributes (status_code) and span status
9. Tracing handler calls `span.End()` (deferred)
10. Span is batched by BatchSpanProcessor
11. Batch is exported via OTLP HTTP to collector on configured interval

**TracerProvider initialization:**

1. Metrics plugin `Init()` calls `provider.Init()` with config
2. If `cfg.Traces.Enabled && cfg.OTLP.Enabled`:
   - Create OTLP HTTP trace exporter with same endpoint
   - Create BatchSpanProcessor with exporter
   - Create TracerProvider with resource and processor
   - Set global tracer provider
   - Create application tracer
3. Provider stores tracer for later retrieval

**TracerProvider shutdown:**

1. Metrics plugin `Stop()` calls `provider.Shutdown()`
2. TracerProvider is shut down first (flushes pending spans)
3. MeterProvider is shut down second

#### Error Cases

| Error | Handling |
|-------|----------|
| Traces enabled but OTLP disabled | Return error from `provider.Init()`: "traces require OTLP exporter to be enabled" |
| OTLP exporter creation fails | Return error from `provider.Init()` with wrapped exporter error |
| TracerProvider shutdown fails | Log error, continue shutdown (best effort) |
| Span creation panics | Should not happen with valid tracer; if it does, recover in middleware and serve without tracing |

#### Test Strategy

**Unit tests:**

| Test | Location | What it verifies |
|------|----------|------------------|
| `TestTracerAdapter_Start` | `internal/adapters/otel/tracer_test.go` | Span creation, context propagation |
| `TestSpanAdapter_SetAttributes` | `internal/adapters/otel/tracer_test.go` | Attribute setting |
| `TestSpanAdapter_SetStatus` | `internal/adapters/otel/tracer_test.go` | Status code mapping |
| `TestTracingMiddleware_CreatesSpan` | `internal/middleware/tracing_test.go` | Span is created for each request |
| `TestTracingMiddleware_SetsAttributes` | `internal/middleware/tracing_test.go` | HTTP attributes are set correctly |
| `TestTracingMiddleware_SkipsInternalPaths` | `internal/middleware/tracing_test.go` | `/_vibewarden/*` paths are not traced |
| `TestTracingMiddleware_CapturesStatusCode` | `internal/middleware/tracing_test.go` | Status code is captured from response |
| `TestTracingMiddleware_SetsErrorStatus` | `internal/middleware/tracing_test.go` | 5xx responses set error status |

**Integration tests:**

| Test | Location | What it verifies |
|------|----------|------------------|
| `TestProvider_TracerProvider_Init` | `internal/adapters/otel/provider_test.go` | TracerProvider initializes when traces enabled |
| `TestProvider_TracerProvider_RequiresOTLP` | `internal/adapters/otel/provider_test.go` | Error when traces enabled without OTLP |
| `TestProvider_Shutdown_TracerProvider` | `internal/adapters/otel/provider_test.go` | TracerProvider shuts down gracefully |

**Mock tracer for tests:**

Create a mock tracer in `internal/adapters/otel/testing.go` for use in middleware tests:

```go
// MockTracer implements ports.Tracer for testing.
type MockTracer struct {
    StartCalls []struct {
        Name string
        Opts []ports.SpanStartOption
    }
    SpanToReturn *MockSpan
}

// MockSpan implements ports.Span for testing.
type MockSpan struct {
    Ended      bool
    StatusCode ports.SpanStatusCode
    StatusDesc string
    Attrs      []ports.Attribute
    Errors     []error
}
```

#### New Dependencies

**None.** The required OTel tracing packages are already in `go.mod` as indirect dependencies:

| Package | Version | License | Status |
|---------|---------|---------|--------|
| `go.opentelemetry.io/otel/sdk/trace` | v1.42.0 | Apache 2.0 | Already indirect |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | v1.41.0 | Apache 2.0 | Already indirect |

The implementation will promote these to direct dependencies. No new licenses are introduced.

#### Config Schema

**Add to `config.TelemetryConfig`:**

```go
// TracesConfig holds tracing settings.
type TracesConfig struct {
    // Enabled toggles distributed tracing (default: false).
    // When enabled, a span is created for each HTTP request and exported via OTLP.
    // Requires telemetry.otlp.enabled to be true.
    Enabled bool `mapstructure:"enabled"`
}
```

**YAML example:**

```yaml
telemetry:
  enabled: true
  otlp:
    enabled: true
    endpoint: http://otel-collector:4318
  traces:
    enabled: true
```

### Consequences

**Positive:**

- Each HTTP request gets a trace context (trace_id, span_id)
- Span context is available in request context for downstream features (#308, #309)
- Traces are exported via OTLP to the same collector as metrics and logs
- TracerProvider shutdown flushes pending spans before exit
- Middleware pattern is consistent with existing metrics middleware
- Default false preserves backward compatibility

**Negative:**

- Small overhead per request (span creation, context propagation)
- Traces require OTLP to be enabled (no standalone traces-only mode)
- Additional complexity in provider lifecycle management

**Trade-offs:**

- **Trace all requests vs. sampling:** Chose to trace all requests (100% sampling) in v1
  for simplicity. Sampling can be added later via config. For the target vibe coder,
  having complete traces is more valuable than optimizing overhead.

- **Middleware priority:** Chose priority 5 (lower than security headers at 10) so tracing
  captures the full lifecycle. This means the span includes security header injection time,
  which is intentional — we want to measure end-to-end latency.

- **Skip internal paths:** Chose to skip `/_vibewarden/*` paths to avoid self-referential
  noise. This is consistent with the metrics middleware behavior.

- **No span propagation yet:** This ADR does not implement trace context propagation to
  upstream apps (that is #311). The span created here is a root span. Propagation will
  be added in a later story.

---

## ADR-020: Inject trace_id and span_id into slog context
**Date**: 2026-03-28
**Issue**: #308
**Status**: Accepted

### Context

Epic #306 ("Distributed tracing with request correlation") requires that log lines can be
correlated with traces. ADR-019 introduced the TracerProvider and HTTP tracing middleware,
which creates a span for each request and stores the span context in the request context.

Currently, when `SlogEventLogger.Log()` is called with the request context, it does not
extract or include the trace_id and span_id. This means log lines have no request correlation.
When multiple requests are processed concurrently, it is impossible to tell which log lines
belong to which request.

Issue #308 addresses this by extracting trace_id and span_id from the span context and
injecting them as slog attributes. Every log line emitted during request processing will
automatically include these fields.

**Constraints:**
- Must not add trace fields when tracing is disabled (no empty strings)
- Must work with both stdout JSON handler and OTel log bridge
- Must not introduce new dependencies beyond what OTel SDK already provides
- Must not break existing log format (additive change only)

### Decision

Modify `SlogEventLogger.Log()` to extract trace context from the request context and add
`trace_id` and `span_id` as slog attributes when a valid span context is present.

#### Domain Model Changes

None. Trace IDs are observability infrastructure, not domain concepts.

#### Ports (Interfaces)

No port changes required. The `ports.EventLogger` interface remains unchanged.
The context passed to `Log(ctx, event)` already carries the span context after
the tracing middleware runs.

#### Adapters

**Modify `internal/adapters/log/slog_adapter.go`**

Update the `Log()` method to extract trace context and add trace attributes:

```go
import (
    // ... existing imports ...
    "go.opentelemetry.io/otel/trace"
)

// Log writes the event as a single JSON line to the configured writer.
// When the context contains a valid OTel span context (from TracingMiddleware),
// trace_id and span_id are added as top-level fields for request correlation.
func (l *SlogEventLogger) Log(ctx context.Context, event events.Event) error {
    // Serialize the payload map to a json.RawMessage (existing code)
    payload := event.Payload
    if payload == nil {
        payload = map[string]any{}
    }
    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshalling event payload: %w", err)
    }

    // Build the list of attributes.
    attrs := []slog.Attr{
        slog.String("schema_version", event.SchemaVersion),
        slog.String("event_type", event.EventType),
        slog.Time("timestamp", event.Timestamp),
        slog.String("ai_summary", event.AISummary),
        slog.Any("payload", json.RawMessage(payloadBytes)),
    }

    // Extract trace context if present and valid.
    if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
        attrs = append(attrs,
            slog.String("trace_id", sc.TraceID().String()),
            slog.String("span_id", sc.SpanID().String()),
        )
    }

    l.logger.LogAttrs(ctx, slog.LevelInfo, "", attrs...)

    return nil
}
```

Key implementation details:

1. **Use `trace.SpanContextFromContext(ctx)`** — This is more efficient than
   `trace.SpanFromContext(ctx).SpanContext()` because it avoids creating a no-op
   span when the context has no span.

2. **Check `sc.IsValid()`** — Returns false when:
   - Tracing is disabled (no span in context)
   - The span context has invalid/zero trace ID or span ID
   This ensures we never emit empty string fields.

3. **Append to attrs slice** — Trace fields appear after payload in the JSON output.
   Order is: schema_version, event_type, timestamp, ai_summary, payload, trace_id, span_id.

4. **String representation** — `TraceID().String()` returns a 32-character hex string.
   `SpanID().String()` returns a 16-character hex string. These match the W3C Trace
   Context format that OTel collectors expect.

#### Application Service

No application service changes. The trace context flows through automatically via
the request context.

#### File Layout

**Modified files:**

| File | Change |
|------|--------|
| `internal/adapters/log/slog_adapter.go` | Add trace context extraction in `Log()` |
| `internal/adapters/log/slog_adapter_test.go` | Add tests for trace context injection |

**No new files required.** This is a minimal, focused change to the existing adapter.

#### Sequence

Request flow with trace context injection:

1. Request arrives at VibeWarden
2. TracingMiddleware creates span, stores in context via `r.WithContext(ctx)`
3. Request flows through auth, rate limiting, other middleware
4. Middleware or handler calls `eventLogger.Log(r.Context(), ev)`
5. `SlogEventLogger.Log()` extracts span context: `trace.SpanContextFromContext(ctx)`
6. If `sc.IsValid()`, appends trace_id and span_id to slog attrs
7. `logger.LogAttrs()` writes JSON with trace fields
8. Log line includes trace_id and span_id for correlation

For non-traced requests (tracing disabled or internal paths):

1. Request arrives at VibeWarden
2. TracingMiddleware skips span creation (or is not in middleware chain)
3. Request flows through middleware
4. Middleware calls `eventLogger.Log(r.Context(), ev)`
5. `SlogEventLogger.Log()` extracts span context: returns invalid SpanContext
6. `sc.IsValid()` returns false, no trace fields added
7. Log line has no trace_id or span_id fields (not empty strings, completely absent)

#### Error Cases

| Scenario | Handling |
|----------|----------|
| Context is nil | `SpanContextFromContext(nil)` returns invalid SpanContext; no trace fields added |
| Context has no span | `SpanContextFromContext` returns invalid SpanContext; no trace fields added |
| Span context is invalid | `sc.IsValid()` returns false; no trace fields added |
| TraceID or SpanID is zero | `sc.IsValid()` returns false; no trace fields added |

There are no error cases that require special handling. The design gracefully degrades
to no trace fields when tracing is not active.

#### Test Strategy

**Unit tests in `internal/adapters/log/slog_adapter_test.go`:**

| Test | What it verifies |
|------|------------------|
| `TestSlogEventLogger_Log_WithTraceContext` | When context has valid span, trace_id and span_id appear in JSON |
| `TestSlogEventLogger_Log_WithoutTraceContext` | When context has no span, trace_id and span_id are absent (not empty strings) |
| `TestSlogEventLogger_Log_WithInvalidSpanContext` | When span context is invalid, trace_id and span_id are absent |

Test implementation approach:

```go
func TestSlogEventLogger_Log_WithTraceContext(t *testing.T) {
    // Create a real span using OTel SDK in-memory exporter.
    // This ensures we test with actual OTel span context, not mocks.
    tp := sdktrace.NewTracerProvider()
    tracer := tp.Tracer("test")
    ctx, span := tracer.Start(context.Background(), "test-span")
    defer span.End()

    var buf bytes.Buffer
    logger := log.NewSlogEventLogger(&buf)

    ev := events.Event{
        SchemaVersion: "v1",
        EventType:     "test.event",
        Timestamp:     time.Now(),
        AISummary:     "Test event",
        Payload:       map[string]any{},
    }
    _ = logger.Log(ctx, ev)

    var out map[string]any
    _ = json.Unmarshal(buf.Bytes(), &out)

    // Verify trace_id is present and valid (32 hex chars).
    traceID, ok := out["trace_id"].(string)
    if !ok || len(traceID) != 32 {
        t.Errorf("trace_id = %q, want 32-char hex string", traceID)
    }

    // Verify span_id is present and valid (16 hex chars).
    spanID, ok := out["span_id"].(string)
    if !ok || len(spanID) != 16 {
        t.Errorf("span_id = %q, want 16-char hex string", spanID)
    }
}

func TestSlogEventLogger_Log_WithoutTraceContext(t *testing.T) {
    var buf bytes.Buffer
    logger := log.NewSlogEventLogger(&buf)

    ev := events.Event{
        SchemaVersion: "v1",
        EventType:     "test.event",
        Timestamp:     time.Now(),
        AISummary:     "Test event",
        Payload:       map[string]any{},
    }
    _ = logger.Log(context.Background(), ev)

    var out map[string]any
    _ = json.Unmarshal(buf.Bytes(), &out)

    // Verify trace_id and span_id are completely absent, not empty strings.
    if _, ok := out["trace_id"]; ok {
        t.Error("trace_id should be absent when no span context")
    }
    if _, ok := out["span_id"]; ok {
        t.Error("span_id should be absent when no span context")
    }
}
```

**No integration tests needed.** The trace context injection is purely in-memory
manipulation of slog attributes. The existing integration tests for the tracing
middleware (ADR-019) already verify that span context is stored in the request context.

#### New Dependencies

**None.** The `go.opentelemetry.io/otel/trace` package is already a direct dependency
(used by the tracer adapter in ADR-019). No new imports are introduced.

Existing dependency:
| Package | Version | License | Status |
|---------|---------|---------|--------|
| `go.opentelemetry.io/otel/trace` | v1.42.0 | Apache 2.0 | Already direct |

### Consequences

**Positive:**

- Every log line during a traced request includes trace_id and span_id
- Log aggregators (Grafana Loki, etc.) can correlate logs with traces
- No trace fields when tracing is disabled (clean output)
- Works with both stdout JSON and OTel log bridge
- Zero new dependencies
- Minimal code change (~10 lines)

**Negative:**

- Slight increase in log line size (48 bytes for trace fields)
- Import of `go.opentelemetry.io/otel/trace` in slog adapter (couples adapter to OTel)

**Trade-offs:**

- **Coupling slog adapter to OTel vs. using a port:** Chose direct OTel import
  because creating a port for trace context extraction would be over-engineering.
  The slog adapter is already an infrastructure adapter, and the OTel trace package
  is a stable, minimal API. If we ever need a non-OTel tracing library, we would
  need a new adapter anyway.

- **Always checking span context vs. configurable:** Chose to always check span
  context because `SpanContextFromContext` is cheap (single map lookup) and
  the conditional add is simpler than config-based logic. No performance concern.

- **Field names `trace_id`/`span_id` vs. OTel convention `traceID`/`spanID`:**
  Chose snake_case to match the existing VibeWarden log schema (schema_version,
  event_type, ai_summary). Consistency within our schema trumps OTel naming.

---

## ADR-021: Include trace_id in JSON Error Responses
**Date**: 2026-03-28
**Issue**: #309
**Status**: Accepted

### Context

Epic #306 ("Distributed tracing with request correlation") requires that users can correlate
error responses with sidecar logs. Currently, when a user encounters a 429, 403, or 500 error,
they receive a JSON response like:

```json
{"error": "rate_limit_exceeded", "retry_after_seconds": 5}
```

There is no way to correlate this response with the corresponding log entry in the sidecar.
Support tickets often say "I got a 429" with no way to find the exact request in logs.

ADR-019 introduced TracingMiddleware which creates a span for each request and stores the
span context in the request context. ADR-020 injected trace_id and span_id into log lines.
This story completes the correlation loop by including the trace_id (or a fallback request_id)
in the JSON error response body.

**Error response locations in the codebase:**

1. **Rate limiter middleware** (`internal/middleware/ratelimit.go`):
   - 429 Too Many Requests with JSON body `{"error":"rate_limit_exceeded","retry_after_seconds":N}`
   - 403 Forbidden (unidentified client) with plain text

2. **Auth middleware** (`internal/middleware/auth.go`):
   - 503 Service Unavailable (Kratos unavailable) with plain text
   - Redirects (302) do not need trace_id

3. **IP filter Caddy handler** (`internal/adapters/caddy/ipfilter_handler.go`):
   - 403 Forbidden with plain text

4. **Body size Caddy handler** (`internal/adapters/caddy/bodysize_handler.go`):
   - 413 Payload Too Large via `http.MaxBytesReader` (handled by net/http, not our code)

5. **Admin auth middleware** (`internal/middleware/admin_auth.go`):
   - 401 Unauthorized with plain text

**Constraints:**

- Must include `trace_id` when tracing is enabled (span context is valid)
- Must include `request_id` as fallback when tracing is disabled
- The ID in the response must match the ID in the corresponding log line
- Must not break existing API contracts (additive field only)
- Plain text responses should become JSON responses for consistency

### Decision

Create a shared error response helper in `internal/middleware/error_response.go` that:

1. Extracts trace_id from the span context when available
2. Generates a lightweight request_id when tracing is disabled
3. Writes a consistent JSON error response with the correlation ID
4. Stores the correlation ID in the request context for logging

All middleware and handlers that return error responses will use this helper.

#### Domain Model Changes

None. Request IDs and trace IDs are observability infrastructure, not domain concepts.

#### Ports (Interfaces)

No new ports required. The helper is a pure function that operates on `context.Context`
and `http.ResponseWriter`. It does not need abstraction since it is tightly coupled to
HTTP error handling.

#### Adapters

**New file: `internal/middleware/error_response.go`**

Provides a centralized helper for writing JSON error responses with correlation IDs:

```go
// ErrorResponse is the JSON structure for all error responses from VibeWarden middleware.
// It always includes a correlation ID for log matching.
type ErrorResponse struct {
    // Error is the machine-readable error code (e.g., "rate_limit_exceeded", "forbidden").
    Error string `json:"error"`

    // Status is the HTTP status code.
    Status int `json:"status"`

    // Message is a human-readable error description (optional).
    Message string `json:"message,omitempty"`

    // TraceID is the OTel trace ID when tracing is enabled.
    // Mutually exclusive with RequestID.
    TraceID string `json:"trace_id,omitempty"`

    // RequestID is a generated ID when tracing is disabled.
    // Mutually exclusive with TraceID.
    RequestID string `json:"request_id,omitempty"`

    // RetryAfterSeconds is set only for 429 responses.
    RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
}

// WriteErrorResponse writes a JSON error response with a correlation ID.
// When the context contains a valid OTel span context, trace_id is used.
// Otherwise, a request_id is generated.
//
// The correlation ID is also stored in the request context under the
// correlationIDKey for use by event logging.
func WriteErrorResponse(w http.ResponseWriter, r *http.Request, status int, errorCode, message string) {
    // Extract trace_id or generate request_id
    var traceID, requestID string
    if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
        traceID = sc.TraceID().String()
    } else {
        requestID = generateRequestID()
    }

    resp := ErrorResponse{
        Error:     errorCode,
        Status:    status,
        Message:   message,
        TraceID:   traceID,
        RequestID: requestID,
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(resp)
}

// WriteRateLimitResponse writes a 429 response with retry information.
// It sets the Retry-After header and includes retry_after_seconds in the body.
func WriteRateLimitResponse(w http.ResponseWriter, r *http.Request, retryAfterSeconds int) {
    var traceID, requestID string
    if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
        traceID = sc.TraceID().String()
    } else {
        requestID = generateRequestID()
    }

    resp := ErrorResponse{
        Error:             "rate_limit_exceeded",
        Status:            http.StatusTooManyRequests,
        TraceID:           traceID,
        RequestID:         requestID,
        RetryAfterSeconds: retryAfterSeconds,
    }

    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
    w.WriteHeader(http.StatusTooManyRequests)
    _ = json.NewEncoder(w).Encode(resp)
}

// generateRequestID creates a short, URL-safe request ID.
// Format: "req_" + 12 random base32 characters (e.g., "req_A3BKDMF7HQLN").
// Uses crypto/rand for unpredictability.
func generateRequestID() string {
    b := make([]byte, 8) // 8 bytes = 64 bits of randomness
    _, _ = rand.Read(b)  // crypto/rand never fails for 8 bytes
    return "req_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)[:12]
}

// CorrelationID extracts the trace_id or request_id from the context.
// Returns the trace_id if a valid span context exists, otherwise returns
// any previously stored request_id, or generates a new one.
// This is used by event loggers to ensure logs match error responses.
func CorrelationID(ctx context.Context) string {
    if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
        return sc.TraceID().String()
    }
    if id := requestIDFromContext(ctx); id != "" {
        return id
    }
    return generateRequestID()
}
```

**Request ID context storage:**

When a request_id is generated, it must be stored in the context so that subsequent
log calls include the same ID. Add context key and helpers:

```go
type contextKey int

const requestIDContextKey contextKey = iota

// ContextWithRequestID returns a new context with the request ID stored.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
    return context.WithValue(ctx, requestIDContextKey, id)
}

// requestIDFromContext retrieves a previously stored request ID.
func requestIDFromContext(ctx context.Context) string {
    if id, ok := ctx.Value(requestIDContextKey).(string); ok {
        return id
    }
    return ""
}
```

**Modify: `internal/middleware/ratelimit.go`**

Replace `writeRateLimitResponse()` with the new helper:

```go
// Before:
func writeRateLimitResponse(w http.ResponseWriter, result ports.RateLimitResult) {
    retrySeconds := retryAfterSeconds(result.RetryAfter)
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
    w.WriteHeader(http.StatusTooManyRequests)
    body := rateLimitErrorBody{
        Error:             "rate_limit_exceeded",
        RetryAfterSeconds: retrySeconds,
    }
    _ = json.NewEncoder(w).Encode(body)
}

// After:
// Remove writeRateLimitResponse, use WriteRateLimitResponse from error_response.go
```

Update the middleware to pass the request to the error helper:

```go
if !ipResult.Allowed {
    emitRateLimitHit(r, eventLogger, "ip", clientIP, "", ipResult)
    WriteRateLimitResponse(w, r, retryAfterSeconds(ipResult.RetryAfter))
    return
}
```

For the 403 "unidentified client" case, convert to JSON:

```go
// Before:
http.Error(w, "Forbidden", http.StatusForbidden)

// After:
WriteErrorResponse(w, r, http.StatusForbidden, "unidentified_client",
    "Could not identify client IP address")
```

**Modify: `internal/middleware/auth.go`**

Convert 503 responses to JSON:

```go
// Before:
http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)

// After:
WriteErrorResponse(w, r, http.StatusServiceUnavailable, "auth_provider_unavailable",
    "Authentication service is temporarily unavailable")
```

**Modify: `internal/adapters/caddy/ipfilter_handler.go`**

Convert 403 responses to JSON. The handler needs access to the request context
for trace extraction:

```go
// Before:
http.Error(w, "Forbidden", http.StatusForbidden)

// After:
middleware.WriteErrorResponse(w, r, http.StatusForbidden, "ip_blocked",
    "Access denied by IP filter")
```

**Modify: `internal/middleware/admin_auth.go`**

Convert 401 responses to JSON:

```go
// Before:
http.Error(w, "Unauthorized", http.StatusUnauthorized)

// After:
WriteErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
    "Admin authentication required")
```

**Note on body size handler:**

The `http.MaxBytesReader` error is handled by net/http, not our code. When the
body exceeds the limit, the reader returns an error and net/http writes a 413
response. We cannot intercept this easily without wrapping the response writer,
which adds complexity. The body size handler is therefore **out of scope** for
this story. A future story could wrap the response writer to intercept 413 errors.

#### Application Service

No application service changes.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/middleware/error_response.go` | Shared error response helper with trace/request ID |
| `internal/middleware/error_response_test.go` | Unit tests for error response helper |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/middleware/ratelimit.go` | Use `WriteRateLimitResponse`, convert 403 to JSON |
| `internal/middleware/ratelimit_test.go` | Update tests for new response format |
| `internal/middleware/auth.go` | Use `WriteErrorResponse` for 503 |
| `internal/middleware/auth_test.go` | Update tests for new response format |
| `internal/middleware/admin_auth.go` | Use `WriteErrorResponse` for 401 |
| `internal/middleware/admin_auth_test.go` | Update tests for new response format |
| `internal/adapters/caddy/ipfilter_handler.go` | Use `WriteErrorResponse` for 403 |
| `internal/adapters/caddy/ipfilter_handler_test.go` | Update tests for new response format |

#### Sequence

**Error response with tracing enabled:**

1. Request arrives at VibeWarden
2. TracingMiddleware creates span, stores span context in request context
3. Rate limiter (or other middleware) determines request should be rejected
4. Middleware calls `WriteRateLimitResponse(w, r, retrySeconds)`
5. Helper extracts trace_id via `trace.SpanContextFromContext(r.Context())`
6. Helper writes JSON: `{"error":"rate_limit_exceeded","status":429,"trace_id":"abc123...","retry_after_seconds":5}`
7. Event logger also extracts trace_id from same context
8. Log line includes matching trace_id for correlation

**Error response with tracing disabled:**

1. Request arrives at VibeWarden
2. TracingMiddleware skips span creation (or is disabled)
3. Rate limiter determines request should be rejected
4. Middleware calls `WriteRateLimitResponse(w, r, retrySeconds)`
5. Helper finds no valid span context
6. Helper generates request_id: "req_A3BKDMF7HQLN"
7. Helper writes JSON: `{"error":"rate_limit_exceeded","status":429,"request_id":"req_A3BKDMF7HQLN","retry_after_seconds":5}`
8. (Future enhancement: event logger includes same request_id)

#### Error Cases

| Scenario | Handling |
|----------|----------|
| Context is nil | `SpanContextFromContext(nil)` returns invalid context; generate request_id |
| JSON encoding fails | Encoding a struct with string fields never fails; silently ignore |
| crypto/rand fails | For 8 bytes, crypto/rand never fails on any OS |
| Response already written | Standard Go behavior; second WriteHeader is ignored |

#### Test Strategy

**Unit tests in `internal/middleware/error_response_test.go`:**

| Test | What it verifies |
|------|------------------|
| `TestWriteErrorResponse_WithTraceContext` | trace_id in response when span context valid |
| `TestWriteErrorResponse_WithoutTraceContext` | request_id in response when no span context |
| `TestWriteRateLimitResponse_IncludesRetryAfter` | retry_after_seconds and Retry-After header |
| `TestGenerateRequestID_Format` | Format is "req_" + 12 chars, unique per call |
| `TestCorrelationID_WithTrace` | Returns trace_id when span context valid |
| `TestCorrelationID_WithRequestID` | Returns stored request_id when present |
| `TestCorrelationID_GeneratesNew` | Generates new ID when nothing in context |

**Updated tests in existing files:**

| File | Test changes |
|------|--------------|
| `internal/middleware/ratelimit_test.go` | Verify JSON response includes trace_id or request_id |
| `internal/middleware/auth_test.go` | Verify 503 is JSON with trace_id or request_id |
| `internal/middleware/admin_auth_test.go` | Verify 401 is JSON with trace_id or request_id |
| `internal/adapters/caddy/ipfilter_handler_test.go` | Verify 403 is JSON with trace_id or request_id |

**Test helper for creating span context:**

Reuse the pattern from ADR-020 tests:

```go
func withTraceContext(ctx context.Context) context.Context {
    tp := sdktrace.NewTracerProvider()
    tracer := tp.Tracer("test")
    ctx, _ = tracer.Start(ctx, "test-span")
    return ctx
}
```

**What to mock vs. real:**

- Real: OTel SDK for span context (cheap, no external calls)
- Real: crypto/rand for request ID generation (deterministic enough for tests)
- Mock: Nothing needed

#### New Dependencies

**None.** All required packages are already in use:

| Package | Status | License |
|---------|--------|---------|
| `go.opentelemetry.io/otel/trace` | Already direct (ADR-019/020) | Apache 2.0 |
| `crypto/rand` | stdlib | N/A |
| `encoding/base32` | stdlib | N/A |

### Consequences

**Positive:**

- Every error response includes a correlation ID (trace_id or request_id)
- Users can include the ID in support tickets for fast log lookup
- IDs match between response and log lines (same extraction logic)
- Consistent JSON format for all error responses
- No new dependencies

**Negative:**

- Breaking change to response format (plain text -> JSON) for some errors
- Body size 413 errors are not covered (net/http limitation)
- Slightly larger response bodies (~50 bytes for ID)

**Trade-offs:**

- **JSON vs. plain text for all errors:** Chose JSON for consistency and machine
  readability. The target vibe coder audience is building APIs, so JSON errors
  are expected. Frontends parsing "Forbidden" text strings is fragile anyway.

- **trace_id vs. request_id field naming:** Chose separate fields (`trace_id`
  when tracing enabled, `request_id` when disabled) rather than a generic
  `correlation_id`. This makes it explicit which ID type is being used and
  matches the log field names from ADR-020.

- **Request ID format:** Chose "req_" prefix + base32 for:
  - Human recognizable as a request ID
  - URL-safe (no special characters)
  - Short (16 chars total) but sufficient entropy (64 bits)
  - Different format from trace_id (32 hex chars) to avoid confusion

- **Body size 413 not covered:** Chose to exclude rather than wrap ResponseWriter.
  The added complexity of intercepting net/http errors is not worth it for v1.
  Body size errors are rare (misconfigured clients) and the rate limiter/auth
  paths are the common support ticket cases.

---

## ADR-022: Identity Provider Port Abstraction
**Date**: 2026-03-28
**Issue**: #385
**Status**: Accepted

### Context

VibeWarden currently supports Ory Kratos as the sole identity provider for session-based
authentication. The `SessionChecker` interface in `internal/ports/auth.go` is tightly
coupled to Kratos concepts (session cookies, Kratos-specific Identity/Session types).

Epic #373 ("Flexible Auth") requires support for multiple identity providers:
- Ory Kratos (existing, session-based)
- JWT/OIDC validation (stateless tokens from external IdPs like Auth0, Okta, Keycloak)
- API keys (already implemented separately in ADR for API key auth)

The current `SessionChecker` interface cannot support JWT/OIDC because:
1. JWT validation uses `Authorization: Bearer` headers, not session cookies
2. JWT claims have a different structure than Kratos sessions
3. The `Identity` and `Session` types in ports are Kratos-specific
4. The auth middleware directly references Kratos concepts

**Acceptance criteria from issue #385:**
- Create a generic `IdentityProvider` port that abstracts session/token validation
- The port returns a provider-agnostic `Identity` value object
- Existing Kratos adapter implements the new port
- Auth middleware uses the new port instead of `SessionChecker`
- Backward compatibility: no changes to existing configuration or behavior

### Decision

Introduce a new `IdentityProvider` port interface that abstracts the authentication
mechanism (session cookie vs. Bearer token vs. API key). The existing `SessionChecker`
interface is preserved for backward compatibility but marked as deprecated.

#### Domain Model Changes

**New value object: `internal/domain/identity/identity.go`**

The `Identity` value object represents a verified user identity independent of the
authentication mechanism. It is immutable and has no behavior beyond validation.

```go
// Package identity provides domain types for authenticated user identity.
// This package has zero external dependencies — only the Go standard library.
package identity

import (
    "errors"
    "strings"
)

// Identity is a value object representing an authenticated user's identity.
// It is provider-agnostic: the same Identity type is returned whether the user
// authenticated via Kratos session, JWT, API key, or any future mechanism.
//
// Identity is immutable. Create new instances via NewIdentity.
type Identity struct {
    // id is the unique identifier for the user. Format depends on the provider:
    // - Kratos: UUID string
    // - OIDC: "sub" claim value
    // - API key: key name or hash
    id string

    // email is the user's primary email address. May be empty for non-human
    // identities (e.g., service accounts, API keys).
    email string

    // emailVerified indicates whether the email has been verified by the provider.
    emailVerified bool

    // provider identifies which identity provider authenticated this user.
    // Examples: "kratos", "oidc", "jwt", "apikey".
    provider string

    // claims contains additional attributes from the provider. Keys are
    // claim names, values are claim values (typically string, []string, or bool).
    // For Kratos: traits
    // For JWT/OIDC: all claims except reserved ones (sub, iss, aud, exp, iat, nbf)
    // For API keys: scopes as {"scopes": []string{...}}
    claims map[string]any
}

// NewIdentity creates a new Identity with the given attributes.
// Returns an error if required fields are invalid.
func NewIdentity(id, email, provider string, emailVerified bool, claims map[string]any) (Identity, error) {
    if id == "" {
        return Identity{}, errors.New("identity id cannot be empty")
    }
    if provider == "" {
        return Identity{}, errors.New("identity provider cannot be empty")
    }
    // Email validation: if provided, must contain @
    if email != "" && !strings.Contains(email, "@") {
        return Identity{}, errors.New("invalid email format")
    }

    // Defensive copy of claims to ensure immutability
    claimsCopy := make(map[string]any, len(claims))
    for k, v := range claims {
        claimsCopy[k] = v
    }

    return Identity{
        id:            id,
        email:         email,
        emailVerified: emailVerified,
        provider:      provider,
        claims:        claimsCopy,
    }, nil
}

// ID returns the user's unique identifier.
func (i Identity) ID() string { return i.id }

// Email returns the user's email address. May be empty.
func (i Identity) Email() string { return i.email }

// EmailVerified returns true if the email has been verified.
func (i Identity) EmailVerified() bool { return i.emailVerified }

// Provider returns the name of the identity provider that authenticated this user.
func (i Identity) Provider() string { return i.provider }

// Claims returns a copy of the additional claims map.
// Modifying the returned map does not affect the Identity.
func (i Identity) Claims() map[string]any {
    copy := make(map[string]any, len(i.claims))
    for k, v := range i.claims {
        copy[k] = v
    }
    return copy
}

// Claim returns the value of a specific claim, or nil if not present.
func (i Identity) Claim(name string) any {
    return i.claims[name]
}

// HasClaim reports whether the identity has the named claim.
func (i Identity) HasClaim(name string) bool {
    _, ok := i.claims[name]
    return ok
}

// IsZero reports whether this is the zero value (no identity).
func (i Identity) IsZero() bool {
    return i.id == ""
}
```

**New value object: `internal/domain/identity/auth_result.go`**

```go
package identity

// AuthResult represents the outcome of an authentication attempt.
// It contains either a valid Identity or information about why auth failed.
type AuthResult struct {
    // Identity is the authenticated user's identity. Zero value if auth failed.
    Identity Identity

    // Authenticated is true if authentication succeeded.
    Authenticated bool

    // Reason is a machine-readable code explaining auth failure (e.g., "token_expired",
    // "invalid_signature", "session_not_found"). Empty when Authenticated is true.
    Reason string

    // Message is a human-readable description of the failure. Empty when Authenticated is true.
    Message string
}

// Success creates an AuthResult for a successful authentication.
func Success(identity Identity) AuthResult {
    return AuthResult{
        Identity:      identity,
        Authenticated: true,
    }
}

// Failure creates an AuthResult for a failed authentication.
func Failure(reason, message string) AuthResult {
    return AuthResult{
        Authenticated: false,
        Reason:        reason,
        Message:       message,
    }
}
```

#### Ports (Interfaces)

**New file: `internal/ports/identity.go`**

```go
package ports

import (
    "context"
    "net/http"

    "github.com/vibewarden/vibewarden/internal/domain/identity"
)

// IdentityProvider validates authentication credentials from an HTTP request
// and returns the authenticated user's identity.
//
// This is the primary authentication port. Implementations include:
// - Kratos adapter (session cookie validation)
// - JWT adapter (Bearer token validation)
// - API key adapter (X-API-Key header validation)
//
// The auth middleware chains multiple IdentityProviders when configured,
// trying each in order until one succeeds or all fail.
type IdentityProvider interface {
    // Name returns the provider identifier (e.g., "kratos", "jwt", "apikey").
    // Used for logging, metrics labels, and the Identity.Provider field.
    Name() string

    // Authenticate extracts credentials from the request and validates them.
    // Returns an AuthResult indicating success or failure.
    //
    // If the provider cannot find any credentials it recognizes (e.g., no session
    // cookie for Kratos, no Bearer token for JWT), it returns a Failure result
    // with Reason "no_credentials". This allows the middleware to try the next
    // provider in the chain.
    //
    // If credentials are present but invalid, it returns a Failure result with
    // a specific Reason (e.g., "token_expired", "session_invalid").
    //
    // The context may carry request-scoped values (trace context, etc.).
    // Implementations must honour context cancellation.
    Authenticate(ctx context.Context, r *http.Request) identity.AuthResult
}

// IdentityProviderUnavailable is returned when the underlying identity service
// (e.g., Kratos, JWKS endpoint) cannot be reached. Middleware should handle this
// according to the configured degradation mode (fail-closed vs. allow-public).
type IdentityProviderUnavailable struct {
    Provider string
    Cause    error
}

func (e IdentityProviderUnavailable) Error() string {
    return "identity provider " + e.Provider + " unavailable: " + e.Cause.Error()
}

func (e IdentityProviderUnavailable) Unwrap() error {
    return e.Cause
}
```

**Update: `internal/ports/auth.go`**

Mark `SessionChecker` as deprecated but preserve for backward compatibility:

```go
// SessionChecker validates sessions against an identity provider.
//
// Deprecated: Use IdentityProvider instead. SessionChecker will be removed in v2.
// The Kratos adapter implements both interfaces during the migration period.
type SessionChecker interface {
    // CheckSession validates the given session cookie and returns the session if valid.
    // Returns ErrSessionInvalid if the session is invalid or expired.
    // Returns ErrSessionNotFound if no session exists for the cookie.
    // Returns ErrAuthProviderUnavailable when the identity provider cannot be reached.
    CheckSession(ctx context.Context, sessionCookie string) (*Session, error)
}
```

#### Adapters

**Update: `internal/adapters/kratos/adapter.go`**

The Kratos adapter now implements both `SessionChecker` (deprecated) and `IdentityProvider`.

```go
package kratos

import (
    // ... existing imports ...
    "github.com/vibewarden/vibewarden/internal/domain/identity"
)

// Adapter implements ports.SessionChecker and ports.IdentityProvider
// using the Ory Kratos public API.
type Adapter struct {
    publicURL     string
    client        *http.Client
    logger        *slog.Logger
    cookieName    string
}

// NewAdapter creates a new Kratos adapter.
// publicURL is the base URL of the Kratos public API (e.g. "http://localhost:4433").
// cookieName is the session cookie name (default: "ory_kratos_session").
func NewAdapter(publicURL string, cookieName string, timeout time.Duration, logger *slog.Logger) *Adapter {
    if timeout == 0 {
        timeout = defaultTimeout
    }
    if cookieName == "" {
        cookieName = defaultCookieName
    }
    return &Adapter{
        publicURL:  publicURL,
        client:     &http.Client{Timeout: timeout},
        logger:     logger,
        cookieName: cookieName,
    }
}

// Name implements ports.IdentityProvider.
func (a *Adapter) Name() string { return "kratos" }

// Authenticate implements ports.IdentityProvider.
// It extracts the session cookie from the request, validates it with Kratos,
// and returns an AuthResult with the user's identity.
func (a *Adapter) Authenticate(ctx context.Context, r *http.Request) identity.AuthResult {
    // Extract session cookie
    cookie, err := r.Cookie(a.cookieName)
    if err != nil {
        // No cookie = no credentials for this provider
        return identity.Failure("no_credentials", "no session cookie")
    }

    sessionCookie := a.cookieName + "=" + cookie.Value

    // Validate with Kratos
    session, err := a.CheckSession(ctx, sessionCookie)
    if err != nil {
        switch {
        case errors.Is(err, ports.ErrSessionInvalid):
            return identity.Failure("session_invalid", "session is invalid or expired")
        case errors.Is(err, ports.ErrSessionNotFound):
            return identity.Failure("session_not_found", "session does not exist")
        case errors.Is(err, ports.ErrAuthProviderUnavailable):
            return identity.Failure("provider_unavailable", err.Error())
        default:
            return identity.Failure("auth_error", err.Error())
        }
    }

    // Map Kratos session to domain Identity
    ident, err := identity.NewIdentity(
        session.Identity.ID,
        session.Identity.Email,
        "kratos",
        session.Identity.EmailVerified,
        session.Identity.Traits,
    )
    if err != nil {
        return identity.Failure("invalid_identity", err.Error())
    }

    return identity.Success(ident)
}

// CheckSession implements ports.SessionChecker (deprecated).
// Retained for backward compatibility; new code should use Authenticate.
func (a *Adapter) CheckSession(ctx context.Context, sessionCookie string) (*ports.Session, error) {
    // ... existing implementation unchanged ...
}
```

#### Application Service

No application service changes. The auth middleware is infrastructure, not a use case.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/domain/identity/identity.go` | Identity value object |
| `internal/domain/identity/identity_test.go` | Unit tests for Identity |
| `internal/domain/identity/auth_result.go` | AuthResult value object |
| `internal/domain/identity/auth_result_test.go` | Unit tests for AuthResult |
| `internal/ports/identity.go` | IdentityProvider interface |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/ports/auth.go` | Add deprecation notice to SessionChecker |
| `internal/adapters/kratos/adapter.go` | Add Name() and Authenticate() methods |
| `internal/adapters/kratos/adapter_test.go` | Add tests for IdentityProvider implementation |
| `internal/middleware/auth.go` | Accept IdentityProvider, fall back to SessionChecker wrapper |
| `internal/middleware/auth_test.go` | Update tests for IdentityProvider |
| `internal/plugins/auth/plugin.go` | Create IdentityProvider adapter, pass to middleware |

#### Sequence

**Authentication flow with new IdentityProvider:**

1. HTTP request arrives at VibeWarden
2. Auth middleware calls `provider.Authenticate(ctx, r)`
3. Kratos adapter:
   a. Extracts session cookie from request
   b. If no cookie, returns `Failure("no_credentials", ...)`
   c. Calls Kratos `/sessions/whoami` with cookie
   d. If Kratos returns 401, returns `Failure("session_invalid", ...)`
   e. If Kratos unavailable, returns `Failure("provider_unavailable", ...)`
   f. Parses response, creates domain `Identity` value object
   g. Returns `Success(identity)`
4. Auth middleware:
   a. If `Authenticated == true`: stores Identity in context, calls next handler
   b. If `Reason == "no_credentials"`: tries next provider (if chained) or redirects to login
   c. If `Reason == "provider_unavailable"`: handles per degradation mode config
   d. If other failure: redirects to login or returns error

**Backward compatibility flow:**

When code still uses the deprecated `SessionChecker`:
1. Auth plugin detects old-style config (no `provider` field)
2. Creates Kratos adapter implementing both interfaces
3. Middleware receives `IdentityProvider`, uses new flow
4. Old `CheckSession` method is only used by legacy code paths

#### Error Cases

| Error | Cause | Handling |
|-------|-------|----------|
| No credentials | Cookie/token absent | Return Failure("no_credentials"), try next provider |
| Session invalid | Expired or revoked | Return Failure("session_invalid"), redirect to login |
| Provider unavailable | Network/timeout | Return Failure("provider_unavailable"), use degradation mode |
| Invalid identity data | Malformed Kratos response | Return Failure("invalid_identity"), log error |

#### Test Strategy

**Unit tests:**

| File | Coverage |
|------|----------|
| `internal/domain/identity/identity_test.go` | NewIdentity validation, immutability, accessors |
| `internal/domain/identity/auth_result_test.go` | Success/Failure constructors |
| `internal/adapters/kratos/adapter_test.go` | Authenticate method with mock HTTP responses |
| `internal/middleware/auth_test.go` | Middleware with mock IdentityProvider |

**Unit test approach:**

- Test Identity value object in isolation (pure Go, no mocks needed)
- Test Kratos adapter with httptest server returning canned JSON
- Test middleware with fake IdentityProvider returning fixed AuthResults
- Table-driven tests for various auth scenarios

**Integration tests:**

| File | Coverage |
|------|----------|
| `internal/adapters/kratos/adapter_integration_test.go` | Real Kratos container via testcontainers |

**What to mock vs. real:**

- Mock: HTTP responses for unit tests
- Real: Kratos container for integration tests (existing setup)
- Real: Domain value objects (no external deps)

#### New Dependencies

**None.** This ADR introduces no new external dependencies. All new code uses:
- Go standard library
- Existing internal packages

### Consequences

**Positive:**

- Clean abstraction for multiple identity providers (Kratos, JWT, API keys)
- Domain-centric Identity type with no external dependencies
- Existing Kratos integration works unchanged
- Auth middleware becomes provider-agnostic
- Future JWT/OIDC adapter slots in cleanly
- Immutable value objects prevent accidental mutation

**Negative:**

- Two parallel interfaces during migration (SessionChecker deprecated)
- Slightly more code in Kratos adapter (implements both interfaces)
- Identity in domain layer vs. ports.Identity — minor type duplication

**Trade-offs:**

- **Identity in domain vs. ports:** Chose domain layer because Identity is a
  core concept with validation logic, not just a data transfer object. The
  ports layer will have a slim reference to domain.Identity rather than
  defining its own type.

- **AuthResult vs. error returns:** Chose explicit AuthResult value object over
  error returns because authentication failure is an expected outcome, not an
  exceptional condition. This makes the success/failure branches explicit and
  carries structured failure information.

- **Preserve SessionChecker vs. remove:** Chose deprecation over removal for
  backward compatibility. Any code using SessionChecker continues to work.
  Removal scheduled for v2.

- **Single Authenticate method vs. ExtractCredentials + Validate:** Chose
  single method for simplicity. The provider knows best how to extract its
  credentials; splitting adds complexity without benefit.

**Migration path:**

1. This story: Add IdentityProvider port, Kratos implements it, middleware updated
2. Future story #386: Add JWT/OIDC adapter implementing IdentityProvider
3. Future story #387: Add config to select/chain providers
4. v2: Remove deprecated SessionChecker interface

## ADR-023: JWT/OIDC Identity Adapter
**Date**: 2026-03-28
**Issue**: #384
**Status**: Accepted

### Context

Epic #373 ("Flexible Auth") requires VibeWarden to support JWT/OIDC authentication
in addition to Ory Kratos sessions. Many users already have an external identity
provider (Auth0, Okta, Keycloak, Azure AD, Google) and want to use VibeWarden as
a security sidecar without running Kratos.

ADR-022 introduced the `IdentityProvider` port interface that abstracts authentication
mechanisms. This ADR designs the JWT/OIDC adapter that implements that port.

**Requirements from issue #384:**

- Implement `ports.IdentityProvider` for JWT validation
- JWKS fetching with local caching and automatic key rotation
- Support RS256 and ES256 signature algorithms (minimum)
- Standard claim validation: `exp`, `iat`, `nbf`, `iss`, `aud`
- Claims-to-headers mapping (e.g., `sub` -> `X-User-Id`)
- OIDC Discovery support (auto-detect JWKS URL from `/.well-known/openid-configuration`)
- Structured log events for JWT validation outcomes

### Decision

Create a JWT adapter in `internal/adapters/jwt/` that:

1. Fetches JWKS from the configured endpoint (or discovers it via OIDC)
2. Caches keys with background refresh
3. Validates JWT signatures, timestamps, issuer, and audience
4. Maps claims to a domain `Identity` value object
5. Supports configurable claims-to-headers mapping

#### Domain Model Changes

**No new domain types.** The existing `identity.Identity` and `identity.AuthResult`
from ADR-022 are sufficient. The JWT adapter will:

- Set `Identity.ID()` from the `sub` claim
- Set `Identity.Email()` from the `email` claim (if present)
- Set `Identity.EmailVerified()` from the `email_verified` claim (if present, default false)
- Set `Identity.Provider()` to `"jwt"`
- Store all non-reserved claims in `Identity.Claims()`

Reserved claims excluded from `Claims()`: `sub`, `iss`, `aud`, `exp`, `iat`, `nbf`, `jti`, `typ`.

#### Ports (Interfaces)

**No new port interfaces.** The JWT adapter implements the existing `ports.IdentityProvider`
interface defined in ADR-022:

```go
// internal/ports/identity.go (existing)
type IdentityProvider interface {
    Name() string
    Authenticate(ctx context.Context, r *http.Request) identity.AuthResult
}
```

**New port interface for JWKS fetching:**

A new port is needed to abstract the JWKS HTTP client, enabling testability without
hitting real endpoints.

**File: `internal/ports/jwks.go`**

```go
package ports

import (
    "context"

    "github.com/go-jose/go-jose/v4"
)

// JWKSFetcher retrieves JSON Web Key Sets from a remote endpoint.
// Implementations handle HTTP transport, caching, and key rotation.
type JWKSFetcher interface {
    // FetchKeys retrieves the current JWKS from the configured endpoint.
    // The implementation should cache keys and refresh them periodically or
    // when a key is not found (key rotation scenario).
    //
    // Returns the JWKS or an error if the endpoint cannot be reached.
    FetchKeys(ctx context.Context) (*jose.JSONWebKeySet, error)

    // GetKey retrieves a specific key by key ID (kid).
    // If the key is not in the cache, the implementation should attempt a
    // refresh before returning an error.
    GetKey(ctx context.Context, kid string) (*jose.JSONWebKey, error)
}
```

#### Adapters

**New adapter: `internal/adapters/jwt/adapter.go`**

The JWT adapter implements `ports.IdentityProvider` using go-jose/v4 for JWT
parsing and signature validation.

```go
package jwt

import (
    "context"
    "errors"
    "log/slog"
    "net/http"
    "strings"
    "time"

    "github.com/go-jose/go-jose/v4"
    josejwt "github.com/go-jose/go-jose/v4/jwt"

    "github.com/vibewarden/vibewarden/internal/domain/identity"
    "github.com/vibewarden/vibewarden/internal/ports"
)

// Config holds JWT adapter configuration.
type Config struct {
    // JWKSURL is the URL to fetch the JWKS from.
    // Either JWKSURL or IssuerURL must be set.
    JWKSURL string

    // IssuerURL is the OIDC issuer URL for discovery.
    // When set, JWKS URL is discovered from /.well-known/openid-configuration.
    // If JWKSURL is also set, JWKSURL takes precedence.
    IssuerURL string

    // Issuer is the expected "iss" claim value. Required.
    Issuer string

    // Audience is the expected "aud" claim value. Required.
    Audience string

    // ClaimsToHeaders maps JWT claim names to HTTP header names.
    // Example: {"sub": "X-User-Id", "email": "X-User-Email"}
    // The middleware injects these headers into the request forwarded to upstream.
    ClaimsToHeaders map[string]string

    // AllowedAlgorithms restricts which signing algorithms are accepted.
    // Default: ["RS256", "ES256"] if empty.
    AllowedAlgorithms []string

    // CacheTTL is how long to cache the JWKS before refreshing.
    // Default: 1 hour.
    CacheTTL time.Duration

    // RefreshOnMissingKey controls whether to refresh JWKS when a key ID is
    // not found (supports key rotation). Default: true.
    RefreshOnMissingKey bool
}

// Adapter implements ports.IdentityProvider for JWT/OIDC tokens.
type Adapter struct {
    config  Config
    fetcher ports.JWKSFetcher
    logger  *slog.Logger
}

// NewAdapter creates a new JWT identity adapter.
func NewAdapter(cfg Config, fetcher ports.JWKSFetcher, logger *slog.Logger) (*Adapter, error) {
    // Validate configuration
    if cfg.JWKSURL == "" && cfg.IssuerURL == "" {
        return nil, errors.New("jwt: either jwks_url or issuer_url is required")
    }
    if cfg.Issuer == "" {
        return nil, errors.New("jwt: issuer is required")
    }
    if cfg.Audience == "" {
        return nil, errors.New("jwt: audience is required")
    }
    if len(cfg.AllowedAlgorithms) == 0 {
        cfg.AllowedAlgorithms = []string{"RS256", "ES256"}
    }
    if cfg.CacheTTL == 0 {
        cfg.CacheTTL = time.Hour
    }

    return &Adapter{
        config:  cfg,
        fetcher: fetcher,
        logger:  logger,
    }, nil
}

// Name implements ports.IdentityProvider.
func (a *Adapter) Name() string { return "jwt" }

// Authenticate implements ports.IdentityProvider.
// It extracts the Bearer token from the Authorization header, validates it,
// and returns an AuthResult with the user's identity.
func (a *Adapter) Authenticate(ctx context.Context, r *http.Request) identity.AuthResult {
    // Extract Bearer token
    authHeader := r.Header.Get("Authorization")
    if authHeader == "" {
        return identity.Failure("no_credentials", "no Authorization header")
    }
    if !strings.HasPrefix(authHeader, "Bearer ") {
        return identity.Failure("no_credentials", "Authorization header is not Bearer")
    }
    rawToken := strings.TrimPrefix(authHeader, "Bearer ")
    if rawToken == "" {
        return identity.Failure("no_credentials", "empty Bearer token")
    }

    // Parse the token (without verification first to get the key ID)
    tok, err := josejwt.ParseSigned(rawToken, a.config.AllowedAlgorithms)
    if err != nil {
        return identity.Failure("invalid_token", "failed to parse JWT: "+err.Error())
    }

    // Get the signing key
    if len(tok.Headers) == 0 {
        return identity.Failure("invalid_token", "JWT has no headers")
    }
    kid := tok.Headers[0].KeyID

    key, err := a.fetcher.GetKey(ctx, kid)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
            return identity.Failure("provider_unavailable", "JWKS fetch timed out")
        }
        return identity.Failure("invalid_signature", "key not found: "+kid)
    }

    // Verify signature and extract claims
    var claims josejwt.Claims
    var customClaims map[string]any
    if err := tok.Claims(key.Key, &claims, &customClaims); err != nil {
        return identity.Failure("invalid_signature", "signature verification failed: "+err.Error())
    }

    // Validate standard claims
    expected := josejwt.Expected{
        Issuer:   a.config.Issuer,
        Audience: josejwt.Audience{a.config.Audience},
        Time:     time.Now(),
    }
    if err := claims.Validate(expected); err != nil {
        reason := "token_invalid"
        if strings.Contains(err.Error(), "expired") {
            reason = "token_expired"
        } else if strings.Contains(err.Error(), "not yet valid") {
            reason = "token_not_yet_valid"
        } else if strings.Contains(err.Error(), "issuer") {
            reason = "invalid_issuer"
        } else if strings.Contains(err.Error(), "audience") {
            reason = "invalid_audience"
        }
        return identity.Failure(reason, err.Error())
    }

    // Build Identity
    email, _ := customClaims["email"].(string)
    emailVerified, _ := customClaims["email_verified"].(bool)

    // Filter out reserved claims from the claims map
    reservedClaims := map[string]bool{
        "sub": true, "iss": true, "aud": true, "exp": true,
        "iat": true, "nbf": true, "jti": true, "typ": true,
    }
    filteredClaims := make(map[string]any)
    for k, v := range customClaims {
        if !reservedClaims[k] {
            filteredClaims[k] = v
        }
    }

    ident, err := identity.NewIdentity(
        claims.Subject,
        email,
        "jwt",
        emailVerified,
        filteredClaims,
    )
    if err != nil {
        return identity.Failure("invalid_identity", err.Error())
    }

    return identity.Success(ident)
}
```

**New adapter: `internal/adapters/jwt/jwks_fetcher.go`**

The JWKS fetcher handles HTTP transport, caching, and key rotation.

```go
package jwt

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log/slog"
    "net/http"
    "sync"
    "time"

    "github.com/go-jose/go-jose/v4"
)

// HTTPJWKSFetcher implements ports.JWKSFetcher using HTTP.
type HTTPJWKSFetcher struct {
    jwksURL  string
    client   *http.Client
    logger   *slog.Logger
    cacheTTL time.Duration

    mu        sync.RWMutex
    cache     *jose.JSONWebKeySet
    cachedAt  time.Time
    refreshMu sync.Mutex // Prevents concurrent refresh requests
}

// NewHTTPJWKSFetcher creates a fetcher for the given JWKS URL.
func NewHTTPJWKSFetcher(jwksURL string, timeout time.Duration, cacheTTL time.Duration, logger *slog.Logger) *HTTPJWKSFetcher {
    if timeout == 0 {
        timeout = 10 * time.Second
    }
    if cacheTTL == 0 {
        cacheTTL = time.Hour
    }
    return &HTTPJWKSFetcher{
        jwksURL:  jwksURL,
        client:   &http.Client{Timeout: timeout},
        logger:   logger,
        cacheTTL: cacheTTL,
    }
}

// FetchKeys retrieves the current JWKS, using cache if valid.
func (f *HTTPJWKSFetcher) FetchKeys(ctx context.Context) (*jose.JSONWebKeySet, error) {
    f.mu.RLock()
    if f.cache != nil && time.Since(f.cachedAt) < f.cacheTTL {
        jwks := f.cache
        f.mu.RUnlock()
        return jwks, nil
    }
    f.mu.RUnlock()

    return f.refresh(ctx)
}

// GetKey retrieves a specific key by ID, refreshing if not found.
func (f *HTTPJWKSFetcher) GetKey(ctx context.Context, kid string) (*jose.JSONWebKey, error) {
    jwks, err := f.FetchKeys(ctx)
    if err != nil {
        return nil, err
    }

    keys := jwks.Key(kid)
    if len(keys) > 0 {
        return &keys[0], nil
    }

    // Key not found — try a refresh (handles key rotation)
    jwks, err = f.refresh(ctx)
    if err != nil {
        return nil, err
    }

    keys = jwks.Key(kid)
    if len(keys) > 0 {
        return &keys[0], nil
    }

    return nil, fmt.Errorf("key not found: %s", kid)
}

func (f *HTTPJWKSFetcher) refresh(ctx context.Context) (*jose.JSONWebKeySet, error) {
    f.refreshMu.Lock()
    defer f.refreshMu.Unlock()

    // Double-check: another goroutine may have refreshed while we waited for the lock.
    f.mu.RLock()
    if f.cache != nil && time.Since(f.cachedAt) < f.cacheTTL {
        jwks := f.cache
        f.mu.RUnlock()
        return jwks, nil
    }
    f.mu.RUnlock()

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.jwksURL, nil)
    if err != nil {
        return nil, fmt.Errorf("creating JWKS request: %w", err)
    }
    req.Header.Set("Accept", "application/json")

    resp, err := f.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("fetching JWKS: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
    }

    body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
    if err != nil {
        return nil, fmt.Errorf("reading JWKS response: %w", err)
    }

    var jwks jose.JSONWebKeySet
    if err := json.Unmarshal(body, &jwks); err != nil {
        return nil, fmt.Errorf("parsing JWKS: %w", err)
    }

    f.mu.Lock()
    f.cache = &jwks
    f.cachedAt = time.Now()
    f.mu.Unlock()

    f.logger.Info("JWKS cache refreshed",
        slog.String("url", f.jwksURL),
        slog.Int("key_count", len(jwks.Keys)),
    )

    return &jwks, nil
}
```

**New adapter: `internal/adapters/jwt/discovery.go`**

OIDC Discovery support for auto-detecting JWKS URL.

```go
package jwt

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
)

// OIDCConfiguration is the subset of OpenID Connect Discovery response we need.
type OIDCConfiguration struct {
    JwksURI string `json:"jwks_uri"`
    Issuer  string `json:"issuer"`
}

// DiscoverJWKSURL fetches the OIDC configuration and returns the JWKS URI.
func DiscoverJWKSURL(ctx context.Context, issuerURL string, timeout time.Duration) (string, error) {
    if timeout == 0 {
        timeout = 10 * time.Second
    }

    // Normalize issuer URL
    issuerURL = strings.TrimSuffix(issuerURL, "/")
    discoveryURL := issuerURL + "/.well-known/openid-configuration"

    client := &http.Client{Timeout: timeout}
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
    if err != nil {
        return "", fmt.Errorf("creating discovery request: %w", err)
    }
    req.Header.Set("Accept", "application/json")

    resp, err := client.Do(req)
    if err != nil {
        return "", fmt.Errorf("fetching OIDC configuration: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("OIDC discovery endpoint returned %d", resp.StatusCode)
    }

    body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
    if err != nil {
        return "", fmt.Errorf("reading OIDC configuration: %w", err)
    }

    var config OIDCConfiguration
    if err := json.Unmarshal(body, &config); err != nil {
        return "", fmt.Errorf("parsing OIDC configuration: %w", err)
    }

    if config.JwksURI == "" {
        return "", fmt.Errorf("OIDC configuration missing jwks_uri")
    }

    return config.JwksURI, nil
}
```

#### Application Service

No application service changes. The JWT adapter is infrastructure (an adapter
implementing a port). The auth middleware handles the authentication flow.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/ports/jwks.go` | JWKSFetcher interface |
| `internal/adapters/jwt/adapter.go` | JWT IdentityProvider implementation |
| `internal/adapters/jwt/adapter_test.go` | Unit tests for JWT adapter |
| `internal/adapters/jwt/jwks_fetcher.go` | HTTP JWKS fetcher implementation |
| `internal/adapters/jwt/jwks_fetcher_test.go` | Unit tests for JWKS fetcher |
| `internal/adapters/jwt/discovery.go` | OIDC Discovery helpers |
| `internal/adapters/jwt/discovery_test.go` | Unit tests for discovery |
| `internal/adapters/jwt/integration_test.go` | Integration tests with mock OIDC server |
| `internal/domain/events/jwt.go` | JWT-specific event constructors |
| `internal/domain/events/jwt_test.go` | Event tests |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/config/config.go` | Add `JWTConfig` struct, update `AuthConfig` |
| `internal/config/config_test.go` | Tests for JWT config validation |
| `internal/middleware/identity_headers.go` | Support claims-to-headers mapping |
| `internal/middleware/identity_headers_test.go` | Tests for claims mapping |
| `internal/plugins/auth/plugin.go` | Create JWT adapter when configured |
| `go.mod` | Promote go-jose/v4 from indirect to direct |

#### Configuration Schema

**Update `internal/config/config.go`:**

```go
// JWTConfig holds JWT/OIDC authentication settings.
type JWTConfig struct {
    // JWKSURL is the URL to fetch the JSON Web Key Set.
    // Mutually exclusive with IssuerURL: if both are set, JWKSURL takes precedence.
    JWKSURL string `mapstructure:"jwks_url"`

    // IssuerURL is the OIDC issuer URL for auto-discovery.
    // When set, JWKS URL is discovered from /.well-known/openid-configuration.
    IssuerURL string `mapstructure:"issuer_url"`

    // Issuer is the expected "iss" claim value. Required when provider is "jwt".
    Issuer string `mapstructure:"issuer"`

    // Audience is the expected "aud" claim value. Required when provider is "jwt".
    Audience string `mapstructure:"audience"`

    // ClaimsToHeaders maps JWT claim names to HTTP header names.
    // The mapped claims are injected into requests forwarded to the upstream app.
    // Default: {"sub": "X-User-Id", "email": "X-User-Email", "email_verified": "X-User-Verified"}
    ClaimsToHeaders map[string]string `mapstructure:"claims_to_headers"`

    // AllowedAlgorithms restricts which signing algorithms are accepted.
    // Default: ["RS256", "ES256"].
    AllowedAlgorithms []string `mapstructure:"allowed_algorithms"`

    // CacheTTL is how long to cache the JWKS before refreshing.
    // Default: "1h".
    CacheTTL time.Duration `mapstructure:"cache_ttl"`
}

// AuthMode is an enum for the authentication strategy.
type AuthMode string

const (
    AuthModeKratos AuthMode = "kratos"
    AuthModeJWT    AuthMode = "jwt"
    AuthModeAPIKey AuthMode = "api-key"
    AuthModeNone   AuthMode = "none"
)

// AuthConfig updated:
type AuthConfig struct {
    Enabled           bool                    `mapstructure:"enabled"`
    Mode              AuthMode                `mapstructure:"mode"` // Updated: add "jwt"
    JWT               JWTConfig               `mapstructure:"jwt"`  // New field
    APIKey            AuthAPIKeyConfig        `mapstructure:"api_key"`
    // ... existing fields ...
}
```

**Example YAML configuration:**

```yaml
auth:
  enabled: true
  provider: jwt
  jwt:
    # Option 1: Direct JWKS URL
    jwks_url: https://example.auth0.com/.well-known/jwks.json

    # Option 2: OIDC Discovery (auto-detect JWKS URL)
    # issuer_url: https://example.auth0.com/

    issuer: https://example.auth0.com/
    audience: my-api

    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      name: X-User-Name
      roles: X-User-Roles

    allowed_algorithms:
      - RS256
      - ES256

    cache_ttl: 1h
```

#### Sequence

**JWT authentication flow:**

1. HTTP request arrives at VibeWarden with `Authorization: Bearer <token>` header
2. Auth middleware calls `jwtAdapter.Authenticate(ctx, r)`
3. JWT adapter extracts Bearer token from Authorization header
4. If no token present, return `Failure("no_credentials", ...)`
5. Parse JWT (without verification) to extract key ID (kid) from header
6. Call `fetcher.GetKey(ctx, kid)` to retrieve the signing key
7. If JWKS fetch fails (network error), return `Failure("provider_unavailable", ...)`
8. Verify JWT signature using the key
9. If signature invalid, return `Failure("invalid_signature", ...)`
10. Validate standard claims (iss, aud, exp, iat, nbf)
11. If validation fails, return `Failure("token_expired" | "invalid_issuer" | ...)` 
12. Extract `sub`, `email`, custom claims and build `identity.Identity`
13. Return `Success(identity)`
14. Auth middleware stores Identity in request context
15. IdentityHeadersMiddleware injects configured claims as X-User-* headers
16. Request forwarded to upstream application

**JWKS caching flow:**

1. First request triggers JWKS fetch from `jwks_url`
2. Keys cached in memory with timestamp
3. Subsequent requests use cached keys if within `cache_ttl`
4. If `kid` not found in cache, trigger immediate refresh (handles key rotation)
5. Background refresh recommended but not required (lazy refresh on TTL expiry)

#### Error Cases

| Error | HTTP Status | Reason Code | Handling |
|-------|-------------|-------------|----------|
| No Authorization header | 302 Redirect | `no_credentials` | Redirect to login URL |
| Not a Bearer token | 302 Redirect | `no_credentials` | Redirect to login URL |
| JWT parse error | 401 | `invalid_token` | Return error response |
| Key ID not found | 401 | `invalid_signature` | Return error response |
| Signature verification failed | 401 | `invalid_signature` | Return error response |
| Token expired | 401 | `token_expired` | Return error response |
| Token not yet valid | 401 | `token_not_yet_valid` | Return error response |
| Invalid issuer | 401 | `invalid_issuer` | Return error response |
| Invalid audience | 401 | `invalid_audience` | Return error response |
| JWKS endpoint unreachable | 503 | `provider_unavailable` | Fail closed |
| JWKS timeout | 503 | `provider_unavailable` | Fail closed |

Note: For JWT mode, `no_credentials` may want to return 401 instead of redirect,
depending on use case (API vs. browser). Make this configurable via
`auth.jwt.on_missing_token: "redirect" | "401"` (default: "401" for JWT mode).

#### Structured Log Events

**New events in `internal/domain/events/jwt.go`:**

```go
const (
    EventTypeJWTValid    = "auth.jwt_valid"
    EventTypeJWTInvalid  = "auth.jwt_invalid"
    EventTypeJWTExpired  = "auth.jwt_expired"
    EventTypeJWKSRefresh = "auth.jwks_refresh"
    EventTypeJWKSError   = "auth.jwks_error"
)

// JWTValidParams contains parameters for auth.jwt_valid event.
type JWTValidParams struct {
    Method     string
    Path       string
    Subject    string
    Issuer     string
    Audience   string
}

// NewJWTValid creates an auth.jwt_valid event.
func NewJWTValid(params JWTValidParams) Event { ... }

// JWTInvalidParams contains parameters for auth.jwt_invalid event.
type JWTInvalidParams struct {
    Method string
    Path   string
    Reason string // "invalid_signature", "invalid_issuer", etc.
    Detail string
}

// NewJWTInvalid creates an auth.jwt_invalid event.
func NewJWTInvalid(params JWTInvalidParams) Event { ... }

// JWTExpiredParams contains parameters for auth.jwt_expired event.
type JWTExpiredParams struct {
    Method    string
    Path      string
    Subject   string
    ExpiredAt time.Time
}

// NewJWTExpired creates an auth.jwt_expired event.
func NewJWTExpired(params JWTExpiredParams) Event { ... }
```

#### Test Strategy

**Unit tests (`*_test.go` files):**

- `adapter_test.go`:
  - Test Authenticate with valid token (mock fetcher returns valid key)
  - Test Authenticate with missing Authorization header
  - Test Authenticate with non-Bearer Authorization
  - Test Authenticate with expired token
  - Test Authenticate with wrong issuer
  - Test Authenticate with wrong audience
  - Test Authenticate with invalid signature
  - Test Authenticate with unknown kid
  - Test claims extraction and Identity mapping
  - Test claims-to-headers mapping

- `jwks_fetcher_test.go`:
  - Test FetchKeys returns cached JWKS when within TTL
  - Test FetchKeys refreshes when TTL expired
  - Test GetKey finds existing key
  - Test GetKey triggers refresh on missing key
  - Test concurrent access to cache is safe
  - Test HTTP error handling (non-200, timeout)

- `discovery_test.go`:
  - Test DiscoverJWKSURL with valid OIDC config
  - Test DiscoverJWKSURL with missing jwks_uri
  - Test DiscoverJWKSURL with HTTP error

**Integration tests (`integration_test.go`):**

- Spin up httptest.Server serving mock JWKS
- Generate valid RS256 and ES256 tokens
- Test full flow: token -> adapter -> Identity
- Test key rotation: remove old key, add new key, verify refresh
- Test OIDC discovery against mock discovery endpoint

**Config validation tests:**

- Test that `auth.provider: jwt` requires `jwt.issuer` and `jwt.audience`
- Test that at least one of `jwks_url` or `issuer_url` is required
- Test default values for `allowed_algorithms` and `cache_ttl`

#### New Dependencies

**Direct dependency (promote from indirect):**

| Library | Version | License | Reason |
|---------|---------|---------|--------|
| `github.com/go-jose/go-jose/v4` | v4.1.3+ | Apache 2.0 | JWT parsing, signature verification, JWKS handling |

Already an indirect dependency. License verified: Apache 2.0.

**No new dependencies.** go-jose/v4 is the standard library for JOSE in Go,
maintained by the original authors of square/go-jose, and is already in the
dependency tree via Ory Kratos.

### Consequences

**Positive:**

- Users with existing OIDC providers (Auth0, Okta, Keycloak) can use VibeWarden
  without running Kratos
- Stateless authentication — no session storage needed
- Standard OIDC compatibility — works with any compliant provider
- JWKS caching minimizes network calls and latency
- Key rotation handled automatically via refresh-on-missing-key
- Claims-to-headers mapping gives upstream apps full access to user context

**Negative:**

- No session revocation — JWTs are valid until expiry (stateless trade-off)
- JWKS endpoint becomes a dependency — if unreachable, auth fails (fail-closed)
- More configuration surface than Kratos (issuer, audience, etc.)

**Trade-offs:**

- **Fail-closed on JWKS unavailable:** Chose security over availability. If we
  cannot verify signatures, we cannot trust tokens. Configure short JWKS cache
  TTL (1h default) with refresh-on-missing-key for resilience.

- **Lazy refresh vs. background refresh:** Chose lazy refresh (refresh on cache
  miss or TTL expiry) for simplicity. Background refresh adds complexity and a
  goroutine lifecycle to manage. Lazy refresh is sufficient for most workloads.

- **go-jose/v4 vs. lestrrat-go/jwx:** Chose go-jose because it is already an
  indirect dependency (via Ory Kratos), avoiding dependency duplication. Both
  libraries are well-maintained with compatible licenses.

- **JWT mode returns 401 by default (not redirect):** JWT mode is typically for
  APIs, not browsers. Redirecting to a login URL makes no sense for API clients.
  Made `on_missing_token` configurable for hybrid use cases.

**Security considerations:**

- Algorithm restriction: Only RS256 and ES256 by default. Never allow `none` or
  symmetric algorithms like HS256 (where the secret would need to be shared).
- Issuer and audience validation mandatory: Prevents token substitution attacks.
- Key ID (kid) required: JWTs without kid are rejected to prevent key confusion.

---

## ADR-497: Graceful Shutdown / Connection Draining

**Issue:** #497
**Status:** CLOSED — already implemented
**Date:** 2026-03-28

### Context

Issue #497 asked whether the VibeWarden sidecar handles SIGTERM/SIGINT gracefully
(i.e., drains in-flight connections before exiting) and, if not, to add proper
signal handling with a shutdown timeout.

### Decision

No code changes required. The full graceful-shutdown pipeline was already in place
before this issue was raised:

1. **Signal capture** (`cmd/vibewarden/serve.go`): A goroutine calls
   `signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)` and, on receipt of
   either signal, calls `cancel()` on the root `context.WithCancel` context.
   Signal handling is set up *before* `registry.InitAll` so that even a slow
   plugin initialisation can be interrupted.

2. **Proxy service shutdown** (`internal/app/proxy/service.go`): `Service.Run`
   blocks on `ctx.Done()`. When the context is cancelled it calls
   `server.Stop(shutdownCtx)` with a dedicated `context.WithTimeout` set to
   `30 seconds` (`defaultShutdownTimeout`). This gives in-flight HTTP requests
   up to 30 seconds to complete.

3. **Caddy adapter** (`internal/adapters/caddy/adapter.go`): `Stop` delegates
   directly to `caddy.Stop()`. Caddy's own shutdown logic drains active
   connections and releases listeners before returning.

4. **Plugin registry cleanup** (`cmd/vibewarden/serve.go`): A `defer` block
   calls `registry.StopAll(stopCtx)` with a `10-second` timeout, ensuring that
   background plugins (metrics exporter, egress proxy, etc.) are stopped cleanly
   after the main proxy has drained.

The shutdown sequence is therefore:

```
SIGTERM / SIGINT
  └─> context cancel
        └─> proxy.Service.Run exits select
              └─> caddy.Stop() drains connections (30 s budget)
                    └─> registry.StopAll() stops plugins (10 s budget)
```

### Verification

Existing unit tests in `internal/app/proxy/service_test.go` cover:

- `TestService_Run_ContextCancellation` — verifies `Stop` is called on context
  cancellation and the service returns `context.Canceled`.
- `TestService_Run_ServerError` — verifies early server errors propagate.
- `TestService_Run_StopError` — verifies stop errors propagate correctly.

### Consequences

**Positive:**

- Zero downtime deploys and container restarts are safe: Kubernetes / systemd
  both send SIGTERM and wait for the process to exit.
- Active connections complete normally within the 30-second drain window.
- Plugin teardown (flush metrics, close DB connections) runs within 10 seconds
  even if the proxy drain is slow or completes early.

**No new dependencies.** The implementation uses only stdlib (`os/signal`,
`syscall`, `context`) and existing Caddy APIs.
- JWKS URL must be HTTPS in production (not enforced in code for local dev).

---

## ADR-057: Defer config value-object migration from ports/ to config/ (issue #527)
**Date**: 2026-03-28
**Status**: Deferred — post-1.0

### Context

Issue #527 identified that `internal/ports/` contains plain data structs that carry
no interface behavior and arguably belong in `internal/config/` or a dedicated
`internal/config/proxy/` package. Examples:

| Type | File |
|---|---|
| `ProxyConfig`, `TLSConfig`, `TLSProvider*` constants | `ports/proxy.go` |
| `SecurityHeadersConfig` | `ports/proxy.go` |
| `ResilienceConfig`, `RetryConfig` | `ports/proxy.go` |
| `IPFilterConfig`, `BodySizeConfig`, `BodySizeOverride` | `ports/proxy.go` |
| `AdminProxyConfig`, `MetricsProxyConfig`, `ReadinessProxyConfig` | `ports/proxy.go` |
| `AdminAuthConfig` | `ports/admin_auth.go` |
| `AuthConfig`, `KratosUnavailableBehavior` | `ports/auth.go` |
| `MetricsConfig` | `ports/metrics.go` |
| `RateLimitConfig`, `RateLimitRule`, `RateLimitResult` | `ports/ratelimit.go` |
| `CircuitBreakerConfig` | `ports/circuit_breaker.go` |

The root cause is that `internal/config/config.go` already defines structurally
equivalent types (`config.TLSConfig`, `config.RateLimitConfig`, etc.) for YAML
deserialization. `cmd/vibewarden/serve_config.go` then performs explicit field-by-field
copies from `config.*` into `ports.*` when building `ports.ProxyConfig`. The `ports`
versions carry no `mapstructure` tags and exist purely to keep the adapters and
middleware decoupled from `internal/config`.

The ideal end state would be one of:
- A dedicated `internal/config/proxy/` package holding the ports-facing config
  structs, imported by both the YAML loader and the adapters (eliminating the
  field-copy boilerplate).
- Or simply re-using `config.*` types directly in the ports layer after verifying
  the import graph remains acyclic.

### Why deferred

A grep across the repository identified **55 unique Go files** referencing config
types from the `ports` package (`ports.SecurityHeadersConfig`, `ports.RateLimitConfig`,
`ports.ResilienceConfig`, etc.), spread across:

- `internal/adapters/caddy/` — 14 files (including integration tests)
- `internal/adapters/ratelimit/` — 8 files
- `internal/adapters/resilience/` — 3 files
- `internal/middleware/` — 11 files
- `internal/plugins/` — 3 files
- `cmd/vibewarden/` — 2 files
- `test/benchmarks/` — 1 file

This exceeds the threshold for a safe pre-1.0 refactor. Touching 55 files across
adapters, middleware, and tests in a single PR creates significant merge risk and a
large surface for subtle regressions with no user-visible benefit.

### Decision

Defer the migration to post-1.0. No code changes are made in this ADR.

The current design — duplicate config structs in `ports/` with explicit mapping in
`serve_config.go` — is verbose but correct. It keeps the import graph clean and the
adapters free of `mapstructure` tags.

### Post-1.0 plan

When tackling this after v1 ships:

1. Create `internal/config/proxy/` package with the ports-facing structs (no
   `mapstructure` tags, no viper dependency).
2. Delete the duplicate definitions from `internal/ports/`.
3. Update all 55 callers in a single atomic commit.
4. Remove the field-copy boilerplate in `serve_config.go` by sharing the struct
   directly between loader and adapter.
5. Ensure `go vet ./...` and all integration tests pass before merging.

---

## ADR-058: Plugin extension point for external plugin registration (Pro)
**Date**: 2026-03-28
**Issue**: #575
**Status**: Accepted

### Context

The VibeWarden Pro binary needs to register additional plugins alongside the OSS
catalog. Currently `registerPlugins()` in `cmd/vibewarden/serve_plugins.go` is an
unexported function that hardcodes all plugin registrations. There is no way for an
external package (the vibewarden-pro repo) to add plugins without forking.

The Pro repo will import the OSS module and compile a binary with additional plugins
registered. This requires the OSS registration to be composable and the serve
entrypoint to accept extension hooks.

### Decision

Introduce a `PluginRegistrar` function type and move the plugin registration logic
from `cmd/vibewarden/` into the `internal/plugins/` package. The serve command becomes
a thin wrapper that calls an exported `RunServe` function, which accepts optional
`PluginRegistrar` hooks for extension.

#### Domain model changes

None. This is a pure refactoring of serve-time wiring code. No entities, value
objects, or domain events are introduced or modified.

#### Ports (interfaces)

No new interfaces in `internal/ports/`. The `PluginRegistrar` type is a function
type, not an interface, and lives in `internal/plugins/` alongside the `Registry`.

#### Adapters

No new adapters. Existing adapters are unaffected.

#### Application service

Create a new package `internal/app/serve/` containing the serve orchestration logic.
This centralizes the startup sequence and makes it importable from both the OSS
`main.go` and the Pro `main.go`.

#### File layout

New files to create:

| Path | Purpose |
|------|---------|
| `internal/plugins/registrar.go` | Defines `PluginRegistrar` type |
| `internal/plugins/builtin.go` | Exports `RegisterBuiltinPlugins` function |
| `internal/plugins/builtin_helpers.go` | Plugin builder helpers (moved from `serve_config.go`) |
| `internal/app/serve/serve.go` | Exports `RunServe` function with extension hooks |
| `internal/app/serve/logger.go` | Logger construction helpers (moved from `serve.go`) |
| `internal/app/serve/config.go` | ProxyConfig construction helpers (moved from `serve_config.go`) |

Files to modify:

| Path | Change |
|------|--------|
| `cmd/vibewarden/serve.go` | Thin wrapper calling `serve.RunServe()` |

Files to delete:

| Path | Reason |
|------|--------|
| `cmd/vibewarden/serve_plugins.go` | Logic moved to `internal/plugins/builtin.go` |
| `cmd/vibewarden/serve_config.go` | Logic moved to `internal/app/serve/config.go` and `internal/plugins/builtin_helpers.go` |

#### Type definitions

```go
// internal/plugins/registrar.go

// PluginRegistrar is a function that registers plugins with a Registry.
// It is called during serve startup to allow external packages to contribute
// additional plugins.
type PluginRegistrar func(
    registry *Registry,
    cfg *config.Config,
    eventLogger ports.EventLogger,
    logger *slog.Logger,
)
```

```go
// internal/plugins/builtin.go

// RegisterBuiltinPlugins registers all OSS plugins with the registry based on
// the provided configuration. This function is called automatically by RunServe
// and should not be called directly unless composing a custom startup sequence.
func RegisterBuiltinPlugins(
    registry *Registry,
    cfg *config.Config,
    eventLogger ports.EventLogger,
    logger *slog.Logger,
)
```

```go
// internal/app/serve/serve.go

// Options configures the serve command behavior.
type Options struct {
    ConfigPath string
    Version    string
}

// RunServe loads config, builds the plugin registry, wires Caddy via plugin
// contributors, and runs until a shutdown signal is received.
//
// Additional plugins can be registered by passing PluginRegistrar functions.
// They are called after RegisterBuiltinPlugins, allowing Pro or custom plugins
// to be added to the registry.
func RunServe(ctx context.Context, opts Options, extraPlugins ...plugins.PluginRegistrar) error
```

#### Sequence

1. `cmd/vibewarden/main.go` calls `rootCmd.AddCommand(newServeCmd())`
2. User runs `vibewarden serve --config /path/to/config.yaml`
3. `newServeCmd().RunE` calls `serve.RunServe(ctx, opts)`
4. `RunServe`:
   a. Calls `config.Load(opts.ConfigPath)` to load configuration
   b. Calls `serve.buildLogger(cfg.Log)` to create the logger
   c. Calls `config.MigrateLegacyMetrics(cfg, logger)` for backward compat
   d. Creates initial event logger (stdout-only)
   e. Creates plugin registry via `plugins.NewRegistry(logger)`
   f. Calls `plugins.RegisterBuiltinPlugins(registry, cfg, eventLogger, logger)`
   g. Calls each `extraPlugins[i](registry, cfg, eventLogger, logger)` in order
   h. Sets up OS signal handling (SIGINT, SIGTERM)
   i. Calls `registry.InitAll(ctx)`
   j. Calls `serve.buildEventLogger(registry, logger)` to upgrade to OTel if enabled
   k. Calls `serve.wireTLSMetricsCollector(registry)` for cert expiry metrics
   l. Calls `registry.StartAll(ctx)`
   m. Defers `registry.StopAll(stopCtx)` for cleanup
   n. Calls `serve.buildProxyConfig(cfg, registry, opts.Version)` to construct ProxyConfig
   o. Creates Caddy adapter via `caddyadapter.NewAdapter(proxyCfg, logger, eventLogger)`
   p. Creates proxy service via `proxy.NewService(adapter, logger)`
   q. Calls `svc.Run(ctx)` and blocks until shutdown
5. Pro binary: `vibewarden-pro/main.go` calls `serve.RunServe(ctx, opts, proplugins.RegisterAll)`
   - `proplugins.RegisterAll` is a `PluginRegistrar` that registers Pro plugins

#### Error cases

| Error | Handling |
|-------|----------|
| Config load failure | Return wrapped error immediately |
| Critical plugin Init failure | Return wrapped error, no Start |
| Non-critical plugin Init failure | Log warning, mark degraded, continue |
| Critical plugin Start failure | Return wrapped error |
| Non-critical plugin Start failure | Log warning, mark degraded, continue |
| Proxy Run failure | Return wrapped error |
| Plugin Stop failure | Log error, continue stopping remaining plugins |

Unknown config sections: Viper's `Unmarshal` naturally ignores keys that do not
map to struct fields. When the OSS binary encounters config for Pro plugins (e.g.,
`pii_masking:`, `llm_cost:`), the values are silently ignored. No warning is logged
because the config loader has no knowledge of which plugins are compiled in.

The Pro binary will validate that required config sections are present for its
plugins during plugin Init (not at config load time). This keeps the config
loader simple and stateless.

#### Test strategy

**Unit tests:**

- `internal/plugins/builtin_test.go`:
  - Test `RegisterBuiltinPlugins` registers expected plugin count
  - Test plugin order matches expected priority order (verify via `registry.Plugins()`)
  - Use mock config to verify each plugin is conditionally registered based on enabled flags

- `internal/app/serve/serve_test.go`:
  - Test `RunServe` with mock registry and mock proxy server
  - Test that `extraPlugins` are called after builtin registration
  - Test that context cancellation triggers graceful shutdown
  - Test error propagation from config load, InitAll, StartAll, Run

**Integration tests:**

None required. This is a pure refactoring with no behavior change. Existing
integration tests in `cmd/vibewarden/serve_test.go` and `cmd/vibewarden/serve_config_test.go`
continue to exercise the same code paths (now via `RunServe`).

#### New dependencies

None. This refactoring uses only existing dependencies and stdlib.

### Consequences

**Positive:**

- Pro repo can compile a binary with additional plugins without forking OSS
- Clear extension point via `PluginRegistrar` function type
- `cmd/vibewarden/serve.go` becomes a thin entrypoint (< 30 lines)
- Plugin registration logic is testable in isolation
- `RunServe` is reusable for embedding VibeWarden in other Go programs

**Negative:**

- Moving code from `cmd/` to `internal/` increases the "internal API surface"
  that the Pro repo depends on. Changes to `RunServe` signature or
  `PluginRegistrar` type require coordinated releases.
- Three new files created, two deleted. Net increase of one file.

**Migration for Pro repo:**

The Pro repo currently does not exist. When it is created:

1. Import `github.com/vibewarden/vibewarden/internal/app/serve`
2. Import `github.com/vibewarden/vibewarden/internal/plugins`
3. Implement `proplugins.RegisterAll` as a `plugins.PluginRegistrar`
4. Call `serve.RunServe(ctx, opts, proplugins.RegisterAll)` from Pro's `main.go`

**Unknown config handling:**

No warning is logged for unknown config sections. This is intentional:

- Viper silently ignores unknown keys by design
- Adding a warning would require the config loader to know about all valid keys,
  which changes with each plugin
- Strict validation is problematic when users share a single config file between
  OSS and Pro deployments

If a user misconfigures a Pro-only section in an OSS deployment, the section is
simply ignored. When they upgrade to Pro, the section takes effect. This is the
expected behavior.

---

## ADR-059: Rename `vibew init` to `vibew wrap` for sidecar-only scaffolding
**Date**: 2026-03-28
**Issue**: #601
**Status**: Accepted

### Context

The current `vibew init` command scaffolds sidecar wrapping for existing projects:
- `vibewarden.yaml` (configuration)
- `vibew`, `vibew.ps1`, `vibew.cmd` (wrapper scripts)
- `.vibewarden-version` (version pin)
- `.gitignore` entry for `.vibewarden/`
- AI agent context files (`.claude/CLAUDE.md`, `.cursor/rules`, `AGENTS.md`)

Issue #600 (epic) introduces full project scaffolding, where `vibew init` will create
an entirely new project from templates (similar to `cargo new` or `go mod init`). The
current "wrap an existing project" behavior must move to a new command to free up
`vibew init` for this purpose.

The command rename is purely cosmetic — no changes to scaffolded output or behavior.

### Decision

Rename the existing `vibew init` command to `vibew wrap`. Create a placeholder
`vibew init` that prints a helpful message until #602 lands.

#### Domain model changes

None. This is a pure CLI layer change. No entities, value objects, or domain events
are affected.

#### Ports (interfaces)

None. No new interfaces required. The existing `ports.TemplateRenderer` and
`ports.ProjectDetector` interfaces remain unchanged.

#### Adapters

None. No adapter changes required.

#### Application service

None. The existing `internal/app/scaffold.Service` and `internal/app/scaffold.AgentContextService`
remain unchanged. Only the CLI command layer changes.

#### File layout

Files to rename:

| From | To |
|------|-----|
| `internal/cli/cmd/init.go` | `internal/cli/cmd/wrap.go` |
| `internal/cli/cmd/init_test.go` | `internal/cli/cmd/wrap_test.go` |
| `internal/cli/cmd/init_wrapper_test.go` | `internal/cli/cmd/wrap_wrapper_test.go` |

New files to create:

| Path | Purpose |
|------|---------|
| `internal/cli/cmd/init.go` | Placeholder `vibew init` command |
| `internal/cli/cmd/init_test.go` | Test for placeholder behavior |

Files to modify:

| Path | Change |
|------|--------|
| `internal/cli/cmd/root.go` | Add `NewWrapCmd()`, keep `NewInitCmd()` for placeholder |
| `internal/cli/cmd/wrap.go` | Rename command from "init" to "wrap", update help text |
| `internal/cli/cmd/add_*.go` | Update error messages from "run 'vibewarden init'" to "run 'vibewarden wrap'" |
| `internal/cli/cmd/context.go` | Update hint from "Run 'vibewarden init --agent all'" to "Run 'vibewarden wrap --agent all'" |
| `internal/cli/cmd/add_helpers.go` | Update error message |
| `internal/cli/templates/vibew.tmpl` | Update comment from "Generated by `vibewarden init`" to "Generated by `vibewarden wrap`" |
| `internal/cli/templates/vibew.ps1.tmpl` | Update comment |
| `internal/cli/templates/vibew.cmd.tmpl` | Update comment |
| `internal/cli/templates/vibewarden.yaml.tmpl` | Update comment |
| `internal/app/scaffold/service.go` | Update doc comment referencing `vibewarden init` |
| `README.md` | Update `vibew init` references to `vibew wrap` |
| `docs/index.md` | Update command table |
| `docs/getting-started.md` | Update Step 2 heading and instructions |
| `docs/postgres.md` | Update references |
| `docs/rate-limiting.md` | Update references |
| `docs/troubleshooting.md` | Update references |
| `scripts/install.sh` | Update post-install instructions |
| `scripts/install.ps1` | Update post-install instructions |
| `test/quickstart/test.sh` | Update test comments and command invocations |
| `test/quickstart/README.md` | Update documentation |
| `.claude/agents/user.md` | Update CLI reference |
| `.claude/agents/writer.md` | Update CLI reference |
| `CHANGELOG.md` | No change — historical references remain accurate for when they were written |

#### Command specifications

**New `vibew wrap` command:**

```go
// internal/cli/cmd/wrap.go

// NewWrapCmd creates the `vibewarden wrap` subcommand.
//
// The command scaffolds vibewarden.yaml and the vibew wrapper scripts in the
// current directory (or the directory supplied as the first positional argument).
// When --agent is specified, AI agent context files are also generated.
// Docker Compose and Kratos config are generated at runtime by `vibew dev`.
func NewWrapCmd() *cobra.Command {
    // ... same implementation as current NewInitCmd ...
    cmd := &cobra.Command{
        Use:   "wrap [directory]",
        Short: "Add VibeWarden sidecar to an existing project",
        Long: `Scaffold vibewarden.yaml and the vibew wrapper scripts in an existing project directory.

Docker Compose and Kratos config are generated at runtime by ` + "`vibew dev`" + `.
The command detects the project type and upstream port automatically.
Pass flags to enable optional features.

Examples:
  vibewarden wrap
  vibewarden wrap --upstream 8000
  vibewarden wrap --auth --rate-limit
  vibewarden wrap --tls --domain example.com
  vibewarden wrap --version v0.2.0
  vibewarden wrap --skip-wrapper
  vibewarden wrap --agent claude
  vibewarden wrap --agent all
  vibewarden wrap --force`,
        // ... rest identical to current init command ...
    }
    // ... flags identical ...
    return cmd
}
```

**Placeholder `vibew init` command:**

```go
// internal/cli/cmd/init.go

// NewInitCmd creates a placeholder `vibewarden init` subcommand.
//
// The init command is reserved for full project scaffolding (creating a new
// project from scratch). Until that feature is implemented, this placeholder
// directs users to `vibewarden wrap` for adding VibeWarden to existing projects.
func NewInitCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "init",
        Short: "Create a new VibeWarden project (coming soon)",
        Long: `Create a new project with VibeWarden pre-configured.

This command is currently under development. To add VibeWarden to an
existing project, use:

  vibewarden wrap [directory]

See 'vibewarden wrap --help' for options.`,
        RunE: func(cmd *cobra.Command, args []string) error {
            fmt.Fprintln(cmd.OutOrStdout(), `The 'init' command is reserved for full project scaffolding (coming soon).

To add VibeWarden to an existing project, use:

  vibewarden wrap [directory]

Run 'vibewarden wrap --help' for available options.`)
            return nil
        },
    }
}
```

**Success message update:**

```go
// internal/cli/cmd/wrap.go

func printSuccessMessage(cmd *cobra.Command, dir string, opts scaffoldapp.InitOptions, agentFiles []string) {
    // Change "VibeWarden initialized!" to "VibeWarden added to project!"
    fmt.Fprintln(w, "VibeWarden added to project!")
    // ... rest unchanged ...
}
```

#### Sequence

1. User runs `vibewarden wrap --upstream 3000`
2. CLI parses flags (identical to current `init`)
3. `NewWrapCmd().RunE` invokes `scaffoldapp.Service.Init()`
4. Service scaffolds files (unchanged behavior)
5. CLI prints success message "VibeWarden added to project!"

If user runs `vibewarden init`:

1. User runs `vibewarden init`
2. CLI prints placeholder message directing to `vibewarden wrap`
3. Command exits with success (code 0) — this is informational, not an error

#### Error cases

No new error cases. The `wrap` command inherits all error handling from the
current `init` command:

| Error | Handling |
|-------|----------|
| `vibewarden.yaml` exists without `--force` | Return error wrapping `os.ErrExist` |
| `--tls` without `--domain` | Return error "domain is required when --tls is set" |
| Invalid `--agent` value | Return error listing valid values |
| File write permission denied | Return wrapped OS error |
| Project detection failure | Return wrapped error |

The placeholder `init` command has no error cases — it always succeeds after
printing the informational message.

#### Test strategy

**Unit tests for `vibew wrap` (`internal/cli/cmd/wrap_test.go`):**

Rename all existing tests from `TestNewInitCmd_*` to `TestNewWrapCmd_*`. Update
test cases to use `wrap` instead of `init` in command args. Tests verify:

- Flag combinations work identically to before
- Files are generated correctly
- Error conditions trigger appropriate errors
- Success message now says "VibeWarden added to project!"

**Unit tests for placeholder `vibew init` (`internal/cli/cmd/init_test.go`):**

New minimal test file:

```go
func TestNewInitCmd_PrintsPlaceholder(t *testing.T) {
    root := cmd.NewRootCmd("test")
    var out bytes.Buffer
    root.SetOut(&out)
    root.SetArgs([]string{"init"})

    err := root.Execute()
    if err != nil {
        t.Fatalf("init command should not error: %v", err)
    }

    output := out.String()
    if !strings.Contains(output, "vibewarden wrap") {
        t.Errorf("expected output to mention 'vibewarden wrap', got: %s", output)
    }
    if !strings.Contains(output, "coming soon") {
        t.Errorf("expected output to mention 'coming soon', got: %s", output)
    }
}

func TestNewInitCmd_IgnoresArgs(t *testing.T) {
    // Placeholder should not error on args (for discoverability)
    root := cmd.NewRootCmd("test")
    var out bytes.Buffer
    root.SetOut(&out)
    root.SetArgs([]string{"init", "some-directory", "--upstream", "3000"})

    err := root.Execute()
    // Should not error, just print message
    if err != nil {
        t.Fatalf("init command should not error even with args: %v", err)
    }
}
```

**Integration test update (`test/quickstart/test.sh`):**

Update the test to use `vibewarden wrap` instead of `vibewarden init`.

#### New dependencies

None. This is a pure rename refactoring using existing dependencies.

### Consequences

**Positive:**

- `vibew init` is now free for full project scaffolding (#602)
- Clearer command naming: "wrap" implies adding to existing project
- Backward compatibility: users running old `init` get helpful redirect message
- Documentation updates make the distinction clear

**Negative:**

- Breaking change for users with scripts invoking `vibew init`
- Documentation/tutorials need updating (one-time cost)

**Migration path for users:**

Users with existing scripts should replace `vibew init` with `vibew wrap`. The
placeholder message provides this guidance. No data migration is needed — the
scaffolded files are identical.
