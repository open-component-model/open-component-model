// @ts-check
import fs from 'fs';
import yaml from 'js-yaml';

/**
 * Prepare OCM plugin registry constructor file.
 * Handles loading existing registry, deduplicating plugins, and adding new plugin versions.
 */

/**
 * Deduplicate component references by plugin name.
 * When multiple references with the same name exist, keeps only the last occurrence.
 *
 * @param {Array<{name: string, componentName: string, version: string}>} refs - Component references
 * @returns {Array<{name: string, componentName: string, version: string}>} Deduplicated references
 */
export function deduplicateReferences(refs) {
    const refMap = new Map();
    for (const ref of refs) {
        refMap.set(ref.name, ref);
    }
    return Array.from(refMap.values());
}

/**
 * Update plugin reference in the component references list.
 * Removes any existing reference with the same name and adds the new one.
 *
 * @param {Array<{name: string, componentName: string, version: string}>} refs - Component references
 * @param {{name: string, componentName: string, version: string}} plugin - Plugin to add/update
 * @returns {Array<{name: string, componentName: string, version: string}>} Updated references
 */
export function updatePluginReference(refs, plugin) {
    // Remove existing entry for this plugin
    const filtered = refs.filter(ref => ref.name !== plugin.name);
    // Add the new version
    return [...filtered, plugin];
}

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
 * @returns {{constructor: object, stats: {totalRefs: number, deduplicatedRefs: number, isNewRegistry: boolean}}}
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

    let totalRefs = 0;
    let deduplicatedRefs = 0;

    if (registryExists) {
        if (!descriptorPath) {
            throw new Error('descriptorPath is required when registryExists is true');
        }

        // Load existing componentReferences from descriptor
        const descriptor = JSON.parse(fs.readFileSync(descriptorPath, 'utf8'));
        const existingRefs = descriptor.componentReferences || [];
        totalRefs = existingRefs.length;

        // Deduplicate by plugin name
        constructor.componentReferences = deduplicateReferences(existingRefs);
        deduplicatedRefs = constructor.componentReferences.length;
    } else {
        // New registry
        if (!constructor.componentReferences) {
            constructor.componentReferences = [];
        }
        deduplicatedRefs = 0;
    }

    // Update plugin reference
    const plugin = {
        name: pluginName,
        componentName: pluginComponent,
        version: pluginVersion
    };
    constructor.componentReferences = updatePluginReference(
        constructor.componentReferences,
        plugin
    );

    return {
        constructor,
        stats: {
            totalRefs,
            deduplicatedRefs,
            isNewRegistry: !registryExists
        }
    };
}

/**
 * Write constructor to YAML file.
 *
 * @param {object} constructor - Constructor object
 * @param {string} outputPath - Output file path
 */
export function writeConstructor(constructor, outputPath) {
    const rendered = yaml.dump(constructor, { lineWidth: -1 });
    fs.writeFileSync(outputPath, rendered, 'utf8');
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

        const { constructor, stats } = prepareRegistryConstructor({
            constructorPath,
            registryVersion,
            pluginName,
            pluginComponent,
            pluginVersion,
            registryExists,
            descriptorPath
        });

        // Write constructor
        writeConstructor(constructor, constructorPath);

        // Output stats
        if (stats.isNewRegistry) {
            core.info('Creating new registry');
        } else {
            core.info(`Loaded ${stats.totalRefs} references, deduplicated to ${stats.deduplicatedRefs}`);
        }
        core.info(`Added plugin reference: ${pluginName} v${pluginVersion}`);
        core.info(`Registry version: ${registryVersion}`);
        core.info(`Constructor written to: ${constructorPath}`);

        await core.summary
            .addHeading('📦 Registry Constructor Prepared')
            .addTable([
                [
                    { data: 'Field', header: true },
                    { data: 'Value', header: true },
                ],
                ['Registry Version', registryVersion],
                ['Plugin Name', pluginName],
                ['Plugin Version', pluginVersion],
                ['New Registry', stats.isNewRegistry ? 'Yes' : 'No'],
                ['Total References', stats.isNewRegistry ? '0' : stats.totalRefs.toString()],
                ['After Deduplication', constructor.componentReferences.length.toString()],
            ])
            .write();
    } catch (error) {
        core.setFailed(error.message);
    }
}
