// Tests for generate-release-feed.js
//
// Run: `npm test` or `node --test scripts/generate-release-feed.test.js`

"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const { mkdtempSync, writeFileSync, readFileSync, rmSync } = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const { renderReleaseFeed } = require("./generate-release-feed");

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

const SINCE = "2026-11-11T00:00:00Z";

/** Canonical URL prefix for release GUIDs and links */
const PREFIX = "https://github.com/open-component-model/open-component-model/releases/tag/";

/**
 * Minimal record factory. Only fields the generator inspects.
 * @param {string} tag
 * @param {string | null} published_at
 * @param {{ draft?: boolean, prerelease?: boolean }} [opts]
 * @returns {object}
 */
function rec(tag, published_at, { draft = false, prerelease = tag.includes("-rc.") } = {}) {
    return { tag_name: tag, draft, published_at, prerelease };
}

/** Fixture from the canonical testdata file (two pages) */
const FIXTURE_PAGES = JSON.parse(
    readFileSync(path.join(__dirname, "testdata", "release-feed.json"), "utf8")
);

// ---------------------------------------------------------------------------
// Helpers to parse GUIDs from rendered XML quickly without a full parser
// ---------------------------------------------------------------------------

/**
 * Extract text content of every <guid> element, in document order.
 * @param {string} xml
 * @returns {string[]}
 */
function extractGuids(xml) {
    const re = /<guid[^>]*>([^<]+)<\/guid>/g;
    const results = [];
    let m;
    while ((m = re.exec(xml)) !== null) {
        results.push(m[1]);
    }
    return results;
}

/**
 * Extract <link> values that appear inside <item> elements only.
 * @param {string} xml
 * @returns {string[]}
 */
function extractItemLinks(xml) {
    const results = [];
    const itemRe = /<item>([\s\S]*?)<\/item>/g;
    const linkRe = /<link>([^<]+)<\/link>/;
    let im;
    while ((im = itemRe.exec(xml)) !== null) {
        const lm = linkRe.exec(im[1]);
        if (lm) {
            results.push(lm[1]);
        }
    }
    return results;
}

// ---------------------------------------------------------------------------
// Core fixture: exact ordered GUIDs
// ---------------------------------------------------------------------------

test("fixture: produces correct three items in newest-first order", () => {
    const xml = renderReleaseFeed(FIXTURE_PAGES, SINCE);
    const guids = extractGuids(xml);
    // v0.22.2 is newer (2026-11-13), v0.24.0 next (2026-11-12), v0.24.0-rc.1 oldest (2026-11-11)
    assert.deepEqual(guids, [
        `${PREFIX  }v0.22.2`,
        `${PREFIX  }v0.24.0`,
        `${PREFIX  }v0.24.0-rc.1`
    ]);
});

test("fixture: item links equal item guids", () => {
    const xml = renderReleaseFeed(FIXTURE_PAGES, SINCE);
    assert.deepEqual(extractItemLinks(xml), extractGuids(xml));
});

test("fixture: all guids have isPermaLink=true", () => {
    const xml = renderReleaseFeed(FIXTURE_PAGES, SINCE);
    const matches = [...xml.matchAll(/<guid\s+isPermaLink="true">/g)];
    assert.equal(matches.length, 3);
});

test("fixture: Atom self-link points to canonical feed URL", () => {
    const xml = renderReleaseFeed(FIXTURE_PAGES, SINCE);
    assert.ok(xml.includes("href=\"https://ocm.software/releases.xml\""), "missing Atom self-link");
});

test("fixture: RSS 2.0 declaration and channel structure present", () => {
    const xml = renderReleaseFeed(FIXTURE_PAGES, SINCE);
    assert.ok(xml.startsWith("<?xml version=\"1.0\" encoding=\"UTF-8\"?>"), "missing XML declaration");
    assert.ok(xml.includes("<rss version=\"2.0\""), "missing rss element");
    assert.ok(xml.includes("<title>OCM Releases</title>"), "missing channel title");
    assert.ok(xml.includes("<language>en</language>"), "missing language");
});

// ---------------------------------------------------------------------------
// Determinism: page/record reordering
// ---------------------------------------------------------------------------

test("reordering records within a page does not change output", () => {
    const reordered = [
        [...FIXTURE_PAGES[0]].reverse(),
        [...FIXTURE_PAGES[1]].reverse()
    ];
    assert.equal(renderReleaseFeed(reordered, SINCE), renderReleaseFeed(FIXTURE_PAGES, SINCE));
});

test("swapping pages does not change output", () => {
    const swapped = [FIXTURE_PAGES[1], FIXTURE_PAGES[0]];
    assert.equal(renderReleaseFeed(swapped, SINCE), renderReleaseFeed(FIXTURE_PAGES, SINCE));
});

test("equal published_at timestamps break tie by ascending ASCII tag", () => {
    // v0.24.0-rc.1 and v0.24.0 have different dates, so we use a synthetic pair
    const pages = [[
        rec("v1.0.0-rc.1", "2026-12-01T00:00:00Z"),
        rec("v1.0.0",      "2026-12-01T00:00:00Z")
    ]];
    const xml = renderReleaseFeed(pages, "2026-01-01T00:00:00Z");
    const guids = extractGuids(xml);
    // ascending ASCII: "v1.0.0" < "v1.0.0-rc.1" → both at same ms, "v1.0.0" first
    assert.deepEqual(guids, [`${PREFIX  }v1.0.0`, `${PREFIX  }v1.0.0-rc.1`]);
});

// ---------------------------------------------------------------------------
// Mutable-field independence
// ---------------------------------------------------------------------------

test("replacing name/body/html_url/updated_at/prerelease does not change output", () => {
    const modified = FIXTURE_PAGES.map((page) =>
        page.map((r) => ({
            ...r,
            name: "Completely Different Title <script>",
            body: "## Release notes\n<b>XSS &amp;</b>",
            html_url: "https://evil.example.com/",
            updated_at: "2099-01-01T00:00:00Z",
            // flip prerelease on every record
            prerelease: !r.prerelease
        }))
    );
    assert.equal(renderReleaseFeed(modified, SINCE), renderReleaseFeed(FIXTURE_PAGES, SINCE));
});

test("XML metacharacters in tag name are escaped in output", () => {
    // Synthetic tag that passes the regex but includes characters that would break XML if unescaped
    // (The canonical regex won't match tags with < or &, but test escapeXml path via title/desc)
    const pages = [[rec("v1.0.0", "2026-12-01T00:00:00Z")]];
    const xml = renderReleaseFeed(pages, "2026-01-01T00:00:00Z");
    // Confirm no unescaped < or & in the item section
    const itemSection = xml.slice(xml.indexOf("<item>"));
    // Strip tags themselves, check text nodes
    const textOnly = itemSection.replace(/<[^>]+>/g, "");
    assert.ok(!textOnly.includes("&") || textOnly.includes("&amp;") || textOnly.includes("&lt;") || !textOnly.match(/&(?!amp;|lt;|gt;|quot;|apos;)/), "bare & in text");
});

// ---------------------------------------------------------------------------
// Empty/zero-item cases
// ---------------------------------------------------------------------------

test("[[]] produces valid RSS with zero items", () => {
    const xml = renderReleaseFeed([[]], SINCE);
    assert.deepEqual(extractGuids(xml), []);
    assert.ok(xml.includes("<rss"), "should still emit RSS wrapper");
    assert.ok(!xml.includes("<item>"), "should have no items");
});

test("input with no post-cutoff releases produces zero items", () => {
    const pages = [[rec("v0.23.0", "2026-11-10T23:59:59Z")]];
    const xml = renderReleaseFeed(pages, SINCE);
    assert.equal(extractGuids(xml).length, 0);
    assert.ok(!xml.includes("<item>"), "should have no items");
});

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

test("conflicting published_at for same canonical tag throws", () => {
    const pages = [[
        rec("v1.0.0", "2026-12-01T00:00:00Z"),
        rec("v1.0.0", "2026-12-02T00:00:00Z")
    ]];
    assert.throws(() => renderReleaseFeed(pages, "2026-01-01T00:00:00Z"), /conflicting/i);
});

test("invalid since throws", () => {
    assert.throws(() => renderReleaseFeed([[]], "not-a-date"), /invalid since/i);
});

test("non-array pages throws", () => {
    assert.throws(() => renderReleaseFeed(/** @type {any} */ ({}), SINCE), /must be an array/i);
});

test("page member that is not an array throws", () => {
    assert.throws(() => renderReleaseFeed(/** @type {any} */ ([{}]), SINCE), /each page must be an array/i);
});

test("record missing tag_name throws", () => {
    assert.throws(() => renderReleaseFeed([[{ draft: false, published_at: "2026-12-01T00:00:00Z", prerelease: false }]], SINCE), /tag_name/i);
});

test("record with non-boolean draft throws", () => {
    assert.throws(() => renderReleaseFeed([[{ tag_name: "v1.0.0", draft: "no", published_at: "2026-12-01T00:00:00Z", prerelease: false }]], SINCE), /draft must be boolean/i);
});

test("invalid published_at on eligible record throws", () => {
    const pages = [[rec("v1.0.0", "not-a-date")]];
    assert.throws(() => renderReleaseFeed(pages, "2026-01-01T00:00:00Z"), /invalid published_at/i);
});

// ---------------------------------------------------------------------------
// CLI integration tests (use actual node subprocess)
// ---------------------------------------------------------------------------

/** Shared tmpdir for CLI tests — created at module load, cleaned up on process exit. */
const tmpDir = mkdtempSync(path.join(os.tmpdir(), "ocm-feed-test-"));
process.on("exit", () => rmSync(tmpDir, { recursive: true, force: true }));

test("CLI: successful run produces correct three GUIDs and replaces output", () => {
    const outPath = path.join(tmpDir, "out.xml");
    // pre-existing file must be replaced
    writeFileSync(outPath, "OLD CONTENT");

    execFileSync(process.execPath, [
        path.join(__dirname, "generate-release-feed.js"),
        "--input",  path.join(__dirname, "testdata", "release-feed.json"),
        "--output", outPath,
        "--since",  SINCE
    ], { encoding: "utf8" });

    const xml = readFileSync(outPath, "utf8");
    const guids = extractGuids(xml);
    assert.deepEqual(guids, [
        `${PREFIX  }v0.22.2`,
        `${PREFIX  }v0.24.0`,
        `${PREFIX  }v0.24.0-rc.1`
    ]);
});

test("CLI: corrupt JSON leaves pre-existing output unchanged and exits nonzero", () => {
    const inputPath = path.join(tmpDir, "corrupt.json");
    const outPath   = path.join(tmpDir, "preserved.xml");

    writeFileSync(inputPath, "{ not json }");
    writeFileSync(outPath, "PRESERVED");

    let threw = false;
    try {
        execFileSync(process.execPath, [
            path.join(__dirname, "generate-release-feed.js"),
            "--input",  inputPath,
            "--output", outPath,
            "--since",  SINCE
        ], { encoding: "utf8" });
    } catch {
        threw = true;
    }

    assert.ok(threw, "should have exited nonzero");
    assert.equal(readFileSync(outPath, "utf8"), "PRESERVED");
});

test("CLI: missing --input exits nonzero", () => {
    assert.throws(() =>
        execFileSync(process.execPath, [
            path.join(__dirname, "generate-release-feed.js"),
            "--output", "/tmp/x.xml",
            "--since",  SINCE
        ], { encoding: "utf8" })
    );
});

test("CLI: missing --output exits nonzero", () => {
    assert.throws(() =>
        execFileSync(process.execPath, [
            path.join(__dirname, "generate-release-feed.js"),
            "--input", path.join(__dirname, "testdata", "release-feed.json"),
            "--since", SINCE
        ], { encoding: "utf8" })
    );
});

test("CLI: missing --since exits nonzero", () => {
    assert.throws(() =>
        execFileSync(process.execPath, [
            path.join(__dirname, "generate-release-feed.js"),
            "--input",  path.join(__dirname, "testdata", "release-feed.json"),
            "--output", path.join(tmpDir, "nosince.xml")
        ], { encoding: "utf8" })
    );
});

test("CLI: missing input file exits nonzero", () => {
    assert.throws(() =>
        execFileSync(process.execPath, [
            path.join(__dirname, "generate-release-feed.js"),
            "--input",  path.join(tmpDir, "nonexistent.json"),
            "--output", path.join(tmpDir, "out2.xml"),
            "--since",  SINCE
        ], { encoding: "utf8" })
    );
});
