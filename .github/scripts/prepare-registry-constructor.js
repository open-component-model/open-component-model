// @ts-check
import fs from 'fs';
import yaml from 'js-yaml';

/**
 * Prepare OCM plugin registry constructor file.
 */

/**
 * Prepare registry constructor with updated plugin reference.
 *
 * @param {Object} options - Configuration options
 * @param {string} options.constructorPath - Path to constructor template file
 * @param {string} options.registryVersion - New registry version
 * @param {string} options.pluginName - Plugin name
 * @param {string} options.pluginComponent - Plugin component name
 * @param {string} options.pluginVersion - Plugin version
 * @param {boolean} options.registryExists - Whether registry already exists
 * @param {string} [options.descriptorPath] - Path to existing registry descriptor (required if registryExists is true)
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
        descriptorPath
    } = options;

    // Load constructor template
    const template = fs.readFileSync(constructorPath, 'utf8');
    const constructor = yaml.load(template);
    constructor.version = registryVersion;

    if (registryExists) {
        if (!descriptorPath) {
            throw new Error('descriptorPath is required when registryExists is true');
        }

        const descriptor = JSON.parse(fs.readFileSync(descriptorPath, 'utf8'));
        constructor.componentReferences = descriptor.componentReferences || [];

        // We do this in a previous GitHub action outside, but just to be on the safe side, we check again.
        if (constructor.componentReferences.find(r => {
            return r.name === options.pluginName && r.version === pluginVersion;
        })) {
            throw new Error(`Plugin with name ${pluginName} and version ${pluginVersion} already exists in reference list`);
        }

    } else {
        if (!constructor.componentReferences) {
            constructor.componentReferences = [];
        }
    }

    const plugin = {
        name: pluginName,
        componentName: pluginComponent,
        version: pluginVersion
    };
    constructor.componentReferences.push(plugin)

    return {constructor}
}

/**
 * GitHub Actions entrypoint for preparing registry constructor.
 *
 * Environment variables:
 * - REGISTRY_CONSTRUCTOR: Path to constructor template file (required)
 * - REGISTRY_VERSION: New registry version (required)
 * - PLUGIN_NAME: Plugin name (required)
 * - PLUGIN_COMPONENT: Plugin component name (required)
 * - PLUGIN_VERSION: Plugin version (required)
 * - REGISTRY_EXISTS: Whether registry exists - "true" or "false" (required)
 * - DESCRIPTOR_PATH: Path to existing registry descriptor (required if REGISTRY_EXISTS is "true")
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function prepareRegistryConstructorAction({ core }) {
    const constructorPath = process.env.REGISTRY_CONSTRUCTOR;
    const registryVersion = process.env.REGISTRY_VERSION;
    const pluginName = process.env.PLUGIN_NAME;
    const pluginComponent = process.env.PLUGIN_COMPONENT;
    const pluginVersion = process.env.PLUGIN_VERSION;
    const registryExists = process.env.REGISTRY_EXISTS === 'true';
    const descriptorPath = process.env.DESCRIPTOR_PATH;

    if (!constructorPath) {
        core.setFailed('REGISTRY_CONSTRUCTOR environment variable is required');
        return;
    }

    if (!registryVersion || !pluginName || !pluginComponent || !pluginVersion) {
        core.setFailed('Missing required environment variables: REGISTRY_VERSION, PLUGIN_NAME, PLUGIN_COMPONENT, PLUGIN_VERSION');
        return;
    }

    if (registryExists && !descriptorPath) {
        core.setFailed('DESCRIPTOR_PATH is required when REGISTRY_EXISTS is true');
        return;
    }

    try {
        core.info(`Preparing registry constructor for ${pluginName} v${pluginVersion}`);

        const constructor = prepareRegistryConstructor({
            constructorPath,
            registryVersion,
            pluginName,
            pluginComponent,
            pluginVersion,
            registryExists,
            descriptorPath
        });

        // dump the rendered yaml
        const rendered = yaml.dump(constructor, { lineWidth: -1 });
        fs.writeFileSync(constructorPath, rendered, 'utf8');

        core.info(`Added plugin reference: ${pluginName} v${pluginVersion}`);
        core.info(`Registry version: ${registryVersion}`);
        core.info(`Constructor written to: ${constructorPath}`);

        await core.summary
            .addHeading('Registry Constructor Prepared')
            .addTable([
                [
                    { data: 'Field', header: true },
                    { data: 'Value', header: true },
                ],
                ['Registry Version', registryVersion],
                ['Plugin Name', pluginName],
                ['Plugin Version', pluginVersion],
            ])
            .write();
    } catch (error) {
        core.setFailed(error.message);
    }
}
