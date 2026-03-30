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

    // Find previous release tag for changelog range.
    // git-cliff's --include-path can miss tag boundaries when the tagged commit
    // doesn't touch files in the component path. Passing an explicit range avoids this.
    const mergedTags = run(core, "git", [
        "tag", "--list", `${tagPrefix}*`,
        "--merged", "HEAD",
        "--sort=-version:refname"
    ]);
    const previousTag = findPreviousTag(
        mergedTags.split("\n").filter(Boolean),
        rcTag
    );
    const changelogRange = previousTag ? `${previousTag}..HEAD` : "";

    core.setOutput("new_tag", rcTag);
    core.setOutput("new_version", rcVersion);
    core.setOutput("base_version", baseVersion);
    core.setOutput("promotion_tag", promotionTag);
    core.setOutput("changelog_range", changelogRange);

    core.info(`Previous release tag: ${previousTag || "(none — first release)"}`);
    core.info(`Changelog range: ${changelogRange || "(full history)"}`);

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
            ["Previous Release Tag", previousTag || "(none — first release)"],
            ["Changelog Range", changelogRange || "(full history)"],
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

// --------------------------------------------
// Changelog range determination for git-cliff
// --------------------------------------------

/**
 * Find the most recent stable semver release tag for a component on the current branch.
 *
 * Required because git-cliff's --include-path filter can cause it to lose
 * track of tag boundaries when the tagged commit doesn't modify files matching
 * the include path. By finding the previous tag explicitly, we can pass it as
 * commit range (e.g. "cli/v0.1.0..HEAD") to git-cliff instead of relying on
 * --latest, which avoids the issue entirely.
 *
 * @param {string[]} tags - List of tags reachable from HEAD (from git tag --list --merged HEAD)
 * @param {string} newTag - The tag about to be created (to exclude from results)
 * @returns {string} The previous stable semver tag, or empty string if none found (first release)
 */
export function findPreviousTag(tags, newTag) {
    const compareVersions = (a, b) => {
        for (let i = 0; i < 3; i++) {
            const diff = (a[i] || 0) - (b[i] || 0);
            if (diff !== 0) return diff;
        }
        return 0;
    };

    const newTagParts = parseVersion(newTag);

    return tags
        .filter(t => t && t !== newTag && /^.+\/v\d+\.\d+\.\d+$/.test(t))
        .filter(t => compareVersions(parseVersion(t), newTagParts) < 0)
        .sort((a, b) => compareVersions(parseVersion(b), parseVersion(a)))[0] || "";
}

// --------------------------
// Latest release determination
// --------------------------

/** GitHub Actions entrypoint for determining if release should be latest */
export async function determineLatestRelease({ core, github, context }) {
    const { COMPONENT_PATH: componentPath, NEW_VERSION: newVersion } = process.env;
    if (!componentPath || !newVersion) return core.setFailed("Missing COMPONENT_PATH or NEW_VERSION");

    const tagPrefix = `${componentPath}/v`;
    let releases = [];
    try {
        releases = (await github.rest.repos.listReleases({ owner: context.repo.owner, repo: context.repo.repo, per_page: 100 })).data;
    } catch (e) {
        core.setFailed(`Could not fetch releases: ${e.message}`);
        return;
    }

    const highestPreviousReleaseVersion = extractHighestPreviousReleaseVersion(releases, tagPrefix);
    const setLatest = shouldSetLatest(newVersion, highestPreviousReleaseVersion);

    core.setOutput('set_latest', setLatest ? 'true' : 'false');
    core.setOutput('highest_previous_release_version', highestPreviousReleaseVersion || '(none)');
    core.info(setLatest ? `✅ Will set :latest (${newVersion} >= ${highestPreviousReleaseVersion || 'none'})` : `⚠️ Will NOT set :latest (${newVersion} < ${highestPreviousReleaseVersion})`);

    await core.summary.addRaw('---').addEOL().addHeading('Latest Tag Decision', 2)
        .addTable([[{ data: 'Field', header: true }, { data: 'Value', header: true }], ['New Version', newVersion], ['Highest Previous Release Version', highestPreviousReleaseVersion || '(none)'], ['Will Set Latest', setLatest ? '✅ Yes' : '⚠️ No']]).write();
}

/** Extract highest previous release (non-prerelease) version from releases */
export function extractHighestPreviousReleaseVersion(releases, tagPrefix) {
    const versions = releases.filter(r => !r.prerelease && r.tag_name.startsWith(tagPrefix))
        .map(r => r.tag_name.replace(tagPrefix, '')).filter(v => /^\d+\.\d+\.\d+$/.test(v));
    if (!versions.length) return '';
    return versions.sort((a, b) => isStableNewer(`v${a}`, `v${b}`) ? 1 : -1).pop();
}

/** Determine if new version should be tagged as latest */
export function shouldSetLatest(newVersion, highestPreviousReleaseVersion) {
    return !highestPreviousReleaseVersion || !isStableNewer(`v${highestPreviousReleaseVersion}`, `v${newVersion}`);
}
