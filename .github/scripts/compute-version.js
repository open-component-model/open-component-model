// @ts-check

/**
 * Unified version computation for OCM components.
 * Supports both CLI and bindings (plugins) with configurable tag prefixes.
 */

// Kubernetes label values must be at most 63 characters (RFC 1123).
// The helm.sh/chart label is formatted as "<chart-name>-<version>".
// Our chart name is "chart" (6 chars including the hyphen separator),
// so the version portion must not exceed 57 characters.
// If the chart name changes, this constant must be updated accordingly.
const MAX_VERSION_LENGTH = 57;

/**
 * Compute version from a git ref.
 *
 * Rules:
 * - Tag refs (matching tagPrefix pattern): Extract version from tag name
 * - Branch/other refs: Generate pseudo-version (0.0.0-<sanitized-ref>),
 *   truncated to maxLength to stay within Kubernetes label limits
 *
 * @param {string} ref - Git ref (branch name, tag name, or other ref)
 * @param {string} tagPrefix - Tag prefix pattern (e.g., "cli/v" or "bindings/go/helm/v")
 * @param {object} [options] - Optional settings
 * @param {number} [options.maxLength] - Max length for pseudo-versions (default: MAX_VERSION_LENGTH)
 * @param {(msg: string) => void} [options.warn] - Callback invoked when a version is truncated
 * @returns {string} Computed version string
 *
 * @example
 * computeVersion("cli/v1.2.3", "cli/v") // returns "1.2.3"
 * computeVersion("bindings/go/helm/v2.0.0-alpha1", "bindings/go/helm/v") // returns "2.0.0-alpha1"
 * computeVersion("main", "cli/v") // returns "0.0.0-main"
 * computeVersion("releases/v0.1", "cli/v") // returns "0.0.0-releases-v0.1"
 */
export function computeVersion(ref, tagPrefix, options = {}) {
    if (!ref) {
        throw new Error("ref is required");
    }
    if (!tagPrefix) {
        throw new Error("tagPrefix is required");
    }

    const maxLength = options.maxLength ?? MAX_VERSION_LENGTH;

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
        // Replace slashes and other problematic chars with hyphens
        const sanitized = ref.replace(/[\/+#?_^%$]/g, "-").toLocaleLowerCase();
        const version = `0.0.0-${sanitized}`;

        if (version.length > maxLength) {
            const truncated = version.substring(0, maxLength).replace(/-$/, "");
            if (options.warn) {
                options.warn(
                    `Version "${version}" truncated to "${truncated}" ` +
                    `to fit Kubernetes 63-char label value limit (max version length: ${maxLength})`
                );
            }
            return truncated;
        }

        return version;
    }
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

    if (!ref) {
        core.setFailed("REF environment variable is required");
        return;
    }

    if (!tagPrefix) {
        core.setFailed("TAG_PREFIX environment variable is required");
        return;
    }

    try {
        const version = computeVersion(ref, tagPrefix, {
            warn: (msg) => core.warning(msg),
        });

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
