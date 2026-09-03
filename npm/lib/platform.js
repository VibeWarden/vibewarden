'use strict';

/**
 * Maps the running Node platform/arch onto the GOOS/GOARCH pair published by
 * the release pipeline, and derives the release archive name.
 *
 * The archive name format is the goreleaser `name_template` in .goreleaser.yml:
 *   vibewarden_<version>_<goos>_<goarch>.tar.gz   (version without the leading "v")
 *
 * This is the fourth consumer of that contract, after scripts/install.sh and
 * internal/app/upgrade/service.go. Renaming archives breaks all of them;
 * scripts/release/prepare-npm-package.sh is the tripwire.
 */

const INSTALL_SH = 'curl -fsSL https://vibewarden.dev/install.sh | sh';

/** Supported `${process.platform}/${process.arch}` keys mapped to GOOS/GOARCH. */
const SUPPORTED = {
  'darwin/x64': { goos: 'darwin', goarch: 'amd64' },
  'darwin/arm64': { goos: 'darwin', goarch: 'arm64' },
  'linux/x64': { goos: 'linux', goarch: 'amd64' },
  'linux/arm64': { goos: 'linux', goarch: 'arm64' },
};

/** Human-readable list of the supported combinations, used in error messages. */
const SUPPORTED_LIST = Object.keys(SUPPORTED);

/** Thrown when the running platform has no published VibeWarden binary. */
class UnsupportedPlatformError extends Error {
  /**
   * @param {string} platform value of process.platform
   * @param {string} arch value of process.arch
   */
  constructor(platform, arch) {
    const lines = [
      `VibeWarden does not publish a binary for ${platform}/${arch}.`,
      '',
      `Supported platforms: ${SUPPORTED_LIST.join(', ')}.`,
    ];
    if (platform === 'win32') {
      lines.push(
        '',
        'Native Windows is not supported yet. Run VibeWarden under WSL2 and install with:',
        `  ${INSTALL_SH}`,
      );
    } else {
      lines.push('', 'Build from source instead: https://github.com/vibewarden/vibewarden');
    }
    super(lines.join('\n'));
    this.name = 'UnsupportedPlatformError';
    this.code = 'EUNSUPPORTEDPLATFORM';
    this.platform = platform;
    this.arch = arch;
  }
}

/**
 * archiveName builds the goreleaser archive filename for a version and target.
 *
 * @param {string} version release version without the leading "v"
 * @param {string} goos GOOS value (darwin, linux)
 * @param {string} goarch GOARCH value (amd64, arm64)
 * @returns {string} archive filename
 */
function archiveName(version, goos, goarch) {
  return `vibewarden_${version}_${goos}_${goarch}.tar.gz`;
}

/**
 * resolvePlatform maps the running platform onto a release target.
 *
 * @param {string} version release version without the leading "v"
 * @param {string} [platform] override for process.platform (tests)
 * @param {string} [arch] override for process.arch (tests)
 * @returns {{goos: string, goarch: string, archiveName: string}}
 * @throws {UnsupportedPlatformError} when no binary is published for the platform
 */
function resolvePlatform(version, platform = process.platform, arch = process.arch) {
  const target = SUPPORTED[`${platform}/${arch}`];
  if (!target) {
    throw new UnsupportedPlatformError(platform, arch);
  }
  return {
    goos: target.goos,
    goarch: target.goarch,
    archiveName: archiveName(version, target.goos, target.goarch),
  };
}

module.exports = {
  SUPPORTED_LIST,
  UnsupportedPlatformError,
  archiveName,
  resolvePlatform,
};
