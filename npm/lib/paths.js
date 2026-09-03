'use strict';

/**
 * Filesystem layout of the installed package.
 *
 * Everything the shim and the installer need to agree on lives here so the two
 * can never drift. Paths are absolute and resolved from this file's location,
 * which works for global installs, local installs, pnpm stores and npx caches
 * alike.
 */

const path = require('node:path');

/** Absolute path of the package root (the directory holding package.json). */
const PACKAGE_ROOT = path.resolve(__dirname, '..');

/** Directory the verified binary is extracted into. Not published; created at postinstall. */
const VENDOR_DIR = path.join(PACKAGE_ROOT, 'vendor');

/** Name of the executable inside the release archive and inside vendor/. */
const BINARY_NAME = 'vibew';

/** Absolute path of the extracted binary. */
const BINARY_PATH = path.join(VENDOR_DIR, BINARY_NAME);

/** Absolute path of the postinstall script, used in the --ignore-scripts hint. */
const INSTALL_SCRIPT = path.join(PACKAGE_ROOT, 'install.js');

module.exports = {
  PACKAGE_ROOT,
  VENDOR_DIR,
  BINARY_NAME,
  BINARY_PATH,
  INSTALL_SCRIPT,
};
