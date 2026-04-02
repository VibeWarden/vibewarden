#!/usr/bin/env sh
# test.sh — Egress proxy integration tests using local httpbin.
#
# Usage:
#   cd test/egress
#   docker compose up -d
#   ./test.sh
#   docker compose down
#
# All requests go through: app → vibewarden:8081 (egress) → httpbin (local)
# No internet access required.

set -eu

PASS=0
FAIL=0
TOTAL=0

check() {
    TOTAL=$((TOTAL + 1))
    desc="$1"
    shift
    if "$@" > /dev/null 2>&1; then
        echo "  [PASS] $desc"
        PASS=$((PASS + 1))
    else
        echo "  [FAIL] $desc"
        FAIL=$((FAIL + 1))
    fi
}

check_output() {
    TOTAL=$((TOTAL + 1))
    desc="$1"
    expected="$2"
    actual="$3"
    if echo "$actual" | grep -q "$expected"; then
        echo "  [PASS] $desc"
        PASS=$((PASS + 1))
    else
        echo "  [FAIL] $desc (expected '$expected' in output)"
        FAIL=$((FAIL + 1))
    fi
}

# Wait for vibewarden egress to be ready
echo "Waiting for egress proxy..."
for i in $(seq 1 30); do
    if docker exec egress-app-1 wget -qO- http://vibewarden:8081/ 2>/dev/null | grep -q "denied\|Forbidden" 2>/dev/null; then
        break
    fi
    sleep 1
done

echo ""
echo "=== Egress Proxy Tests ==="
echo ""

# --- Transparent mode (X-Egress-URL header) ---
echo "--- Transparent mode ---"

RESULT=$(docker exec egress-app-1 wget -qO- \
    --header="X-Egress-URL: http://httpbin/get" \
    http://vibewarden:8081/ 2>&1 || true)
check_output "GET via transparent mode returns httpbin response" '"url": "http://httpbin/get"' "$RESULT"

# --- Named route ---
echo "--- Named routes ---"

RESULT=$(docker exec egress-app-1 wget -qO- \
    http://vibewarden:8081/_egress/httpbin-get/get 2>&1 || true)
check_output "GET via named route" '"url"' "$RESULT"

# --- POST forwarding ---
echo "--- POST forwarding ---"

RESULT=$(docker exec egress-app-1 wget -qO- \
    --header="X-Egress-URL: http://httpbin/post" \
    --header="Content-Type: application/json" \
    --post-data='{"hello":"world"}' \
    http://vibewarden:8081/ 2>&1 || true)
check_output "POST body forwarded correctly" '"hello": "world"' "$RESULT"

# --- Default deny ---
echo "--- Default deny ---"

RESULT=$(docker exec egress-app-1 wget -S \
    --header="X-Egress-URL: http://evil.com/steal" \
    http://vibewarden:8081/ 2>&1 || true)
check_output "Unlisted URL returns 403" "403 Forbidden" "$RESULT"

# --- Method enforcement (#583) ---
echo "--- Method enforcement ---"

# httpbin-get route only allows GET; a POST should not match and fall to deny.
RESULT=$(docker exec egress-app-1 wget -S \
    --header="X-Egress-URL: http://httpbin/get" \
    --header="Content-Type: application/json" \
    --post-data='{}' \
    http://vibewarden:8081/ 2>&1 || true)
check_output "POST to GET-only route returns 403" "403 Forbidden" "$RESULT"

# httpbin-headers route only allows GET; the route should match for GET.
RESULT=$(docker exec egress-app-1 wget -qO- \
    --header="X-Egress-URL: http://httpbin/headers" \
    http://vibewarden:8081/ 2>&1 || true)
check_output "GET to headers route succeeds" '"headers"' "$RESULT"

# --- Header injection (#583) ---
echo "--- Header injection ---"

# httpbin-headers route injects X-Injected-By: vibewarden-egress.
# httpbin /headers echoes the request headers in the response body.
RESULT=$(docker exec egress-app-1 wget -qO- \
    --header="X-Egress-URL: http://httpbin/headers" \
    http://vibewarden:8081/ 2>&1 || true)
check_output "Injected header appears in upstream request" "vibewarden-egress" "$RESULT"

# --- Header stripping (#583) ---
echo "--- Header stripping ---"

# httpbin-headers route strips Cookie; Cookie should not appear upstream.
RESULT=$(docker exec egress-app-1 wget -qO- \
    --header="X-Egress-URL: http://httpbin/headers" \
    --header="Cookie: session=abc123" \
    http://vibewarden:8081/ 2>&1 || true)
# httpbin echoes headers; Cookie must not appear in the response body.
if echo "$RESULT" | grep -qi '"Cookie"'; then
    echo "  [FAIL] Cookie header should have been stripped before forwarding"
    FAIL=$((FAIL + 1))
else
    echo "  [PASS] Cookie header stripped before forwarding"
    PASS=$((PASS + 1))
fi
TOTAL=$((TOTAL + 1))

# --- Summary ---
echo ""
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
