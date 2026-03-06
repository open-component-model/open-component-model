import assert from "assert";
import fs from "fs";
import path from "path";
import os from "os";
import {
  prepareReleaseNotes,
  getOrCreateRelease,
  uploadChartAssets,
  writeSummary,
} from "./publish-final-release.js";

// ----------------------------------------------------------
// Helpers
// ----------------------------------------------------------

/** Create a temp directory with optional files. */
function tmpDir(files = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pfr-test-"));
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

// Rewrites header line with final tag and today's date
{
  const dir = tmpDir({ "notes.md": "[controller/v0.1.0-rc.1] - 2025-01-01\n\n- Some change" });
  const result = prepareReleaseNotes(
    path.join(dir, "notes.md"),
    "controller/v0.1.0-rc.1",
    "controller/v0.1.0",
  );
  const today = new Date().toISOString().split("T")[0];
  assert.ok(
    result.startsWith(`[controller/v0.1.0] - promoted from [controller/v0.1.0-rc.1] on ${today}`),
    `Expected header rewrite, got: ${result.split("\n")[0]}`,
  );
  assert.ok(result.includes("- Some change"), "Body should be preserved");
}

// Preserves notes that don't match the header pattern
{
  const dir = tmpDir({ "notes.md": "Just some plain notes\n\n- Fix bug" });
  const result = prepareReleaseNotes(path.join(dir, "notes.md"), "rc", "final");
  assert.strictEqual(result, "Just some plain notes\n\n- Fix bug");
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
    finalTag: "v1.0.0",
    finalVersion: "1.0.0",
    notes: "notes",
    isLatest: true,
  });
  assert.strictEqual(result.id, 42);
  assert.strictEqual(calls.length, 1);
  assert.strictEqual(calls[0].opts.make_latest, "true");
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
    finalTag: "v1.0.0",
    finalVersion: "1.0.0",
    notes: "notes",
    isLatest: false,
  });
  assert.strictEqual(result.id, 10);
  assert.strictEqual(calls.length, 1);
  assert.strictEqual(calls[0].opts.make_latest, "false");
}

// Rethrows non-404 errors
{
  const gh = mockGitHub({
    getReleaseByTag: async () => { throw Object.assign(new Error("Server Error"), { status: 500 }); },
  });
  await assert.rejects(
    () => getOrCreateRelease(gh, mockContext, {
      finalTag: "v1.0.0", finalVersion: "1.0.0", notes: "", isLatest: false,
    }),
    (err) => err.status === 500,
  );
}

// ----------------------------------------------------------
// uploadChartAssets tests
// ----------------------------------------------------------

// Uploads new files
{
  const dir = tmpDir({ "chart-1.0.0.tgz": "fake-chart-data" });
  const uploaded = [];
  const gh = mockGitHub({
    listReleaseAssets: async () => ({ data: [] }),
    uploadReleaseAsset: async (opts) => uploaded.push(opts.name),
  });
  await uploadChartAssets(gh, mockContext, mockCore(), 1, dir);
  assert.deepStrictEqual(uploaded, ["chart-1.0.0.tgz"]);
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
  await uploadChartAssets(gh, mockContext, mockCore(), 1, dir);
  assert.deepStrictEqual(deleted, [99]);
  assert.deepStrictEqual(uploaded, ["chart-1.0.0.tgz"]);
}

// Ignores non-.tgz files
{
  const dir = tmpDir({ "chart-1.0.0.tgz": "data", "README.md": "ignore me" });
  const uploaded = [];
  const gh = mockGitHub({
    listReleaseAssets: async () => ({ data: [] }),
    uploadReleaseAsset: async (opts) => uploaded.push(opts.name),
  });
  await uploadChartAssets(gh, mockContext, mockCore(), 1, dir);
  assert.deepStrictEqual(uploaded, ["chart-1.0.0.tgz"]);
}

// ----------------------------------------------------------
// writeSummary tests
// ----------------------------------------------------------

// Does not throw and calls write()
{
  let written = false;
  const core = mockCore();
  core.summary.write = async () => { written = true; };
  await writeSummary(core, {
    finalTag: "v1.0.0",
    rcTag: "v1.0.0-rc.1",
    finalVersion: "1.0.0",
    imageRepo: "ghcr.io/org/img",
    chartRepo: "ghcr.io/org/chart",
    imageDigest: "sha256:abc123def456789012345",
    isLatest: true,
    highestFinalVersion: "0.9.0",
    releaseUrl: "https://example.com",
  });
  assert.ok(written, "summary.write() should have been called");
}

// Handles missing imageDigest gracefully
{
  let written = false;
  const core = mockCore();
  core.summary.write = async () => { written = true; };
  await writeSummary(core, {
    finalTag: "v1.0.0",
    rcTag: "v1.0.0-rc.1",
    finalVersion: "1.0.0",
    imageRepo: "ghcr.io/org/img",
    chartRepo: "ghcr.io/org/chart",
    imageDigest: "",
    isLatest: false,
    highestFinalVersion: "",
    releaseUrl: "https://example.com",
  });
  assert.ok(written, "summary.write() should have been called");
}

console.log("✅ All publish-final-release tests passed.");
