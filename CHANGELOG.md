# Changelog

All notable changes to VibeWarden are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

Future releases are generated automatically via [goreleaser](https://goreleaser.com/).
This initial entry was written by hand to summarise the work leading up to v0.1.0.

---

## [Unreleased]

### Migrating from v0.17.0

This release trims CLI surface and tightens validation. Most users upgrading from v0.17.0 will be unaffected; the following breaking-ish changes have direct migration paths:

| If you used (v0.17.0) | Use instead (v0.18.0) |
|-----------------------|------------------------|
| `vibew dev --observability` | `vibew dev` then `vibew obs up` (and `vibew obs down` to tear it down) |
| `vibew restart` | `vibew build && vibew dev` (rebuild + restart) or `vibew down && vibew dev` (clean restart) |
| `cd .vibewarden/bundle && ./deploy.sh user@host` | `ssh user@host mkdir -p /path` → `scp -r .vibewarden/bundle/* user@host:/path/` → `ssh user@host "cd /path && docker load -i image.tar && docker compose up -d"` → verify with `curl https://yourdomain/_vibewarden/health` (port **443** in production, not 8443). The bundle's `README.md` documents this contract. |
| `vibew init` produced a `Dockerfile` placeholder | Write your own Dockerfile against the contract in `AGENTS-VIBEWARDEN.md` §Dockerfile contract (alpine base, `EXPOSE` matches `upstream.port`, no `HEALTHCHECK`, multi-stage for compiled languages, builder image major.minor matches your toolchain manifest). `vibew doctor` validates the Dockerfile against this contract. |
| Multi-site projects (`sites/<name>/vibewarden.yaml`) ran end-to-end | Multi-site is post-v1. `vibew validate` and `vibew add` now refuse multi-site configs; the production-deploy architecture is tracked in #1169. Multi-site `vibew dev` (local reverse-proxy of N apps) keeps working. |
| `.env` with `VIBEWARDEN_APP_IMAGE=vibewarden-app:latest` (legacy tag) | `vibew validate` now FAILs on the legacy tag. Run `vibew bundle --overwrite` to regenerate `.env` with the project-scoped tag, or edit by hand. |

If `vibew validate` or `vibew doctor` start FAILing on a previously-clean project, the most likely causes are above. Each FAIL row carries an actionable hint pointing at the fix.

### Added

- **`vibew doctor` Dockerfile lint** (#1140). Doctor now validates user-written Dockerfiles against the contract documented in AGENTS-VIBEWARDEN: alpine base, `EXPOSE` matches `upstream.port`, no `HEALTHCHECK` directive, non-root `USER` (warn), multi-stage build for compiled languages, and builder image major.minor matches the project's toolchain manifest (`go.mod`, `.nvmrc`, `pyproject.toml`). The toolchain-match check pre-empts the qr-dali deploy retro's #1 opaque error: `go mod download exit 1` masquerading as a build failure when really it's a Go-version mismatch between the Dockerfile and the project. Closes the loop on #1139's "stop generating placeholder Dockerfile" decision.

- **`vibew obs up` / `vibew obs down`** (#1149) — bring up / tear down the Prometheus + Grafana observability profile against the running dev stack. Uses Docker Compose profiles; the obs services share the same compose project as the main stack but are gated by the `observability` profile. Run `vibew dev` first, then `vibew obs up`.

- **`vibew bundle` now prints a "Sensitive files in this bundle" awareness block** (#1142, [ADR-094](decisions/adr-094-bundle-sensitive-files-awareness-block.md)). After writing the bundle, `vibew bundle` scans the output directory and prints a stable awareness block listing any credential or key files (`.env`, `.credentials`, `kratos/secrets`, `*.pem`, `*.key`, `*.token`) when they are present. The block appears after the `Contents:` listing and before the `Next:` hint. The block is omitted entirely when no sensitive files are detected — no flag, no env var, no opt-out. The bundle `README.md` gains a `## Secrets` section documenting the credential surface.

### Fixed

- **`vibew bundle --skip-image` now works on a fresh `vibew init` project** (#1178). The generated `vibewarden.production.yaml` carried a stale `tls: provider: letsencrypt` block that #1145 was supposed to remove. With no `tls.domain` set, validation failed at bundle time with an opaque `tls.domain is required when tls.provider is "letsencrypt"`. Fix: production template strips the `tls:` block (it was only relevant after `vibew add tls --domain ...`); template test asserts the absence as a regression guard; bundle's error wrapper now hints at `vibewarden.production.yaml` so any user who DOES set `tls.provider` without a domain knows where to look.

- **`vibew obs up` / `vibew obs down` actually work** (#1176, #1177, [ADR-097](decisions/adr-097-obs-up-down-fix.md)). Two release-blocker bugs in v0.17.0's #1149: (a) `vibew obs up` silently no-opped because the generated compose only included the obs services when `cfg.Observability.Enabled = true` — the profile gate was never hit; (b) `vibew obs down` stopped the entire project because compose's `--profile X` flag doesn't scope `down`. Fix: obs services emit unconditionally in the compose, gated by `profiles: [observability]`. `obs down` now runs service-targeted `compose stop + rm` against `grafana`, `prometheus`, `loki`, `promtail`, `otel-collector`, and `jaeger` only — main stack is preserved.

- **`vibew status`: disabled features now show OFF instead of probing and failing; self-signed dev TLS no longer triggers near-expiry alarm** (#1143, [ADR-095](decisions/adr-095-status-three-state-ok-off-fail.md)). The status dashboard now uses three text labels — `OK`, `OFF`, `FAIL` — instead of `✓`/`✗` glyphs. Features disabled in config (auth off, metrics off) render `OFF` and suppress the HTTP probe entirely; false alarms such as `✗ Auth (Kratos) unreachable` on a `vibew dev` stack with `auth.mode: none` are eliminated. Self-signed dev TLS (`KindSelfSignedLocal`) maps to `OK` with a `self-signed (dev)` annotation; expiring-soon certs (`KindExpiringSoon`) also map to `OK` with an annotation — `FAIL` is reserved for `KindFailing` (ACME renewal failure). A one-line legend (`States: OK = healthy   OFF = disabled   FAIL = check failed`) is printed below the table header. TTY output is coloured (green/dim/red); non-TTY and `--no-color` output is plain text.

- **`vibew bundle` staleness check now uses content-hash, not mtime** (#1146, [ADR-089](decisions/adr-089-bundle-image-health-tag-scoping-freshness-arch.md) §Refinement). `touch vibewarden.yaml` (no content change) no longer triggers a false STALE warning. SHA-256 is computed over the same file set the existing staleness walker considers (same `.gitignore` / `.dockerignore` / `hardIgnoreDirs` rules). The digest is stored at `.vibewarden/.input-digest` after every successful bundle and auto-appended to the project `.gitignore`. First-run and corrupt-digest paths fall back to the existing mtime comparison — no flag-day on upgrade.

- **`vibew bundle` no longer ignores the cwd-basename fallback for project name** (#1141, [ADR-093](decisions/adr-093-bundle-image-name-cwd-basename-fallback.md)). With no `name:` field set, `vibew bundle` and `vibew bundle --build` now both look for `<dirname>-app:latest`, matching the documented behavior. Previously `vibew bundle` resolved the image tag via `cfg.ComposeProjectName()` which silently fell through to the literal `"vibewarden"` when `ProjectRoot` was not populated by the loader, while `vibew bundle --build` correctly derived the cwd-basename — two different names from the same input. `deriveProjectName` is now the single project-name authority inside `runBundle`; its result is fed to both the image-tag default and the `--build` step.

### Changed

- **`vibew validate` now checks name collision, EXPOSE/upstream.port mismatch, image-tag drift, ACME-incompatible domain, and WAF prod-mode sanity** (#1144). Five runtime checks run after strict schema validation and catch conditions that cause the next `vibew bundle` or `vibew up` to fail. Each failing check emits a `FAIL` row on stderr with an actionable hint; checks that do not apply (no Dockerfile, no `.env`, non-ACME provider) are silently skipped. Exit code 1 when any check fails. New config key `waf.acknowledge_log_mode: true` suppresses the WAF log-mode check when intentional.

- **Multi-site projects flag-gated as post-v1** (#1150). `vibew validate` and `vibew add` now refuse multi-site configs with a clear "see #1169" message. `vibew bundle`'s existing hard-fail updated to point at the same tracking issue. Multi-site dev (`vibew dev` reverse-proxying multiple apps on host headers) keeps working — only the production-deploy path is gated. The architecture for multi-app-per-VM bundles is tracked in #1169 (post-stable).

- **`vibew eject` clarified as the non-Docker escape hatch** (#1147, [ADR-096](decisions/adr-096-vibew-eject-keep-and-clarify-non-docker-escape-hatch.md)). The `--help` lead line and the agents-template description now make clear: `vibew bundle` is the canonical Docker Compose path; `vibew eject` produces raw Caddy JSON for vanilla-Caddy / non-Docker deploys. No behavior change. The deeper consolidation (`vibew bundle --target ...`) is tracked separately.

- **`vibew init` now generates a minimal `vibewarden.production.yaml`** (#1145). The generated file no longer carries commented `# auth: ...`, `# admin: ...`, `# rate_limit: ...` stubs. Override patterns moved to `docs/examples/production-overrides.md`. Per CLAUDE.md §Artifact policy: examples belong in docs, not in live config. `vibew add tls` / `vibew add waf` continue to work; they don't depend on the stubs being present.

- **`vibew init` no longer generates a placeholder `Dockerfile` or `.dockerignore`** (#1139). `AGENTS-VIBEWARDEN.md` now carries a Dockerfile contract checklist; agents and users write their own Dockerfile against that contract. Migration: existing projects that already have a Dockerfile are unaffected. New projects must write a Dockerfile after `vibew init` — see `AGENTS-VIBEWARDEN.md` §Dockerfile contract.

### Removed

- **`vibew dev --observability` flag** (#1149). Replaced by the new `vibew obs up` / `vibew obs down` subcommand pair. Migration: `vibew dev && vibew obs up` (run `vibew obs up` after the main stack is running). Per CLAUDE.md §Architecture principles: trim CLI surface; `vibew dev` is the core loop, observability is opt-in tooling.

- **`vibew restart` removed** (#1148). For incremental rebuilds use `vibew build && vibew dev` (`vibew dev` auto-recreates stale app containers). For a clean restart of a wedged stack use `vibew down && vibew dev`.

- **`deploy.sh` no longer ships in the bundle** (#1138). The qr-dali deploy retrospective surfaced that the bundled script's hardcoded healthcheck port (8443, the dev port) reported false failures on successful production deploys (which listen on 443 with TLS). The bundle's `README.md` now describes the deploy contract directly — what the bundle is, where it goes, and the two non-obvious traps (the remote directory must exist before copying; healthcheck port in production is 443, not 8443). Operators and AI agents own the `scp` / `ssh` / `docker compose up -d` chain themselves. ADR-088 marked as superseded. See `CLAUDE.md` §Architecture principles → Artifact policy for the rationale (no example-shaped middle-ground artifacts).

  **Migration:** if you scripted around `cd .vibewarden/bundle && ./deploy.sh user@host`, replace it with the steps the bundle README now describes: `ssh user@host mkdir -p /path` → `scp -r .vibewarden/bundle/* user@host:/path/` → `ssh user@host "cd /path && docker load -i image.tar && docker compose up -d"` → `curl https://yourdomain/_vibewarden/health`.

---

## [v0.17.0] — 2026-04-25

**Theme: architectural cleanup + LE preflight + bundle correctness.**

Three areas of work converged in this release:

1. **Bundle correctness** (#1084–#1091, ADR-088, ADR-089) — project-scoped image tags, image health block, staleness/arch warnings, local deploy.sh convention, TLS state in status/doctor, hand-written comment preservation in `vibew add`.
2. **LE rate-limit preflight** (#1057, [ADR-090](decisions/adr-090-le-rate-limit-preflight.md)) — `vibew doctor` now queries crt.sh before any ACME attempt, preventing blind exhaustion of the 5-certs/domain/week limit.
3. **Architectural cleanup pass** — ports-purity invariant now enforced in CI (#1099, ADR-067), `internal/config` god package split into 11 per-concern files (#1100), Caddy handler DI fixed a real pre-v0.16.0 bug (#1102, ADR-092), middleware log drops are now observable via Prometheus (#1104), oversized functions extracted (#1105), dead `SessionCheckerToIdentityProvider` adapter purged (#1106), and three outbound port interfaces relocated from `internal/app/` to `internal/ports/` (#1107, ADR-091).

**No new breaking changes** beyond the project-scoped image tag (see Breaking section).

### Breaking

- **`VIBEWARDEN_APP_IMAGE` default tag is now project-scoped** (#1084, [ADR-089](decisions/adr-089-bundle-image-health-tag-scoping-freshness-arch.md)). `vibew init`, `vibew wrap`, and `vibew bundle` no longer write the generic `vibewarden-app:latest` tag that collided across all projects on the same workstation. The generated `.env` and `docker-compose.yml` now use `<project>-app:latest` (e.g. `mysite-app:latest`). Existing v0.16.0 projects carrying the old tag will see a migration warning on every `vibew validate` run until you update `.env` — run `vibew bundle --overwrite` or follow the `sed` one-liner in the warning.

### Added

- **LE rate-limit preflight in `vibew doctor`** (#1057, [ADR-090](decisions/adr-090-le-rate-limit-preflight.md)) — when `tls.provider: letsencrypt` is configured, `vibew doctor` queries the public [crt.sh](https://crt.sh) Certificate Transparency log to count certificates issued for the registered domain in the last 168 hours. Reports WARN at 4/5, FAIL at 5/5. Degrades to WARN when crt.sh is unreachable. Opt-out: `--skip-le-preflight` flag or `tls.skip_rate_limit_check: true`.
- **`vibew doctor --skip-le-preflight`** (#1057) — suppresses the LE rate-limit CT log check for a single invocation.
- **`tls.skip_rate_limit_check`** config key (#1057) — persistent opt-out for the LE rate-limit preflight. Defaults to `false`.
- **`vibew bundle --build`** (#1084) — runs `vibew build --platform <target>` before inspecting or packaging; use when the image is missing or stale.
- **`vibew bundle --allow-stale`** (#1085) — suppresses the STALE freshness warning; bundle proceeds regardless of source-file mtime.
- **`vibew bundle --target-platform linux/<arch>`** (#1091) — overrides the default expected deployment platform (`linux/amd64`); use for Graviton / Pi / arm64 servers.
- `vibew validate` now emits a migration warning to stderr when `.env` contains the legacy `vibewarden-app:latest` tag. (#1084)
- **`vibew down`** (#1089) — stop the local dev stack. Runs `docker compose down` in the project's `.vibewarden/` directory, preserving data volumes by default. Pass `-v`/`--volumes` to also drop named volumes (destroys Kratos DB state).
- **`vibew dev --verbose`** (#1075) — streams docker compose output during startup so the user can see image pulls and container boot messages in real time instead of a silent wait.
- **`vibewarden_event_log_drops_total` Prometheus counter** (#1104) — new metric with labels `{middleware, reason}` incremented whenever a structured log or audit event is dropped instead of delivered. Previously silent; now visible in Grafana without any dashboard changes.
- **`CircuitBreakerFactory` port** (#1102, [ADR-092](decisions/adr-092-caddy-handler-dependency-injection.md)) — new outbound port interface in `internal/ports/` allowing Caddy handlers to receive a circuit-breaker factory via the `RuntimeServices` registry rather than constructing one directly.
- **`vibew init` positional arg produces actionable error** (#1076, #1117) — running `vibew init myapp` now explains the cwd convention and exits non-zero instead of silently ignoring the argument.
- **`vibew dev` prints startup summary** (#1093) — dev URL, logs hint (`docker compose -f .vibewarden/compose.yaml logs -f`), and stop hint (`vibew down`) printed after successful startup.
- **`.vibewarden-version` file removed** (#732, #1116) — obsolete since the wrapper-less era; `vibew` no longer creates or reads it.

### Changed

- **`vibew bundle` now prints an image health block before writing any files** (#1084, #1085, #1091). Tag, digest, arch, creation timestamp, size, target platform, freshness verdict, and warnings are printed to stdout once per invocation in a stable key-value layout parseable without ANSI colour.
- `vibew bundle` aborts with exit code **2** (image missing) or exit code **3** (docker daemon unreachable) rather than a generic exit 1. Exit code 0 includes successful bundles with stale/arch warnings.
- **Bundle `deploy.sh` now runs locally** (#1087, [ADR-088](decisions/adr-088-deploy-sh-local-run-convention.md)). One command on the workstation — `cd .vibewarden/bundle && ./deploy.sh user@host` — scps the bundle, loads the image, runs `docker compose up -d`, and probes `/_vibewarden/health` on the remote. The script accepts `user@host[:/remote/path]` and defaults the remote path to `~/vibewarden-bundle`.
- **`vibew status` and `vibew doctor` now report TLS state** (#1090, #1078). The status table and doctor checks surface the live Caddy TLS state: `Obtaining` (ACME in progress), `Obtained` (cert active), `Failing` (ACME failed), or `SelfSignedLocal` (dev self-signed cert). Previously only a binary up/down was shown.
- `vibew add` commands that hit an unparseable YAML file now fail fast with a `vibew validate` remediation hint instead of silently rewriting. (#1086)
- **`internal/config/config.go` split into 11 per-concern files** (#1100, #1131) — no behaviour change, public API unchanged. Files: `config.go` (types), `load.go`, `merge.go`, `validate.go`, `defaults.go`, `tls.go`, `auth.go`, `plugins.go`, `sites.go`, `secrets.go`, `override.go`. Reduces edit-conflict surface for parallel agent work.
- **Caddy handlers now receive dependencies via `RuntimeServices` registry** (#1102, [ADR-092](decisions/adr-092-caddy-handler-dependency-injection.md)) — wired from `cmd/vibewarden/`. Eliminates the ad-hoc per-handler constructor pattern and keeps `internal/app/` free of adapter imports.
- **Middleware helpers replace `_ = logger.Log(...)` silent discards** (#1104, #1129) — `logEvent` and `logAudit` helpers call `slog.Warn` on drop and increment `vibewarden_event_log_drops_total`.
- **`BuildCaddyConfig`, `Proxy.forward`, `Proxy.handleRequest` refactored** (#1105, #1132) — extracted helpers reduce the three functions from 329/309/183 LOC to 19/27/15 LOC. No behaviour change; covered by existing tests.
- **Three outbound port interfaces relocated from `internal/app/*` to `internal/ports/`** (#1107, #1126, [ADR-091](decisions/adr-091-ports-hygiene.md)) — `AuditLogger`, `EventLogger`, and `MetricRecorder` are now package-level ports, not embedded in app packages. Callers import from `internal/ports/`.
- **`AdminServerIface` renamed to `AdminServerAPI`** (#1107, #1126) — aligns with the `*API` naming convention for inbound port interfaces. Update any external test helpers that referenced the old name.
- **`llms-full.txt` and examples use `vibewarden.production.yaml`** (#1108, #1114, #1120) — non-existent `--config` flag removed; `vibewarden.prod.yaml` references corrected to `vibewarden.production.yaml` throughout.
- **`/_vibewarden/healthz` corrected to `/_vibewarden/health`** (#1109, #1121) — docs, example configs, and reference YAML all now point to the real endpoint.
- **`vibew init` vs `vibew wrap` comparison table added to README and getting-started** (#1110, #1122) — clarifies that the two commands produce different scaffolding; old docs incorrectly implied they were equivalent.
- **`vibew dev --watch` and `vibew dev --observability` documented in CLI reference table** (#1112, #1113, #1125) — previously missing from `llms-full.txt`.
- `github.com/moby/patternmatcher` promoted from indirect to direct dependency — already vendored via Docker client, Apache 2.0.

### Fixed

- **CRITICAL: Caddy handlers were silently routing audit and log events to `/dev/null` since before v0.16.0** (#1102, #1130). Eight handlers constructed their audit/log sinks via `io.Discard` because the wiring in `cmd/` never reached the handler constructors. The `RuntimeServices` registry fix (#1102) closes the circuit: all handlers now receive the sinks configured in `vibewarden.yaml`. If your Grafana dashboard showed zero `audit.*` events despite traffic, this is why.
- **`vibew dev` surfaces docker compose stderr on failure** (#1075, #1093). Previous releases swallowed compose errors and returned a bare exit code; failures now include the full compose stderr in the error message.
- **`vibew dev` no longer silently exits 0** (#1088). After a successful startup the command now prints the dev URL and stop/logs hints.
- `vibew add tls` no longer silently regenerates `vibewarden.production.yaml`, wiping commented-out stanzas (WAF block mode, auth, headers). Comments/ordering/whitespace preserved via AST edit. (#1086, #1094)
- **`vibew doctor` no longer reports "expires 0 days" for self-signed certs** (#1078, #1096). Self-signed local certificates are now identified and reported as `SelfSignedLocal` instead of triggering a spurious near-expiry warning.
- **`vibew add` preserves hand-written comments in `vibewarden.production.yaml`** (#1086, #1094) — AST-level upsert replaces the previous marshal/unmarshal round-trip that dropped all comments.
- **Ports-purity invariant now enforced in CI** (#1098, #1099, #1119) — the architectural test that prevents `internal/app/*` from importing adapter packages was previously gated behind a build tag and never ran. Ungated; the `bundle.go` adapter-import regression that triggered the audit is fixed.
- **`llms.txt` and `llms-full.txt` merged into main repo** (#1082, #1083) — main repo is now the single source of truth; website fetches at build time.
- ADRs 073, 088 added to `decisions/README.md` index; ADR-090 promoted from Proposed to Accepted (#1111, #1103, #1124).
- Small docs pass (#1115): poll-not-stream MCP wording corrected, README curl flag normalized, `vibew down --yes` documented, agent-doc reference fixed.

### Removed

- **Dead `SessionCheckerToIdentityProvider` adapter deleted** (#1106, #1126) — the adapter had no callers since v0.11.0 and was kept only as scaffolding. Removes ~120 LOC.

### Internal

- `internal/app/eject` coverage raised from 34.5% to 100% via table-driven tests (#1101, #1128).
- Five new ADRs landed this cycle: [ADR-088](decisions/adr-088-deploy-sh-local-run-convention.md) (deploy.sh local-run), [ADR-089](decisions/adr-089-bundle-image-health-tag-scoping-freshness-arch.md) (bundle image health/staleness/arch warn), [ADR-090](decisions/adr-090-le-rate-limit-preflight.md) (LE rate-limit preflight), [ADR-091](decisions/adr-091-ports-hygiene.md) (ports hygiene), [ADR-092](decisions/adr-092-caddy-handler-dependency-injection.md) (Caddy DI registry).

---

## [v0.16.0] — 2026-04-21

**Theme: `vibew deploy` is gone.**

Four retros converged on `vibew deploy` as the single largest source of user
friction (16 bugs across three retro cycles). This release retires the remote
SSH orchestration command entirely in favour of the purely-local
`vibew bundle` pipeline. One CLI command removed, ~8000 LOC deleted, and six
new ADRs landed ([082](decisions/adr-082-strict-config-merge-unknown-keys-fail-loudly.md),
[083](decisions/adr-083-acme-chain-hardening-email-preflight-buypass-removed.md),
[084](decisions/adr-084-doctor-port-ownership-via-vibewarden-health-signature.md),
[085](decisions/adr-085-vibew-bundle-compose-only.md),
[086](decisions/adr-086-sunset-vibew-deploy.md),
[087](decisions/adr-087-test-placement-contract-tests-and-architectural-invariants.md)).

**Migration recipe** (replaces `vibew deploy`):

```bash
vibew bundle
cd .vibewarden/bundle && ./deploy.sh user@host
```

> Note: earlier drafts of this recipe told users to `ssh user@host 'cd
> ~/bundle && bash deploy.sh'`. That form was inconsistent with the
> local-run script and has been corrected — see
> [ADR-088](decisions/adr-088-deploy-sh-local-run-convention.md).

See [`docs/guide/bundle-to-vps.md`](docs/guide/bundle-to-vps.md) for the
end-to-end walkthrough and [`docs/deploy-reference.md`](docs/deploy-reference.md)
for the breaking-change landing page.

### Breaking

- **`vibew deploy` removed** (#1051, #1063, #1071,
  [ADR-086](decisions/adr-086-sunset-vibew-deploy.md) + amendment). The remote
  SSH orchestration command — along with its `status` and `logs` subcommands
  — has been retired. `vibew deploy` is no longer a registered command;
  invoking it prints cobra's default `unknown command "deploy"` error and
  exits non-zero. ADR-086 originally staged the removal across two releases
  (sunset + one-release stub); the amendment recorded in #1063 collapses the
  stub into this same release so the "deploy is gone" messaging matches
  runtime behaviour. Use `vibew bundle` + `./deploy.sh user@host`
  (migration recipe above).
- **MCP deploy tools removed** (#1062, #1069,
  [ADR-086 §"MCP-server tools"](decisions/adr-086-sunset-vibew-deploy.md)).
  MCP tools `vibewarden_prepare_deploy`, `vibewarden_verify_deploy`, and
  `vibewarden_get_deploy_logs` are gone. Use the `vibew bundle` CLI directly
  (see [`docs/guide/bundle-to-vps.md`](docs/guide/bundle-to-vps.md)); an MCP
  tool wrapping `vibew bundle` is tracked in #1068.
- **`vibew validate` / `vibew bundle` reject unknown keys** (#1053, #1056,
  [ADR-082](decisions/adr-082-strict-config-merge-unknown-keys-fail-loudly.md)).
  Typos or removed keys in `vibewarden.yaml` or `vibewarden.production.yaml`
  (e.g. `tls.dmain: example.com`) now fail loudly with an error naming the
  file and the offending key. Previously such keys were silently dropped,
  masking typos and causing silent misconfiguration in production. The
  runtime loader (`vibewarden serve`) is unchanged — it still accepts unknown
  keys for forward-compat per ADR-065. If you relied on the silent-drop
  behaviour for scratch annotations, move them to YAML comments
  (`# staging cutover 2026-04-18`).
- **Buypass removed from the default `letsencrypt` fallback chain** (#1055,
  #1058, [ADR-083](decisions/adr-083-acme-chain-hardening-email-preflight-buypass-removed.md)).
  `provider: letsencrypt` no longer falls back to Buypass. Buypass's ACME
  directory currently returns `403 Forbidden`, so keeping it in the chain
  only wasted recovery time. Buypass remains available as explicit opt-in
  via `provider: buypass`; a `tls.acme.provider_deprecated` event is emitted
  at Init when selected. If you relied on the silent Buypass fallback, set
  `provider: buypass` explicitly or keep the default `letsencrypt` (which
  now falls through to ZeroSSL only when `tls.email` is set).

### Changed

- **ZeroSSL skipped from the default chain when `tls.email` is empty**
  (#1055, #1058, [ADR-083](decisions/adr-083-acme-chain-hardening-email-preflight-buypass-removed.md)).
  Previously `provider: letsencrypt` wired ZeroSSL into the chain
  unconditionally; ZeroSSL then rejected the order because EAB requires an
  email, surfacing as a transient issuance error. The default chain now
  degrades to single-issuer Let's Encrypt when email is absent, and emits a
  `tls.acme.chain_skipped` event naming `zerossl` +
  `reason=email_not_configured` so operators see why. Set `tls.email` to
  opt back into the two-issuer chain.
- **`vibew doctor` port/TLS/remote checks redesigned** (#1054, #1060,
  [ADR-084](decisions/adr-084-doctor-port-ownership-via-vibewarden-health-signature.md)).
  Port-ownership detection now probes `/_vibewarden/health` to distinguish a
  running `vibew dev` from a foreign process; TLS cert inspection performs a
  live TLS handshake instead of reading a hardcoded `server.crt` path;
  remote-container error rendering no longer leaks raw shell fragments.
- **Port-layer tests moved to adapters / architectural invariants** (#717,
  #1070, [ADR-087](decisions/adr-087-test-placement-contract-tests-and-architectural-invariants.md)).
  Contract tests now live with the adapter implementations they exercise;
  architectural invariants are enforced via dedicated test packages.
- **`theme-sync.js` ported into mkdocs `extra_javascript`** (#1074) so
  website regeneration is reproducible end-to-end.

### Added

- **`vibew bundle` command** (#1044, #1061,
  [ADR-085](decisions/adr-085-vibew-bundle-compose-only.md)) — generates
  Docker Compose deployment artifacts (`docker-compose.yml`,
  `vibewarden.yaml`, `.env`, `sample.env`, `deploy.sh`, `README.md`,
  `image.tar`) into `.vibewarden/bundle/` with no SSH, no remote docker, and
  no network calls. Replaces the `vibew deploy --dry-run` workflow for users
  who drive their own `scp`/`rsync`/CI pipeline. `.env` is preserved across
  re-runs via a defer-safe snapshot so a mid-run generator failure cannot
  clobber user edits.
- `vibew init --non-interactive` flag (#1065, #1066) — skips interactive
  prompts even when stdin is a TTY; primarily for CI / agent scripting. Also
  fixes the integration-test port flag and multisite-skip path.
- **Four new v1 structured log events for ACME chain observability** (#1055,
  #1058, [ADR-083](decisions/adr-083-acme-chain-hardening-email-preflight-buypass-removed.md)):
  - `tls.acme.chain_skipped` — emitted at plugin Init for every issuer
    evaluated and excluded from the default chain (payload: `provider`,
    `reason`, `primary_provider`).
  - `tls.acme.chain_configured` — emitted once at plugin Init with the
    resolved chain (payload: `primary_provider`, `resolved_chain`,
    `domain`).
  - `tls.acme.provider_deprecated` — emitted when `provider: buypass` is
    resolved (payload: `provider`, `reason`, `guidance`).
  - `tls.acme.chain_fallback` — reserved in the v1 schema for future use;
    emitted only once Caddy/certmagic exposes a stable issuer-transition
    hook (payload: `from_provider`, `to_provider`, `reason`, `domain`).

### Fixed

- **`vibew init` scaffold no longer suggests `example.com` as the TLS domain**
  (#1077, #1079). The production scaffold placeholder is now
  `app.yourcompany.com` and calls out that Let's Encrypt rejects RFC-2606
  reserved names. The `tls.domain is required` validation error for ACME
  providers was updated to the same guidance. Prevents the confusing
  `rejectedIdentifier` failure users hit when they copy-pasted the previous
  placeholder verbatim.
- **`vibew doctor` no longer flags a running sidecar as a port conflict**
  (#1054, #1060, [ADR-084](decisions/adr-084-doctor-port-ownership-via-vibewarden-health-signature.md)).
  When `vibew dev` is already running on the proxy port, doctor probes
  `/_vibewarden/health` and reports `[OK] Proxy port in use by local vibew
  dev (expected)` instead of `[FAIL] Proxy port 8443 is already in use`.
  Foreign processes continue to FAIL as before.
- **`vibew doctor` TLS cert check now inspects the live sidecar** (#1054,
  #1060, [ADR-084](decisions/adr-084-doctor-port-ownership-via-vibewarden-health-signature.md)).
  The self-signed cert check performs a live TLS handshake against the
  proxy and reads the leaf certificate from
  `tls.Conn.ConnectionState().PeerCertificates`, instead of looking for a
  hardcoded `server.crt` path on disk. When the sidecar is not reachable
  the check is WARN with `sidecar not reachable on <addr> — start 'vibew
  dev'`.
- **`vibew doctor` remote-container errors no longer leak raw shell
  fragments** (#1054, #1060, [ADR-084](decisions/adr-084-doctor-port-ownership-via-vibewarden-health-signature.md)).
  `checkRemoteContainerHealth` previously surfaced errors containing literal
  `2>/dev/null` and `|| docker-compose ps`. Errors are now rendered as a
  single clean line of the form
  `exit <code>: <first stderr line> (<hint>)` — for example
  `exit 127: docker: command not found; docker compose not installed on
  remote`.
- **Production override preserves every schema field** (#1053, #1056).
  `tls.email`, `tls.acme_ca`, `tls.cert_monitoring.*`, `server.host`, and
  any other field set only in `vibewarden.production.yaml` now reach the
  runtime `*config.Config`. Previously a hand-written allow-list silently
  dropped fields beyond `server.port`, `tls.enabled/provider/domain`,
  `log.level`, and `waf.mode`, breaking the ADR-078 promise that `tls.email`
  wires to the Caddy ACME issuer. The struct overlay now routes through the
  same YAML deep-merge that feeds the on-disk bundle.

### Removed

- **`vibew deploy` package + orchestration** (~8000 LOC) — rsync, remote
  docker, SSH health probing, drift detection, and the `status`/`logs`
  subcommands are all gone (#1051, #1063, #1064, #1071,
  [ADR-086](decisions/adr-086-sunset-vibew-deploy.md)).
- **5 production-only `vibew doctor` checks** removed as part of the deploy
  sunset: `checkSSHConnectivity`, `checkArchCompatibility`,
  `checkRemoteContainerHealth`, `checkDomainDNS`, `checkRemoteTLSCert`. The
  `--target` and `--ssh-key` flags on `vibew doctor` are gone; use
  `vibew tls status` for remote TLS expiry. (#1059, #1072)
- **MCP deploy tools** (`vibewarden_prepare_deploy`,
  `vibewarden_verify_deploy`, `vibewarden_get_deploy_logs`) removed from the
  MCP server (#1062, #1069).

### Documentation

- Main-repo docs audit removing stale deploy references (#1067, #1073).
- Companion website releases: [vibewarden.dev#92](https://github.com/vibewarden/vibewarden.dev/pull/92)
  (landing + root `llms-full.txt` cleaned) and
  [vibewarden.dev#93](https://github.com/vibewarden/vibewarden.dev/pull/93)
  (regenerated `docs/vibewarden/` with theme-sync.js).

### Known follow-ups (not in this release)

- LE rate-limit preflight (#1057)
- MCP bundle tool replacement (#1068)
- `vibew dev` stderr swallowing (#1075)
- `vibew init` positional arg (#1076)
- `vibew doctor` TLS expiry message (#1078)
- docker/docker CVE — deferred to a security patch release (#805)

---

## [v0.15.0] — 2026-04-20

### Features

- **ACME fallback chain** (#1026). `tls.provider: letsencrypt` now configures three
  ACME issuers (Let's Encrypt → ZeroSSL → Buypass). If one CA is rate-limited, Caddy
  tries the next automatically. New providers: `zerossl`, `buypass`, `letsencrypt-staging`.
- **`/_vibewarden/me` endpoint** (#1021). When `auth.mode: kratos`, the sidecar serves
  session info as JSON at `/_vibewarden/me`. Frontend JS can fetch user ID, email,
  verified status, and role without calling Kratos directly.
- **`vibew tls status`** (#1034). New CLI command inspects the remote TLS certificate
  via SSH — shows domain, issuer, validity dates, and days remaining.
- **`vibew doctor` improvements** (#1033). Suggests `vibew doctor` on deploy failure.
  New checks: architecture compatibility, ACME email for ZeroSSL, image tag consistency.
- **Architecture mismatch detection** (#1032). Deploy detects when the local build arch
  doesn't match the remote server and errors with a fix-it message.

### Fixes

- **`tls.email` wired to Caddy** (#1027). The ACME account email was accepted in config
  but silently dropped in single-site mode. Now properly passed to the Caddy issuer.
- **`vibew dev` stale container detection** (#1028). Detects and rebuilds stale or
  wrong-project containers instead of silently reusing them.
- **Deploy health check diagnostics** (#1029). Classifies failures into container
  unhealthy, TLS error, upstream unreachable, timeout, or unknown — with relevant
  Caddy log excerpts.
- **Deploy status/logs correct directory** (#1030). `vibew deploy status` and
  `vibew deploy logs` now derive the remote directory consistently with `vibew deploy`.
- **Deploy drift false positives** (#1031). Credential preservation runs before drift
  detection, and rsync uses `--checksum` instead of mtime. Deploy-owned files are
  categorized separately from user modifications.

### Documentation

- AGENTS-VIBEWARDEN.md: image tag convention, TLS config keys table, VPS deploy
  section with manual fallback, cross-architecture build guidance.
- Updated deploy-reference.md, troubleshooting.md, configuration.md, llms-full.txt.
- Added reply style guidelines to CLAUDE.md for briefer agent responses.

---

## [v0.14.0] — 2026-04-20

### Features

- **Role-based access control (RBAC)** via Kratos identity traits (#985, #1019).
  `X-User-Role` header set on all authenticated requests. Optional `auth.role_paths`
  config for path-based enforcement with HTTP 403 JSON responses.
- **Optional auth on public paths** (#984, #1017). Public paths now check session
  cookies and inject identity headers (`X-User-Id`, `X-User-Email`, `X-User-Verified`,
  `X-User-Role`) when a valid session exists — without blocking or redirecting.
- **Composite secret placeholders** (#994, #1020). Embed secrets in strings with
  `${secret://path/key}` syntax. Supports multiple placeholders per value,
  `value_template` on inject entries, and `$${...}` escaping for literal output.
- **`secret://` URI resolution in config** (#1008, #1014). Any string field in
  `vibewarden.yaml` supports `secret://path/key` URIs, resolved from the encrypted
  store at config load time.

### Improvements

- Consolidated CI: Build, Test, and Coverage merged into single "Build & Test" job (#1015).
- Pipeline status tracked via GitHub labels instead of comments (#1013).
- Dual review pipeline enforced: Reviewer + Writer agents review every PR (#1010, #1011, #1012).
- Reviewer and writer agents post inline PR comments and resolve threads on re-approval.
- Documentation diagrams migrated from ASCII to Mermaid (#1004, #1016).

### Fixes

- CSP inline styles and health check HTTP fallback during ACME cert acquisition (#1007).
- Preserve `.env` and `.credentials` on redeploy (#991, #992, #1005).
- Kratos URLs use `tls.domain` instead of localhost in production (#990).
- Kratos `ui_url` uses HTTPS when TLS is enabled (#982).
- File/directory permissions fixed to 644/755 for container readability (#988, #989).
- Deploy config merge order and OpenBao bootstrap check (#986, #987).
- `vibew add tls --domain` creates production YAML when missing (#986).

### Dependencies

- Bump `golang.org/x/crypto` to 0.50.0
- Bump `pgx/v5` to 5.9.2
- Bump `go-viper/mapstructure/v2` to 2.5.0

---

## [v0.13.1] — 2026-04-19

### Features

- **`vibew init --name`** (#959): set a project name for image tags, compose
  project names, and deploy directories. Eliminates cross-project Docker image
  cache collisions.
- **`vibew deploy --dry-run`** (#958): generate the deploy bundle and inspect it
  without SSH/rsync. Shows exactly what would land on the server.
- **`vibew status` shows WAF** (#960): status output now lists WAF (with mode),
  CORS, egress, and compression plugins when enabled.

### Bug Fixes

- **Deploy bundle merge order** (#953): the generator was overwriting the merged
  `vibewarden.yaml` with the unmerged base config. Fixed by writing the merged
  config after generation. Also: compose template now uses production port/TLS
  via `overlayProdConfig`, and health check probes the correct port.
- **Build context rsync excludes** (#953): `TransferExcluding` prevents the
  app source rsync from overwriting bundle files (vibewarden.yaml, compose,
  credentials).
- **Deploy compose `context: .`** (#952): deploy mode uses `context: .` instead
  of the dev-mode `../../.` relative path. Verified by artifact regression test.
- **Drift detection false positive** (#962): first deploy to empty remote no
  longer reports "files modified" for new-file entries.
- **`vibew add tls --domain`** (#954): no longer modifies base `vibewarden.yaml`
  — writes domain to production override only.
- **Sidecar DNS** (#956): compose template includes `dns: [1.1.1.1, 8.8.8.8]`
  for hosts with systemd-resolved.
- **`vibew init` hidden dirs** (#957): `.claude/` and `.git/` directories no
  longer trigger "not empty" error.
- **Project-scoped Docker image tags** (#955): compose project name derives from
  `--name` flag or directory name, preventing `vibewarden-app:latest` collision.
- **Artifact regression tests** (#963): 10 tests that verify generated file
  content — deploy compose context, production merge, upstream resolution, image
  naming, build context exclusion, drift detection, YAML field preservation.

### Documentation

- AGENTS-VIBEWARDEN.md includes Dockerfile guidance: Alpine requirement, port
  matching, Node.js and Go examples. (#961)

---

## [v0.13.0] — 2026-04-19

### Breaking Changes

- **Deploy redesign (ADR-075)**: `vibew deploy` now generates a complete deploy
  bundle locally at `.vibewarden/deploy/<env>/` — no `sed` or runtime patching on
  the remote. All config is resolved before transfer.
- **Environment separation**: `vibew init` now generates two files:
  `vibewarden.yaml` (local dev, self-signed TLS, port 8443) and
  `vibewarden.production.yaml` (production overrides: letsencrypt, port 443).
  `vibew deploy` merges the production override on top of the base config.
  **Never put production-only config in `vibewarden.yaml`.**
- **`vibew add tls --domain`** now writes to `vibewarden.production.yaml` instead
  of the base config.
- **`vibew restart`** now runs `docker compose up -d --force-recreate --build`
  instead of `docker compose restart` — Dockerfile changes are picked up
  automatically.

### Features

- **Local Docker image transfer** (#937): `vibew deploy` with a bare image name
  (no registry prefix) automatically transfers the locally-built image via
  `docker save | rsync | docker load`. No registry needed. (#950)
- **Deploy bundles** (#938): `.vibewarden/deploy/production/` contains every file
  needed to run on the remote — inspectable, portable, no magic. (#948)
- **Self-documenting production overrides**: `vibewarden.production.yaml` shows
  all config options as comments with default values. Uncomment what you need.
- **`vibew deploy --env`** flag for environment-scoped bundles (default:
  production).

### Bug Fixes

- **`vibew init` non-TTY** (#939): no longer dies with EOF when run without a
  terminal — defaults to empty description. (#947)
- **Healthcheck shell detection** (#940): `vibew build` warns when the app image
  has no `/bin/sh` (distroless/scratch). (#946)
- **`vibew status` diagnosis** (#942): shows container state, ACME errors, and
  letsencrypt local-dev hint instead of bare "Proxy unreachable." (#946)
- **`vibew dev` letsencrypt warning** (#943): warns when `tls.provider:
  letsencrypt` is used locally (ACME challenges can't reach localhost). (#946)
- **`vibew dev` sidecar verification** (#945): checks sidecar is running after
  compose up, shows logs on failure instead of printing false success. (#947)
- **Stale AGENTS-VIBEWARDEN.md** (#944): removed incorrect "vibew add waf does
  not exist" note, updated doctor limitation. (#947)

### Documentation

- ADR-075: deploy redesign with environment separation model.
- Updated getting-started, deploy-to-vps, llms-full.txt, AGENTS-VIBEWARDEN.md,
  and reference yaml for two-file model.

---

## [v0.12.1] — 2026-04-19

### Bug Fixes

- **CRITICAL: WAF and rate-limiting now enforce in multi-site mode** — plugin handlers
  were completely missing from the multi-site Caddy config. Per-site plugin registries
  are now created and handlers injected into each site's route chain.
  (#934, closes #925)
- **Deploy exits non-zero on health check timeout** — previously reported "Site deployed"
  even when the sidecar wasn't running. Now returns `ErrHealthCheck` and suggests
  `vibew doctor --target <host>` for diagnostics. (#933, closes #927)
- **`vibew restart` shows stderr and diagnostic hint** on failure instead of bare exit
  code. (#935, closes #928)
- **`vibew add tls --domain` updates existing TLS config** instead of saying "already
  enabled — nothing to do." (#935, closes #929)
- **Project-scoped Docker image names** — compose `name:` directive prevents stale image
  reuse across different VibeWarden projects. (#935, closes #930)

### Documentation

- Multi-site deployment section added to AGENTS-VIBEWARDEN.md template — documents
  `sites/` layout, upstream.host container naming, centralized TLS.
  (#935, closes #931)

---

## [v0.12.0] — 2026-04-18

### Features

- **Built-in AES-256-GCM encrypted secret store** — eliminates OpenBao dependency
  for common secret management. Zero external deps, stdlib crypto only.
  (#904, closes #899)
- **`vibew add waf`** subcommand for CLI parity with other plugins. Accepts
  `--mode detect|block`. (#922, closes #912)
- **`vibew doctor` enhanced** with local runtime checks (upstream reachability,
  TLS cert validity) and production checks (SSH connectivity, remote container
  health, domain DNS, TLS cert expiry). Auto-detects applicable checks.
  (#923, closes #913)

### Bug Fixes

- **WAF handlers in `vibew eject`** — previously missing from the Caddy route
  chain in eject output, leading to false "WAF not enforced" diagnosis.
  (#915, closes #906)
- **Deploy health check protocol** — now uses HTTPS with `-k` when TLS is
  enabled, was polling plain HTTP on port 443. (#919, closes #907)
- **Deploy drift detection** — `vibew deploy` detects hand-edited files on the
  server and requires `--force` to overwrite. (#920, closes #908)
- **`proxy.started` log accuracy** — emits per-site events with correct TLS,
  upstream, and security header values in multi-site mode. (#918, closes #909)
- **Multi-site deploy production fixes** — `app.build` rsync, network
  `external: true`, `upstream.host` rewrite from loopback to container name.
  (#921, closes #911)
- AI prompt templates: removed 'without confirmation' phrasing that triggered
  safety guardrails. (#903)

### Documentation

- Fixed non-existent CLI flags in `llms-full.txt`; added `init`, `wrap`, and
  `add` flag documentation. (#917, closes #910)
- Known limitations section added to `AGENTS-VIBEWARDEN.md` template and
  example. (#916, closes #914)
- Missing `package-lock.json` added to Next.js example. (#902, closes #897)

---

## [v0.11.0] — 2026-04-15

### Breaking changes

- **`auth.enabled` removed from `vibewarden.yaml`** (ADR-065). `auth.mode` is
  now the single source of truth for whether authentication is enabled. Set
  `auth.mode: "none"` to disable auth, or `auth.mode: "kratos" | "jwt" | "api-key"`
  to enable a strategy. Any presence of `auth.enabled` — even `false` — is
  rejected at config load with an actionable error pointing at ADR-065.
  Migration: delete the `auth.enabled` line; keep or set `auth.mode`.
  (#845, closes #816)
- **`vibew init` no longer accepts a positional name argument** (ADR-073). It
  always scaffolds in the current directory. Use `mkdir myapp && cd myapp &&
  vibew init` instead of `vibew init myapp`. The `--name` flag is also removed.
  (#895, closes #842)

### Features

- **Multi-app deployment** (epic #869, ADRs 068-072) — deploy multiple apps to
  the same VM with subdomain routing. Each app gets its own `vibewarden.yaml`,
  independent TLS certs, and per-site middleware. `vibew deploy` detects an
  existing VibeWarden instance and adds sites automatically without downtime.
  - Site domain model and multi-config loader (#875, closes #870)
  - Caddy multi-host route generation (#876, closes #871)
  - Deploy detection and multi-app orchestration (#877, closes #872)
  - Multi-site directory watcher and hot-reload service (#878, closes #873)
  - Multi-app CLI and serve entry point (#879, closes #874)
- **Language-aware Docker health check probes** — Python uses `python -c`, Node
  uses `node -e`, Go/Alpine uses `wget`. Eliminates missing-wget failures on slim
  images. (#886, closes #884)
- **MCP tool list generated at runtime** — `vibew mcp --help` always reflects
  all registered tools; the list can no longer drift from the live registry.
  (#835, closes #813)
- **Composition roots moved to `cmd/vibewarden/`** (ADR-067) — cleaner
  separation between wiring and domain logic. (#867, closes #809)
- **Ports layer consolidated** (ADR-064) — all port interfaces live under
  `internal/ports/`, reducing import fan-out. (#843, closes #818)

### Bug fixes

- Caddy auth handler modules now registered when `auth.mode: kratos` is set;
  previously crashed with `unknown field "cookie_name"`. (#885, closes #883)
- External TLS provider in multi-site mode no longer falls through to ACME.
  (#858, closes #823)
- YAML quoting in language-aware health check commands corrected. (#898, closes #898)
- `vibew generate` no longer requires a scaffolding marker to be present.
  (#881, closes #880)
- Docker build context fixed for generated compose files. (#859, closes #808)
- `errors.Is` used consistently for `http.ErrServerClosed` checks across all
  five serve paths. (#837, closes #814)
- OpenBao `tryOpenBao` and `tryDynamicCredentials` silent error masking removed;
  transport errors now propagate. (#830, closes #812) (#834, closes #832)
- `CertMonitor.Stop` double-close panic fixed by adding a sync.Once guard.
  (#858, closes #823)
- SSRF `privateRanges` moved from package-level `init()` to per-guard state,
  eliminating a data race. (#838, closes #815)
- OpenBao `NotFound` is now a sentinel error for clean `errors.Is` checks.
  (#830, closes #821)
- WAF defaults in `vibewarden.reference.yaml` corrected. (#828, closes #810)
- Scaffold tests isolated and repo safety check added (ADR-066). (#846, closes #844)
- Removed `.claude/CLAUDE.md` and `.cursor/rules` from scaffold file lists.
  (#829, closes #811)
- CLI help-text corrections for `plugins` and `secret generate` commands.
  (#868, closes #865, #866)
- `vibew init` docs aligned with `vibew wrap` in getting-started guide.
  (#841, closes #820)
- `--lang` flag removed; bare `vibew init` used everywhere in docs. (#857, closes #842)

### Infrastructure and dependencies

- Dependabot configured for gomod, pip, docker, and github-actions ecosystems.
  (#847)
- Integration test pipeline added: Tier 1 multi-site Host-header routing and
  Tier 3 Docker-in-Docker deploy test. (#889, #890, #891)
- 9 Dependabot PRs merged: Alpine 3.21 to 3.23, `actions/checkout@v6`,
  `actions/setup-go@v6`, `actions/upload-artifact@v7`,
  `docker/setup-qemu-action@v4`, `docker/setup-buildx-action@v4`,
  `golangci/golangci-lint-action@v9`, OTel exporters (CVE-2026-39882),
  `modernc.org/sqlite` bump. (#847-#856, #827)
- CI Trivy image scan and coverage gate stabilised on main. (#833)
- All agents upgraded to Opus 4.7. (#803)
- ADRs split into individual files under `decisions/`; 6 previously missing
  ADRs recovered. (#894)

### Documentation

- `vibew deploy`, `vibew upgrade`, `vibew migrate`, `vibew plugins`, and
  `vibew secret generate` commands documented. (#864, closes #817)
- `llms.txt` short-index added at repo root. (#860, closes #826)
- Sample `AGENTS-VIBEWARDEN.md` added to `docs/examples/`. (#861, closes #825)
- Install PATH hint and dead-upstream error example added to docs. (#863, closes #825)
- Reference YAML readers pointed at `example.yaml` first. (#839, closes #822)

---

## [v0.1.0] — 2026-03-28

First public release of the VibeWarden OSS core.
Single Go binary embedding Caddy. Zero-to-secure in minutes for vibe-coded apps.

### Core sidecar

- Embedded Caddy reverse proxy — programmatic config, no Caddyfile required
- Automatic TLS via Let's Encrypt, self-signed (dev), or external provider passthrough
- Per-path request body size limits
- W3C `traceparent` header propagation to upstream
- `trace_id` injected into JSON error responses
- OpenAPI 3.0 spec served at `/_vibewarden/openapi.json`
- Graceful degradation — sidecar stays up when optional backends (Kratos, OpenBao) are unavailable
- Project scaffold (`vibewarden init`) with profile-based Docker Compose generation

### Authentication

- [Ory Kratos](https://www.ory.sh/kratos/) session validation middleware
- Kratos flow proxy routes (`/self-service/*`) forwarded transparently
- Built-in auth UI pages: login, registration, account recovery, e-mail verification
- Social login (OIDC) with auto-selection of identity schema preset
- JWT/OIDC identity adapter with JWKS caching and configurable clock skew
- `auth.mode` config switch: `kratos` | `jwt` | `none`
- Identity provider port abstraction — swap backends without touching middleware
- Scoped API keys with path-based authorization
- API key validation middleware with OpenBao-backed storage and TTL cache
- `X-User-*` headers stripped at the Caddy layer to prevent client spoofing
- Configurable public-path bypass list

### Rate limiting

- In-memory token-bucket rate limiter (IP-based and user-based)
- Redis-backed rate limiter with graceful fallback to in-memory on Redis failure
- Per-path rate limiting configuration
- StateSync port abstraction with both in-memory and Redis adapters
- External Redis configuration with shared counters across replicas
- Per-route rate limiting on egress proxy routes

### Security

- Security headers plugin: `Strict-Transport-Security`, `X-Frame-Options`,
  `X-Content-Type-Options`, `Content-Security-Policy`, `Referrer-Policy`,
  `Permissions-Policy`
- CORS plugin with per-origin, per-method, and per-header configuration
- IP filter plugin: allowlist and blocklist with CIDR support
- Content-Type validation middleware (rejects mismatched or missing `Content-Type`)
- WAF rule engine with pattern detection for SQLi, XSS, path traversal, and more
- WAF middleware: `block` mode (reject request) and `detect` mode (log only)
- Audit event domain model with structured `AuditEvent` type
- Audit log sink adapters: JSON file, OTel logs, and multi-writer fan-out
- Audit events emitted from all security-relevant middleware
- Webhook delivery for audit events with retry and HMAC signing

### Secrets management

- OpenBao (HashiCorp Vault fork, Apache 2.0) integration
- Secret management plugin: read/write KV secrets at runtime
- `vibewarden secret get` and `vibewarden secret list` CLI commands
- `.env.template` generation with `vibewarden generate` for credential bootstrapping

### Observability

- Structured log events via `log/slog` with `schema_version`, `event_type`,
  `ai_summary`, and `payload` fields
- JSON Schema v1 for log events, published at `vibewarden.dev/schema/v1/event.json`
- Prometheus metrics adapter, metrics exposed at `/_vibewarden/metrics`
- OpenTelemetry SDK integration: metrics, logs, and traces under a single provider
- OTLP exporter with configurable endpoint and TLS
- OTel Collector in Docker Compose observability stack
- Jaeger / Grafana Tempo trace backend options
- HTTP tracing middleware with automatic span creation per request
- `trace_id` and `span_id` injected into slog context
- `slog` structured events bridged to OTel logs
- Grafana dashboards for request rates, error rates, latency, and upstream health
- Aggregate health endpoint at `/_vibewarden/health` — reports component and upstream status
- Active upstream health checker with configurable interval and thresholds
- Telemetry configuration guide and annotated example YAML

### Resilience

- Request timeout middleware (configurable per-path; returns `504` on breach)
- Circuit breaker middleware with half-open probe and configurable thresholds
- Retry middleware with exponential backoff and jitter
- Aggregate health endpoint combining all resilience signals

### Egress proxy

- Core egress proxy listener and request forwarding
- Domain types, ports, and config schema for egress routes
- Per-route header injection and stripping
- Per-route circuit breaker
- Per-route rate limiting
- Per-route timeout (`504`) and retry with exponential backoff
- Per-route mTLS client certificates
- Per-route secret injection via OpenBao
- SSRF protection and DNS resolution control (RFC 1918 blocking)
- TLS enforcement on egress routes with per-route override
- Request sanitisation and PII redaction before forwarding
- Request and response body size limits
- In-memory LRU response caching per route
- Egress response validation (status code allow-list, header assertions)
- Egress observability: tracing, Prometheus metrics, and structured logs
- Egress proxy wired into the plugin system (enable/disable via config)

### Developer experience

- `vibewarden init` — interactive project scaffold with opinionated defaults
- `vibewarden generate` — produces `docker-compose.yml` from `vibewarden.yaml`;
  includes app service, plugin-dependent services, observability stack, and
  credential generation via `.env.template`
- `vibewarden doctor` — pre-flight checks for config, TLS, and backend connectivity
- `vibewarden secret get / list` — read secrets from OpenBao at runtime
- Profile-based Docker Compose: `--profile observability`, `--profile demo`
- Demo app with Vulnerability Lab (SQLi, XSS, path traversal, and more)
- Production deployment guide, hardening checklist, and framework integration examples
- Rate limiting at scale guide with annotated Redis config reference
- Postgres deployment strategies guide with connection resilience config reference
- Identity providers and JWT/OIDC setup guide
- Social login setup guide

### CI / CD

- GitHub Actions CI pipeline: build, vet, test on every push and pull request
- goreleaser configuration with cross-compiled binaries and Docker image publishing
- Multi-arch Docker images published to `ghcr.io/vibewarden/vibewarden` for
  `linux/amd64` and `linux/arm64` via OCI manifest lists; works transparently on
  Apple Silicon, AWS Graviton, and other ARM64 hosts

---

[v0.17.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.17.0
[v0.16.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.16.0
[v0.15.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.15.0
[v0.14.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.14.0
[v0.13.1]: https://github.com/vibewarden/vibewarden/releases/tag/v0.13.1
[v0.13.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.13.0
[v0.12.1]: https://github.com/vibewarden/vibewarden/releases/tag/v0.12.1
[v0.12.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.12.0
[v0.11.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.11.0
[v0.1.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.1.0
