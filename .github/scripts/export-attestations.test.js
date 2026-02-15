// @ts-check
/**
 * Tests for export-attestations.js
 *
 * These tests validate the helper functions used in the attestation export flow.
 * The main runExport function requires GitHub CLI and is tested via workflow runs.
 */
import { strict as assert } from "node:assert";
import { parsePatterns, findAssets, bundleNameForAsset, sha256File } from "./export-attestations.js";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";

// ─────────────────────────────────────────────────────────────
// parsePatterns - validates JSON array input for asset patterns
// ─────────────────────────────────────────────────────────────

console.log("Testing parsePatterns...");

// Valid JSON array should parse correctly
assert.deepEqual(parsePatterns('["ocm-*"]'), ["ocm-*"]);
assert.deepEqual(parsePatterns('["bin/ocm-*", "lib/*.so"]'), ["bin/ocm-*", "lib/*.so"]);

// Invalid inputs should throw
assert.throws(() => parsePatterns("not-json"), /Invalid ASSET_PATTERNS/);
assert.throws(() => parsePatterns("[]"), /non-empty/);
assert.throws(() => parsePatterns('[""]'), /non-empty/);
assert.throws(() => parsePatterns("[123]"), /non-empty/);

console.log("✅ parsePatterns tests passed");

// ─────────────────────────────────────────────────────────────
// bundleNameForAsset - creates human-readable bundle names
// ─────────────────────────────────────────────────────────────

console.log("Testing bundleNameForAsset...");

// Should create attestation-{name}.jsonl from file path
assert.equal(bundleNameForAsset("/tmp/bin/ocm-linux-amd64"), "attestation-ocm-linux-amd64.jsonl");
assert.equal(bundleNameForAsset("ocm-darwin-arm64"), "attestation-ocm-darwin-arm64.jsonl");
assert.equal(bundleNameForAsset("/path/to/my-binary"), "attestation-my-binary.jsonl");

console.log("✅ bundleNameForAsset tests passed");

// ─────────────────────────────────────────────────────────────
// findAssets - resolves files from glob patterns
// ─────────────────────────────────────────────────────────────

console.log("Testing findAssets...");

// Create temp directory with test files
const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "attestation-test-"));
fs.writeFileSync(path.join(tmpDir, "ocm-linux-amd64"), "test content");
fs.writeFileSync(path.join(tmpDir, "ocm-darwin-arm64"), "test content");
fs.writeFileSync(path.join(tmpDir, "readme.txt"), "other file");

try {
  // Should find files matching pattern
  const assets = findAssets(tmpDir, ["ocm-*"]);
  assert.equal(assets.length, 2);
  assert(assets.some((a) => a.endsWith("ocm-linux-amd64")));
  assert(assets.some((a) => a.endsWith("ocm-darwin-arm64")));

  // Should throw if pattern matches nothing
  assert.throws(() => findAssets(tmpDir, ["nonexistent-*"]), /did not match/);

  // Should throw if directory doesn't exist
  assert.throws(() => findAssets("/nonexistent/path", ["*"]), /does not exist/);

  console.log("✅ findAssets tests passed");
} finally {
  // Cleanup temp directory
  fs.rmSync(tmpDir, { recursive: true });
}

// ─────────────────────────────────────────────────────────────
// sha256File - computes file digest
// ─────────────────────────────────────────────────────────────

console.log("Testing sha256File...");

// Create temp file with known content
const tmpFile = path.join(os.tmpdir(), "sha256-test-file");
fs.writeFileSync(tmpFile, "hello world");

try {
  const digest = sha256File(tmpFile);
  // "hello world" has a known sha256 hash
  assert(digest.startsWith("sha256:"));
  assert.equal(digest.length, 7 + 64); // "sha256:" + 64 hex chars
  assert.equal(digest, "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9");

  console.log("✅ sha256File tests passed");
} finally {
  fs.unlinkSync(tmpFile);
}

console.log("\n✅ All export-attestations tests passed!");