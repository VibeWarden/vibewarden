'use strict';

const test = require('node:test');
const assert = require('node:assert');
const http = require('node:http');

const { download, DownloadError } = require('../lib/download');

/**
 * startServer boots a throwaway HTTP server driven by a request handler and
 * returns its base URL plus a close function.
 */
async function startServer(handler) {
  const server = http.createServer(handler);
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  return {
    baseURL: `http://127.0.0.1:${port}`,
    close: () => new Promise((resolve) => server.close(resolve)),
  };
}

test('download returns the response body', async (t) => {
  const srv = await startServer((req, res) => {
    res.writeHead(200);
    res.end('payload');
  });
  t.after(() => srv.close());

  const body = await download(`${srv.baseURL}/archive.tar.gz`);
  assert.strictEqual(body.toString('utf8'), 'payload');
});

test('download follows a redirect hop, as GitHub release downloads require', async (t) => {
  let hops = 0;
  const srv = await startServer((req, res) => {
    if (req.url === '/archive.tar.gz') {
      hops++;
      res.writeHead(302, { location: '/objects/blob' });
      res.end();
      return;
    }
    res.writeHead(200);
    res.end('redirected-payload');
  });
  t.after(() => srv.close());

  const body = await download(`${srv.baseURL}/archive.tar.gz`);
  assert.strictEqual(body.toString('utf8'), 'redirected-payload');
  assert.strictEqual(hops, 1);
});

test('download gives up on a redirect loop', async (t) => {
  const srv = await startServer((req, res) => {
    res.writeHead(302, { location: '/loop' });
    res.end();
  });
  t.after(() => srv.close());

  await assert.rejects(
    () => download(`${srv.baseURL}/loop`, { maxRedirects: 3 }),
    (err) => {
      assert.ok(err instanceof DownloadError);
      assert.strictEqual(err.code, 'ETOOMANYREDIRECTS');
      return true;
    },
  );
});

test('download does not retry a 404', async (t) => {
  let requests = 0;
  const srv = await startServer((req, res) => {
    requests++;
    res.writeHead(404);
    res.end('not found');
  });
  t.after(() => srv.close());

  await assert.rejects(
    () => download(`${srv.baseURL}/missing.tar.gz`, { attempts: 3, backoffMs: 1 }),
    (err) => {
      assert.ok(err instanceof DownloadError);
      assert.strictEqual(err.code, 'ENOTFOUND404');
      return true;
    },
  );
  assert.strictEqual(requests, 1, 'a 404 must not be retried');
});

test('download retries a 5xx and succeeds', async (t) => {
  let requests = 0;
  const srv = await startServer((req, res) => {
    requests++;
    if (requests < 3) {
      res.writeHead(503);
      res.end('unavailable');
      return;
    }
    res.writeHead(200);
    res.end('eventually-ok');
  });
  t.after(() => srv.close());

  const body = await download(`${srv.baseURL}/archive.tar.gz`, { attempts: 3, backoffMs: 1 });
  assert.strictEqual(body.toString('utf8'), 'eventually-ok');
  assert.strictEqual(requests, 3);
});

test('download fails with the status code after exhausting retries on 5xx', async (t) => {
  const srv = await startServer((req, res) => {
    res.writeHead(500);
    res.end('boom');
  });
  t.after(() => srv.close());

  await assert.rejects(
    () => download(`${srv.baseURL}/archive.tar.gz`, { attempts: 2, backoffMs: 1 }),
    (err) => {
      assert.strictEqual(err.code, 'ESERVER');
      assert.strictEqual(err.status, 500);
      return true;
    },
  );
});

test('download times out on a hanging response', async (t) => {
  const pending = [];
  const srv = await startServer((req, res) => {
    res.writeHead(200);
    res.write('partial');
    pending.push(res); // never ended
  });
  t.after(async () => {
    for (const res of pending) {
      res.end();
    }
    await srv.close();
  });

  await assert.rejects(
    () => download(`${srv.baseURL}/slow.tar.gz`, { attempts: 1, timeoutMs: 50, backoffMs: 1 }),
    (err) => {
      assert.ok(err instanceof DownloadError);
      assert.strictEqual(err.code, 'ETIMEDOUT');
      return true;
    },
  );
});

test('download reports a connection failure with the URL', async () => {
  // Port 1 on loopback is never listening: connection refused, not a timeout.
  await assert.rejects(
    () => download('http://127.0.0.1:1/archive.tar.gz', { attempts: 2, backoffMs: 1 }),
    (err) => {
      assert.ok(err instanceof DownloadError);
      assert.strictEqual(err.code, 'ENETWORK');
      assert.match(err.message, /127\.0\.0\.1:1/);
      return true;
    },
  );
});
