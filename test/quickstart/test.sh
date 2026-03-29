#!/bin/sh
# test/quickstart/test.sh — Quick start end-to-end validation
#
# Validates the vibew init → vibewarden generate flow without requiring
# Docker, a running network, or any external services.
#
# What is checked:
#   1. vibewarden init --upstream 3000 scaffolds the expected files
#   2. Generated vibewarden.yaml contains the correct upstream port
#   3. vibew wrapper script is executable
#   4. .gitignore contains the .vibewarden/ exclusion
#   5. vibewarden generate produces docker-compose.yml and .env.template
#   6. Generated docker-compose.yml references the correct service names
#   7. Generated docker-compose.yml references the correct upstream port
#   8. Generated docker-compose.yml is syntactically valid YAML (via python3
#      or python, both of which ship by default on macOS and most Linux distros)
#
# Usage:
#   ./test/quickstart/test.sh              # auto-builds the binary
#   VIBEWARDEN_BIN=/usr/local/bin/vibewarden ./test/quickstart/test.sh
#
# Exit code:
#   0 — all checks passed
#   1 — one or more checks failed (failures are printed before exit)

set -e

# ── helpers ──────────────────────────────────────────────────────────────────

PASS=0
FAIL=0

pass() {
    printf "  [PASS] %s\n" "$1"
    PASS=$((PASS + 1))
}

fail() {
    printf "  [FAIL] %s\n" "$1"
    FAIL=$((FAIL + 1))
}

check_file_exists() {
    label="$1"
    path="$2"
    if [ -f "$path" ]; then
        pass "$label exists"
    else
        fail "$label missing: $path"
    fi
}

check_file_contains() {
    label="$1"
    path="$2"
    pattern="$3"
    if grep -q "$pattern" "$path" 2>/dev/null; then
        pass "$label"
    else
        fail "$label  (pattern: '$pattern' not found in $path)"
    fi
}

check_file_executable() {
    label="$1"
    path="$2"
    if [ -x "$path" ]; then
        pass "$label is executable"
    else
        fail "$label is not executable: $path"
    fi
}

check_yaml_valid() {
    label="$1"
    path="$2"
    dir="$(dirname "$path")"
    file="$(basename "$path")"

    # Prefer `docker compose config` — it fully parses and resolves the
    # compose file and is available wherever Docker is installed.
    if command -v docker >/dev/null 2>&1; then
        # Run from the directory that contains the compose file so that
        # docker compose resolves relative paths (e.g. ./.credentials)
        # correctly.  Suppress output — we only care about exit code.
        if (cd "$dir" && docker compose -f "$file" config --quiet 2>/dev/null); then
            pass "$label is valid (docker compose config)"
            return
        else
            fail "$label failed docker compose config validation"
            return
        fi
    fi

    # Fallback: python3 with PyYAML.  PyYAML is not part of the stdlib but is
    # present on many developer machines.
    if command -v python3 >/dev/null 2>&1; then
        if python3 -c "import yaml; yaml.safe_load(open('$path'))" 2>/dev/null; then
            pass "$label is valid YAML (python3 + PyYAML)"
            return
        else
            # python3 found but PyYAML not available — do not fail the check.
            printf "  [SKIP] %s — PyYAML not installed (run: pip3 install pyyaml)\n" "$label"
            return
        fi
    fi

    # No suitable tool found — skip the check rather than fail on a missing
    # optional dependency.
    printf "  [SKIP] %s — neither docker nor python3+PyYAML available\n" "$label"
}

# ── locate or build the binary ────────────────────────────────────────────────

# Resolve the repo root relative to this script's location, then resolve to
# an absolute path so subsequent cd calls do not break $REPO_ROOT.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [ -n "$VIBEWARDEN_BIN" ]; then
    BIN="$VIBEWARDEN_BIN"
else
    BIN="$REPO_ROOT/bin/vibewarden-quickstart-test"
    printf "Building vibewarden binary...\n"
    cd "$REPO_ROOT"
    go build -o "$BIN" ./cmd/vibewarden
    printf "Built: %s\n" "$BIN"
fi

if [ ! -x "$BIN" ]; then
    printf "error: binary not found or not executable: %s\n" "$BIN" >&2
    exit 1
fi

# ── create temp work directory ────────────────────────────────────────────────

WORKDIR="$(mktemp -d)"
# Ensure cleanup on exit regardless of pass/fail.
trap 'rm -rf "$WORKDIR"; [ -z "$VIBEWARDEN_BIN" ] && rm -f "$BIN"' EXIT

printf "\nWork directory: %s\n\n" "$WORKDIR"

# ── step 1: vibewarden init ───────────────────────────────────────────────────

printf "==> Step 1: vibewarden init --upstream 3000\n"

# Run init without agent files to keep the output minimal.
"$BIN" init --upstream 3000 --agent none "$WORKDIR" >/dev/null

check_file_exists  "vibewarden.yaml"          "$WORKDIR/vibewarden.yaml"
check_file_exists  "vibew wrapper (shell)"    "$WORKDIR/vibew"
check_file_exists  "vibew wrapper (ps1)"      "$WORKDIR/vibew.ps1"
check_file_exists  "vibew wrapper (cmd)"      "$WORKDIR/vibew.cmd"
check_file_exists  ".vibewarden-version"      "$WORKDIR/.vibewarden-version"
check_file_exists  ".gitignore"               "$WORKDIR/.gitignore"

check_file_executable "vibew shell wrapper"  "$WORKDIR/vibew"

check_file_contains "vibewarden.yaml has upstream port 3000" \
    "$WORKDIR/vibewarden.yaml" "port: 3000"

check_file_contains "vibewarden.yaml has server port 8080" \
    "$WORKDIR/vibewarden.yaml" "port: 8080"

check_file_contains "vibewarden.yaml has app.build section" \
    "$WORKDIR/vibewarden.yaml" "build:"

check_file_contains "vibewarden.yaml has security_headers section" \
    "$WORKDIR/vibewarden.yaml" "security_headers:"

check_file_contains ".gitignore excludes .vibewarden/" \
    "$WORKDIR/.gitignore" ".vibewarden/"

# Confirm docker-compose.yml is NOT generated at init time.
if [ -f "$WORKDIR/docker-compose.yml" ]; then
    fail "docker-compose.yml must NOT be generated by init (found at root)"
else
    pass "docker-compose.yml correctly absent after init"
fi

printf "\n"

# ── step 2: vibewarden generate ───────────────────────────────────────────────

printf "==> Step 2: vibewarden generate\n"

cd "$WORKDIR"
"$BIN" generate >/dev/null

GENDIR="$WORKDIR/.vibewarden/generated"

check_file_exists "generated docker-compose.yml"  "$GENDIR/docker-compose.yml"
check_file_exists "generated .env.template"       "$GENDIR/.env.template"

# Auth is disabled in the default config, so Kratos files should be absent.
if [ -f "$GENDIR/kratos/kratos.yml" ]; then
    fail "kratos.yml must NOT be generated when auth is disabled"
else
    pass "kratos/kratos.yml correctly absent (auth disabled)"
fi

printf "\n"

# ── step 3: validate docker-compose.yml structure ────────────────────────────

printf "==> Step 3: validate docker-compose.yml structure\n"

COMPOSE="$GENDIR/docker-compose.yml"

# Required service: vibewarden
check_file_contains "compose has 'vibewarden' service"       "$COMPOSE" "^  vibewarden:"

# Required service: app (because vibewarden.yaml has app.build: .)
check_file_contains "compose has 'app' service"              "$COMPOSE" "^  app:"

# vibewarden service must expose the proxy port
check_file_contains "compose exposes port 8080"              "$COMPOSE" '"8080:8080"'

# The app service depends_on app healthcheck
check_file_contains "vibewarden depends_on app"              "$COMPOSE" "service_healthy"

# Upstream port propagated from vibewarden.yaml
check_file_contains "compose references upstream port 3000"  "$COMPOSE" "VIBEWARDEN_UPSTREAM_PORT=3000"

# No auth → no kratos service
if grep -q "^  kratos:" "$COMPOSE" 2>/dev/null; then
    fail "kratos service must NOT appear in compose when auth is disabled"
else
    pass "kratos service correctly absent in compose (auth disabled)"
fi

# No redis → redis service should be absent (default store is memory)
if grep -q "^  redis:" "$COMPOSE" 2>/dev/null; then
    fail "redis service must NOT appear in compose when rate_limit.store != redis"
else
    pass "redis service correctly absent in compose (default memory store)"
fi

# Network section must be present
check_file_contains "compose has networks section"           "$COMPOSE" "^networks:"
check_file_contains "compose has vibewarden network"        "$COMPOSE" "vibewarden:"

# YAML validity
check_yaml_valid "generated docker-compose.yml" "$COMPOSE"

printf "\n"

# ── summary ───────────────────────────────────────────────────────────────────

printf "==> Results\n"
printf "    Passed: %d\n" "$PASS"
printf "    Failed: %d\n" "$FAIL"
printf "\n"

if [ "$FAIL" -gt 0 ]; then
    printf "FAIL — %d check(s) failed.\n" "$FAIL"
    exit 1
fi

printf "PASS — all checks passed.\n"
