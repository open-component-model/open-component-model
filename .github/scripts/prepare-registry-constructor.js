// @ts-check
import fs from 'fs';
import yaml from 'js-yaml';
import {computeNextVersions} from "./compute-rc-version.js";
import {execSync} from "child_process";
import {dirname} from "path";

/**
 * Prepare OCM plugin registry constructor file.
 */

// The image of the ocm CLI.
const cliImage = "ghcr.io/open-component-model/cli:main"

/**
 * Generates an OCM config for the OCM CLI calls.
 * @returns {string} Location of the config file.
 */
function generateOCMConfig() {
    const homeDir = process.env.HOME || process.env.USERPROFILE;
    const config = `type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: /.docker/config.json
          propagateConsumerIdentity: true`;

    const ocmConfig = `${homeDir}/.ocmconfig`;
    fs.writeFileSync(ocmConfig, config);

    return ocmConfig;
}

/**
 * Execute OCM CLI command in Docker container.
 *
 * @param {import('@actions/github-core')} core
 * @param {Object} options - Command options
 * @param {string[]} options.args - OCM CLI arguments
 * @param {Object.<string, string>} [options.volumes] - Additional volume mounts (hostPath: containerPath)
 * @param {string} [options.workdir] - Working directory inside container
 * @param {boolean} [options.throwOnError] - Whether to throw on non-zero exit (default: true)
 * @returns {string} Command output
 */
function runOcmCommand({core, args, volumes = {}, workdir, throwOnError = true}) {
    const homeDir = process.env.HOME || process.env.USERPROFILE;

    // always required
    const volumeMounts = [
        `-v "${homeDir}/.docker/config.json:/.docker/config.json:ro"`,
        `-v "${homeDir}/.ocmconfig:/.ocmconfig:ro"`,
        `-v "/etc/ssl/certs/:/etc/ssl/certs/:ro"`,
    ];

    // add workdir and the other required mounts for the push call
    for (const [hostPath, containerPath] of Object.entries(volumes)) {
        volumeMounts.push(`-v "${hostPath}:${containerPath}"`);
    }

    // build the command
    const dockerCmd = [
        "docker run --rm",
        ...volumeMounts,
        workdir ? `-w "${workdir}"` : "",
        `"${cliImage}"`,
        ...args,
    ]
        .filter(Boolean)
        .join(" ");

    try {
        core.info(`running the following command: ${dockerCmd}`);

        return execSync(dockerCmd, {
            encoding: "utf8",
            stdio: "pipe",
        })
    } catch (error) {
        if (throwOnError) {
            const stdout = error.stdout?.toString() || "";
            const stderr = error.stderr?.toString() || "";

            core.warning("From command output stderr: ", stderr);
            core.warning("From command output stdout: ", stdout);

            throw new Error(
                `OCM command failed: ${error.message}\nCommand: ${dockerCmd}`
            );
        }
        return "";
    }
}


/**
 * Prepare registry constructor with updated plugin reference and calculated new version based on whether the plugin
 * exists or not in the registry. If the plugin exists, we bump as a patch version if it doesn't we increase the minor version.
 *
 * @param {Object} options - Configuration options
 * @param {string} options.constructorPath - Path to constructor template file
 * @param {string} options.registryVersion - New registry version
 * @param {string} options.pluginName - Plugin name
 * @param {string} options.pluginComponent - Plugin component name
 * @param {string} options.pluginVersion - Plugin version
 * @param {boolean} options.registryExists - Whether registry already exists
 * @param {Object} [options.descriptor] - The actual descriptor of the root registry
 * @returns {Object} constructor - The constructor object that is created from the template.
 */
export function prepareRegistryConstructor(options) {
    const {
        constructorPath,
        registryVersion,
        pluginName,
        pluginComponent,
        pluginVersion,
        registryExists,
        descriptor,
    } = options;

    const template = fs.readFileSync(constructorPath, 'utf8');
    const constructor = yaml.load(template);

    if (registryExists) {
        if (!descriptor) {
            throw new Error("Registry descriptor is missing.");
        }

        constructor.componentReferences = descriptor.componentReferences || [];

        if (constructor.componentReferences.find(r => {
            return r.name === pluginName && r.version === pluginVersion;
        })) {
            throw new Error(`Plugin with name ${pluginName} and version ${pluginVersion} already exists in reference list`);
        }

        // Compute the new version. If the plugin does not exist we increase the minor version.
        const pluginExists = constructor.componentReferences.find(r => r.name === pluginName);
        const nextVersion = computeNextVersions(registryVersion, registryVersion, "", !pluginExists);
        constructor.version = nextVersion.baseVersion;

    } else {
        if (!constructor.componentReferences) {
            constructor.componentReferences = [];
        }

        constructor.version = "v0.0.1"
    }

    const plugin = {
        name: pluginName,
        componentName: pluginComponent,
        version: pluginVersion
    };
    constructor.componentReferences.push(plugin)

    return constructor
}

/**
 * Return descriptor information for the plugin registry.
 * @typedef RegistryInfo
 * @property {string} version The version of the registry.
 * @property {boolean} exists If the registry exists or not.
 * @property {Object|null} descriptor The descriptor for the registry.
 */

/** getRegistryDescriptor returns information about the registry.
 *
 * @param {string} repository Defines the name of the repository
 * @param {string} componentName Is the name of the component for the plugin registry
 * @param {string} configPath Is the path to the OCM configuration
 * @param {import('@actions/core')} core
 * @return {RegistryInfo} Registry information
 */
function getRegistryDescriptor(repository, componentName, configPath, core) {
    try {
        core.info(`Fetching existing registry descriptor for ${repository}//${componentName}`);
        const output = runOcmCommand({
            core: core,
            args: [
                "get cv",
                `${repository}//${componentName}`,
                "-ojson",
                "--loglevel=error",
                "--latest",
                `--config "/.ocmconfig"`,
            ],
            throwOnError: true,
        });

        const data = JSON.parse(output.trim());
        const component = data[0]?.component;

        if (!component) {
            core.warning(`no component found in ${repository} with name ${componentName}`);

            return {
                exists: false,
                version: "v0.0.1",
                descriptor: null,
            }
        }

        core.info(`Found registry with ${component.componentReferences?.length || 0} existing plugins`);
        return {
            exists: true,
            version: component.version,
            descriptor: component,
        }
    } catch (error) {
        // If command fails or JSON parse fails, registry doesn't exist
        core.warning(`Failed to fetch registry: ${error.message}`);

        return {
            exists: false,
            version: "v0.0.1",
            descriptor: null,
        };
    }
}

/**
 * GitHub Actions entrypoint for preparing registry constructor.
 *
 * Environment variables:
 * - REGISTRY_CONSTRUCTOR: Path to constructor template file (required)
 * - PLUGIN_NAME: Plugin name (required)
 * - PLUGIN_COMPONENT: Plugin component name (required)
 * - PLUGIN_VERSION: Plugin version (required)
 * - OCM_REPOSITORY: The repository of the plugin registry component
 * - REGISTRY_COMPONENT: The name of the registry.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function prepareRegistryConstructorAction({core}) {
    const constructorPath = process.env.REGISTRY_CONSTRUCTOR;
    const pluginName = process.env.PLUGIN_NAME;
    const pluginComponent = process.env.PLUGIN_COMPONENT;
    const pluginVersion = process.env.PLUGIN_VERSION;
    const ocmRepository = process.env.OCM_REPOSITORY;
    const registryComponentName = process.env.REGISTRY_COMPONENT;

    if (!pluginName || !pluginComponent || !pluginVersion) {
        core.setFailed('Missing required environment variables: REGISTRY_VERSION, PLUGIN_NAME, PLUGIN_COMPONENT, PLUGIN_VERSION');
        return;
    }

    try {
        core.info(`Preparing registry constructor for ${pluginName} v${pluginVersion}`);

        const ocmConfig = generateOCMConfig() // generate the required OCM config.

        // Pre-fetch the cli because it pollutes the command output, and we can't get ONLY the JSON output.
        runOcmCommand({
            core: core,
            args: [
                "--help",
            ],
            throwOnError: true,
        });

        // get the registry descriptor and check if it exists
        const registryInfo = getRegistryDescriptor(
            ocmRepository,
            registryComponentName,
            ocmConfig,
            core
        )

        // generate the constructor
        const result = prepareRegistryConstructor({
            core: core,
            constructorPath: constructorPath,
            registryVersion: registryInfo.version,
            pluginName: pluginName,
            pluginComponent: pluginComponent,
            pluginVersion: pluginVersion,
            registryExists: registryInfo.exists,
            descriptor: registryInfo.descriptor,
        });

        const rendered = yaml.dump(result, {lineWidth: -1});
        fs.writeFileSync(constructorPath, rendered, 'utf8');

        core.info(`Preparing to publish the registry component with constructor: ${rendered}`);
        // push the new plugin registry component
        const workdir = dirname(constructorPath);
        runOcmCommand({
            core: core,
            args: [
                "add cv",
                "--component-version-conflict-policy replace",
                `--config "/.ocmconfig"`,
                `--repository "${ocmRepository}"`,
                `--constructor "./plugin-registry-constructor.yaml"`,
                `--display-mode static`,
            ], volumes: {
                [workdir]: workdir,
            }, workdir,
            throwOnError: true,
        });
        core.info("Successfully publish the registry component, running verification...")

        // verify that the component exists
        runOcmCommand({
            core: core,
            args: [
                "get component",
                `--config "/.ocmconfig"`,
                `"${ocmRepository}//${result.name}:${result.version}"`,
            ],
            throwOnError: true,
        });

        core.info(`Added plugin reference: ${pluginName} v${pluginVersion}`);
        core.info(`Registry version: ${result.version}`);
        core.info(`Constructor written to: ${constructorPath}`);

        // set this value for the Summary and the Verify action.
        core.setOutput("new_version", result.version);
        core.setOutput("old_version", registryInfo.version);

        await core.summary
            .addHeading('Registry Constructor Prepared')
            .addTable([
                [
                    {data: 'Field', header: true},
                    {data: 'Value', header: true},
                ],
                ['New registry Version', result.version],
                ['Plugin Name', pluginName],
                ['Plugin Version', pluginVersion],
            ])
            .write();
    } catch (error) {
        core.setFailed(error.message);
    }
}
