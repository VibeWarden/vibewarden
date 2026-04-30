#!/usr/bin/env bash
# emit-kickoff-artifacts.sh — Emit agent-kickoff-{dev,deploy}.txt artifacts
# for the release.
#
# Invoked from .goreleaser.yml before.hooks so the artifacts are ready before
# goreleaser's release stage uploads them via release.extra_files.
#
# Usage:
#   ./scripts/release/emit-kickoff-artifacts.sh [OUT_DIR]
#
# OUT_DIR defaults to "release-artifacts". Both output files are written there:
#   $OUT_DIR/agent-kickoff-dev.txt
#   $OUT_DIR/agent-kickoff-deploy.txt
#
# Design: ADR-101.
# Edge case: config.SanitizeProjectName rewrites curly braces in project names
# to hyphens. The public placeholder {{prjname}} would be mangled to "prjname".
# Solution: pass a sanitiser-safe sentinel ("vwprjname") for --name, then
# sed-rewrite the rendered output to restore the two-brace placeholder form.
# The --describe and --domain values are not sanitised by the CLI so they can
# be passed through literally as {{description}} and a domain sentinel.

set -euo pipefail

OUT_DIR="${1:-release-artifacts}"
mkdir -p "$OUT_DIR"

# Build vibew once for this run. Goreleaser may have already built it; reuse
# if the binary is present at the project root. The ldflags stamp matches the
# goreleaser build so the version header in the artifact is accurate.
VIBEW_BIN="./vibew"
if [ ! -x "$VIBEW_BIN" ]; then
  VERSION="$(git describe --tags --always 2>/dev/null || echo "dev")"
  go build -o "$VIBEW_BIN" -ldflags "-X main.version=${VERSION}" ./cmd/vibewarden
fi

# Sentinels used in place of the public two-brace placeholders.
# vwprjname survives SanitizeProjectName (lowercase alphanum only).
# vwdomain.example.invalid is a valid FQDN for the CLI validator.
#
# Reserved sentinel token. Used because config.SanitizeProjectName rewrites
# `{{prjname}}` to `prjname`, so we can't pass the literal placeholder to
# `vibew prompt-template --name`. The sed rewrite below is unanchored — if
# any future template line happens to contain the substring `vwprjname` it
# would be silently corrupted. The forensic test in
# internal/app/promptkickoff/wrapper_script_test.go::TestWrapperScript_ArtifactsPassForensicChecks
# guards against the leakage case but pick a different sentinel here if you
# ever introduce a template line that legitimately contains `vwprjname`.
NAME_SENTINEL="vwprjname"
DOMAIN_SENTINEL="vwdomain.example.invalid"

# write_header appends the self-describing artifact header (shell-comment-safe,
# every line #-prefixed) to $outfile.
write_header() {
  local flavor="$1"
  local outfile="$2"
  local src_cmd="$3"
  cat >> "$outfile" <<EOF
# VibeWarden Agent Kickoff Release Artifact
# Flavor: $flavor
# vibew version: $("$VIBEW_BIN" --version 2>&1 | head -1)
# Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)
# Source command: $src_cmd
#
# This file uses two-brace placeholders. Substitute before pasting:
#   {{prjname}}     — project name (lowercase, hyphenated, no spaces)
#   {{description}} — one-line project description
#   {{domain}}      — FQDN the app will be served on (e.g. myapp.example.com)
#
# Regeneration:
#   vibew prompt-template [--deploy] --name "{{prjname}}" --describe "{{description}}" [--domain "{{domain}}"]
#
# Canonical URL:
#   https://github.com/vibewarden/vibewarden/releases/latest/download/agent-kickoff-$flavor.txt
# ----------------------------------------------------------------------------
EOF
}

# Dev flavor: --name sentinel, --describe literal placeholder.
DEV_OUT="$OUT_DIR/agent-kickoff-dev.txt"
: > "$DEV_OUT"  # truncate / create
write_header \
  "dev" \
  "$DEV_OUT" \
  'vibew prompt-template --name "{{prjname}}" --describe "{{description}}"'
"$VIBEW_BIN" prompt-template \
  --name "$NAME_SENTINEL" \
  --describe "{{description}}" \
  | sed "s/${NAME_SENTINEL}/{{prjname}}/g" \
  >> "$DEV_OUT"

# Deploy flavor: adds --deploy and --domain sentinel.
DEPLOY_OUT="$OUT_DIR/agent-kickoff-deploy.txt"
: > "$DEPLOY_OUT"  # truncate / create
write_header \
  "deploy" \
  "$DEPLOY_OUT" \
  'vibew prompt-template --deploy --name "{{prjname}}" --describe "{{description}}" --domain "{{domain}}"'
"$VIBEW_BIN" prompt-template \
  --deploy \
  --name "$NAME_SENTINEL" \
  --describe "{{description}}" \
  --domain "$DOMAIN_SENTINEL" \
  | sed -e "s/${NAME_SENTINEL}/{{prjname}}/g" \
        -e "s/${DOMAIN_SENTINEL}/{{domain}}/g" \
  >> "$DEPLOY_OUT"

echo "Wrote $DEV_OUT and $DEPLOY_OUT"
