// Verifies that no frozen snapshot page has a `url:` frontmatter field
// that would hijack the default-version URL, unless a content_overrides/
// counterpart exists that strips the field.
//
// Background: the Hugo multisite plugin namespaces pages by version prefix
// (e.g. /0.9/docs/…). A `url:` override in a frozen snapshot page bypasses
// that prefix and causes two versions to claim the same canonical URL,
// breaking the version switcher. The fix is to supply an override file in
// content_overrides/ that is identical to the snapshot content but without
// the `url:` field. This test ensures the fix stays in place and catches
// any new occurrences introduced by future snapshot imports.
//
// Run: `npm test` or `node --test scripts/snapshot-url-overrides.test.js`

const test = require("node:test");
const assert = require("node:assert/strict");
const { execSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const REPO_ROOT = path.resolve(__dirname, "../..");
const WEBSITE_DIR = path.join(REPO_ROOT, "website");
const MODULE_YAML = path.join(WEBSITE_DIR, "config/_default/module.yaml");
const OVERRIDES_DIR = path.join(WEBSITE_DIR, "content_overrides");
const WEBSITE_MODULE_PATH = "ocm.software/open-component-model/website";

// Parse module.yaml and return every distinct version imported for the
// website module (these are the frozen snapshots).
function getFrozenSnapshotVersions() {
    const lines = fs.readFileSync(MODULE_YAML, "utf8").split("\n");
    const versions = new Set();
    let inImports = false;
    let currentPath = null;

    for (const line of lines) {
        if (/^imports:/.test(line)) { inImports = true; continue; }
        if (!inImports) continue;

        const pathMatch = line.match(/^\s*-?\s*path:\s*(.+)/);
        if (pathMatch) currentPath = pathMatch[1].trim();

        const verMatch = line.match(/^\s*version:\s*(.+)/);
        if (verMatch && currentPath === WEBSITE_MODULE_PATH) {
            versions.add(verMatch[1].trim());
        }
    }
    return [...versions];
}

// Hugo module version → git tag for the website sub-module.
function toGitTag(version) {
    return `website/${version}`;
}

// List all markdown files under website/content/ at the given git tag.
function listMarkdownFiles(gitTag) {
    try {
        const out = execSync(
            `git ls-tree -r --name-only "${gitTag}" -- website/content/`,
            { cwd: REPO_ROOT, encoding: "utf8" }
        );
        return out.trim().split("\n").filter(f => f.endsWith(".md") && f.length > 0);
    } catch {
        return [];
    }
}

// Read a file from a git tag. Returns null if unavailable.
function readAtTag(gitTag, filePath) {
    try {
        return execSync(`git show "${gitTag}:${filePath}"`, {
            cwd: REPO_ROOT, encoding: "utf8"
        });
    } catch {
        return null;
    }
}

// Extract the YAML frontmatter block (between the first pair of ---).
// Returns an object with the parsed key-value pairs.
function parseFrontmatter(content) {
    const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---/);
    if (!match) return {};
    const fm = {};
    for (const line of match[1].split(/\r?\n/)) {
        const kv = line.match(/^([A-Za-z_][\w-]*):\s*(.*)/);
        if (kv) fm[kv[1]] = kv[2].trim();
    }
    return fm;
}

// Path relative to website/content/
function toRelPath(filePath) {
    return filePath.replace(/^website\/content\//, "");
}

// --- tests ---

const versions = getFrozenSnapshotVersions();

test("module.yaml lists at least one frozen website snapshot", () => {
    assert.ok(versions.length > 0, "No frozen snapshot versions found in module.yaml imports");
});

for (const version of versions) {
    const gitTag = toGitTag(version);
    const files = listMarkdownFiles(gitTag);

    for (const filePath of files) {
        const relPath = toRelPath(filePath);

        test(`${gitTag}: ${relPath}: url: override requires a content_overrides counterpart`, () => {
            const content = readAtTag(gitTag, filePath);
            if (!content) return;

            const fm = parseFrontmatter(content);
            if (fm.url === undefined) return; // no url: field — nothing to check

            // A url: override exists in this snapshot. There must be a local
            // override file in content_overrides/ that removes it.
            const overridePath = path.join(OVERRIDES_DIR, relPath);
            assert.ok(
                fs.existsSync(overridePath),
                `${filePath} at ${gitTag} has url: "${fm.url}" but no override file at content_overrides/${relPath} — ` +
                "create an override without the url: field to prevent the version switcher from breaking"
            );

            const overrideFm = parseFrontmatter(fs.readFileSync(overridePath, "utf8"));
            assert.equal(
                overrideFm.url,
                undefined,
                `content_overrides/${relPath} still contains url: "${overrideFm.url}" — the override must omit it`
            );
        });
    }
}
