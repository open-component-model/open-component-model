// Tests for check-page-resources.js
//
// Run: `node --test scripts/check-page-resources.test.js`
//
// The key scenario is "the page is fine but a resource it embeds is missing".
// The schema-renderer fixtures below build a page that is valid HTML, yet
// points `data-schema-url` at a versioned path that was never published -
// exactly the failure the link verifier cannot see.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const {
    isInternalUrl,
    normalizeUrl,
    resolveCandidates,
    parseSrcset,
    stripNonMarkup,
    extractResourceRefs,
    checkPublicDir,
    parseArguments
} = require("./check-page-resources");

// --- isInternalUrl ---

test("isInternalUrl: root-relative and relative are internal", () => {
    assert.equal(isInternalUrl("/images/logo.png"), true);
    assert.equal(isInternalUrl("../css/main.css"), true);
    assert.equal(isInternalUrl("/0.9/schemas/x.yaml"), true);
});

test("isInternalUrl: external, scheme, protocol-relative and anchors are skipped", () => {
    assert.equal(isInternalUrl("https://ocm.software/x"), false);
    assert.equal(isInternalUrl("//cdn.example.com/x.js"), false);
    assert.equal(isInternalUrl("mailto:a@b.c"), false);
    assert.equal(isInternalUrl("data:image/png;base64,AAAA"), false);
    assert.equal(isInternalUrl("#section"), false);
    assert.equal(isInternalUrl("  "), false);
});

// --- normalizeUrl ---

test("normalizeUrl: strips query and fragment and decodes", () => {
    assert.equal(normalizeUrl("/a/b.png?v=2#top"), "/a/b.png");
    assert.equal(normalizeUrl("/a/my%20file.png"), "/a/my file.png");
});

// --- parseSrcset ---

test("parseSrcset: takes the URL part of each candidate", () => {
    assert.deepEqual(
        parseSrcset("/a.png 1x, /b.png 2x"),
        ["/a.png", "/b.png"]
    );
});

test("parseSrcset: keeps commas inside data: URIs and handles no-descriptor lists", () => {
    assert.deepEqual(
        parseSrcset("data:image/svg+xml,%3Csvg%3E 1x,/b.png 2x"),
        ["data:image/svg+xml,%3Csvg%3E", "/b.png"]
    );
    assert.deepEqual(parseSrcset("/a.png, /b.png"), ["/a.png", "/b.png"]);
    assert.deepEqual(parseSrcset(""), []);
});

// --- extractResourceRefs ---

test("extractResourceRefs: finds src, link href and data-schema-url", () => {
    const html = [
        "<link rel=\"stylesheet\" href=\"/css/main.css\">",
        "<img src=\"/images/logo.png\">",
        "<a href=\"/docs/intro/\">intro</a>",
        "<div data-schema-renderer data-schema-url=\"/0.17/schemas/x.yaml\"></div>",
        "<script src=\"https://cdn.example.com/x.js\"></script>"
    ].join("\n");

    const refs = extractResourceRefs(html);
    const byUrl = Object.fromEntries(refs.map((r) => [r.url, r.attr]));

    assert.equal(byUrl["/css/main.css"], "href");
    assert.equal(byUrl["/images/logo.png"], "src");
    assert.equal(byUrl["/0.17/schemas/x.yaml"], "data-schema-url");
    assert.equal(byUrl["https://cdn.example.com/x.js"], "src");
});

test("extractResourceRefs: navigational <a href> and <area href> are NOT reported", () => {
    const html = "<a href=\"/docs/intro/\">intro</a>"
        + "<a class=\"x\"href=\"../broken/\">b</a>"
        + "<map><area href=\"/nav/\" alt=\"n\"></map>"
        + "<a data-src=\"/img/lazy.png\" href=\"/nav2/\">c</a>";
    const urls = extractResourceRefs(html).map((r) => r.url);
    assert.deepEqual(urls, ["/img/lazy.png"]);
});

test("extractResourceRefs: text and <code> content is never scanned", () => {
    const html = "<p>Use src=\"/text/not-a-ref.png\" in your page</p>"
        + "<pre><code>&lt;img src=\"/code/not-a-ref.png\"&gt;</code></pre>"
        + "<img src=\"/real.png\">";
    const urls = extractResourceRefs(html).map((r) => r.url);
    assert.deepEqual(urls, ["/real.png"]);
});

test("extractResourceRefs: data-src and data-srcset are handled", () => {
    const html = "<img data-src=\"/img/lazy.png\" data-srcset=\"/img/a.png 1x, /img/b.png 2x\">";
    const refs = extractResourceRefs(html).map((r) => `${r.attr}:${r.url}`);
    assert.deepEqual(refs, [
        "data-src:/img/lazy.png",
        "data-srcset:/img/a.png",
        "data-srcset:/img/b.png"
    ]);
});

test("extractResourceRefs: unquoted attributes (hugo --minify)", () => {
    const html = "<link rel=stylesheet href=/css/main.css>"
        + "<img alt=logo src=/images/logo.png>"
        + "<script src=/js/app.js defer></script>";
    const refs = extractResourceRefs(html).map((r) => `${r.tag}:${r.attr}:${r.url}`);
    assert.deepEqual(refs, [
        "link:href:/css/main.css",
        "img:src:/images/logo.png",
        "script:src:/js/app.js"
    ]);
});

test("extractResourceRefs: adjacent space-less attributes are all found", () => {
    const html = "<img src=\"/a.png\"srcset=\"/b.png 1x\"data-src=\"/c.png\">";
    const refs = extractResourceRefs(html).map((r) => `${r.attr}:${r.url}`);
    assert.deepEqual(refs, ["src:/a.png", "srcset:/b.png", "data-src:/c.png"]);
});

test("extractResourceRefs: a > inside a quoted value does not end the tag", () => {
    const refs = extractResourceRefs("<img alt=\"a > b\" src=\"/x.png\">");
    assert.deepEqual(refs.map((r) => r.url), ["/x.png"]);
});

test("extractResourceRefs: srcset with w descriptors and a data: URI", () => {
    const html = "<img srcset=\"/img/a-320.png 320w, /img/a-640.png 640w,"
        + " data:image/png;base64,AAAA,BBBB 1x, /img/a-1280.png 1280w\">";
    const refs = extractResourceRefs(html);
    assert.deepEqual(refs.map((r) => r.url), [
        "/img/a-320.png",
        "/img/a-640.png",
        "data:image/png;base64,AAAA,BBBB",
        "/img/a-1280.png"
    ]);
    // data: URIs are not internal, so the checker never resolves them.
    assert.deepEqual(refs.filter((r) => isInternalUrl(r.url)).length, 3);
});

test("extractResourceRefs: does not confuse data= with data-schema-url", () => {
    const html = "<object data=\"/f/report.pdf\"></object>"
        + "<div data-schema-url=\"/s/a.yaml\"></div>";
    const refs = extractResourceRefs(html);
    const attrs = refs.map((r) => `${r.attr}:${r.url}`);
    assert.ok(attrs.includes("data:/f/report.pdf"));
    assert.ok(attrs.includes("data-schema-url:/s/a.yaml"));
});

test("extractResourceRefs: finds attributes with no leading space (hugo --minify)", () => {
    // tdewolff drops the space between a quoted value and the next attribute.
    const html = "<img alt=\"logo\"src=\"/images/logo.png\">"
        + "<link rel=\"stylesheet\"href=\"/css/main.css\">";
    const refs = extractResourceRefs(html);
    const byUrl = Object.fromEntries(refs.map((r) => [r.url, r.attr]));
    assert.equal(byUrl["/images/logo.png"], "src");
    assert.equal(byUrl["/css/main.css"], "href");
});

test("extractResourceRefs: end-to-end srcset yields each candidate URL", () => {
    const refs = extractResourceRefs("<img srcset=\"/a.png 1x, /b.png 2x\">");
    const urls = refs.filter((r) => r.attr === "srcset").map((r) => r.url);
    assert.deepEqual(urls, ["/a.png", "/b.png"]);
});

test("stripNonMarkup: removes comments and script/style bodies but keeps the tags", () => {
    const html = [
        "<!-- <img src=\"/old/removed.png\"> -->",
        "<style>.x{background:url(/css/bg.png)}</style>",
        "<script src=\"/js/app.js\">const img = \"/js/not-a-ref.png\";</script>"
    ].join("\n");
    const stripped = stripNonMarkup(html);
    assert.ok(!stripped.includes("removed.png"));
    assert.ok(!stripped.includes("bg.png"));
    assert.ok(!stripped.includes("not-a-ref.png"));
    assert.ok(stripped.includes("<script src=\"/js/app.js\"></script>"));
    assert.ok(stripped.includes("<style></style>"));
});

test("stripNonMarkup: a <!-- inside an inline script does not swallow real markup", () => {
    const html = "<script>var s = \"<!--\";</script><img src=\"/after.png\"><!-- c -->";
    const stripped = stripNonMarkup(html);
    assert.ok(stripped.includes("<img src=\"/after.png\">"));
    assert.ok(!stripped.includes("<!-- c -->"));
});

test("extractResourceRefs: comments and inline script/style bodies are not scanned", () => {
    const html = [
        "<!-- <img src=\"/old/removed.png\"> -->",
        "<style>.x{background:url(/css/bg.png)}</style>",
        "<script src=\"/js/app.js\">const img = \"/js/not-a-ref.png\";</script>"
    ].join("\n");
    assert.deepEqual(extractResourceRefs(html).map((r) => r.url), ["/js/app.js"]);
});

// --- resolveCandidates ---

test("resolveCandidates: trailing slash resolves to index.html", () => {
    const got = resolveCandidates("/docs/intro/", "/pub/index.html", "/pub");
    assert.deepEqual(got, [path.join("/pub/docs/intro", "index.html")]);
});

test("resolveCandidates: extensionless tries file, index.html and .html", () => {
    const got = resolveCandidates("/docs/intro", "/pub/index.html", "/pub");
    assert.deepEqual(got, [
        "/pub/docs/intro",
        path.join("/pub/docs/intro", "index.html"),
        "/pub/docs/intro.html"
    ]);
});

test("resolveCandidates: relative URL resolves against the referencing page dir", () => {
    const got = resolveCandidates("../img/a.png", "/pub/docs/intro/index.html", "/pub");
    assert.equal(got[0], "/pub/docs/img/a.png");
});

test("resolveCandidates: paths that escape publicDir yield no candidates", () => {
    assert.deepEqual(resolveCandidates("/../../etc/passwd", "/pub/index.html", "/pub"), []);
    assert.deepEqual(resolveCandidates("../../x.png", "/pub/docs/index.html", "/pub"), []);
    assert.deepEqual(resolveCandidates("/sibling/../../pub2/x.png", "/pub/index.html", "/pub"), []);
});

// --- checkPublicDir (integration against a temp build output) ---

function writeFile(file, content) {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, content);
}

function buildFixtureSite(schemaUrl, { publishSchema }) {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ocm-res-"));

    // Shared assets that always exist.
    writeFile(path.join(dir, "css", "main.css"), "body{}");
    writeFile(path.join(dir, "images", "logo.png"), "png");
    writeFile(path.join(dir, "docs", "intro", "index.html"), "<html></html>");

    // The schema file is published only for the "healthy" case.
    if (publishSchema) {
        writeFile(path.join(dir, schemaUrl.replace(/^\//, "")), "type: object");
    }

    // A perfectly valid page (would return 200) that embeds a schema via the
    // shortcode output. Its correctness depends on the schema file existing.
    writeFile(
        path.join(dir, "docs", "reference", "index.html"),
        [
            "<html><head>",
            "<link rel=\"stylesheet\" href=\"/css/main.css\">",
            "</head><body>",
            "<img src=\"/images/logo.png\">",
            "<a href=\"/docs/intro/\">intro</a>",
            `<div class="sr-root" data-schema-renderer data-schema-url="${schemaUrl}"></div>`,
            "</body></html>"
        ].join("\n")
    );

    return dir;
}

test("checkPublicDir: healthy site reports no missing resources", () => {
    const dir = buildFixtureSite("/0.17/schemas/x.yaml", { publishSchema: true });
    try {
        const report = checkPublicDir(dir);
        assert.equal(report.missing.length, 0, JSON.stringify(report.missing, null, 2));
        assert.equal(report.checked, 3);
    } finally {
        fs.rmSync(dir, { recursive: true, force: true });
    }
});

test("checkPublicDir: catches a 200 page whose embedded schema 404s", () => {
    // This is the EPIC #1160 case: versioned path /0.9/schemas/... never
    // published, page still renders, linkspector would stay green.
    const dir = buildFixtureSite("/0.9/schemas/x.yaml", { publishSchema: false });
    try {
        const report = checkPublicDir(dir);
        assert.equal(report.missing.length, 1);
        assert.equal(report.missing[0].attr, "data-schema-url");
        assert.equal(report.missing[0].url, "/0.9/schemas/x.yaml");
        assert.equal(report.missing[0].page, path.join("docs", "reference", "index.html"));
    } finally {
        fs.rmSync(dir, { recursive: true, force: true });
    }
});

test("checkPublicDir: --ignore pattern suppresses a known-dynamic path", () => {
    const dir = buildFixtureSite("/0.9/schemas/x.yaml", { publishSchema: false });
    try {
        const report = checkPublicDir(dir, { ignore: [/^\/0\.9\/schemas\//] });
        assert.equal(report.missing.length, 0);
    } finally {
        fs.rmSync(dir, { recursive: true, force: true });
    }
});

test("checkPublicDir: <a href> navigation to a missing page is not reported", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ocm-res-"));
    try {
        writeFile(
            path.join(dir, "index.html"),
            "<html><body><a href=\"/does/not/exist/\">x</a><a href=\"../gone.html\">y</a></body></html>"
        );
        const report = checkPublicDir(dir);
        assert.equal(report.missing.length, 0);
        assert.equal(report.checked, 0);
    } finally {
        fs.rmSync(dir, { recursive: true, force: true });
    }
});

test("checkPublicDir: versioned page resolves root-relative and ../ refs", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ocm-res-"));
    try {
        writeFile(path.join(dir, "0.9", "images", "a.png"), "png");
        writeFile(path.join(dir, "0.9", "docs", "img", "rel.png"), "png");
        writeFile(path.join(dir, "0.9", "schemas", "ok.yaml"), "type: object");
        writeFile(
            path.join(dir, "0.9", "docs", "guide", "index.html"),
            [
                "<img src=/0.9/images/a.png>",
                "<img src=\"../img/rel.png\">",
                "<div data-schema-url=\"/0.9/schemas/ok.yaml\"></div>",
                "<img src=\"/0.9/images/missing.png\">",
                "<img src=\"../img/missing-rel.png\">",
                "<div data-schema-url=/0.9/schemas/missing.yaml></div>",
                "<img src=\"../../../../escape.png\">"
            ].join("")
        );
        const report = checkPublicDir(dir);
        const page = path.join("0.9", "docs", "guide", "index.html");
        assert.deepEqual(
            report.missing.map((m) => `${m.page}|${m.attr}|${m.url}`),
            [
                `${page}|data-schema-url|/0.9/schemas/missing.yaml`,
                `${page}|src|../../../../escape.png`,
                `${page}|src|../img/missing-rel.png`,
                `${page}|src|/0.9/images/missing.png`
            ]
        );
        assert.equal(report.checked, 7);
    } finally {
        fs.rmSync(dir, { recursive: true, force: true });
    }
});

// --- CLI exit codes (end-to-end through main) ---

const { execFileSync } = require("node:child_process");
const SCRIPT = path.join(__dirname, "check-page-resources.js");

function runCli(args) {
    try {
        const stdout = execFileSync(process.execPath, [SCRIPT, ...args], { encoding: "utf8" });
        return { code: 0, stdout };
    } catch (err) {
        return { code: err.status, stdout: `${err.stdout ?? ""}${err.stderr ?? ""}` };
    }
}

test("CLI: exit 0 on a healthy site, 1 on a missing resource", () => {
    const ok = buildFixtureSite("/0.17/schemas/x.yaml", { publishSchema: true });
    const broken = buildFixtureSite("/0.9/schemas/x.yaml", { publishSchema: false });
    try {
        assert.equal(runCli([ok]).code, 0);
        const bad = runCli([broken]);
        assert.equal(bad.code, 1);
        assert.match(bad.stdout, /data-schema-url: \/0\.9\/schemas\/x\.yaml/);
    } finally {
        fs.rmSync(ok, { recursive: true, force: true });
        fs.rmSync(broken, { recursive: true, force: true });
    }
});

test("CLI: --ignore suppresses a known-missing path and keeps the rest", () => {
    const broken = buildFixtureSite("/0.9/schemas/x.yaml", { publishSchema: false });
    try {
        assert.equal(runCli([broken]).code, 1);
        assert.equal(runCli([broken, "--ignore", "^/0\\.9/schemas/"]).code, 0);
        assert.equal(runCli(["--ignore", "^/0\\.9/schemas/", broken]).code, 0);
        const wrong = runCli([broken, "--ignore", "^/other/"]);
        assert.equal(wrong.code, 1);
        assert.match(wrong.stdout, /data-schema-url: \/0\.9\/schemas\/x\.yaml/);
    } finally {
        fs.rmSync(broken, { recursive: true, force: true });
    }
});

test("CLI: --ignore without a value exits 2", () => {
    assert.equal(runCli(["--ignore"]).code, 2);
});

test("CLI: exit 2 when the build output directory is missing", () => {
    const res = runCli([path.join(os.tmpdir(), "ocm-does-not-exist-xyz")]);
    assert.equal(res.code, 2);
});

// --- parseArguments ---

test("parseArguments: defaults and overrides", () => {
    assert.deepEqual(parseArguments([]), { publicDir: "public", ignore: [] });
    assert.equal(parseArguments(["dist"]).publicDir, "dist");
    const withIgnore = parseArguments(["--ignore", "^/search"]);
    assert.equal(withIgnore.ignore.length, 1);
    assert.ok(withIgnore.ignore[0].test("/search/index.json"));
});
