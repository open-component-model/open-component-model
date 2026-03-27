import assert from 'assert';
import fs from 'fs';
import path from 'path';
import {prepareRegistryConstructor,} from './prepare-registry-constructor.js';

function mockCore() {
    return {
        warning: (msg) => {
            console.warn(msg)
        },
    };
}

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

const result1 = prepareRegistryConstructor(mockCore(), {
    constructorPath: constructorTemplate1,
    registryVersion: 'v0.1.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '1.0.0',
    registryExists: false,
    descriptor: [],
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

let componentReferences2 = [
    {name: 'plugin1', componentName: 'ocm.software/plugins/plugin1', version: '1.0.0'},
    {name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0'}
]

const result2 = prepareRegistryConstructor(mockCore(), {
    constructorPath: constructorTemplate2,
    registryVersion: 'v0.2.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '3.0.0',
    registryExists: true,
    descriptor: {componentReferences: componentReferences2},
});

assert.strictEqual(result2.version, '0.3.0', 'Should update registry version');
assert.strictEqual(result2.componentReferences.length, 3, 'Should have three plugins');

// Test 3: Existing registry with multiple plugins
const constructorTemplate3 = path.join(tmpDir, 'constructor3.yaml');
fs.writeFileSync(constructorTemplate3, `name: ocm.software/plugin-registry
version: ((REGISTRY_VERSION))
provider:
  name: ocm.software
componentReferences: []
`);

const componentReferences3 = [
    {name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '3.0.8'},
    {name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '3.1.0'},
    {name: 'plugin2', componentName: 'ocm.software/plugins/plugin2', version: '2.0.0'}
]

const result3 = prepareRegistryConstructor(mockCore(), {
    constructorPath: constructorTemplate3,
    registryVersion: 'v0.3.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '3.2.0',
    registryExists: true,
    descriptor: {componentReferences: componentReferences3}
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

const componentReferences4 = [
    {name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '1.0.0'},
]

const result4 = prepareRegistryConstructor(mockCore(), {
    constructorPath: constructorTemplate4,
    registryVersion: 'v0.4.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '2.0.0',
    registryExists: true,
    descriptor: {componentReferences: componentReferences4}
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

const componentReferences5 = [
    {name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '1.0.0'}
]

assert.throws(() => {
        prepareRegistryConstructor(mockCore(), {
            constructorPath: constructorTemplate4,
            registryVersion: 'v0.4.0',
            pluginName: 'helminput',
            pluginComponent: 'ocm.software/plugins/helminput',
            pluginVersion: '1.0.0',
            registryExists: true,
            descriptor: {componentReferences: componentReferences5}
        });
    },
    /Plugin helminput v1.0.0 already exists in registry/,
    'Should throw when plugin version already exists'
);

// Test 6: Should increase the constructor version
const constructorTemplate6 = path.join(tmpDir, 'constructor6.yaml');
fs.writeFileSync(constructorTemplate6, `name: ocm.software/plugin-registry
version: ((REGISTRY_VERSION))
provider:
  name: ocm.software
componentReferences: []
`);

const componentReferences6 = [
    {name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: '1.0.0'}
]

const result6 = prepareRegistryConstructor(mockCore(), {
    constructorPath: constructorTemplate6,
    registryVersion: 'v0.6.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: '2.0.0',
    registryExists: true,
    descriptor: {componentReferences: componentReferences6},
});

assert.strictEqual(result6.componentReferences.length, 2, 'Should have two plugins');
assert.strictEqual(result6.componentReferences[1].version, '2.0.0', 'Should update version');
assert.strictEqual(result6.version, '0.6.1', 'Should update version of the plugin root component version');
assert.strictEqual(result6.name, 'ocm.software/plugin-registry', 'Should set component version name');

// Test 7: Should allow overriding a plugin with version v0.0.0-main and emit a warning
const constructorTemplate7 = path.join(tmpDir, 'constructor7.yaml');
fs.writeFileSync(constructorTemplate7, `name: ocm.software/plugin-registry
version: ((REGISTRY_VERSION))
provider:
  name: ocm.software
componentReferences: []
`);

const componentReferences7 = [
    {name: 'helminput', componentName: 'ocm.software/plugins/helminput', version: 'v0.0.0-main'}
]

const warnings7 = [];
const mockCore7 = {
    warning: (msg) => {
        warnings7.push(msg);
    },
};

const result7 = prepareRegistryConstructor(mockCore7, {
    constructorPath: constructorTemplate7,
    registryVersion: 'v0.7.0',
    pluginName: 'helminput',
    pluginComponent: 'ocm.software/plugins/helminput',
    pluginVersion: 'v0.0.0-main',
    registryExists: true,
    descriptor: {componentReferences: componentReferences7},
});

assert.strictEqual(warnings7.length, 1, 'Should emit exactly one warning');
assert.ok(warnings7[0].includes('v0.0.0-main'), 'Warning should mention v0.0.0-main');
assert.ok(warnings7[0].includes('helminput'), 'Warning should mention plugin name');
assert.strictEqual(result7.componentReferences.length, 2, 'Should allow adding the plugin despite same version');

// Cleanup
fs.rmSync(tmpDir, {recursive: true, force: true});

console.log('✅ All tests passed.');
