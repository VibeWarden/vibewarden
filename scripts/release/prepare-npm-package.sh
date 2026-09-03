#!/usr/bin/env bash
# prepare-npm-package.sh — Prepare npm/ for publication of @vibewarden/cli.
#
# Invoked by the `npm` job in .github/workflows/release.yml after the GitHub
# Release exists, so the archives and checksums.txt it reads are already public.
#
# Usage:
#   ./scripts/release/prepare-npm-package.sh <tag>       # e.g. v0.21.0
#
# What it does:
#   1. Derives VERSION from the tag (strips the leading "v").
#   2. Skips everything if that version is already on the registry, so re-running
#      a release workflow is idempotent instead of failing on npm's 403.
#   3. Downloads checksums.txt from the release.
#   4. Sets npm/package.json to VERSION via `npm version` (never sed on JSON).
#   5. Writes npm/checksums.json with the digests of the four published archives,
#      failing loudly if any of them is missing from checksums.txt — that means
#      the release matrix changed and npm/lib/platform.js is now wrong.
#   6. Copies LICENSE into npm/ (npm publishes the package directory only).
#   7. Emits version / dist_tag / skip to $GITHUB_OUTPUT. Prereleases go to the
#      `next` dist-tag: a prerelease must never become `latest`.
#
# Environment overrides (testing):
#   VIBEWARDEN_CHECKSUMS_FILE   use a local checksums.txt instead of downloading
#   VIBEWARDEN_SKIP_REGISTRY_CHECK=1   do not query the npm registry
#
# Design: ADR-112.

set -euo pipefail

PACKAGE_NAME="@vibewarden/cli"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NPM_DIR="${REPO_ROOT}/npm"

TAG="${1:-}"
if [ -z "$TAG" ]; then
  echo "error: tag argument is required (e.g. v0.21.0)" >&2
  exit 1
fi

VERSION="${TAG#v}"
if ! printf '%s' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
  echo "error: tag '${TAG}' does not look like a semver release tag" >&2
  exit 1
fi

emit() {
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "$1" >>"$GITHUB_OUTPUT"
  fi
  echo "prepare-npm-package: $1"
}

# --- 2. Idempotence: already published? ---
if [ "${VIBEWARDEN_SKIP_REGISTRY_CHECK:-}" != "1" ]; then
  if npm view "${PACKAGE_NAME}@${VERSION}" version >/dev/null 2>&1; then
    echo "prepare-npm-package: ${PACKAGE_NAME}@${VERSION} is already published, nothing to do."
    emit "skip=true"
    exit 0
  fi
fi
emit "skip=false"

# --- 3. Checksums from the release ---
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

CHECKSUMS="${WORK_DIR}/checksums.txt"
if [ -n "${VIBEWARDEN_CHECKSUMS_FILE:-}" ]; then
  cp "$VIBEWARDEN_CHECKSUMS_FILE" "$CHECKSUMS"
else
  echo "prepare-npm-package: downloading checksums.txt for ${TAG}..."
  gh release download "$TAG" --repo vibewarden/vibewarden --pattern checksums.txt --dir "$WORK_DIR"
fi

# --- 4. Version the package ---
( cd "$NPM_DIR" && npm version --no-git-tag-version --allow-same-version "$VERSION" >/dev/null )
echo "prepare-npm-package: npm/package.json set to ${VERSION}"

# --- 5. Embed the digests ---
VERSION="$VERSION" CHECKSUMS="$CHECKSUMS" OUT="${NPM_DIR}/checksums.json" NPM_DIR="$NPM_DIR" node -e '
const fs = require("node:fs");
const { parseChecksumsTxt } = require(process.env.NPM_DIR + "/lib/verify");
const { archiveName } = require(process.env.NPM_DIR + "/lib/platform");

const version = process.env.VERSION;
const digests = parseChecksumsTxt(fs.readFileSync(process.env.CHECKSUMS, "utf8"));

const targets = [
  ["darwin", "amd64"],
  ["darwin", "arm64"],
  ["linux", "amd64"],
  ["linux", "arm64"],
];

const archives = {};
const missing = [];
for (const [goos, goarch] of targets) {
  const name = archiveName(version, goos, goarch);
  if (!digests[name]) {
    missing.push(name);
    continue;
  }
  archives[name] = digests[name];
}

if (missing.length > 0) {
  console.error("error: checksums.txt has no entry for: " + missing.join(", "));
  console.error("The release archive matrix changed. npm/lib/platform.js, scripts/install.sh");
  console.error("and internal/app/upgrade/service.go all depend on this naming contract.");
  process.exit(1);
}

fs.writeFileSync(process.env.OUT, JSON.stringify({ version, archives }, null, 2) + "\n");
console.log("prepare-npm-package: embedded " + Object.keys(archives).length + " digests");
'

# --- 6. License ---
cp "${REPO_ROOT}/LICENSE" "${NPM_DIR}/LICENSE"

# --- 7. Dist-tag ---
case "$VERSION" in
  *-*) emit "dist_tag=next" ;;
  *)   emit "dist_tag=latest" ;;
esac
emit "version=${VERSION}"
