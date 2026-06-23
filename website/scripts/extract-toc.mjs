#!/usr/bin/env node
/**
 * Pre-build script: extract heading structure from Hugo markdown content files
 * (including headings inside {{< tab >}} and {{< steps >}} shortcodes) and
 * write it to data/toc.json so Hugo's TOC partial can render a complete,
 * document-order table of contents without JavaScript.
 *
 * Run before Hugo: npm run build  (see "prebuild" in package.json).
 */

import { readFileSync, writeFileSync, readdirSync, mkdirSync } from 'fs';
import { join, relative, extname, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, '..');
const CONTENT_DIR = join(ROOT, 'content', 'docs');
const OUTPUT_FILE = join(ROOT, 'data', 'toc.json');

// Read the same heading level bounds Hugo uses for .TableOfContents so the
// pre-built TOC respects config/_default/markup.yaml tableOfContents settings.
function readTocLevels() {
  try {
    const yaml = readFileSync(join(ROOT, 'config/_default/markup.yaml'), 'utf8');
    const start = yaml.match(/startLevel:\s*(\d+)/)?.[1];
    const end   = yaml.match(/endLevel:\s*(\d+)/)?.[1];
    return { start: start ? parseInt(start) : 2, end: end ? parseInt(end) : 3 };
  } catch {
    return { start: 2, end: 3 };
  }
}
const TOC_LEVELS = readTocLevels();

// ---------------------------------------------------------------------------
// Slug generation — must match Goldmark's anchorize output for ASCII content.
// ---------------------------------------------------------------------------
function slugify(text) {
  return text
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '') // strip combining diacriticals
    .replace(/[^\w\s-]/g, '')         // keep word chars, spaces, hyphens
    .replace(/\s+/g, '-')             // spaces → hyphens
    .replace(/-+/g, '-')              // collapse runs
    .replace(/^-+|-+$/g, '');         // trim
}

// Strip inline markdown formatting so heading text is plain.
function stripInline(text) {
  return text
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/\*([^*]+)\*/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/<!--.*?-->/g, '')
    .trim();
}

// ---------------------------------------------------------------------------
// Heading extraction
// ---------------------------------------------------------------------------
const HEADING_RE = /^(#{2,6})\s+(.+?)(?:\s*\{[^}]*\})?\s*$/gm;

function extractPageHeadings(text, out) {
  HEADING_RE.lastIndex = 0;
  let m;
  while ((m = HEADING_RE.exec(text)) !== null) {
    const level = m[1].length;
    if (level < TOC_LEVELS.start || level > TOC_LEVELS.end) continue;
    const raw = stripInline(m[2]);
    if (!raw) continue;
    out.push({ kind: 'page', level, id: slugify(raw), text: raw });
  }
}

function extractTabHeadings(tabContent, tabName, seen, out) {
  out.push({ kind: 'group', name: tabName });
  HEADING_RE.lastIndex = 0;
  let m;
  while ((m = HEADING_RE.exec(tabContent)) !== null) {
    const level = m[1].length;
    if (level < TOC_LEVELS.start || level > TOC_LEVELS.end) continue;
    const raw = stripInline(m[2]);
    if (!raw) continue;

    const baseId = slugify(raw);
    let id = baseId;

    // Replicate tabs.html dedup: first occurrence keeps plain ID;
    // later occurrences in other tabs get -tabslug suffix.
    if (id in seen) {
      const tabSlug = slugify(tabName);
      id = `${baseId}-${tabSlug}`;
      let n = 1;
      while (id in seen) id = `${baseId}-${tabSlug}-${n++}`;
    }
    seen[id] = tabName;

    out.push({ kind: 'tab', level, id, text: raw });
  }
}

// ---------------------------------------------------------------------------
// Per-file parsing: returns flat item list in document order.
// ---------------------------------------------------------------------------
function parseFile(rawContent) {
  // Strip YAML/TOML front matter.
  const body = rawContent.replace(/^---[\s\S]*?---\r?\n/, '');

  const raw = [];   // raw items: {kind, level?, id?, text?, name?}
  const seen = {};  // cross-tab dedup: id → first tab name

  // Walk {{< tabs >}}…{{< /tabs >}} blocks in order.
  const TABS_RE = /\{\{<\s*tabs(?:\s[^>]*)?\s*>\}\}([\s\S]*?)\{\{<\s*\/tabs\s*>\}\}/g;
  const TAB_RE  = /\{\{<\s*tab\s+"([^"]+)"(?:\s[^>]*)?\s*>\}\}([\s\S]*?)\{\{<\s*\/tab\s*>\}\}/g;

  let cursor = 0;
  let m;
  while ((m = TABS_RE.exec(body)) !== null) {
    extractPageHeadings(body.slice(cursor, m.index), raw);

    TAB_RE.lastIndex = 0;
    let t;
    while ((t = TAB_RE.exec(m[1])) !== null) {
      extractTabHeadings(t[2], t[1], seen, raw);
    }

    cursor = m.index + m[0].length;
  }
  extractPageHeadings(body.slice(cursor), raw);

  if (raw.length === 0) return [];

  // Compute min heading level to normalise depth to 0-based.
  const minLevel = raw
    .filter(i => i.kind === 'page' || i.kind === 'tab')
    .reduce((min, i) => Math.min(min, i.level), 6);

  // Convert to final flat format with pre-computed depth.
  return raw.map(item => {
    if (item.kind === 'group') return { label: item.name, depth: 0 };
    const extra = item.kind === 'tab' ? 1 : 0;
    return { id: item.id, text: item.text, depth: item.level - minLevel + extra };
  });
}

// ---------------------------------------------------------------------------
// File discovery
// ---------------------------------------------------------------------------
function collectMarkdownFiles(dir) {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, e.name);
    if (e.isDirectory()) out.push(...collectMarkdownFiles(full));
    else if (e.isFile() && extname(e.name) === '.md') out.push(full);
  }
  return out;
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------
const files = collectMarkdownFiles(CONTENT_DIR);
const result = {};
let count = 0;

for (const file of files) {
  const items = parseFile(readFileSync(file, 'utf8'));
  if (items.length === 0) continue;

  // Key matches Hugo's .File.Path (relative to content/, forward slashes).
  const key = relative(join(ROOT, 'content'), file).replace(/\\/g, '/');
  result[key] = items;
  count++;
}

mkdirSync(join(ROOT, 'data'), { recursive: true });
writeFileSync(OUTPUT_FILE, JSON.stringify(result, null, 2), 'utf8');
console.log(`extract-toc: wrote ${count} pages → data/toc.json`);
