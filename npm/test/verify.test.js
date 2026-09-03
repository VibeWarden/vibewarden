'use strict';

const test = require('node:test');
const assert = require('node:assert');
const crypto = require('node:crypto');

const {
  sha256,
  verifyDigest,
  lookupDigest,
  parseChecksumsTxt,
  ChecksumMismatchError,
  MissingChecksumError,
} = require('../lib/verify');

const payload = Buffer.from('vibewarden release archive');
const digest = crypto.createHash('sha256').update(payload).digest('hex');

test('sha256 matches node:crypto', () => {
  assert.strictEqual(sha256(payload), digest);
});

test('verifyDigest accepts a matching digest, case-insensitively', () => {
  assert.strictEqual(verifyDigest(payload, digest, 'archive.tar.gz'), digest);
  assert.strictEqual(verifyDigest(payload, digest.toUpperCase(), 'archive.tar.gz'), digest);
});

test('verifyDigest rejects a mismatching digest and names both digests', () => {
  const wrong = 'f'.repeat(64);
  assert.throws(
    () => verifyDigest(payload, wrong, 'vibewarden_0.19.0_linux_amd64.tar.gz'),
    (err) => {
      assert.ok(err instanceof ChecksumMismatchError);
      assert.strictEqual(err.code, 'ECHECKSUM');
      assert.match(err.message, /vibewarden_0\.19\.0_linux_amd64\.tar\.gz/);
      assert.match(err.message, new RegExp(wrong));
      assert.match(err.message, new RegExp(digest));
      return true;
    },
  );
});

// Registered as top-level tests rather than t.test() subtests: a synchronous
// parent does not await its subtests, so Node < 23 cancels them and the
// assertions never run. Same reason for the loop below.
const publishedDigests = { 'vibewarden_0.19.0_linux_amd64.tar.gz': digest.toUpperCase() };

test('lookupDigest returns the published digest, normalised to lowercase', () => {
  assert.strictEqual(lookupDigest(publishedDigests, 'vibewarden_0.19.0_linux_amd64.tar.gz'), digest);
});

test('lookupDigest rejects an archive with no published digest', () => {
  assert.throws(
    () => lookupDigest(publishedDigests, 'vibewarden_0.19.0_darwin_arm64.tar.gz'),
    (err) => {
      assert.ok(err instanceof MissingChecksumError);
      assert.strictEqual(err.code, 'EMISSINGCHECKSUM');
      return true;
    },
  );
});

test('lookupDigest rejects an empty digest map', () => {
  assert.throws(() => lookupDigest({}, 'anything.tar.gz'), MissingChecksumError);
});

test('lookupDigest rejects an undefined digest map', () => {
  assert.throws(() => lookupDigest(undefined, 'anything.tar.gz'), MissingChecksumError);
});

test('parseChecksumsTxt parses goreleaser output', () => {
  const text = [
    `${digest}  vibewarden_0.19.0_darwin_arm64.tar.gz`,
    `${'a'.repeat(64)}  vibewarden_0.19.0_linux_amd64.tar.gz`,
    '',
  ].join('\n');

  assert.deepStrictEqual(parseChecksumsTxt(text), {
    'vibewarden_0.19.0_darwin_arm64.tar.gz': digest,
    'vibewarden_0.19.0_linux_amd64.tar.gz': 'a'.repeat(64),
  });
});

const malformedChecksums = [
  { name: 'html error page', text: '<html><body>404</body></html>' },
  { name: 'short digest', text: 'abc123  vibewarden_0.19.0_linux_amd64.tar.gz' },
  { name: 'missing filename', text: `${digest}` },
];

for (const c of malformedChecksums) {
  test(`parseChecksumsTxt rejects malformed input rather than trusting it: ${c.name}`, () => {
    assert.throws(() => parseChecksumsTxt(c.text), /malformed checksums\.txt line/);
  });
}
