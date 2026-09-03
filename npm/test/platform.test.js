'use strict';

const test = require('node:test');
const assert = require('node:assert');

const { resolvePlatform, archiveName, UnsupportedPlatformError } = require('../lib/platform');

test('resolvePlatform maps every published platform to its archive', (t) => {
  const cases = [
    { platform: 'darwin', arch: 'x64', goos: 'darwin', goarch: 'amd64' },
    { platform: 'darwin', arch: 'arm64', goos: 'darwin', goarch: 'arm64' },
    { platform: 'linux', arch: 'x64', goos: 'linux', goarch: 'amd64' },
    { platform: 'linux', arch: 'arm64', goos: 'linux', goarch: 'arm64' },
  ];

  for (const c of cases) {
    t.test(`${c.platform}/${c.arch}`, () => {
      const got = resolvePlatform('0.19.0', c.platform, c.arch);
      assert.strictEqual(got.goos, c.goos);
      assert.strictEqual(got.goarch, c.goarch);
      assert.strictEqual(got.archiveName, `vibewarden_0.19.0_${c.goos}_${c.goarch}.tar.gz`);
    });
  }
});

test('resolvePlatform rejects unpublished platforms', (t) => {
  const cases = [
    { platform: 'win32', arch: 'x64' },
    { platform: 'win32', arch: 'arm64' },
    { platform: 'linux', arch: 'arm' },
    { platform: 'linux', arch: 's390x' },
    { platform: 'freebsd', arch: 'x64' },
    { platform: 'darwin', arch: 'ia32' },
  ];

  for (const c of cases) {
    t.test(`${c.platform}/${c.arch}`, () => {
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
});

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
