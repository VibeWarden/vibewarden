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

test('lookupDigest returns the published digest and rejects absent ones', (t) => {
  const digests = { 'vibewarden_0.19.0_linux_amd64.tar.gz': digest.toUpperCase() };

  t.test('present', () => {
    assert.strictEqual(lookupDigest(digests, 'vibewarden_0.19.0_linux_amd64.tar.gz'), digest);
  });

  t.test('absent', () => {
    assert.throws(
      () => lookupDigest(digests, 'vibewarden_0.19.0_darwin_arm64.tar.gz'),
      (err) => {
        assert.ok(err instanceof MissingChecksumError);
        assert.strictEqual(err.code, 'EMISSINGCHECKSUM');
        return true;
      },
    );
  });

  t.test('empty map', () => {
    assert.throws(() => lookupDigest({}, 'anything.tar.gz'), MissingChecksumError);
  });

  t.test('undefined map', () => {
    assert.throws(() => lookupDigest(undefined, 'anything.tar.gz'), MissingChecksumError);
  });
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

test('parseChecksumsTxt rejects malformed input rather than trusting it', (t) => {
  const cases = [
    { name: 'html error page', text: '<html><body>404</body></html>' },
    { name: 'short digest', text: 'abc123  vibewarden_0.19.0_linux_amd64.tar.gz' },
    { name: 'missing filename', text: `${digest}` },
  ];

  for (const c of cases) {
    t.test(c.name, () => {
      assert.throws(() => parseChecksumsTxt(c.text), /malformed checksums\.txt line/);
    });
  }
});
