#!/usr/bin/env node
/**
 * Verify embedded page resources in the built Hugo site.
 *
 * Usage:
 *   node scripts/check-page-resources.js [publicDir] [--ignore <regex> ...]
 *
 * Why this exists (https://github.com/open-component-model/ocm-project/issues/1160):
 * The link verifier (linkspector) walks clickable <a> links and confirms the
 * target page returns 200. It does NOT verify the resources a page embeds
 * while it renders: images, stylesheets, scripts, and - importantly - the
 * schema files that the `schema-renderer` shortcode fetches client-side
 * through a `data-schema-url` attribute. When the versioned website rewrites a
 * resource path (for example `/0.9/schemas/...`) and the file is not published
 * there, the page still returns 200, so linkspector stays green while the
 * browser fetch returns 404 and the user sees a broken image or empty schema
 * block.
 *
 * This checker closes that gap. It walks the built output directory, matches
 * each HTML tag, extracts the embedded-resource attributes that are meaningful
 * for that tag (see RESOURCE_ATTRS), and fails if the referenced file does not
 * exist in the build. It is deterministic (filesystem only, no network) and
 * runs against the same artifact that gets published.
 *
 * Scope and assumptions:
 * - Checked: embedded resources only - `src` (any element), `href` on <link>,
 *   <use> and <image>, `poster` on <video>, `data` on <object>, `srcset` on
 *   <img>/<source>, and the custom `data-schema-url`, `data-src`,
 *   `data-srcset` attributes on any element.
 * - Not checked: navigational links (<a href>, <area href>) - that is the link
 *   verifier's job, and frozen/legacy versions contain pre-existing broken
 *   navigation that must not fail this job. Also out of scope: CSS `url()`,
 *   inline `style` attributes and SVG `xlink:href` sprite references.
 * - Only tags are parsed, so text and <code> content is never scanned.
 *   Comments and <script>/<style> bodies are stripped first.
 * - Run it against a MINIFIED build (`npm run build`, hugo --minify), as the
 *   CI workflow does; other output is not supported.
 * - Use `--ignore <regex>` (repeatable) to skip known-dynamic URLs.
 */

const fs = require('node:fs');
const path = require('node:path');

// Embedded-resource attributes. `tags: null` means any element. Keys are
// lowercase. `data-schema-url` is the custom attribute emitted by the
// schema-renderer shortcode; a stock link checker never looks at it.
// `href` is deliberately limited to resource-bearing elements so that
// navigational <a href>/<area href> are never reported.
const RESOURCE_ATTRS = {
    src: { tags: null, srcset: false },
    href: { tags: new Set(['link', 'use', 'image']), srcset: false },
    poster: { tags: new Set(['video']), srcset: false },
    data: { tags: new Set(['object']), srcset: false },
    srcset: { tags: new Set(['img', 'source']), srcset: true },
    'data-schema-url': { tags: null, srcset: false },
    'data-src': { tags: null, srcset: false },
    'data-srcset': { tags: null, srcset: true },
};

/**
 * Decide whether a URL points at something inside this site that maps to a
 * file in the build output. External, protocol-relative, fragment-only and
 * non-http scheme URLs (mailto:, tel:, data:, javascript:) are skipped - those
 * are either not our files or handled by the external link checker.
 */
function isInternalUrl(url) {
    if (!url) {
        return false;
    }
    const value = url.trim();
    if (value === '') {
        return false;
    }
    if (value.startsWith('#')) {
        return false; // in-page anchor
    }
    if (value.startsWith('//')) {
        return false; // protocol-relative (external)
    }
    if (/^[a-z][a-z0-9+.-]*:/i.test(value)) {
        return false; // has a scheme
    }
    return true;
}

/** Strip query string and fragment, then percent-decode. */
function normalizeUrl(url) {
    let value = url.trim();
    const hash = value.indexOf('#');
    if (hash !== -1) {
        value = value.slice(0, hash);
    }
    const query = value.indexOf('?');
    if (query !== -1) {
        value = value.slice(0, query);
    }
    try {
        return decodeURIComponent(value);
    } catch {
        return value; // leave malformed encodings as-is
    }
}

/**
 * Map an internal URL to the candidate filesystem paths that would satisfy it.
 * Root-relative URLs resolve against publicDir; relative URLs resolve against
 * the directory of the referencing page. Extensionless and trailing-slash URLs
 * also try Hugo's pretty-URL `index.html` form. A URL that resolves outside
 * publicDir yields no candidates (it can never be a published file); callers
 * distinguish that from an empty URL via normalizeUrl.
 */
function resolveCandidates(url, htmlFile, publicDir) {
    const clean = normalizeUrl(url);
    if (clean === '') {
        return [];
    }

    const root = path.resolve(publicDir);
    const base = clean.startsWith('/')
        ? path.join(root, clean)
        : path.resolve(path.dirname(htmlFile), clean);

    const rel = path.relative(root, base);
    if (rel === '..' || rel.startsWith(`..${path.sep}`) || path.isAbsolute(rel)) {
        return [];
    }

    if (clean.endsWith('/')) {
        return [path.join(base, 'index.html')];
    }
    return [base, path.join(base, 'index.html'), `${base}.html`];
}

/**
 * Pull the URL out of each srcset candidate, following the HTML tokenizing
 * rules: a URL is a run of non-whitespace characters, so commas inside a
 * `data:` URI are kept. Only trailing commas terminate a URL that has no
 * descriptor; otherwise descriptors run up to the next comma.
 */
function parseSrcset(value) {
    const urls = [];
    let i = 0;
    while (i < value.length) {
        while (i < value.length && /[\s,]/.test(value[i])) {
            i += 1;
        }
        const start = i;
        while (i < value.length && !/\s/.test(value[i])) {
            i += 1;
        }
        let url = value.slice(start, i);
        if (url === '') {
            break;
        }
        if (url.endsWith(',')) {
            url = url.replace(/,+$/, '');
        } else {
            while (i < value.length && value[i] !== ',') {
                i += 1;
            }
        }
        if (url !== '') {
            urls.push(url);
        }
    }
    return urls;
}

/**
 * Remove regions whose text must not be scanned: the bodies of <script> and
 * <style> (inline JS/CSS and JSON-LD) first, then HTML comments. Script and
 * style go first so that a `<!--` inside inline JS cannot swallow real markup.
 * The opening and closing tags are kept so a real `<script src=...>` is still
 * extracted.
 */
function stripNonMarkup(html) {
    return html
        .replace(/(<(script|style)\b[^>]*>)[\s\S]*?(<\/\2\s*>)/gi, '$1$3')
        .replace(/<!--[\s\S]*?-->/g, '');
}

// One start tag: name, then attribute text with quoted values kept intact so a
// `>` inside a quoted value does not end the tag.
const TAG_PATTERN = /<([a-zA-Z][a-zA-Z0-9:-]*)((?:"[^"]*"|'[^']*'|[^'">])*)>/g;

// One attribute inside a tag. The lookbehind keeps the boundary character
// (whitespace, quote or slash) unconsumed so that `hugo --minify` output, which
// drops the space between a quoted value and the next attribute
// (`<img alt="x"src="/logo.png">`), still yields the second attribute.
const ATTRIBUTE_PATTERN =
    /(?<=[\s"'/])([^\s"'<>/=]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))/g;

/**
 * Extract embedded-resource references from a chunk of HTML. Returns
 * `{ tag, attr, url }` entries in document order. Each tag is matched first
 * and its attributes are filtered by RESOURCE_ATTRS, so text content (for
 * example inside <code>) and navigational links are never reported.
 */
function extractResourceRefs(html) {
    const refs = [];
    const markup = stripNonMarkup(html);

    for (const tagMatch of markup.matchAll(TAG_PATTERN)) {
        const tag = tagMatch[1].toLowerCase();
        for (const attrMatch of tagMatch[2].matchAll(ATTRIBUTE_PATTERN)) {
            const attr = attrMatch[1].toLowerCase();
            const rule = RESOURCE_ATTRS[attr];
            if (!rule || (rule.tags && !rule.tags.has(tag))) {
                continue;
            }
            const value = attrMatch[2] ?? attrMatch[3] ?? attrMatch[4] ?? '';
            const urls = rule.srcset ? parseSrcset(value) : [value];
            for (const url of urls) {
                refs.push({ tag, attr, url });
            }
        }
    }

    return refs;
}

/** Recursively collect every *.html file under dir. */
function findHtmlFiles(dir) {
    const out = [];
    const entries = fs.readdirSync(dir, { withFileTypes: true })
        .sort((x, y) => (x.name < y.name ? -1 : x.name > y.name ? 1 : 0));
    for (const entry of entries) {
        const full = path.join(dir, entry.name);
        if (entry.isDirectory()) {
            out.push(...findHtmlFiles(full));
        } else if (entry.isFile() && full.toLowerCase().endsWith('.html')) {
            out.push(full);
        }
    }
    return out;
}

function fileExists(file) {
    try {
        return fs.statSync(file).isFile();
    } catch {
        return false;
    }
}

/**
 * Check one built site directory. Returns a report with the list of missing
 * references, each annotated with the page that embeds it.
 */
function checkPublicDir(publicDir, options = {}) {
    const ignore = options.ignore ?? [];
    const htmlFiles = findHtmlFiles(publicDir);

    const missing = [];
    let checked = 0;

    for (const htmlFile of htmlFiles) {
        const html = fs.readFileSync(htmlFile, 'utf8');
        const refs = extractResourceRefs(html);
        const seen = new Set();

        for (const { attr, url } of refs) {
            if (!isInternalUrl(url)) {
                continue;
            }
            if (ignore.some((re) => re.test(url))) {
                continue;
            }

            const key = `${attr}\u0000${normalizeUrl(url)}`;
            if (seen.has(key)) {
                continue;
            }
            seen.add(key);

            if (normalizeUrl(url) === '') {
                continue; // query/fragment only
            }

            checked += 1;
            const candidates = resolveCandidates(url, htmlFile, publicDir);
            if (!candidates.some(fileExists)) {
                missing.push({
                    page: path.relative(publicDir, htmlFile),
                    attr,
                    url,
                    tried: candidates.map((c) => path.relative(publicDir, c)),
                });
            }
        }
    }

    missing.sort((x, y) => (
        x.page.localeCompare(y.page) || x.attr.localeCompare(y.attr) || x.url.localeCompare(y.url)
    ));

    return { pages: htmlFiles.length, checked, missing };
}

function parseArguments(argv) {
    const args = { publicDir: 'public', ignore: [] };
    for (let i = 0; i < argv.length; i += 1) {
        const arg = argv[i];
        if (arg === '--ignore') {
            const pattern = argv[i + 1];
            if (!pattern) {
                throw new Error('--ignore requires a regex argument');
            }
            args.ignore.push(new RegExp(pattern));
            i += 1;
        } else if (!arg.startsWith('-')) {
            args.publicDir = arg;
        } else {
            throw new Error(`unknown argument: ${arg}`);
        }
    }
    return args;
}

function main(argv) {
    const { publicDir, ignore } = parseArguments(argv);
    const resolved = path.resolve(publicDir);

    if (!fs.existsSync(resolved) || !fs.statSync(resolved).isDirectory()) {
        console.error(`error: build output directory not found: ${resolved}`);
        console.error('Run `npm run build` first, or pass the correct path.');
        process.exit(2);
    }

    const { pages, checked, missing } = checkPublicDir(resolved, { ignore });

    console.log(`Scanned ${pages} page(s), checked ${checked} internal resource reference(s).`);

    if (missing.length === 0) {
        console.log('All checked embedded resource references resolve to a published file.');
        return;
    }

    console.error(`\nFound ${missing.length} missing resource reference(s):\n`);
    for (const m of missing) {
        console.error(`  page:  ${m.page}`);
        console.error(`  ${m.attr}: ${m.url}`);
        console.error(`  tried: ${m.tried.length > 0 ? m.tried.join(', ') : '(path resolves outside the build directory)'}`);
        console.error('');
    }
    process.exit(1);
}

if (require.main === module) {
    try {
        main(process.argv.slice(2));
    } catch (err) {
        console.error(`error: ${err.message}`);
        process.exit(2);
    }
}

module.exports = {
    isInternalUrl,
    normalizeUrl,
    resolveCandidates,
    parseSrcset,
    stripNonMarkup,
    extractResourceRefs,
    findHtmlFiles,
    checkPublicDir,
    parseArguments,
};
