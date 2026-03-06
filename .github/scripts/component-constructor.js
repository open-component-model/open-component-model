// @ts-check
import fs from "fs";
import path from "path";
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
            if (typeof resource.input.path !== "string" || resource.input.path.length === 0) {
                throw new Error("CLI file resource is missing required field 'input.path'");
            }
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
 * GitHub Actions entrypoint: patch the CLI component constructor for publishing.
 *
 * Verifies that CLI binaries exist, parses the constructor YAML, patches paths
 * and image access, and writes the result to the target location.
 *
 * Environment variables:
 * - CONSTRUCTOR_SOURCE: Path to the source component-constructor.yaml (required)
 * - TARGET_CONSTRUCTOR: Path to write the patched constructor (required)
 * - IMAGE_REF: Full OCI image reference, e.g. "ghcr.io/owner/cli:tag" (required)
 * - IMAGE_TAG: Image tag / version string (required)
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function patchCliConstructorAction({ core }) {
    const constructorSource = process.env.CONSTRUCTOR_SOURCE;
    const targetConstructor = process.env.TARGET_CONSTRUCTOR;
    const imageRef = process.env.IMAGE_REF;
    const imageTag = process.env.IMAGE_TAG;

    if (!constructorSource) {
        core.setFailed("CONSTRUCTOR_SOURCE environment variable is required");
        return;
    }
    if (!targetConstructor) {
        core.setFailed("TARGET_CONSTRUCTOR environment variable is required");
        return;
    }
    if (!imageRef) {
        core.setFailed("IMAGE_REF environment variable is required");
        return;
    }
    if (!imageTag) {
        core.setFailed("IMAGE_TAG environment variable is required");
        return;
    }

    try {
        // Verify CLI binaries exist
        const binDir = "bin";
        const entries = fs.readdirSync(binDir).filter(f => f.startsWith("ocm-"));
        if (entries.length === 0) {
            throw new Error(`No CLI binaries found under ./${binDir}`);
        }
        core.info(`✅ Found ${entries.length} CLI binary(ies): ${entries.join(", ")}`);

        // Verify source constructor exists
        if (!fs.existsSync(constructorSource)) {
            throw new Error(`Constructor source not found: ${constructorSource}`);
        }

        // Parse, patch, and write
        const constructor = parseConstructorFile(constructorSource);
        patchCliConstructor(constructor, imageRef, imageTag);

        const targetDir = path.dirname(targetConstructor);
        fs.mkdirSync(targetDir, { recursive: true });
        fs.writeFileSync(targetConstructor, yaml.dump(constructor), "utf8");

        core.info(`✅ Patched constructor written to ${targetConstructor}`);

        // Validate round-trip
        const written = parseConstructorFile(targetConstructor);
        const imageResource = written.resources.find(r => r.name === "image");
        if (!imageResource || imageResource.access?.imageReference !== imageRef) {
            throw new Error("Validation failed: patched constructor does not contain expected image reference");
        }
        core.info(`✅ Validated image reference: ${imageRef}`);
    } catch (error) {
        core.setFailed(error.message);
    }
}

/**
 * Promote a constructor from RC to final version:
 * - Replace the top-level version
 * - Replace all resource versions
 * - Update the image resource's access.imageReference
 *
 * @param {Object} constructor - Parsed constructor YAML object (mutated in place)
 * @param {string} finalVersion - Final version string (e.g. "0.17.0")
 * @param {string} imageRef - Full OCI image reference with final tag (e.g. "ghcr.io/owner/cli:0.17.0")
 * @returns {Object} The mutated constructor object
 * @throws {Error} If expected resources or access fields are not found
 */
export function promoteConstructorVersion(constructor, finalVersion, imageRef) {
    if (!Array.isArray(constructor.resources)) {
        throw new Error("Constructor has no 'resources' array");
    }

    constructor.version = finalVersion;

    for (const resource of constructor.resources) {
        resource.version = finalVersion;
    }

    const imageResource = constructor.resources.find(r => r.name === "image");
    if (!imageResource || !imageResource.access) {
        throw new Error("No image resource with 'access' found — was the constructor patched by patchCliConstructor first?");
    }
    imageResource.access.imageReference = imageRef;

    return constructor;
}

/**
 * GitHub Actions entrypoint: promote an RC constructor to final version.
 *
 * Reads the RC constructor, replaces all version fields with the final version,
 * updates the image reference, validates the result, and writes it back.
 *
 * Environment variables:
 * - CONSTRUCTOR: Path to the component-constructor.yaml (read and written in place) (required)
 * - FINAL_VERSION: Final version string, e.g. "0.17.0" (required)
 * - IMAGE_REF: Full OCI image reference with final tag (required)
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function promoteConstructorVersionAction({ core }) {
    const constructorPath = process.env.CONSTRUCTOR;
    const finalVersion = process.env.FINAL_VERSION;
    const imageRef = process.env.IMAGE_REF;

    if (!constructorPath) {
        core.setFailed("CONSTRUCTOR environment variable is required");
        return;
    }
    if (!finalVersion) {
        core.setFailed("FINAL_VERSION environment variable is required");
        return;
    }
    if (!imageRef) {
        core.setFailed("IMAGE_REF environment variable is required");
        return;
    }

    try {
        if (!fs.existsSync(constructorPath)) {
            throw new Error(`Constructor not found: ${constructorPath}`);
        }

        const constructor = parseConstructorFile(constructorPath);
        const rcVersion = constructor.version;
        core.info(`Promoting constructor from ${rcVersion} to ${finalVersion}...`);

        promoteConstructorVersion(constructor, finalVersion, imageRef);

        fs.writeFileSync(constructorPath, yaml.dump(constructor), "utf8");
        core.info(`✅ Patched constructor written to ${constructorPath}`);

        // Validate round-trip
        const written = parseConstructorFile(constructorPath);

        if (written.version !== finalVersion) {
            throw new Error(`Validation failed: .version is '${written.version}', expected '${finalVersion}'`);
        }

        const resourceVersions = [...new Set(written.resources.map(r => r.version))];
        if (resourceVersions.length !== 1 || resourceVersions[0] !== finalVersion) {
            throw new Error(`Validation failed: resource versions are [${resourceVersions}], expected all '${finalVersion}'`);
        }

        const imageResource = written.resources.find(r => r.name === "image");
        if (!imageResource || imageResource.access?.imageReference !== imageRef) {
            throw new Error(`Validation failed: image reference is '${imageResource?.access?.imageReference}', expected '${imageRef}'`);
        }

        core.info(`✅ Validated: version=${finalVersion}, image=${imageRef}`);
    } catch (error) {
        core.setFailed(error.message);
    }
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