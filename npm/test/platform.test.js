'use strict';

const test = require('node:test');
const assert = require('node:assert');

const { resolvePlatform, archiveName, UnsupportedPlatformError } = require('../lib/platform');

// Registered as top-level tests rather than t.test() subtests: a synchronous
// parent does not await its subtests, so Node < 23 cancels them and the
// assertions never run. Same reason for every loop below.
const supported = [
  { platform: 'darwin', arch: 'x64', goos: 'darwin', goarch: 'amd64' },
  { platform: 'darwin', arch: 'arm64', goos: 'darwin', goarch: 'arm64' },
  { platform: 'linux', arch: 'x64', goos: 'linux', goarch: 'amd64' },
  { platform: 'linux', arch: 'arm64', goos: 'linux', goarch: 'arm64' },
];

for (const c of supported) {
  test(`resolvePlatform maps ${c.platform}/${c.arch} to its archive`, () => {
    const got = resolvePlatform('0.19.0', c.platform, c.arch);
    assert.strictEqual(got.goos, c.goos);
    assert.strictEqual(got.goarch, c.goarch);
    assert.strictEqual(got.archiveName, `vibewarden_0.19.0_${c.goos}_${c.goarch}.tar.gz`);
  });
}

const unsupported = [
  { platform: 'win32', arch: 'x64' },
  { platform: 'win32', arch: 'arm64' },
  { platform: 'linux', arch: 'arm' },
  { platform: 'linux', arch: 's390x' },
  { platform: 'freebsd', arch: 'x64' },
  { platform: 'darwin', arch: 'ia32' },
];

for (const c of unsupported) {
  test(`resolvePlatform rejects unpublished platform ${c.platform}/${c.arch}`, () => {
    assert.throws(
      () => resolvePlatform('0.19.0', c.platform, c.arch),
      (err) => {
        assert.ok(err instanceof UnsupportedPlatformError);
        assert.strictEqual(err.code, 'EUNSUPPORTEDPLATFORM');
        assert.match(err.message, new RegExp(`${c.platform}/${c.arch}`));
        return true;
      },
    );
  });
}

test('win32 error points at WSL and the shell installer', () => {
  assert.throws(
    () => resolvePlatform('0.19.0', 'win32', 'x64'),
    (err) => {
      assert.match(err.message, /WSL2/);
      assert.match(err.message, /install\.sh/);
      return true;
    },
  );
});

test('archiveName matches the goreleaser name_template contract', () => {
  // Contract shared with scripts/install.sh and internal/app/upgrade/service.go.
  assert.strictEqual(archiveName('1.2.3', 'linux', 'amd64'), 'vibewarden_1.2.3_linux_amd64.tar.gz');
  assert.strictEqual(archiveName('0.19.0-rc.1', 'darwin', 'arm64'), 'vibewarden_0.19.0-rc.1_darwin_arm64.tar.gz');
});
