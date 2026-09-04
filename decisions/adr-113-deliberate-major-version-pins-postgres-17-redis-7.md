# ADR-113: Deliberate major-version pins for two referenced service images — Postgres 17 and Redis 7

**Date**: 2026-09-04
**Issue**: [#1495](https://github.com/vibewarden/vibewarden/issues/1495)
**Status**: Accepted

---

## Context

PR #1484 (issue #1298) taught Dependabot to see the image pins inside the compose
templates and example Dockerfiles. It immediately opened ten PRs (#1485–#1494). Eight are
routine. Two are majors that change what a user's stack actually is:

| PR | Bump | Pin sites |
|---|---|---|
| #1489 | `postgres:17-alpine` → `18-alpine` | `docker-compose.yml`, `internal/config/templates/docker-compose.yml.tmpl` |
| #1485 | `redis:7-alpine` → `8-alpine` | same two |

`CLAUDE.md` says to use the latest stable version of everything and not to pin back
"without an explicit reason documented in an ADR". This is that ADR. It also has to
settle a prior question the issue assumed away: whether `CLAUDE.md`'s approved-license
list (Apache 2.0, MIT, BSD-2, BSD-3) applies to a third-party server image named in a
generated compose file at all.

---

## Decision

### 1. Scope of the approved-license list

`CLAUDE.md`'s approved-license list governs **code linked into the VibeWarden binary and
artifacts VibeWarden redistributes**. It does not automatically reject a third-party
server image that a generated compose file names, that the user's own Docker daemon pulls
from a public registry, and that VibeWarden talks to over a network protocol.

Nothing is linked and nothing is redistributed in that arrangement. The network-copyleft
obligations in AGPLv3 and SSPLv1 attach to whoever *offers the licensed program* as a
service, not to a client speaking its wire protocol. RSALv2's restriction is likewise on
providing the software to third parties as a managed service.

The list stays strict and unmodified. This is the same posture as ADR-109: record the
reasoning as a reusable ruling rather than widen the allow-list.

Two constraints attach to the referenced-service category, so it is not a loophole:

1. Prefer a permissively-licensed upstream whenever a drop-in exists.
2. Referenced-image licenses are still checked and recorded. "Not a dependency" means the
   allow-list does not mechanically reject it, not that nobody looks.

### 2. Postgres — pin 17, reject the 18 bump

`postgres:17-alpine` stays. A `# pinned deliberately` comment goes at both pin sites and a
`version-update:semver-major` ignore rule for `postgres` goes on the `docker-compose`
ecosystem entry in `.github/dependabot.yml`.

Every Postgres major bumps the catalog version, so an existing `kratos-db-data` or
`postgres_data` volume refuses to start under an 18 image. That alone is the #1436 failure
mode with a worse recovery story. PostgreSQL 18's own release notes make the recovery
worse still, listing this first under incompatibilities:

> Change initdb default to enable data checksums. […] pg_upgrade requires matching cluster
> checksum settings, so this new option can be useful to upgrade non-checksummed clusters.

So the escape hatch is not "run `pg_upgrade`", it is "run `pg_upgrade` and know to pass
`--no-data-checksums` to the new cluster's `initdb`". That is two layers past the target
user, on a sidecar whose pitch is zero-to-secure in minutes.

Against that, the benefit is zero. Postgres is only Kratos's store and the admin store.
None of PG18's headline features (async I/O, skip scan, `uuidv7()`, virtual generated
columns, retained optimizer statistics) is reachable from anything VibeWarden does.

Option (b) from the issue — bump for new stacks with a `vibew doctor` check that detects a
17 data directory under an 18 image — is rejected. The check converts a crashloop into a
clear error message but gives the user no way out, and building the `pg_upgrade` path was
explicitly out of scope. Shipping a better error message for a break we chose to cause is
worse than not causing it.

**Revisit when** VibeWarden owns a migration path for the volume, or PG17 approaches EOL
(November 2029), whichever comes first. Minor and patch bumps inside 17.x are unaffected by
the ignore rule and keep flowing, which is what matters for security.

### 3. Redis — pin 7, reject the 8 bump, migrate to Valkey

`redis:7-alpine` stays, with the same comment-and-ignore treatment.

**The license framing in the issue rests on a false premise.** Verified against upstream
`LICENSE.txt` on 2026-09-04:

| Tag | Resolves to | License |
|---|---|---|
| `redis:7-alpine` | 7.4.x | dual RSALv2 / SSPLv1 |
| `redis:8-alpine` | 8.x | tri RSALv2 / SSPLv1 / AGPLv3 |

Redis has been off any approved license since 7.4 (March 2024); only 7.2 and earlier were
BSD-3. So the license list cannot decide 7 versus 8: both are off-list, and 8 is strictly a
*superset* of 7.4's options, adding an OSI-approved one. "Reject 8 because it is not
approved, therefore pin 7" would have been a false reassurance — it buys no license
improvement whatsoever. Under the ruling in section 1, neither pin is a violation.

Pinning `redis:7.2-alpine`, the last BSD-3 release, was considered and rejected: 7.2 is
EOL, so it trades a nonexistent legal problem for a real unpatched-CVE problem.

**The actual reason to reject the bump is that the destination is Valkey, not Redis 8.**

VibeWarden's entire Redis command surface is four commands: `HMGET`, `HMSET` and `EXPIRE`
inside one `EVAL`'d Lua script (`tokenBucketScript` in
`internal/adapters/ratelimit/redis_adapter.go`), plus `PING`
(`internal/plugins/ratelimit/plugin.go:185`). Client construction is `redis.ParseURL` /
`redis.Options`. No Redis 8 change breaks any of it — `HMSET` has been deprecated since 4.0
but is still present in 8.x — and no Redis 8 feature is reachable from it. The bump is
pure verification cost for zero benefit.

It is not free, either. Amd64 compressed image sizes:

| Image | Size |
|---|---|
| `redis:7-alpine` | 16.3 MB |
| `redis:8-alpine` | 39.0 MB |
| `valkey/valkey:9-alpine` | 21.2 MB |

Redis 8's official image bundles the query engine, JSON, TimeSeries and Bloom modules, so
the bump is a 2.4× size increase on a component that, for us, is a token-bucket hash. In a
sidecar whose whole pitch is being light, that is a regression.

Valkey is the Linux Foundation fork of Redis 7.2.4, **BSD-3-Clause**, actively released
(9.1.2 as of 2026-09-03), and RESP-compatible, so the four-command surface above carries
over unchanged and `go-redis` stays. [#1497](https://github.com/vibewarden/vibewarden/issues/1497)
owns the migration.

The `redis` ignore rule is a **holding measure with an explicit owner**, in the style of
ADR-109's conditional waiver. When #1497 lands, the ignore rule, the deliberate-pin
comments and this section's pin all go away together. If #1497 is closed without migrating,
the Redis pin must be re-evaluated here rather than inherited by default.

### 4. Accepted routine bumps

All eight, in the same PR. Two carry traps that the Dependabot PRs do not cover, described
under Error cases.

| Image | From | To | Pin sites |
|---|---|---|---|
| `grafana/loki` | 3.6.10 | 3.7.7 | compose + template |
| `grafana/promtail` | 3.6.10 | **3.6.11** | compose + template |
| `prom/prometheus` | v3.11.2 | v3.14.0 | compose + template |
| `curlimages/curl` | 8.19.0 | 8.22.0 | compose + template |
| `golang` | 1.26-alpine | 1.27-alpine | `examples/demo-app/Dockerfile` |
| `node` | 22-alpine | 26-alpine | `examples/nextjs` (3 stages), `examples/node-express` (2 stages) |
| `python` | 3.13-slim | 3.14-slim | `examples/python-flask` (2 stages) |
| `eclipse-temurin` / `maven` | 21 | 25 | `examples/spring-boot` (both stages) |

`grafana/promtail` goes to 3.6.11, not 3.7.7: **Promtail has no 3.7 line.** Its last
release is 3.6.11 (2026-05-13); Grafana deprecated Promtail in favour of Grafana Alloy.
Promtail 3.6.11 ships to Loki 3.7.7 over the stable `/loki/api/v1/push` v1 API, so the
divergence is safe, but it is now permanent and gets a comment at both pin sites. The
Alloy migration is out of scope here.

Node 26 is Current, not yet LTS (Node 26 enters LTS in October 2026). It is accepted under
the "latest stable" rule; Next 16.3.4 requires Node >= 20.9 and Express 5.2.1 requires
>= 18. If either example fails to build or run on 26, fall back to `node:24-alpine` (the
active LTS) and record why in the CHANGELOG.

---

## Domain model, ports, adapters, application service

**None.** This decision changes infrastructure pins, a Dependabot policy file and one test.
No entity, value object, domain event, port, adapter or application service is added,
removed or altered. The hexagonal boundaries are untouched.

---

## File layout

No new files except this ADR. Modified:

| File | Change |
|---|---|
| `docker-compose.yml` | loki, promtail, prometheus, curl bumps; deliberate-pin comments above the postgres and redis pins; promtail-divergence comment |
| `internal/config/templates/docker-compose.yml.tmpl` | same four bumps, same three comments |
| `internal/config/templates/image_pins_test.go` | new `TestIntegrationTestImagePins_MatchDevCompose` |
| `internal/app/generate/service_test.go` | `TestGenerate_Observability_ComposeDependsOn` — loki dependents assert `service_started` (Error cases 6) |
| `internal/adapters/postgres/audit_integration_test.go`, `internal/adapters/kratos/adapter_integration_test.go` | drifted `postgres:16-alpine` pins → 17, caught by the new test |
| `.github/dependabot.yml` | `ignore` block on the `docker-compose` ecosystem entry |
| `examples/demo-app/Dockerfile` | builder stage → `golang:1.27-alpine` |
| `examples/demo-app/go.mod` | `go 1.26` → `go 1.27` |
| `examples/nextjs/Dockerfile` | all three stages → `node:26-alpine` |
| `examples/node-express/Dockerfile` | both stages → `node:26-alpine`; `npm ci` → `npm install` (see Error cases 7) |
| `examples/python-flask/Dockerfile` | both stages → `python:3.14-slim` |
| `examples/spring-boot/Dockerfile` | build → `maven:3.9-eclipse-temurin-25-alpine`, runtime → `eclipse-temurin:25-jre-alpine` |
| `docs/postgres.md` | short "why 17 and not 18" note pointing here |
| `decisions/README.md` | index row |
| `CHANGELOG.md` | Unreleased entry |

---

## Closing the third pin site

Postgres and Redis are pinned in a **third** location that is invisible to both Dependabot
and the #1298 drift guard:

- `internal/adapters/postgres/migration_integration_test.go:20` — `"postgres:17-alpine"`
- `internal/adapters/ratelimit/redis_adapter_integration_test.go:21` — `tcredis.Run(ctx, "redis:7-alpine")`

Now that both majors are deliberately pinned, that gap is exactly how a deliberate pin
rots: someone bumps the testcontainers image and CI starts certifying a version no user
runs, or the compose pin moves and the integration suite silently keeps testing the old
one. The pin is only as good as the mechanism that holds it.

`TestIntegrationTestImagePins_MatchDevCompose` in
`internal/config/templates/image_pins_test.go` closes it: walk `../../../internal` for
`*_integration_test.go`, scan each for Go string literals of the form `"<name>:<tag>"`
where `<name>` is a key of `devComposePins`, and assert the tag equals the dev compose tag.

Keying on names the dev compose already tracks is what makes this self-maintaining and
free of false positives — an arbitrary `"foo:bar"` string in some unrelated test cannot
trip it, and a new integration test pinning a tracked image is covered the day it lands
with no list to update. It fails if it finds zero literals, matching the vacuous-truth
guard `parseImagePins` already uses.

---

## Error cases

1. **`vibew doctor` fails on our own flagship example.** `RuleToolchainMatch`
   (`internal/app/dockerfile/rules.go`) compares the builder stage's tag major.minor
   against the `go` directive and returns `SeverityFail` on mismatch.
   `examples/demo-app/go.mod` declares `go 1.26`. Bumping the Dockerfile to
   `golang:1.27-alpine` alone makes the demo app fail our own doctor check. Both move
   together. The root repo is already on `golang:1.27.1-alpine` / `go 1.27.1`, so this
   restores consistency rather than creating it.

2. **Spring Boot build and runtime JDKs drift apart.** Dependabot's #1494 touches only line
   8 (`eclipse-temurin:21-jre-alpine`); line 1 is the build stage
   (`maven:3.9-eclipse-temurin-21-alpine`). Bumping only the runtime leaves a JDK-21 build
   feeding a JRE-25 runtime. That happens to work, and that is the problem: it hides the
   drift until someone sets `<java.version>` and the build fails. Both stages move.
   `maven:3.9-eclipse-temurin-25-alpine` is verified to exist. The pom sets no
   `<java.version>` and inherits 17 from the Spring Boot 4.1.1 parent; leave it inherited.

3. **Multi-stage Dockerfiles bumped in one stage only.** `examples/nextjs` has three
   `node:22-alpine` stages, `node-express` has two, `python-flask` has two. Every stage
   moves, not just the one in the Dependabot diff.

4. **Promtail bumped in lockstep with Loki and failing.** `grafana/promtail:3.7.7` does not
   exist. The drift guard will not catch this: it keys on image name, and loki and promtail
   are different names. See section 4.

5. **The observability smoke test cannot run from the repo root.** The root compose mounts
   `./observability/loki/loki-config.yml`, and no `observability/` directory exists at the
   repo root, so `make observability-up` cannot bring that profile up. Smoke the loki and
   prometheus bumps through the generated stack instead — regenerate first
   (`cd examples/demo-app && vibew generate`), because the gitignored `.vibewarden/` tree
   there is stale (loki 3.4.3, OpenBao 2.2.0) and smoking it unregenerated validates the
   old pins.

6. **Loki 3.7 rejecting the generated config.** `loki-config.yml.tmpl` already uses `tsdb`,
   schema `v13` and `delete_request_store`, which is the current 3.x shape, so no config
   change is expected. Confirm `/ready` returns 200 (the compose healthcheck asserts it)
   and that promtail 3.6.11 logs actually land in Loki 3.7.7 by querying for a label — a
   healthy container proves the cross-version push path works only if something was pushed.

   **Implementation outcome.** The config was accepted unchanged, but the *healthcheck*
   was not: `grafana/loki:3.7.x` is distroless. The image contains `/usr/bin/loki` and
   nothing else — no `/bin/sh`, no `wget`, no `curl` — so both the `CMD-SHELL` probe in
   the template and the `CMD wget` probe in the dev compose fail with
   `exec: "/bin/sh": no such file or directory`, the container is marked unhealthy, and
   every `condition: service_healthy` dependent refuses to start. Docker has no
   out-of-container HTTP probe, so the healthcheck is removed and loki's three dependents
   (promtail, otel-collector, grafana) move to `condition: service_started`. This follows
   the pattern already in the template for `otel-collector`, which is distroless for the
   same reason; all three retry their own pushes and queries, so nothing is lost but
   ordering. Verified after the change: `/ready` returns 200 once warm, and promtail
   3.6.11 log lines are queryable out of Loki 3.7.7 under the `container` label.

7. **`examples/node-express` has never built.** Found while satisfying the build-and-run
   acceptance criterion, and unrelated to the Node bump — it reproduces identically on
   `node:22-alpine`. The Dockerfile does `COPY package.json package-lock.json* ./` followed
   by `RUN npm ci --omit=dev`, but the example deliberately ships no lockfile (the
   `/examples/node-express` entry in `.github/dependabot.yml` says so), the glob copies
   nothing, and `npm ci` hard-fails with `EUSAGE`. Fixed in the same PR by switching to
   `npm install --omit=dev --no-audit --no-fund`, which keeps the example's no-lockfile
   posture, with a comment saying why `npm ci` is not used.

---

## Test strategy

- **Unit** — the new `TestIntegrationTestImagePins_MatchDevCompose` plus the three existing
  drift tests in `internal/config/templates/image_pins_test.go`. No change is needed to the
  existing three for the accepted bumps: loki, promtail, prometheus and curl are each
  pinned in both the dev compose and the template under one image name, so
  `TestTemplateImagePins_MatchDevCompose` already forces them to move together.
- **Unchanged on purpose** — `internal/app/generate/service_test.go` and
  `service_integration_test.go` assert `redis:7-alpine`, which is the pin being kept. They
  move with #1497, not here.
- **Integration** — unchanged. The postgres 17 and redis 7 testcontainers pins stay; the
  new test now holds them to the compose pins.
- **Manual, not skippable** — `docker compose config` on the root compose and on a
  regenerated demo-app stack; the observability smoke of item 5; and building *and running*
  each of the five bumped examples. Node 22 → 26 and Temurin 21 → 25 are majors on user-
  facing examples; a green `docker build` is not evidence the app serves a request.
- `/usr/bin/make check`.

---

## New dependencies

**None.** No Go module, no new image. Two images are held at their current major.

---

## Consequences

- Two images are now deliberately behind. `.github/dependabot.yml` stops surfacing them
  weekly, which means nothing re-raises the question on a schedule — this ADR's revisit
  triggers (a VibeWarden-owned volume migration path or PG17 EOL for Postgres; #1497 for
  Redis) are the only prompts. Minor and patch updates still flow for both, so security
  patching is unaffected.
- Section 1 is the reusable half of this ADR. Future referenced-service images
  (Kratos, OpenBao, Grafana, Loki, Prometheus) are evaluated under it rather than
  mechanically rejected by an allow-list written for linked code. Anything actually linked
  into the binary still goes through the strict list and ADR-109's waiver process.
- Recording that Redis has been off any approved license since 7.4, and that Redis 8 is
  *more* permissive than the version we ship, means nobody re-derives the wrong conclusion
  from a headline. The reason we are leaving Redis is that a permissive drop-in exists,
  not that Redis 8 is uniquely tainted.
- The generated compose file now carries pin-rationale comments into every user's stack.
  That is intentional under the artifact policy: the compose file is a real, owned
  artifact, and a user who wonders why their sidecar runs Postgres 17 gets the answer in
  the file rather than in a doc they will not find.
- The Promtail pin is now permanently behind Loki and cannot catch up. That is a
  standing invitation to migrate the log shipper to Grafana Alloy; the comment at the pin
  site is the marker, and no issue owns it yet.
- `examples/demo-app` moves to Go 1.27 for the toolchain-match rule, so the demo app's
  minimum Go version rises with it. It is an example, not a library, so nothing depends on
  that floor.
