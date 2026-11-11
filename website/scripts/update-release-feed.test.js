const test = require("node:test");
const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const { mkdtempSync, readFileSync, writeFileSync, rmSync, existsSync } = require("node:fs");
const { join } = require("node:path");
const { tmpdir } = require("node:os");

const SCRIPT = join(__dirname, "update-release-feed.py");
const RELEASE = JSON.parse(readFileSync(join(__dirname, "testdata/release.json"), "utf8"));
const PREFIX = "https://github.com/open-component-model/open-component-model/releases/tag/";
const FINAL = { ...RELEASE, tag_name: "v0.24.0", published_at: "2026-11-12T00:00:00Z", prerelease: false };

function setup(t) {
    const dir = mkdtempSync(join(tmpdir(), "ocm-release-feed-"));
    t.after(() => rmSync(dir, { recursive: true, force: true }));
    const feed = join(dir, "releases.xml");
    const input = join(dir, "release.json");
    const invoke = (...args) => spawnSync("python3", [SCRIPT, ...args], { encoding: "utf8" });
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

function guids(file) {
    return [...readFileSync(file, "utf8").matchAll(/<guid[^>]*>([^<]+)<\/guid>/g)].map((match) => match[1]);
}

function succeeded(result) {
    assert.equal(result.status, 0, result.stderr);
}

test("first run adds only the passed release, including an old release", (t) => {
    const env = setup(t);
    const old = { ...RELEASE, tag_name: "v0.1.0", published_at: "2020-01-01T00:00:00Z" };
    succeeded(env.run(old));
    assert.deepEqual(guids(env.feed), [`${PREFIX}v0.1.0`]);
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

test("invalid JSON and missing arguments do not create a feed", (t) => {
    const env = setup(t);
    writeFileSync(env.input, "{invalid}");
    assert.notEqual(env.invoke("--release", env.input, "--feed", env.feed).status, 0);
    assert.notEqual(env.invoke("--release", env.input).status, 0);
    assert.notEqual(env.invoke("--feed", env.feed).status, 0);
    assert.equal(existsSync(env.feed), false);
});
