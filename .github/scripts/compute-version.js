// @ts-check

/**
 * Unified version computation for OCM components.
 * Supports both CLI and bindings (plugins) with configurable tag prefixes.
 */

/**
 * Compute version from a git ref.
 *
 * Rules:
 * - Tag refs (matching tagPrefix pattern): Extract version from tag name
 * - Branch/other refs: Generate unique pseudo-version
 *   (0.0.0-<sanitized-ref>.<yyyymmddHHMMSS>.<shortsha>)
 *
 * @param {string} ref - Git ref (branch name, tag name, or other ref)
 * @param {string} tagPrefix - Tag prefix pattern (e.g., "cli/v" or "bindings/go/helm/v")
 * @param {{ now?: Date, gitSha?: string }} [options] - Optional deterministic inputs for testing or overrides
 * @returns {string} Computed version string
 *
 * @example
 * computeVersion("cli/v1.2.3", "cli/v") // returns "1.2.3"
 * computeVersion("bindings/go/helm/v2.0.0-alpha1", "bindings/go/helm/v") // returns "2.0.0-alpha1"
 * computeVersion("main", "cli/v", { now: new Date("2026-03-03T12:34:56Z"), gitSha: "abcdef1234567890" }) // returns "0.0.0-main.20260303123456.abcdef123456"
 */
export function computeVersion(ref, tagPrefix, options = {}) {
    if (!ref) {
        throw new Error("ref is required");
    }
    if (!tagPrefix) {
        throw new Error("tagPrefix is required");
    }

    // Build regex to match tag pattern: prefix + semver
    // Example: "cli/v" matches "cli/v1.2.3" or "cli/v1.2.3-rc.1"
    const tagPattern = new RegExp(
        `^${escapeRegex(tagPrefix)}\\d+\\.\\d+(\\.\\d+)?(-.*)?$`
    );

    const isTag = tagPattern.test(ref);

    if (isTag) {
        // Extract version by removing prefix
        return ref.replace(tagPrefix, "");
    } else {
        // Convert ref to semver-safe pseudo version
        // using current timestamp and git SHA for uniqueness
        const timestamp = toUtcCompactTimestamp(options.now ?? new Date());
        const sanitizedRef = sanitizePrereleaseIdentifier(ref) || "ref";
        const shortSha = options.gitSha ? normalizeSha(options.gitSha) : "unknown";

        return `0.0.0-${sanitizedRef}.${timestamp}.${shortSha}`;
    }
}

/**
 * Convert a value into a semver-safe prerelease identifier segment.
 *
 * @param {string} value
 * @returns {string}
 */
function sanitizePrereleaseIdentifier(value) {
    return value
        .toLowerCase()
        .replace(/[^0-9a-z.-]/g, "-")
        .replace(/-+/g, "-")
        .replace(/^-|-$/g, "");
}

/**
 * Format a Date as UTC timestamp (yyyymmddHHMMSS).
 *
 * @param {Date} date
 * @returns {string}
 */
function toUtcCompactTimestamp(date) {
    return date.toISOString().replace(/[-:TZ.]/g, "").slice(0, 14);
}

/**
 * Normalize a git SHA to lower-case hex and trim to 12 chars.
 *
 * @param {string} sha
 * @returns {string}
 */
function normalizeSha(sha) {
    const normalized = sha.toLowerCase().replace(/[^0-9a-f]/g, "").slice(0, 12);
    if (normalized.length < 7) {
        throw new Error(
            `Invalid git SHA: expected at least 7 hex characters, got "${sha}" (normalized: "${normalized}")`
        );
    }
    return normalized;
}

/**
 * Escape special regex characters in a string.
 *
 * @param {string} str - String to escape
 * @returns {string} Escaped string safe for use in regex
 */
function escapeRegex(str) {
    return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * GitHub Actions entrypoint for computing versions.
 *
 * Environment variables:
 * - REF: Git ref to compute version from (required)
 * - TAG_PREFIX: Tag prefix pattern (required, e.g. "cli/v" or "bindings/go/helm/v")
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function computeVersionAction({ core }) {
    const ref = process.env.REF;
    const tagPrefix = process.env.TAG_PREFIX;
    const gitSha = process.env.GITHUB_SHA;

    if (!ref) {
        core.setFailed("REF environment variable is required");
        return;
    }

    if (!tagPrefix) {
        core.setFailed("TAG_PREFIX environment variable is required");
        return;
    }

    try {
        const version = computeVersion(ref, tagPrefix, { gitSha });

        core.exportVariable("VERSION", version);
        core.setOutput("version", version);
        core.info(`✅ Computed VERSION=${version} from REF=${ref} with TAG_PREFIX=${tagPrefix}`);

        await core.summary
            .addHeading("📦 Version Computation")
            .addTable([
                [
                    { data: "Field", header: true },
                    { data: "Value", header: true },
                ],
                ["Git Ref", ref],
                ["Tag Prefix", tagPrefix],
                ["Computed Version", version],
            ])
            .write();
    } catch (error) {
        core.setFailed(error.message);
    }
}
