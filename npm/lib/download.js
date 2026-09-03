'use strict';

/**
 * Minimal HTTP(S) downloader built on the Node stdlib.
 *
 * node:https does not follow redirects, and GitHub release downloads always
 * 302 to objects.githubusercontent.com, so redirects are followed by hand.
 * Retries cover transient network errors and 5xx responses only: a 404 means
 * the release or asset does not exist and will never succeed.
 */

const http = require('node:http');
const https = require('node:https');
const { URL } = require('node:url');

const DEFAULT_TIMEOUT_MS = 30_000;
const DEFAULT_ATTEMPTS = 3;
const DEFAULT_MAX_REDIRECTS = 5;
const DEFAULT_BACKOFF_MS = 1_000;

/** Thrown for every download failure, carrying a machine-readable `code`. */
class DownloadError extends Error {
  /**
   * @param {string} message human-readable failure description
   * @param {{code: string, url: string, status?: number, cause?: Error}} details
   */
  constructor(message, details) {
    super(message);
    this.name = 'DownloadError';
    this.code = details.code;
    this.url = details.url;
    if (details.status !== undefined) {
      this.status = details.status;
    }
    if (details.cause !== undefined) {
      this.cause = details.cause;
    }
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * fetchOnce performs a single GET, following redirects, and buffers the body.
 *
 * @param {string} url absolute http(s) URL
 * @param {{timeoutMs: number, maxRedirects: number}} opts
 * @returns {Promise<Buffer>} response body
 */
function fetchOnce(url, opts) {
  return new Promise((resolve, reject) => {
    let current;
    try {
      current = new URL(url);
    } catch (err) {
      reject(new DownloadError(`invalid download URL: ${url}`, { code: 'EINVALIDURL', url, cause: err }));
      return;
    }

    const client = current.protocol === 'http:' ? http : https;
    const req = client.get(
      current,
      { headers: { 'user-agent': 'vibewarden-npm-installer', accept: '*/*' } },
      (res) => {
        const status = res.statusCode ?? 0;

        if (status >= 300 && status < 400 && res.headers.location) {
          res.resume();
          if (opts.maxRedirects <= 0) {
            reject(
              new DownloadError(`too many redirects while downloading ${url}`, {
                code: 'ETOOMANYREDIRECTS',
                url,
                status,
              }),
            );
            return;
          }
          const next = new URL(res.headers.location, current).toString();
          fetchOnce(next, { ...opts, maxRedirects: opts.maxRedirects - 1 }).then(resolve, reject);
          return;
        }

        if (status === 404) {
          res.resume();
          reject(
            new DownloadError(`not found (HTTP 404): ${url}`, { code: 'ENOTFOUND404', url, status }),
          );
          return;
        }

        if (status < 200 || status >= 300) {
          res.resume();
          reject(
            new DownloadError(`unexpected HTTP ${status} for ${url}`, {
              code: status >= 500 ? 'ESERVER' : 'EHTTP',
              url,
              status,
            }),
          );
          return;
        }

        const chunks = [];
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('end', () => resolve(Buffer.concat(chunks)));
        res.on('error', (err) => {
          reject(new DownloadError(`connection error while reading ${url}: ${err.message}`, {
            code: 'ENETWORK',
            url,
            cause: err,
          }));
        });
      },
    );

    req.setTimeout(opts.timeoutMs, () => {
      req.destroy(
        new DownloadError(`timed out after ${opts.timeoutMs}ms downloading ${url}`, {
          code: 'ETIMEDOUT',
          url,
        }),
      );
    });

    req.on('error', (err) => {
      if (err instanceof DownloadError) {
        reject(err);
        return;
      }
      reject(
        new DownloadError(`network error downloading ${url}: ${err.message}`, {
          code: 'ENETWORK',
          url,
          cause: err,
        }),
      );
    });
  });
}

/** Failure codes that are worth retrying: transient by nature. */
const RETRYABLE = new Set(['ENETWORK', 'ETIMEDOUT', 'ESERVER']);

/**
 * download GETs a URL into memory with bounded retries.
 *
 * 404 responses are never retried. Transport errors, timeouts and 5xx responses
 * are retried with linear backoff.
 *
 * @param {string} url absolute http(s) URL
 * @param {object} [options]
 * @param {number} [options.timeoutMs] per-request timeout, default 30000
 * @param {number} [options.attempts] total attempts, default 3
 * @param {number} [options.maxRedirects] redirect hops allowed, default 5
 * @param {number} [options.backoffMs] base backoff between attempts, default 1000
 * @param {(msg: string) => void} [options.log] progress logger
 * @returns {Promise<Buffer>} response body
 * @throws {DownloadError} when every attempt fails
 */
async function download(url, options = {}) {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const attempts = options.attempts ?? DEFAULT_ATTEMPTS;
  const maxRedirects = options.maxRedirects ?? DEFAULT_MAX_REDIRECTS;
  const backoffMs = options.backoffMs ?? DEFAULT_BACKOFF_MS;
  const log = options.log ?? (() => {});

  let lastErr;
  for (let attempt = 1; attempt <= attempts; attempt++) {
    try {
      return await fetchOnce(url, { timeoutMs, maxRedirects });
    } catch (err) {
      lastErr = err;
      if (!(err instanceof DownloadError) || !RETRYABLE.has(err.code) || attempt === attempts) {
        throw err;
      }
      log(`attempt ${attempt}/${attempts} failed (${err.code}), retrying: ${err.message}`);
      await sleep(backoffMs * attempt);
    }
  }
  throw lastErr;
}

module.exports = { DownloadError, download };
