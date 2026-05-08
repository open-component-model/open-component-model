import assert from "assert";
import fs from "fs";
import path from "path";
import os from "os";
import {
  prepareReleaseNotes,
  getOrCreateRelease,
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
        createRelease: overrides.createRelease || (() => ({ data: { id: 1, html_url: "https://example.com/release/1" } })),
        updateRelease: overrides.updateRelease || (() => ({ data: { id: 1, html_url: "https://example.com/release/1" } })),
        listReleaseAssets: overrides.listReleaseAssets || (() => ({ data: [] })),
        deleteReleaseAsset: overrides.deleteReleaseAsset || (() => {}),
        uploadReleaseAsset: overrides.uploadReleaseAsset || (() => {}),
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
  const result = prepareReleaseNotes("/nonexistent/path.md", "rc-tag", "final-tag");
  assert.strictEqual(result, "Promoted from rc-tag");
}

// Returns fallback when file is empty
{
  const dir = tmpDir({ "empty.md": "" });
  const result = prepareReleaseNotes(path.join(dir, "empty.md"), "rc-tag", "final-tag");
  assert.strictEqual(result, "Promoted from rc-tag");
}

// Rewrites git-cliff header line for controller tags
{
  const dir = tmpDir({ "notes.md": "## [kubernetes/controller/v0.1.0-rc.1] - 2025-01-01\n\n- Some change" });
  const result = prepareReleaseNotes(
    path.join(dir, "notes.md"),
    "kubernetes/controller/v0.1.0-rc.1",
    "kubernetes/controller/v0.1.0",
  );
  const today = new Date().toISOString().split("T")[0];
  assert.ok(
    result.startsWith(`## [kubernetes/controller/v0.1.0] - promoted from [kubernetes/controller/v0.1.0-rc.1] on ${today}`),
    `Expected header rewrite, got: ${result.split("\n")[0]}`,
  );
  assert.ok(result.includes("- Some change"), "Body should be preserved");
}

// Rewrites git-cliff header line for CLI tags
{
  const dir = tmpDir({ "notes.md": "## [cli/v0.17.0-rc.1] - 2025-02-02\n\n- Fix bug" });
  const result = prepareReleaseNotes(
    path.join(dir, "notes.md"),
    "cli/v0.17.0-rc.1",
    "cli/v0.17.0",
  );
  const today = new Date().toISOString().split("T")[0];
  assert.ok(
    result.startsWith(`## [cli/v0.17.0] - promoted from [cli/v0.17.0-rc.1] on ${today}`),
    `Expected header rewrite for CLI, got: ${result.split("\n")[0]}`,
  );
}

// Prepends header when notes don't match the RC header pattern
{
  const dir = tmpDir({ "notes.md": "Just some plain notes\n\n- Fix bug" });
  const today = new Date().toISOString().split("T")[0];
  const result = prepareReleaseNotes(path.join(dir, "notes.md"), "rc-tag", "final-tag");
  assert.ok(
    result.startsWith(`## [final-tag] - promoted from [rc-tag] on ${today}`),
    `Expected prepended header, got: ${result.split("\n")[0]}`,
  );
  assert.ok(result.includes("- Fix bug"), "Original body should be preserved");
}

// Truncates body when it exceeds GitHub's 125000-char release body limit
{
  const oversize = "## [v0.7.0-rc.1] - 2026-05-08\n\n" + "x".repeat(130000);
  const dir = tmpDir({ "huge.md": oversize });
  const result = prepareReleaseNotes(path.join(dir, "huge.md"), "v0.7.0-rc.1", "v0.7.0");
  assert.strictEqual(result.length, 120000, `Expected exact MAX_RELEASE_BODY_LENGTH (120000), got: ${result.length}`)
  assert.ok(result.endsWith("complete history.*"), "Expected truncation notice as suffix");
  assert.ok(result.startsWith("## [v0.7.0]"), "Expected rewritten header to remain intact");
}

// Does not truncate body when within limit
{
  const fits = "## [v0.7.0-rc.1] - 2026-05-08\n\nSmall body";
  const dir = tmpDir({ "small.md": fits });
  const result = prepareReleaseNotes(path.join(dir, "small.md"), "v0.7.0-rc.1", "v0.7.0");
  assert.ok(!result.includes("Release notes truncated"), "Expected no truncation notice for small body");
}

// ----------------------------------------------------------
// getOrCreateRelease tests
// ----------------------------------------------------------

// Creates release when none exists (404 path)
{
  const calls = [];
  const gh = mockGitHub({
    createRelease: async (opts) => {
      calls.push({ method: "create", opts });
      return { data: { id: 42, html_url: "https://example.com/42" } };
    },
  });
  const result = await getOrCreateRelease(gh, mockContext, {
    newReleaseTag: "v1.0.0",
    newReleaseVersion: "1.0.0",
    componentName: "OCM Controller",
    notes: "notes",
    isLatest: true,
  });
  assert.strictEqual(result.id, 42);
  assert.strictEqual(calls.length, 1);
  assert.strictEqual(calls[0].opts.make_latest, "true");
  assert.strictEqual(calls[0].opts.name, "OCM Controller 1.0.0");
}

// Updates existing release when tag already exists
{
  const calls = [];
  const gh = mockGitHub({
    getReleaseByTag: async () => ({ data: { id: 10 } }),
    updateRelease: async (opts) => {
      calls.push({ method: "update", opts });
      return { data: { id: 10, html_url: "https://example.com/10" } };
    },
  });
  const result = await getOrCreateRelease(gh, mockContext, {
    newReleaseTag: "v1.0.0",
    newReleaseVersion: "1.0.0",
    componentName: "OCM CLI",
    notes: "notes",
    isLatest: false,
  });
  assert.strictEqual(result.id, 10);
  assert.strictEqual(calls.length, 1);
  assert.strictEqual(calls[0].opts.make_latest, "false");
  assert.strictEqual(calls[0].opts.name, "OCM CLI 1.0.0");
}

// Rethrows non-404 errors
{
  const gh = mockGitHub({
    getReleaseByTag: async () => { throw Object.assign(new Error("Server Error"), { status: 500 }); },
  });
  await assert.rejects(
    () => getOrCreateRelease(gh, mockContext, {
      newReleaseTag: "v1.0.0", newReleaseVersion: "1.0.0", componentName: "Test", notes: "", isLatest: false,
    }),
    (err) => err.status === 500,
  );
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
    uploadReleaseAsset: async (opts) => uploaded.push(opts.name),
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
    uploadReleaseAsset: async (opts) => uploaded.push(opts.name),
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
    uploadReleaseAsset: async (opts) => uploaded.push(opts.name),
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
    uploadReleaseAsset: async (opts) => uploaded.push(opts.name),
  });
  const count = await uploadAssets(gh, mockContext, mockCore(), 1, dir);
  assert.strictEqual(count, 1);
  assert.deepStrictEqual(uploaded, ["chart.tgz"]);
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
    componentName: "OCM Controller",
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
    componentName: "OCM CLI",
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

// Latest set but not release version (e.g., promoting an RC that is highest overall)
{
  let written = false;
  const core = mockCore();
  core.summary.write = async () => { written = true; };
  await writeSummary(core, {
    newReleaseTag: "v1.0.0",
    rcTag: "v1.0.0-rc.1",
    newReleaseVersion: "1.0.0",
    componentName: "OCM Controller",
    imageRepo: "ghcr.io/org/img",
    chartRepo: "ghcr.io/org/chart",
    imageDigest: "sha256:abc123def456789012345",
    isLatest: true,
    highestPreviousReleaseVersion: "1.1.0",
    uploadedCount: 1,
    releaseUrl: "https://example.com",
  });
  assert.ok(written, "summary.write() should have been called");
}

console.log("✅ All publish-final-release tests passed.");
