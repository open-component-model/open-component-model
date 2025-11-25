// @ts-check

/**
 * Shared semantic versioning utilities.
 * Used by bump-semver.js, compute-version.js, and compute-rc-version.js.
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

    // Remove leading 'v' if present and any quotation marks
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
 * Parse a version tag into an array of version components.
 * Useful for version comparison and manipulation.
 *
 * @param {string} tag - Version tag (e.g., "cli/v0.1.2" or "cli/v0.1.2-rc.3")
 * @returns {number[]} Array of [major, minor, patch]
 */
export function parseVersionArray(tag) {
    if (!tag) return [];
    const version = tag.replace(/^.*v/, "").replace(/-rc\.\d+$/, "");
    return version.split(".").map(Number);
}

/**
 * Escape special regex characters in a string.
 *
 * @param {string} str - String to escape
 * @returns {string} Escaped string safe for use in regex
 */
export function escapeRegex(str) {
    return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
