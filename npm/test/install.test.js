'use strict';

const test = require('node:test');
const assert = require('node:assert');
const http = require('node:http');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const { install } = require('../install');
const { makeReleaseArchive } = require('./fixtures/make-fixture');

const VERSION = '9.9.9';
const ARCHIVE = `vibewarden_${VERSION}_linux_amd64.tar.gz`;

/**
 * startMirror serves a release-download tree at /v<version>/<asset>.
 */
async function startMirror(assets) {
  const requests = [];
  const server = http.createServer((req, res) => {
    requests.push(req.url);
    const body = assets[req.url];
    if (body === undefined) {
      res.writeHead(404);
      res.end('not found');
      return;
    }
    res.writeHead(200);
    res.end(body);
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  return {
    baseURL: `http://127.0.0.1:${port}`,
    requests,
    close: () => new Promise((resolve) => server.close(resolve)),
  };
}

function tempVendorDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'vibew-npm-test-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return path.join(dir, 'vendor');
}

function baseOptions(t, mirrorURL, overrides = {}) {
  return {
    env: { VIBEWARDEN_BINARY_MIRROR: mirrorURL, ...(overrides.env ?? {}) },
    packageJson: { version: VERSION },
    vendorDir: tempVendorDir(t),
    platform: 'linux',
    arch: 'x64',
    log: () => {},
    downloadOptions: { attempts: 1, backoffMs: 1, timeoutMs: 5000 },
    ...overrides,
  };
}

test('install downloads, verifies and extracts the binary', async (t) => {
  const fixture = makeReleaseArchive();
  const mirror = await startMirror({ [`/v${VERSION}/${ARCHIVE}`]: fixture.buffer });
  t.after(() => mirror.close());

  const opts = baseOptions(t, mirror.baseURL, {
    checksums: { version: VERSION, archives: { [ARCHIVE]: fixture.sha256 } },
  });

  const result = await install(opts);

  assert.strictEqual(result.skipped, false);
  assert.strictEqual(result.version, VERSION);
  assert.strictEqual(result.binaryPath, path.join(opts.vendorDir, 'vibew'));
  assert.strictEqual(fs.readFileSync(result.binaryPath, 'utf8'), fixture.binaryContent);
  assert.strictEqual(fs.statSync(result.binaryPath).mode & 0o777, 0o755);
  // The embedded digest is used: checksums.txt is never requested.
  assert.deepStrictEqual(mirror.requests, [`/v${VERSION}/${ARCHIVE}`]);
});

test('install leaves vendor/ empty when the archive fails verification', async (t) => {
  const fixture = makeReleaseArchive();
  const mirror = await startMirror({ [`/v${VERSION}/${ARCHIVE}`]: fixture.buffer });
  t.after(() => mirror.close());

  const opts = baseOptions(t, mirror.baseURL, {
    checksums: { version: VERSION, archives: { [ARCHIVE]: 'd'.repeat(64) } },
  });

  await assert.rejects(() => install(opts), (err) => {
    assert.strictEqual(err.code, 'ECHECKSUM');
    return true;
  });
  assert.strictEqual(fs.existsSync(opts.vendorDir), false, 'nothing may be written on mismatch');
});

test('install refuses to proceed when no digest is published for the archive', async (t) => {
  const fixture = makeReleaseArchive();
  const mirror = await startMirror({ [`/v${VERSION}/${ARCHIVE}`]: fixture.buffer });
  t.after(() => mirror.close());

  const opts = baseOptions(t, mirror.baseURL, {
    checksums: { version: VERSION, archives: {} },
  });

  await assert.rejects(() => install(opts), (err) => {
    assert.strictEqual(err.code, 'EMISSINGCHECKSUM');
    return true;
  });
  assert.strictEqual(fs.existsSync(opts.vendorDir), false);
});

test('install falls back to checksums.txt when the version is overridden', async (t) => {
  const fixture = makeReleaseArchive();
  const mirror = await startMirror({
    [`/v${VERSION}/${ARCHIVE}`]: fixture.buffer,
    [`/v${VERSION}/checksums.txt`]: `${fixture.sha256}  ${ARCHIVE}\n`,
  });
  t.after(() => mirror.close());

  const logged = [];
  const opts = baseOptions(t, mirror.baseURL, {
    env: { VIBEWARDEN_BINARY_MIRROR: mirror.baseURL, VIBEWARDEN_INSTALL_VERSION: `v${VERSION}` },
    packageJson: { version: '0.0.0-dev' },
    checksums: { version: '0.0.0-dev', archives: {} },
    log: (msg) => logged.push(msg),
  });

  const result = await install(opts);

  assert.strictEqual(result.version, VERSION);
  assert.ok(fs.existsSync(result.binaryPath));
  assert.ok(mirror.requests.includes(`/v${VERSION}/checksums.txt`));
  assert.ok(
    logged.some((m) => m.includes('transport integrity only')),
    'the weaker verification path must warn',
  );
});

test('install reports a missing release asset without retrying it into a timeout', async (t) => {
  const mirror = await startMirror({});
  t.after(() => mirror.close());

  const opts = baseOptions(t, mirror.baseURL, {
    checksums: { version: VERSION, archives: { [ARCHIVE]: 'd'.repeat(64) } },
  });

  await assert.rejects(() => install(opts), (err) => {
    assert.match(err.message, /release asset not found/);
    assert.match(err.message, new RegExp(ARCHIVE));
    return true;
  });
});

test('install fails on an unsupported platform before touching the network', async (t) => {
  const mirror = await startMirror({});
  t.after(() => mirror.close());

  const opts = baseOptions(t, mirror.baseURL, { platform: 'win32', arch: 'x64' });

  await assert.rejects(() => install(opts), (err) => {
    assert.strictEqual(err.code, 'EUNSUPPORTEDPLATFORM');
    return true;
  });
  assert.deepStrictEqual(mirror.requests, []);
});

test('install skips the download when asked to', async (t) => {
  const mirror = await startMirror({});
  t.after(() => mirror.close());

  const skipRequested = await install(
    baseOptions(t, mirror.baseURL, {
      env: { VIBEWARDEN_BINARY_MIRROR: mirror.baseURL, VIBEWARDEN_SKIP_DOWNLOAD: '1' },
    }),
  );
  assert.deepStrictEqual(skipRequested, { skipped: true, reason: 'skip-requested' });

  const placeholder = await install(
    baseOptions(t, mirror.baseURL, { packageJson: { version: '0.0.0-dev' } }),
  );
  assert.deepStrictEqual(placeholder, { skipped: true, reason: 'placeholder-version' });

  assert.deepStrictEqual(mirror.requests, []);
});
