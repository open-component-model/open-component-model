// Tests for release-feed-publication.js
//
// Run: `node --test .github/scripts/release-feed-publication.test.js` (from repo root)
// or:  `npm test` from `.github/scripts/`

import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync, rmSync, existsSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { publishReleaseFeed, preserveReleaseFeed } from "./release-feed-publication.js";

// ---------------------------------------------------------------------------
// Stub factory for the actions/github-script { github, context, core } object
// ---------------------------------------------------------------------------

const REPO = { owner: "open-component-model", repo: "open-component-model" };

function makeCore() {
    const logs = [];
    return {
        info: (msg) => logs.push(msg),
        _logs: logs,
    };
}

/**
 * Build a minimal github stub.
 * @param {{ getContent?: Function, getBlob?: Function, getBranch?: Function, createOrUpdate?: Function }} overrides
 */
function makeGithub({
    getContent = async () => { throw Object.assign(new Error("404"), { status: 404 }); },
    getBlob    = async () => { throw new Error("getBlob should not be called"); },
    getBranch  = async () => ({ data: { name: "gh-pages" } }),  // branch exists by default
    createOrUpdate = async () => ({ data: { commit: { html_url: "https://github.com/example/commit/abc" } } }),
} = {}) {
    return {
        rest: {
            repos: {
                getContent:               getContent,
                getBranch:                getBranch,
                createOrUpdateFileContents: createOrUpdate,
            },
            git: {
                getBlob: getBlob,
            },
        },
    };
}

function makeCtx(github, core) {
    return { github, context: { repo: REPO }, core };
}

// ---------------------------------------------------------------------------
// Tmpdir shared across tests
// ---------------------------------------------------------------------------

const TMP = mkdtempSync(join(tmpdir(), "fp-test-"));
process.on("exit", () => rmSync(TMP, { recursive: true, force: true }));

function tmpFile(name, content) {
    const p = join(TMP, name);
    if (content !== undefined) writeFileSync(p, content);
    return p;
}

// ---------------------------------------------------------------------------
// publishReleaseFeed
// ---------------------------------------------------------------------------

test("publishReleaseFeed: returns false and makes no API call when bytes are unchanged", async () => {
    const feedContent = Buffer.from("<?xml version='1.0'?><rss/>");
    const b64 = feedContent.toString("base64");

    let createCalled = false;
    const github = makeGithub({
        getContent: async () => ({
            data: { type: "file", encoding: "base64", content: b64, sha: "abc123" },
        }),
        createOrUpdate: async () => { createCalled = true; },
    });
    const core = makeCore();
    const inputPath = tmpFile("unchanged.xml", feedContent);

    const result = await publishReleaseFeed(makeCtx(github, core), inputPath);

    assert.equal(result, false);
    assert.equal(createCalled, false);
    assert.ok(core._logs.some(l => l.includes("unchanged")));
});

test("publishReleaseFeed: returns true and calls createOrUpdateFileContents when bytes differ", async () => {
    const oldContent = Buffer.from("OLD");
    const newContent = Buffer.from("NEW");

    let capturedParams = null;
    const github = makeGithub({
        getContent: async () => ({
            data: { type: "file", encoding: "base64", content: oldContent.toString("base64"), sha: "sha1" },
        }),
        createOrUpdate: async (params) => {
            capturedParams = params;
            return { data: { commit: { html_url: "https://example.com/commit" } } };
        },
    });
    const core = makeCore();
    const inputPath = tmpFile("changed.xml", newContent);

    const result = await publishReleaseFeed(makeCtx(github, core), inputPath);

    assert.equal(result, true);
    assert.ok(capturedParams !== null);
    assert.equal(capturedParams.sha, "sha1");
    assert.equal(capturedParams.path, "releases.xml");
    assert.equal(capturedParams.branch, "gh-pages");
    assert.equal(capturedParams.content, newContent.toString("base64"));
});

test("publishReleaseFeed: initial creation omits sha", async () => {
    const newContent = Buffer.from("INITIAL");

    let capturedParams = null;
    const github = makeGithub({
        getContent: async () => { throw Object.assign(new Error("404"), { status: 404 }); },
        getBranch: async () => ({ data: { name: "gh-pages" } }),
        createOrUpdate: async (params) => {
            capturedParams = params;
            return { data: { commit: { html_url: "https://example.com/commit" } } };
        },
    });
    const core = makeCore();
    const inputPath = tmpFile("initial.xml", newContent);

    const result = await publishReleaseFeed(makeCtx(github, core), inputPath);

    assert.equal(result, true);
    assert.ok(!("sha" in capturedParams), "sha must not be present for initial creation");
});

test("publishReleaseFeed: encoding:none triggers blob fetch", async () => {
    const feedContent = Buffer.from("BIG FILE");
    const newContent = Buffer.from("DIFFERENT");

    let blobCalled = false;
    const github = makeGithub({
        getContent: async () => ({
            data: { type: "file", encoding: "none", content: "", sha: "blobsha" },
        }),
        getBlob: async ({ file_sha }) => {
            blobCalled = true;
            assert.equal(file_sha, "blobsha");
            return { data: { encoding: "base64", content: feedContent.toString("base64") } };
        },
        createOrUpdate: async () => ({ data: { commit: { html_url: "https://example.com" } } }),
    });
    const core = makeCore();
    const inputPath = tmpFile("bigenough.xml", newContent);

    const result = await publishReleaseFeed(makeCtx(github, core), inputPath);

    assert.ok(blobCalled, "should have fetched blob for encoding:none");
    assert.equal(result, true);
});

test("publishReleaseFeed: missing Pages branch throws, not treated as absent file", async () => {
    const github = makeGithub({
        getContent: async () => { throw Object.assign(new Error("404"), { status: 404 }); },
        getBranch: async () => { throw Object.assign(new Error("Not Found"), { status: 404 }); },
    });
    const core = makeCore();
    const inputPath = tmpFile("nobranch.xml", Buffer.from("x"));

    await assert.rejects(
        () => publishReleaseFeed(makeCtx(github, core), inputPath),
        /inaccessible|does not exist/i
    );
});

test("publishReleaseFeed: non-file type response throws", async () => {
    const github = makeGithub({
        getContent: async () => ({
            data: { type: "dir", encoding: "base64", content: "", sha: "x" },
        }),
    });
    const core = makeCore();
    const inputPath = tmpFile("nonfile.xml", Buffer.from("x"));

    await assert.rejects(
        () => publishReleaseFeed(makeCtx(github, core), inputPath),
        /type/i
    );
});

test("publishReleaseFeed: malformed/unreadable encoding throws without committing", async () => {
    let createCalled = false;
    const github = makeGithub({
        getContent: async () => ({
            data: { type: "file", encoding: "gzip", content: "", sha: "x" },
        }),
        createOrUpdate: async () => { createCalled = true; },
    });
    const core = makeCore();
    const inputPath = tmpFile("badenc.xml", Buffer.from("x"));

    await assert.rejects(
        () => publishReleaseFeed(makeCtx(github, core), inputPath),
        /encoding/i
    );
    assert.equal(createCalled, false);
});

// ---------------------------------------------------------------------------
// preserveReleaseFeed
// ---------------------------------------------------------------------------

test("preserveReleaseFeed: writes existing feed bytes to output and returns true", async () => {
    const feedContent = Buffer.from("<?xml version='1.0'?><rss/>");
    const github = makeGithub({
        getContent: async () => ({
            data: { type: "file", encoding: "base64", content: feedContent.toString("base64"), sha: "sha1" },
        }),
    });
    const core = makeCore();
    const outPath = join(TMP, "preserved.xml");
    // ensure it doesn't exist yet
    try { rmSync(outPath); } catch { /* ok */ }

    const result = await preserveReleaseFeed(makeCtx(github, core), outPath);

    assert.equal(result, true);
    assert.ok(readFileSync(outPath).equals(feedContent));
});

test("preserveReleaseFeed: genuinely absent feed returns false and leaves output absent", async () => {
    const github = makeGithub({
        // 404 file, branch exists
        getContent: async () => { throw Object.assign(new Error("404"), { status: 404 }); },
        getBranch: async () => ({ data: { name: "gh-pages" } }),
    });
    const core = makeCore();
    const outPath = join(TMP, "absent-output-that-should-not-exist.xml");
    try { rmSync(outPath); } catch { /* ok */ }

    const result = await preserveReleaseFeed(makeCtx(github, core), outPath);

    assert.equal(result, false);
    assert.ok(!existsSync(outPath), "output must not be created when feed is absent");
});

test("preserveReleaseFeed: absent feed but existing local output file throws", async () => {
    const github = makeGithub({
        getContent: async () => { throw Object.assign(new Error("404"), { status: 404 }); },
        getBranch: async () => ({ data: { name: "gh-pages" } }),
    });
    const core = makeCore();
    const outPath = tmpFile("existing-unowned.xml", Buffer.from("STALE"));

    await assert.rejects(
        () => preserveReleaseFeed(makeCtx(github, core), outPath),
        /unowned|refusing/i
    );
});

test("preserveReleaseFeed: API error throws without writing output", async () => {
    const github = makeGithub({
        getContent: async () => { throw new Error("network failure"); },
    });
    const core = makeCore();
    const outPath = join(TMP, "no-write-on-error.xml");
    try { rmSync(outPath); } catch { /* ok */ }

    await assert.rejects(
        () => preserveReleaseFeed(makeCtx(github, core), outPath),
        /network failure/i
    );
    assert.ok(!existsSync(outPath), "output must not be written on API error");
});
