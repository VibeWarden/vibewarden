# ADR-003: Project Scaffold Technical Design


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
