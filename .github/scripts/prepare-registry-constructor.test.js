import assert from 'assert';
import fs from 'fs';
import path from 'path';
import {
    deduplicateReferences,
    updatePluginReference,
    prepareRegistryConstructor,
    writeConstructor
} from './prepare-registry-constructor.js';

// ============================================================================
// deduplicateReferences tests
// ============================================================================
console.log('Testing deduplicateReferences...');

assert.deepStrictEqual(
    deduplicateReferences([
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' },
        { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0' }
    ]),
    [
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' },
        { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0' }
    ],
    'Should keep unique references unchanged'
);

assert.deepStrictEqual(
    deduplicateReferences([
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' },
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.1.0' }
    ]),
    [
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.1.0' }
    ],
    'Should keep last occurrence when duplicates exist'
);

const deduped = deduplicateReferences([
    { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' },
    { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0' },
    { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.1.0' },
    { name: 'plugin3', componentName: 'ocm.software/plugins/plugin3', version: '3.0.0' },
    { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.1.0' }
]);

assert.strictEqual(deduped.length, 3, 'Should have 3 unique plugins');
assert.ok(deduped.find(p => p.name === 'plugin1' && p.version === '1.1.0'), 'Should keep last plugin1');
assert.ok(deduped.find(p => p.name === 'plugin2' && p.version === '2.1.0'), 'Should keep last plugin2');
assert.ok(deduped.find(p => p.name === 'plugin3' && p.version === '3.0.0'), 'Should keep plugin3');

assert.deepStrictEqual(
    deduplicateReferences([]),
    [],
    'Should handle empty array'
);

// ============================================================================
// updatePluginReference tests
// ============================================================================
console.log('Testing updatePluginReference...');

assert.deepStrictEqual(
    updatePluginReference(
        [
            { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' }
        ],
        { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0' }
    ),
    [
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' },
        { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0' }
    ],
    'Should add new plugin when it does not exist'
);

assert.deepStrictEqual(
    updatePluginReference(
        [
            { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' }
        ],
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.1.0' }
    ),
    [
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.1.0' }
    ],
    'Should replace existing plugin with same name'
);

assert.deepStrictEqual(
    updatePluginReference(
        [
            { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' },
            { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0' },
            { name: 'plugin3', componentName: 'ocm.software/plugins/plugin3', version: '3.0.0' }
        ],
        { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.5.0' }
    ),
    [
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' },
        { name: 'plugin3', componentName: 'ocm.software/plugins/plugin3', version: '3.0.0' },
        { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.5.0' }
    ],
    'Should update plugin in middle of list'
);

assert.deepStrictEqual(
    updatePluginReference(
        [],
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' }
    ),
    [
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' }
    ],
    'Should add plugin to empty list'
);

// ============================================================================
// prepareRegistryConstructor tests
// ============================================================================
console.log('Testing prepareRegistryConstructor...');

// Setup test files
const tmpDir = fs.mkdtempSync(path.join('/tmp', 'registry-test-'));

// Test 1: New registry
const constructorTemplate1 = path.join(tmpDir, 'constructor1.yaml');
fs.writeFileSync(constructorTemplate1, `name: ocm.software/plugin-registry
version: ((REGISTRY_VERSION))
provider:
  name: ocm.software
componentReferences: []
`);

const result1 = prepareRegistryConstructor({
    constructorPath: constructorTemplate1,
    registryVersion: 'v0.1.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '1.0.0',
    registryExists: false
});

assert.strictEqual(result1.constructor.version, 'v0.1.0', 'Should set registry version');
assert.strictEqual(result1.constructor.componentReferences.length, 1, 'Should have one plugin');
assert.strictEqual(result1.constructor.componentReferences[0].name, 'helminput', 'Should add plugin');
assert.strictEqual(result1.constructor.componentReferences[0].version, '1.0.0', 'Should set plugin version');
assert.strictEqual(result1.stats.isNewRegistry, true, 'Should be new registry');
assert.strictEqual(result1.stats.totalRefs, 0, 'Should have zero existing refs');

// Test 2: Existing registry with no duplicates
const constructorTemplate2 = path.join(tmpDir, 'constructor2.yaml');
fs.writeFileSync(constructorTemplate2, `name: ocm.software/plugin-registry
version: ((REGISTRY_VERSION))
provider:
  name: ocm.software
componentReferences: []
`);

const descriptorPath2 = path.join(tmpDir, 'descriptor2.json');
fs.writeFileSync(descriptorPath2, JSON.stringify({
    componentReferences: [
        { name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0' },
        { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0' }
    ]
}));

const result2 = prepareRegistryConstructor({
    constructorPath: constructorTemplate2,
    registryVersion: 'v0.2.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '3.0.0',
    registryExists: true,
    descriptorPath: descriptorPath2
});

assert.strictEqual(result2.constructor.version, 'v0.2.0', 'Should update registry version');
assert.strictEqual(result2.constructor.componentReferences.length, 3, 'Should have three plugins');
assert.strictEqual(result2.stats.isNewRegistry, false, 'Should not be new registry');
assert.strictEqual(result2.stats.totalRefs, 2, 'Should have two existing refs');
assert.strictEqual(result2.stats.deduplicatedRefs, 2, 'Should not deduplicate');

// Test 3: Existing registry with duplicates
const constructorTemplate3 = path.join(tmpDir, 'constructor3.yaml');
fs.writeFileSync(constructorTemplate3, `name: ocm.software/plugin-registry
version: ((REGISTRY_VERSION))
provider:
  name: ocm.software
componentReferences: []
`);

const descriptorPath3 = path.join(tmpDir, 'descriptor3.json');
fs.writeFileSync(descriptorPath3, JSON.stringify({
    componentReferences: [
        { name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '3.0.8' },
        { name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '3.1.0' },
        { name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0' }
    ]
}));

const result3 = prepareRegistryConstructor({
    constructorPath: constructorTemplate3,
    registryVersion: 'v0.3.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '3.2.0',
    registryExists: true,
    descriptorPath: descriptorPath3
});

assert.strictEqual(result3.constructor.componentReferences.length, 2, 'Should have two plugins after dedup');
assert.strictEqual(result3.stats.totalRefs, 3, 'Should have three existing refs');
assert.strictEqual(result3.stats.deduplicatedRefs, 2, 'Should deduplicate to two');

// Find the helminput plugin
const helminputRef = result3.constructor.componentReferences.find(r => r.name === 'helminput');
assert.ok(helminputRef, 'Should have helminput plugin');
assert.strictEqual(helminputRef.version, '3.2.0', 'Should update to new version');

// Test 4: Updating existing plugin
const constructorTemplate4 = path.join(tmpDir, 'constructor4.yaml');
fs.writeFileSync(constructorTemplate4, `name: ocm.software/plugin-registry
version: ((REGISTRY_VERSION))
provider:
  name: ocm.software
componentReferences: []
`);

const descriptorPath4 = path.join(tmpDir, 'descriptor4.json');
fs.writeFileSync(descriptorPath4, JSON.stringify({
    componentReferences: [
        { name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '1.0.0' }
    ]
}));

const result4 = prepareRegistryConstructor({
    constructorPath: constructorTemplate4,
    registryVersion: 'v0.4.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '2.0.0',
    registryExists: true,
    descriptorPath: descriptorPath4
});

assert.strictEqual(result4.constructor.componentReferences.length, 1, 'Should still have one plugin');
assert.strictEqual(result4.constructor.componentReferences[0].version, '2.0.0', 'Should update version');

// ============================================================================
// writeConstructor tests
// ============================================================================
console.log('Testing writeConstructor...');

const outputPath = path.join(tmpDir, 'output.yaml');
const testConstructor = {
    name: 'ocm.software/plugin-registry',
    version: 'v1.0.0',
    componentReferences: [
        { name: 'test', componentName: 'ocm.software/plugins/test', version: '1.0.0' }
    ]
};

writeConstructor(testConstructor, outputPath);

assert.ok(fs.existsSync(outputPath), 'Should create output file');
const written = fs.readFileSync(outputPath, 'utf8');
assert.ok(written.includes('name: ocm.software/plugin-registry'), 'Should contain registry name');
assert.ok(written.includes('version: v1.0.0'), 'Should contain version');
assert.ok(written.includes('test'), 'Should contain plugin name');

// ============================================================================
// Error handling tests
// ============================================================================
console.log('Testing error handling...');

assert.throws(
    () => {
        prepareRegistryConstructor({
            constructorPath: constructorTemplate1,
            registryVersion: 'v1.0.0',
            pluginName: 'test',
            pluginComponent: 'test',
            pluginVersion: '1.0.0',
            registryExists: true
            // Missing descriptorPath
        });
    },
    /descriptorPath is required/,
    'Should throw when descriptorPath missing for existing registry'
);

// Cleanup
fs.rmSync(tmpDir, { recursive: true, force: true });

console.log('✅ All tests passed.');
