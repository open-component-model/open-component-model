#!/usr/bin/env node
/**
 * Bundle scripts/mermaid-init.src.js into assets/js/mermaid-init.js.
 *
 * WHY THIS SCRIPT EXISTS
 * ----------------------
 * The source file at scripts/mermaid-init.src.js contains
 *     import mermaid from 'mermaid';
 * which is a "bare specifier" - only a bundler (or an import map) can
 * resolve it to a URL the browser will accept.
 *
 * doks-core's stock layouts/_partials/footer/script-footer.html serves
 * assets/js/mermaid-init.js via `<script type="module" src=...>` with no
 * bundling step. Without the pre-bundle done here, the browser would fetch
 * a URL literally called "mermaid" from the site root and fail.
 *
 * Bundling the source into a self-contained ESM module means the stock
 * doks-core partial works verbatim - no need for a project-level override
 * of script-footer.html, and therefore no risk of that override silently
 * drifting from upstream on doks-core bumps.
 *
 * WHY THIS RUNS FROM postinstall
 * ------------------------------
 * The bundle depends on node_modules/mermaid/. Regenerating on every
 * `npm install` / `npm ci` keeps the shipped bundle version-locked to
 * package-lock.json, so Renovate bumps of `mermaid` produce a fresh
 * bundle in CI (and locally) without any manual step.
 *
 * WHY THE OUTPUT IS GITIGNORED
 * ----------------------------
 * assets/js/mermaid-init.js is a build product - regenerated on every
 * install. Committing it would either produce noisy diffs on every run
 * (esbuild output is not byte-stable across versions) or require a CI
 * check to enforce "did you rerun the bundler?". Gitignoring it makes
 * the source of truth unambiguous: edit scripts/mermaid-init.src.js.
 */

const path = require('node:path');
const esbuild = require('esbuild');

const ROOT = path.resolve(__dirname, '..');

esbuild.buildSync({
  entryPoints: [path.join(ROOT, 'scripts', 'mermaid-init.src.js')],
  outfile: path.join(ROOT, 'assets', 'js', 'mermaid-init.js'),
  bundle: true,
  // ESM format matches the <script type="module"> tag doks-core emits.
  format: 'esm',
  // es2015 matches the target used by doks-core's footer/esbuild partial for
  // consistency across the site's bundled JS.
  target: 'es2015',
  minify: true,
  legalComments: 'none',
});

console.log('bundle-mermaid: wrote assets/js/mermaid-init.js');
