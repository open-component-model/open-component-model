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
 * - Branch/other refs: Generate pseudo-version (0.0.0-<sanitized-ref>),
 *   truncated to maxLength to stay within Kubernetes label limits
 *
 * @param {string} ref - Git ref (branch name, tag name, or other ref)
 * @param {string} tagPrefix - Tag prefix pattern (e.g., "cli/v" or "bindings/go/helm/v")
 * @param {object} [options] - Optional settings
 * @param {number} [options.maxLength] - Max length for pseudo-versions. If unset, no truncation is applied.
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

    const maxLength = options.maxLength;

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

        if (Number.isInteger(maxLength) && maxLength > 0 && version.length > maxLength) {
            const truncated = version.substring(0, maxLength).replace(/-$/, "");
            console.warn(
                `Version "${version}" truncated to "${truncated}" ` +
                `to fit label value limit (max version length: ${maxLength})`
            );
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
 * - MAX_VERSION_LENGTH: Optional max length (chars) for pseudo-versions. Required when the version
 *   is embedded in a Kubernetes label value (max 63 chars, RFC 1123). For example, the helm.sh/chart
 *   label uses "<chart-name>-<version>", so MAX_VERSION_LENGTH = 63 - len("<chart-name>-").
 *   Leave unset for workflows that do not produce Kubernetes labels (e.g. CLI, OCI tags).
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function computeVersionAction({ core }) {
    const ref = process.env.REF;
    const tagPrefix = process.env.TAG_PREFIX;
    const maxLengthEnv = process.env.MAX_VERSION_LENGTH;

    // Parse optional max version length. When set, pseudo-versions are truncated to fit within
    // Kubernetes label value limits (63 chars, RFC 1123). Only workflows embedding the version
    // in a label (e.g. helm.sh/chart) need to set this; others leave it unset to skip truncation.
    const maxLength = maxLengthEnv ? Number(maxLengthEnv) : undefined;
    if (maxLengthEnv && (!Number.isInteger(maxLength) || maxLength <= 0)) {
        core.setFailed(`MAX_VERSION_LENGTH must be a positive integer, got "${maxLengthEnv}"`);
        return;
    }

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
            maxLength,
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
