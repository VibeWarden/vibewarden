# ADR-088: deploy.sh runs locally — scp + ssh + healthcheck in one script

**Date**: 2026-04-20
**Issue**: [#1087](https://github.com/VibeWarden/vibewarden/issues/1087)
**Status**: Accepted
**Supersedes the deploy.sh / docs alignment implied by**: ADR-085 (vibew bundle
compose-only), ADR-086 (sunset vibew deploy)

## Context

The generated `deploy.sh` emitted by `internal/app/bundle/bundle_extras.go::renderDeploySH`
is written to run **locally** — it does `scp` + `ssh` against an argv-supplied
`<user@host>`. But the bundle's own `README.md` (from `renderBundleReadme`),
`docs/guide/bundle-to-vps.md`, `docs/deploy-reference.md`, the static
`docs/examples/AGENTS-VIBEWARDEN.md`, `internal/cli/templates/agents/agents-vibewarden.md.tmpl`,
`README.md`, and `llms-full.txt` all tell the user to **ssh into the VPS and
run it server-side**: `ssh user@host 'cd ~/bundle && bash deploy.sh'`.

Run that way, the script `scp`s the server to itself. The "Next:" hint printed
by `internal/cli/cmd/bundle.go:200` advertises the local form, so half of the
surfaces contradict the other half within a single bundle. Retro 2026-04-21
confirmed the user hit this, abandoned the script, and shipped with two manual
commands (`scp -r …` + `ssh … 'docker load && docker compose up -d'`) — which
is exactly what a correct local-run script should automate.

The PM spec (posted 2026-04-20 on #1087) locks the **local-run** convention
(Option A). This ADR records the technical design for implementing it and the
exhaustive list of documentation surfaces that must be realigned in the same
PR so the bundle stops contradicting itself.

Options considered and rejected:

- **Option B (remote-run)** — forces every doc to add a mandatory prior `scp`
  step before the ssh invocation; same number of user actions but with a
  `deploy.sh` that cannot stand alone. Worse UX.
- **Option C (both via `--local`/`--remote` flags)** — doubles the test matrix
  and keeps two contradictory semantics alive in the same file. Rejected.

No new `vibew deploy` subcommand — that is tracked separately in #1092 and
remains deferred.

## Decision

Rewrite `renderDeploySH` to emit a local-run script. Rewrite `renderBundleReadme`,
the `Next:` hint in `bundle.go`, and every documentation surface that shows
the deploy recipe so all of them converge on exactly one incantation from the
operator's workstation:

```bash
cd .vibewarden/bundle && ./deploy.sh user@host[:/remote/path]
```

Add one `//go:build integration` test that end-to-end exercises the script
against a real `sshd` (skipped when the harness is not available — mirrors
`TestMultiSite`'s pattern).

### Domain model changes

None. This is a render-layer + documentation change. No new entities, value
objects, or domain events.

### Ports (interfaces)

No new ports. Existing `ports.BundleFS` and `ports.ImageSaver` are reused.

### Adapters

No new adapters. The integration test wires a thin local SSH harness directly
inside `test/integration/`; the harness is not a reusable adapter.

### Application service

`renderDeploySH` gains one parameter: the healthcheck port, baked into the
script literal at render time. `Service.writeBundleExtras` already holds the
merged `*config.Config` in `opts.Config`; reading `opts.Config.Server.Port` is
sufficient — no signature churn on `BundleOptions`.

Signature change:

```go
// before
func renderDeploySH(projectName string, skipImage bool) string

// after
func renderDeploySH(projectName string, skipImage bool, healthPort int) string
```

Call site in `writeBundleExtras` becomes:

```go
healthPort := 8443
if opts.Config != nil && opts.Config.Server.Port > 0 {
    healthPort = opts.Config.Server.Port
}
deployBody := renderDeploySH(projectName, opts.SkipImage, healthPort)
```

No change to the `BundleOptions` contract, no change to the `ImageSaver` port,
no change to call sites in `vibew bundle`.

### File layout

Modified files only (no new production files):

- `internal/app/bundle/bundle_extras.go` — rewrite `renderDeploySH` and
  `renderBundleReadme`; pass `healthPort` from `writeBundleExtras`.
- `internal/app/bundle/bundle_extras_test.go` — replace the existing golden-ish
  assertions with a table-driven suite that covers `{skipImage=true|false}` ×
  `{host only, host:/remote/path}`; add a `bash -n` syntax check on the rendered
  output and a golden-file comparison (two fixtures under
  `internal/app/bundle/testdata/deploy_sh/{with_image,skip_image}.sh`).
- `internal/cli/cmd/bundle.go` — rewrite the `Next:` hint line 200.
- `docs/guide/bundle-to-vps.md` — collapse sections 2 + 3 into "Deploy with
  deploy.sh" with the one-liner; retain "Day-two operations" and
  "Troubleshooting"; drop the three-line `scp -r` + `ssh … bash deploy.sh`
  remote-run recipe at the top.
- `docs/deploy-reference.md` — rewrite the "Migration — three steps" block
  (lines 26–40) to the local-run form; update the first-row example in the
  "What changed" table so the after-cell matches.
- `internal/cli/templates/agents/agents-vibewarden.md.tmpl` — rewrite the
  "Deploying to a VPS" section (currently lines 169–183) to: `vibew build
  --platform linux/amd64` → `vibew bundle` → `cd .vibewarden/bundle && ./deploy.sh
  user@host`. Keep the surrounding paragraphs.
- `docs/examples/AGENTS-VIBEWARDEN.md` — same rewrite (this file is the
  rendered exemplar of the template above; both must be updated in the same
  commit or the template-vs-exemplar test trips).
- `llms-full.txt` — step 7 at line 1156 and the recap at line 1168 both become
  the local-run form.
- `README.md` — no change needed (it links to the walkthrough, doesn't inline
  the commands). Verify during implementation; if a command fragment is
  inlined elsewhere in the file, update it.
- `CHANGELOG.md` — add a "Changed" bullet under Unreleased: "Bundle `deploy.sh`
  now runs locally (scp + ssh + healthcheck in one command). The remote-run
  form in previous docs was a bug — see ADR-088."
- Integration test: `test/integration/bundle_deploy_sh_test.go` — new,
  `//go:build integration` gated, auto-skips when no local `sshd` + `docker`
  is usable.

New fixture files:

- `internal/app/bundle/testdata/deploy_sh/with_image.sh` — expected bytes for
  `renderDeploySH("myproject", false, 8443)`.
- `internal/app/bundle/testdata/deploy_sh/skip_image.sh` — expected bytes for
  `renderDeploySH("myproject", true, 8443)`.

### Script body (exact render target — ~25 lines)

The reference output for `(projectName="myproject", skipImage=false, healthPort=8443)`:

```bash
#!/usr/bin/env bash
# Reference deploy script generated by `vibew bundle` for myproject.
# Runs LOCALLY. Ships this bundle to user@host, loads the image, brings
# the stack up, and probes /_vibewarden/health. Edit freely — vibew
# will never modify this file.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <user@host[:/remote/path]>" >&2
  exit 1
fi

TARGET="$1"
USER_HOST="${TARGET%%:*}"
REMOTE_PATH="${TARGET#*:}"
if [[ "$REMOTE_PATH" == "$TARGET" ]]; then REMOTE_PATH="~/vibewarden-bundle"; fi

scp -r . "$USER_HOST:$REMOTE_PATH/"
ssh "$USER_HOST" "cd $REMOTE_PATH && docker load -i image.tar && docker compose up -d"
sleep 3
if ! ssh "$USER_HOST" "curl -fsSL -m 10 http://localhost:8443/_vibewarden/health" >/dev/null; then
  ssh "$USER_HOST" "cd $REMOTE_PATH && docker compose logs --tail 50" >&2
  echo "deploy.sh: healthcheck failed — dumped last 50 log lines above" >&2
  exit 1
fi
echo "deploy.sh: healthy ($USER_HOST:$REMOTE_PATH)"
```

For `skipImage=true` the single `docker load -i image.tar && ` fragment is
replaced with `docker compose pull && ` (matches the current `loadCmd` branch).

Script contract details:

- Shebang `#!/usr/bin/env bash`, mode `0o750` (unchanged).
- `set -euo pipefail` at top.
- Exactly one positional arg `user@host[:/remote/path]`. Zero or >1 → print
  the usage line to stderr and `exit 1`. The usage line is part of the golden
  output.
- Default remote path: `~/vibewarden-bundle` (hardcoded literal, not resolved
  at render time).
- Single `scp -r .` of the bundle directory (not a tar+scp — the previous
  `tar czf bundle.tar.gz .` step is removed, per PM answer to open question
  #2). The caller must `cd` into the bundle first — the `Next:` hint and
  README both reflect this.
- One `ssh` invocation that runs `docker load && docker compose up -d` (or
  `docker compose pull && docker compose up -d` under `--skip-image`).
- `sleep 3` then a second `ssh` that runs `curl -fsSL -m 10` against
  `http://localhost:<healthPort>/_vibewarden/health`. The `-m 10` cap prevents
  hung probes. Path is `/_vibewarden/health` — the canonical endpoint per
  `internal/mcp/tools.go:114`, `internal/app/ops/status.go:89`,
  `internal/adapters/caddy/config_test.go:1213` (PM open question #3 answered).
- On probe failure: a third `ssh` dumps `docker compose logs --tail 50` to
  stderr, then a one-line error to stderr, then `exit 1`.
- Deterministic: no timestamps, no random values. Three inputs
  `(projectName, skipImage, healthPort)` → one byte-for-byte identical output.

### README body (same file, same function)

`renderBundleReadme` collapses the contradictory three-step block into one
step. Keep the h1, "What this is" paragraph, and "Rebuild for a different
arch" section. The new "Deploy" section:

```markdown
## Deploy

From the directory that holds this README:

    ./deploy.sh user@host                              # default remote path: ~/vibewarden-bundle
    ./deploy.sh user@host:/srv/<projectName>           # custom remote path

`deploy.sh` runs locally. It `scp`s this directory to the host, loads the
image (or `docker compose pull`s when the bundle was built with
`--skip-image`), runs `docker compose up -d`, and probes
`/_vibewarden/health` on the remote. Non-zero exit on any failure, with the
last 50 log lines dumped to stderr for diagnosis.
```

### "Next:" hint (bundle.go:200)

```go
fmt.Fprintf(out, "Next: cd %s && ./deploy.sh user@host  (runs locally — scps bundle, loads image, brings stack up, probes /_vibewarden/health)\n", absOut)
```

### Sequence (happy path)

1. User runs `vibew bundle` from project root. Exits 0; prints
   `Next: cd .vibewarden/bundle && ./deploy.sh user@host`.
2. User runs `cd .vibewarden/bundle && ./deploy.sh root@vps.example.com`.
3. Script parses `user@host` → `USER_HOST=root@vps.example.com`,
   `REMOTE_PATH=~/vibewarden-bundle`.
4. `scp -r . "$USER_HOST:$REMOTE_PATH/"` copies the entire bundle dir
   (`image.tar` included when present).
5. First `ssh`: `cd <path> && docker load -i image.tar && docker compose up -d`.
6. `sleep 3` → second `ssh` probes `/_vibewarden/health` via `curl -fsSL -m 10`.
7. On HTTP 200: prints `deploy.sh: healthy …`, `exit 0`.
8. On probe failure: third `ssh` runs `docker compose logs --tail 50`,
   dumps output to stderr; prints final error line; `exit 1`.

### Error cases

| Condition | Detection | Exit | User-visible output |
|---|---|---|---|
| Zero or >1 positional arg | `[[ $# -ne 1 ]]` | 1 | `usage: ./deploy.sh <user@host[:/remote/path]>` on stderr |
| SSH auth failure | `set -e` under `scp` | non-zero from ssh | raw scp/ssh stderr (user's `~/.ssh/config` governs) |
| Docker load/compose failure | `set -o pipefail` under the first ssh | non-zero | remote docker stderr + `set -e` aborts before the healthcheck |
| Healthcheck non-200 / timeout | `curl -fsSL -m 10` returns non-zero | 1 | `docker compose logs --tail 50` on stderr, then `deploy.sh: healthcheck failed — dumped last 50 log lines above` |
| `image.tar` missing (non-skip-image bundle) | detected by the first `ssh`'s `docker load` | non-zero | remote docker stderr |

SSH-key setup, agent forwarding, and host-key acceptance are out of scope —
the script assumes `~/.ssh/config` already works. Documented in the README
and in `docs/guide/bundle-to-vps.md`.

### Test strategy

**Unit tests** (`internal/app/bundle/bundle_extras_test.go`, no build tag):

- Golden-file comparison: render for `(myproject, skipImage=false, 8443)` and
  `(myproject, skipImage=true, 8443)`, byte-compare against the fixtures under
  `testdata/deploy_sh/`. Regenerate fixtures via `go test -update` only on
  intentional format changes.
- `bash -n` syntax check: `exec.Command("bash", "-n", …)` on each rendered
  body. Auto-skip when `bash` is not in `$PATH` (Windows CI without WSL).
  Uses the same skip pattern as `TestMultiSite`.
- Table-driven arg-parsing smoke: run the rendered script under `bash` with
  `$# = 0, 1, 2` and a fake `scp`/`ssh` on `$PATH`. Assert the usage message
  and exit codes. Fake binaries live in `t.TempDir()` and are prepended to
  `$PATH` for the duration of the test.
- Determinism: call `renderDeploySH` twice with the same inputs, assert
  byte-equality. Mirrors the pattern in `bundle_determinism_test.go`.
- `renderBundleReadme`: golden-string check for the single-step "Deploy"
  section and absence of the old contradictory lines (negative assertions
  on `ssh user@host 'cd ~/bundle && bash deploy.sh'`).

**Integration test** (`test/integration/bundle_deploy_sh_test.go`,
`//go:build integration`):

- **Harness option chosen: (c) — shell out to a local `sshd` when present,
  skip otherwise.** Rationale: testcontainers-go-based sshd+DinD is heavy and
  flaky on CI macOS runners; a mock SSH server would re-implement enough of
  openssh to be a second bug farm. The TestMultiSite pattern already
  auto-skips on missing prerequisites — reuse it.
- Skip sentinel: attempt `exec.LookPath("sshd")` AND `exec.LookPath("docker")`
  AND verify `ssh -o BatchMode=yes localhost true` succeeds against a
  harness-started sshd bound to a loopback port. Any failure → `t.Skipf(...)`
  with an actionable message ("install openssh-server and docker, or run
  with --tags=integration on a Linux host with docker").
- Harness shape: start an ephemeral `sshd` on a random loopback port with a
  per-test host key and an authorized ED25519 keypair generated in `t.TempDir()`.
  Inject the keypair via `GIT_SSH_COMMAND`-style env wrappers so the script
  picks it up without touching the user's `~/.ssh/`.
- Assertions:
  1. `./deploy.sh testuser@127.0.0.1:$tmpdir` exits 0.
  2. Bundle dir was `scp`ed to the remote tempdir (check a marker file).
  3. `docker compose up -d` was invoked (stub the remote `docker` binary with
     a logging shell script on the sshd's PATH; assert the log contains
     `compose up -d`).
  4. The healthcheck HTTP probe reached the expected port (stub
     `curl` similarly OR run a tiny in-process HTTP server on the expected
     port and assert one GET on `/_vibewarden/health`).
- Runtime budget: < 15s when the harness is available; 0s (skip) otherwise.

**Rationale for the skip pattern over testcontainers-go**: macOS CI lacks a
Linux kernel, making Docker-in-Docker inside a testcontainer brittle; our
existing `TestMultiSite` already skips gracefully in that environment and the
architect prefers one consistent skip sentinel across the repo over a second
one layered on top of a heavier harness.

**Documentation of the skip pattern**: a one-paragraph note added to
`CLAUDE.md`'s Testing section explaining that integration tests requiring
SSH/Docker prerequisites auto-skip with `t.Skipf` rather than failing, and
that the canonical skip message format is `"integration prerequisite missing:
<tool> not on PATH — install it or run under make integration"`. This is
docs-only, no code enforcement.

### New dependencies

None. The script uses `bash`, `scp`, `ssh`, `curl` — already assumed by the
existing guide. The unit test uses only `bash` (stdlib `os/exec`). The
integration test uses `openssh-server` and `docker` on the host — neither is
a Go module dependency; both are declared skip prerequisites.

No new Go modules are added. License scan: N/A (no new deps to vet).

## Consequences

**Positive**:

- One incantation, one script, one set of docs. No more docs-vs-runtime
  drift on this surface (the #1 retro finding, three times over).
- The retro user's working manual path (`scp -r` + `ssh docker load && compose
  up -d`) is now the script. Removes the motivation to abandon `deploy.sh`
  for hand-typed commands.
- Healthcheck baked in means the user sees `healthy` or `failed` immediately,
  without a second `ssh docker compose logs` round-trip.
- Script remains small (~25 lines, well under the 30-line PM cap), auditable
  by the vibe coder in one screen.

**Trade-offs**:

- `scp -r .` is slower than `rsync --delete` over high-latency links. The
  guide will still mention `rsync` as a manual redeploy alternative. For v1
  we keep the script on `scp` because it is already in everyone's PATH and
  the user is scp'ing a bundle that's typically <200 MB (image.tar dominates).
- Skipping the tar step means `scp` walks the tree, producing more round trips
  per small file. Acceptable for v1; revisit if perf complaints arise (none
  to date).
- Integration test auto-skips on most developer laptops. CI must run it on
  at least one Linux runner; update `make integration` docs accordingly. This
  matches the existing `TestMultiSite` reality.

**Forward implications**:

- `vibew deploy` (#1092) can later become a thin wrapper that calls
  `vibew bundle` then `bash deploy.sh "$@"` — no work lost here.
- If we ever add `--rsync` / `--concurrent-hosts` flags, they become new args
  to the script; the single-arg usage contract is the hinge.
- The `healthPort` baking pattern (merged config → literal) is the cleanest
  way to plumb any future config-dependent script values (e.g. a
  user-supplied healthcheck path). Document in a follow-up ADR if we add
  more.

**Non-goals reaffirmed** (already locked by the PM spec):

- No `vibew deploy` subcommand.
- No rsync-based delta transfers inside the script (`scp -r .` only).
- No multi-host fan-out, zero-downtime rollouts, or blue/green.
- No SSH key setup automation.
- No Windows PowerShell port (Bash only; WSL is the Windows path).
- No changes to the bundle directory layout or `image.tar` contract.
