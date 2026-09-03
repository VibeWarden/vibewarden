# ADR-112: npm distribution — `@vibewarden/cli`, a zero-dependency wrapper that downloads the goreleaser binary

**Date**: 2026-09-03
**Issue**: #1258 (child of epic #664)
**Status**: Accepted

## Context

The install path today is `curl -fsSL https://vibewarden.dev/install.sh | sh` (`scripts/install.sh`),
plus `vibew upgrade` (`internal/app/upgrade/service.go`) for in-place updates. VibeWarden's
primary audience already has Node installed and reaches for `npm install` before a pipe-to-shell.
Pipe-to-shell also has no version pinning, no registry discoverability, and is a friction
surface for AI agents in sandboxed environments (v0.18.5 Codex retro).

Constraints that shape the design, discovered from the current repo state:

1. **`.goreleaser.yml` builds `linux` and `darwin` only** (`goarch: amd64, arm64`). There is a
   `format_overrides` entry for `goos: windows`, but `windows` is not in the `goos` list, so
   **no Windows binary has ever been published**. The issue body's `windows-amd64` bullet and
   the PM spec's "Windows (amd64)" acceptance criterion contradict the PM spec's own qualifier
   ("the same OS/arch matrix the existing release pipeline already publishes binaries for").
   The published matrix wins; see "Platform matrix" below.
2. Archive naming is `vibewarden_{{.Version}}_{{.Os}}_{{.Arch}}.tar.gz` with `.Version` stripped
   of the leading `v`, checksums in `checksums.txt`, binary name `vibew` inside the archive.
   `internal/app/upgrade/service.go:169` and `scripts/install.sh:142` already depend on this
   exact shape; the npm package becomes the third consumer.
3. Node's stdlib has **no tar reader and no zip reader**. `node:zlib` gives gunzip only.
4. The kickoff prompt Step-1 install command lives in
   `internal/cli/templates/prompts/{dev,deploy}.tmpl`, is rendered by `vibew prompt-template`,
   and is emitted as a release asset by `scripts/release/emit-kickoff-artifacts.sh` (ADR-101).
   Changing the install command is a template edit plus a golden regeneration, not a script edit.
5. npm name availability was verified live on 2026-09-03 against `registry.npmjs.org`:
   `@vibewarden/cli` → 404, `vibewarden` → 404, `vibew` → 404, `vibewarden-cli` → 404.
   The `@vibewarden` org is unclaimed. Open question 1 in the PM spec is resolved.

## Decision

Ship `@vibewarden/cli`, a **zero-dependency** npm package living in the main repo at `npm/`.
`postinstall` downloads the matching platform archive from the GitHub Release for the package's
own version, verifies it against SHA-256 digests **embedded in the published package at publish
time**, extracts `vibew` into `npm/vendor/`, and a small Node shim at `bin/vibew` execs it with
argv passed through. Publication is a new `npm` job in `.github/workflows/release.yml` gated on
`needs: [release]`.

Nothing in `internal/` changes except the two prompt templates and their goldens. There is **no
new port, no new domain type, and no new Go dependency**. The Go binary is untouched, per the
issue's out-of-scope list.

### Why an ADR at all

Below the usual threshold for code changes, but this is a new **distribution channel** with a
new **supply-chain trust boundary** (an npm publish credential and a second place a user's
`vibew` can come from), and it creates a documented conflict with `vibew upgrade`. That is the
same class as ADR-101 (release artifacts), which carries an ADR.

### Platform matrix

| `process.platform` / `process.arch` | GOOS/GOARCH | Archive |
|---|---|---|
| `darwin` / `x64` | `darwin/amd64` | `vibewarden_<v>_darwin_amd64.tar.gz` |
| `darwin` / `arm64` | `darwin/arm64` | `vibewarden_<v>_darwin_arm64.tar.gz` |
| `linux` / `x64` | `linux/amd64` | `vibewarden_<v>_linux_amd64.tar.gz` |
| `linux` / `arm64` | `linux/arm64` | `vibewarden_<v>_linux_arm64.tar.gz` |
| anything else (incl. `win32`) | — | hard error, see Error cases |

**Windows is out of scope for this ADR** because no Windows binary is published. Supporting it
would require (a) adding `goos: windows` to `.goreleaser.yml`, (b) a zip reader in Node (no
stdlib support, and the obvious candidates are ISC/other licenses outside the approved set), and
(c) reworking the shim, since npm on Windows generates a `vibew.cmd` that invokes `node`. The
win32 branch therefore emits an actionable error pointing at WSL and `scripts/install.sh`.
A follow-up issue should track native Windows support end to end.

Alpine/musl needs no special handling: goreleaser builds with `CGO_ENABLED=0`, so the binaries
are static.

`os`/`cpu` fields are deliberately **omitted** from `package.json`. Setting them would make npm
abort with a generic `EBADPLATFORM` before `postinstall` runs, and the PM spec requires a clear,
actionable message. The check lives in `lib/platform.js` instead.

### Version resolution

The package version **is** the release version. `install.js` reads `version` from its own
`package.json` and derives the tag as `v${version}`. Consequences:

- `npm i -g @vibewarden/cli` → npm resolves `latest` → that version's binary. No `latest` logic
  in our code.
- `npm i -g @vibewarden/cli@0.19.0` → exactly `v0.19.0`'s binary.
- `vibew --version` prints `vibew <version>` (cobra `Version` field, set by goreleaser's
  `-X main.version={{ .Version }}`, which is the tag without `v`) — byte-identical to the npm
  package version. The matching acceptance criterion is satisfied structurally, not by a check.
- **No call to `api.github.com` at install time.** One less failure mode and no rate limit,
  unlike `scripts/install.sh` and `vibew upgrade`.

### Checksum verification — embedded, not fetched

The release job writes `npm/checksums.json` from the release's `checksums.txt` **before**
`npm publish`, so the published tarball carries the expected digests:

```json
{
  "version": "0.19.0",
  "archives": {
    "vibewarden_0.19.0_darwin_arm64.tar.gz": "<sha256 hex>",
    "...": "..."
  }
}
```

This is materially stronger than fetching `checksums.txt` at install time: a fetched checksum
only proves transport integrity, since whoever controls the release controls both files.
Embedding chains the digest to npm's own package-tarball integrity, which is what a lockfile
pins. It also removes a second HTTP request from the install path.

Fallback: when `VIBEWARDEN_INSTALL_VERSION` overrides the version (dev/testing), the embedded
digests no longer apply. In that case `install.js` fetches `checksums.txt` from the release and
prints a warning that verification is transport-integrity only. **Verification is never skipped.**

### Shim strategy: spawn, do not overwrite `bin/vibew`

`bin/vibew` stays a Node shim for the life of the install; it `spawnSync`s `vendor/vibew` with
`stdio: 'inherit'`.

Rejected alternative — esbuild's trick of having `postinstall` `rename()` the real binary over
`bin/vibew` for zero-overhead direct exec. Rejected because pnpm/yarn/corepack content-addressed
stores hardlink package files, so overwriting can corrupt the store; because it needs a
second degraded code path when the rename fails; and because the ~40 ms Node startup it saves is
irrelevant for a scaffolding CLI whose commands shell out to Docker anyway. Simplicity wins.

### Publish trust model (PM open question 5)

- Package published under the `@vibewarden` npm org (must be claimed by a human before the first
  release — see Prerequisites).
- **Granular automation token** scoped to publish rights on `@vibewarden/*` only, stored as the
  repo secret `NPM_TOKEN`. Org 2FA set to "require 2FA or automation token".
- `npm publish --provenance` with `permissions: id-token: write`, producing a signed Sigstore
  attestation tying the tarball to this repo, this workflow, and this commit. For a security
  product this is not optional garnish.
- `prepublishOnly` guard fails the publish if the version is still the committed placeholder
  `0.0.0-dev`, so an accidental `npm publish` from a laptop cannot ship a broken package.

## Domain model changes

None. This is a distribution wrapper; it touches no entity, value object, or domain event.

## Ports (interfaces)

None. No new interface in `internal/ports/`. The hexagon is untouched.

## Adapters

None in the Go sense. The npm package is a build/distribution artifact outside the Go module,
analogous to `scripts/install.sh` and `Dockerfile.goreleaser`.

## Application service

None. The only Go-side change is content in two embedded templates.

## File layout

New files:

- `npm/package.json` — name `@vibewarden/cli`, `version: "0.0.0-dev"` placeholder, `bin: {"vibew": "bin/vibew"}`, `scripts.postinstall: "node install.js"`, `scripts.test: "node --test test/"`, `scripts.prepublishOnly`, `license: "Apache-2.0"`, `engines: {"node": ">=22"}`, `repository: {type, url, directory: "npm"}`, `files: ["bin/","lib/","install.js","checksums.json","README.md","LICENSE"]`. No `dependencies`, no `devDependencies`, no `os`/`cpu`.
- `npm/checksums.json` — committed as `{"version":"0.0.0-dev","archives":{}}`; overwritten at publish time.
- `npm/install.js` — postinstall entry point; orchestration only.
- `npm/lib/platform.js` — `process.platform`/`process.arch` → `{goos, goarch, archiveName}`; throws a typed `UnsupportedPlatformError`.
- `npm/lib/download.js` — `https.get` with manual redirect following (max 5 hops), per-request timeout, bounded retries.
- `npm/lib/verify.js` — SHA-256 of the archive buffer via `node:crypto`, lookup and compare against `checksums.json` / a parsed `checksums.txt`.
- `npm/lib/targz.js` — `zlib.gunzipSync` + a minimal tar reader (512-byte headers: name at 0, size octal at 124, typeflag at 156) that extracts one named member. Mirrors esbuild's inline extractor; keeps the dependency count at zero.
- `npm/lib/paths.js` — resolves package root, `vendor/` dir, binary name.
- `npm/bin/vibew` — `#!/usr/bin/env node` shim.
- `npm/README.md` — npm registry landing page (install, pinning, `--ignore-scripts` fallback, mirror env vars, `vibew upgrade` caveat).
- `npm/test/platform.test.js`, `npm/test/verify.test.js`, `npm/test/targz.test.js`, `npm/test/download.test.js`, `npm/test/install.test.js`, `npm/test/fixtures/make-fixture.js`.
- `scripts/release/prepare-npm-package.sh` — publish-time preparation.
- `decisions/adr-112-npm-distribution-vibewarden-cli-wrapper.md` — this file.

Modified files:

- `.github/workflows/release.yml` — new `npm` job, `needs: [release]`.
- `internal/cli/templates/prompts/dev.tmpl`, `internal/cli/templates/prompts/deploy.tmpl` — Step 1.
- `internal/app/promptkickoff/testdata/dev.golden`, `deploy.golden` — regenerated with `UPDATE_GOLDEN=1 go test ./internal/app/promptkickoff/`.
- `internal/app/promptkickoff/golden_test.go` — assert Step 1 carries both the npm command and the curl fallback.
- `Makefile` — `check-npm` target (`node --test npm/test/`), wired into `check`.
- `.gitignore` — `npm/vendor/`, `npm/node_modules/`, `npm/LICENSE` (copied at publish time).
- `README.md`, `llms.txt`, `llms-full.txt` (4 sites), `docs/getting-started.md`, `docs/agent-kickoff.md` — npm as the default install, curl retained as the alternative.
- `docs/upgrading.md` — npm upgrade path and the `vibew upgrade` conflict warning.
- `CHANGELOG.md` — `## [Unreleased] / ### Added` entry.

Not modified: `scripts/install.sh` (explicitly out of scope), `.goreleaser.yml`, anything under
`internal/` other than the two templates and their tests.

## Sequence

Install (`npm i -g @vibewarden/cli[@X.Y.Z]`):

1. npm resolves the version, unpacks the tarball into the global prefix, and links `bin/vibew`.
2. npm runs `postinstall` → `node install.js`.
3. `install.js` reads `version` from its own `package.json`; `VIBEWARDEN_INSTALL_VERSION` overrides it if set. Tag = `v${version}`.
4. `lib/platform.js` maps the platform. Unsupported → throw, exit 1.
5. `VIBEWARDEN_SKIP_DOWNLOAD=1` → log the reason and exit 0, leaving the shim to emit the actionable "binary not installed" error on first run.
6. Build the URL: `${VIBEWARDEN_BINARY_MIRROR ?? "https://github.com/vibewarden/vibewarden/releases/download"}/${tag}/${archiveName}`.
7. `lib/download.js` GETs into memory, following redirects (GitHub 302s to `objects.githubusercontent.com`), 30 s per-request timeout, up to 3 attempts with backoff on network errors and 5xx. 404 is **not** retried.
8. `lib/verify.js` SHA-256s the buffer and compares against `checksums.json`. Mismatch → throw, exit 1, nothing written.
9. `lib/targz.js` gunzips and extracts the `vibew` member to `vendor/vibew.tmp-<pid>` inside the package dir.
10. `chmod 0o755`, then `fs.renameSync` to `vendor/vibew` (same filesystem, atomic; a partially-written file is never reachable at the final path).
11. Print a one-line success with the resolved version and platform.

Invocation (`vibew <args>`):

1. npm's global `vibew` link resolves to `bin/vibew`; Node runs the shim.
2. Shim `statSync`s `vendor/vibew`. Missing → print the "binary not installed" message including the literal, fully-resolved `node /abs/path/install.js` command, exit 1.
3. `process.on('SIGINT', () => {})` so the shim does not die before the child; the Go binary owns shutdown.
4. `spawnSync(vendorBin, process.argv.slice(2), {stdio: 'inherit'})`.
5. `process.exit(res.status ?? 1)`; if `res.signal` is set, re-raise it after resetting the handler.

Publish (tag `v*` pushed):

1. `release` job runs goreleaser, creating the GitHub Release with archives, `checksums.txt`, and the ADR-101 kickoff artifacts.
2. `npm` job (`needs: [release]`) checks out, sets up Node 24 with `registry-url: https://registry.npmjs.org`.
3. `scripts/release/prepare-npm-package.sh "$GITHUB_REF_NAME"`:
   a. `VERSION=${TAG#v}`.
   b. If `npm view "@vibewarden/cli@$VERSION" version` succeeds → emit `skip=true` and stop. Makes re-running a release workflow idempotent instead of failing on npm's 403.
   c. `gh release download "$TAG" --pattern checksums.txt`.
   d. `npm version --no-git-tag-version "$VERSION"` inside `npm/`. Uses the tool rather than `sed` on JSON — same lesson as the yamlmod rule in CLAUDE.md.
   e. Parse `checksums.txt` into `npm/checksums.json`, keeping only the four supported archive names. Fail if any of the four is absent from `checksums.txt` — that means the release matrix changed underneath us.
   f. `cp LICENSE npm/LICENSE`.
   g. Emit `dist_tag=next` if `$VERSION` contains `-` (goreleaser sets `prerelease: auto`), else `latest`. **A prerelease tag must not land on `latest`.**
4. `node --test npm/test/` — the package's own tests run against the prepared package.
5. `npm publish --access public --provenance --tag "$DIST_TAG"` in `npm/`, with `NODE_AUTH_TOKEN: ${{ env.NPM_TOKEN }}`.

`NPM_TOKEN` is lifted to a job-level `env` mapping so the publish step can gate on
`env.NPM_TOKEN != ''`. Until the org is claimed and the secret exists, the job runs
the prepare and test steps, emits a workflow notice, and skips publishing. An
unauthenticated `npm publish` would otherwise fail the whole Release workflow on
the next `v*` tag.

## Error cases

| Case | Handling |
|---|---|
| Unsupported platform (`win32`, `arm`, `s390x`, …) | `install.js` exits 1: detected platform/arch, the four supported combinations, and for `win32` a pointer to WSL and `curl -fsSL https://vibewarden.dev/install.sh \| sh`. |
| Network unreachable / DNS failure / timeout | 3 attempts with backoff, then exit 1 naming the URL and the underlying syscall error. Not a generic "install failed". |
| HTTP 404 on the archive | No retry. Exit 1 stating the release or asset does not exist for this version+platform, with the exact URL. Most likely a yanked release or a mismatched `VIBEWARDEN_INSTALL_VERSION`. |
| HTTP 5xx | Retried; after exhaustion, exit 1 with the status code. |
| Checksum mismatch | Exit 1 with expected vs actual digest and the archive name. The buffer is discarded; nothing is written to `vendor/`, so **no partially-verified binary is ever executable**. |
| Redirect loop / >5 hops | Exit 1 naming the final URL. |
| `EACCES` writing `vendor/` | Common when `npm i -g` runs as root and npm drops privileges for lifecycle scripts. Exit 1 with the target path plus the two real remedies: a user-owned npm prefix (`npm config set prefix ~/.npm-global`), or `sudo chown -R "$(whoami)" "$(npm root -g)"`. Not `--unsafe-perm`: npm 9 removed that flag, so on any Node >= 22 the hint itself fails with `Unknown cli config`. |
| Archive present but missing the `vibew` member | Exit 1 naming the archive; indicates a release-packaging regression. |
| `--ignore-scripts` (postinstall never ran) | Shim prints the literal resolved `node /abs/path/install.js` command and exits 1. Also documented in `npm/README.md` and `docs/getting-started.md`. |
| Version already on npm | `prepare-npm-package.sh` skips the publish; the workflow succeeds. |
| Placeholder version reaches `npm publish` | `prepublishOnly` fails the publish. |
| A supported archive missing from `checksums.txt` | `prepare-npm-package.sh` fails the release job loudly rather than shipping a package that cannot verify. |

## Test strategy

**Node unit tests** (`node --test npm/test/`, stdlib test runner, no framework dependency):

- `platform.test.js` — table-driven over every `platform`×`arch` pair including the unsupported ones; asserts the exact archive name and that the error names the platform.
- `targz.test.js` — builds a `.tar.gz` fixture in-process (`make-fixture.js`), asserts round-trip extraction, a missing member, and a truncated archive.
- `verify.test.js` — matching digest, mismatched digest, digest absent from the map, malformed `checksums.txt` line.
- `download.test.js` — against a `node:http` server started by the test: happy path, one 302 hop, a redirect loop, 404 (assert **no** retry), 500 then 200 (assert retry succeeds), and a hanging response (assert the timeout fires).
- `install.test.js` — end-to-end with `VIBEWARDEN_BINARY_MIRROR` pointed at the local server serving a real fixture archive; asserts `vendor/vibew` exists with mode `0755`, and that on a corrupted archive `vendor/` is left empty.

The `VIBEWARDEN_BINARY_MIRROR` override is the testability lever: without it every test would
need the network. It must be honoured by `install.js` from day one, not bolted on.

**Go tests** — existing suites cover the template change once goldens are regenerated. Add one
case to `golden_test.go`'s `TestDeployTemplate_ContainsRequiredCommands` asserting Step 1
contains both `npm i -g @vibewarden/cli` and the curl fallback, so a future edit cannot silently
drop the non-Node path. `internal/app/promptkickoff/wrapper_script_test.go` and
`release_artifact_test.go` re-run unchanged and cover the emitted artifacts.

**Not automated** — the cross-platform smoke matrix (`npm i -g @vibewarden/cli@<v> && vibew --version`
on macOS arm64/amd64 and Linux amd64/arm64). Record the results in the PR body. Automating it
needs release-time runners the project does not have.

**Test placement** — `npm/test/` sits with the code it tests, consistent with ADR-087.

## New dependencies

**None.** No runtime dependency, no install-time dependency, no devDependency. `install.js` uses
`node:https`, `node:zlib`, `node:crypto`, `node:fs`, `node:path`; tests use `node:test` and
`node:http`. This satisfies the PM spec's final acceptance criterion trivially and avoids a
license waiver: the obvious tar/zip helpers (`tar`, `adm-zip`, `yauzl`) are ISC or similar,
outside the approved Apache-2.0 / MIT / BSD-2 / BSD-3 set, and would each need an ADR-109-style
waiver for a hundred lines of code we can write ourselves.

New CI actions: `actions/setup-node` (MIT). Already-approved tooling family.

## Prerequisites (human, before the first release that includes this)

1. Claim the `@vibewarden` npm org. Verified unclaimed 2026-09-03.
2. Create a granular automation token scoped to `@vibewarden/*` publish; store as repo secret `NPM_TOKEN`.
3. These block only the publish step. Releases still succeed without them: the `npm` job skips publishing and emits a notice when `NPM_TOKEN` is unset. All code and tests are developable and verifiable locally via `npm pack` plus a local install against a mirror.

## Consequences

**Good**

- Version pinning, lockfile-able installs, registry discoverability, no pipe-to-shell trust ask.
- Embedded checksums plus npm provenance make this the **most** verifiable install path VibeWarden offers — stronger than `install.sh` and `vibew upgrade`, both of which fetch their checksums from the same host as the artifact.
- Zero dependencies means the supply-chain surface added is exactly one credential and one registry, with no transitive npm tree.
- No `api.github.com` call at install time removes the rate-limit failure mode that `install.sh` has.

**Costs and risks**

- **Fourth consumer of the archive-naming contract.** `vibewarden_<version>_<os>_<arch>.tar.gz` is now depended on by `scripts/install.sh:142`, `internal/app/upgrade/service.go:169`, and `npm/lib/platform.js`. Renaming archives in `.goreleaser.yml` breaks all three. `prepare-npm-package.sh` step 3e fails loudly if the expected names vanish from `checksums.txt`, which is the tripwire for exactly this.
- **`vibew upgrade` conflicts with npm-managed installs.** `ResolveExecutablePath()` resolves to `npm/vendor/vibew`, so `vibew upgrade` succeeds but silently desynchronises the binary from the npm package version, and the next `npm i -g` reverts it. Fixing this properly means teaching the Go binary about its install method, which the issue puts out of scope. **Mitigation for this ADR: document it in `docs/upgrading.md` and `npm/README.md`; open a follow-up issue** for an install-method marker (e.g. a sentinel file next to the binary) that makes `vibew upgrade` refuse and point at `npm i -g @vibewarden/cli@latest`.
- ~40 ms Node startup on every `vibew` invocation. Acceptable, and reversible later via the esbuild rename trick if it ever matters.
- Node becomes required to run the full `make check`. Reasonable for a repo that now ships an npm package.
- Two publish targets per release means two ways a release can be half-shipped. The npm job runs after the GitHub Release exists, so the failure mode is "release exists, npm lags", never "npm package points at a release that does not exist". Re-running the workflow is safe because the publish is idempotent.
- **Windows remains unserved.** The PM spec asked for it; no binary exists to serve. Tracked as a follow-up rather than silently designed around.
