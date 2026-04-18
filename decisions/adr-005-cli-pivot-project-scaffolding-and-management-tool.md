# ADR-005: CLI Pivot — Project Scaffolding and Management Tool


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
