# Upgrading VibeWarden

This guide explains how VibeWarden is versioned, how to upgrade, what the breaking
change policy is, and how to handle config migrations.

---

## How versioning works

VibeWarden follows [Semantic Versioning](https://semver.org/) (`MAJOR.MINOR.PATCH`):

| Component | Incremented when… |
|-----------|-------------------|
| `MAJOR`   | A breaking change is introduced (config key removed, API incompatibility, behavior change requiring action) |
| `MINOR`   | A new feature is added in a backward-compatible way |
| `PATCH`   | A bug fix or security patch is released, no behavior change |

**Pre-1.0 note:** While VibeWarden is `0.x`, minor version bumps (`0.1 → 0.2`) may
contain breaking changes. Each such release documents all breaking changes in the
[GitHub release notes](https://github.com/vibewarden/vibewarden/releases). Once 1.0.0
is tagged, the policy above is strictly enforced.

---

## Checking your current version

```bash
./vibew version
```

Example output:

```
vibew v0.19.0
```

`./vibew --version` produces identical output.

---

## How to upgrade

Upgrade the same way you installed. The three install paths are independent:
mixing them leaves you with two binaries and no clear winner on `PATH`.

| Installed with | Upgrade with |
|----------------|--------------|
| `npm install -g @vibewarden/cli` | `npm install -g @vibewarden/cli@latest` |
| `install.sh` or a direct download | `vibew upgrade` |
| Docker image | change the image tag, `docker compose pull` |

### Upgrading an npm-managed install

```bash
npm install -g @vibewarden/cli@latest   # or @X.Y.Z to pin
```

!!! warning "Do not run `vibew upgrade` on an npm install"
    `vibew upgrade` resolves the running binary to the copy inside the npm
    package's `vendor/` directory and replaces it there. The upgrade appears to
    succeed, but the binary no longer matches the installed package version, and
    the next `npm install -g` silently reverts it. Always upgrade npm installs
    with npm.

### Upgrading with `vibew upgrade`

`vibew upgrade` downloads and installs a new VibeWarden release, replacing the
running binary in-place.

```bash
vibew upgrade            # install the latest release
vibew upgrade v0.4.0     # install a specific version
vibew upgrade --dry-run  # preview without writing files
```

The command:

1. Resolves the target version (latest from the GitHub API, or the tag you
   supplied).
2. Downloads the binary archive for the current OS and architecture.
3. Verifies the SHA-256 checksum.
4. Replaces the running binary in-place (resolved via `os.Executable` +
   symlink evaluation). Falls back to `~/.vibewarden/bin` if resolution fails.
5. Touches `vibew`, `vibew.ps1`, `vibew.cmd` in the current directory when present.

If the target directory is not writable (e.g. `/usr/local/bin` without sudo),
the command automatically retries with `sudo`.

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dry-run` | `false` | Print what would happen without writing any files |
| `--install-dir` | path of running binary | Directory to install the binary into |

### Upgrading to the latest release (alternative)

```bash
./vibew self-update
```

This fetches the latest stable release from
[github.com/vibewarden/vibewarden/releases](https://github.com/vibewarden/vibewarden/releases)
and re-downloads the binary on the next `vibew` invocation.

!!! warning "Review the release notes first"
    Always read the release notes before upgrading across a MAJOR version boundary.
    Run `./vibew version` and compare it against the target version before proceeding.

### Upgrading the Docker image

If you run VibeWarden via the Docker image directly, update the image tag in your
`docker-compose.yml` (or generated file):

```yaml
services:
  vibewarden:
    image: ghcr.io/vibewarden/vibewarden:v0.13.0   # change this line
```

Then pull and restart:

```bash
docker compose pull vibewarden
docker compose up -d vibewarden
```

---

## Breaking change policy

### What counts as a breaking change

VibeWarden considers the following to be breaking changes that require a MAJOR version
bump (after 1.0.0):

- Removing or renaming a config key in `vibewarden.yaml` without a deprecation period
- Changing the default value of an existing config key in a way that alters observed behavior
- Removing a CLI flag or sub-command
- Removing or renaming a field in the structured log schema (`event_type`, `payload` shape)
- Removing a Prometheus metric name or label that was present in a prior stable release

### What is not a breaking change

- Adding a new config key with a sensible default
- Adding a new CLI flag or sub-command
- Adding a new Prometheus metric
- Adding a new field to a structured log event's `payload`
- Improving error messages or log output
- Internal refactoring with no observable behavior change

### Deprecation process

When a config key, CLI flag, or schema field is scheduled for removal, it goes through
a **two-release deprecation cycle**:

1. **Deprecation release** — the old name still works; a deprecation warning is printed
   at startup. The new name is documented in the release notes and in this guide.
2. **Removal release** — the old name is removed in the next MAJOR release. The
   release notes contain migration instructions.

---

## Config migration

### `OPENBAO_DEV_ROOT_TOKEN` → `OPENBAO_ROOT_TOKEN` (v0.19, removed in v0.20)

The credential key written to `.credentials` was renamed from `OPENBAO_DEV_ROOT_TOKEN`
to `OPENBAO_ROOT_TOKEN`. The misleading `DEV_` prefix implied the token was only for
dev use — it was always the credential used in prod contexts too.

The old name is still recognised by `vibew bundle` until v0.20. `vibew bundle` prints
a deprecation warning when `OPENBAO_DEV_ROOT_TOKEN=` is detected in `.credentials` and
continues to work until v0.20.

**Migration steps:**

1. Open `.credentials` in your project directory.
2. Rename the `OPENBAO_DEV_ROOT_TOKEN=` line to `OPENBAO_ROOT_TOKEN=`.
3. Re-run `vibew bundle` to regenerate the bundle with the new key name.

**Warning — production deployments:** In v0.19+, `seed-secrets.sh` writes
`OPENBAO_UNSEAL_KEY` into `.credentials` on first boot. This key is required to
unseal the vault after every host reboot. Back up `.credentials` before editing or
re-generating it. Do not overwrite the host `.credentials` by copying a freshly
generated bundle `.credentials` — the new file will not contain the `OPENBAO_UNSEAL_KEY`
written at boot time.

If `OPENBAO_UNSEAL_KEY` is lost, see the [OpenBao: unseal key missing](troubleshooting.md#openbao-unseal-key-missing-after-host-reboot-or-bundle-re-run)
troubleshooting entry for the recovery procedure.

---

### Generated `kratos.yml` auth UI URLs: `/auth/*` → `/_vibewarden/*`

Up to and including v0.21.0, `vibew generate` / `vibew bundle` wrote every
`selfservice.flows.*.ui_url` in `kratos/kratos.yml` as `/auth/login`,
`/auth/registration`, `/auth/recovery`, `/auth/settings`, `/auth/verification`
and `/auth/error`. The built-in auth UI has never served those paths — it serves
`/_vibewarden/login`, `/_vibewarden/registration`, `/_vibewarden/recovery`,
`/_vibewarden/verification` and `/_vibewarden/settings`.

**Symptom:** anything Kratos redirects on its own (an expired login flow, the
logout return URL, the error page) lands on a path VibeWarden does not route, so
the request falls through to your app and you get your app's 404 instead of a
login page. Clicking a link to `/_vibewarden/login` yourself still works, which
is why this can hide for a while.

**Migration:** regenerate. No `vibewarden.yaml` change is needed.

```bash
vibew generate     # or: vibew bundle
docker compose up -d --force-recreate kratos
```

If you pinned a hand-written Kratos config with `overrides.kratos_config`,
VibeWarden copies that file verbatim and cannot fix it for you — update the
`ui_url` values in your own file to the `/_vibewarden/*` paths above.

**If you serve your own auth pages,** set `auth.ui.mode: custom` and the
`auth.ui.*_url` keys. The generated `kratos.yml` then points the flows at your
URLs instead. Absolute URLs are used as-is; a path such as `/login` is resolved
against your public base URL. Flows with no key of their own
(`verification`, `error`) fall back to `auth.ui.login_url`.

```yaml
auth:
  ui:
    mode: custom
    login_url: https://myapp.example.com/login
    registration_url: https://myapp.example.com/register
    settings_url: https://myapp.example.com/settings
    recovery_url: https://myapp.example.com/recovery
```

---

### `auth.ui` branding now applies to the built-in auth pages

Up to and including v0.21.0, the `auth.ui` block was parsed and validated but
never reached the auth plugin, so none of it applied. Fixing that
([#1511](https://github.com/vibewarden/vibewarden/issues/1511)) changes two
things on any existing project with `auth.mode: kratos`.

**1. `auth.ui.mode: custom` stops serving the built-in pages.** Until now,
custom mode still got the sidecar's own
`/_vibewarden/{login,registration,recovery,verification,settings}` pages and
routes. The built-in UI is no longer started in custom mode at all, and
unauthenticated requests redirect to your `auth.ui.login_url`.

**Before you regenerate or redeploy, confirm that `login_url` actually
serves a login page.** It may have been dead the whole time without you
noticing, because the built-in pages were covering for it. An absolute URL is
used as-is; a bare path such as `/login` is resolved against your public base
URL and must be reachable without a session (add it to `auth.public_paths` if
your app gates it).

```bash
curl -I https://<your-domain>/login   # expect 200, not 404 or a redirect loop
```

If it is not ready, switch back to `auth.ui.mode: built-in` (the default),
regenerate, and move to custom mode later.

**2. Your colours apply, and the default background changes.** A project that
set `primary_color` / `background_color` gets those colours for the first time.
A project that set neither moves from the old adapter fallback `#F3F4F6` to the
documented `auth.ui.background_color` default `#1a1a2e`, so the built-in login
page goes from light grey to dark. To keep the previous look:

```yaml
auth:
  ui:
    background_color: "#F3F4F6"
```

**Also:** `auth.ui.logo_url` is now validated as a browser asset URL, and
validation runs on the startup path. A malformed value that was previously
inert (`logo.png`, `javascript:...`, `//host/logo.png`) now fails config
validation and the sidecar refuses to start. Use an absolute `http(s)` URL or a
root-relative path (`/static/logo.png`). Run `vibew validate` before upgrading
to catch this without downtime.

---

### `vibew add auth` / `vibew wrap --auth`: Kratos URLs now point at the `kratos` service

Up to and including v0.21.0, `vibew add auth` and `vibew wrap --auth` wrote
`kratos.public_url` / `kratos.admin_url` as `http://localhost:4433` /
`http://localhost:4434` and also wrote an absolute `auth.login_url`. Both are
wrong from inside the sidecar's own container, where `localhost` is the sidecar
itself, not the `kratos` service.

**Symptom:** the admin API returns `503 service_unavailable`
(`dial tcp [::1]:4434: connect: connection refused`), `/self-service/login/browser`
returns `502`, and unauthenticated requests redirect off the sidecar to
`http://localhost:4433/...`, a port the generated stack never publishes.

**v0.22.0 fixes the generators**: both commands now write
`http://kratos:4433` / `http://kratos:4434` and omit `auth.login_url` entirely.

**Existing projects are not migrated automatically.** A `vibewarden.yaml`
already generated by either command keeps the broken values. Fix it by hand:

```yaml
kratos:
  public_url: http://kratos:4433
  admin_url: http://kratos:4434
auth:
  # delete login_url entirely — the middleware's sidecar-relative
  # /self-service/login/browser default takes over once it's gone
```

or delete the `kratos:` block and the `auth.login_url` key and re-run
`vibew add auth` / `vibew wrap --auth` to regenerate them.

If you run the binary outside Docker (not via the generated compose stack),
the `127.0.0.1` defaults still apply — set the URLs to whatever host and port
resolve for your setup instead of `kratos`.

---

### `security_headers` now applies to VibeWarden's own routes, not just the app

Up to and including v0.21.0, a configured `security_headers` block only reached
the app-proxied route. The built-in login page (`/_vibewarden/login`), the
admin API (`/_vibewarden/admin/*`), and the Kratos proxy (`/self-service/*`,
`/_vibewarden/config`) answered with no security headers at all — no
`X-Frame-Options`, no CSP, no `nosniff`.

**v0.22.0 applies the same header set to every response the sidecar serves.**
No config change is required — this takes effect on upgrade, without a
`vibew generate` / `vibew bundle` regeneration step.

**Check before upgrading if you rely on the old gap:**

- If anything embeds the built-in login page in an `<iframe>` from another
  origin, `X-Frame-Options` / `frame-ancestors` now blocks it.
- `response_headers` overrides are still applied to the app-proxied route
  only. They never applied to VibeWarden's own routes and still do not — if
  you were using an operator override to loosen a header on the admin API or
  login page specifically, that override was never effective and remains so.

---

### `metrics:` → `telemetry:` (deprecated)

The `metrics:` block was replaced by `telemetry:` to reflect VibeWarden's move to
OpenTelemetry as the unified telemetry foundation.

**Automatic migration:** VibeWarden auto-migrates `metrics:` settings to `telemetry:`
at startup and logs a deprecation warning:

```
[WARN] config: "metrics" block is deprecated and will be removed in the next major version. Migrate to "telemetry:". See https://vibewarden.dev/docs/upgrading#metrics-telemetry
```

**Manual migration:** Update `vibewarden.yaml` before the next major release:

=== "Before (deprecated)"

    ```yaml
    metrics:
      enabled: true
      path: /_vibewarden/metrics
      labels:
        app: my-api
    ```

=== "After (current)"

    ```yaml
    telemetry:
      enabled: true
      prometheus:
        enabled: true
        path: /_vibewarden/metrics
      labels:
        app: my-api
    ```

Run `./vibew validate` after editing — it reports any remaining deprecated keys.

---

## Step-by-step upgrade procedure

The following procedure applies to any version bump.

1. **Read the release notes** for every version between your current version and the
   target. Look for entries marked **Breaking** or **Deprecated**.

    ```bash
    ./vibew version   # note your current version
    ```

    Release notes: [github.com/vibewarden/vibewarden/releases](https://github.com/vibewarden/vibewarden/releases)

2. **Back up your config** (if you keep `vibewarden.yaml` outside version control):

    ```bash
    cp vibewarden.yaml vibewarden.yaml.bak
    ```

3. **Apply any manual config changes** described in the release notes for breaking
   releases.

4. **Validate the updated config:**

    ```bash
    ./vibew validate
    ```

    Fix any reported errors before proceeding.

5. **Restart the stack:**

    ```bash
    ./vibew dev       # development
    # or
    docker compose up -d vibewarden   # production
    ```

6. **Verify:**

    ```bash
    ./vibew version
    ./vibew doctor
    ```

    `vibew doctor` runs a full health check and surfaces any post-upgrade issues.

---

## Rolling back

If an upgrade causes an unexpected issue, roll back by installing the previous version explicitly:

```bash
vibew upgrade vX.Y.Z-previous
```

This installs the specified version immediately. No uninstall step is needed.

!!! tip "Keep your old config backup"
    If you applied manual config changes for the upgrade, restore `vibewarden.yaml.bak`
    before rolling back the binary so the old version can parse the config correctly.

---

## Getting help

- [Troubleshooting guide](troubleshooting.md)
- [GitHub Discussions](https://github.com/vibewarden/vibewarden/discussions)
- [GitHub Issues](https://github.com/vibewarden/vibewarden/issues)
