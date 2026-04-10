#!/bin/sh
# vibew_test.sh — unit tests for vibew wrapper script URL construction logic.
#
# Validates that the wrapper constructs correct GoReleaser-compatible archive
# names and download URLs. Run with: sh scripts/wrapper/vibew_test.sh
set -e

PASS=0
FAIL=0

assert_eq() {
    label="$1"
    expected="$2"
    actual="$3"
    if [ "${expected}" = "${actual}" ]; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        echo "FAIL: ${label}" >&2
        echo "  expected: ${expected}" >&2
        echo "  actual:   ${actual}" >&2
    fi
}

REPO="vibewarden/vibewarden"

test_version_prefix_stripping() {
    VERSION="v1.0.0"
    CLEAN_VERSION="${VERSION#v}"
    assert_eq "strip v prefix from v1.0.0" "1.0.0" "${CLEAN_VERSION}"

    VERSION="v2.3.4-beta.1"
    CLEAN_VERSION="${VERSION#v}"
    assert_eq "strip v prefix from v2.3.4-beta.1" "2.3.4-beta.1" "${CLEAN_VERSION}"

    VERSION="1.0.0"
    CLEAN_VERSION="${VERSION#v}"
    assert_eq "no-op when no v prefix" "1.0.0" "${CLEAN_VERSION}"
}

test_archive_name_construction() {
    VERSION="v1.0.0"
    CLEAN_VERSION="${VERSION#v}"
    OS="linux"
    ARCH="amd64"
    ARCHIVE_NAME="vibewarden_${CLEAN_VERSION}_${OS}_${ARCH}.tar.gz"
    assert_eq "linux amd64 archive name" "vibewarden_1.0.0_linux_amd64.tar.gz" "${ARCHIVE_NAME}"

    OS="darwin"
    ARCH="arm64"
    ARCHIVE_NAME="vibewarden_${CLEAN_VERSION}_${OS}_${ARCH}.tar.gz"
    assert_eq "darwin arm64 archive name" "vibewarden_1.0.0_darwin_arm64.tar.gz" "${ARCHIVE_NAME}"
}

test_download_urls() {
    VERSION="v1.0.0"
    CLEAN_VERSION="${VERSION#v}"
    OS="linux"
    ARCH="amd64"
    ARCHIVE_NAME="vibewarden_${CLEAN_VERSION}_${OS}_${ARCH}.tar.gz"
    BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
    ARCHIVE_URL="${BASE_URL}/${ARCHIVE_NAME}"
    CHECKSUMS_URL="${BASE_URL}/checksums.txt"

    assert_eq "archive URL" \
        "https://github.com/vibewarden/vibewarden/releases/download/v1.0.0/vibewarden_1.0.0_linux_amd64.tar.gz" \
        "${ARCHIVE_URL}"

    assert_eq "checksums URL" \
        "https://github.com/vibewarden/vibewarden/releases/download/v1.0.0/checksums.txt" \
        "${CHECKSUMS_URL}"
}

test_cached_binary_name() {
    VERSION="v1.0.0"
    CACHE_DIR="${HOME}/.vibewarden/bin"
    CACHED_BIN="${CACHE_DIR}/vibewarden-${VERSION}"
    assert_eq "cached binary path" "${HOME}/.vibewarden/bin/vibewarden-v1.0.0" "${CACHED_BIN}"
}

test_version_prefix_stripping
test_archive_name_construction
test_download_urls
test_cached_binary_name

echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
if [ "${FAIL}" -gt 0 ]; then
    exit 1
fi
