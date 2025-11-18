// @ts-check

/**
 * Semantic version bumping utilities for OCM components.
 * Supports major, minor, and patch version bumps.
 */

/**
 * Parse a semantic version string into its components.
 *
 * @param {string} version - Version string (e.g., "v1.2.3" or "1.2.3-rc.1")
 * @returns {{major: number, minor: number, patch: number, prerelease: string}} Parsed version components
 *
 * @example
 * parseVersion("v1.2.3") // returns { major: 1, minor: 2, patch: 3, prerelease: "" }
 * parseVersion("1.2.3-rc.1") // returns { major: 1, minor: 2, patch: 3, prerelease: "rc.1" }
 */
export function parseVersion(version) {
    if (!version) {
        throw new Error("version is required");
    }

    // Remove leading 'v' if present and any quotation marks that are left by JQ capture.
    const cleanVersion = version.replace(/["']/g, '').replace(/^v/, '').trim();

    // Match semver pattern: major.minor.patch[-prerelease]
    const match = cleanVersion.match(/^(\d+)\.(\d+)\.(\d+)(?:-(.+))?$/);

    if (!match) {
        throw new Error(`Invalid semantic version: ${version}`);
    }

    return {
        major: parseInt(match[1], 10),
        minor: parseInt(match[2], 10),
        patch: parseInt(match[3], 10),
        prerelease: match[4] || ""
    };
}

/**
 * Bump a semantic version.
 *
 * @param {string} version - Current version (e.g., "v1.2.3")
 * @param {"major" | "minor" | "patch"} bumpType - Type of version bump
 * @returns {string} New version string with 'v' prefix
 *
 * @example
 * bumpVersion("v1.2.3", "patch") // returns "v1.2.4"
 * bumpVersion("v1.2.3", "minor") // returns "v1.3.0"
 * bumpVersion("v1.2.3", "major") // returns "v2.0.0"
 * bumpVersion("v1.2.3-rc.1", "patch") // returns "v1.2.4" (removes prerelease)
 */
export function bumpVersion(version, bumpType) {
    if (!version) {
        throw new Error("version is required");
    }

    if (!bumpType || !["major", "minor", "patch"].includes(bumpType)) {
        throw new Error(`Invalid bump type: ${bumpType}. Must be 'major', 'minor', or 'patch'`);
    }

    const parsed = parseVersion(version);

    // Bump the appropriate component and reset lower components
    switch (bumpType) {
        case "major":
            parsed.major += 1;
            parsed.minor = 0;
            parsed.patch = 0;
            break;
        case "minor":
            parsed.minor += 1;
            parsed.patch = 0;
            break;
        case "patch":
            parsed.patch += 1;
            break;
    }

    // Always remove prerelease on bump
    return `v${parsed.major}.${parsed.minor}.${parsed.patch}`;
}

/**
 * GitHub Actions entrypoint for bumping versions.
 *
 * Environment variables:
 * - CURRENT_VERSION: Current version to bump (required)
 * - BUMP_TYPE: Type of bump - major, minor, or patch (required)
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function bumpVersionAction({ core }) {
    const currentVersion = process.env.CURRENT_VERSION;
    const bumpType = process.env.BUMP_TYPE;

    if (!currentVersion) {
        core.setFailed("CURRENT_VERSION environment variable is required");
        return;
    }

    if (!bumpType) {
        core.setFailed("BUMP_TYPE environment variable is required");
        return;
    }

    try {
        const newVersion = bumpVersion(currentVersion, bumpType);

        core.exportVariable("NEW_VERSION", newVersion);
        core.setOutput("new_version", newVersion);
        core.info(`✅ Bumped version from ${currentVersion} to ${newVersion} (${bumpType})`);

        await core.summary
            .addHeading("📦 Version Bump")
            .addTable([
                [
                    { data: "Field", header: true },
                    { data: "Value", header: true },
                ],
                ["Current Version", currentVersion],
                ["Bump Type", bumpType],
                ["New Version", newVersion],
            ])
            .write();
    } catch (error) {
        core.setFailed(error.message);
    }
}
