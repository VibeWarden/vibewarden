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
VibeWarden v0.12.3 (git: abc1234, built: 2026-03-28)
```

To show only the version string (useful in scripts):

```bash
./vibew version --short
# v0.12.3
```

---

## How to upgrade

### Automatic upgrade via the `vibew` wrapper

The `vibew` script reads the pinned version from `.vibewarden-version` in your project
directory and downloads the correct binary automatically on every invocation. To upgrade,
update `.vibewarden-version` to the desired version:

```bash
echo "v0.13.0" > .vibewarden-version
./vibew version   # downloads v0.13.0 and prints it
```

Commit `.vibewarden-version` so everyone on your team, as well as your CI environment,
uses the same version automatically.

### Upgrading to the latest release

```bash
./vibew self-update
```

This fetches the latest stable release from
[github.com/vibewarden/vibewarden/releases](https://github.com/vibewarden/vibewarden/releases),
writes the new version tag to `.vibewarden-version`, and re-downloads the binary on the
next `vibew` invocation.

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
    ./vibew version --short   # note your current version
    ```

    Release notes: [github.com/vibewarden/vibewarden/releases](https://github.com/vibewarden/vibewarden/releases)

2. **Back up your config** (if you keep `vibewarden.yaml` outside version control):

    ```bash
    cp vibewarden.yaml vibewarden.yaml.bak
    ```

3. **Apply any manual config changes** described in the release notes for breaking
   releases.

4. **Update the pinned version:**

    ```bash
    echo "vX.Y.Z" > .vibewarden-version
    ```

5. **Validate the updated config:**

    ```bash
    ./vibew validate
    ```

    Fix any reported errors before proceeding.

6. **Restart the stack:**

    ```bash
    ./vibew dev       # development
    # or
    docker compose up -d vibewarden   # production
    ```

7. **Verify:**

    ```bash
    ./vibew version
    ./vibew doctor
    ```

    `vibew doctor` runs a full health check and surfaces any post-upgrade issues.

---

## Rolling back

If an upgrade causes an unexpected issue, roll back by reverting `.vibewarden-version`:

```bash
echo "vX.Y.Z-previous" > .vibewarden-version
./vibew dev
```

Because the wrapper downloads a fresh binary matching `.vibewarden-version`, the rollback
takes effect immediately on the next `vibew` invocation. No uninstall step is needed.

!!! tip "Keep your old config backup"
    If you applied manual config changes for the upgrade, restore `vibewarden.yaml.bak`
    before rolling back the binary so the old version can parse the config correctly.

---

## Getting help

- [Troubleshooting guide](troubleshooting.md)
- [GitHub Discussions](https://github.com/vibewarden/vibewarden/discussions)
- [GitHub Issues](https://github.com/vibewarden/vibewarden/issues)
