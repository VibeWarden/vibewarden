# Changelog

All notable changes to VibeWarden are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

Future releases are generated automatically via [goreleaser](https://goreleaser.com/).
This initial entry was written by hand to summarise the work leading up to v0.1.0.

---

## [Unreleased]

### Added

- **feat(#1391): embedded user-management admin UI at `/_vibewarden/admin/ui` (ADR-107).** Adds a browser-based admin console for user management, served directly from the binary via `go:embed`. The UI (vanilla HTML/CSS/JS, no Node/npm, no CDN) is embedded under `internal/plugins/usermgmt/ui/assets/` and served by a new `AdminUIHandler` using stdlib `http.FileServerFS`. Static assets load without a token (carved out of the `AdminAuthMiddleware` token gate — matched against the cleaned path as an exact subtree so traversal/prefix-confusion cannot reach gated routes — while remaining hidden when `admin.enabled: false`); all data endpoints (`/_vibewarden/admin/users*`, `/events`, etc.) keep their `X-Admin-Key` requirement unchanged. The UI shows the official VibeWarden logo and a "Protected by VibeWarden" badge linking to vibewarden.dev. The JS prompts for the admin token, stores it in `sessionStorage` only, and sends it as `X-Admin-Key` per request. Works under `default-src 'self'` CSP with no policy change (no inline code, no external origins). No new dependencies.

### Changed

- **chore(#1418): bump `github.com/prometheus/client_golang` from v1.23.2 to v1.24.1.** VibeWarden's only use of this module is in `internal/adapters/otel/provider.go` (`prometheus.NewRegistry` plus `promhttp.HandlerFor` with `HandlerOpts{EnableOpenMetrics: true}`), so none of the v1.24.0 breaking changes apply: the `api/prometheus/v1` and `exp/api/remote` signature changes are in packages we do not import, and we never set the deprecated `model.NameValidationScheme` global. The new minimum Go version (1.25) is already satisfied by the pinned go1.26.4 toolchain. The bump picks up two upstream fixes relevant to the `/metrics` endpoint: `promhttp` no longer panics on requests with a nil URL (v1.24.1), and `Gather()` now recovers from collector panics and returns an error instead of crashing the process. Transitive upgrades pulled in by MVS: `prometheus/common` v0.67.5 → v0.70.1, `prometheus/procfs` v0.20.1 → v0.21.1, `klauspost/compress` v1.18.6 → v1.19.1, and the `golang.org/x/{crypto,mod,net,sync,sys,term,text,tools}` set. All Apache-2.0 or BSD-3-Clause licensed. No source changes required.

- **chore(#1425): batch outstanding dependency updates (supersedes Dependabot PRs #1408, #1410, #1411, #1415, #1416, #1419).** Three Go module bumps and three CI action bumps, each audited against the exact call sites and inputs VibeWarden uses:
  - `github.com/redis/go-redis/v9` v9.20.1 → v9.21.0 (main module). Upstream declares the release a drop-in replacement for 9.20.x with no breaking changes; the additions (`GetToBuffer`/`SetFromBuffer`, the `XTrimLimitDisabled` sentinel, bounded PubSub health-check timeouts) touch APIs we do not call. Our surface is limited to `redis.NewScript` + `Script.Run(...).Int64Slice()` (`internal/adapters/ratelimit/redis_adapter.go`), `redis.Cmdable`, and `redis.ParseURL`/`redis.Options`/`redis.NewClient` (`internal/plugins/ratelimit/plugin.go`), all unchanged.
  - `github.com/moby/moby/api` v1.54.2 → v1.55.0 (main module). The 1.55.0 changelog is purely additive at the HTTP-API level (per-device blkio settings on `POST /containers/{id}/update`, the new `GET /images/{name}/attestations` endpoint); the only types we use are `container.HostConfig` and `mount.Mount`/`mount.TypeBind` in `test/integration/multisite_test.go`, which are unaffected.
  - `modernc.org/sqlite` v1.52.0 → v1.55.0 (`examples/demo-app` module), carrying `modernc.org/libc` v1.72.3 → v1.74.1 and `golang.org/x/sys` v0.42.0 → v0.46.0. The demo app only uses the driver through `database/sql` (`sql.Open("sqlite", ...)`), so there is no direct API surface. No new module paths entered either `go.sum`, so no new licenses needed review.
  - `actions/checkout` v6 → v7, `actions/setup-go` v6 → v7, `actions/setup-python` v6 → v7 across all five workflows. checkout v7's behavioral change (blocking fork-PR checkout under `pull_request_target`/`workflow_run`) does not apply — no workflow in this repo uses either trigger — and the only input we pass is `fetch-depth`. setup-go v7 is an ESM/dependency migration with no input changes; we pass `go-version`, `go-version-file`, and `cache`. setup-python v7 removes the `pip-install` input, which we never set; we pass only `python-version`.

### Fixed

- **fix(#1339): stop the sidecar spamming `failed to upload metrics` on every OTLP export interval.** In dev mode the `observability` compose profile is off, so `otel-collector` is not running and every OTLP push failed with `dial tcp: lookup otel-collector ... no such host`. The OTel SDK routes background export failures to `otel.Handle`, whose default handler writes to the stdlib `log` package — which Caddy redirects into its own logger at **info** level, producing one noise line per interval (~every 30s) and burying real errors in `vibew logs vibewarden`. The metrics plugin now installs a slog-backed global OTel error handler (`internal/adapters/otel/errorhandler.go`) before any exporter starts: the first occurrence of a given error message is logged once at `warn` — so a genuinely misconfigured collector is still visible — and every consecutive repeat of the same message is demoted to `debug` with a `repeat_count` attribute. The dedup state resets when the message changes, so a different failure is always surfaced at `warn`. This covers metric, trace and log export failures alike, since all of them funnel through the same global handler.

- **fix(#1338): correct the stale service names in `vibew logs --help` and `vibew dev --help`.** The logs help advertised `postgres` as a default-stack service, but the generated `docker-compose.yml` names the Kratos database `kratos-db` — an agent following the help text ran `vibew logs postgres` and got an "unknown service" dead end. The list now matches the compose template exactly (`vibewarden`, `app`, `kratos`, `kratos-migrate`, `kratos-db`, `seed-users`, `openbao`, `seed-secrets`, `redis`), annotates each entry with the `vibewarden.yaml` condition that emits it, and points at `vibew obs` for the observability services. The `seed-users` entry names the real opt-in key `auth.seed_demo_users` (there is no `auth.users` key — the strict loader rejects it) and states the actual condition: the container is emitted for every locally managed Kratos stack, while only the mounted seeding script is gated on the flag. `vibew dev --help` now gives the compose service name alongside each baseline component instead of the bare product name "PostgreSQL" and marks `kratos`/`kratos-db` as conditional on `auth.mode: kratos`, since a default `vibew init` scaffold has no `auth:` block. The two `vibew logs postgres` examples in `docs/troubleshooting.md` were corrected to `vibew logs kratos-db`, and the `--rebuild --volumes` descriptions in `docs/troubleshooting.md` and `llms-full.txt` now say "kratos-db data" instead of "Postgres data". A new artifact test (`internal/cli/cmd/help_service_names_test.go`) parses the service names out of the rendered help text and asserts every one of them is declared in `internal/config/templates/docker-compose.yml.tmpl`, so this class of drift now fails the build instead of reaching users.

- **fix(#1337): stop `vibew status` reporting FAIL for a healthy Kratos when `kratos.admin_url` is a container-internal hostname.** The bundled config addresses Kratos by its Docker Compose service name (`http://kratos:4434`), which resolves only inside the compose network — so the host-side probe in `vibew status` always failed with `FAIL Auth (Kratos) unreachable (http://kratos:4434)` even when `vibew probe` reported the stack healthy. Since the generated `docker-compose.yml` publishes the Kratos admin port on the host, the status check now rewrites such addresses to the equivalent `127.0.0.1:<port>` before probing, and labels the row with `(published port for container-internal kratos:4434)` so the shown URL is not mistaken for the configured one. Only bare single-label hostnames are rewritten: loopback addresses, IP literals, and dotted FQDNs (external Kratos deployments) are still probed exactly as configured, so a genuinely unreachable instance still reports FAIL.

- **fix(#1341): drop `app.build` from the bundled `vibewarden.yaml` when `app.image` is set.** `vibew bundle` deep-merges `vibewarden.production.yaml` onto `vibewarden.yaml`, so a base config with `app.build: .` (dev mode) plus a production override with `app.image: ghcr.io/org/app:latest` produced a bundle whose config carried both keys. A build context is meaningless on the remote host — no source code is shipped — and having both left readers (humans and AI agents) guessing which field wins. `buildMergedConfigYAML` now calls the new `ResolveProdAppBuild`, which removes `app.build` from the merged map whenever `app.image` is a non-empty value; the production image and all sibling `app.*` keys are preserved verbatim. When no image is configured, `app.build` is left untouched — it is then the only description of how the image is produced. Applies to both single-site and multi-site bundles; the generated `docker-compose.yml` is unchanged (it already used `image:`).

- **fix(#1425): add QEMU/binfmt setup to the GoReleaser snapshot dry-run workflow.** `.github/workflows/release-dryrun.yml` ran `goreleaser release --snapshot` on an amd64 runner without `docker/setup-qemu-action`, so the `linux/arm64` image build could not execute the arm64 `alpine` base image: the `RUN apk upgrade` layer in `Dockerfile.goreleaser` failed with `exec /bin/sh: exec format error`. The check had therefore failed on every run since it was introduced — it only triggers on PRs that touch `.goreleaser.yml`, `Dockerfile.goreleaser`, the wrapper scripts, or the two release workflows, so the breakage stayed latent between such PRs. Added the `Set up QEMU` and `Set up Docker Buildx` steps that `release.yml` already had, so the dry-run now exercises the same multi-arch build path as a real release.

- **fix(#1421): remove data race between the config-watcher debounce timer and channel close.** `fsnotify.Watcher.Watch` ran its `time.AfterFunc` debounce callback with a `select` over `<-done` and `ch <- struct{}{}`. When both cases were ready Go picks one at random, so the `done` guard did not stop a late send racing the watch goroutine's `defer close(ch)` — `go test -race` reported the write/read conflict (and a real run could panic with "send on closed channel"). The callback now only signals an internal buffered `fired` channel that is never closed; the watch goroutine is the sole sender on and closer of `ch`, so the race is structurally impossible. Pending timers are stopped on shutdown via `defer`. A new `TestWatcher_CancelDuringDebounce` stress test cancels the context exactly at the debounce deadline over 50 iterations and reproduces the old race reliably under `-race`.

- **fix(#1393): close admin-API auth bypass and sidecar crash-loop when `admin.enabled: true`.** Two independent bugs existed:
  1. **Admin-API auth bypass (security):** The `/_vibewarden/admin/*` dedicated route in the Caddy config had only `[reverse_proxy]` in its handle chain — no `vibewarden_admin_auth` gate. Caddy evaluates routes top-to-bottom and stops at the first match; the auth handler in the catch-all chain never ran for admin paths. Every request to `/_vibewarden/admin/users` (and all other admin data routes) was proxied to the internal server regardless of the `X-Admin-Key` header value. Fixed by inlining `vibewarden_admin_auth` as the **first** handler in `buildAdminRoute` (before `reverse_proxy`), making the gate run in-path.
  2. **Sidecar crash-loop:** `usermgmt.Plugin.ContributeCaddyHandlers()` emitted a handler map with `"handler":"admin_auth"` — the wrong Caddy module name (the registered name is `"vibewarden_admin_auth"`). Caddy's `caddy.Load` rejected the unknown module name and crashed on startup whenever `admin.enabled: true`. The plugin's divergent route and handler contributions were removed; the canonical routes + gate are now built once by the caddy adapter (`config_routes.go:buildAdminRoute` and `buildConfigRoute`). `ConfigPath` (`/_vibewarden/config/`) was also missing from the handler serialisation — fixed in `buildAdminAuthHandlerJSON`.

  The config hot-reload endpoints (`POST /_vibewarden/config/reload`, `GET /_vibewarden/config`) are served by the internal admin server and now have their own canonical Caddy route with the same inlined `vibewarden_admin_auth` gate, proxying to the internal admin server. The route matches both the `/_vibewarden/config/*` subtree and the exact no-slash `/_vibewarden/config` inspection path; the middleware's config-path matcher was reconciled so the no-slash GET is also token-gated (previously it would have passed the bare prefix check tokenless). Integration tests confirm 401-without-token / 200-with-token end-to-end through a running Caddy instance for both the admin and config routes, and a clean-start test guards against the crash-loop regression.

### Fixed

- **fix(#1279): eliminate ordering dependency in per-user rate limiting (OWASP A04 hardening).** `RateLimitMiddleware` previously resolved the authenticated user ID exclusively from the `X-User-Id` request header, which is injected by `IdentityHeadersMiddleware`. In the standalone `net/http` wiring path, if `RateLimitMiddleware` ran before `IdentityHeadersMiddleware` the header was absent and per-user limiting was silently skipped — authenticated users fell through to per-IP limits only. Fixed by adding a `resolveUserID` helper that prefers the domain `Identity` stored in the request context by `AuthMiddleware` (via `IdentityFromContext`), and falls back to the `X-User-Id` header only when no context identity exists. This eliminates the inter-middleware ordering dependency entirely. The Caddy wiring path (`vibewarden_authentication` at priority 40, `vibewarden_rate_limit` at priority 50) was already ordered correctly and is unaffected. A behavioral regression test asserts that a user exceeding their per-user limit receives 429 even when the `X-User-Id` header has not been set but identity exists in context.

### Security

- **chore(#1427): triage the remaining Dependabot alert backlog; close two `updates:` coverage gaps that suppress Dependabot PRs.** The 20 alerts (8 high / 10 medium / 2 low) reported when this issue was filed were re-enumerated against `GET /repos/vibewarden/vibewarden/dependabot/alerts`. All but one had already been closed by the #1425/#1426 batch bump and the #1400 Next.js 16.3.0 bump — the alert count in the issue predates both merges. The single remaining alert is triaged as follows:
  - **`github.com/google/cel-go` v0.28.1, GHSA-gcjh-h69q-9w9g (medium) — dismissed as not-reachable, no adoptable patch.** cel-go is an indirect dependency with a single consumer: `github.com/caddyserver/caddy/v2` (`go mod why` reports "main module does not need package"; VibeWarden has zero direct cel-go imports). **No patched version is adoptable:** every version in the fixed range (v0.29.0, v0.29.1, v0.29.2, v0.30.0 — all four were built and verified) breaks Caddy v2.11.4's CEL request matcher, which passes `[]interpreter.Interpretable` to `interpreter.NewCall` where cel-go ≥ v0.29.0 now requires `[]interpreter.InterpretableV2` (`modules/caddyhttp/celmatcher.go:506,529`). Caddy v2.11.4 is the latest stable release, so there is no upstream version to move to. **The vulnerable code path is also not reachable:** the advisory requires the `ext.NativeTypes()` extension or the `cel.ParseStructTag()` option to expose unexported struct fields through CEL; Caddy registers only `ext.Strings()`, `ext.Bindings()`, `ext.Lists()` and `ext.Math()` and never enables native-type binding. (Caddy's own `caddy.ParseStructTag` in `modules.go` is an unrelated same-name function for `caddy:` struct tags, not cel-go's option.) Follow-up issue #1429 tracks re-bumping cel-go once Caddy ships a release compatible with v0.29+.

  The issue title's "no PRs filed by Dependabot" also pointed at a systemic gap, and two more instances of it were found in `.github/dependabot.yml`: `examples/node-express` (npm, `express`) and `examples/spring-boot` (maven, `org.springframework.boot`) both appear in the repo dependency graph — so they can raise alerts — but had no `updates:` entry, so Dependabot could never open an update PR for either. This is the identical failure mode that let `examples/nextjs` drift 13 advisories behind before #1400 added its entry. Both ecosystems are now registered on the same weekly Monday schedule as the rest. No product code or dependency versions changed.

- **fix(#1400): bump `examples/nextjs` to Next.js 16.3.0 — clears 13 open Dependabot alerts (8 high).** The Next.js example app shipped `next@^15.2.4` (lockfile resolved 15.5.15) and, transitively, `postcss` 8.4.31, exposing CVE-2026-44573/44574/44575/44578/44579/45109 and GHSA-8h8q-6873-q5fj (high), CVE-2026-44576/44577/44580/44581/44582 (medium/low), and CVE-2026-41305 (postcss). Staying on the 15.5.x line would have fixed only the `next` advisories: every 15.5.x release — including the current 15.5.22 — pins `postcss` to exactly `8.4.31`, so clearing CVE-2026-41305 there would have required an artificial `overrides` block in `package.json`. Next.js 16.3.0 (current `latest`) pins `postcss` 8.5.23 natively, so a single major bump clears all 13 advisories with no override hack and keeps the example on the latest stable release per the dependency policy. `react`/`react-dom` moved to 19.2.8 alongside. The example's surface is a root layout, one page, and three App Router route handlers using only `next/server`'s `NextResponse.json` — none of it touches a Next.js 16 breaking change. Verified locally: `npm audit` reports 0 vulnerabilities, `next build` succeeds, `output: 'standalone'` still emits `.next/standalone/server.js` (so `examples/nextjs/Dockerfile` is unchanged and still correct), and the built standalone server serves `/`, `/api/health`, and `/api/protected` (including `X-User-*` header pass-through) correctly. `node:22-alpine` in the Dockerfile satisfies Next 16's `engines: node >= 20.9.0`. Also added an `npm` ecosystem entry for `/examples/nextjs` to `.github/dependabot.yml` — its absence is why Dependabot only ever raised alerts and never opened update PRs for this directory, which is how the app drifted 13 advisories behind. No product code affected.

- **fix(#1421): bump `google.golang.org/grpc` to v1.82.1, `golang.org/x/text` to v0.39.0, and the Go toolchain to go1.26.5.** All three bumps clear currently-red repo-wide CI gates that block every merge: govulncheck flags `google.golang.org/grpc` v1.81.1 (indirect, pulled in via the OpenTelemetry OTLP exporters), Trivy flags the go1.26.4 standard library, and both `Go Vulnerability Check` and `Trivy Vulnerability Scan` flag `golang.org/x/text` v0.37.0 (GO-2026-5970 / CVE-2026-56852, HIGH — infinite loop in `norm.Iter`, fixed in v0.39.0). The `x/text` finding is reported as **called**, reached from `caddy.Adapter.Reload` (`internal/adapters/caddy/adapter.go`) and `postgres.NewAuditAdapter` (`internal/adapters/postgres/audit.go`), so it could not be waived as unreachable. `go mod tidy` carried the usual coupled `golang.org/x` module-graph bumps along with it (`crypto` v0.53.0, `mod` v0.37.0, `net` v0.56.0, `sync` v0.21.0, `tools` v0.47.0). Pinned `toolchain go1.26.5` in `go.mod`, `golang:1.26.5-alpine` in `Dockerfile`, and `go-version: "1.26.5"` in all CI workflows, keeping the toolchain single-sourced as in the go1.26.4 bump. No API changes.

- **fix(#1276): add `health.expose_version` to suppress version fingerprinting on `/_vibewarden/health` (OWASP A05).** The health endpoint always returned the running binary version string in its JSON body, enabling targeted CVE exploitation by giving attackers the exact sidecar version. A new `health.expose_version` config field (default `true` — no behavior change for existing deployments) allows operators to set `expose_version: false` to omit the `"version"` key entirely from the response. Port-ownership detection (`vibew doctor`) is decoupled from the version string: a stable `X-Vibewarden: 1` response header is now always emitted by the health handler regardless of `expose_version`, and `port_owner.go` checks that header first (with the old body-prefix as a backward-compatible fallback for sidecars predating this fix). No dependency changes; no breaking changes.

- **fix(#1274): extend symlink-containment check to `FileSystemStalenessWalker` (OWASP A01).** PR #1223 added a check to `computeInputDigest` (`input_digest.go`) that resolves symlink targets and skips any that escape the project root. The same escape path existed in `FileSystemStalenessWalker.NewestMTime` (`staleness.go`): a symlink inside the project pointing outside (e.g. to `/var/log`) caused the walker to count the symlink's own mtime, which could produce a spurious always-stale signal in `vibew dev`. The fix applies the identical `filepath.EvalSymlinks` + separator-terminated prefix check (`resolved == absRoot || strings.HasPrefix(resolved, absRoot+string(os.PathSeparator))`) to the staleness walker, resolving the root once outside the walk for efficiency. Escaping symlinks are skipped and debug-logged; in-root symlinks are unaffected. Completes the #1223 symlink-containment fix across both bundle walkers.

- **fix(#1281): reject path-traversal and encoded-slash injection in `secret://` URIs (OWASP A03).** Three coordinated hardening changes:
  1. **`ParseURI` (`internal/domain/secret/uri.go`)** — now rejects URIs containing `..` path segments, a leading `/` (absolute paths), or any percent character (`%`). Rejecting `%` outright blocks `%2F`/`%2f` *and* the double-encoded `%252F` (which an HTTP client decodes back to `%2F` and then to `/`); legitimate secret paths never need percent-encoding. Segment-based checking via `strings.Split` ensures a key legitimately named `a..b` is accepted while the traversal segment `..` alone is not. This closes the primary injection vector: `secret://../sys/mounts/secret/key` could previously reach unintended OpenBao API paths.
  2. **`List` (`internal/adapters/builtin/store.go`)** — replaces bare `strings.HasPrefix(path, prefix)` with `path == prefix || strings.HasPrefix(path, prefix+"/")`. The old check matched `"auth-evil"` for prefix `"auth"`. (The builtin store is an in-memory encrypted map, so this is an information-disclosure correctness fix — sibling keys leaking into `List` results — not a filesystem/HTTP traversal risk; it is included here because it is the same prefix-confusion class as the traversal bug.)
  3. **`validateSecretPath` (`internal/adapters/openbao/adapter.go`)** — new unexported helper enforces the same rules (`..` segments, leading `/`, any `%`) and is called at the top of `Get`, `Put`, `Delete`, `List`, and `GetMetadata`. Defense-in-depth: blocks traversal even when a path reaches the adapter without going through `ParseURI`.

- **fix(#805): bump `testcontainers-go` to v0.43.0 — stop compiling vulnerable `github.com/docker/docker` (CVE-2026-34040, CVE-2026-33997).** testcontainers-go v0.43.0 migrated off `github.com/docker/docker` onto the new modular `github.com/moby/moby/api` + `github.com/moby/moby/client` packages, so no `docker/docker` package is compiled into any VibeWarden binary or test binary anymore (`go mod why github.com/docker/docker` reports "main module does not need package"). This is defense-in-depth: it removes the only path that pulled the high-severity (CVE-2026-34040) and medium-severity (CVE-2026-33997) AuthZ-plugin code into a compiled artifact. `github.com/docker/docker v28.5.2+incompatible` itself **remains** in the module graph as an `// indirect` requirement — it is pulled in by `golang-migrate` (already latest, v4.19.1) and cannot be removed; it also cannot be upgraded out of the advisory range (`< 29.3.1`) because no patched `docker/docker` is published to the Go proxy (the fix exists only under the new `github.com/moby/moby/v2` module path). The two Dependabot alerts are therefore dismissed as not-reachable (test-tooling transitive, vulnerable code not compiled, no fix available). Production risk was always zero (no Docker daemon at runtime). `github.com/moby/moby` is Apache-2.0 licensed.

- **Bump Go toolchain to go1.26.4 (GO-2026-5037, GO-2026-5039).** Both vulnerabilities affect the Go standard library and are fixed in go1.26.4. GO-2026-5037 is an inefficient candidate hostname parsing bug in `crypto/x509` (reachable via TLS dialing and certificate verification). GO-2026-5039 is an arbitrary-input inclusion bug in `net/textproto` (reachable via HTTP response reading in the OpenBao adapter). Pinned `toolchain go1.26.4` in `go.mod`, `golang:1.26.4-alpine` in `Dockerfile`, and `go-version: "1.26.4"` in all CI workflows. Unblocks the repo-wide Trivy and govulncheck CI gates.

- **Upgrade Alpine OpenSSL to >=3.5.7-r0 (CVE-2026-45447).** The runtime image shipped `libcrypto3`/`libssl3` 3.5.6-r0 from the `alpine:3.23` base, which Trivy flags HIGH for an OpenSSL heap use-after-free in `PKCS7_verify()`. Floored both packages at `3.5.7-r0` in `Dockerfile` and `Dockerfile.goreleaser`; the explicit version floor also busts the stale GHA `apk upgrade` build-cache layer that kept the vulnerable version. This was the second half of the repo-wide Trivy image-scan failure blocking all PRs.

## [v0.20.1] — 2026-06-06

Theme: patch — pin the sidecar image to the CLI version (ADR-106), closing the silent CLI↔sidecar version-skew gap surfaced by the v0.20.0 release smoke test (#1385).

### Fixed

- **fix(#1385): pin sidecar image to CLI version in generated compose files (ADR-106).** The generated `docker-compose.yml` (from `vibew dev`, `vibew generate`, `vibew obs up`) and the sidecar compose inside a `vibew bundle` artifact both hardcoded `image: ghcr.io/vibewarden/vibewarden:latest` with no `pull_policy`, causing silent CLI↔sidecar version skew when a stale local `:latest` layer was cached. Release builds (goreleaser sets `main.version` without the leading `v`, matching the image tag verbatim) now emit `image: ghcr.io/vibewarden/vibewarden:<version>` with no `pull_policy` (pinned immutable tag — airgap-friendly). Dev/source builds emit `image: ghcr.io/vibewarden/vibewarden:latest` + `pull_policy: always`. The version is threaded from `main.version` → `NewRootCmd` → the four compose-rendering subcommand constructors; image ref computed in `config.SidecarImageRef`; templates only interpolate.

## [v0.20.0] — 2026-06-05

Theme: `vibew version` subcommand + 2026-06-04 docs/code drift audit. Adds the `vibew version` subcommand (#1340); fixes the OpenBao-with-`builtin`-store bug (#1369) and canonicalizes the v1 event schema with a real drift-guard test (#1368); bumps `x/net` and `x/crypto` for two HIGH-severity CVEs; and resolves nine docs/code drift findings (#1370, #1371, #1372, #1373, #1374, #1382) from a full architect/PM/writer audit.

### Added

- **feat(#1340): `vibew version` subcommand.** `vibew version` now works as a proper cobra subcommand and produces output identical to `vibew --version` (`vibew <version>\n`). Follows the convention of standard CLI tools (docker, kubectl, gh). Both invocations share a single version source in the root command.

### Security

- **Bump `golang.org/x/net` to v0.55.0 (GO-2026-5026) and `golang.org/x/crypto` to v0.52.0 (GO-2026-5018) — both HIGH-severity CVEs were reachable from our code.**

### Fixed

- **fix(#1374): correct three CLI help-text / plugin-catalog drift findings (W3, P12, P13).**
  - **W3:** Removed `Mailslurper (email sink)` from `vibew dev` help and `mailslurper  local email sink` from `vibew logs` service list. No mail container is generated by the compose template; the entry caused `vibew logs mailslurper` to match nothing.
  - **P12:** Updated `vibew add admin` and `vibew add metrics` help to reference `https://localhost:8443/_vibewarden/admin/` and `https://localhost:8443/_vibewarden/metrics` respectively. Default port is 8443 (`internal/config/config.go:307`); admin path is `/_vibewarden/admin/` (`internal/middleware/admin_auth.go`); metrics path is `/_vibewarden/metrics` (`internal/middleware/metrics.go`).
  - **P13:** Expanded the `store` field description in the rate-limiting plugin catalog entry from "memory (default)" to "memory (default) or redis (distributed, requires Redis server)". `internal/plugins/ratelimit/config.go` accepts both values.

- **fix(#1369): OpenBao container and seed-secrets job are no longer generated when `secrets.store` is `builtin` (or unset).** The compose template previously guarded all OpenBao infrastructure on `.Secrets.Enabled`, so users who set `secrets.enabled: true` with the default `store: builtin` (a self-contained encrypted file store) got an unwanted OpenBao container. The guard is now `UsesOpenBao` (enabled AND store == "openbao"). The `EgressNoProxy` helper is fixed to match. Existing `store: openbao` users are unaffected. Also gates the two on-disk OpenBao-only files (`openbao/config.hcl`, `seed-secrets.sh`) on `UsesOpenBao()` — with `store:builtin` these files were written to the bundle with no service to consume them.

- **fix(#1368): delete `internal/schema/v1/event.json` (dead data) and add a real drift-guard test.** The internal copy was a stale 662-line subset (14 event types) of the canonical 2370-line `schema/v1/event.json` (47 constrained event types). No Go code, build script, or `//go:embed` directive read it — it was pure dead data. A false doc comment in `internal/schema/v1/schema_test.go` claimed the test "mirrors" the file, but no file comparison existed, making divergence undetectable. Fixed: deleted the internal duplicate; corrected the misleading doc comment to accurately describe the hand-encoded pattern check; repointed `docs/schema-evolution.md` to the canonical path; added `TestSchemaCoversAllEventTypes` to `schema/v1/schema_validate_test.go` which constructs a payload-violating instance for each of the 47 constrained event types and asserts the schema rejects it — this test fails if any if/then branch is removed from the canonical file.

- **docs(#1382): fix observability port drift and demo-app Mailslurper claim.**
  - **O1:** Corrected four occurrences of `localhost:8080` in `docs/observability.md` to `localhost:8443` (the real default `server.port`, `internal/config/config.go:307`). Reworked the Architecture mermaid diagram: VibeWarden now shows `:8443` and the Prometheus scrape arrow now points at `otel-collector:8889` — the actual target from `internal/config/templates/observability/prometheus.yml.tmpl` — with an added OTLP push edge from VibeWarden to the collector. The troubleshooting section was updated to name the correct scrape target (`otel-collector:8889`) and fix the Prometheus targets page reference.
  - **O2:** Removed the false Mailslurper claim from `examples/demo-app/README.md:263`. The generated compose (`examples/demo-app/.vibewarden/generated/docker-compose.yml`) has no mail container; replaced with accurate behavior note.

### Documentation

- **docs(#1372): fix three docs/code drift findings in `docs/index.md`, `README.md`, and `CHANGELOG.md` (audit P5, P7, P8).**
  - **P5:** Secrets feature row in `docs/index.md` now reads "Built-in AES-256-GCM store (default) or OpenBao" — the default store is built-in (`internal/plugins/secrets/plugin.go:99`: `cfg.Store = "builtin"`), not OpenBao. Also updated the Comparison table (line 129) and the CLI table description for `vibew secret get` (line 152) to drop the OpenBao-only framing.
  - **P7:** Removed non-existent `--profile demo` compose profile reference from `docs/index.md:81` and the historical `CHANGELOG.md` v0.17-era entry. Only the `observability` profile exists in `docker-compose.yml.tmpl`; the demo app starts via `vibew dev`.
  - **P8:** Added `vibew add waf [--mode block|detect]` to CLI tables in both `README.md` and `docs/index.md`. Flag verified from `internal/cli/cmd/add_waf.go:59`: `cmd.Flags().StringVar(&mode, "mode", "detect", ...)`.

- **docs(#1370): fix four docs/code drift findings in `docs/architecture.md` and `docs/ai-log-schema.md`.**
  - **A1:** Replaced the fabricated event-type table in `docs/architecture.md` (8 of 9 names were invented: `request.completed`, `auth.allowed`, `auth.blocked`, `rate_limit.blocked`, `waf.detected`, `waf.blocked`, `secret.injected`, `secret.fetch_failed`, `upstream.error`) with the real names emitted by `internal/domain/events/events.go`. Fixed the inline example event to use a canonical `auth.success` envelope (`severity`/`category`/`timestamp` instead of `level`/`time`).
  - **A2:** Fixed `upstream.health_changed` state progression in `docs/ai-log-schema.md` from the incorrect `unknown → ok → failing` to the correct `unknown → healthy → unhealthy` (matching `internal/domain/health/health.go` and `schema/v1/event.json`).
  - **A5:** Moved the Fleet plugin row out of the "Available plugins" table in `docs/architecture.md` into a clearly-labelled "Planned / Pro-tier (not yet implemented)" note. Fleet is a locked-decision roadmap item with no implementation in `internal/plugins/`.
  - **A6:** Corrected the directory layout in `docs/architecture.md` — replaced non-existent `adapters/redis/` with `adapters/ratelimit/` (the actual location of the Redis rate-limit store: `internal/adapters/ratelimit/redis_adapter.go`).

### Documentation

- **docs(#1373): document three previously undocumented config keys (`telemetry.traces.enabled`, `egress.routes[].mtls`, `egress.routes[].prompt_injection`).**
  - **P9:** Added `telemetry.traces:` subsection to `vibewarden.reference.yaml` with `enabled: false` default. Added `v.SetDefault("telemetry.traces.enabled", false)` to `setDefaults()` so users without a `telemetry:` block get the opt-in default. Added `TestSetDefaults_EmptyYAML` assertion to pin it.
  - **P10:** Added `mtls:` and `prompt_injection:` subsections to the `egress.routes` example block in `vibewarden.reference.yaml`. All keys derived from `EgressMTLSConfig` and `EgressPromptInjectionConfig` mapstructure tags — strict-loader valid.
  - **P11:** Added `### Prompt Injection Detection` section to `docs/egress.md` with YAML example, field table, and link to the `llm.prompt_injection_blocked` event schema in `docs/ai-log-schema.md`.

### Changed

- chore(lint): allow G124 in test files; reflect.Ptr → reflect.Pointer in internal/config

### Documentation

- **fix(#1371): remove phantom `NameGrafana` domain constant and fix plugin-activation docs.** Grafana is a Docker Compose service in the `observability` profile, not a plugin. The unused `NameGrafana = "grafana"` constant has been removed from `internal/domain/plugin/names.go`. The `CLAUDE.md` "Plugin model" section and `names.go` godoc now describe the real flat top-level key activation model (no `plugins:` wrapper — the strict loader rejects unknown top-level keys). `internal/plugins/usermgmt/config.go` godoc now names the actual source keys (`admin.enabled`, `admin.token`, `kratos.admin_url`, `database.url`). `NameFleet` kept as reserved roadmap placeholder per locked decision. ADR-105. Also fixed residual `plugins:` wrapper drift in `docs/identity-providers.md` and `docs/websockets.md` runnable examples — both now use real flat top-level keys (`admin:`, `database:`, `rate_limit:`) that pass `vibew validate`.

## [v0.19.0] — 2026-05-07

Theme: audit-driven stabilization. Closes the 2026-05-03 cross-cutting audit (15 critical+high findings) plus 8 follow-up bugs surfaced by 4 smoke tests against demo.vibewarden.dev. Two breaking changes — see Migration below.

### Fixed

- **fix(#1353): demo-app — derive `tls_enabled` from `X-Forwarded-Proto` header instead of `VIBEWARDEN_PROFILE` env var.** Previously the demo's `/profile` endpoint reported `tls_enabled: false` even when vibewarden was serving HTTPS (both `vibew dev` self-signed and prod Let's Encrypt). Profile is the wrong axis — TLS is enabled by default in all profiles; only disabled when an external terminator (Cloudflare, ALB) handles it. Now derived from the standard reverse-proxy header. Misleading "Start with VIBEWARDEN_PROFILE=tls" hint text removed.
- **fix(#1351): OpenBao prod init+unseal — five follow-up bugs found in smoke test #3.** The wave-5 #1345 fix shipped a structurally correct init+unseal flow but five compounding implementation bugs prevented it from running on a real Hetzner deploy: (1) `VIBEWARDEN_PROFILE` was missing from generated `.env`, so seed-secrets skipped its prod branch entirely; (2) preserved `.env` retained deprecated `OPENBAO_DEV_ROOT_TOKEN` on re-runs; (3) OpenBao storage path was `/openbao/data` (root-owned named volume) instead of `/openbao/file` (image's pre-configured path); (4) busybox `cp` doesn't overwrite bind-mounted `.credentials` files; (5) JSON parser of `bao operator init -format=json` used `grep -A1` which broke on pretty-printed multi-line JSON. All five fixed; smoke test #3 confirms full happy path on demo.vibewarden.dev with login working.
- **fix(#1345): bundle — OpenBao prod auto-init+unseal via seed-secrets, HTTP healthcheck.** Previously fresh prod deploys hung forever: `bao status` exits 2 when uninitialized, blocking the docker-compose healthcheck and the seed-secrets wait loop, while the generated root token was never registered with OpenBao in prod mode. Now seed-secrets owns init+unseal; healthcheck uses `/v1/sys/health?uninitcode=200&sealedcode=200` to accept transient states. ADR-104.
- **fix(#1346): bundle — `seed-users.sh` now written with 0755 permissions (was 0750).** The wave-4 fix #1335 generated the script with `rwxr-x---` permissions, breaking execution by the `curlimages/curl` container (UID 100, not in owner group). Now world-readable+executable (`rwxr-xr-x`). The script carries no secrets at write-time (all credentials are env-injected at run-time), so world-readable in the bundle is safe. New artifact test `TestGenerate_SeedUsersFile_Permissions` pins the mode to prevent regression.
- **fix(#1335): bundle — include `seed-users.sh` in bundle output AND use relative volume path.** Previously bundle generated by macOS produced a docker-compose.yml with a hardcoded `/var/folders/.../T/scripts/seed-users.sh` path AND omitted the script file from the bundle. Both bugs broke Kratos-enabled deploys on Linux. Now `seed-users.sh` ships in `bundle/scripts/` and the compose volume references `./scripts/seed-users.sh:/seed-users.sh:ro`. Surfaced by the v0.19 smoke test on demo.vibewarden.dev.
- **fix(#1334): bundle — remove `disable_mlock = false` from generated `openbao/config.hcl`.** OpenBao 2.2.0+ rejects this directive ("OpenBao has dropped support for mlock"). Every Linux deploy with `secrets.provider: openbao` (i.e., every default deploy) failed at startup. Surfaced by the v0.19 smoke test on demo.vibewarden.dev (Hetzner).
- **fix(#1336): `vibew add tls` now writes `profile: prod` + `deploy:` scaffolding into the generated production override.** Previously the generated `vibewarden.production.yaml` contained only `server.port` and `tls.*` fields. Operators who ran `vibew bundle` next received SSH commands with bracketed `<your-ssh-user>@<your-ssh-host>` placeholders that copy-paste verbatim into auth failure — violating the cross-LLM literal-vs-template clarity rule (v0.18.4 retro). Now `vibew add tls` always upserts `profile: prod` and `deploy.target_platform: linux/amd64` into `vibewarden.production.yaml`. A commented-out `# host:` hint is seeded into freshly-created files. Operators only need to fill in `deploy.host` before running `vibew bundle` — all other fields are complete.
- **fix(#1304): cap TLS retry loop at a hard iteration ceiling.** Previously the retry path spun 500K–750K iterations on persistent failure with no cap, observable in the exhaustion test (the `noSleep` test seam removes the natural pacing of `time.Sleep`). New `MaxIterations(budget, poll)` helper returns `floor(budget/poll) + 2` and gates both `runTLSRetry` and the boot-gap loop. Production iteration counts stay in the low tens (30s budget / 2s poll → 17 max). Returns `ErrTLSRetryExhausted` cleanly after the cap is hit.

### Added

- **feat(#1335): new `auth.seed_demo_users: bool` config field (default `false`).** When `true` (and `auth.mode: kratos`, `kratos.external: false`), the bundle generator writes `scripts/seed-users.sh` — a script that seeds demo Kratos identities (`demo@vibewarden.dev`, `alice@vibewarden.dev`) on first deploy. Off by default so greenfield projects never receive demo vibewarden.dev credentials in their Kratos instance. The `examples/demo-app` sets `auth.seed_demo_users: true`; all other projects must opt in explicitly. Never enable in production.
- **feat(#1302): wire `api-key` auth mode into Caddy handler chain.** New Caddy module `http.handlers.api_key_auth` (priority 36) consumes `ports.APIKeyValidator` via `RuntimeServices`. When `auth.mode: api-key` is set, `ContributeCaddyHandlers` returns an `APIKeyHandler` at priority 36 (after secrets/webhooksig at 35, before Kratos/JWT auth handlers at 38+; rate-limiting runs later at 50). The handler fails closed (HTTP 500) when no validator is present. The composition root wires a `ConfigValidator` from `cfg.Auth.APIKey.Keys` when the mode is active. Architecture invariant test `TestAuthModes_AllModesHaveHandlerOrAreNone` pins that every non-None auth mode must contribute at least one Caddy handler.
- **test(#1303): four cross-cutting architecture invariants now enforced in `test/architecture/`.** Previously these patterns were enforced by code review only. Tests cover: (1) `TestDomainPackages_NoExternalImports` — no domain file may import adapters/app/cli/ports/plugins/middleware (single pre-approved exception: `internal/domain/site/site.go → internal/config` per ADR-068); (2) `TestConfigPackage_NoOuterLayerImports` — `internal/config` must not import outer layers; (3) `TestDockerErrorDetection_OnlyInClassifyDockerError` — all docker error string matching lives exclusively in `ClassifyDockerError`; (4) `TestLoadMergedConfig_RestrictedCallers` — `LoadMergedConfig` callers restricted to the approved allow-list (definition, bundle.go, env/resolver.go; `validate.go` included with a `TODO(#1301)` until that bypass is migrated). Pre-work: inline `isDockerNotFound` check in `image_inspect.go:145` migrated into `ClassifyDockerError` in `docker_errors.go`, consolidating all docker binary-absent detection into the canonical seam.

### Documentation

- **docs(#1270): vibewarden.reference.yaml — add 13 previously-missing top-level config sections.** Brings the reference file to full coverage of fields documented in llms-full.txt or defined in internal/config Go structs. Also fixes a class of latent default-bugs surfaced by the new TestReferenceYAML_UnmarshalsCleanly test: audit.enabled, audit.output, and resilience.circuit_breaker/retry fields now apply their documented defaults via setDefaults(), where previously they were silently zero.
- **docs(#1286): getting-started — document `vibew doctor --preflight` and `vibew bundle --print-deploy`.** Two pre-deploy verification commands were absent from the operator quick-start despite being part of the standard deploy loop. Added a "Pre-deploy validation" section between "Enable TLS for production" and "Validate your config" covering: `vibew doctor --preflight production` (five pre-deploy checks against the merged production config), `vibew bundle` (bundle step), and `vibew bundle --print-deploy --host <h> --user <u> --path <p>` (ad-hoc SSH override for CI / one-off deploys). Also corrects P2-severity drift in `docs/troubleshooting.md` and `llms-full.txt` (P2 is WARN, not FAIL — code is canonical).

### Security

- **fix(#1271): config — validate Deploy.Host (allowlist regex) + shell-quote in bundle output and stdout.** Closes shell-injection vector via crafted Deploy.Host values flowing into SSH command strings. Three-layer defense: config-load validation (rejects shell metacharacters), POSIX shell-quoting helper (`config.ShellQuoteSingleDeploy`, with `'\''` escape for embedded single-quotes) applied at all 5 SSH command emission sites, and `deploy.tmpl` uses single-quoted SSH target placeholders. Duplicate `shellQuoteSSHTarget` in `bundle_extras.go` replaced with the single canonical `config.ShellQuoteSingleDeploy`.
- **fix(#1267): authui — `return_to` parameter validates same-origin to close open-redirect vector.** Server-side `isSafeReturnTo` and client-side `safeReturnTo()` both reject external URLs, protocol-relative URLs, backslash variants, and CRLF-injected paths. Affected pages: `/auth/login`, `/auth/registration`.
- **fix(#1269): reject env names containing path-traversal sequences in `vibew probe --env` and `vibew doctor --preflight`.** New `validateEnvName` (`^[a-zA-Z0-9_-]+$`) returns `ErrInvalidEnvName`; defense-in-depth: resolved path is verified inside project root after `filepath.EvalSymlinks` to block symlink-escape via legitimately-named override files.
- **fix(#1301): consolidate env override path resolution through `env.FileResolver`.** `vibew bundle` (`prodConfigPathForEnv`) and `vibew validate` (`discoverProdOverride`) previously built the production override path inline, bypassing the resolver's allowlist and `filepath.EvalSymlinks` containment check added in #1269. Both callers now route through `resolveEnvOverridePath` backed by `env.FileResolver.ResolvePath` — single source of truth for env-name validation. A new `ResolvePath` method is added alongside the existing `Resolve`; it applies the same security checks but skips the lenient `LoadMergedConfig` step so callers can use their own strict loader (`config.LoadStrict` for validate, `LoadMergedConfig` for bundle).
- **fix(#1264): caddy auth handlers — strip ALL X-User-* headers from incoming requests + fix public-path prefix matching.** Closes identity-spoofing via forged X-User-* values in JWT mode (notably X-User-Name) and public-path bypass via prefix-sibling paths (e.g. /auth-evil matching /auth/*). Both Caddy-layer (config_handlers.go x-user-* glob) and Go-layer (stripXUserHeaders defense-in-depth) defenses now active.
- **fix(#1302): `auth.mode: api-key` is now actually enforced.** Previously this mode was a no-op — the validator existed but no Caddy handler invoked it, so services configured with `auth.mode: api-key` were silently unauthenticated. This release wires `APIKeyHandler` into the Caddy chain at priority 36, closing the gap. No config changes required. Operators upgrading must ensure clients have valid API keys before deploy; previously-passing unauthenticated requests will now be rejected with 401. Closes a latent OWASP A01:2021 (Broken Access Control) gap. ADR-103.

### Changed

- **chore(#1345): rename `OPENBAO_DEV_ROOT_TOKEN` → `OPENBAO_ROOT_TOKEN`.** Old name still recognized via deprecation warning in `vibew bundle` and a fallback in the credentials store for one minor release; will be removed in v0.20. The misleading `DEV_` prefix implied the token was only for dev use — it was always the credential used in prod contexts too. ADR-104.

- **chore(#1290): upgrade Ory Kratos image v1.3.1 → v26.2.0.** Catches up on 24+ CalVer versions of security/bugfixes. Five config/template files updated (docker-compose.yml, two templates, dev/kratos/kratos.yml, integration test image). The architect's pre-analysis suggested "no Go source changes" but four real breaking changes in the admin API required adapter fixes:
  - `PATCH /admin/identities/:id` (used by `DeactivateUser`) now requires JSON Patch (RFC 6902) shape `[{"op":"replace","path":"/state","value":"inactive"}]` with `Content-Type: application/json-patch+json` (was a bare `{"state":"inactive"}` JSON object).
  - `POST /admin/recovery/link` was removed; replaced with `POST /admin/recovery/code` (used by `generateRecoveryLink`).
  - `GET /admin/identities` pagination switched to 0-indexed pages; the adapter now translates caller-facing `Page=N` to wire-level `page=N-1`.
  - Session token detection: API-flow tokens (`ory_st_*` prefix) now use the `X-Session-Token` header on Kratos requests; browser cookies still use the `Cookie` header (auto-detected by prefix).
  Operators upgrading: pull the new image; `kratos-migrate` handles schema migrations automatically. Direct callers of vibewarden's admin adapter are unaffected — the breaking changes are absorbed inside the adapter.
- **chore(#1316): publish `llms-full.txt`, `llms.txt`, and `vibewarden.reference.yaml` as GitHub Release assets.** The website (vibewarden.dev) will fetch these at build time, eliminating the silent drift that bit v0.18.7. Mirrors the ADR-101 pattern already used for `agent-kickoff-{dev,deploy}.txt`. A new architecture invariant test (`test/architecture/release_assets_test.go`) fails the build if any of these files is absent or empty from the repo root.

### Removed

- **chore(#1302): delete orphan `internal/adapters/logprint/` package.** Zero callers in the main module. The package was superseded by `internal/adapters/log/` (slog-based) and was the only reason `fatih/color` would have remained exclusively as a logprint dependency. Removed `printer.go` and `printer_test.go`.
- **chore(#1300): remove dead StateSync port + adapters + domain/sync.** Zero external callers. Cross-instance state-sync was scoped under `epic:state-sync` but never wired into any handler chain. Files removed: `internal/ports/statesync.go`, `internal/adapters/statesync/` (5 files), `internal/domain/sync/` (2 files). `redis/go-redis` remains in go.mod (used by ratelimit adapter and plugin).

### Migration

**OpenBao token env var rename (#1345):**
- Operators with existing prod deployments using `OPENBAO_DEV_ROOT_TOKEN` in `.env`: `vibew bundle` now emits a deprecation warning + backward-compat fallback works for v0.19. Rename the variable in your `.env` before v0.20 ships.
- First-time prod deploys: just use `OPENBAO_ROOT_TOKEN` — `seed-secrets` writes it on first init.

**`auth.seed_demo_users` opt-in (#1335):**
- Projects with `auth.mode: kratos` that previously got auto-seeded demo identities (`demo@vibewarden.dev`, `alice@vibewarden.dev`) on first deploy must now set `auth.seed_demo_users: true` in `vibewarden.yaml` to retain that behavior.
- Greenfield Kratos projects: no action needed — demo seeding is correctly off by default.

## [v0.18.7] — 2026-05-05

Theme: doc-lie criticals from the 2026-05-03 cross-cutting audit (#1265, #1266, #1268). Three single-line fixes to surfaces that misled AI agents and operators. No code changes.

### Fixed

- **fix(#1265): `llms-full.txt` no longer claims `vibew init` generates the `/health` endpoint.** Stale line removed (was directly contradicting the post-#1202 Application contract). The example Python snippet stands on its own.
- **fix(#1266): CHANGELOG v0.18.3 image-identity error message now correctly cites v0.18.3 instead of v0.19.0.** The runtime error and `docs/troubleshooting.md` already had the right version; only the CHANGELOG block was wrong.
- **fix(#1268): `llms-full.txt` `security_headers.content_security_policy` default is now documented as `""` (disabled).** The previous documented default suggested CSP was set out of the box; the actual code default in `internal/config/config.go` is empty. `vibewarden.reference.yaml` already documented this correctly.

### Changed

- **chore: ADR audit cleanup** — applied the 2026-05-03 ADR audit (full report at `~/notes/vibewarden/audit-adr-2026-05-03.md`). 13 ADRs demoted to `docs/internal/` or `docs/observability.md`, 11 replaced with tombstones (includes ADR-497 anomaly), 3 ADRs gain new Status banners (ADR-058, ADR-063, ADR-070). ADR numbers remain stable (no renumbering); existing PR / commit references continue to resolve to demoted stubs or tombstones. ADR-074 missing-reference clarification added to `decisions/README.md`. Active ADR count drops to 42 KEEP; 3 pre-existing historical banners (ADR-080, ADR-081, ADR-088) unchanged. (Audit report: `~/notes/vibewarden/audit-adr-2026-05-03.md`.)

## [v0.18.6] — 2026-04-30

Theme: single-issue patch release for the v0.18.5 Codex retro finding. `vibew dev`, `vibew bundle`, `vibew logs` now wrap raw Docker socket errors with an actionable operator hint (macOS / Linux per-OS recovery commands, original error preserved). New `ErrDockerSocketPermission` + `ErrDockerDaemonNotRunning` sentinels wrap the existing `ErrDockerUnavailable` umbrella; existing `errors.Is` callers continue to match. Substring detection consolidated into a single `ClassifyDockerError` helper (replaces the previous duplicated detection in compose-logs-stream and image-inspect adapters). 21 new tests + 4 build-adapter sad-path integration tests. No new ADRs.

### Changed

- **changed:** `vibew dev`, `vibew bundle`, and `vibew logs` now wrap raw Docker socket errors with an actionable operator hint (#1255). Substring detection on `permission denied while trying to connect to the docker API` and `Cannot connect to the Docker daemon` triggers a clean formatted block: "Ensure Docker Desktop is running... On macOS: open Docker Desktop. On Linux: sudo usermod -aG docker $USER && newgrp docker." Underlying error preserved below the wrapped message. `vibew probe` does not shell docker and is unaffected. New `ErrDockerSocketPermission` and `ErrDockerDaemonNotRunning` sentinels both wrap the existing `ErrDockerUnavailable` umbrella; existing `errors.Is(err, ErrDockerUnavailable)` callers continue to match. All three wired commands exit with code 3 on Docker unavailable. (retro-0.18.5 Codex finding)

## [v0.18.5] — 2026-04-30

Theme: v0.18.4 retrospective fixes — first cross-vendor smoke (Codex agent) surfaced LLM-shape assumptions that prior Claude smokes had absorbed implicitly. Six retro-tagged pipelines (#1242–#1247) covering: per-env error messages on `vibew probe` (CRITICAL — was telling production users to run `vibew dev`); ACME-aware TLS retry on `vibew probe --env <name>` (30s budget); SSH placeholder bracketing + new `deploy.host` config + Codex prompt preambles ("vibew init does not scaffold", "VibeWarden does not deploy for you"); `vibew bundle --print-deploy --host --user --path` ad-hoc override; `vibew doctor --preflight <env>` pre-deploy validation; AGENTS-VIBEWARDEN.md must-know checklist at the top. CLAUDE.md gains a §Architecture principles bullet locking cross-LLM literal-vs-template clarity. No new ADRs; all changes refine the v0.18.3/v0.18.4 architecture rather than establish new patterns.

### Added

- **`vibew doctor --preflight <env>`** (#1246) — pre-deploy validation against a named env (e.g. `--preflight production` reads `vibewarden.production.yaml`). Five new checks: DNS resolves the merged `tls.domain`, `server.port` is 443 for production, `deploy.target_platform` is set, app image arch matches `deploy.target_platform`, `tls.email` is configured (Let's Encrypt warning only). Reuses the env-resolver from #1233 and the image-inspect path from #1219. Static config + Dockerfile checks still run; preflight section appends afterward.

- **`vibew probe --env <name>` retries TLS handshake errors during ACME issuance** (#1243). When the probe hits a recognised TLS handshake error (`tls: internal error`, `tls: handshake failure`, `bad certificate`, `tls: protocol version not supported`) AND `--env` is set, retry every 2s for up to 30s with progress messages on stderr. After 30s, exit 1 with an actionable message pointing at `docker compose logs vibewarden | grep -i acme`. Default mode (no `--env`) does not retry on TLS errors — localhost dev cert is from Caddy's local CA; immediate failure is a real config bug. Recovers from the v0.18.4 retro Codex finding where the first probe right after `docker compose up -d` failed during ACME issuance and the unactionable `tls: internal error` was the only signal.

- **`vibew bundle --print-deploy --host <h> --user <u> --path <p>`** (#1245) — ad-hoc, per-invocation override for the printed "Next: deploy" stdout block. All three sub-flags required when `--print-deploy` is set; flag wins over `deploy.host` from `vibewarden.production.yaml`. Bundle README is unaffected (config and placeholder paths only). Useful for one-off deploys and multi-host CI without mutating config. (retro-0.18.4 follow-up)

### Changed

- **changed:** SSH target placeholder in vibew-emitted deploy commands is now `<your-ssh-user>@<your-ssh-host>` (was: `user@<domain>`). Codex agent followed the old form literally and hit auth failure on the v0.18.4 demo deploy. New `deploy.host` field in `vibewarden.production.yaml` (mirrors `deploy.target_platform`) — when set, `vibew bundle` stdout and bundle README substitute the configured host verbatim. Kickoff release artifacts (post-#1232) keep the bracketed placeholder forever (released-once, can't know any user's config). Two new prose preambles in the kickoff prompt clarify (a) `vibew init` does not scaffold app code/Dockerfile, (b) VibeWarden does not deploy for you. CLAUDE.md gains a §Architecture principles bullet locking the cross-LLM literal-vs-template clarity rule. (#1244, retro-0.18.4 Codex finding)

### Documentation

- **docs(#1247): AGENTS-VIBEWARDEN.md gains a "Quick reference (must-know checklist)" section at the top.** 6 lines for the must-knows (bind 0.0.0.0, listen on upstream.port, GET /health, Dockerfile EXPOSE match, no app security, run doctor && dev && probe). Lets agents start before reading the longer reference. Mirrored across the canonical doc, the init template, and llms-full.txt. (#1247, retro-0.18.4 Codex follow-up #6)

### Fixed

- **fixed:** `vibew probe --env <name>` now emits a per-env error message on connection-refused. Default mode still says `Stack is not running. Start with: vibew dev`. With `--env <name>`: lists real causes (bundle not deployed, host down, DNS misconfig, sidecar exited) without suggesting `vibew dev`. New `ErrDNSFailure` sentinel surfaces DNS resolution failures separately with their own actionable message. Failure modes pinned by golden tests across all four variants. (#1242, retro-0.18.4 Codex finding)

## [v0.18.4] — 2026-04-30

Theme: v0.18.3 retrospective fixes. Four retro-tagged pipelines (#1217 #1232 #1233 #1234) covering the dotfile-safe deploy contract (tar pipe replaces buggy scp glob across four surfaces — three retros flagged this), the systemic agent-kickoff-artifact pattern that makes vibewarden.dev's `/start` page fetch from main repo's release tag (eliminating drift by construction), the new `vibew probe` verb that bypasses macOS LibreSSL via Go's stdlib HTTP, and a Dockerfile-contract docs bullet about frozen-install lockfiles. Two small companion patches (#1237 release-artifacts dist path, #1238 dry-run workflow follow-up tracked separately). One ADR landed: ADR-101 (kickoff release artifacts, content authority) plus ADR-102 (vibew probe + reusable env-resolver pattern). Companion website work tracked at vibewarden/vibewarden.dev#95 — fetches the new artifacts at build time once this release is tagged.

### Added

- **`vibew probe [--env <name>]`** (#1233, ADR-102) — Go-stdlib HTTPS probe of `_vibewarden/health`. Default probes `https://localhost:<server.port>` with `InsecureSkipVerify` (bypasses macOS LibreSSL friction by construction). `--env <name>` resolves `vibewarden.<name>.yaml`, reads merged `tls.domain`, probes the production endpoint with full cert verification. Boot-gap retry: up to 10s when `components.upstream:"unknown"`. Generalizable env-resolver in `internal/app/env/` is available for future verbs to adopt (`vibew status --env prod`, `vibew validate --env prod`, etc.); migration of existing verbs is out of scope here.

- **Release artifacts: `agent-kickoff-dev.txt` and `agent-kickoff-deploy.txt`** (#1232, ADR-101). Goreleaser now emits both flavors of the canonical agent kickoff prompt as release assets, with `{{prjname}}`, `{{description}}`, and `{{domain}}` two-brace placeholders for consumers to substitute. Stable URL: `https://github.com/vibewarden/vibewarden/releases/latest/download/agent-kickoff-dev.txt`. The website (vibewarden/vibewarden.dev) will fetch these at build time, replacing the hand-rolled JS template literal that drifted across three retros (vibewarden.dev#95). Forensic CI test asserts both artifacts contain the post-#1138 deploy contract (`docker load -i image.tar && docker compose up -d`), include the post-#1217 tar pipe transfer, and do NOT contain `bash deploy.sh` or the buggy `scp -r .vibewarden/bundle/*` glob form.

### Documentation

- **docs(#1234): AGENTS-VIBEWARDEN.md Dockerfile contract gains a "frozen-install lockfiles" bullet.** Catches the v0.18.3 retro friction where an agent's `npm ci` Dockerfile failed at build time because no `package-lock.json` was shipped. Language-agnostic by examples (npm/pnpm/yarn/bun/pip-sync/cargo --locked). Mirrored across the AGENTS template, the docs example, and llms-full.txt.

### Fixed

- **fixed:** bundle stdout, bundle README, `vibew prompt-template --deploy` output, and `llms-full.txt § Agent Kickoff Prompt` all switch from `scp -r .vibewarden/bundle/* user@host:/path/` (which silently drops dotfiles like `.env`, `.credentials`, `.env.template` because shells don't include dotfiles in `*` glob) to a POSIX tar pipe: `tar -czf - -C .vibewarden/bundle . | ssh user@host 'tar -xzf - -C /opt/<app>/'`. Three retros (v0.18.1, v0.18.2, v0.18.3) flagged this; the v0.18.3 deploy shipped a "successful" stack that was missing `.env` and required manual recovery. Tar pipe is dotfile-safe by construction, single ssh connection, POSIX baseline. Forensic alignment test extended to enforce the new transfer command across all four surfaces and forbid the buggy form. (#1217)

## [v0.18.3] — 2026-04-28

Theme: v0.18.2 retrospective fixes. Six retro-tagged pipelines (#1219–#1224) covering image-identity collision (the "most dangerous failure mode" of the retro), `--rebuild` recovery flag, local logs wrapper, doctor pre-stack noise reduction, bundle freshness false-positive (also fixed a security-relevant symlink-escape bug discovered during review), and macOS LibreSSL advisory. One breaking-for-existing-images change is user-visible: `vibew dev` blocks on stale images from a different project, recoverable via `vibew dev --rebuild`. See "Behavior changes" first.

### Behavior changes

**`vibew dev` now blocks on stale images from a different project (#1219, ADR-100).**
`vibew build` stamps two Docker labels on every produced image:
`org.vibewarden.project-root-hash` (sha256 of the realpath — used for equality) and
`org.vibewarden.project-root` (informational literal path — shown in error messages).
`vibew dev` inspects these labels before compose-up and exits 1 with an actionable
message when the label is missing or points at a different project root.

This closes the v0.18.2 retro's silent-image-collision bug where a `qr-code-blackhole-app:latest`
image from a prior unrelated project of the same directory name was silently reused,
causing the stack to serve foreign content while reporting "healthy".

**Breaking for existing local images.** Every image built by VibeWarden ≤ v0.18.2 is
unlabelled. The first `vibew dev` after upgrading will block with:

```
Error: app image <tag> is missing the vibew project-root label.
  This image was built before VibeWarden v0.18.3 OR by something other than vibew build.
  Current project: <path>

Rebuild with: vibew dev --rebuild
```

Recovery: rebuild via `vibew build` (or `vibew dev --rebuild` once #1220 lands).
Custom user-managed images set via `app.image:` in vibewarden.yaml are skipped
automatically with an informational stderr line.

**Release gate satisfied by #1220.** `vibew dev --rebuild` ships in the same release
as #1219 (both in v0.18.3). The recovery command in the error message above is live.

### Changed

- **`vibew doctor` no longer probes runtime checks before `vibew dev` is up** (#1222). Three checks were misleading pre-stack: "Generated files", "Container health", "TLS certificate". Generated files + TLS certificate are now gated on stack-state detection (run only when `docker compose ps` returns containers); Container health is deleted entirely (covered by `/_vibewarden/health` since #1197). New `vibew doctor --help` text reflects the narrower scope: static config + Dockerfile + toolchain pre-stack; TLS + generated-files post-stack. Same misleading-warn class as the upstream-reachable check deleted in #1198.

### Fixed

- **`vibew bundle` no longer fires STALE warnings on a fresh build** (#1223). Root cause: `WriteInputDigest` was mutating the user's `.gitignore` to add `.vibewarden/` exclusions, which bumped the file's mtime inside the watched input set, tripping the freshness check on the next bundle. Fix: drop `.gitignore` mutation; vibew now writes `<projectRoot>/.vibewarden/.gitignore` (`*\n`) for self-contained exclusion. Freshness comparison switches from mtime to per-file SHA-256 (digest schema v2). The freshness block now lists up to 5 changed paths labeled `added`/`removed`/`modified` so users know exactly which file tripped the warning. Existing v1 digest files are treated as missing on first read — first post-upgrade bundle is FRESH baseline, no false positive.

### Added

- **`vibew doctor` advisory note for macOS**: system curl (LibreSSL) may fail handshake on the local dev cert. Doctor now prints a darwin-only note pointing at Homebrew curl + python3 ssl alternatives. See `docs/troubleshooting.md` for the workarounds. (#1224)
- **`vibew dev --rebuild`** — collapses the four-command rebuild dance
  (`vibew down && docker rmi <tag> && vibew build && vibew dev`) into a single command
  (#1220). Stops the stack, removes the resolved app image, rebuilds via `vibew build`
  (which stamps #1219's identity labels on the new image), then starts the stack.
  Volumes are preserved by default; pass `--rebuild --volumes` for explicit named-volume
  reset (Postgres data, Let's Encrypt certs, etc.). This is the recovery path for
  the image-identity mismatch block introduced by #1219 in this same release.
- **`vibew logs [--tail N] [--follow] [--since <duration>] [<service>...]`** — local dev-stack logs wrapper (#1221). All services interleaved by default; positional service args (variadic, e.g. `vibew logs vibewarden app`) scope the output. `--tail` defaults to 100. `--follow` streams. `--since` accepts any duration docker compose supports. Stack-not-running emits `Stack is not running. Start with: vibew dev` (exit 1). Unknown service lists known service names. Docker unavailable maps to exit 3 via the existing `ports.ErrDockerUnavailable` sentinel.

## [v0.18.2] — 2026-04-28

Theme: v0.18.1 retrospective fixes. Eight retro-tagged issues + two smoke catches covering health-endpoint correctness, deploy-pipeline drift, language-agnostic onboarding, and a CI guard against re-introducing removed artifacts. Two breaking changes are user-visible: the `/_vibewarden/health` JSON wire format and `vibew bundle`'s arch-mismatch behavior. See "Breaking changes" first.

### Breaking changes

**`/_vibewarden/health` wire-format change** (#1197, ADR-098). If you parse the JSON
body of this endpoint, update your parser before upgrading.

| Field | Before | After |
|---|---|---|
| `components.upstream` | `"healthy"` / `"unhealthy"` / `"unknown"` | `"ok"` / `"failing"` / `"unknown"` |
| outer `status` | always `"ok"` (hardcoded) | `"ok"` / `"degraded"` / `"failing"` (worst-component-wins) |
| HTTP status | 200 | 200 (unchanged) |

`"unknown"` on `components.upstream` now means only one thing: the first probe has not
yet completed (boot gap of ~5–10s). After that it is always `"ok"` or `"failing"`.

**`vibew bundle` arch mismatch is now a hard failure** (#1200). Previously the mismatch
emitted a warning and the bundle was written anyway. Any CI or deploy script that
continued past a `vibew bundle` warning is now broken: the command exits with code 1
and writes no files. Fix: run `vibew build --platform linux/amd64` before `vibew bundle`
when the image arch does not match the VPS target.

### Added

- **Bundles include an auto-generated `MANIFEST.md`** listing every file in the bundle with a one-line description (#1204). Entries are sorted alphabetically; unknown files receive a generic "bundle artifact" description so future generators don't silently drop entries.
- **Read-only inspection recipes** (logs, ps, healthcheck) are now documented in the bundle README (#1204). Folds in the dropped `vibew remote logs` proposal.
- **`vibew prompt-template`** — emits the canonical agent kickoff prompt to stdout (#1203, ADR-099). Two flavors: default (dev only) and `--deploy` (adds bundle + scp + ssh + healthcheck). Always uses `vibew init --name <prjname> --describe "<desc>"`. `--domain` is required when `--deploy` is set. See `docs/agent-kickoff.md` and ADR-099.

### Changed

- **`vibew doctor` no longer probes the upstream port directly** (#1198). The check was structurally wrong — the upstream lives on the docker-compose internal network, never bound to the host — and produced misleading "unreachable" warnings before `vibew dev` ran. Runtime upstream health is reported by `/_vibewarden/health` (#1197).
- **Bundle README now opens with a fenced shell block containing the literal deploy commands** (was: prose-only) (#1204). `app.name` and `tls.domain` are substituted; empty values produce `<your-app>` / `<your-domain>` placeholders. With `--skip-image` the `docker load -i image.tar &&` clause is omitted so the block is always copy-pasteable.
- **`vibew bundle` stdout now prints the literal `ssh`/`scp`/`docker compose up -d` sequence** with `app.name` and `tls.domain` substituted (#1204). An agent has a copy-pasteable next step without opening the README.

### Fixed

- **chore(#1201): removed-artifact CI grep guard added** (`internal/quality/removed_artifacts_test.go` + `.github/removed-artifacts.txt`). First entry: `deploy.sh`. Fails CI if a removed-artifact name appears outside `CHANGELOG.md` / `decisions/`. Add new entries to the registry when deprecating a named artifact. Also fixes 4 stale `deploy.sh` godoc references in `internal/app/bundle/` and 3 stale test-assertion references in `internal/app/bundle/` and `test/integration/`.
- **docs(#1202): removed false claims that `vibew init` scaffolds app code or a starter `main.go`.** The sidecar is language-agnostic and only writes config + AGENTS files. Added an `## Application contract` section to `docs/examples/AGENTS-VIBEWARDEN.md` with language-neutral requirements (listen on `upstream.port`, bind to `0.0.0.0`, serve `GET /health`). `vibew init` now prints a 3-line next-steps hint pointing at the contract and at `vibew prompt-template`.
- **fix(#1213): init AGENTS template now contains the language-agnostic `## Application contract` section.** #1202 updated `docs/examples/AGENTS-VIBEWARDEN.md` (the canonical reference) but missed the actual init template at `internal/cli/templates/agents/agents-vibewarden.md.tmpl`, so generated agent docs still carried the old `## Required: /health endpoint` heading. Added a smoke regression test asserting the generated file contains the new section.
- **fix(#1215): `vibew bundle` now substitutes `tls.domain` from `vibewarden.production.yaml`** into the README's fenced deploy block and the "Next: deploy" stdout block. Previously the substitution only consulted the un-merged base config; production-only domains rendered as `<your-domain>` placeholder, defeating the purpose of #1204's copy-pasteable deploy commands. Added a regression test covering domain pickup from production.yaml.
- **`vibew bundle` now fails (was: warned) when image architecture doesn't match `deploy.target_platform`** (#1200). The primary case is Apple Silicon builds (linux/arm64) landing on amd64 VPS hosts without being noticed. On mismatch the bundle aborts before writing any files and prints: `image arch is linux/arm64, target is linux/amd64. Rebuild with: vibew build --platform linux/amd64` / `Then re-run: vibew bundle`. Default target is `linux/amd64`. Override via `--target-platform` flag or `deploy.target_platform` in `vibewarden.production.yaml`.
- **`/_vibewarden/health` now reports real upstream state** (#1197). Previously the route was a hard-coded Caddy `static_response` that always returned `"upstream":"unknown"` regardless of actual upstream health. The existing `HTTPChecker` adapter, `UpstreamHealthChecker` port, and `UpstreamHealth` domain entity were fully implemented but never wired into the production composition root. Fix: the route is replaced by a new `vibewarden_health` Caddy handler module that reads the cached probe result from `RuntimeServices`; the probe default changes from `enabled: false` to `enabled: true` with interval=5s/timeout=2s; the checker is constructed, started, and stopped in `cmd/vibewarden/wiring_serve.go`.
- **`docker compose` project name is now consistent between `vibew dev` and the bundled deploy** (#1199). Both environments use the canonical `app.name` from `vibewarden.yaml`. Previously `vibew init` only wrote `name:` when `--name` was passed; without it, `vibew dev` derived the project name from the directory basename and `vibew bundle` fell through to a legacy image-tag branch that yielded `vibewarden-app`, leaving `docker ps | grep <app>` returning different containers in each environment. Fix: `vibew init` and `vibew wrap` now unconditionally write `name:`, defaulting to the directory basename. `ComposeProjectName()` no longer has an `App.Image` derivation branch. `DeriveProjectName()` is now a thin sanitising wrapper around `ComposeProjectName()`.
- **Upgrade note (#1199):** existing remote deployments running under the old project name `vibewarden-app` should run `docker compose -p vibewarden-app down` ONCE on the remote before bringing up the new stack. The bundle README includes this note.

---

## [v0.18.1] — 2026-04-28

Theme: polish from the v0.18.0 deploy to demo.vibewarden.dev and follow-up smoke testing. No new features; UX and correctness tightening.

### Changed

- **`vibew validate` auto-checks `vibewarden.production.yaml` when present** (#1180). Previously only the base file (or the file passed via `--config`) was checked, so production-only failures (WAF `mode: log`, ACME-incompatible domain in prod overrides, etc.) silently passed unless the user knew to invoke validate twice. Now `vibew validate` (no args) discovers `vibewarden.production.yaml` next to `vibewarden.yaml` and runs all 5 runtime checks against both files. FAIL rows annotate the source: `FAIL (vibewarden.production.yaml)  waf.mode: log — ...`. Explicit `--config <file>` keeps the existing single-file behavior.
- **`vibew add tls --domain X` auto-sets `provider: letsencrypt` for LE-compatible domains** (#1188). Previously only wrote the domain to `vibewarden.production.yaml`, leaving the merged config with `provider: self-signed` (from base) plus a real-world domain — the v0.18.0 deploy to `demo.vibewarden.dev` had to manually edit production.yaml. Now the command classifies the domain via the same checker `vibew validate` uses (`internal/app/tlsdomain`); LE-compatible domains get `provider: letsencrypt` + domain; localhost / IP / RFC 1918 / `.local` / `.test` domains get only the domain plus a stderr hint about picking a provider manually. Explicit `--provider <self-signed|letsencrypt|external>` flag honored as override.

### Fixed

- **`vibew status` annotates self-signed dev cert** (#1181). Self-signed dev certs (~12-hour TTL, auto-rotated by Caddy) used to render as `TLS: obtained (expires in 0 days)   OK` — technically correct (rotation happens) but visually alarming. The classifier now returns `KindSelfSignedLocal` whenever the cert was issued by the local CA regardless of `tls.provider` config, so the existing dev annotation from #1143 / ADR-095 fires correctly.
- **Match Caddy intermediate-CA issuer CN via prefix, not equality** (#1194). The #1181 fix used exact-equality on `"Caddy Local Authority"` but Caddy stamps the leaf's issuer as the intermediate CA's CN — e.g. `"Caddy Local Authority - ECC Intermediate"` or `"... - RSA Intermediate"`. The equality check never fired, so dev certs were still classified as `KindObtained` with `expires in 0 days`. Fix: `tlsdomain.IsCaddyLocalIssuer(cn)` uses `strings.HasPrefix` on the constant prefix `"Caddy Local Authority"`. All three classifier sites (in-process resolver, handshake resolver, app-layer fallback) use the helper. Surfaced by v0.18.0 smoke test pass-2.
- **`vibew obs up` success message lists all UIs** (#1186). Previously printed only Grafana + Prometheus URLs; now also lists Loki (`/ready`) and Jaeger. Ports come from `observability.*_port` config keys (Jaeger is hardcoded — Jaeger port is not yet a config key).

---

## [v0.18.0] — 2026-04-28

Theme: agent-deploy stability. Every entry below traces to the qr-dali deploy retrospective on v0.17.0 (~/notes/vibewarden/retro-0.17.0.md). Three blockers found in v0.18.0-candidate smoke testing were fixed before tagging (#1176, #1177, #1178, #1184).

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

- **Observability config files now generated as files, not empty directories** (#1184). PR #1182 removed the `cfg.Observability.Enabled` gate from the compose template (correct — `profiles: [observability]` is the new gate) but left the same gate on the file generator at `internal/app/generate/service.go:292`. Result: when `vibew obs up` ran the obs profile, Docker auto-created missing bind-mount sources as empty directories, and Prometheus / Loki / Grafana / Promtail / otel-collector all failed to start with `not a directory: Are you trying to mount a directory onto a file (or vice-versa)?`. Fix: remove the gate symmetrically — the file generator now always writes obs configs, regardless of `Observability.Enabled`. The configs are inert until the obs profile activates.

- **`vibew bundle --skip-image` now works on a fresh `vibew init` project** (#1178). The generated `vibewarden.production.yaml` carried a stale `tls: provider: letsencrypt` block that #1145 was supposed to remove. With no `tls.domain` set, validation failed at bundle time with an opaque `tls.domain is required when tls.provider is "letsencrypt"`. Fix: production template strips the `tls:` block (it was only relevant after `vibew add tls --domain ...`); template test asserts the absence as a regression guard; bundle's error wrapper now hints at `vibewarden.production.yaml` so any user who DOES set `tls.provider` without a domain knows where to look.

- **`vibew obs up` / `vibew obs down` actually work** (#1176, #1177, [ADR-097](decisions/adr-097-obs-up-down-fix.md)). Two release-blocker bugs in v0.17.0's #1149: (a) `vibew obs up` silently no-opped because the generated compose only included the obs services when `cfg.Observability.Enabled = true` — the profile gate was never hit; (b) `vibew obs down` stopped the entire project because compose's `--profile X` flag doesn't scope `down`. Fix: obs services emit unconditionally in the compose, gated by `profiles: [observability]`. `obs down` now runs service-targeted `compose stop + rm` against `grafana`, `prometheus`, `loki`, `promtail`, `otel-collector`, and `jaeger` only — main stack is preserved.

- **`vibew status`: disabled features now show OFF instead of probing and failing; self-signed dev TLS no longer triggers near-expiry alarm** (#1143, [ADR-095](decisions/adr-095-status-three-state-ok-off-fail.md)). The status dashboard now uses three text labels — `OK`, `OFF`, `FAIL` — instead of `✓`/`✗` glyphs. Features disabled in config (auth off, metrics off) render `OFF` and suppress the HTTP probe entirely; false alarms such as `✗ Auth (Kratos) unreachable` on a `vibew dev` stack with `auth.mode: none` are eliminated. Self-signed dev TLS (`KindSelfSignedLocal`) maps to `OK` with a `self-signed (dev)` annotation; expiring-soon certs (`KindExpiringSoon`) also map to `OK` with an annotation — `FAIL` is reserved for `KindFailing` (ACME renewal failure). A one-line legend (`States: OK = healthy   OFF = disabled   FAIL = check failed`) is printed below the table header. TTY output is coloured (green/dim/red); non-TTY and `--no-color` output is plain text.

- **`vibew bundle` staleness check now uses content-hash, not mtime** (#1146, [ADR-089](decisions/adr-089-bundle-image-health-tag-scoping-freshness-arch.md) §Refinement). `touch vibewarden.yaml` (no content change) no longer triggers a false STALE warning. SHA-256 is computed over the same file set the existing staleness walker considers (same `.gitignore` / `.dockerignore` / `hardIgnoreDirs` rules). The digest is stored at `.vibewarden/.input-digest` after every successful bundle and auto-appended to the project `.gitignore`. First-run and corrupt-digest paths fall back to the existing mtime comparison — no flag-day on upgrade.

- **`vibew bundle` no longer ignores the cwd-basename fallback for project name** (#1141, [ADR-093](decisions/adr-093-bundle-image-name-cwd-basename-fallback.md)). With no `name:` field set, `vibew bundle` and `vibew bundle --build` now both look for `<dirname>-app:latest`, matching the documented behavior. Previously `vibew bundle` resolved the image tag via `cfg.ComposeProjectName()` which silently fell through to the literal `"vibewarden"` when `ProjectRoot` was not populated by the loader, while `vibew bundle --build` correctly derived the cwd-basename — two different names from the same input. `deriveProjectName` is now the single project-name authority inside `runBundle`; its result is fed to both the image-tag default and the `--build` step.

- **`vibew validate` ACME-incompatible-domain message uses singular `tls.domain`** (#1179). The error message was pointing users at `tls.domains` (plural list) — a non-existent key. The config field is `tls.domain` (singular string). Surfaced during v0.18.0-candidate smoke testing.

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
- Profile-based Docker Compose: `--profile observability`
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

[v0.18.1]: https://github.com/vibewarden/vibewarden/releases/tag/v0.18.1
[v0.18.0]: https://github.com/vibewarden/vibewarden/releases/tag/v0.18.0
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
