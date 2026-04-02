# Quick start end-to-end validation

This directory contains the automated validation script for the
`vibew wrap → vibewarden generate` quick start flow (issue #422).

## Purpose

The quick start flow is the primary user-facing experience for VibeWarden.
This script validates the entire flow without requiring Docker, a network
connection, or any external services.  It exercises:

1. `vibewarden wrap --upstream 3000` — scaffold vibewarden.yaml and wrappers
2. `vibewarden generate` — generate runtime config from vibewarden.yaml
3. Structural validation of the generated docker-compose.yml

## Running the test

From the repository root:

```sh
./test/quickstart/test.sh
```

The script builds a fresh binary automatically.  To skip the build step and
use a pre-installed binary:

```sh
VIBEWARDEN_BIN=/usr/local/bin/vibewarden ./test/quickstart/test.sh
```

Exit code is `0` on success and `1` if any check fails.

## Checks performed

### Step 1: vibewarden wrap

| Check | Expected result |
|---|---|
| vibewarden.yaml exists | present |
| vibew (shell) exists | present |
| vibew.ps1 exists | present |
| vibew.cmd exists | present |
| .vibewarden-version exists | present |
| .gitignore exists | present |
| vibew shell wrapper is executable | +x permission set |
| vibewarden.yaml contains upstream port 3000 | `port: 3000` present |
| vibewarden.yaml contains server port 8080 | `port: 8080` present |
| vibewarden.yaml contains app.build section | `build:` present |
| vibewarden.yaml contains security_headers section | `security_headers:` present |
| .gitignore excludes .vibewarden/ | `.vibewarden/` entry present |
| docker-compose.yml NOT generated at wrap time | file absent at project root |

### Step 2: vibewarden generate

| Check | Expected result |
|---|---|
| .vibewarden/generated/docker-compose.yml exists | present |
| .vibewarden/generated/.env.template exists | present |
| kratos/kratos.yml NOT generated (auth disabled) | file absent |

### Step 3: docker-compose.yml structure

| Check | Expected result |
|---|---|
| vibewarden service present | `vibewarden:` in services block |
| app service present (app.build is set) | `app:` in services block |
| proxy port 8080 exposed | `"8080:8080"` in ports |
| vibewarden depends_on app with healthcheck | `service_healthy` condition |
| upstream port 3000 propagated via env var | `VIBEWARDEN_UPSTREAM_PORT=3000` |
| kratos service absent (auth disabled) | no `kratos:` service |
| redis service absent (default memory store) | no `redis:` service |
| networks section present | `networks:` block present |
| vibewarden network defined | `vibewarden:` under networks |
| docker-compose.yml is valid YAML | python3 parse succeeds |

## vibew dev flow (manual validation)

The `vibew dev` command wraps `vibewarden generate` + `docker compose up`.
It requires Docker to be running and is not automated here because it pulls
images and starts containers.  To validate manually:

```sh
# In a fresh directory
vibewarden wrap --upstream 3000
./vibew dev
```

Expected outcome:

- VibeWarden proxy starts on http://localhost:8080
- `/_vibewarden/health` returns `200 OK`
- Security headers are present on all responses (`X-Content-Type-Options`,
  `X-Frame-Options`, `Referrer-Policy`)
- Rate limiting returns `429 Too Many Requests` when the burst is exceeded

To stop the environment:

```sh
docker compose -f .vibewarden/generated/docker-compose.yml down
```

## Known issues found during validation

None at time of writing (issue #422).  Any issues discovered during manual
`vibew dev` testing should be documented here and tracked as follow-up issues.

## Test coverage

This script is a black-box integration test that complements the unit and
integration tests in `internal/`.  It is the canonical smoke test to run
before any release that touches the `wrap` or `generate` commands.
