import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, rmSync, existsSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { DOMParser, onWarningStopParsing } from "@xmldom/xmldom";

const SCRIPT = join(import.meta.dirname, "update-release-feed.js");
const RELEASE = JSON.parse(readFileSync(join(import.meta.dirname, "testdata/release.json"), "utf8"));
const PREFIX = "https://github.com/open-component-model/open-component-model/releases/tag/";
const FINAL = { ...RELEASE, tag_name: "v0.24.0", published_at: "2026-11-12T00:00:00Z", prerelease: false };

function setup(t) {
    const dir = mkdtempSync(join(tmpdir(), "ocm-release-feed-"));
    t.after(() => rmSync(dir, { recursive: true, force: true }));
    const feed = join(dir, "releases.xml");
    const input = join(dir, "release.json");
    const invoke = (...args) => spawnSync(process.execPath, [SCRIPT, ...args], { encoding: "utf8" });
    return {
        dir, feed, input, invoke,
        run(release = RELEASE, pending) {
            writeFileSync(input, JSON.stringify(release));
            const args = ["--release", input, "--feed", feed];
            if (pending) {
                args.push("--pending", pending);
            }
            return invoke(...args);
        }
    };
}

function parseFeed(file) {
    return new DOMParser({ onError: onWarningStopParsing }).parseFromString(readFileSync(file, "utf8"), "application/xml");
}

function guids(file) {
    return Array.from(parseFeed(file).getElementsByTagName("guid")).map((element) => element.textContent);
}

function succeeded(result) {
    assert.ifError(result.error);
    assert.equal(result.status, 0, result.stderr);
}

test("first run adds only the passed release, including an old release", (t) => {
    const env = setup(t);
    const old = { ...RELEASE, tag_name: "v0.1.0", published_at: "2020-01-01T00:00:00Z" };
    succeeded(env.run(old));
    assert.deepEqual(guids(env.feed), [`${PREFIX}v0.1.0`]);
});

test("RSS exposes canonical item links and the Atom subscription URL", (t) => {
    const env = setup(t);
    succeeded(env.run());
    const document = parseFeed(env.feed);
    assert.equal(document.documentElement.tagName, "rss");
    assert.equal(document.documentElement.getAttribute("version"), "2.0");
    const item = document.getElementsByTagName("item")[0];
    const guid = item.getElementsByTagName("guid")[0];
    assert.equal(guid.textContent, `${PREFIX}${RELEASE.tag_name}`);
    assert.equal(item.getElementsByTagName("link")[0].textContent, guid.textContent);
    assert.equal(guid.getAttribute("isPermaLink"), "true");
    const self = document.getElementsByTagNameNS("http://www.w3.org/2005/Atom", "link")[0];
    assert.equal(self.getAttribute("href"), "https://ocm.software/releases.xml");
    assert.equal(self.getAttribute("rel"), "self");
    assert.equal(self.getAttribute("type"), "application/rss+xml");
});

test("adding a final retains its RC and orders by publication time", (t) => {
    const env = setup(t);
    succeeded(env.run(RELEASE));
    const rc = readFileSync(env.feed, "utf8").match(/<item>[\s\S]*?<\/item>/)[0];
    succeeded(env.run(FINAL));
    assert.deepEqual(guids(env.feed), [`${PREFIX}v0.24.0`, `${PREFIX}v0.24.0-rc.1`]);
    assert.ok(readFileSync(env.feed, "utf8").includes(rc));
});

test("rerunning a tag leaves the complete feed byte-identical despite release edits", (t) => {
    const env = setup(t);
    succeeded(env.run());
    const before = readFileSync(env.feed);
    succeeded(env.run({ ...RELEASE, name: "Edited <title>", body: "<b>& notes</b>", html_url: "https://example.org", published_at: "2026-12-01T00:00:00Z" }));
    assert.deepEqual(readFileSync(env.feed), before);
});

test("pending PR entries survive a subsequent release before merge", (t) => {
    const pending = setup(t);
    succeeded(pending.run(RELEASE));
    const current = setup(t);
    succeeded(current.run(FINAL, pending.feed));
    assert.deepEqual(guids(current.feed), [`${PREFIX}v0.24.0`, `${PREFIX}v0.24.0-rc.1`]);
    const before = readFileSync(current.feed);
    succeeded(current.run(FINAL, pending.feed));
    assert.deepEqual(readFileSync(current.feed), before);
});

test("published entries take precedence over stale copies in the pending PR", (t) => {
    const current = setup(t);
    const pending = setup(t);
    succeeded(current.run(RELEASE));
    succeeded(pending.run(RELEASE));
    const published = readFileSync(current.feed, "utf8");
    writeFileSync(pending.feed, readFileSync(pending.feed, "utf8").replace("is available.", "pending replacement."));
    succeeded(current.run(FINAL, pending.feed));
    assert.ok(readFileSync(current.feed, "utf8").includes(published.match(/<item>[\s\S]*?<\/item>/)[0]));
    assert.deepEqual(guids(current.feed), [`${PREFIX}v0.24.0`, `${PREFIX}v0.24.0-rc.1`]);
});

for (const [name, release] of [
    ["side tag", { ...RELEASE, tag_name: "bindings/go/v0.24.0" }],
    ["draft", { ...RELEASE, draft: true }],
    ["unpublished", { ...RELEASE, published_at: null }],
    ["invalid publication date", { ...RELEASE, published_at: "not-a-date" }],
    ["invalid calendar date", { ...RELEASE, published_at: "2026-02-30T00:00:00Z" }],
    ["invalid hour", { ...RELEASE, published_at: "2026-11-11T24:00:00Z" }],
    ["tag with trailing newline", { ...RELEASE, tag_name: "v0.24.0\n" }],
    ["missing timezone", { ...RELEASE, published_at: "2026-11-11T00:00:00" }],
    ["multiple releases", [RELEASE, FINAL]]
]) {
    test(`rejects ${name} without changing the feed`, (t) => {
        const env = setup(t);
        succeeded(env.run());
        const before = readFileSync(env.feed);
        assert.notEqual(env.run(release).status, 0);
        assert.deepEqual(readFileSync(env.feed), before);
    });
}

for (const pending of [false, true]) {
    test(`malformed ${pending ? "pending" : "published"} XML leaves the output unchanged`, (t) => {
        const env = setup(t);
        succeeded(env.run());
        const broken = join(env.dir, "broken.xml");
        writeFileSync(broken, "<rss><channel>");
        if (!pending) {
            writeFileSync(env.feed, "<rss><channel>");
        }
        const before = readFileSync(env.feed);
        assert.notEqual(env.run(FINAL, pending ? broken : undefined).status, 0);
        assert.deepEqual(readFileSync(env.feed), before);
    });
}

test("missing pending feed fails without replacing the published feed", (t) => {
    const env = setup(t);
    succeeded(env.run());
    const before = readFileSync(env.feed);
    assert.notEqual(env.run(FINAL, join(env.dir, "missing.xml")).status, 0);
    assert.deepEqual(readFileSync(env.feed), before);
});

test("malformed XML is rejected rather than repaired by the parser", (t) => {
    const env = setup(t);
    succeeded(env.run());
    const malformed = readFileSync(env.feed, "utf8").replace('isPermaLink="true"', "isPermaLink=true");
    writeFileSync(env.feed, malformed);
    assert.notEqual(env.run(FINAL).status, 0);
    assert.equal(readFileSync(env.feed, "utf8"), malformed);
});

test("invalid JSON and missing arguments do not create a feed", (t) => {
    const env = setup(t);
    writeFileSync(env.input, "{invalid}");
    assert.notEqual(env.invoke("--release", env.input, "--feed", env.feed).status, 0);
    assert.notEqual(env.invoke("--release", env.input).status, 0);
    assert.notEqual(env.invoke("--feed", env.feed).status, 0);
    assert.equal(existsSync(env.feed), false);
});

function encoded(file) {
    return Array.from(parseFeed(file).getElementsByTagName("content:encoded")).map((element) => element.textContent);
}

test("release notes render to sanitized content:encoded HTML", (t) => {
    const env = setup(t);
    const body = "## Changes\n\n- *(cli)* did a thing (#123)\n- plain item\n";
    succeeded(env.run({ ...RELEASE, body }));
    const [html] = encoded(env.feed);
    assert.match(html, /<h2>Changes<\/h2>/);
    assert.match(html, /<li><em>\(cli\)<\/em> did a thing \(#123\)<\/li>/);
    // The plain-text description one-liner is retained as a fallback.
    const description = parseFeed(env.feed).getElementsByTagName("description");
    assert.ok(Array.from(description).some((d) => /is available\. Release notes and downloads:/.test(d.textContent)));
});

test("hostile markup in release notes is stripped from the rendered HTML", (t) => {
    const env = setup(t);
    const body = "Safe [link](https://ocm.software).\n\n<script>alert(1)</script>\n\n<img src=x onerror=alert(2)>\n\n[evil](javascript:alert(3))";
    succeeded(env.run({ ...RELEASE, body }));
    const [html] = encoded(env.feed);
    assert.doesNotMatch(html, /<script/i);
    assert.doesNotMatch(html, /onerror/i);
    assert.doesNotMatch(html, /javascript:/i);
    assert.doesNotMatch(html, /<img/i);
    assert.match(html, /<a href="https:\/\/ocm\.software">link<\/a>/);
});

test("a literal CDATA terminator in notes stays escaped and parseable", (t) => {
    const env = setup(t);
    succeeded(env.run({ ...RELEASE, body: "before ]]> after" }));
    // marked HTML-escapes '>' so the raw ]]> never reaches the CDATA; the feed
    // stays valid XML and the text round-trips through the parser.
    const [html] = encoded(env.feed);
    assert.match(html, /before \]\]&gt; after/);
});

test("a release with notes reruns byte-identically", (t) => {
    const env = setup(t);
    const body = "## Changes\n\n- *(cli)* did a thing (#123)\n";
    succeeded(env.run({ ...RELEASE, body }));
    const before = readFileSync(env.feed);
    succeeded(env.run({ ...RELEASE, body }));
    assert.deepEqual(readFileSync(env.feed), before);
});

test("a non-string release body is rejected without creating a feed", (t) => {
    const env = setup(t);
    writeFileSync(env.input, JSON.stringify({ ...RELEASE, body: { not: "a string" } }));
    assert.notEqual(env.invoke("--release", env.input, "--feed", env.feed).status, 0);
    assert.equal(existsSync(env.feed), false);
});
