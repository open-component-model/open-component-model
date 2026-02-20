// @ts-check
import { execFileSync } from "child_process";

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

    // Get all matching tags and filter/sort in JavaScript
    const allTags = run(core, "git", ["tag", "--list", `${tagPrefix}${basePrefix}.*`]);
    const tagList = allTags ? allTags.split("\n").filter(Boolean) : [];

    // Exclude RC tags here, otherwise latestStable may incorrectly resolve to e.g. v0.4.0-rc.3
    const stableTags = tagList.filter(tag => !/-rc\.\d+$/.test(tag));
    const latestStable = sortVersions(stableTags).pop() || "";

    // Get all RC tags
    const rcTags = tagList.filter(tag => /-rc\.\d+$/.test(tag));
    const latestRc = sortVersions(rcTags).pop() || "";

    core.info(`Latest stable: ${latestStable || "(none)"}`);
    core.info(`Latest RC: ${latestRc || "(none)"}`);

    const { baseVersion, rcVersion } = computeNextVersions(basePrefix, latestStable, latestRc, false);

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
/**
 * Run a shell command safely using execFileSync.
 * @param {*} core - GitHub Actions core module
 * @param {string} executable - The executable to run (e.g., "git", "grep")
 * @param {string[]} args - Array of arguments
 * @returns {string} Command output or empty string on failure
 */
export function run(core, executable, args) {
  const cmdStr = `${executable} ${args.join(" ")}`;
  core.info(`> ${cmdStr}`);
  try {
    const out = execFileSync(executable, args, { encoding: "utf-8" }).trim();
    if (out) core.info(`Output: ${out}`);
    return out;
  } catch (err) {
    core.warning(`Command failed: ${cmdStr}\n${err.message}`);
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
 *  - If both exist and share the same base: bump patch and start new RC sequence (e.g., 0.1.2 and 0.1.1-rc.4 -> 0.1.3, 0.1.3-rc.1).
 *  - If the stable tag is newer: bump patch and start new RC sequence (e.g., 0.1.2 and 0.1.1-rc.4 -> 0.1.3, 0.1.3-rc.1).
 *  - If the RC tag is newer: continue RC numbering with its base version (e.g., 0.1.1 and 0.1.2-rc.6 -> 0.1.2, 0.1.2-rc.7).
 *
 * @param {string} basePrefix - Branch base prefix (e.g., "0.1" from "releases/v0.1").
 * @param {string} [latestStableTag] - Most recent stable tag (e.g., "cli/v0.1.0").
 * @param {string} [latestRcTag] - Most recent RC tag (e.g., "cli/v0.1.1-rc.2").
 * @param {boolean} [bumpMinorVersion] - Bump minor version instead of patch version.
 * @returns {{ baseVersion: string, rcVersion: string }}
 *   baseVersion: The semantic base version (e.g., "0.1.1").
 *   rcVersion: The computed RC tag (e.g., "0.1.1-rc.3").
 */
export function computeNextVersions(basePrefix, latestStableTag, latestRcTag, bumpMinorVersion) {
    const parseTag = tag => parseVersion(tag).join(".");
    const extractRcNumber = tag => parseInt(tag?.match(/-rc\.(\d+)/)?.[1] ?? "0", 10);
    const incrementVersion = ([maj, min, pat]) => {
        if (bumpMinorVersion) {
            return [maj, min + 1, 0];
        }

        return [maj, min, pat + 1];
    };

    const stableVersionParts = parseVersion(latestStableTag);
    const rcVersionParts = parseVersion(latestRcTag);

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
            [major, minor, patch] = incrementVersion([major, minor, patch]);
            nextBaseVersion = `${major}.${minor}.${patch}`;
            break;

        // Only RCs so far → continue RC numbering
        case !latestStableTag && latestRcTag:
            nextRcNumber = extractRcNumber(latestRcTag) + 1;
            nextBaseVersion = parseTag(latestRcTag);
            [major, minor, patch] = rcVersionParts;
            break;

        // Same base between stable and RC → bump patch or minor and start new RC
        case parseTag(latestStableTag) === parseTag(latestRcTag):

        // Stable newer → bump patch or minor and start new RC
        case isStableNewer(latestStableTag, latestRcTag):
            [major, minor, patch] = incrementVersion([major, minor, patch]);
            nextBaseVersion = `${major}.${minor}.${patch}`;
            nextRcNumber = 1;
            break;

        // RC newer → continue RC sequence
        default:
            nextRcNumber = extractRcNumber(latestRcTag) + 1;
            [major, minor, patch] = rcVersionParts;
            nextBaseVersion = `${major}.${minor}.${patch}`;
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

    const stableParts = parseVersion(stable);
    const rcParts = parseVersion(rc);

    // Compare [major, minor, patch] numerically
    for (let i = 0; i < 3; i++) {
        const s = stableParts[i] || 0;
        const r = rcParts[i] || 0;
        if (s > r) return true;
        if (s < r) return false;
    }

    // Same base version → stable is not newer than RC
    return false;
}

/**
 * Parse a version tag into an array of version components.
 * Useful for version comparison and manipulation.
 *
 * @param {string} tag - Version tag (e.g., "cli/v0.1.2" or "cli/v0.1.2-rc.3")
 * @returns {number[]} Array of [major, minor, patch]
 */
export function parseVersion(tag) {
    if (!tag) return [];
    const version = tag.replace(/^.*v/, "").replace(/-rc\.\d+$/, "");
    return version.split(".").map(Number);
}

/**
 * Sort version tags in ascending order (similar to `sort -V`).
 * Handles both stable versions (v0.1.2) and RC versions (v0.1.2-rc.3).
 *
 * @param {string[]} tags - Array of version tags to sort
 * @returns {string[]} Sorted array of version tags
 */
export function sortVersions(tags) {
    return [...tags].sort((a, b) => {
        const partsA = parseVersion(a);
        const partsB = parseVersion(b);

        // Compare major.minor.patch numerically (early return ensures correct ordering)
        for (let i = 0; i < 3; i++) {
            const diff = (partsA[i] || 0) - (partsB[i] || 0);
            if (diff !== 0) return diff;
        }

        // If base versions are equal, compare RC numbers
        const rcA = parseInt(a.match(/-rc\.(\d+)/)?.[1] ?? "0", 10);
        const rcB = parseInt(b.match(/-rc\.(\d+)/)?.[1] ?? "0", 10);
        return rcA - rcB;
    });
}
