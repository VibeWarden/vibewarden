# ADR-094: vibew bundle sensitive-file awareness block

**Date**: 2026-04-23
**Issue**: #1142
**Status**: Accepted

## Context

`vibew bundle` writes `.credentials` (Kratos admin credentials) and `.env`
(image tag, port config, and credentials-derived vars) into the bundle
directory. Those files are silently scp'd to the remote host alongside the
compose file. Users see no signal that secrets are shipping. The README does
not flag the credential surface either.

The PM spec on #1142 locks the shape: print an awareness block on stdout
after the file listing, no prompt, no flag, purely additive. Add a
`## Secrets` section to the bundle README. No suppression mechanism — the
output is unconditional whenever sensitive files are detected in the
written bundle.

This ADR turns that spec into a precise design.

## Decision

### Domain model changes

None. This is a CLI/output concern only. No new entities, value objects, or
domain events. The existing bundle service contract is unchanged.

### Ports (interfaces)

None. Detection is a post-bundle filesystem scan run in `internal/cli/cmd/`
using the existing `os`/`filepath` stdlib. No new port is justified — we are
not abstracting an external system, we are reading bytes we just wrote.

Choosing scan-after-write over widening `bundleapp.Service.Bundle` to return
a sensitive-file slice keeps the application service's contract stable
(referenced by the deferred-restore test path and ADR-085 multi-site
boundary) and isolates the awareness feature inside the CLI layer.

### Adapters

None. The detection helper is a private function in `internal/cli/cmd/`.

### Application service

`internal/app/bundle.renderBundleReadme` gains a static `## Secrets` section
between `## What's in this bundle` and `## Rebuild for a different arch`.
The section is pure prose (no shell snippets) so the existing
`TestBundle_Extras_Readme_DeployContract` negative assertions continue to
hold.

### File layout

New files:
- `internal/cli/cmd/bundle_secrets.go` — sensitive-file detection +
  awareness-block renderer.
- `internal/cli/cmd/bundle_secrets_test.go` — table-driven tests for the
  detector and the renderer.

Modified files:
- `internal/cli/cmd/bundle.go::runBundle` — call the detector after the
  existing `Contents:` listing, before the `Next:` hint.
- `internal/app/bundle/bundle_extras.go::renderBundleReadme` — append
  `## Secrets` section.
- `internal/app/bundle/bundle_extras_test.go` — extend
  `TestBundle_Extras_Readme_DeployContract` with positive `## Secrets`
  assertions; raise the 40-line cap in
  `TestBundle_Extras_Readme_MentionsPlatformHint` to 60 lines (the cap
  exists to bound noise, not enforce a numeric ceiling — a targeted bound
  is still informative once the Secrets section ships).

### Sequence

1. `runBundle` finishes writing the bundle (existing path, unchanged).
2. `runBundle` walks the output dir via existing `bundleListing` and prints
   `Contents:` (unchanged).
3. New step: `runBundle` calls
   `detectSensitiveFiles(absOut)` which walks the output dir and returns a
   sorted `[]sensitiveFile`. Each entry has `RelPath string` and
   `Description string`.
4. If the slice is non-empty, `runBundle` prints one blank line, then the
   block:
   ```
   Sensitive files in this bundle:
     <relpath>  — <description>
     <relpath>  — <description>
   These files ship with the bundle when you copy it to a host. If the host
   or transport is untrusted, generate fresh credentials there instead.
   ```
   Two-space indent, two spaces around the em-dash separator (`  — `), no
   ANSI color, stable header text. Description column is NOT padded — keep
   output deterministic without depending on the longest filename.
5. If the slice is empty, the block is omitted entirely (no header, no
   prose footer, no extra blank line). The existing `Next:` line still
   prints with its current preceding blank line.
6. `runBundle` prints the existing `Next:` hint (unchanged).

### Detection rules

`detectSensitiveFiles(rootDir)` walks `rootDir` recursively. A file matches
when **any** rule fires; the first matching rule provides the description.

Rule order (first match wins):

| Rule                                       | Description                          |
|--------------------------------------------|--------------------------------------|
| basename `.env`                            | generated environment variables      |
| basename `.credentials`                    | Kratos admin credentials             |
| relpath `kratos/secrets` (file, exact)     | Kratos cookie and cipher secrets     |
| any path under `kratos/` (other files)     | Kratos identity store data           |
| basename matches `*-key.pem`               | private key material                 |
| basename matches `*.pem`                   | private key material                 |
| basename matches `*.key`                   | private key material                 |
| basename matches `*.token`                 | API token / bearer credential        |

Notes:
- Walk uses `filepath.WalkDir` and skips directories.
- Comparisons are case-sensitive (matches POSIX file naming; the bundle
  pipeline only writes lowercase names).
- Output is sorted by `RelPath` (uses `sort.Slice`).
- The `kratos/` directory rule is a path-prefix check; the explicit
  `kratos/secrets` rule must come first so its specific description wins
  over the generic "Kratos identity store data" fallback.

### Error cases

- `filepath.WalkDir` returns an I/O error: propagate as a wrapped error
  (`"scanning bundle for sensitive files: %w"`). Bundle has already been
  written — surface the error but do not roll back the bundle. Exit code
  stays 1 (generic) since the bundle on disk is still valid.
- The rootDir does not exist: should not happen post-bundle, but if it
  does the walk returns an error (handled above).
- A walk encounters a file whose name cannot be made relative: skip the
  file, continue the walk (defensive — same posture as `bundleListing`
  but here we choose skip-and-continue rather than abort because the
  awareness block is advisory).

### Test strategy

Unit tests in `internal/cli/cmd/bundle_secrets_test.go` (no docker, no
Kratos, no real bundle pipeline — just the detector + renderer):

1. `TestDetectSensitiveFiles_StandardBundle` — temp dir with `.env`,
   `.credentials`, `docker-compose.yml`, `README.md`. Asserts exactly two
   matches with the correct descriptions and order.
2. `TestDetectSensitiveFiles_KratosTree` — temp dir with `kratos/secrets`,
   `kratos/identity.schema.json`. Asserts both detected with distinct
   descriptions, `kratos/secrets` getting the specific description.
3. `TestDetectSensitiveFiles_KeyMaterial` — temp dir with `tls.key`,
   `cert-key.pem`, `cert.pem`, `bearer.token`. Asserts all four match
   with the right descriptions.
4. `TestDetectSensitiveFiles_NoMatches_ReturnsEmpty` — temp dir with only
   `docker-compose.yml`, `README.md`, `sample.env`. Asserts empty slice.
5. `TestDetectSensitiveFiles_FirstMatchWins` — temp dir with `kratos/.env`
   (basename `.env` rule fires before `kratos/` fallback). Asserts the
   `.env` description.
6. `TestRenderSensitiveBlock_Empty_ReturnsEmpty` — empty input → empty
   string.
7. `TestRenderSensitiveBlock_StableFormat` — fixed input → exact expected
   string match (golden-style assertion). Locks the public stdout surface.

Integration-style test in `internal/cli/cmd/` (or extend an existing
`bundle_test.go` if present): drive `runBundle` via a temp project with a
fake generator that writes `.env` + `.credentials`, capture
`cmd.OutOrStdout()`, assert the awareness block appears between the
`Contents:` listing and the `Next:` hint.

README assertions in `internal/app/bundle/bundle_extras_test.go`:
- Extend `TestBundle_Extras_Readme_DeployContract` positive set with
  `"## Secrets"`, `".credentials"`, and the static prose footer
  `"transport is untrusted"`.
- Confirm the existing forbidden-token list (`scp `, `ssh `, etc.) still
  passes with the new section.
- Raise the 40-line ceiling in `TestBundle_Extras_Readme_MentionsPlatformHint`
  to 60.

### New dependencies

None. Stdlib only (`path/filepath`, `strings`, `sort`).

## Consequences

**Pros**
- Stable, parseable stdout surface (mirrors ADR-089's image health block).
- No new ports, no service-interface widening — the awareness logic lives
  where it is consumed (CLI).
- README documents the credential surface without shell snippets, keeping
  ADR-088's artifact policy intact.
- No flag, no env var, no prompt — agents and CI are never blocked.

**Cons**
- Detection logic duplicates a subset of `bundleListing`'s walk. Acceptable
  because the two have different semantics (listing prints everything;
  detection classifies a subset) and unifying them would couple a stable
  output surface to an internal helper.
- The match table is hardcoded. Future sensitive-file types (e.g., a new
  TLS adapter that writes `*.crt` private bundles) must update both the
  detector and the README. Mitigation: the renderer is a single function;
  the table sits alongside it.

**Future**
- If we ever add a `--quiet` mode for `vibew bundle` (none planned), the
  block participates in the existing stdout suppression — no special
  casing required.
- If a Pro fleet feature wants to forward the sensitive-file list as
  telemetry, the `[]sensitiveFile` slice is already a clean data structure
  to hand off; no refactor needed.
