# VibeWarden End-to-End Integration Tests

This package contains end-to-end integration tests for VibeWarden.  The tests
use [testcontainers-go](https://golang.testcontainers.org/) to spin up a real
two-container stack and exercise the full HTTP request path.

## What is tested

| Test | What it verifies |
|---|---|
| `health_endpoint` | `/_vibewarden/health` returns `200 OK` |
| `proxy_passthrough` | A plain `GET /` is proxied to the upstream echo server and returns `200` |
| `security_headers` | `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`, and `Referrer-Policy` are present in every proxied response |
| `metrics_endpoint` | `/_vibewarden/metrics` returns `200` with a non-empty Prometheus plain-text body |
| `rate_limiting` | Rapid-fire requests from the same IP eventually receive `429 Too Many Requests` |

### What is NOT tested here

Kratos (authentication flows) and OpenBao (secret management) are intentionally
excluded from this suite — they require extra containers and significant startup
time.  Those integrations are exercised manually via the demo stack
(`make demo`) and their own adapter-level integration tests.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│  test host                                               │
│                                                          │
│   go test  ──────►  http://localhost:<mapped-port>       │
│                              │                           │
│   ┌──────────────────────────▼──────────────────────┐   │
│   │          Docker bridge network (e2e-*)           │   │
│   │                                                  │   │
│   │  ┌─────────────────┐    ┌─────────────────────┐ │   │
│   │  │  vibewarden:8080│───►│  upstream:3000      │ │   │
│   │  │  (project image)│    │  (python echo HTTP) │ │   │
│   │  └─────────────────┘    └─────────────────────┘ │   │
│   └──────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────┘
```

The upstream server is a Python one-liner HTTP server that returns
`{"ok":true}` for every `GET` and `POST` request.

VibeWarden is built from the project's `Dockerfile` (multi-stage Go build) the
first time the suite runs.  Subsequent runs reuse the cached Docker layer
(`KeepImage: true`) so only changed layers are rebuilt.

## Prerequisites

- Docker daemon running and accessible (Docker Desktop, Rancher Desktop, Colima, etc.)
- The Docker socket is mounted or accessible to the process running `go test`
- Go 1.26+

## Running the tests

```bash
# From the repository root
go test -tags integration -v -timeout 5m ./test/e2e/
```

The `-timeout 5m` flag is required.  Without it Go's default 10-minute test
timeout applies, but the suite enforces its own 5-minute deadline via
`context.WithTimeout`.

### Verbose output with timestamps

```bash
go test -tags integration -v -timeout 5m -count=1 ./test/e2e/ 2>&1 | ts
```

(`ts` is provided by `moreutils` on most Linux distros and via Homebrew on macOS.)

### Skipping in normal CI

The `//go:build integration` build tag ensures the tests are excluded from
`go test ./...` (used in `make check` and normal PR CI).  They should be run
in a separate release-gate CI step that has Docker available:

```yaml
# Example GitHub Actions step
- name: Run e2e tests
  run: go test -tags integration -v -timeout 5m ./test/e2e/
```

## Troubleshooting

**Container image build fails** — make sure Docker is running and the project
root is accessible.  The `Dockerfile` at the repository root performs a
multi-stage Go build; all source files must be present.

**Tests time out during container startup** — increase the `-timeout` flag or
check Docker resource limits (memory, CPU).  The first run pulls the Python
Alpine image and builds the VibeWarden image from scratch.

**`429` test never triggers** — the upstream echo server must be reachable
from the VibeWarden container.  Check that both containers joined the same
ephemeral bridge network (logged by testcontainers at `INFO` level).

**Port conflicts** — the tests use dynamically assigned host ports, so
conflicts should not occur.  If they do, check for lingering containers from a
previous interrupted run: `docker ps -a | grep vibewarden-e2e`.
