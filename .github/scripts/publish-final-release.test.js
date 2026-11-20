import assert from "assert";
import fs from "fs";
import path from "path";
import os from "os";
import {
  prepareReleaseNotes,
  findRelease,
  getOrCreateDraftRelease,
  publishRelease,
  uploadAssets,
  writeSummary,
} from "./publish-final-release.js";

// ----------------------------------------------------------
// Helpers
// ----------------------------------------------------------

/** Create a temp directory with optional files. */
function tmpDir(files = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "publish-final-release-test-"));
  for (const [name, content] of Object.entries(files)) {
    fs.writeFileSync(path.join(dir, name), content);
  }
  return dir;
}

/** Minimal mock for github.rest.repos.* */
function mockGitHub(overrides = {}) {
  return {
    rest: {
      repos: {
        getReleaseByTag: overrides.getReleaseByTag || (() => { throw Object.assign(new Error("Not Found"), { status: 404 }); }),
        listReleases: overrides.listReleases || (() => ({ data: [] })),
        createRelease: overrides.createRelease || (() => ({ data: { id: 1, html_url: "https://example.com/release/1" } })),
        updateRelease: overrides.updateRelease || (() => ({ data: { id: 1, html_url: "https://example.com/release/1" } })),
        listReleaseAssets: overrides.listReleaseAssets || (() => ({ data: [] })),
        deleteReleaseAsset: overrides.deleteReleaseAsset || (() => {}),
        uploadReleaseAsset: overrides.uploadReleaseAsset || ((opts) => ({ data: { state: "uploaded", size: opts.data.length } })),
      },
    },
  };
}

const mockContext = { repo: { owner: "test-owner", repo: "test-repo" } };

function mockCore() {
  const logs = [];
  const summaryChain = {};
  for (const method of ["addHeading", "addTable", "addEOL", "addLink", "addRaw"]) {
    summaryChain[method] = () => summaryChain;
  }
  summaryChain.write = async () => {};
  return {
    info: (msg) => logs.push(msg),
    setFailed: (msg) => { throw new Error(`setFailed: ${msg}`); },
    summary: summaryChain,
    _logs: logs,
  };
}

// ----------------------------------------------------------
// prepareReleaseNotes tests
// ----------------------------------------------------------

// Returns fallback when file does not exist
{
  const result = prepareReleaseNotes("/nonexistent/path.md", "rc-tag");
  assert.strictEqual(result, "Promoted from rc-tag");
}

// Returns fallback when file is empty
{
  const dir = tmpDir({ "empty.md": "" });
  const result = prepareReleaseNotes(path.join(dir, "empty.md"), "rc-tag");
  assert.strictEqual(result, "Promoted from rc-tag");
}

// Prepends the promotion provenance and keeps the rendered changelog verbatim
{
  const body = "## [0.1.0] - 2025-01-01\n\n- Some change\n\ncurl -sfL https://ocm.software/install-cli.sh | OCM_VERSION=0.1.0 bash";
  const dir = tmpDir({ "notes.md": body });
  const today = new Date().toISOString().split("T")[0];
  const result = prepareReleaseNotes(path.join(dir, "notes.md"), "v0.1.0-rc.1");
  assert.strictEqual(
    result,
    `> Promoted from \`v0.1.0-rc.1\` on ${today}\n\n${body}`,
    "Expected provenance line followed by the git-cliff output as-is",
  );
}

// Truncates body when it exceeds GitHub's 125000-char release body limit
{
  const oversize = "## [0.7.0] - 2026-05-08\n\n" + "x".repeat(130000);
  const dir = tmpDir({ "huge.md": oversize });
  const result = prepareReleaseNotes(path.join(dir, "huge.md"), "v0.7.0-rc.1");
  assert.strictEqual(result.length, 120000, `Expected exact MAX_RELEASE_BODY_LENGTH (120000), got: ${result.length}`)
  assert.ok(result.endsWith("complete history.*"), "Expected truncation notice as suffix");
  assert.ok(result.includes("## [0.7.0] - 2026-05-08"), "Expected header to remain intact");
  assert.ok(result.startsWith("> Promoted from `v0.7.0-rc.1`"), "Expected provenance line to survive truncation");
}

// Does not truncate body when within limit
{
  const fits = "## [0.7.0] - 2026-05-08\n\nSmall body";
  const dir = tmpDir({ "small.md": fits });
  const result = prepareReleaseNotes(path.join(dir, "small.md"), "v0.7.0-rc.1");
  assert.ok(!result.includes("Release notes truncated"), "Expected no truncation notice for small body");
}

// ----------------------------------------------------------
// getOrCreateDraftRelease / publishRelease tests
// ----------------------------------------------------------

const releaseOpts = { newReleaseTag: "v1.0.0", newReleaseVersion: "1.0.0", componentName: "OCM", notes: "notes" };

// Creates a draft release when none exists
{
  const calls = [];
  const gh = mockGitHub({
    createRelease: async (opts) => {
      calls.push(opts);
      return { data: { id: 42, html_url: "https://example.com/42" } };
    },
  });
  const result = await getOrCreateDraftRelease(gh, mockContext, releaseOpts);
  assert.strictEqual(result.id, 42);
  assert.strictEqual(calls.length, 1);
  assert.strictEqual(calls[0].draft, true);
  assert.strictEqual(calls[0].prerelease, false);
  assert.strictEqual(calls[0].name, "OCM 1.0.0");
}

// Reuses a draft left by an interrupted run (getReleaseByTag does not return drafts)
{
  const updates = [];
  let created = false;
  const gh = mockGitHub({
    listReleases: async () => ({ data: [
      { id: 7, draft: false, tag_name: "v0.9.0" },
      { id: 10, draft: true, tag_name: "v1.0.0" },
    ] }),
    createRelease: async () => { created = true; },
    updateRelease: async (opts) => {
      updates.push(opts);
      return { data: { id: 10, html_url: "https://example.com/10" } };
    },
  });
  const result = await getOrCreateDraftRelease(gh, mockContext, releaseOpts);
  assert.strictEqual(result.id, 10);
  assert.strictEqual(created, false);
  assert.strictEqual(updates.length, 1);
  assert.strictEqual(updates[0].release_id, 10);
  assert.strictEqual(updates[0].draft, true);
  assert.strictEqual(updates[0].name, "OCM 1.0.0");
}

// Refuses a release that is already published: its assets are immutable
{
  let changed = false;
  const gh = mockGitHub({
    getReleaseByTag: async () => ({ data: { id: 10, draft: false, tag_name: "v1.0.0" } }),
    createRelease: async () => { changed = true; },
    updateRelease: async () => { changed = true; },
  });
  await assert.rejects(() => getOrCreateDraftRelease(gh, mockContext, releaseOpts), /already published/);
  assert.strictEqual(changed, false);
}

// findRelease ignores published releases in the list and returns null without a draft
{
  const gh = mockGitHub({
    listReleases: async () => ({ data: [{ id: 7, draft: false, tag_name: "v1.0.0" }] }),
  });
  assert.strictEqual(await findRelease(gh, mockContext.repo, "v1.0.0"), null);
}

// Rethrows non-404 errors
{
  const gh = mockGitHub({
    getReleaseByTag: async () => { throw Object.assign(new Error("Server Error"), { status: 500 }); },
  });
  await assert.rejects(
    () => getOrCreateDraftRelease(gh, mockContext, releaseOpts),
    (err) => err.status === 500,
  );
}

// publishRelease publishes the draft and sets "Latest"
for (const isLatest of [true, false]) {
  const calls = [];
  const gh = mockGitHub({
    updateRelease: async (opts) => {
      calls.push(opts);
      return { data: { id: 42, html_url: "https://example.com/tag/v1.0.0" } };
    },
  });
  const result = await publishRelease(gh, mockContext, 42, isLatest);
  assert.strictEqual(result.html_url, "https://example.com/tag/v1.0.0");
  assert.strictEqual(calls.length, 1);
  assert.strictEqual(calls[0].release_id, 42);
  assert.strictEqual(calls[0].draft, false);
  assert.strictEqual(calls[0].make_latest, isLatest ? "true" : "false");
}

// ----------------------------------------------------------
// uploadAssets tests
// ----------------------------------------------------------

// Uploads all files from directory
{
  const dir = tmpDir({ "chart-1.0.0.tgz": "fake-chart-data" });
  const uploaded = [];
  const gh = mockGitHub({
    listReleaseAssets: async () => ({ data: [] }),
    uploadReleaseAsset: async (opts) => { uploaded.push(opts.name); return { data: { state: "uploaded", size: opts.data.length } }; },
  });
  const count = await uploadAssets(gh, mockContext, mockCore(), 1, dir);
  assert.deepStrictEqual(uploaded, ["chart-1.0.0.tgz"]);
  assert.strictEqual(count, 1);
}

// Replaces duplicate assets
{
  const dir = tmpDir({ "chart-1.0.0.tgz": "new-data" });
  const deleted = [];
  const uploaded = [];
  const gh = mockGitHub({
    listReleaseAssets: async () => ({ data: [{ name: "chart-1.0.0.tgz", id: 99 }] }),
    deleteReleaseAsset: async (opts) => deleted.push(opts.asset_id),
    uploadReleaseAsset: async (opts) => { uploaded.push(opts.name); return { data: { state: "uploaded", size: opts.data.length } }; },
  });
  const count = await uploadAssets(gh, mockContext, mockCore(), 1, dir);
  assert.deepStrictEqual(deleted, [99]);
  assert.deepStrictEqual(uploaded, ["chart-1.0.0.tgz"]);
  assert.strictEqual(count, 1);
}

// Uploads all files (no pattern filtering)
{
  const dir = tmpDir({ "chart-1.0.0.tgz": "data", "ocm-linux-amd64": "binary" });
  const uploaded = [];
  const gh = mockGitHub({
    listReleaseAssets: async () => ({ data: [] }),
    uploadReleaseAsset: async (opts) => { uploaded.push(opts.name); return { data: { state: "uploaded", size: opts.data.length } }; },
  });
  const count = await uploadAssets(gh, mockContext, mockCore(), 1, dir);
  assert.strictEqual(count, 2);
  assert.ok(uploaded.includes("chart-1.0.0.tgz"));
  assert.ok(uploaded.includes("ocm-linux-amd64"));
}

// Skips subdirectories
{
  const dir = tmpDir({ "chart.tgz": "data" });
  fs.mkdirSync(path.join(dir, "subdir"));
  const uploaded = [];
  const gh = mockGitHub({
    listReleaseAssets: async () => ({ data: [] }),
    uploadReleaseAsset: async (opts) => { uploaded.push(opts.name); return { data: { state: "uploaded", size: opts.data.length } }; },
  });
  const count = await uploadAssets(gh, mockContext, mockCore(), 1, dir);
  assert.strictEqual(count, 1);
  assert.deepStrictEqual(uploaded, ["chart.tgz"]);
}

// Throws when the server reports a truncated or unfinished upload
{
  const dir = tmpDir({ "chart.tgz": "data" });
  const gh = mockGitHub({
    listReleaseAssets: async () => ({ data: [] }),
    uploadReleaseAsset: async () => ({ data: { state: "uploaded", size: 1 } }),
  });
  await assert.rejects(
    () => uploadAssets(gh, mockContext, mockCore(), 1, dir),
    /upload unverified/,
  );
}

// ----------------------------------------------------------
// writeSummary tests
// ----------------------------------------------------------

// Does not throw and calls write() — latest + previous both set
{
  let written = false;
  const core = mockCore();
  core.summary.write = async () => { written = true; };
  await writeSummary(core, {
    newReleaseTag: "v1.0.0",
    rcTag: "v1.0.0-rc.1",
    newReleaseVersion: "1.0.0",
    componentName: "OCM",
    imageRepo: "ghcr.io/org/img",
    chartRepo: "ghcr.io/org/chart",
    imageDigest: "sha256:abc123def456789012345",
    isLatest: true,
    highestPreviousReleaseVersion: "0.9.0",
    uploadedCount: 2,
    releaseUrl: "https://example.com",
  });
  assert.ok(written, "summary.write() should have been called");
}

// Handles missing optional fields gracefully — neither latest nor previous
{
  let written = false;
  const core = mockCore();
  core.summary.write = async () => { written = true; };
  await writeSummary(core, {
    newReleaseTag: "v1.0.0",
    rcTag: "v1.0.0-rc.1",
    newReleaseVersion: "1.0.0",
    componentName: "OCM",
    imageRepo: "",
    chartRepo: "",
    imageDigest: "",
    isLatest: false,
    highestPreviousReleaseVersion: "",
    uploadedCount: 0,
    releaseUrl: "https://example.com",
  });
  assert.ok(written, "summary.write() should have been called");
}

console.log("✅ All publish-final-release tests passed.");
