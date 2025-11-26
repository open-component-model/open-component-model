// @ts-check
import fs from 'fs';
import yaml from 'js-yaml';
import {computeNextVersions} from "./compute-rc-version.js";
import {execSync} from "child_process";
import {dirname} from "path";

const CLI_IMAGE = "ghcr.io/open-component-model/cli:main";
const HOME_DIR = process.env.HOME || process.env.USERPROFILE;

/**
 * Validates required environment variables.
 * @param {Object} vars - Variables to validate
 * @throws {Error} If any required variable is missing
 */
function validateEnvVars(vars) {
    const missing = Object.entries(vars)
        .filter(([_, value]) => !value)
        .map(([key, _]) => key);

    if (missing.length > 0) {
        throw new Error(`Missing required environment variables: ${missing.join(', ')}`);
    }
}

/**
 * Generate OCM config file for authentication.
 */
function generateOCMConfig() {
    const config = `type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: /.docker/config.json
          propagateConsumerIdentity: true`;

    const configPath = `${HOME_DIR}/.ocmconfig`;
    fs.writeFileSync(configPath, config);
    return configPath;
}

/**
 * Execute OCM CLI command via Docker.
 *
 * @param {import('@actions/core')} core
 * @param {string[]} args - OCM CLI arguments
 * @param {Object} [options] - Additional options
 * @param {Object.<string, string>} [options.volumes] - Volume mounts (hostPath: containerPath)
 * @param {string} [options.workdir] - Working directory
 * @param {boolean} [options.throwOnError=true] - Throw on command failure
 * @returns {string} Command output
 */
function runOcmCommand(core, args, {volumes = {}, workdir, throwOnError = true} = {}) {
    const volumeMounts = [
        `-v "${HOME_DIR}/.docker/config.json:/.docker/config.json:ro"`,
        `-v "${HOME_DIR}/.ocmconfig:/.ocmconfig:ro"`,
        `-v "/etc/ssl/certs/:/etc/ssl/certs/:ro"`,
        ...Object.entries(volumes).map(([host, container]) => `-v "${host}:${container}"`),
    ];

    const dockerCmd = [
        "docker run --rm",
        ...volumeMounts,
        workdir && `-w "${workdir}"`,
        `"${CLI_IMAGE}"`,
        ...args,
    ].filter(Boolean).join(" ");

    try {
        core.debug(`Executing: ${dockerCmd}`);
        return execSync(dockerCmd, {encoding: "utf8", stdio: "pipe"});
    } catch (error) {
        const stdout = error.stdout?.toString() || "";
        const stderr = error.stderr?.toString() || "";

        core.error(`Command failed: ${dockerCmd}`);
        if (stderr) core.error(`stderr: ${stderr}`);
        if (stdout) core.error(`stdout: ${stdout}`);

        if (throwOnError) {
            throw new Error(`OCM command failed: ${error.message}`);
        }
        return "";
    }
}

/**
 * Fetch registry descriptor from OCI repository.
 *
 * @param {import('@actions/core')} core
 * @param {string} repository - OCI repository URL
 * @param {string} componentName - Component name
 * @returns {{exists: boolean, version: string, descriptor: Object|null}}
 */
function getRegistryDescriptor(core, repository, componentName) {
    try {
        core.info(`Fetching registry descriptor: ${repository}//${componentName}`);

        const output = runOcmCommand(core, [
            "get cv",
            `${repository}//${componentName}`,
            "-ojson",
            "--loglevel=error",
            "--latest",
            `--config "/.ocmconfig"`,
        ]);

        const data = JSON.parse(output.trim());
        const component = data[0]?.component;

        if (!component) {
            core.info(`Registry not found, will create new registry`);
            return {exists: false, version: "v0.0.1", descriptor: null};
        }

        const pluginCount = component.componentReferences?.length || 0;
        core.info(`Found registry v${component.version} with ${pluginCount} plugin(s)`);

        return {
            exists: true,
            version: component.version,
            descriptor: component,
        };
    } catch (error) {
        core.warning(`Failed to fetch registry: ${error.message}`);
        return {exists: false, version: "v0.0.1", descriptor: null};
    }
}

/**
 * Prepare registry constructor with new plugin reference.
 *
 * @param {Object} params
 * @param {string} params.constructorPath - Path to constructor template
 * @param {string} params.registryVersion - Current registry version
 * @param {string} params.pluginName - Plugin name
 * @param {string} params.pluginComponent - Plugin component name
 * @param {string} params.pluginVersion - Plugin version
 * @param {boolean} params.registryExists - Whether registry exists
 * @param {Object|null} params.descriptor - Existing registry descriptor
 * @returns {Object} Updated constructor
 */
export function prepareRegistryConstructor({
    constructorPath,
    registryVersion,
    pluginName,
    pluginComponent,
    pluginVersion,
    registryExists,
    descriptor,
}) {
    const template = fs.readFileSync(constructorPath, 'utf8');
    const constructor = yaml.load(template);

    // Initialize or copy component references
    constructor.componentReferences = registryExists
        ? (descriptor?.componentReferences || [])
        : [];

    // Check for duplicate plugin version
    const isDuplicate = constructor.componentReferences.some(
        ref => ref.name === pluginName && ref.version === pluginVersion
    );
    if (isDuplicate) {
        throw new Error(`Plugin ${pluginName} v${pluginVersion} already exists in registry`);
    }

    // Calculate new version
    if (registryExists) {
        const pluginExists = constructor.componentReferences.some(ref => ref.name === pluginName);
        const {baseVersion} = computeNextVersions(registryVersion, registryVersion, "", !pluginExists);
        constructor.version = baseVersion;
    } else {
        constructor.version = "v0.0.1";
    }

    // Add new plugin reference
    constructor.componentReferences.push({
        name: pluginName,
        componentName: pluginComponent,
        version: pluginVersion,
    });

    return constructor;
}

/**
 * Publish component version to OCI repository.
 *
 * @param {import('@actions/core')} core
 * @param {string} repository - OCI repository URL
 * @param {string} constructorPath - Path to constructor file
 */
function publishComponent(core, repository, constructorPath) {
    const workdir = dirname(constructorPath);

    core.info(`Publishing component to ${repository}`);

    runOcmCommand(core, [
        "add cv",
        "--component-version-conflict-policy replace",
        `--config "/.ocmconfig"`,
        `--repository "${repository}"`,
        `--constructor "./plugin-registry-constructor.yaml"`,
        `--display-mode static`,
    ], {volumes: {[workdir]: workdir}, workdir});
}

/**
 * Verify published component exists.
 *
 * @param {import('@actions/core')} core
 * @param {string} repository - OCI repository URL
 * @param {string} componentName - Component name
 * @param {string} version - Component version
 */
function verifyComponent(core, repository, componentName, version) {
    core.info(`Verifying component: ${componentName}:${version}`);

    runOcmCommand(core, [
        "get component",
        `--config "/.ocmconfig"`,
        `"${repository}//${componentName}:${version}"`,
    ]);

    core.info("Verification successful");
}

/**
 * Main GitHub Actions entrypoint.
 *
 * Environment variables:
 * - REGISTRY_CONSTRUCTOR: Path to constructor template (required)
 * - PLUGIN_NAME: Plugin name (required)
 * - PLUGIN_COMPONENT: Plugin component name (required)
 * - PLUGIN_VERSION: Plugin version (required)
 * - OCM_REPOSITORY: OCI repository URL (required)
 * - REGISTRY_COMPONENT: Registry component name (required)
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function prepareRegistryConstructorAction({core}) {
    try {
        // Extract and validate environment variables
        const {
            REGISTRY_CONSTRUCTOR: constructorPath,
            PLUGIN_NAME: pluginName,
            PLUGIN_COMPONENT: pluginComponent,
            PLUGIN_VERSION: pluginVersion,
            OCM_REPOSITORY: ocmRepository,
            REGISTRY_COMPONENT: registryComponentName,
        } = process.env;

        validateEnvVars({
            REGISTRY_CONSTRUCTOR: constructorPath,
            PLUGIN_NAME: pluginName,
            PLUGIN_COMPONENT: pluginComponent,
            PLUGIN_VERSION: pluginVersion,
            OCM_REPOSITORY: ocmRepository,
            REGISTRY_COMPONENT: registryComponentName,
        });

        core.info(`Adding ${pluginName} v${pluginVersion} to plugin registry`);

        // Setup OCM configuration
        generateOCMConfig();

        // Pre-pull CLI image to avoid polluting JSON output
        runOcmCommand(core, ["--help"]);

        // Fetch existing registry
        const registryInfo = getRegistryDescriptor(core, ocmRepository, registryComponentName);

        // Prepare constructor with new plugin
        const constructor = prepareRegistryConstructor({
            constructorPath,
            registryVersion: registryInfo.version,
            pluginName,
            pluginComponent,
            pluginVersion,
            registryExists: registryInfo.exists,
            descriptor: registryInfo.descriptor,
        });

        // Write updated constructor
        const rendered = yaml.dump(constructor, {lineWidth: -1});
        fs.writeFileSync(constructorPath, rendered, 'utf8');
        core.debug(`Constructor:\n${rendered}`);

        // Publish to OCI registry
        publishComponent(core, ocmRepository, constructorPath);

        // Verify publication
        verifyComponent(core, ocmRepository, constructor.name, constructor.version);

        // Set outputs
        core.setOutput("new_version", constructor.version);
        core.setOutput("old_version", registryInfo.version);

        // Create summary
        await core.summary
            .addHeading('Plugin Registry Updated')
            .addTable([
                [{data: 'Field', header: true}, {data: 'Value', header: true}],
                ['Registry Version', `${registryInfo.version} → ${constructor.version}`],
                ['Plugin', `${pluginName} v${pluginVersion}`],
                ['Component', pluginComponent],
                ['Repository', `${ocmRepository}//${constructor.name}:${constructor.version}`],
            ])
            .write();

        core.info(`Successfully published registry v${constructor.version}`);
    } catch (error) {
        core.setFailed(error.message);
    }
}
