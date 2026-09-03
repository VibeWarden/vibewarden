# @vibewarden/cli

`vibew` — the CLI for [VibeWarden](https://vibewarden.dev), an open-source security
sidecar for vibe-coded apps. TLS, auth, rate limiting, WAF, security headers and
AI-readable structured logs, with zero changes to your app code.

This package is a thin wrapper. On install it downloads the official `vibew`
binary for your platform from the matching GitHub Release, verifies its SHA-256
against digests embedded in this package at publish time, and installs it. The
binary is byte-identical to the release artifact.

## Install

```bash
npm install -g @vibewarden/cli
vibew --version
```

Pin a version the way you pin anything else:

```bash
npm install -g @vibewarden/cli@0.19.0
```

The npm package version always equals the VibeWarden release version.

## Quick start

```bash
mkdir myapp && cd myapp
vibew init
vibew dev
```

Full docs: <https://vibewarden.dev>

## Supported platforms

| OS | Architectures |
|----|---------------|
| macOS | arm64 (Apple Silicon), x64 |
| Linux | x64, arm64 (glibc and musl — the binaries are static) |

Native Windows is not supported: VibeWarden publishes no Windows binary. Use WSL2,
where the Linux install works unchanged.

## Upgrading

Upgrade the way you installed:

```bash
npm install -g @vibewarden/cli@latest
```

**Do not use `vibew upgrade` on an npm-managed install.** It replaces the binary
inside this package's `vendor/` directory, which desynchronises it from the npm
package version and is silently reverted by the next `npm install -g`. Use npm.

## Environment variables

| Variable | Effect |
|----------|--------|
| `VIBEWARDEN_INSTALL_VERSION` | Install a different release than the package version. Digests are then fetched from the release's `checksums.txt` (transport integrity only) instead of the embedded ones. Verification still happens. |
| `VIBEWARDEN_BINARY_MIRROR` | Base URL for release assets, instead of `https://github.com/vibewarden/vibewarden/releases/download`. Useful behind a corporate proxy or an artifact mirror. |
| `VIBEWARDEN_SKIP_DOWNLOAD=1` | Skip the download at postinstall. `vibew` then prints the command to complete the install on first run. |

## If npm ran with `--ignore-scripts`

Lifecycle scripts are disabled in many CI setups and lockfile-audit workflows, so
the postinstall download never runs. Running `vibew` then prints the exact command
to finish the install. It is:

```bash
node "$(npm root -g)/@vibewarden/cli/install.js"
```

Alternatively, install without npm:

```bash
curl -fsSL https://vibewarden.dev/install.sh | sh
```

## Security

- **Checksums are embedded, not fetched.** They ship inside the published npm
  tarball, so the digest is pinned by your lockfile's integrity hash rather than
  served by the same host as the artifact.
- **Nothing unverified is ever executable.** The archive is verified in memory;
  the binary is written to a temporary file and atomically renamed into place only
  after the digest matches.
- Releases are published with [npm provenance](https://docs.npmjs.com/generating-provenance-statements),
  attesting the tarball to this repository, workflow and commit.

Report vulnerabilities per the [security policy](https://github.com/vibewarden/vibewarden/blob/main/SECURITY.md).

## License

Apache-2.0. Source: <https://github.com/vibewarden/vibewarden>
