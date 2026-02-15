// @ts-check
import fs from "fs";
import path from "path";
import { sha256File, parsePatterns, findAssets, runCmd } from "./attestation-utils.js";

/** Create human-readable attestation bundle file name from asset name. */
function bundleNameForAsset(assetPath) {
  return `attestation-${path.basename(assetPath)}.jsonl`;
}

/**
 * Export attestation bundles for RC releases.
 * Downloads from GitHub for all matching binaries and the OCI image.
 * Creates attestations-index.json for later verification.
 */
export async function runExport({ core, run = runCmd } = {}) {
  const assetsDir = process.env.ASSETS_DIR;
  const assetPatterns = process.env.ASSET_PATTERNS;
  const imageDigest = process.env.IMAGE_DIGEST;
  const imageTag = process.env.IMAGE_TAG;
  const targetRepo = process.env.TARGET_REPO;
  const outputDir = process.env.OUTPUT_DIR;
  const repository = process.env.REPOSITORY || process.env.GITHUB_REPOSITORY;

  if (!assetsDir || !assetPatterns || !outputDir || !repository) {
    throw new Error("Missing required env: ASSETS_DIR, ASSET_PATTERNS, OUTPUT_DIR, REPOSITORY");
  }
  if (!imageDigest || !targetRepo) {
    throw new Error("Missing required env: IMAGE_DIGEST, TARGET_REPO");
  }

  fs.mkdirSync(outputDir, { recursive: true });
  const patterns = parsePatterns(assetPatterns);
  const assets = findAssets(assetsDir, patterns);
  const attestations = [];

  core?.info(`Found ${assets.length} assets to export attestations for`);

  // Export binary attestations
  for (const asset of assets) {
    const digest = sha256File(asset);
    const bundleName = bundleNameForAsset(asset);
    const bundlePath = path.join(outputDir, bundleName);

    core?.info(`Downloading attestation for ${path.basename(asset)}...`);
    run("gh", ["attestation", "download", asset, "--repo", repository, "--limit", "100"], { cwd: outputDir });

    const digestBundlePath = path.join(outputDir, `${digest}.jsonl`);
    if (!fs.existsSync(digestBundlePath)) {
      throw new Error(`Attestation bundle not found after download: ${digestBundlePath}`);
    }
    fs.renameSync(digestBundlePath, bundlePath);

    attestations.push({ subject: path.basename(asset), type: "binary", digest, bundle: bundleName });
  }

  // Export OCI image attestation
  const imageRef = `${targetRepo}:${imageTag}`;
  const imageBundleName = "attestation-ocm-oci-image.jsonl";
  const imageBundlePath = path.join(outputDir, imageBundleName);

  core?.info(`Downloading attestation for OCI image ${imageRef}...`);
  run("gh", ["attestation", "download", `oci://${targetRepo}@${imageDigest}`, "--repo", repository, "--limit", "100"], { cwd: outputDir });

  const imageDigestBundlePath = path.join(outputDir, `${imageDigest}.jsonl`);
  if (!fs.existsSync(imageDigestBundlePath)) {
    throw new Error(`OCI image attestation bundle not found: ${imageDigestBundlePath}`);
  }
  fs.renameSync(imageDigestBundlePath, imageBundlePath);

  attestations.push({ subject: imageRef, type: "oci-image", digest: imageDigest, bundle: imageBundleName });

  // Write index
  const index = {
    version: "1",
    generated_at: new Date().toISOString(),
    rc_version: imageTag,
    image: { ref: imageRef, digest: imageDigest },
    attestations,
  };
  const indexPath = path.join(outputDir, "attestations-index.json");
  fs.writeFileSync(indexPath, JSON.stringify(index, null, 2));

  core?.info(`✅ Exported ${attestations.length} attestation bundles`);
  core?.setOutput("bundle_count", String(attestations.length));
  core?.setOutput("index_path", indexPath);
}

/** @param {import('@actions/github-script').AsyncFunctionArguments} args */
export default async function main({ core }) {
  try {
    await runExport({ core });
  } catch (err) {
    core.setFailed(err.message);
  }
}