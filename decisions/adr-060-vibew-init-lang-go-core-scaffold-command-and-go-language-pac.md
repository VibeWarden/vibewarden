# ADR-060: `vibew init --lang go` — Core scaffold command and Go language pack

**Date**: 2026-03-28
**Issue**: #602
**Status**: Accepted

### Context

Vibe coders starting a new Go project have no way to get a pre-structured project with
VibeWarden sidecar and AI coding agents in one command. They must manually set up
directory layout, agent files, and sidecar config.

Issue #602 implements full project scaffolding: when a user runs `vibew init --lang go myproject`,
it creates a complete Go project with:
- Hexagonal architecture layout (cmd/, internal/domain/, internal/ports/, internal/adapters/, internal/app/)
- Minimal main.go that compiles and runs on port 3000 with /health endpoint
- Dockerfile
- go.mod
- .claude/agents/ with architect.md, dev.md, reviewer.md — all vibew-aware
- CLAUDE.md with vibew CLI reference
- vibewarden.yaml (TLS self-signed, rate limiting enabled, no auth by default)
- .gitignore

The generated agent files are the key deliverable — they must enable AI agents to operate
autonomously with vibew commands without asking the user how to run or test the project.

### Decision

Implement `vibew init --lang go` as a new cobra command that generates a complete Go project
from embedded templates. The command replaces the placeholder `NewInitCmd()` in
`internal/cli/cmd/init.go`.

#### Domain model changes

Add new value objects to `internal/domain/scaffold/types.go`:

```go
// Language represents a supported programming language for project scaffolding.
type Language string

const (
    // LanguageGo is the Go programming language.
    LanguageGo Language = "go"
    // Future: LanguageTypeScript, LanguageKotlin
)

// InitProjectData is the data passed to project scaffold templates when rendering.
type InitProjectData struct {
    // ProjectName is the name of the project (directory name, used in go.mod).
    ProjectName string

    // ModulePath is the Go module path (e.g., "github.com/user/myproject").
    ModulePath string

    // Port is the HTTP port the generated app listens on.
    Port int

    // Language is the target programming language.
    Language Language
}
```

No new domain events — scaffolding is a synchronous operation.

#### Ports (interfaces)

No new interfaces required. The existing `ports.TemplateRenderer` handles all template
rendering. The existing `ports.ProjectDetector` is not used for init (we create from
scratch, not wrap existing).

#### Adapters

No new adapters. The existing `internal/adapters/template.Renderer` handles template
rendering. New templates are added to the embedded FS.

#### Application service

Add a new `InitProjectService` in `internal/app/scaffold/init_project.go`:

```go
// InitProjectService scaffolds a complete new project from language-specific templates.
type InitProjectService struct {
    renderer ports.TemplateRenderer
}

// NewInitProjectService creates a new InitProjectService.
func NewInitProjectService(renderer ports.TemplateRenderer) *InitProjectService {
    return &InitProjectService{renderer: renderer}
}

// InitProjectOptions carries options for full project scaffolding.
type InitProjectOptions struct {
    // ProjectName is the project/directory name.
    ProjectName string

    // ModulePath is the Go module path. When empty, defaults to ProjectName.
    ModulePath string

    // Port is the HTTP port. When zero, defaults to 3000.
    Port int

    // Language is the target language. Required.
    Language domainscaffold.Language

    // Force allows overwriting existing files.
    Force bool
}

// InitProject creates a new project directory with all scaffolded files.
// Returns an error wrapping os.ErrExist if the directory already exists and
// contains files (non-empty), unless Force is true.
func (s *InitProjectService) InitProject(ctx context.Context, parentDir string, opts InitProjectOptions) error
```

#### File layout

**New files to create:**

| Path | Purpose |
|------|---------|
| `internal/cli/cmd/init.go` | Replace placeholder with full implementation |
| `internal/cli/cmd/init_test.go` | Unit tests for init command |
| `internal/app/scaffold/init_project.go` | Application service for project scaffolding |
| `internal/app/scaffold/init_project_test.go` | Unit tests for InitProjectService |
| `internal/cli/templates/go/main.go.tmpl` | Go main.go template |
| `internal/cli/templates/go/go.mod.tmpl` | Go module template |
| `internal/cli/templates/go/dockerfile.tmpl` | Dockerfile template |
| `internal/cli/templates/go/gitignore.tmpl` | .gitignore for Go projects |
| `internal/cli/templates/go/vibewarden.yaml.tmpl` | vibewarden.yaml for init (differs from wrap) |
| `internal/cli/templates/go/claude.md.tmpl` | CLAUDE.md with vibew CLI reference |
| `internal/cli/templates/go/architect.md.tmpl` | .claude/agents/architect.md |
| `internal/cli/templates/go/dev.md.tmpl` | .claude/agents/dev.md |
| `internal/cli/templates/go/reviewer.md.tmpl` | .claude/agents/reviewer.md |
| `internal/cli/templates/go/embed.go` | embed.FS for Go templates |

**Modified files:**

| Path | Changes |
|------|---------|
| `internal/domain/scaffold/types.go` | Add `Language` type, `InitProjectData` struct |
| `internal/cli/templates/embed.go` | Update to include `go/` subdirectory templates |

#### Generated project structure

When a user runs `vibew init --lang go myproject`, the following directory structure is created:

```
myproject/
├── cmd/
│   └── myproject/
│       └── main.go
├── internal/
│   ├── domain/
│   │   └── .gitkeep
│   ├── ports/
│   │   └── .gitkeep
│   ├── adapters/
│   │   └── .gitkeep
│   └── app/
│       └── .gitkeep
├── .claude/
│   ├── CLAUDE.md
│   └── agents/
│       ├── architect.md
│       ├── dev.md
│       └── reviewer.md
├── go.mod
├── Dockerfile
├── vibewarden.yaml
├── vibew              (wrapper script)
├── vibew.ps1          (wrapper script)
├── vibew.cmd          (wrapper script)
└── .gitignore
```

#### Template content specifications

**1. main.go.tmpl — Minimal compilable HTTP server with /health**

```go
// Package main is the entry point for {{ .ProjectName }}.
package main

import (
    "encoding/json"
    "log/slog"
    "net/http"
    "os"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    slog.SetDefault(logger)

    mux := http.NewServeMux()
    mux.HandleFunc("GET /health", healthHandler)
    mux.HandleFunc("GET /", rootHandler)

    addr := ":{{ .Port }}"
    logger.Info("starting server", "addr", addr)
    if err := http.ListenAndServe(addr, mux); err != nil {
        logger.Error("server error", "error", err)
        os.Exit(1)
    }
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "service": "{{ .ProjectName }}",
        "message": "Hello from {{ .ProjectName }}! Edit cmd/{{ .ProjectName }}/main.go to get started.",
    })
}
```

**2. go.mod.tmpl**

```
module {{ .ModulePath }}

go 1.24
```

**3. dockerfile.tmpl**

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /{{ .ProjectName }} ./cmd/{{ .ProjectName }}

FROM gcr.io/distroless/static-debian12
COPY --from=builder /{{ .ProjectName }} /{{ .ProjectName }}
EXPOSE {{ .Port }}
ENTRYPOINT ["/{{ .ProjectName }}"]
```

**4. gitignore.tmpl — Go + VibeWarden**

```
# Binaries
/{{ .ProjectName }}
*.exe

# Go
/vendor/

# VibeWarden runtime
.vibewarden/

# IDE
.idea/
.vscode/
*.swp
```

**5. vibewarden.yaml.tmpl — Default config for init (rate limiting on, auth off)**

```yaml
# vibewarden.yaml — VibeWarden Security Sidecar Configuration
# Generated by `vibew init --lang go`. Edit as needed.
# Full reference: https://vibewarden.dev/docs/config

server:
  host: "127.0.0.1"
  port: 8443

upstream:
  host: "127.0.0.1"
  port: {{ .Port }}

app:
  build: "."

log:
  level: "info"
  format: "json"

security_headers:
  enabled: true
  hsts_max_age: 31536000
  hsts_include_subdomains: true
  hsts_preload: false
  content_type_nosniff: true
  frame_option: "SAMEORIGIN"
  content_security_policy: "default-src 'self'"
  referrer_policy: "strict-origin-when-cross-origin"
  permissions_policy: "geolocation=(), microphone=(), camera=()"

tls:
  enabled: true
  provider: "self-signed"

rate_limit:
  enabled: true
  trust_proxy_headers: false
  per_ip:
    requests_per_second: 10
    burst: 20
  exempt_paths:
    - "/health"
```

#### Agent template content (KEY DELIVERABLE)

The generated agent files enable AI agents to work autonomously with vibew. Each agent
has a specific role and must understand the vibew CLI and security architecture.

**6. claude.md.tmpl — CLAUDE.md (root project instructions)**

```markdown
# {{ .ProjectName }} — Project Instructions

## Architecture

This project follows hexagonal architecture (ports and adapters):

- `cmd/{{ .ProjectName }}/` — Main entry point
- `internal/domain/` — Domain logic, zero external dependencies
- `internal/ports/` — Interfaces (inbound and outbound)
- `internal/adapters/` — Implementations of ports
- `internal/app/` — Application services (use cases)

## Security — VibeWarden Sidecar

This project uses **VibeWarden** as its security sidecar. VibeWarden handles:

- TLS termination (self-signed in dev, Let's Encrypt in prod)
- Rate limiting (10 req/s per IP by default)
- Security headers (HSTS, CSP, X-Frame-Options, etc.)
- Authentication (when enabled via `vibew add auth`)

**Do not implement TLS, auth, rate limiting, or security headers in app code.**
The sidecar handles all security concerns. App code focuses on business logic only.

## Running the project

### Development

```bash
./vibew dev          # Start everything (app + sidecar + dependencies)
./vibew dev --watch  # Start with hot reload
./vibew status       # Check component health
./vibew logs         # Tail logs
./vibew doctor       # Diagnose common issues
```

The app runs on port {{ .Port }}, but access it through the sidecar at https://localhost:8443.

### Testing authenticated endpoints

When auth is enabled:

```bash
./vibew token                    # Generate a dev JWT
./vibew token --email user@test.com --role admin  # Custom claims
curl -H "Authorization: Bearer $(./vibew token)" https://localhost:8443/api/...
```

### Working with secrets

Never hardcode secrets in code. Use the vibew secret commands:

```bash
./vibew secret set MY_SECRET "value"   # Store a secret
./vibew secret get MY_SECRET           # Retrieve a secret
./vibew secret list                    # List all secrets
```

Access secrets in code via environment variables — vibew injects them at runtime.

## vibew CLI Reference

| Command | Description |
|---------|-------------|
| `./vibew dev` | Start dev environment (generates config, starts stack) |
| `./vibew dev --watch` | Start with file watching and hot reload |
| `./vibew status` | Show health of all components |
| `./vibew logs` | Tail logs from all services |
| `./vibew logs app` | Tail logs from app only |
| `./vibew doctor` | Diagnose common issues |
| `./vibew token` | Generate a dev JWT for testing |
| `./vibew secret get NAME` | Get a secret value |
| `./vibew secret set NAME VALUE` | Set a secret value |
| `./vibew secret list` | List all secrets |
| `./vibew cert export` | Export TLS certificate |
| `./vibew validate` | Validate vibewarden.yaml |
| `./vibew add auth` | Enable authentication |
| `./vibew add postgres` | Add PostgreSQL |
| `./vibew eject` | Eject to raw Docker Compose (advanced) |

## Adding features

Enable additional security features:

```bash
./vibew add auth       # Enable Ory Kratos authentication
./vibew add postgres   # Add PostgreSQL database
```

These modify `vibewarden.yaml` — review changes before committing.

## Code conventions

- Go standard formatting (`gofmt`, `goimports`)
- Error wrapping: `fmt.Errorf("context: %w", err)`
- No `panic` in library code
- Table-driven tests preferred
- Interfaces defined in `internal/ports/`, not next to implementations

## Dependencies

- Check license before adding (approved: Apache 2.0, MIT, BSD-2, BSD-3)
- Use latest stable versions unless there's a documented reason
```

**7. architect.md.tmpl — .claude/agents/architect.md**

```markdown
# Architect Agent Instructions

You are the software architect for {{ .ProjectName }}. You own technical correctness,
architectural consistency, and dependency decisions.

## Architecture principles

This project follows hexagonal architecture (ports and adapters) with DDD principles:

1. **Domain layer** (`internal/domain/`) has zero external dependencies
2. **Ports** (`internal/ports/`) define interfaces — never implementations
3. **Adapters** (`internal/adapters/`) implement ports
4. **Application services** (`internal/app/`) orchestrate use cases
5. No global state — dependency injection everywhere
6. Functional where Go allows — pure functions, immutable value objects

## Security architecture

**VibeWarden sidecar handles all security.** Design assuming:

- TLS termination is handled by the sidecar
- Authentication is handled by the sidecar (Ory Kratos)
- Rate limiting is handled by the sidecar
- Security headers are injected by the sidecar

**App code focuses on business logic only.** Never design custom:

- Auth middleware
- Rate limiters
- TLS configuration
- Security header middleware

If a feature requires auth, design it to read identity from request headers
(`X-User-Id`, `X-User-Email`, `X-User-Verified`) — the sidecar injects these.

## Design deliverables

When designing a feature, specify:

1. Domain model changes (entities, value objects, domain events)
2. Ports — new interfaces in `internal/ports/`
3. Adapters — which adapters implement those ports
4. Application service — the use case flow
5. File layout — exact paths for new files
6. Sequence — numbered request/response flow
7. Error cases — what can fail and how to handle it
8. Test strategy — unit vs integration, what to mock

## Dependency rules

Before adding any dependency:

1. Verify license is Apache 2.0, MIT, BSD-2, or BSD-3
2. Use `go list -m -json <module>` to check
3. Rejected licenses: GPL, AGPL, LGPL, CC-BY-SA, proprietary
4. Document the dependency, version, license, and reason in your design

## Running the project

Use vibew commands to run and test:

```bash
./vibew dev          # Start dev environment
./vibew doctor       # Diagnose issues
./vibew validate     # Validate config
```
```

**8. dev.md.tmpl — .claude/agents/dev.md**

```markdown
# Developer Agent Instructions

You are the developer for {{ .ProjectName }}. You implement features according to
the architect's design. You write clean, idiomatic Go code.

## Running the project

```bash
./vibew dev          # Start dev environment
./vibew dev --watch  # Start with hot reload
./vibew status       # Check component health
./vibew logs         # Tail logs
./vibew doctor       # Diagnose common issues (run this when something breaks)
```

## Testing

```bash
go test ./...                    # Run all tests
go test -v ./internal/domain/... # Run domain tests verbosely
go test -cover ./...             # With coverage
```

### Testing authenticated endpoints

```bash
./vibew token                    # Generate a dev JWT
./vibew token --email test@example.com --role admin  # Custom claims
curl -H "Authorization: Bearer $(./vibew token)" https://localhost:8443/api/...
```

### Working with secrets

Never hardcode secrets. Use vibew:

```bash
./vibew secret set DB_PASSWORD "secret123"
./vibew secret get DB_PASSWORD
```

## Go idioms to follow

1. **Error handling**: Always wrap errors with context
   ```go
   if err != nil {
       return fmt.Errorf("creating user: %w", err)
   }
   ```

2. **Formatting**: Run `gofmt` and `goimports` before committing

3. **Testing**: Use table-driven tests
   ```go
   tests := []struct {
       name    string
       input   string
       want    string
       wantErr bool
   }{
       {"valid input", "foo", "FOO", false},
       {"empty input", "", "", true},
   }
   for _, tt := range tests {
       t.Run(tt.name, func(t *testing.T) {
           got, err := Transform(tt.input)
           if (err != nil) != tt.wantErr {
               t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
           }
           if got != tt.want {
               t.Errorf("got %q, want %q", got, tt.want)
           }
       })
   }
   ```

4. **Interfaces**: Define in `internal/ports/`, not next to implementations

5. **No panics**: Return errors, never panic in library code

6. **Godoc**: Every exported type and function needs a doc comment

## Security — DO NOT IMPLEMENT

The VibeWarden sidecar handles security. Do not implement:

- Authentication middleware (sidecar handles it)
- Rate limiting (sidecar handles it)
- TLS configuration (sidecar handles it)
- Security headers (sidecar handles it)

When auth is enabled, read user identity from headers:

```go
userID := r.Header.Get("X-User-Id")
userEmail := r.Header.Get("X-User-Email")
isVerified := r.Header.Get("X-User-Verified") == "true"
```

## Adding dependencies

Check license before adding any dependency:

```bash
go list -m -json github.com/some/module | jq .License
```

Approved: Apache 2.0, MIT, BSD-2, BSD-3
Rejected: GPL, AGPL, LGPL, CC-BY-SA, proprietary

## Common tasks

### Add a new HTTP endpoint

1. Add handler function in appropriate package
2. Register route in `cmd/{{ .ProjectName }}/main.go`
3. Add test in `*_test.go` next to the handler
4. Run `./vibew dev` to test

### Add a new domain entity

1. Create file in `internal/domain/`
2. Define as value object (immutable) or entity (with ID)
3. Add unit tests in `internal/domain/*_test.go`
4. Domain layer must have zero external imports
```

**9. reviewer.md.tmpl — .claude/agents/reviewer.md**

```markdown
# Reviewer Agent Instructions

You are the code reviewer for {{ .ProjectName }}. You ensure code quality,
architectural consistency, and security.

## Review checklist

### Architecture

- [ ] Domain layer (`internal/domain/`) has zero external dependencies
- [ ] Interfaces defined in `internal/ports/`, not next to implementations
- [ ] Adapters implement port interfaces
- [ ] No global state — dependency injection used
- [ ] Application services in `internal/app/` orchestrate use cases

### Code quality

- [ ] Error handling: errors wrapped with context (`fmt.Errorf("context: %w", err)`)
- [ ] No panics in library code
- [ ] Table-driven tests where appropriate
- [ ] Every exported type/function has godoc comment
- [ ] `gofmt` and `goimports` applied

### Security — REJECT if violated

**Reject PRs that implement security features handled by vibew:**

- [ ] No custom auth middleware (sidecar handles authentication)
- [ ] No custom rate limiter (sidecar handles rate limiting)
- [ ] No TLS configuration in app code (sidecar handles TLS)
- [ ] No security header middleware (sidecar injects headers)
- [ ] No hardcoded secrets (use `./vibew secret get`)

If auth is needed, verify the code reads identity from headers:
- `X-User-Id` — user identifier
- `X-User-Email` — user email
- `X-User-Verified` — email verification status

### Dependencies

- [ ] New dependencies have approved license (Apache 2.0, MIT, BSD-2, BSD-3)
- [ ] No GPL, AGPL, LGPL, CC-BY-SA, or proprietary dependencies
- [ ] Latest stable versions used (unless documented reason)

### Testing

- [ ] Unit tests for domain logic
- [ ] Integration tests for adapters (use testcontainers-go)
- [ ] Tests live next to the code they test (`foo_test.go`)
- [ ] Tests pass: `go test ./...`

## Running verification

```bash
go test ./...        # Run tests
go vet ./...         # Static analysis
gofmt -d .           # Check formatting
./vibew validate     # Validate vibewarden.yaml
./vibew dev          # Start and manually verify
./vibew doctor       # Check for issues
```

## Common rejection reasons

1. **Reimplementing sidecar features**: "This rate limiter duplicates what vibew provides.
   Remove this middleware and configure rate limiting in vibewarden.yaml instead."

2. **Hardcoded secrets**: "DB_PASSWORD is hardcoded. Use `./vibew secret set DB_PASSWORD value`
   and read from environment variable at runtime."

3. **Domain with external deps**: "internal/domain/user.go imports database/sql. Domain
   layer must have zero external dependencies. Move DB logic to an adapter."

4. **Missing error context**: "Error returned without context. Wrap with:
   `fmt.Errorf(\"creating user: %w\", err)`"
```

#### Sequence

1. User runs `vibew init --lang go myproject`
2. CLI parses flags (`--lang go`, project name `myproject`)
3. CLI validates: `--lang` is required, project name defaults to current dir name
4. CLI checks target directory: if `myproject/` exists and is non-empty, error (unless `--force`)
5. CLI creates `myproject/` directory
6. CLI instantiates `InitProjectService` with template renderer
7. Service iterates through Go language pack templates, rendering each to target paths
8. Service calls existing wrapper generation (vibew, vibew.ps1, etc.)
9. CLI prints success message with next steps
10. User runs `./vibew dev` and has a working secure stack

#### Error cases

| Error | Handling |
|-------|----------|
| `--lang` not specified | Return error: "flag --lang is required (supported: go)" |
| Unknown language | Return error: "unsupported language \"X\" (supported: go)" |
| Directory exists and non-empty | Return error wrapping `os.ErrExist` with hint to use `--force` |
| Template rendering failure | Return wrapped error with template name |
| File write permission denied | Return wrapped OS error |
| Invalid project name (contains `/`, etc.) | Return validation error |

#### Test strategy

**Unit tests (`internal/cli/cmd/init_test.go`):**

| Test | Verification |
|------|--------------|
| `TestInitCmd_RequiresLang` | Error when `--lang` is missing |
| `TestInitCmd_RejectsUnknownLang` | Error when `--lang` is not `go` |
| `TestInitCmd_CreatesProjectDir` | Creates directory with correct name |
| `TestInitCmd_GeneratesAllFiles` | All expected files exist after init |
| `TestInitCmd_MainGoCompiles` | Generated main.go compiles with `go build` |
| `TestInitCmd_ErrorsOnNonEmptyDir` | Error when target dir has files |
| `TestInitCmd_ForceOverwrites` | `--force` allows overwriting |
| `TestInitCmd_DefaultsProjectName` | Uses current dir name when no arg |
| `TestInitCmd_CustomModulePath` | `--module` flag sets go.mod path |
| `TestInitCmd_CustomPort` | `--port` flag sets port in templates |

**Unit tests (`internal/app/scaffold/init_project_test.go`):**

| Test | Verification |
|------|--------------|
| `TestInitProject_CreatesStructure` | All directories created |
| `TestInitProject_RendersTemplates` | Template variables substituted |
| `TestInitProject_SetsExecutableBit` | vibew script is executable |
| `TestInitProject_RespectsForce` | Force flag behavior |

**Integration test:**

Add to `test/quickstart/test.sh`:

```bash
# Test init command creates working project
./vibewarden init --lang go testproject
cd testproject
go build ./cmd/testproject
./vibew validate
```

**What to mock vs. real:**

- Mock: filesystem for unit tests (use `t.TempDir()`)
- Real: actual file operations for integration tests
- Real: `go build` to verify generated code compiles

#### New dependencies

None. This implementation uses only existing dependencies:

- `github.com/spf13/cobra` (Apache 2.0) — already in use for CLI
- `embed` (stdlib) — already in use for templates
- `text/template` (stdlib) — already in use for rendering

### Consequences

**Positive:**

- Vibe coders can start a new Go project with one command
- Generated agents are fully vibew-aware — AI can operate autonomously
- Hexagonal architecture is enforced from project creation
- Security best practices are embedded in agent instructions

**Negative:**

- Larger binary size (embedded templates)
- Go-specific templates need maintenance as Go evolves
- Agent templates may need updates as vibew CLI evolves

**Trade-offs:**

- **Hardcoded templates vs. external files**: Hardcoded (embedded) was chosen because
  it matches the distribution model (single binary) and avoids runtime file resolution.
  Template abstraction (#603) will improve maintainability later.

- **Minimal main.go vs. feature-rich**: Minimal was chosen to reduce confusion.
  Users add features incrementally. A complex starter would overwhelm vibe coders.

**Future work:**

- #603: Abstract template loading for language packs
- #604: Kotlin language pack
- #605: TypeScript language pack
- #606: Language auto-detection (infer from existing files)
- #607: Interactive project description prompt

---
