// @ts-check
import { execFileSync } from "child_process";

// --------------------------
// GitHub Actions entrypoint
// --------------------------
//
// Computes the next RC version for the unified product release. The release
// tag scheme is:
//   v0.X.Y                          canonical release tag (user-facing)
//   kubernetes/controller/v0.X.Y    Go-module side tag, same commit
//
// The cli/v* prefix is intentionally not emitted: the CLI is not consumed
// as a Go module by anyone.
//
// noinspection JSUnusedGlobalSymbols
/** @param {import('@actions/github-script').AsyncFunctionArguments} args */
export default async function computeRcVersion({ core }) {
    const releaseBranch = process.env.BRANCH;
    if (!releaseBranch) {
        core.setFailed("Missing BRANCH env var");
        return;
    }

    const basePrefix = parseBranch(releaseBranch);

    // Stable tags on this minor: v0.X.Y (no component prefix)
    const stableTags = run(core, "git", [
        "tag", "--list", `v${basePrefix}.*`,
        "--sort=-version:refname"
    ]);
    const previousBaseVersion = stableTags
        .split("\n")
        .filter(t => t && /^v\d+\.\d+\.\d+$/.test(t))[0] || "";

    // RC tags on this minor: v0.X.Y-rc.N
    const rcTags = run(core, "git", [
        "tag", "--list", `v${basePrefix}.*-rc.*`,
        "--sort=-version:refname"
    ]);
    const previousBaseRcVersion = rcTags
        .split("\n")
        .filter(t => t && /^v\d+\.\d+\.\d+-rc\.\d+$/.test(t))[0] || "";

    core.info(`Previous base version: ${previousBaseVersion || "(none)"}`);
    core.info(`Previous base RC version: ${previousBaseRcVersion || "(none)"}`);

    const { baseVersion, rcVersion } = computeNextVersions(
        basePrefix, previousBaseVersion, previousBaseRcVersion, false
    );

    const rcTag = `v${rcVersion}`;
    const promotionTag = `v${baseVersion}`;
    const controllerModuleRcTag = `kubernetes/controller/${rcTag}`;
    const controllerModulePromotionTag = `kubernetes/controller/${promotionTag}`;

    // Find previous canonical release tag for the changelog range.
    const allTags = run(core, "git", [
        "tag", "--list", "v*",
        "--sort=-version:refname"
    ]);
    const previousTag = findPreviousTag(
        allTags.split("\n").filter(Boolean),
        rcTag
    );
    const changelogRange = previousTag ? `${previousTag}..HEAD` : "";

    core.setOutput("new_tag", rcTag);
    core.setOutput("new_version", rcVersion);
    core.setOutput("base_version", baseVersion);
    core.setOutput("promotion_tag", promotionTag);
    core.setOutput("controller_module_rc_tag", controllerModuleRcTag);
    core.setOutput("controller_module_promotion_tag", controllerModulePromotionTag);
    core.setOutput("changelog_range", changelogRange);

    core.info(`Previous release tag: ${previousTag || "(none — first release)"}`);
    core.info(`Changelog range: ${changelogRange || "(full history)"}`);

    await core.summary
        .addHeading("Release Version Computation")
        .addTable([
            [{ data: "Field", header: true }, { data: "Value", header: true }],
            ["Release Branch", releaseBranch],
            ["Next RC Tag", rcTag],
            ["Next Release Tag", promotionTag],
            ["Controller Module RC Tag", controllerModuleRcTag],
            ["Controller Module Release Tag", controllerModulePromotionTag],
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
 * @param {string} executable
 * @param {string[]} args
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
 * Compute the next base and RC versions.
 *
 * Versioning rules:
 *  - No tags: start from base prefix (e.g. "0.1" → 0.1.0, 0.1.0-rc.1).
 *  - Stable only: bump patch, start RC sequence (0.1.0 → 0.1.1, 0.1.1-rc.1).
 *  - RC only: continue RC numbering (0.1.1-rc.2 → 0.1.1, 0.1.1-rc.3).
 *  - Both same base: bump patch, restart RC sequence.
 *  - Stable newer: bump patch, restart RC sequence.
 *  - RC newer: continue RC sequence on RC's base.
 *
 * @param {string} basePrefix - Branch base prefix (e.g., "0.1").
 * @param {string} [latestStableTag] - Most recent stable tag (e.g., "v0.1.0" or "cli/v0.1.0").
 * @param {string} [latestRcTag] - Most recent RC tag.
 * @param {boolean} [bumpMinorVersion] - Bump minor instead of patch.
 * @returns {{ baseVersion: string, rcVersion: string }}
 */
export function computeNextVersions(basePrefix, latestStableTag, latestRcTag, bumpMinorVersion) {
    const parseTag = tag => parseVersion(tag).join(".");
    const extractRcNumber = tag => parseInt(tag?.match(/-rc\.(\d+)/)?.[1] ?? "0", 10);
    const incrementVersion = ([maj, min, pat]) => {
        if (bumpMinorVersion) return [maj, min + 1, 0];
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
        case !latestStableTag && !latestRcTag:
            break;

        case latestStableTag && !latestRcTag:
            [major, minor, patch] = incrementVersion([major, minor, patch]);
            nextBaseVersion = `${major}.${minor}.${patch}`;
            break;

        case !latestStableTag && latestRcTag:
            nextRcNumber = extractRcNumber(latestRcTag) + 1;
            nextBaseVersion = parseTag(latestRcTag);
            [major, minor, patch] = rcVersionParts;
            break;

        case parseTag(latestStableTag) === parseTag(latestRcTag):
        case isStableNewer(latestStableTag, latestRcTag):
            [major, minor, patch] = incrementVersion([major, minor, patch]);
            nextBaseVersion = `${major}.${minor}.${patch}`;
            nextRcNumber = 1;
            break;

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

export function isStableNewer(stable, rc) {
    if (!stable) return false;
    if (!rc) return true;

    const stableParts = parseVersion(stable);
    const rcParts = parseVersion(rc);

    for (let i = 0; i < 3; i++) {
        const s = stableParts[i] || 0;
        const r = rcParts[i] || 0;
        if (s > r) return true;
        if (s < r) return false;
    }
    return false;
}

/**
 * Parse a version tag into an array of [major, minor, patch].
 * Handles plain `v0.5.0`, prefixed `cli/v0.5.0`, and RC variants.
 */
export function parseVersion(tag) {
    if (!tag) return [];
    const version = tag.replace(/^.*v/, "").replace(/-rc\.\d+$/, "");
    return version.split(".").map(Number);
}

// --------------------------
// Changelog range helpers
// --------------------------

/**
 * Find the most recent stable canonical tag (vX.Y.Z) excluding RCs.
 *
 * @param {string[]} tags - Tags returned by `git tag --list "v*" --sort=-version:refname`.
 * @param {string} newTag - The tag about to be created (excluded from results).
 * @returns {string} The previous canonical tag, or empty string if none.
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
        .filter(t => t && t !== newTag && /^v\d+\.\d+\.\d+$/.test(t))
        .filter(t => compareVersions(parseVersion(t), newTagParts) < 0)
        .sort((a, b) => compareVersions(parseVersion(b), parseVersion(a)))[0] || "";
}

// --------------------------
// Latest release determination
// --------------------------

/**
 * Decide whether the new release should be marked as the GitHub "Latest"
 * and tagged :latest in OCI registries. False when shipping a back-patch
 * to an older minor.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function determineLatestRelease({ core, github, context }) {
    const newReleaseVersion = process.env.NEW_RELEASE_VERSION;
    if (!newReleaseVersion) {
        core.setFailed("Missing NEW_RELEASE_VERSION");
        return;
    }

    let releases = [];
    try {
        releases = (await github.rest.repos.listReleases({
            owner: context.repo.owner, repo: context.repo.repo, per_page: 100
        })).data;
    } catch (e) {
        core.setFailed(`Could not fetch releases: ${e.message}`);
        return;
    }

    const highestPreviousReleaseVersion = extractHighestPreviousReleaseVersion(releases);
    const setLatest = shouldSetLatest(newReleaseVersion, highestPreviousReleaseVersion);

    core.setOutput('set_latest', setLatest ? 'true' : 'false');
    core.setOutput('highest_previous_release_version', highestPreviousReleaseVersion || '(none)');
    core.info(setLatest
        ? `Will set :latest (${newReleaseVersion} >= ${highestPreviousReleaseVersion || 'none'})`
        : `Will NOT set :latest (${newReleaseVersion} < ${highestPreviousReleaseVersion})`);

    await core.summary
        .addRaw('---').addEOL()
        .addHeading('Latest Tag Decision', 2)
        .addTable([
            [{ data: 'Field', header: true }, { data: 'Value', header: true }],
            ['New Release Version', newReleaseVersion],
            ['Highest Previous Release Version', highestPreviousReleaseVersion || '(none)'],
            ['Will Set Latest', setLatest ? 'Yes' : 'No'],
        ])
        .write();
}

/**
 * Extract the highest non-prerelease canonical version (vX.Y.Z) from releases.
 */
export function extractHighestPreviousReleaseVersion(releases) {
    const versions = releases
        .filter(r => !r.prerelease && /^v\d+\.\d+\.\d+$/.test(r.tag_name))
        .map(r => r.tag_name.replace(/^v/, ''))
        .filter(v => /^\d+\.\d+\.\d+$/.test(v));
    if (!versions.length) return '';
    return versions.sort((a, b) => isStableNewer(`v${a}`, `v${b}`) ? 1 : -1).pop();
}

export function shouldSetLatest(newReleaseVersion, highestPreviousReleaseVersion) {
    return !highestPreviousReleaseVersion ||
        !isStableNewer(`v${highestPreviousReleaseVersion}`, `v${newReleaseVersion}`);
}
