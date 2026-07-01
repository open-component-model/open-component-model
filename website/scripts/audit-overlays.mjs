#!/usr/bin/env node
// Audit website/layouts/ for drift against @thulite/doks-core upstream.
// See website/CONTRIBUTING.md "Theme overlays" for the workflow.
//
// Usage: audit-overlays.mjs [-v|--verbose]
//        npm run audit:overlays [-v|--verbose]
// Exit non-zero on DRIFT, REMOVED, or ERROR.

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const PKG = '@thulite/doks-core';
const TAG_RE = /Based on:\s*@thulite\/doks-core@(\d+\.\d+\.\d+)\s*\(path:\s*(\S+?)\s*\)/;

const WEBSITE = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const LAYOUTS = join(WEBSITE, 'layouts');
const INSTALLED = join(WEBSITE, 'node_modules', '@thulite', 'doks-core');
const CACHE = join(homedir(), '.cache', 'ocm-overlay-audit');
const VERBOSE = process.argv.slice(2).some((a) => a === '-v' || a === '--verbose');

function readOrNull(path) {
  try { return readFileSync(path); }
  catch (e) { if (e.code === 'ENOENT') return null; throw e; }
}

function* walkLayouts(dir) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) yield* walkLayouts(p);
    else if (e.isFile() && (p.endsWith('.html') || p.endsWith('.xml'))) yield p;
  }
}

function fetchUpstream(version) {
  const dir = join(CACHE, version, 'package');
  if (existsSync(dir)) return dir;
  mkdirSync(join(CACHE, version), { recursive: true });
  const stage = mkdtempSync(join(tmpdir(), 'ocm-audit-'));
  try {
    const tarball = execFileSync('npm', ['pack', `${PKG}@${version}`, '--silent'],
      { cwd: stage, encoding: 'utf8' }).trim().split('\n').pop();
    execFileSync('tar', ['-xzf', join(stage, tarball), '-C', join(CACHE, version)]);
  } finally {
    rmSync(stage, { recursive: true, force: true });
  }
  return dir;
}

// `git diff --no-index` emits a/<path> and b/<path> headers using the actual
// paths it was given, so staging the files under version-named dirs gives us
// readable headers without post-processing.
function unifiedDiff(upstreamPath, recVer, instVer, recBuf, instBuf) {
  const stage = mkdtempSync(join(tmpdir(), 'ocm-audit-diff-'));
  const a = join(recVer, upstreamPath);
  const b = join(instVer, upstreamPath);
  mkdirSync(join(stage, dirname(a)), { recursive: true });
  mkdirSync(join(stage, dirname(b)), { recursive: true });
  writeFileSync(join(stage, a), recBuf);
  writeFileSync(join(stage, b), instBuf);
  try {
    return execFileSync('git', ['--no-pager', 'diff', '--no-index', '--no-color', '--', a, b],
      { cwd: stage, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] });
  } catch (e) {
    return e.stdout ?? '';
  } finally {
    rmSync(stage, { recursive: true, force: true });
  }
}

function classify(shadow, instVer) {
  const { shadowPath, recVer, upstreamPath } = shadow;
  const id = { shadow: relative(WEBSITE, shadowPath), upstreamPath, recVer };

  if (upstreamPath.includes('..') || !upstreamPath.startsWith('layouts/')) {
    return { ...id, status: 'ERROR', detail: 'path must be under layouts/ — fix the tag' };
  }

  let recRoot;
  try {
    recRoot = fetchUpstream(recVer);
  } catch (e) {
    return { ...id, status: 'ERROR', detail: `failed to fetch ${PKG}@${recVer} (${e.message.split('\n', 1)[0]})` };
  }

  const recBuf = readOrNull(join(recRoot, upstreamPath));
  if (!recBuf) return { ...id, status: 'ERROR', detail: `path not found in ${PKG}@${recVer} — fix the tag` };

  const instBuf = readOrNull(join(INSTALLED, upstreamPath));
  if (!instBuf) return { ...id, status: 'REMOVED', detail: `path no longer exists in ${PKG}@${instVer}` };

  if (recBuf.equals(instBuf)) {
    return recVer === instVer
      ? { ...id, status: 'OK' }
      : { ...id, status: 'STALE_TAG', detail: `bump tag to @${instVer} — upstream content unchanged` };
  }

  return {
    ...id,
    status: 'DRIFT',
    detail: `upstream changed between @${recVer} and @${instVer}`,
    diff: unifiedDiff(upstreamPath, recVer, instVer, recBuf, instBuf),
  };
}

function findShadows() {
  const shadows = [];
  for (const path of walkLayouts(LAYOUTS)) {
    const m = readFileSync(path, 'utf8').slice(0, 4096).match(TAG_RE);
    if (m) shadows.push({ shadowPath: path, recVer: m[1], upstreamPath: m[2] });
  }
  return shadows;
}

function reportSection(status, items) {
  if (!items?.length || (status === 'OK' && !VERBOSE)) return;
  console.log(`── ${status} (${items.length}) ──`);
  for (const r of items) {
    console.log(`  ${r.shadow}`);
    console.log(`    upstream: ${r.upstreamPath}  recorded: @${r.recVer}`);
    if (r.detail) console.log(`    ${r.detail}`);
    if (r.diff) for (const line of r.diff.split('\n')) console.log(`    │ ${line}`);
  }
  console.log('');
}

function main() {
  const installedPkg = readOrNull(join(INSTALLED, 'package.json'));
  if (!installedPkg) {
    console.error(`error: ${PKG} not installed at ${INSTALLED}. Run \`npm install\` in website/.`);
    process.exit(2);
  }
  const instVer = JSON.parse(installedPkg).version;

  const shadows = findShadows();
  if (!shadows.length) {
    console.log('No tagged shadows found. See CONTRIBUTING.md "Theme overlays" to register one.');
    return;
  }

  console.log(`Auditing ${shadows.length} shadow(s) against ${PKG}@${instVer}\n`);
  const results = shadows.map((s) => classify(s, instVer));
  const grouped = Object.groupBy(results, (r) => r.status);

  const order = ['DRIFT', 'REMOVED', 'ERROR', 'STALE_TAG', 'OK'];
  for (const status of order) reportSection(status, grouped[status]);

  const summary = order.filter((s) => grouped[s]?.length).map((s) => `${grouped[s].length} ${s.toLowerCase()}`).join(', ');
  console.log(`Summary: ${summary}`);

  const fail = (grouped.DRIFT?.length ?? 0) + (grouped.REMOVED?.length ?? 0) + (grouped.ERROR?.length ?? 0);
  process.exit(fail > 0 ? 1 : 0);
}

main();
