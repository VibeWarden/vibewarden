# ADR-101: Agent-kickoff release artifacts — main repo emits canonical kickoff prompts as release assets

**Date**: 2026-04-30
**Issue**: #1232
**Status**: Accepted

## Context

Three retrospectives in a row — v0.18.1 (qr-code-blackhole), v0.18.2
(qr-code-brutalist Go), v0.18.3 (qr-code-brutalist Node) — have flagged the same
recurring defect: agents kicked off from `https://vibewarden.dev/start` are
handed a prompt whose deploy section drifts from reality. The most recent
incident (v0.18.3) shipped agents `bash deploy.sh` even though `deploy.sh` was
removed in #1138 / ADR-088. The agent dutifully ran the obsolete command and
hit `deploy.sh: No such file or directory` mid-task.

Investigation traced the drift to a hand-rolled template literal in
`vibewarden.dev:src/start/index.njk` (~line 32). A code comment on that very
line literally admits the gap: `// Deploy — bundle + manual deploy.sh
(vibew deploy was removed in ADR-086)`. The website also lacks the post-#1200
`vibew build --platform linux/amd64` step required to avoid the silent Apple
Silicon → amd64 runtime fail.

ADR-099 (#1203) gave the kickoff prompt a canonical home in the binary
(`vibew prompt-template`, embedded `text/template` in
`internal/cli/templates/prompts/`). It did not, however, address the website,
which is the single highest-traffic surface where agents pick up the prompt.
As long as the website maintains its own copy, every deploy-contract change
risks silently invalidating it — three retros confirm this is not theoretical.

CLAUDE.md §Content authority already locks the pattern for this class of
content-authority problem: the main repo is the source of truth for
LLM-consumable content (`llms.txt`, `llms-full.txt`,
`vibewarden.reference.yaml`), and the website fetches these files at build
time from the latest release tag. The kickoff prompt is the same shape of
content (canonical, version-stamped, URL-consumable by external builders),
just generated rather than checked in. Extending the same pattern is the
minimum-surprise fix.

This ADR formalises that extension. It also constrains every future tool that
wants to surface the kickoff prompt: from now on, the URL pattern below is the
public contract; do not regress to hand-rolled templates.

## Decision

The release pipeline emits two new canonical artifacts per release tag, each
uploaded to the GitHub Release as a downloadable asset. The website (and any
other downstream consumer) fetches them at build time and runs a trivial
mustache-style placeholder substitution to produce per-user prompts.

### Artifacts

| Asset filename            | Source command                                                                                              | Purpose                                  |
|---------------------------|-------------------------------------------------------------------------------------------------------------|------------------------------------------|
| `agent-kickoff-dev.txt`   | `vibew prompt-template --name "{{prjname}}" --describe "{{description}}"`                                   | Dev-only flavor (install + dev loop)     |
| `agent-kickoff-deploy.txt`| `vibew prompt-template --deploy --name "{{prjname}}" --describe "{{description}}" --domain "{{domain}}"`    | Dev + deploy flavor (bundle + ssh + healthcheck) |

Both files are produced by invoking the freshly-built binary inside the
goreleaser pipeline. The `--name`, `--describe`, `--domain` flag values are
literal mustache-style placeholders, kept verbatim in the rendered output.
Downstream consumers (the website, third-party generators) interpolate by
running `s.replaceAll("{{prjname}}", userName)` — no parsing required.

### Placeholder format

Two-brace mustache style:

- `{{prjname}}` — sanitised project name (Docker-Compose-safe slug). Required.
- `{{description}}` — one-line project description. Required.
- `{{domain}}` — FQDN. Used by both flavors; `--deploy` requires it, `dev`
  passes it through to `vibew add tls`.

### Artifact header

Every artifact opens with a self-describing header so a human reading the file
directly understands what it is, how it was generated, and how to consume it.
The header is appended *above* the existing template output by the wrapper
script — the canonical `vibew prompt-template` body is left byte-for-byte
unchanged so it stays in sync with what users see when they run the command
locally.

```
# VibeWarden Agent Kickoff Release Artifact
# Flavor: dev | deploy
# vibew version: <version>
# Generated: <RFC-3339 UTC timestamp>
# Source command: vibew prompt-template ... (literal command, with placeholders)
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
#   https://github.com/vibewarden/vibewarden/releases/latest/download/agent-kickoff-<flavor>.txt
# ----------------------------------------------------------------------------

<unmodified output of vibew prompt-template ...>
```

The header is shell-comment-safe (every line starts with `#`) so a curious user
can `cat` the file without a special viewer. The body below the rule is the
exact byte sequence produced by the binary — ADR-099's golden tests already
guard that.

### Annotation: optional `mkdir`

The template's Step 2 currently emits:

```
  mkdir {{.Name}} && cd {{.Name}}
```

Per retro feedback, agents are often already inside a project directory. The
template will be amended to:

```
  # Skip if you're already in the project directory:
  mkdir {{.Name}} && cd {{.Name}}
```

This change lives in `internal/cli/templates/prompts/{dev,deploy}.tmpl` and is
guarded by the existing golden tests in
`internal/app/promptkickoff/golden_test.go`. It is *not* a release-pipeline
concern — it ships in the binary and propagates into both the artifact body
and `llms-full.txt § 21`.

### Goreleaser hook approach

**Option A — `before.hooks` shell wrapper.** Goreleaser's top-level `before:
hooks:` runs commands after `git clone` and before any artifact build. We add
a `make` target that:

1. Builds vibew once with the same `-ldflags "-X main.version=<version>"`
   stamp the goreleaser build uses.
2. Invokes the freshly-built binary twice with the placeholder strings to
   produce the two artifact bodies.
3. Prepends the self-describing header to each.
4. Writes the result to `dist/agent-kickoff-{dev,deploy}.txt`.

The artifacts are then attached to the Release via `release.extra_files`
(goreleaser v2 syntax).

This is the chosen approach. Reasons:

- Reuses the existing `vibew prompt-template` CLI verb without inventing a new
  flag. ADR-099's golden tests stay authoritative.
- Goreleaser's `before` hooks run exactly once per release and are the
  documented home for this kind of pre-artifact generation.
- `release.extra_files` is the documented mechanism for uploading
  arbitrary files alongside built artifacts.
- Snapshot builds (`goreleaser release --snapshot`) run hooks too, so the
  release-dryrun workflow exercises this path on every PR that touches
  goreleaser config.

### Alternatives considered

- **Hand-rolled website template (status quo).** Rejected — three retros
  confirm drift recurs. The website team cannot be expected to track every
  deploy-contract change in the main repo.
- **Embed the binary in the website build.** Rejected — wrong distribution
  model. The website is presentation only (CLAUDE.md §Content authority);
  pulling a Go binary into the eleventy build crosses that boundary and
  forces website CI to deal with cross-compilation.
- **Fetch from latest commit on `main`.** Rejected — release tags are the
  intentional content-authority boundary. `latest commit` would mean the
  website renders pre-release commands the binary may not yet support.
  The pattern already established for `llms-full.txt` is "fetch from latest
  release tag"; this ADR mirrors it.
- **Add a new `cmd/genkickoff/main.go` Go binary.** Rejected — a second
  binary in `cmd/` is overkill when the existing CLI verb already does the
  work. The wrapper-script approach is ~20 lines of shell.
- **Add a `vibew prompt-template --emit-release-artifacts <dir>` flag.**
  Rejected — adds a CLI surface that exists solely for the release pipeline.
  The pipeline can shell-script around the existing flags.
- **JSON envelope artifact.** Rejected — adds a parse step on every consumer
  for no benefit. The `.txt` form is `replace`-friendly out of the box and
  human-readable when piped to `cat`.

## Domain model changes

None. This ADR is a release-pipeline / packaging change; no domain entities,
ports, or adapters are added or modified. The kickoff prompt domain model
(`promptkickoff.Service`, `promptkickoff.Options`) was established in ADR-099
and is reused unchanged.

## Ports (interfaces)

None added or modified. The wrapper script is an external orchestrator that
shells out to the existing `vibew prompt-template` CLI; it does not cross the
ports/adapters boundary because the CLI is already the public interface.

## Adapters

None added or modified.

## Application service

The existing `internal/app/promptkickoff/promptkickoff.go` `Service` is the
generator. The release wrapper invokes it transparently via `vibew
prompt-template`.

The only behavioural tweak inside the binary is in the templates themselves
(see *Annotation: optional `mkdir`* above) — a docstring comment line ahead
of the `mkdir` command. This change is guarded by the existing golden tests.

## File layout

### New files

| Path | Purpose |
|------|---------|
| `scripts/release/emit-kickoff-artifacts.sh` | Shell wrapper invoked by goreleaser `before` hook. Builds vibew, invokes `prompt-template` twice, prepends headers, writes to `dist/agent-kickoff-{dev,deploy}.txt`. |
| `decisions/adr-101-agent-kickoff-release-artifacts.md` | This ADR. |
| `internal/app/promptkickoff/release_artifact_test.go` | Forensic CI test: builds vibew, runs both `prompt-template` invocations with placeholder strings, asserts the post-#1138 deploy contract is present and the buggy forms are absent. Uses the existing `Service`+golden harness so it does not require a built binary. |

### Modified files

| Path | Change |
|------|--------|
| `.goreleaser.yml` | Add top-level `before: hooks:` running `bash scripts/release/emit-kickoff-artifacts.sh`. Add `release.extra_files:` listing the two artifacts. |
| `internal/cli/templates/prompts/dev.tmpl` | Insert `# Skip if you're already in the project directory:` line above the `mkdir` line. |
| `internal/cli/templates/prompts/deploy.tmpl` | Same as dev.tmpl. |
| `internal/app/promptkickoff/testdata/dev.golden` | Regenerate via `UPDATE_GOLDEN=1 go test ./internal/app/promptkickoff/...` to capture the new annotation. |
| `internal/app/promptkickoff/testdata/deploy.golden` | Same. |
| `decisions/README.md` | Add ADR-101 row. |
| `CHANGELOG.md` | Unreleased / Added entry pointing at #1232. |
| `docs/agent-kickoff.md` | Add a "Release-asset URLs" subsection documenting the two `releases/latest/download/...` URLs. |
| `llms-full.txt` § 21 | Append a one-paragraph note pointing at the release-asset URLs as the recommended consumption path for tools that don't ship the binary. |

## Sequence

### Release path (CI)

1. Tag pushed to `main`. `.github/workflows/release.yml` triggers.
2. GoReleaser starts. Top-level `before: hooks:` runs
   `scripts/release/emit-kickoff-artifacts.sh`.
3. The wrapper script:
   a. Computes `VERSION` from `git describe --tags --always`.
   b. Builds a vibew binary at `dist/.kickoff-vibew` with the same
      `-ldflags "-X main.version=$VERSION"` stamp the main goreleaser build
      uses.
   c. Invokes `dist/.kickoff-vibew prompt-template --name "{{prjname}}"
      --describe "{{description}}" --domain "{{domain}}"` and pipes stdout
      to a temp file. The literal `{{...}}` strings pass through `--name`
      because `SanitizeProjectName` rewrites non-alphanumerics to `-` —
      see *Edge case: placeholder sanitisation* below.
   d. Same for the `--deploy` flavor.
   e. Composes the final artifact as `<header>` + `\n` + `<body>` and writes
      to `dist/agent-kickoff-dev.txt` and `dist/agent-kickoff-deploy.txt`.
4. GoReleaser proceeds to the normal build/archive/docker steps.
5. At the `release` stage, `release.extra_files` uploads the two `.txt`
   artifacts alongside the binary archives, checksums, and Docker images.
6. The GitHub Release is created. The two artifacts are now reachable at:
   - `https://github.com/vibewarden/vibewarden/releases/download/<tag>/agent-kickoff-dev.txt`
   - `https://github.com/vibewarden/vibewarden/releases/download/<tag>/agent-kickoff-deploy.txt`
   - `https://github.com/vibewarden/vibewarden/releases/latest/download/agent-kickoff-dev.txt` (auto-redirect to current latest)
   - `https://github.com/vibewarden/vibewarden/releases/latest/download/agent-kickoff-deploy.txt` (auto-redirect to current latest)

### Snapshot path (PR dryrun)

`.github/workflows/release-dryrun.yml` runs
`goreleaser release --snapshot --skip=publish --clean` on every PR that
touches goreleaser config or wrapper scripts. The `before` hook still runs,
the artifacts are written to `dist/`, but `release.extra_files` is a no-op
because `--skip=publish` prevents asset upload. This validates the wrapper
script does not regress on PRs that touch it without requiring a real release.

### Consumer path (website)

The website's eleventy build adds a build-time fetch step that resolves
`https://github.com/vibewarden/vibewarden/releases/latest/download/agent-kickoff-{dev,deploy}.txt`
and substitutes the two-brace placeholders against form input. The website
companion is filed separately (`vibewarden/vibewarden.dev:#TBD`) — that PR is
the only place the website code changes. From the main repo's perspective,
the contract is "those two URLs serve those two files in that format" and is
locked by the forensic CI test below.

## Error cases

| Failure | Detection | Behavior |
|---------|-----------|----------|
| `vibew prompt-template` exits non-zero (CLI bug, missing flag) | Wrapper script `set -euo pipefail` | GoReleaser fails the release. No partial artifact uploaded. |
| `SanitizeProjectName("{{prjname}}")` mangles the placeholder | Forensic CI test asserts `{{prjname}}` survives | Test fails before tagging; never reaches a release. |
| Body output lacks `docker compose up -d` | Forensic CI test (extends ADR-099 §Test strategy) | Test fails before tagging. |
| Body output contains `bash deploy.sh`, `./deploy.sh`, or `scp -r .vibewarden/bundle/*` | Forensic CI test | Test fails before tagging. |
| Header is missing the source-command line or the placeholder list | Forensic CI test | Test fails before tagging. |
| GoReleaser snapshot in dryrun fails to find `bash` | `release-dryrun.yml` runs on `ubuntu-latest` (always has bash) | N/A. |

## Edge case: placeholder sanitisation

`promptkickoff.Render` calls `config.SanitizeProjectName(opts.Name)` before
templating. For the literal placeholder string `{{prjname}}`, the sanitiser
runs through every rune and replaces non-`[a-z0-9-]` characters with `-`,
then trims leading/trailing hyphens. Result: `{{prjname}}` becomes
`prjname`. **The placeholder vanishes.**

Two options:

1. **Wrap in alphanumeric so the sanitiser preserves the markers.** Pass
   `--name "x{{prjname}}x"` and have downstream consumers replace the wider
   token. Ugly. Forces website to know the wrapping convention.
2. **Update `SanitizeProjectName` to bypass `{{...}}` segments.** Rejected
   — pollutes a domain primitive with release-tooling concerns.
3. **Substitute the placeholder *into the artifact body after rendering*.**
   The wrapper script invokes the binary with a real, sanitiser-friendly
   sentinel like `__VW_PRJNAME__`, then runs a `sed` pass post-render that
   converts each sentinel to the public `{{...}}` form. The sentinel
   characters (uppercase + underscores) all survive sanitisation
   verbatim (uppercase becomes lowercase: `__vw_prjname__`).

**Decision: option 3** — sentinel + post-render rewrite. The sentinel set:

| Public placeholder | Internal sentinel passed to vibew | Post-render rewrite (sed) |
|--------------------|-----------------------------------|----------------------------|
| `{{prjname}}`      | `vwprjname` (lowercase, alphanum) | `s/vwprjname/{{prjname}}/g` |
| `{{description}}`  | passed via `--describe` (NOT sanitised — only trim+newline-check) | `s/<exact string>/{{description}}/g` |
| `{{domain}}`       | `vwdomain.example.invalid`        | `s/vwdomain.example.invalid/{{domain}}/g` |

Implementation note: `Describe` is not sanitised by `Render` (only trimmed
and newline-checked), so passing `{{description}}` directly works for that
field. Same for `Domain`, which is templated in verbatim. Only `Name`
needs the sentinel dance.

The `Name` sentinel `vwprjname` is chosen so that:

- It survives `SanitizeProjectName` byte-for-byte.
- It is unique enough that a `sed` substitution cannot collide with prose.
- It is short and humane in case a `sed` step is ever skipped.

The forensic CI test asserts the rewrite worked: the final artifact
contains `{{prjname}}` literally and contains no `vwprjname` substring.

## Test strategy

### Unit-level (existing, reused)

ADR-099 already established golden tests in
`internal/app/promptkickoff/golden_test.go` covering both flavors. Those
tests continue to enforce byte-for-byte stability of the `Render` output.
The optional-`mkdir` annotation lands as a golden update in the same PR
(no logic change, only a template-line addition).

### New forensic CI test — `internal/app/promptkickoff/release_artifact_test.go`

Self-contained Go test (no shell, no built binary required) that:

1. Constructs a `promptkickoff.Service` via the existing `clitemplates.FS`.
2. Invokes `Render` twice with `Name: "vwprjname"`, `Describe:
   "{{description}}"`, `Domain: "vwdomain.example.invalid"`, deploy off
   then on.
3. Applies the same post-render rewrite the release wrapper applies.
4. For each rendered+rewritten body, asserts:
   - **Contains** `docker load -i image.tar && docker compose up -d`
     (post-#1138 contract, mirrors ADR-099's TestDeployTemplate test).
   - **Contains** `tar -czf - -C .vibewarden/bundle .` (post-#1217
     dotfile-safe transfer).
   - **Contains** `tar -xzf - -C` (post-#1217 dotfile-safe receive).
   - **Contains** `# Skip if you're already in the project directory:`
     immediately preceding the `mkdir` line.
   - **Contains** `{{prjname}}` literally (rewrite preserved the marker).
   - **Contains** `{{description}}` literally.
   - **Contains** `{{domain}}` literally.
   - **Does NOT contain** `bash deploy.sh`.
   - **Does NOT contain** `./deploy.sh`.
   - **Does NOT contain** `scp -r .vibewarden/bundle/*` (the buggy
     pre-#1217 form).
   - **Does NOT contain** `vwprjname` (rewrite removed all sentinels).

The test runs as part of `make check` so any change to the templates that
breaks the artifact contract fails before reaching a release.

### Wrapper-script test

The wrapper script `scripts/release/emit-kickoff-artifacts.sh` is
exercised end-to-end by the `release-dryrun` workflow on every PR that
touches goreleaser config or the wrapper itself (path filter already in
place). No new workflow needed.

A trivial Go test (`internal/app/promptkickoff/wrapper_script_test.go`)
shells out to the wrapper script in a `t.TempDir()` working tree and
diffs its output against the in-process `Render`+rewrite path: this
guarantees the shell wrapper does not silently drift from the Go test's
expectations. Skipped on Windows (`runtime.GOOS == "windows"`) because
the wrapper is bash-only.

### Architectural-invariant test

No new architecture test required. `internal/domain/` and `internal/app/`
are unchanged.

## New dependencies

None. The wrapper script uses only `bash`, `sed`, and the freshly-built
vibew binary — all already required by the release pipeline. GoReleaser is
already a dependency (MIT-licensed, approved per CLAUDE.md §Dependency
rules).

## Documentation surfaces

| File | Update |
|------|--------|
| `decisions/README.md` | Add ADR-101 row. |
| `CHANGELOG.md` | Unreleased / Added entry pointing at #1232. |
| `docs/agent-kickoff.md` | Add subsection "Consuming via release artifacts" with the two URL patterns and the placeholder substitution recipe. Add to the "Reference" list. |
| `llms-full.txt` § 21 | One-paragraph note: "If your tooling cannot ship a vibew binary, fetch the same canonical content from `https://github.com/vibewarden/vibewarden/releases/latest/download/agent-kickoff-{dev,deploy}.txt` and substitute `{{prjname}}`, `{{description}}`, `{{domain}}` against your inputs." |
| `CLAUDE.md` § Content authority | Append `agent-kickoff-{dev,deploy}.txt` to the canonical-content list so future contributors know these artifacts are owned by the main repo. |

The website-side companion is filed separately
(`vibewarden/vibewarden.dev:#TBD`) and depends on this PR landing first so
that the artifact URLs resolve at the next release.

## Consequences

### Positive

- Three-retro recurrence resolved at the structural level. Drift between
  website prompt and binary prompt is no longer possible: both come from the
  same `text/template` rendering.
- The kickoff prompt joins `llms.txt`, `llms-full.txt`, and
  `vibewarden.reference.yaml` under a single, locked content-authority
  pattern. Future canonical content additions follow the same playbook.
- Every future deploy-contract change automatically propagates to the
  website on the next release tag — zero website code changes required for
  that class of update.
- Forensic CI test forbids regressions to known-bad forms (`bash deploy.sh`,
  `scp -r .../*`). Future deploy-contract bugs fail at PR time, not in
  production retros.

### Negative / trade-offs

- The release pipeline now has a `before` hook that builds vibew once for
  the artifact step. Negligible — adds a few seconds to a release that
  already takes minutes for Docker buildx + multi-arch.
- Snapshot builds emit kickoff artifacts that are not uploaded anywhere.
  This is desirable (validates the wrapper) but does mean `dist/` has more
  files in dryrun. No user impact.
- The placeholder-sentinel dance adds a small amount of complexity to the
  wrapper script. Trade-off accepted to avoid leaking release tooling into
  `SanitizeProjectName`.

### Future implications

- Establishes the precedent that release artifacts can be *generated* from
  the binary, not just *checked in*. Future canonical content (e.g.
  per-version OpenAPI snapshots, version-stamped diagnostic checklists)
  can follow the same pattern.
- Locks the public URL contract:
  `https://github.com/vibewarden/vibewarden/releases/latest/download/agent-kickoff-{dev,deploy}.txt`.
  Any future renaming of the artifacts is a breaking change for the
  website and any third-party tools that picked up the convention. Names
  must be treated like a public API.
- Constrains every future tool that wants to surface the kickoff prompt:
  fetch from these URLs; do not hand-roll a template. Document the
  constraint inside `CLAUDE.md` § Content authority and reject reviews
  that violate it.
