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

    // Get latest stable tag using Git's native version sort (descending)
    // Filter out RC tags after fetching since git doesn't support negative pattern matching
    const stableTags = run(core, "git", [
        "tag", "--list", `${tagPrefix}${basePrefix}.*`,
        "--sort=-version:refname"
    ]);
    const latestStable = stableTags
        .split("\n")
        .filter(tag => tag && !/-rc\.\d+$/.test(tag))[0] || "";

    // Get latest RC tag using Git's native version sort (descending)
    const rcTags = run(core, "git", [
        "tag", "--list", `${tagPrefix}${basePrefix}.*-rc.*`,
        "--sort=-version:refname"
    ]);
    const latestRc = rcTags.split("\n").filter(Boolean)[0] || "";

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

// --------------------------
// Semver comparison with prerelease support
// --------------------------

/**
 * Compare two semver version strings, including RC prerelease suffixes.
 * Returns a negative number if a < b, positive if a > b, 0 if equal.
 *
 * Comparison rules (per semver spec):
 *  - Major.minor.patch are compared numerically.
 *  - A prerelease version (e.g., 0.2.0-rc.1) has lower precedence
 *    than the same version without prerelease (e.g., 0.2.0).
 *  - RC numbers are compared numerically (rc.2 > rc.1).
 *
 * @param {string} a - Version string (e.g., "0.2.0", "0.2.0-rc.1")
 * @param {string} b - Version string
 * @returns {number} Comparison result (-1, 0, or 1)
 */
export function compareSemver(a, b) {
    const partsA = parseVersion(`v${a}`);
    const partsB = parseVersion(`v${b}`);

    // Compare major.minor.patch
    for (let i = 0; i < 3; i++) {
        const va = partsA[i] || 0;
        const vb = partsB[i] || 0;
        if (va > vb) return 1;
        if (va < vb) return -1;
    }

    // Same base version — compare prerelease:
    // no prerelease > any prerelease (per semver spec)
    const rcA = extractRcNumber(a);
    const rcB = extractRcNumber(b);

    if (rcA === 0 && rcB === 0) return 0;  // both final
    if (rcA === 0) return 1;               // a is final, b is RC → a wins
    if (rcB === 0) return -1;              // b is final, a is RC → b wins

    // Both are RCs — compare RC numbers
    if (rcA > rcB) return 1;
    if (rcA < rcB) return -1;
    return 0;
}

/**
 * Extract RC number from a version string. Returns 0 for non-RC versions.
 * @param {string} version - e.g., "0.2.0-rc.3" or "0.2.0"
 * @returns {number}
 */
function extractRcNumber(version) {
    const match = version?.match(/-rc\.(\d+)/);
    return match ? parseInt(match[1], 10) : 0;
}

// --------------------------
// Floating tag determination
// --------------------------

/**
 * GitHub Actions entrypoint for determining which floating tags to set.
 *
 * Outputs:
 *   set_latest      - "true" if this version >= the highest existing version (including RCs)
 *   set_stable      - "true" if this version >= the highest existing final (non-prerelease) version
 *   highest_version - Highest existing version including RCs, for logging
 *   highest_stable_version - Highest existing final version, for logging
 */
export async function determineFloatingTags({ core, github, context }) {
    const { COMPONENT_PATH: componentPath, PROMOTION_VERSION: promotionVersion } = process.env;
    if (!componentPath || !promotionVersion) return core.setFailed("Missing COMPONENT_PATH or PROMOTION_VERSION");

    const tagPrefix = `${componentPath}/v`;
    let releases = [];
    try {
        releases = (await github.rest.repos.listReleases({ owner: context.repo.owner, repo: context.repo.repo, per_page: 100 })).data;
    } catch (e) {
        core.setFailed(`Could not fetch releases: ${e.message}`);
        return;
    }

    const highestStable = extractHighestStableVersion(releases, tagPrefix);
    const highestVersion = extractHighestVersion(releases, tagPrefix);
    const setStable = shouldSetStable(promotionVersion, highestStable);
    const setLatest = shouldSetLatest(promotionVersion, highestVersion);

    core.setOutput('set_latest', setLatest ? 'true' : 'false');
    core.setOutput('set_stable', setStable ? 'true' : 'false');
    core.setOutput('highest_version', highestVersion || '(none)');
    core.setOutput('highest_stable_version', highestStable || '(none)');

    core.info(setLatest
        ? `✅ Will set :latest (${promotionVersion} >= ${highestVersion || 'none'})`
        : `⚠️ Will NOT set :latest (${promotionVersion} < ${highestVersion})`);
    core.info(setStable
        ? `✅ Will set :stable (${promotionVersion} >= ${highestStable || 'none'})`
        : `⚠️ Will NOT set :stable (${promotionVersion} < ${highestStable})`);

    await core.summary.addRaw('---').addEOL().addHeading('Floating Tag Decision', 2)
        .addTable([
            [{ data: 'Field', header: true }, { data: 'Value', header: true }],
            ['Promotion Version', promotionVersion],
            ['Highest Version (incl. RC)', highestVersion || '(none)'],
            ['Highest Stable Version', highestStable || '(none)'],
            ['Will Set :latest', setLatest ? '✅ Yes' : '⚠️ No'],
            ['Will Set :stable', setStable ? '✅ Yes' : '⚠️ No'],
        ]).write();
}

/**
 * Extract highest final (non-prerelease) version from GitHub releases.
 * Only considers versions matching the given tag prefix.
 *
 * @param {Array<{prerelease: boolean, tag_name: string}>} releases
 * @param {string} tagPrefix - e.g., "kubernetes/controller/v"
 * @returns {string} Highest stable version (e.g., "0.2.0") or empty string
 */
export function extractHighestStableVersion(releases, tagPrefix) {
    const versions = releases
        .filter(r => !r.prerelease && r.tag_name.startsWith(tagPrefix))
        .map(r => r.tag_name.replace(tagPrefix, ''))
        .filter(v => /^\d+\.\d+\.\d+$/.test(v));
    if (!versions.length) return '';
    return versions.sort((a, b) => compareSemver(a, b)).pop();
}

/**
 * Extract highest version (including RC prereleases) from GitHub releases.
 * Only considers versions matching the given tag prefix.
 *
 * @param {Array<{prerelease: boolean, tag_name: string}>} releases
 * @param {string} tagPrefix - e.g., "kubernetes/controller/v"
 * @returns {string} Highest version (e.g., "0.3.0-rc.1" or "0.2.0") or empty string
 */
export function extractHighestVersion(releases, tagPrefix) {
    const versions = releases
        .filter(r => r.tag_name.startsWith(tagPrefix))
        .map(r => r.tag_name.replace(tagPrefix, ''))
        .filter(v => /^\d+\.\d+\.\d+(-rc\.\d+)?$/.test(v));
    if (!versions.length) return '';
    return versions.sort((a, b) => compareSemver(a, b)).pop();
}

/**
 * Determine if the promotion version should receive the :latest floating tag.
 * Returns true when the promotion version is >= the highest existing version (including RCs).
 *
 * @param {string} promotionVersion - Version being promoted (e.g., "0.2.0")
 * @param {string} highestVersion - Current highest version including RCs (e.g., "0.3.0-rc.1")
 * @returns {boolean}
 */
export function shouldSetLatest(promotionVersion, highestVersion) {
    if (!highestVersion) return true;
    return compareSemver(promotionVersion, highestVersion) >= 0;
}

/**
 * Determine if the promotion version should receive the :stable floating tag.
 * Returns true when the promotion version is >= the highest existing final (non-prerelease) version.
 *
 * @param {string} promotionVersion - Version being promoted (e.g., "0.2.0")
 * @param {string} highestStable - Current highest stable version (e.g., "0.1.0")
 * @returns {boolean}
 */
export function shouldSetStable(promotionVersion, highestStable) {
    if (!highestStable) return true;
    return compareSemver(promotionVersion, highestStable) >= 0;
}