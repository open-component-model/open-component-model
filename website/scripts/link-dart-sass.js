#!/usr/bin/env node
/**
 * Symlink the native Dart Sass binary from sass-embedded into node_modules/.bin/
 * to allow a self-contained setup which can be installed via `npm ci`.
 *
 * Hugo's dartsass transpiler expects a native `sass` executable on the PATH.
 * The `sass-embedded` npm package bundles a native Dart Sass binary inside a
 * platform-specific optional dependency (e.g. sass-embedded-darwin-arm64), but
 * does not expose it as a CLI in node_modules/.bin/.
 *
 * This script resolves the native binary for the current platform and creates
 * a symlink at node_modules/.bin/sass so Hugo finds the real executable.
 *
 * Runs automatically via the "postinstall" npm lifecycle script.
 */

const fs = require('node:fs');
const path = require('node:path');

const PLATFORM_MAP = {
  'darwin-arm64': 'sass-embedded-darwin-arm64',
  'darwin-x64': 'sass-embedded-darwin-x64',
  'linux-x64': 'sass-embedded-linux-x64',
  'linux-arm64': 'sass-embedded-linux-arm64',
};

function main() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = PLATFORM_MAP[key];
  if (!pkg) {
    throw new Error(
      `Unsupported platform: ${key}. ` +
      `Supported: ${Object.keys(PLATFORM_MAP).join(', ')}. ` +
      'Install the standalone Dart Sass binary manually.'
    );
  }

  const nodeModules = path.resolve(__dirname, '../node_modules');
  const nativeSass = path.join(nodeModules, pkg, 'dart-sass', 'sass');

  if (!fs.existsSync(nativeSass)) {
    throw new Error(
      `Native Dart Sass binary not found at ${nativeSass}. ` +
      `Ensure "${pkg}" is installed (it is an optional dependency of sass-embedded).`
    );
  }

  const binDir = path.join(nodeModules, '.bin');
  const linkPath = path.join(binDir, 'sass');

  // Remove existing entry (JS wrapper or stale symlink).
  // Use lstat to detect broken symlinks that existsSync would miss.
  try {
    fs.lstatSync(linkPath);
    fs.unlinkSync(linkPath);
  } catch {
    // linkPath does not exist -- nothing to remove
  }

  // Create relative symlink so it stays valid if the repo is moved
  const relTarget = path.relative(binDir, nativeSass);
  fs.symlinkSync(relTarget, linkPath);

  console.log(`Linked native Dart Sass: node_modules/.bin/sass -> ${relTarget}`);
}

try {
  main();
} catch (err) {
  console.error(`link-dart-sass: ${err.message}`);
  process.exit(1);
}
