// @ts-check
/**
 * Tests for verify-attestations.js
 *
 * These tests validate the helper functions used in the attestation verification flow.
 * The main runVerify function requires GitHub CLI and is tested via workflow runs.
 */
import { strict as assert } from "node:assert";
import { parsePatterns, findAssets, loadIndex, findBundle, sha256File } from "./verify-attestations.js";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";

// ─────────────────────────────────────────────────────────────
// parsePatterns - validates JSON array input for asset patterns
// ─────────────────────────────────────────────────────────────

console.log("Testing parsePatterns...");

// Valid JSON array should parse correctly
assert.deepEqual(parsePatterns('["ocm-*"]'), ["ocm-*"]);
assert.deepEqual(parsePatterns('["ocm-*", "cli.tar"]'), ["ocm-*", "cli.tar"]);

// Invalid inputs should throw
assert.throws(() => parsePatterns("not-json"), /Invalid ASSET_PATTERNS/);
assert.throws(() => parsePatterns("[]"), /non-empty/);

console.log("✅ parsePatterns tests passed");

// ─────────────────────────────────────────────────────────────
// loadIndex - loads and validates attestations index
// ─────────────────────────────────────────────────────────────

console.log("Testing loadIndex...");

// Create temp directory with valid index
const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "verify-test-"));

// Valid index should load
const validIndex = {
  version: "1",
  generated_at: "2026-01-01T00:00:00.000Z",
  rc_version: "0.8.0-rc.1",
  image: { ref: "ghcr.io/owner/cli:0.8.0-rc.1", digest: "sha256:abc123" },
  attestations: [
    { subject: "ocm-linux-amd64", type: "binary", digest: "sha256:def456", bundle: "attestation-ocm-linux-amd64.jsonl" },
  ],
};
fs.writeFileSync(path.join(tmpDir, "attestations-index.json"), JSON.stringify(validIndex));

try {
  const loaded = loadIndex(tmpDir);
  assert.equal(loaded.version, "1");
  assert.equal(loaded.attestations.length, 1);

  // Should throw if index file doesn't exist
  assert.throws(() => loadIndex("/nonexistent/path"), /not found/);

  // Should throw if index has invalid format (missing attestations array)
  const badIndexDir = fs.mkdtempSync(path.join(os.tmpdir(), "bad-index-"));
  fs.writeFileSync(path.join(badIndexDir, "attestations-index.json"), "{}");
  try {
    assert.throws(() => loadIndex(badIndexDir), /missing attestations/);
  } finally {
    fs.rmSync(badIndexDir, { recursive: true });
  }

  console.log("✅ loadIndex tests passed");
} finally {
  fs.rmSync(tmpDir, { recursive: true });
}

// ─────────────────────────────────────────────────────────────
// findBundle - locates bundle file for a subject from index
// ─────────────────────────────────────────────────────────────

console.log("Testing findBundle...");

// Create temp directory with index and bundle files
const bundleDir = fs.mkdtempSync(path.join(os.tmpdir(), "bundle-test-"));
const testIndex = {
  attestations: [
    { subject: "ocm-linux-amd64", bundle: "attestation-ocm-linux-amd64.jsonl" },
    { subject: "ghcr.io/owner/cli:0.8.0-rc.1", bundle: "attestation-ocm-image.jsonl" },
  ],
};
fs.writeFileSync(path.join(bundleDir, "attestation-ocm-linux-amd64.jsonl"), "{}");
fs.writeFileSync(path.join(bundleDir, "attestation-ocm-image.jsonl"), "{}");

try {
  // Should find existing bundle by subject
  const bundlePath = findBundle(bundleDir, testIndex, "ocm-linux-amd64");
  assert(bundlePath.endsWith("attestation-ocm-linux-amd64.jsonl"));

  // Should find image bundle
  const imageBundlePath = findBundle(bundleDir, testIndex, "ghcr.io/owner/cli:0.8.0-rc.1");
  assert(imageBundlePath.endsWith("attestation-ocm-image.jsonl"));

  // Should throw if subject not in index
  assert.throws(() => findBundle(bundleDir, testIndex, "unknown-subject"), /No attestation entry/);

  // Should throw if bundle file doesn't exist
  const missingIndex = { attestations: [{ subject: "test", bundle: "missing.jsonl" }] };
  assert.throws(() => findBundle(bundleDir, missingIndex, "test"), /bundle not found/);

  console.log("✅ findBundle tests passed");
} finally {
  fs.rmSync(bundleDir, { recursive: true });
}

// ─────────────────────────────────────────────────────────────
// findAssets - resolves files from glob patterns
// ─────────────────────────────────────────────────────────────

console.log("Testing findAssets...");

// Create temp directory with test files
const assetsDir = fs.mkdtempSync(path.join(os.tmpdir(), "assets-test-"));
fs.writeFileSync(path.join(assetsDir, "ocm-linux-amd64"), "test content");
fs.writeFileSync(path.join(assetsDir, "ocm-darwin-arm64"), "test content");
fs.writeFileSync(path.join(assetsDir, "cli.tar"), "tar content");
fs.writeFileSync(path.join(assetsDir, "attestations-index.json"), "{}");

try {
  // Should find files matching patterns
  const assets = findAssets(assetsDir, ["ocm-*"]);
  assert.equal(assets.length, 2);

  // Should exclude non-matching files
  assert(!assets.some((a) => a.includes("cli.tar")));
  assert(!assets.some((a) => a.includes("attestations-index.json")));

  // Should throw if pattern matches nothing
  assert.throws(() => findAssets(assetsDir, ["nonexistent-*"]), /did not match/);

  console.log("✅ findAssets tests passed");
} finally {
  fs.rmSync(assetsDir, { recursive: true });
}

console.log("\n✅ All verify-attestations tests passed!");