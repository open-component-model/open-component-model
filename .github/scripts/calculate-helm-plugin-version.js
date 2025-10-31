// @ts-check
import {execSync} from "child_process";

// --------------------------
// GitHub Actions entrypoint
// --------------------------
// noinspection JSUnusedGlobalSymbols
/** @param {import('@actions/github-script').AsyncFunctionArguments} args */
export default async function calculateHelmPluginVersion({ core, context }) {
    const eventName = context.eventName;
    const ref = context.ref;
    const sha = context.sha;

    // Get workflow inputs if workflow_dispatch
    const manualVersion = process.env.MANUAL_VERSION || "";

    try {
        const version = computeVersion(core, eventName, ref, sha, manualVersion);

        core.setOutput("version", version);
        core.info(`VERSION: ${version} (deterministic, safe for re-runs)`);

        // --------------------------
        // Step summary
        // --------------------------
        await core.summary
            .addHeading("Helm Plugin Version Calculation")
            .addTable([
                [
                    { data: "Field", header: true },
                    { data: "Value", header: true },
                ],
                ["Event", eventName],
                ["Ref", ref],
                ["SHA", sha.substring(0, 12)],
                ["Manual Version", manualVersion || "(none)"],
                ["Computed Version", version],
            ])
            .write();
    } catch (error) {
        core.setFailed(error.message);
    }
}

// --------------------------
// Core logic
// --------------------------

/**
 * Compute the version string based on GitHub Actions context.
 *
 * @param {Object} core - GitHub Actions core object
 * @param {string} eventName - GitHub event name (push, pull_request, workflow_dispatch)
 * @param {string} ref - GitHub ref (refs/heads/main, refs/tags/..., refs/pull/...)
 * @param {string} sha - Git commit SHA
 * @param {string} manualVersion - Manual version input from workflow_dispatch
 * @returns {string} The computed version string
 */
export function computeVersion(core, eventName, ref, sha, manualVersion) {
    const shortSha = sha.substring(0, 12);

    // Workflow dispatch with manual version input
    if (eventName === "workflow_dispatch") {
        if (manualVersion === "" || manualVersion === "main") {
            // Manual "main" build: use dev version
            const latestTag = getLatestTag(core);
            return `${latestTag}-dev.${shortSha}`;
        } else {
            // Validate semver format
            validateSemver(manualVersion);

            // Validate tag exists
            const expectedTag = `bindings/go/helm/v${manualVersion}`;
            validateTagExists(core, expectedTag);

            return manualVersion;
        }
    }

    // Push to main branch
    if (ref === "refs/heads/main") {
        const latestTag = getLatestTag(core);
        return `${latestTag}-dev.${shortSha}`;
    }

    // Tag push
    if (ref.startsWith("refs/tags/bindings/go/helm/v")) {
        return ref.replace("refs/tags/bindings/go/helm/v", "");
    }

    // Pull request
    if (ref.startsWith("refs/pull/") && ref.endsWith("/merge")) {
        const prNumber = ref.replace("refs/pull/", "").replace("/merge", "");
        const latestTag = getLatestTag(core);
        return `${latestTag}-pr.${prNumber}.${shortSha}`;
    }

    throw new Error(`Unsupported ref: ${ref}`);
}

/**
 * Get the latest bindings/go/helm tag version, or default to 0.0.0.
 *
 * @param {Object} core - GitHub Actions core object
 * @returns {string} The latest version (e.g., "0.1.2")
 */
export function getLatestTag(core) {
    const cmd = "git tag | grep '^bindings/go/helm/v' | sort -V | tail -1 | sed 's#.*/v##'";
    core.info(`> ${cmd}`);

    try {
        const result = execSync(cmd, { encoding: "utf8" }).trim();
        if (result) {
            core.info(`Latest tag: ${result}`);
            return result;
        }
    } catch (error) {
        core.warning(`Failed to get latest tag: ${error.message}`);
    }

    core.info("No existing tags found, using default: 0.0.0");
    return "0.0.0";
}

/**
 * Validate that a version string matches semantic versioning format.
 *
 * @param {string} version - Version string to validate
 * @throws {Error} If version is invalid
 */
export function validateSemver(version) {
    // SemVer regex: Major.Minor.Patch with optional -suffix
    const semverRegex = /^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9._-]+)?$/;

    if (!semverRegex.test(version)) {
        throw new Error(`Invalid version format: ${version}`);
    }
}

/**
 * Validate that a git tag exists in the repository.
 *
 * @param {Object} core - GitHub Actions core object
 * @param {string} tag - Tag name to validate
 * @throws {Error} If tag does not exist
 */
export function validateTagExists(core, tag) {
    const cmd = `git ls-remote --tags origin "refs/tags/${tag}"`;
    core.info(`> ${cmd}`);

    try {
        const result = execSync(cmd, { encoding: "utf8" }).trim();

        // Check if the result contains the exact tag reference
        if (!result.includes(`refs/tags/${tag}`)) {
            throw new Error(`Tag ${tag} does not exist in the repository.`);
        }

        core.info(`✅ Tag ${tag} exists`);
    } catch (error) {
        throw new Error(`Tag ${tag} does not exist in the repository.`);
    }
}
