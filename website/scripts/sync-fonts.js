#!/usr/bin/env node
/**
 * Copy the variable-font woff2 files shipped by @fontsource-variable/{inter,
 * quicksand} into static/fonts/vendor/ so Hugo serves them at stable URLs.
 *
 * Runs automatically via the "postinstall" npm lifecycle script so Renovate
 * PRs that bump @fontsource-variable/* refresh the committed binaries -
 * commit the resulting diff.
 *
 * Latin subsets only; matches the @font-face rules in assets/scss/common/
 * _fonts.scss and keeps the payload lean.
 */

const fs = require('node:fs');
const path = require('node:path');

const ROOT = path.resolve(__dirname, '..');

const SETS = [
  {
    pkg: 'inter',
    files: [
      'inter-latin-wght-normal.woff2',
      'inter-latin-ext-wght-normal.woff2',
    ],
  },
  {
    pkg: 'quicksand',
    files: [
      'quicksand-latin-wght-normal.woff2',
      'quicksand-latin-ext-wght-normal.woff2',
    ],
  },
];

function main() {
  for (const { pkg, files } of SETS) {
    const srcDir = path.join(ROOT, 'node_modules', '@fontsource-variable', pkg, 'files');
    const dstDir = path.join(ROOT, 'static', 'fonts', 'vendor', pkg);

    fs.mkdirSync(dstDir, { recursive: true });

    for (const file of files) {
      const src = path.join(srcDir, file);
      const dst = path.join(dstDir, file);
      if (!fs.existsSync(src)) {
        throw new Error(
          `Font file not found at ${src}. ` +
          `Ensure "@fontsource-variable/${pkg}" is installed.`
        );
      }
      fs.copyFileSync(src, dst);
    }
  }

  console.log('sync-fonts: copied Inter + Quicksand variable woff2 files into static/fonts/vendor/');
}

try {
  main();
} catch (err) {
  console.error(`sync-fonts: ${err.message}`);
  process.exit(1);
}
