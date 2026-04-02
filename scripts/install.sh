#!/bin/sh
# install.sh — Download and install the VibeWarden CLI.
#
# Usage:
#   curl -sS https://vibewarden.dev/install.sh | sh
#
# Options (via env vars):
#   VERSION=v0.1.0    Install a specific version (default: latest)
#   INSTALL_DIR=./    Where to put the binary (default: /usr/local/bin or ./)
#
# The script:
#   1. Detects OS and architecture
#   2. Downloads the vibewarden binary from GitHub Releases
#   3. Verifies the SHA-256 checksum
#   4. Installs to INSTALL_DIR
#
# Requires: curl or wget, sha256sum or shasum

set -eu

REPO="vibewarden/vibewarden"
BINARY_NAME="vibew"

# --- Colors (if terminal supports them) ---
RED=""
GREEN=""
CYAN=""
RESET=""
if [ -t 1 ]; then
    RED="\033[0;31m"
    GREEN="\033[0;32m"
    CYAN="\033[0;36m"
    RESET="\033[0m"
fi

log()   { printf "${CYAN}[vibewarden]${RESET} %s\n" "$*"; }
ok()    { printf "${GREEN}[vibewarden]${RESET} %s\n" "$*"; }
fail()  { printf "${RED}[vibewarden]${RESET} %s\n" "$*" >&2; exit 1; }

# --- Detect platform ---
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$OS" in
        darwin) OS="darwin" ;;
        linux)  OS="linux" ;;
        mingw*|msys*|cygwin*) OS="windows" ;;
        *) fail "Unsupported OS: $OS" ;;
    esac

    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64|amd64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) fail "Unsupported architecture: $ARCH" ;;
    esac
}

# --- Resolve version ---
resolve_version() {
    if [ -n "${VERSION:-}" ]; then
        log "Using specified version: $VERSION"
        return
    fi

    log "Resolving latest version..."
    url="https://api.github.com/repos/${REPO}/releases/latest"
    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -fsSL "$url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    elif command -v wget >/dev/null 2>&1; then
        VERSION=$(wget -qO- "$url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    else
        fail "curl or wget is required"
    fi

    if [ -z "$VERSION" ]; then
        fail "Could not determine latest version. Set VERSION=v0.1.0 to install manually."
    fi
    log "Latest version: $VERSION"
}

# --- Download helpers ---
download() {
    url="$1"
    dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$dest" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$dest" "$url"
    else
        fail "curl or wget is required"
    fi
}

# --- Checksum verification ---
verify_checksum() {
    file="$1"
    checksums="$2"
    filename=$(basename "$file")
    expected=$(grep " ${filename}$" "$checksums" | awk '{print $1}')

    if [ -z "$expected" ]; then
        fail "No checksum found for $filename"
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$file" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        log "Warning: cannot verify checksum (sha256sum/shasum not found)"
        return
    fi

    if [ "$actual" != "$expected" ]; then
        fail "Checksum mismatch for $filename\n  expected: $expected\n  actual:   $actual"
    fi
    log "Checksum verified"
}

# --- Determine install directory ---
resolve_install_dir() {
    if [ -n "${INSTALL_DIR:-}" ]; then
        return
    fi

    # Try /usr/local/bin if writable, else current directory
    if [ -w "/usr/local/bin" ]; then
        INSTALL_DIR="/usr/local/bin"
    else
        INSTALL_DIR="."
    fi
}

# --- Main ---
main() {
    log "Installing VibeWarden CLI..."
    detect_platform
    resolve_version
    resolve_install_dir

    CLEAN_VERSION="${VERSION#v}"
    ARCHIVE="vibewarden_${CLEAN_VERSION}_${OS}_${ARCH}.tar.gz"
    BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT

    log "Downloading ${ARCHIVE}..."
    download "${BASE_URL}/${ARCHIVE}" "${TMP_DIR}/${ARCHIVE}"
    download "${BASE_URL}/checksums.txt" "${TMP_DIR}/checksums.txt"

    verify_checksum "${TMP_DIR}/${ARCHIVE}" "${TMP_DIR}/checksums.txt"

    log "Extracting..."
    tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "${TMP_DIR}"

    # Move binary to install dir
    DEST="${INSTALL_DIR}/${BINARY_NAME}"
    if [ "$OS" = "windows" ]; then
        DEST="${DEST}.exe"
    fi

    if [ "$INSTALL_DIR" != "." ] && [ ! -w "$INSTALL_DIR" ]; then
        log "Need sudo to install to ${INSTALL_DIR}"
        sudo mv "${TMP_DIR}/${BINARY_NAME}" "$DEST"
        sudo chmod +x "$DEST"
    else
        mv "${TMP_DIR}/${BINARY_NAME}" "$DEST"
        chmod +x "$DEST"
    fi

    ok "Installed to ${DEST}"
    echo ""
    ok "Get started:"
    echo "  vibew init --upstream 3000"
    echo "  vibew dev"
    echo ""
    ok "Docs: https://vibewarden.dev/docs/"
}

main
