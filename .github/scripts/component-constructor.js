// @ts-check
import fs from "fs";
import yaml from "js-yaml";

/**
 * Parse a component-constructor.yaml file and return the parsed object.
 *
 * @param {string} filePath - Absolute path to the constructor YAML file
 * @returns {Object} Parsed constructor object
 * @throws {Error} If file cannot be read or parsed
 */
export function parseConstructorFile(filePath) {
    const content = fs.readFileSync(filePath, "utf8");
    const doc = yaml.load(content);
    if (!doc || typeof doc !== "object") {
        throw new Error(`Invalid constructor file: ${filePath}`);
    }
    return doc;
}

/**
 * Extract component name and version from a parsed constructor object.
 *
 * @param {Object} constructor - Parsed constructor YAML object
 * @returns {{ name: string, version: string }}
 * @throws {Error} If name or version is missing
 */
export function extractNameVersion(constructor) {
    const name = constructor.name;
    const version = constructor.version;

    if (!name || typeof name !== "string") {
        throw new Error("Constructor is missing required field 'name'");
    }
    if (!version || typeof version !== "string") {
        throw new Error("Constructor is missing required field 'version'");
    }

    return { name, version };
}

/**
 * Build a GitHub Packages URL for a component descriptor.
 *
 * @param {string} repository - GitHub repository (e.g. "open-component-model/open-component-model")
 * @param {string} componentName - OCM component name (e.g. "ocm.software/cli")
 * @returns {string} Full URL to the package on GitHub
 */
export function buildPackageUrl(repository, componentName) {
    // URL-encode the component-descriptors path prefix and slashes in the component name
    const encodedName = componentName.replace(/\//g, "%2F");
    return `https://github.com/${repository}/pkgs/container/component-descriptors%2F${encodedName}`;
}

/**
 * Patch a CLI component constructor for publishing:
 * - Rewrite file-based CLI resource input paths to use a relative resources/bin/ prefix
 * - Replace the local OCI image resource with an ociArtifact access reference
 *
 * @param {Object} constructor - Parsed constructor YAML object (mutated in place)
 * @param {string} imageRef - Full OCI image reference (e.g. "ghcr.io/owner/cli:tag")
 * @param {string} imageTag - Image tag / version string
 * @returns {Object} The mutated constructor object
 * @throws {Error} If expected resources are not found
 */
export function patchCliConstructor(constructor, imageRef, imageTag) {
    if (!Array.isArray(constructor.resources)) {
        throw new Error("Constructor has no 'resources' array");
    }

    let foundImage = false;

    for (const resource of constructor.resources) {
        // Rewrite CLI binary paths: keep only the filename under resources/bin/
        if (
            resource.name === "cli" &&
            resource.input?.type === "file"
        ) {
            const parts = resource.input.path.split("/");
            const filename = parts[parts.length - 1];
            resource.input.path = `resources/bin/${filename}`;
        }

        // Convert local image resource to ociArtifact access
        if (resource.name === "image") {
            foundImage = true;
            resource.type = "ociImage";
            resource.version = imageTag;
            resource.access = {
                type: "ociArtifact",
                imageReference: imageRef,
            };
            // Remove local-only fields
            delete resource.relation;
            delete resource.input;
        }
    }

    if (!foundImage) {
        throw new Error("Constructor has no resource named 'image'");
    }

    return constructor;
}

/**
 * GitHub Actions entrypoint: summarize a published component version in the step summary.
 *
 * Environment variables:
 * - CONSTRUCTOR_FILE: Path to the component-constructor.yaml (required)
 * - OCM_REPOSITORY: Target OCM repository root (required)
 * - GITHUB_REPOSITORY: GitHub repository slug (required)
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function summarizeComponentVersion({ core }) {
    const constructorFile = process.env.CONSTRUCTOR_FILE;
    const ocmRepository = process.env.OCM_REPOSITORY;
    const githubRepository = process.env.GITHUB_REPOSITORY;

    if (!constructorFile) {
        core.setFailed("CONSTRUCTOR_FILE environment variable is required");
        return;
    }
    if (!ocmRepository) {
        core.setFailed("OCM_REPOSITORY environment variable is required");
        return;
    }
    if (!githubRepository) {
        core.setFailed("GITHUB_REPOSITORY environment variable is required");
        return;
    }

    try {
        const constructor = parseConstructorFile(constructorFile);
        const { name, version } = extractNameVersion(constructor);
        const componentRef = `${ocmRepository}//${name}:${version}`;
        const packageUrl = buildPackageUrl(githubRepository, name);

        core.info(`📦 Published component: ${componentRef}`);

        await core.summary
            .addHeading("Published OCM Component Version")
            .addTable([
                [
                    { data: "Field", header: true },
                    { data: "Value", header: true },
                ],
                ["Component", name],
                ["Version", version],
                ["Reference", `<a href="${packageUrl}">${componentRef}</a>`],
            ])
            .write();
    } catch (error) {
        core.setFailed(error.message);
    }
}