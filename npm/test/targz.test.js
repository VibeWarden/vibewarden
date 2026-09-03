'use strict';

const test = require('node:test');
const assert = require('node:assert');
const zlib = require('node:zlib');

const { extractMember, listEntries, TarError } = require('../lib/targz');
const { makeTar, makeTarGz, makeReleaseArchive } = require('./fixtures/make-fixture');

test('extractMember round-trips the binary out of a release-shaped archive', () => {
  const fixture = makeReleaseArchive();
  const got = extractMember(fixture.buffer, 'vibew');
  assert.strictEqual(got.toString('utf8'), fixture.binaryContent);
});

test('extractMember handles binary content with NUL bytes and exact block sizes', () => {
  const content = Buffer.alloc(1024);
  content.fill(0);
  content.write('ELF', 1);
  content[1023] = 0xff;

  const archive = makeTarGz([{ name: 'vibew', content, mode: 0o755 }]);
  const got = extractMember(archive, 'vibew');
  assert.strictEqual(got.length, 1024);
  assert.ok(got.equals(content));
});

test('extractMember normalises leading ./ in member names', () => {
  const archive = makeTarGz([{ name: './vibew', content: 'body', mode: 0o755 }]);
  assert.strictEqual(extractMember(archive, 'vibew').toString('utf8'), 'body');
});

test('extractMember fails when the member is absent', () => {
  const archive = makeTarGz([{ name: 'README.md', content: 'nope' }]);
  assert.throws(
    () => extractMember(archive, 'vibew'),
    (err) => {
      assert.ok(err instanceof TarError);
      assert.strictEqual(err.code, 'EMEMBERNOTFOUND');
      assert.match(err.message, /README\.md/);
      return true;
    },
  );
});

test('extractMember fails on a truncated archive', () => {
  const tar = makeTar([{ name: 'vibew', content: 'x'.repeat(2048), mode: 0o755 }]);
  const truncated = zlib.gzipSync(tar.subarray(0, 1024));
  assert.throws(
    () => extractMember(truncated, 'vibew'),
    (err) => {
      assert.ok(err instanceof TarError);
      assert.strictEqual(err.code, 'ETRUNCATEDTAR');
      return true;
    },
  );
});

test('extractMember fails on non-gzip input', () => {
  assert.throws(
    () => extractMember(Buffer.from('<html>404 not found</html>'), 'vibew'),
    (err) => {
      assert.ok(err instanceof TarError);
      assert.strictEqual(err.code, 'EBADGZIP');
      return true;
    },
  );
});

test('listEntries skips directories and reports regular files only', () => {
  const tar = makeTar([
    { name: 'dir/', content: '', typeflag: '5' },
    { name: 'dir/file.txt', content: 'hello' },
    { name: 'vibew', content: 'binary', mode: 0o755 },
  ]);
  const names = listEntries(tar).map((e) => e.name);
  assert.deepStrictEqual(names, ['dir/file.txt', 'vibew']);
});
