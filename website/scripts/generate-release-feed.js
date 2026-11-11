// @ts-check
// Generates a deterministic RSS 2.0 feed of OCM canonical releases.
//
// Usage:
//   node scripts/generate-release-feed.js \
//     --input <path-to-gh-api-paginated-json> \
//     --output <output-xml-path> \
//     --since  <ISO-UTC-timestamp>
//
// The input file must be the nested-array format produced by:
//   gh api --paginate --slurp "repos/open-component-model/open-component-model/releases?per_page=100"
//
// The output directory must already exist.

"use strict";

const { readFileSync, writeFileSync } = require("node:fs");
const { parseArgs } = require("node:util");

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const REPO_URL = "https://github.com/open-component-model/open-component-model";
const RELEASES_URL = `${REPO_URL}/releases`;
const RELEASE_TAG_URL = `${RELEASES_URL}/tag/`;
const FEED_SELF_URL = "https://ocm.software/releases.xml";

/** Pattern for canonical OCM release tags: v1.2.3 or v1.2.3-rc.4 */
const CANONICAL_TAG_RE = /^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$/;

// ---------------------------------------------------------------------------
// XML helpers
// ---------------------------------------------------------------------------

/**
 * Escape a string for use in XML text content or attribute values.
 * @param {string} s
 * @returns {string}
 */
function escapeXml(s) {
    return s
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&apos;");
}

// ---------------------------------------------------------------------------
// Feed renderer (exported for tests)
// ---------------------------------------------------------------------------

/**
 * @typedef {{ tag_name: string, draft: boolean, published_at: string | null, prerelease: boolean, [k: string]: unknown }} ReleaseRecord
 */

/**
 * Render a deterministic RSS 2.0 feed string from paginated GitHub release records.
 *
 * @param {ReleaseRecord[][]} pages  Nested-array output of `gh api --paginate --slurp`.
 * @param {string}            since  Inclusive ISO UTC lower bound for published_at.
 * @returns {string}                 Complete UTF-8 RSS document.
 */
function renderReleaseFeed(pages, since) {
    // --- Validate inputs ---
    if (!Array.isArray(pages)) {
        throw new TypeError("pages must be an array");
    }
    for (const page of pages) {
        if (!Array.isArray(page)) {
            throw new TypeError("each page must be an array; got a non-array member");
        }
    }

    const sinceMs = Date.parse(since);
    if (!Number.isFinite(sinceMs)) {
        throw new RangeError(`invalid since timestamp: ${since}`);
    }

    // --- Collect and filter records ---

    /** @type {Map<string, { publishedMs: number, tag: string }>} */
    const seen = new Map();

    for (const page of pages) {
        for (const record of page) {
            if (typeof record !== "object" || record === null || typeof record.tag_name !== "string") {
                throw new TypeError("malformed release record: missing tag_name");
            }
            if (typeof record.draft !== "boolean") {
                throw new TypeError(`malformed release record for ${record.tag_name}: draft must be boolean`);
            }
            if (typeof record.published_at !== "string" && record.published_at !== null) {
                throw new TypeError(`malformed release record for ${record.tag_name}: published_at must be string or null`);
            }

            const tag = record.tag_name;

            // Filter: canonical tag shape, not a draft, has a publication date
            if (!CANONICAL_TAG_RE.test(tag)) {
                continue;
            }
            if (record.draft) {
                continue;
            }
            if (record.published_at === null) {
                continue;
            }

            const publishedMs = Date.parse(record.published_at);
            if (!Number.isFinite(publishedMs)) {
                throw new RangeError(`invalid published_at for eligible record ${tag}: ${record.published_at}`);
            }

            // Filter: before cutoff
            if (publishedMs < sinceMs) {
                continue;
            }

            // Deduplication / conflict detection
            if (seen.has(tag)) {
                const existing = /** @type {{ publishedMs: number }} */ (seen.get(tag));
                if (existing.publishedMs !== publishedMs) {
                    throw new RangeError(`conflicting published_at dates for tag ${tag}`);
                }
                // Identical: skip duplicate
                continue;
            }

            seen.set(tag, { publishedMs, tag });
        }
    }

    // --- Sort: newest publication first; equal timestamps → ascending ASCII tag ---
    const items = [...seen.values()].sort((a, b) => {
        if (b.publishedMs !== a.publishedMs) {
            return b.publishedMs - a.publishedMs;
        }
        return a.tag < b.tag ? -1 : 1;
    });

    // --- Render RSS ---
    const itemsXml = items.map(({ tag, publishedMs }) => {
        const url = `${RELEASE_TAG_URL}${escapeXml(tag)}`;
        const pubDate = escapeXml(new Date(publishedMs).toUTCString());
        const title = escapeXml(`OCM ${tag} released`);
        const desc = escapeXml(`OCM ${tag} is available. Release notes and downloads: ${RELEASE_TAG_URL}${tag}`);
        return (
            `    <item>\n` +
            `      <title>${title}</title>\n` +
            `      <link>${url}</link>\n` +
            `      <guid isPermaLink="true">${url}</guid>\n` +
            `      <pubDate>${pubDate}</pubDate>\n` +
            `      <description>${desc}</description>\n` +
            `    </item>`
        );
    }).join("\n");

    return (
        `<?xml version="1.0" encoding="UTF-8"?>\n` +
        `<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">\n` +
        `  <channel>\n` +
        `    <title>OCM Releases</title>\n` +
        `    <link>${escapeXml(RELEASES_URL)}</link>\n` +
        `    <description>Release candidates and final releases of Open Component Model.</description>\n` +
        `    <language>en</language>\n` +
        `    <atom:link href="${escapeXml(FEED_SELF_URL)}" rel="self" type="application/rss+xml"/>\n${
            itemsXml ? `${itemsXml}\n` : ``
        }  </channel>\n` +
        `</rss>\n`
    );
}

// ---------------------------------------------------------------------------
// CLI entrypoint
// ---------------------------------------------------------------------------

async function main() {
    let values;
    try {
        ({ values } = parseArgs({
            args: process.argv.slice(2),
            options: {
                input: { type: "string" },
                output: { type: "string" },
                since: { type: "string" }
            },
            strict: true,
            allowPositionals: false
        }));
    } catch (e) {
        throw new Error(`argument error: ${/** @type {Error} */ (e).message}`, { cause: e });
    }

    if (!values.input)  {
        throw new Error("--input <path> is required");
    }
    if (!values.output) {
        throw new Error("--output <path> is required");
    }
    if (!values.since)  {
        throw new Error("--since <ISO timestamp> is required");
    }

    let raw;
    try {
        raw = readFileSync(values.input, "utf8");
    } catch (e) {
        throw new Error(`cannot read input file ${values.input}: ${/** @type {Error} */ (e).message}`, { cause: e });
    }

    let pages;
    try {
        pages = JSON.parse(raw);
    } catch (e) {
        throw new Error(`input file is not valid JSON: ${/** @type {Error} */ (e).message}`, { cause: e });
    }

    // renderReleaseFeed validates shape and throws with a descriptive message
    const xml = renderReleaseFeed(pages, values.since);

    // Write only after successful render; leave any pre-existing output untouched on error
    writeFileSync(values.output, xml, "utf8");
    console.log(`Release feed written: ${values.output} (${xml.length} bytes, ${pages.flat().length} records scanned)`);
}

if (require.main === module) {
    main().catch((e) => {
        console.error(`[ERROR] ${e.message || String(e)}`);
        process.exit(1);
    });
}

module.exports = { renderReleaseFeed };
