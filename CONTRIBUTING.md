# Contributing to VibeWarden

## Prerequisites

- Go 1.26+ ([download](https://go.dev/dl/))
- Docker and Docker Compose
- Git

## Getting Started

```bash
git clone https://github.com/vibewarden/vibewarden.git
cd vibewarden
make setup-hooks   # install pre-push quality gate (opt-in)
```

## Building

```bash
go build ./...
```

The demo app has its own module:

```bash
cd examples/demo-app && go build ./...
```

## Running Tests

```bash
# Unit tests (fast)
go test -race ./...

# Integration tests (requires Docker)
go test -race -tags integration ./...
```

## Quality Checks

Run all checks before submitting a PR:

```bash
make check
```

This runs across both the main module and `examples/demo-app`:

1. **gofmt** — formatting (fails if any file needs formatting)
2. **go vet** — static analysis
3. **go build** — compilation
4. **go test -race** — tests with race detector

If you installed the pre-push hook (`make setup-hooks`), this runs
automatically before every `git push`. Skip with `git push --no-verify`
if needed.

## Code Style

- **Formatting**: `gofmt` (enforced by `make check`)
- **Error handling**: always wrap with context — `fmt.Errorf("doing X: %w", err)`
- **No panics** in library code — only in `main()` for unrecoverable startup failures
- **No swallowed errors** — every error must be returned, logged, or explicitly handled
- **Godoc**: every exported type and function must have a comment
- **Tests**: table-driven, tests live next to the code (`foo_test.go`)

## Architecture

VibeWarden follows **hexagonal architecture** (ports and adapters) with **DDD**:

```
internal/
  domain/       Zero external dependencies. Entities, value objects, events.
  ports/        Interfaces only. No implementations.
  adapters/     Implements port interfaces. All I/O lives here.
  app/          Application services. Orchestrates domain + ports.
  middleware/   HTTP middleware. Depends on ports, not adapters.
  config/       Configuration loading and validation.
```

**Dependency rule**: inner layers never import outer layers.

```
domain ← ports ← app ← adapters
                      ← middleware
                      ← cli
```

## Adding a Dependency

Before adding any dependency:

1. **Check the license** — approved: Apache 2.0, MIT, BSD-2, BSD-3
2. **Rejected**: GPL, AGPL, LGPL, CC-BY-SA, proprietary
3. **Prefer stdlib** — only add external deps when stdlib is insufficient
4. If unsure, open an issue to discuss

## Branch and Commit Conventions

- **Branches**: `feat/<issue>-<slug>`, `fix/<issue>-<slug>`
- **Commits**: [conventional commits](https://www.conventionalcommits.org/) — `feat:`, `fix:`, `chore:`, `docs:`, `test:`
- **PRs**: target `main`, title follows conventional commit style

## Development Workflow

```bash
# Create a feature branch
git checkout -b feat/123-my-feature

# Make changes, then check
make check

# Commit and push
git add <files>
git commit -m "feat(#123): short description"
git push -u origin feat/123-my-feature

# Open a PR targeting main
gh pr create --base main
```

## Running the Demo App

```bash
cd examples/demo-app
docker compose up -d
# Visit http://localhost:8080
```

See [`examples/demo-app/README.md`](examples/demo-app/README.md) for details.

## Running the Observability Stack

```bash
make observability-up     # Prometheus + Grafana
make grafana-open         # Open dashboard
make observability-down   # Tear down
```

See [`docs/observability.md`](docs/observability.md) for details.

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make check` | Run all quality checks |
| `make setup-hooks` | Install git pre-push hook |
| `make observability-up` | Start Prometheus + Grafana |
| `make observability-down` | Stop Prometheus + Grafana |
| `make grafana-open` | Open Grafana in browser |
| `make prometheus-open` | Open Prometheus in browser |
