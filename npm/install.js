'use strict';

/**
 * postinstall entry point for @vibewarden/cli.
 *
 * Downloads the release archive matching this package's own version, verifies
 * it against the SHA-256 digests embedded in checksums.json at publish time,
 * and extracts the `vibew` binary into vendor/. Orchestration only: every step
 * lives in lib/ and is unit-tested there.
 *
 * Environment overrides:
 *   VIBEWARDEN_INSTALL_VERSION  install a different release than the package version
 *   VIBEWARDEN_BINARY_MIRROR    base URL to download release assets from
 *   VIBEWARDEN_SKIP_DOWNLOAD=1  skip the download entirely (offline/CI/vendored setups)
 */

const fs = require('node:fs');
const path = require('node:path');

const paths = require('./lib/paths');
const { resolvePlatform } = require('./lib/platform');
const { download, DownloadError } = require('./lib/download');
const { lookupDigest, parseChecksumsTxt, verifyDigest } = require('./lib/verify');
const { extractMember } = require('./lib/targz');

const DEFAULT_MIRROR = 'https://github.com/vibewarden/vibewarden/releases/download';

/** Version committed in package.json; replaced at publish time. */
const PLACEHOLDER_VERSION = '0.0.0-dev';

/**
 * install downloads, verifies and extracts the vibew binary.
 *
 * @param {object} [options]
 * @param {NodeJS.ProcessEnv} [options.env] environment, default process.env
 * @param {{version: string}} [options.packageJson] package metadata, default this package's
 * @param {{version: string, archives: Record<string, string>}} [options.checksums] embedded digests
 * @param {string} [options.vendorDir] destination directory, default <package>/vendor
 * @param {string} [options.platform] override for process.platform (tests)
 * @param {string} [options.arch] override for process.arch (tests)
 * @param {(msg: string) => void} [options.log] logger, default console.error
 * @param {object} [options.downloadOptions] passed through to download()
 * @returns {Promise<{skipped: boolean, reason?: string, version?: string, binaryPath?: string}>}
 * @throws {Error} when the platform is unsupported, the download fails, or the digest mismatches
 */
async function install(options = {}) {
  const env = options.env ?? process.env;
  const pkg = options.packageJson ?? require('./package.json');
  const checksums = options.checksums ?? require('./checksums.json');
  const vendorDir = options.vendorDir ?? paths.VENDOR_DIR;
  const log = options.log ?? ((msg) => console.error(msg));
  const downloadOptions = options.downloadOptions ?? {};

  const override = (env.VIBEWARDEN_INSTALL_VERSION ?? '').trim().replace(/^v/, '');
  const version = override !== '' ? override : pkg.version;

  if (env.VIBEWARDEN_SKIP_DOWNLOAD === '1') {
    log('vibew: VIBEWARDEN_SKIP_DOWNLOAD=1, skipping binary download.');
    log(`vibew: run "node ${paths.INSTALL_SCRIPT}" later to complete the install.`);
    return { skipped: true, reason: 'skip-requested' };
  }

  if (version === PLACEHOLDER_VERSION) {
    log(`vibew: package version is the placeholder ${PLACEHOLDER_VERSION} (development checkout), skipping download.`);
    log('vibew: set VIBEWARDEN_INSTALL_VERSION=<version> to install a real release.');
    return { skipped: true, reason: 'placeholder-version' };
  }

  const target = resolvePlatform(version, options.platform, options.arch);
  const mirror = (env.VIBEWARDEN_BINARY_MIRROR ?? DEFAULT_MIRROR).replace(/\/+$/, '');
  const baseURL = `${mirror}/v${version}`;
  const archiveURL = `${baseURL}/${target.archiveName}`;

  log(`vibew: downloading ${target.archiveName} (${target.goos}/${target.goarch})`);
  const archive = await downloadArchive(archiveURL, { ...downloadOptions, log });

  const expected = await resolveDigest({
    checksums,
    version,
    archiveName: target.archiveName,
    baseURL,
    downloadOptions: { ...downloadOptions, log },
    log,
  });
  verifyDigest(archive, expected, target.archiveName);

  const binary = extractMember(archive, paths.BINARY_NAME);
  const binaryPath = writeBinary(vendorDir, binary);

  log(`vibew: installed ${version} (${target.goos}/${target.goarch}) to ${binaryPath}`);
  return { skipped: false, version, binaryPath };
}

/**
 * downloadArchive fetches the release archive and rewrites failures into
 * actionable, cause-specific messages.
 */
async function downloadArchive(url, downloadOptions) {
  try {
    return await download(url, downloadOptions);
  } catch (err) {
    if (err instanceof DownloadError && err.code === 'ENOTFOUND404') {
      throw new Error(
        [
          `release asset not found: ${url}`,
          '',
          'The release or the platform archive does not exist for this version.',
          'Check https://github.com/vibewarden/vibewarden/releases for available versions.',
        ].join('\n'),
        { cause: err },
      );
    }
    if (err instanceof DownloadError) {
      throw new Error(`failed to download ${url}: ${err.message}`, { cause: err });
    }
    throw err;
  }
}

/**
 * resolveDigest returns the expected SHA-256 for the archive.
 *
 * Normally it comes from the embedded checksums.json. When the version was
 * overridden (or the embedded digests were generated for a different version),
 * checksums.txt is fetched from the release instead, with a warning: that only
 * proves transport integrity. Verification is never skipped.
 */
async function resolveDigest({ checksums, version, archiveName, baseURL, downloadOptions, log }) {
  if (checksums && checksums.version === version) {
    return lookupDigest(checksums.archives, archiveName);
  }

  const url = `${baseURL}/checksums.txt`;
  log(
    `vibew: warning — no embedded digest for ${version}; fetching ${url}. ` +
      'This proves transport integrity only, not npm-tarball-pinned integrity.',
  );
  const body = await downloadArchive(url, downloadOptions);
  return lookupDigest(parseChecksumsTxt(body.toString('utf8')), archiveName);
}

/**
 * writeBinary writes the binary to a temporary file, marks it executable, and
 * renames it into place. The rename is atomic on the same filesystem, so a
 * partially written or unverified binary is never reachable at the final path.
 *
 * @param {string} vendorDir destination directory
 * @param {Buffer} binary binary contents
 * @returns {string} absolute path of the installed binary
 */
function writeBinary(vendorDir, binary) {
  const finalPath = path.join(vendorDir, paths.BINARY_NAME);
  const tmpPath = `${finalPath}.tmp-${process.pid}`;
  try {
    fs.mkdirSync(vendorDir, { recursive: true });
    fs.writeFileSync(tmpPath, binary, { mode: 0o755 });
    fs.chmodSync(tmpPath, 0o755);
    fs.renameSync(tmpPath, finalPath);
  } catch (err) {
    try {
      fs.rmSync(tmpPath, { force: true });
    } catch {
      // best effort cleanup; the original error is what matters
    }
    if (err && err.code === 'EACCES') {
      throw new Error(
        [
          `permission denied writing ${finalPath}`,
          '',
          'npm drops privileges for lifecycle scripts, so a "sudo npm i -g" install',
          'often cannot write into the global prefix. Either:',
          '  1. switch to a user-owned npm prefix, then install without sudo:',
          '       npm config set prefix ~/.npm-global',
          '       export PATH="$HOME/.npm-global/bin:$PATH"',
          '       npm i -g @vibewarden/cli',
          '  2. or take ownership of the existing global root, then install without sudo:',
          '       sudo chown -R "$(whoami)" "$(npm root -g)"',
          '       npm i -g @vibewarden/cli',
        ].join('\n'),
        { cause: err },
      );
    }
    throw err;
  }
  return finalPath;
}

module.exports = { install, DEFAULT_MIRROR, PLACEHOLDER_VERSION };

if (require.main === module) {
  install().catch((err) => {
    console.error('');
    console.error('vibew: install failed.');
    console.error(err && err.message ? err.message : err);
    console.error('');
    console.error('Fallback: install without npm —');
    console.error('  curl -fsSL https://vibewarden.dev/install.sh | sh');
    process.exit(1);
  });
}
