// @ts-check
import { execSync } from "child_process";
import { parseVersionArray } from './semver-utils.js';


// --------------------------
// GitHub Actions entrypoint
// --------------------------
// noinspection JSUnusedGlobalSymbols
/** @param {import('@actions/github-script').AsyncFunctionArguments} args */
export default async function computeRcVersion({ core }) {
    const componentPath = process.env.COMPONENT_PATH;
    const releaseBranch = process.env.BRANCH;
    if (!componentPath || !releaseBranch) {
        core.setFailed("Missing COMPONENT_PATH or BRANCH");
        return;
    }

    const basePrefix = parseBranch(releaseBranch);
    const tagPrefix = `${componentPath}/v`;

    const latestStable = run(core, `git tag --list '${tagPrefix}${basePrefix}.*' | sort -V | tail -n1`);
    const latestRc = run(core, `git tag --list '${tagPrefix}${basePrefix}.*-rc.*' | sort -V | tail -n1`);

    const { baseVersion, rcVersion } = computeNextVersions(basePrefix, latestStable, latestRc);

    const rcTag = `${tagPrefix}${rcVersion}`;
    const promotionTag = `${tagPrefix}${baseVersion}`;

    core.setOutput("new_tag", rcTag);
    core.setOutput("new_version", rcVersion);
    core.setOutput("base_version", baseVersion);
    core.setOutput("promotion_tag", promotionTag);

    // --------------------------
    // Step summary
    // --------------------------
    await core.summary
        .addHeading("📦 RC Version Computation")
        .addTable([
            [
                { data: "Field", header: true },
                { data: "Value", header: true },
            ],
            ["Component Path", componentPath],
            ["Release Branch", releaseBranch],
            ["Base Prefix", basePrefix],
            ["Latest Stable", latestStable || "(none)"],
            ["Latest RC", latestRc || "(none)"],
            ["Next Base Version", baseVersion],
            ["Next RC Version", rcVersion],
            ["RC Tag", rcTag],
            ["Promotion Tag", promotionTag],
        ])
        .write();
}

// --------------------------
// Core helpers
// --------------------------
export function run(core, cmd) {
  core.info(`> ${cmd}`);
  try {
    const out = execSync(cmd).toString().trim();
    if (out) core.info(`Output: ${out}`);
    return out;
  } catch (err) {
    core.warning(`Command failed: ${cmd}\n${err.message}`);
    return "";
  }
}

export function parseBranch(branch) {
  const match = /^releases\/v(0\.\d+)/.exec(branch);
  if (!match) throw new Error(`Invalid branch format: ${branch}`);
  return match[1];
}

/**
 * Compute the next base and RC (release candidate) versions for a component.
 *
 * Versioning rules:
 *  - If no stable or RC tags exist: start fresh from the given base prefix (e.g., "0.1" → 0.1.0, 0.1.0-rc.1).
 *  - If only a stable tag exists: bump the patch version and start RC sequence (e.g., 0.1.0 → 0.1.1, 0.1.1-rc.1).
 *  - If only RC tags exist: continue RC numbering (e.g., 0.1.1-rc.2 → 0.1.1, 0.1.1-rc.3).
 *  - If both exist and share the same base: continue RC numbering for that version (e.g., 0.1.1 and 0.1.1-rc.4 → 0.1.1, 0.1.1-rc.5).
 *  - If the stable tag is newer: bump patch and start new RC sequence (e.g., 0.1.2 and 0.1.1-rc.4 -> 0.1.3, 0.1.3-rc.1).
 *  - If the RC tag is newer: continue RC numbering with its base version (e.g., 0.1.1 and 0.1.2-rc.6 -> 0.1.2, 0.1.2-rc.7).
 *
 * @param {string} basePrefix - Branch base prefix (e.g., "0.1" from "releases/v0.1").
 * @param {string} [latestStableTag] - Most recent stable tag (e.g., "cli/v0.1.0").
 * @param {string} [latestRcTag] - Most recent RC tag (e.g., "cli/v0.1.1-rc.2").
 * @returns {{ baseVersion: string, rcVersion: string }}
 *   baseVersion: The semantic base version (e.g., "0.1.1").
 *   rcVersion: The computed RC tag (e.g., "0.1.1-rc.3").
 */
export function computeNextVersions(basePrefix, latestStableTag, latestRcTag) {
    const parseTag = tag => parseVersionArray(tag).join(".");
    const extractRcNumber = tag => parseInt(tag?.match(/-rc\.(\d+)/)?.[1] ?? "0", 10);
    const incrementPatch = ([maj, min, pat]) => [maj, min, pat + 1];

    const stableVersionParts = parseVersionArray(latestStableTag);
    const rcVersionParts = parseVersionArray(latestRcTag);

    let [major, minor, patch] =
        stableVersionParts.length > 0
            ? stableVersionParts
            : basePrefix.split(".").map(Number).concat(0).slice(0, 3);

    let nextBaseVersion = `${major}.${minor}.${patch}`;
    let nextRcNumber = 1;

    switch (true) {
        // No existing versions → start from base prefix
        case !latestStableTag && !latestRcTag:
            break;

        // First RC after last stable release
        case latestStableTag && !latestRcTag:
            [major, minor, patch] = incrementPatch([major, minor, patch]);
            nextBaseVersion = `${major}.${minor}.${patch}`;
            break;

        // Only RCs so far → continue RC numbering
        case !latestStableTag && latestRcTag:
            nextRcNumber = extractRcNumber(latestRcTag) + 1;
            nextBaseVersion = parseTag(latestRcTag);
            [major, minor, patch] = rcVersionParts;
            break;

        // Same base between stable and RC
        case parseTag(latestStableTag) === parseTag(latestRcTag):
            nextRcNumber = extractRcNumber(latestRcTag) + 1;
            nextBaseVersion = parseTag(latestStableTag);
            break;

        // Stable newer → start new patch RC
        case isStableNewer(latestStableTag, latestRcTag):
            [major, minor, patch] = incrementPatch([major, minor, patch]);
            nextBaseVersion = `${major}.${minor}.${patch}`;
            nextRcNumber = 1;
            break;

        // RC newer → continue RC sequence
        default:
            nextRcNumber = extractRcNumber(latestRcTag) + 1;
            [major, minor, patch] = rcVersionParts;
            nextBaseVersion = parseTag(latestStableTag) || `${major}.${minor}.${patch}`;
    }

    return {
        baseVersion: nextBaseVersion,
        rcVersion: `${major}.${minor}.${patch}-rc.${nextRcNumber}`,
    };
}

/**
 * Determine whether the latest stable tag is newer than the latest RC tag.
 */
export function isStableNewer(stable, rc) {
    if (!stable) return false;
    if (!rc) return true;

    const stableParts = parseVersionArray(stable);
    const rcParts = parseVersionArray(rc);

    // Compare [major, minor, patch] lexicographically
    for (let i = 0; i < 3; i++) {
        const s = stableParts[i] || 0;
        const r = rcParts[i] || 0;
        if (s > r) return true;
        if (s < r) return false;
    }

    // Same base version → stable is not newer than RC
    return false;
}