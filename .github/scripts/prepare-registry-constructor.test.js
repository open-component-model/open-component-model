import assert from 'assert';
import fs from 'fs';
import path from 'path';
import {
    prepareRegistryConstructor,
} from './prepare-registry-constructor.js';

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

assert.strictEqual(result1.version, 'v0.0.1', 'Should set registry version');
assert.strictEqual(result1.componentReferences.length, 1, 'Should have one plugin');
assert.strictEqual(result1.componentReferences[0].name, 'helminput', 'Should add plugin');
assert.strictEqual(result1.componentReferences[0].version, '1.0.0', 'Should set plugin version');

// Test 2: Existing registry
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

assert.strictEqual(result2.version, '0.2.1', 'Should update registry version');
assert.strictEqual(result2.componentReferences.length, 3, 'Should have three plugins');

// Test 3: Existing registry with multiple plugins
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

assert.strictEqual(result3.componentReferences.length, 4, 'Should have 4 plugins after push');

// Find the helminput plugin
const helminputRef = result3.componentReferences.find(r => {
    return r.name === 'helminput' && r.version === '3.2.0';
});
assert.ok(helminputRef, 'Should have helminput plugin');
assert.strictEqual(helminputRef.version, '3.2.0', 'Should update to new version');

// Test 4: Should not be able to update existing plugins
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

assert.strictEqual(result4.componentReferences.length, 2, 'Should have two plugins');
assert.strictEqual(result4.componentReferences[1].version, '2.0.0', 'Should update version');

// Test 5: Should fail if trying to add the same plugin.
const constructorTemplate5 = path.join(tmpDir, 'constructor5.yaml');
fs.writeFileSync(constructorTemplate5, `name: ocm.software/plugin-registry
version: ((REGISTRY_VERSION))
provider:
  name: ocm.software
componentReferences: []
`);

const descriptorPath5 = path.join(tmpDir, 'descriptor5.json');
fs.writeFileSync(descriptorPath5, JSON.stringify({
    componentReferences: [
        { name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '1.0.0' }
    ]
}));

assert.throws(() => {
    prepareRegistryConstructor({
        constructorPath: constructorTemplate4,
        registryVersion: 'v0.4.0',
        pluginName: 'helminput',
        pluginComponent: 'ocm.software/plugins/helminput',
        pluginVersion: '1.0.0',
        registryExists: true,
        descriptorPath: descriptorPath5
    });
},
    /Plugin with name helminput and version 1.0.0 already exists in reference list/,
    'Should throw when descriptorPath missing for existing registry'
);

// Test 6: Should increase the constructor version
const constructorTemplate6 = path.join(tmpDir, 'constructor6.yaml');
fs.writeFileSync(constructorTemplate6, `name: ocm.software/plugin-registry
version: ((REGISTRY_VERSION))
provider:
  name: ocm.software
componentReferences: []
`);

const descriptorPath6 = path.join(tmpDir, 'descriptor6.json');
fs.writeFileSync(descriptorPath6, JSON.stringify({
    componentReferences: [
        { name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '1.0.0' }
    ]
}));

const result6 = prepareRegistryConstructor({
    constructorPath: constructorTemplate6,
    registryVersion: 'v0.6.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '2.0.0',
    registryExists: true,
    descriptorPath: descriptorPath6
});

assert.strictEqual(result6.componentReferences.length, 2, 'Should have two plugins');
assert.strictEqual(result6.componentReferences[1].version, '2.0.0', 'Should update version');
assert.strictEqual(result6.version, '0.7.0', 'Should update version of the plugin root component version');
assert.strictEqual(result6.name, 'ocm.software/plugin-registry', 'Should set component version name');

// Cleanup
fs.rmSync(tmpDir, { recursive: true, force: true });

console.log('✅ All tests passed.');
