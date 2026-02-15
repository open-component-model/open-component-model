// @ts-check
import fs from "fs";
import path from "path";
import crypto from "crypto";
import { execFileSync } from "child_process";

/** Compute `sha256:<hex>` for a local file. */
export function sha256File(filePath) {
  const hash = crypto.createHash("sha256");
  hash.update(fs.readFileSync(filePath));
  return `sha256:${hash.digest("hex")}`;
}

/** Parse JSON array input and validate as non-empty string array. */
export function parsePatterns(json) {
  let parsed;
  try {
    parsed = JSON.parse(json);
  } catch {
    throw new Error(`Invalid ASSET_PATTERNS: ${json}`);
  }
  if (!Array.isArray(parsed) || parsed.length === 0 || parsed.some((v) => typeof v !== "string" || !v)) {
    throw new Error(`ASSET_PATTERNS must be a non-empty JSON array of non-empty strings`);
  }
  return parsed;
}

/** Resolve local files from glob patterns. Throws if any pattern has no matches. */
export function findAssets(assetsDir, patterns) {
  if (!fs.existsSync(assetsDir)) {
    throw new Error(`ASSETS_DIR does not exist: ${assetsDir}`);
  }
  const assets = new Set();
  for (const pattern of patterns) {
    const matches = fs.globSync(pattern, { cwd: assetsDir, nodir: true });
    if (matches.length === 0) {
      throw new Error(`Pattern '${pattern}' did not match any file under ${assetsDir}`);
    }
    for (const rel of matches) {
      assets.add(path.join(assetsDir, rel));
    }
  }
  return [...assets].sort();
}

/** Command wrapper for execFileSync, mockable in tests. */
export function runCmd(cmd, args, opts = {}) {
  return execFileSync(cmd, args, { encoding: "utf8", stdio: "pipe", ...opts }).trim();
}