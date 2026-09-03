'use strict';

/**
 * Minimal .tar.gz reader: gunzip via node:zlib plus a hand-rolled tar walker.
 *
 * Node's stdlib has no tar reader, and the obvious npm helpers (tar, adm-zip,
 * yauzl) are ISC, outside this project's approved license set. Extracting one
 * known member from a well-formed goreleaser archive needs ~60 lines, so the
 * package stays dependency-free (ADR-112).
 */

const zlib = require('node:zlib');

const BLOCK_SIZE = 512;
const NAME_OFFSET = 0;
const NAME_LENGTH = 100;
const SIZE_OFFSET = 124;
const SIZE_LENGTH = 12;
const TYPEFLAG_OFFSET = 156;
const PREFIX_OFFSET = 345;
const PREFIX_LENGTH = 155;

/** Thrown when an archive is truncated, malformed, or missing the wanted member. */
class TarError extends Error {
  /**
   * @param {string} message failure description
   * @param {string} code machine-readable code
   */
  constructor(message, code) {
    super(message);
    this.name = 'TarError';
    this.code = code;
  }
}

/** Reads a NUL-terminated string field out of a tar header block. */
function readString(block, offset, length) {
  const raw = block.subarray(offset, offset + length);
  const end = raw.indexOf(0);
  return raw.toString('utf8', 0, end === -1 ? raw.length : end).trim();
}

/** Reads an octal numeric tar header field. */
function readOctal(block, offset, length) {
  const text = readString(block, offset, length).replace(/\0/g, '').trim();
  if (text === '') {
    return 0;
  }
  const value = parseInt(text, 8);
  if (Number.isNaN(value)) {
    throw new TarError(`malformed octal field at offset ${offset}: ${JSON.stringify(text)}`, 'EMALFORMEDTAR');
  }
  return value;
}

/** Normalises a tar member path for comparison ("./vibew" and "vibew" are the same file). */
function normaliseName(name) {
  return name.replace(/^\.\//, '').replace(/\/+$/, '');
}

/**
 * listEntries walks an uncompressed tar buffer and returns its regular files.
 *
 * @param {Buffer} tarBuffer uncompressed tar bytes
 * @returns {Array<{name: string, size: number, offset: number}>} regular file entries
 * @throws {TarError} when the archive is truncated or malformed
 */
function listEntries(tarBuffer) {
  const entries = [];
  let offset = 0;

  while (offset + BLOCK_SIZE <= tarBuffer.length) {
    const header = tarBuffer.subarray(offset, offset + BLOCK_SIZE);

    // Two consecutive zero blocks mark the end of the archive; one is enough for us.
    if (header.every((byte) => byte === 0)) {
      break;
    }

    const name = readString(header, NAME_OFFSET, NAME_LENGTH);
    const prefix = readString(header, PREFIX_OFFSET, PREFIX_LENGTH);
    const size = readOctal(header, SIZE_OFFSET, SIZE_LENGTH);
    const typeflag = header[TYPEFLAG_OFFSET];
    const dataStart = offset + BLOCK_SIZE;
    const dataEnd = dataStart + size;

    if (dataEnd > tarBuffer.length) {
      throw new TarError(
        `truncated tar archive: entry ${JSON.stringify(name)} claims ${size} bytes but only ${tarBuffer.length - dataStart} remain`,
        'ETRUNCATEDTAR',
      );
    }

    // 0x30 ('0') and 0x00 both mark a regular file; everything else
    // (directories, links, PAX/GNU metadata) is skipped.
    if (typeflag === 0x30 || typeflag === 0x00) {
      entries.push({
        name: normaliseName(prefix === '' ? name : `${prefix}/${name}`),
        size,
        offset: dataStart,
      });
    }

    offset = dataStart + Math.ceil(size / BLOCK_SIZE) * BLOCK_SIZE;
  }

  return entries;
}

/**
 * extractMember gunzips a .tar.gz and returns the bytes of one named member.
 *
 * @param {Buffer} archiveBuffer gzip-compressed tar bytes
 * @param {string} memberName member path inside the archive, e.g. "vibew"
 * @returns {Buffer} member contents
 * @throws {TarError} when the archive is unreadable or the member is absent
 */
function extractMember(archiveBuffer, memberName) {
  let tarBuffer;
  try {
    tarBuffer = zlib.gunzipSync(archiveBuffer);
  } catch (err) {
    throw new TarError(`archive is not valid gzip data: ${err.message}`, 'EBADGZIP');
  }

  const wanted = normaliseName(memberName);
  const entries = listEntries(tarBuffer);
  const entry = entries.find((e) => e.name === wanted);
  if (!entry) {
    const found = entries.map((e) => e.name).join(', ') || '(none)';
    throw new TarError(
      `archive does not contain ${JSON.stringify(memberName)}; members: ${found}`,
      'EMEMBERNOTFOUND',
    );
  }

  return Buffer.from(tarBuffer.subarray(entry.offset, entry.offset + entry.size));
}

module.exports = { TarError, listEntries, extractMember };
