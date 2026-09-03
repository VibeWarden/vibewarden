'use strict';

/**
 * SHA-256 verification of a downloaded release archive.
 *
 * The digests normally come from checksums.json, embedded in the published npm
 * tarball at publish time. That chains the digest to npm's own tarball
 * integrity (which a lockfile pins) instead of merely proving transport
 * integrity, which is all a checksums.txt fetched from the artifact host can do.
 */

const crypto = require('node:crypto');

/** Thrown when an archive's digest does not match the expected one. */
class ChecksumMismatchError extends Error {
  /**
   * @param {string} archiveName archive the digest was computed over
   * @param {string} expected expected lowercase hex digest
   * @param {string} actual computed lowercase hex digest
   */
  constructor(archiveName, expected, actual) {
    super(
      [
        `checksum mismatch for ${archiveName}`,
        `  expected sha256: ${expected}`,
        `  actual   sha256: ${actual}`,
        '',
        'The download does not match the digest published for this release.',
        'Nothing was installed. Do not retry blindly: report this at',
        'https://github.com/vibewarden/vibewarden/issues',
      ].join('\n'),
    );
    this.name = 'ChecksumMismatchError';
    this.code = 'ECHECKSUM';
    this.archiveName = archiveName;
    this.expected = expected;
    this.actual = actual;
  }
}

/** Thrown when no digest is published for the archive we need. */
class MissingChecksumError extends Error {
  /**
   * @param {string} archiveName archive with no published digest
   */
  constructor(archiveName) {
    super(
      `no published SHA-256 digest for ${archiveName}; refusing to install an unverified binary`,
    );
    this.name = 'MissingChecksumError';
    this.code = 'EMISSINGCHECKSUM';
    this.archiveName = archiveName;
  }
}

/**
 * sha256 returns the lowercase hex SHA-256 digest of a buffer.
 *
 * @param {Buffer} buffer bytes to digest
 * @returns {string} lowercase hex digest
 */
function sha256(buffer) {
  return crypto.createHash('sha256').update(buffer).digest('hex');
}

/**
 * parseChecksumsTxt parses a goreleaser checksums.txt into a name → digest map.
 *
 * Lines are `<hex>  <filename>`. Blank lines are ignored; malformed lines are
 * rejected so a truncated or HTML error page can never be read as a checksum file.
 *
 * @param {string} text contents of checksums.txt
 * @returns {Record<string, string>} archive name → lowercase hex digest
 * @throws {Error} on a malformed line
 */
function parseChecksumsTxt(text) {
  /** @type {Record<string, string>} */
  const out = {};
  const lines = text.split('\n');
  for (const raw of lines) {
    const line = raw.trim();
    if (line === '') {
      continue;
    }
    const match = /^([0-9a-fA-F]{64})\s+\*?(\S.*)$/.exec(line);
    if (!match) {
      throw new Error(`malformed checksums.txt line: ${JSON.stringify(raw)}`);
    }
    out[match[2].trim()] = match[1].toLowerCase();
  }
  return out;
}

/**
 * lookupDigest returns the published digest for an archive.
 *
 * @param {Record<string, string>} digests archive name → hex digest
 * @param {string} archiveName archive to look up
 * @returns {string} lowercase hex digest
 * @throws {MissingChecksumError} when the archive has no published digest
 */
function lookupDigest(digests, archiveName) {
  const expected = digests?.[archiveName];
  if (typeof expected !== 'string' || expected === '') {
    throw new MissingChecksumError(archiveName);
  }
  return expected.toLowerCase();
}

/**
 * verifyDigest checks a buffer against an expected digest.
 *
 * @param {Buffer} buffer downloaded archive bytes
 * @param {string} expected expected hex digest
 * @param {string} archiveName archive name, for the error message
 * @returns {string} the computed digest, when it matches
 * @throws {ChecksumMismatchError} when the digests differ
 */
function verifyDigest(buffer, expected, archiveName) {
  const actual = sha256(buffer);
  const want = expected.toLowerCase();
  if (actual !== want) {
    throw new ChecksumMismatchError(archiveName, want, actual);
  }
  return actual;
}

module.exports = {
  ChecksumMismatchError,
  MissingChecksumError,
  sha256,
  parseChecksumsTxt,
  lookupDigest,
  verifyDigest,
};
