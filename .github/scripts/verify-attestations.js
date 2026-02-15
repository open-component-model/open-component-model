// @ts-check
import fs from "fs";
import path from "path";
import { parsePatterns, findAssets, runCmd } from "./attestation-utils.js";

/** Load attestations index from assets directory. */
function loadIndex(assetsDir) {
  const indexPath = path.join(assetsDir, "attestations-index.json");
  if (!fs.existsSync(indexPath)) {
    throw new Error(`Attestations index not found: ${indexPath}`);
  }
  const index = JSON.parse(fs.readFileSync(indexPath, "utf8"));
  if (!index.attestations || !Array.isArray(index.attestations)) {
    throw new Error("Invalid attestations index: missing attestations array");
  }
  return index;
}

/** Find bundle path for a subject from the index. */
function findBundle(assetsDir, index, subject) {
  const entry = index.attestations.find((a) => a.subject === subject);
  if (!entry) {
    throw new Error(`No attestation entry found for subject: ${subject}`);
  }
  const bundlePath = path.join(assetsDir, entry.bundle);
  if (!fs.existsSync(bundlePath)) {
    throw new Error(`Attestation bundle not found: ${bundlePath}`);
  }
  return bundlePath;
}

/**
 * Verify attestations from an RC release before final promotion.
 * Verifies all matching binaries and the OCI image from the index.
 */
export async function runVerify({ core, run = runCmd } = {}) {
  const assetsDir = process.env.ASSETS_DIR;
  const assetPatterns = process.env.ASSET_PATTERNS;
  const repository = process.env.REPOSITORY || process.env.GITHUB_REPOSITORY;

  if (!assetsDir || !assetPatterns || !repository) {
    throw new Error("Missing required env: ASSETS_DIR, ASSET_PATTERNS, REPOSITORY");
  }

  const patterns = parsePatterns(assetPatterns);
  const assets = findAssets(assetsDir, patterns);
  const index = loadIndex(assetsDir);

  core?.info(`Loaded index with ${index.attestations.length} entries, found ${assets.length} assets`);

  let verifiedCount = 0;

  // Verify binary attestations
  for (const asset of assets) {
    const subject = path.basename(asset);
    const bundle = findBundle(assetsDir, index, subject);
    core?.info(`Verifying ${subject}...`);
    run("gh", ["attestation", "verify", asset, "--repo", repository, "--bundle", bundle]);
    core?.info(`✅ ${subject}`);
    verifiedCount++;
  }

  // Verify OCI image using digest from index (tags are mutable!)
  if (index.image?.ref && index.image?.digest) {
    const repoWithoutTag = index.image.ref.split(":")[0];
    const ociRef = `oci://${repoWithoutTag}@${index.image.digest}`;
    const bundle = findBundle(assetsDir, index, index.image.ref);
    core?.info(`Verifying OCI image ${ociRef}...`);
    run("gh", ["attestation", "verify", ociRef, "--repo", repository, "--bundle", bundle]);
    core?.info(`✅ OCI image`);
    verifiedCount++;
  }

  core?.info(`✅ All ${verifiedCount} attestations verified`);
  core?.setOutput("verified_count", String(verifiedCount));
  core?.setOutput("verified_image_digest", index.image?.digest || "");
}

/** @param {import('@actions/github-script').AsyncFunctionArguments} args */
export default async function main({ core }) {
  try {
    await runVerify({ core });
  } catch (err) {
    core.setFailed(err.message);
  }
}