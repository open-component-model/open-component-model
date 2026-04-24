// Tests for cutoff-version.js

// Run: `npm test` or `node --test scripts/cutoff-version.test.js`

const test = require('node:test');
const assert = require('node:assert/strict');
const { parseArguments, hasMountForVersion, hasAnyImportForVersion, hasAllImportsForVersion, buildModuleBlocks, compareSemver, assignVersionWeights, pinLatestContentMount } = require('./cutoff-version');

// Test parseArguments
test('parseArguments: valid version', () => {
    const result = parseArguments(['1.2.3']);
    assert.equal(result.version, '1.2.3');
    assert.equal(result.keepDefault, false);
});

test('parseArguments: with --keepDefault', () => {
    const result = parseArguments(['1.2.3', '--keepDefault']);
    assert.equal(result.version, '1.2.3');
    assert.equal(result.keepDefault, true);
});

test('parseArguments: missing version throws', () => {
    assert.throws(() => parseArguments([]), /Missing version/);
});

test('parseArguments: invalid version throws', () => {
    assert.throws(() => parseArguments(['v1.2.3']), /Invalid version/);
    assert.throws(() => parseArguments(['1.2']), /Invalid version/);
    assert.throws(() => parseArguments(['--keepDefault', 'test', '1.2.3']), /Invalid version/);
});

test('parseArguments: unknown flag throws', () => {
    assert.throws(() => parseArguments(['1.2.3', '--unknown']), /Unknown flag/);
});

// Test hasMountForVersion
test('hasMountForVersion: returns true/false correctly', () => {
    const parsed = { mounts: [{ sites: { matrix: { versions: ['1.0.0'] } } }] };
    assert.equal(hasMountForVersion(parsed, '1.0.0'), true);
    assert.equal(hasMountForVersion(parsed, '2.0.0'), false);
    assert.equal(hasMountForVersion(null, '1.0.0'), false);
    assert.equal(hasMountForVersion({}, '1.0.0'), false);
});

// Test hasAnyImportForVersion
test('hasAnyImportForVersion: returns true/false correctly', () => {
    const parsed = { imports: [{ mounts: [{ sites: { matrix: { versions: ['1.0.0'] } } }] }] };
    assert.equal(hasAnyImportForVersion(parsed, '1.0.0'), true);
    assert.equal(hasAnyImportForVersion(parsed, '2.0.0'), false);
    assert.equal(hasAnyImportForVersion(null, '1.0.0'), false);
    assert.equal(hasAnyImportForVersion({}, '1.0.0'), false);
});

// Test hasAllImportsForVersion
test('hasAllImportsForVersion: returns true when all 4 imports exist', () => {
    const { imports } = buildModuleBlocks('1.0.0');
    const parsed = { imports };
    assert.equal(hasAllImportsForVersion(parsed, '1.0.0'), true);
});

test('hasAllImportsForVersion: returns false when only a subset of imports exist', () => {
    const { imports } = buildModuleBlocks('1.0.0');
    // Keep only the CLI import (1 of 4)
    const parsed = { imports: [imports[0]] };
    assert.equal(hasAllImportsForVersion(parsed, '1.0.0'), false);
});

test('hasAllImportsForVersion: returns false when no imports exist', () => {
    assert.equal(hasAllImportsForVersion({}, '1.0.0'), false);
    assert.equal(hasAllImportsForVersion(null, '1.0.0'), false);
});

test('hasAllImportsForVersion: returns false for wrong version', () => {
    const { imports } = buildModuleBlocks('1.0.0');
    const parsed = { imports };
    assert.equal(hasAllImportsForVersion(parsed, '2.0.0'), false);
});

// Test buildModuleBlocks
test('buildModuleBlocks: returns correct mount', () => {
    const { mount } = buildModuleBlocks('1.2.3');
    assert.deepEqual(mount.files, ['**', '!blog/**']);
    assert.equal(mount.source, 'content_versioned/version-1.2.3');
    assert.equal(mount.target, 'content');
    assert.deepEqual(mount.sites.matrix.versions, ['1.2.3']);
});

test('buildModuleBlocks: returns 4 imports (CLI + 3 schema)', () => {
    const { imports } = buildModuleBlocks('1.2.3');
    assert.equal(imports.length, 4);
});

test('buildModuleBlocks: CLI import is correct', () => {
    const { imports } = buildModuleBlocks('1.2.3');
    const cli = imports.find(i => i.path.endsWith('/cli'));
    assert.ok(cli, 'CLI import should exist');
    assert.equal(cli.version, 'v1.2.3');
    assert.equal(cli.mounts[0].target, 'content/docs/reference/ocm-cli');
    assert.deepEqual(cli.mounts[0].sites.matrix.versions, ['1.2.3']);
});

test('buildModuleBlocks: schema imports have correct targets', () => {
    const { imports } = buildModuleBlocks('2.0.0');
    const targets = imports.map(i => i.mounts[0].target).sort();
    assert.deepEqual(targets, [
        'content/docs/reference/ocm-cli',
        'static/2.0.0/schemas/bindings/go/constructor',
        'static/2.0.0/schemas/bindings/go/descriptor/v2',
        'static/2.0.0/schemas/kubernetes/controller',
    ]);
});

test('buildModuleBlocks: CLI and controller use versioned tag, bindings use latest', () => {
    const { imports } = buildModuleBlocks('3.1.4');
    const cli = imports.find(i => i.path.endsWith('/cli'));
    const controller = imports.find(i => i.path.endsWith('/kubernetes/controller'));
    const constructor = imports.find(i => i.path.endsWith('/bindings/go/constructor'));
    const descriptor = imports.find(i => i.path.endsWith('/bindings/go/descriptor/v2'));
    assert.equal(cli.version, 'v3.1.4');
    assert.equal(controller.version, 'v3.1.4');
    assert.equal(constructor.version, 'latest');
    assert.equal(descriptor.version, 'latest');
});

test('buildModuleBlocks: schema imports have correct sources', () => {
    const { imports } = buildModuleBlocks('1.0.0');
    const schemaImports = imports.filter(i => !i.path.endsWith('/cli'));
    const sources = schemaImports.map(i => i.mounts[0].source).sort();
    assert.deepEqual(sources, [
        'config/crd/bases',
        'resources',
        'spec/v1/resources',
    ]);
});

// Test compareSemver
test('compareSemver: equal versions return 0', () => {
    assert.equal(compareSemver('1.2.3', '1.2.3'), 0);
    assert.equal(compareSemver('0.0.0', '0.0.0'), 0);
});

test('compareSemver: major version difference', () => {
    assert.ok(compareSemver('2.0.0', '1.0.0') > 0);
    assert.ok(compareSemver('1.0.0', '2.0.0') < 0);
});

test('compareSemver: minor version difference', () => {
    assert.ok(compareSemver('1.2.0', '1.1.0') > 0);
    assert.ok(compareSemver('1.1.0', '1.2.0') < 0);
});

test('compareSemver: patch version difference', () => {
    assert.ok(compareSemver('1.0.2', '1.0.1') > 0);
    assert.ok(compareSemver('1.0.1', '1.0.2') < 0);
});

test('compareSemver: complex ordering', () => {
    assert.ok(compareSemver('0.22.0', '0.21.0') > 0);
    assert.ok(compareSemver('1.0.0', '0.99.99') > 0);
});

// Test assignVersionWeights
test('assignVersionWeights: first cutoff (latest + legacy -> add version)', () => {
    const existing = {
        latest: { weight: 1 },
        legacy: { weight: 2 }
    };
    const result = assignVersionWeights(existing, '0.21.0');
    assert.deepEqual(result, {
        latest: { weight: 1 },
        '0.21.0': { weight: 2 },
        legacy: { weight: 3 }
    });
});

test('assignVersionWeights: second cutoff adds newer version before older', () => {
    const existing = {
        latest: { weight: 1 },
        '0.21.0': { weight: 2 },
        legacy: { weight: 3 }
    };
    const result = assignVersionWeights(existing, '0.22.0');
    assert.deepEqual(result, {
        latest: { weight: 1 },
        '0.22.0': { weight: 2 },
        '0.21.0': { weight: 3 },
        legacy: { weight: 4 }
    });
});

test('assignVersionWeights: adding older version sorts correctly', () => {
    const existing = {
        latest: { weight: 1 },
        '0.22.0': { weight: 2 },
        legacy: { weight: 3 }
    };
    const result = assignVersionWeights(existing, '0.20.0');
    assert.deepEqual(result, {
        latest: { weight: 1 },
        '0.22.0': { weight: 2 },
        '0.20.0': { weight: 3 },
        legacy: { weight: 4 }
    });
});

test('assignVersionWeights: duplicate version throws', () => {
    const existing = {
        latest: { weight: 1 },
        '0.21.0': { weight: 2 },
        legacy: { weight: 3 }
    };
    assert.throws(() => assignVersionWeights(existing, '0.21.0'), /already exists/);
});

test('assignVersionWeights: no legacy present', () => {
    const existing = {
        latest: { weight: 1 }
    };
    const result = assignVersionWeights(existing, '1.0.0');
    assert.deepEqual(result, {
        latest: { weight: 1 },
        '1.0.0': { weight: 2 }
    });
});

test('assignVersionWeights: no latest present', () => {
    const existing = {
        '0.21.0': { weight: 1 },
        legacy: { weight: 2 }
    };
    const result = assignVersionWeights(existing, '0.22.0');
    assert.deepEqual(result, {
        '0.22.0': { weight: 1 },
        '0.21.0': { weight: 2 },
        legacy: { weight: 3 }
    });
});

test('assignVersionWeights: multiple existing versions re-sorted correctly', () => {
    const existing = {
        latest: { weight: 1 },
        '0.20.0': { weight: 4 },
        '0.22.0': { weight: 2 },
        '0.21.0': { weight: 3 },
        legacy: { weight: 5 }
    };
    const result = assignVersionWeights(existing, '0.23.0');
    assert.deepEqual(result, {
        latest: { weight: 1 },
        '0.23.0': { weight: 2 },
        '0.22.0': { weight: 3 },
        '0.21.0': { weight: 4 },
        '0.20.0': { weight: 5 },
        legacy: { weight: 6 }
    });
});

test('assignVersionWeights: works with "main" as default key', () => {
    const existing = {
        main: { weight: 1 },
        legacy: { weight: 2 }
    };
    const result = assignVersionWeights(existing, '1.0.0');
    assert.deepEqual(result, {
        main: { weight: 1 },
        '1.0.0': { weight: 2 },
        legacy: { weight: 3 }
    });
});

test('assignVersionWeights: main and latest coexist (main first)', () => {
    const existing = {
        main: { weight: 1 },
        latest: { weight: 2 },
        legacy: { weight: 3 }
    };
    const result = assignVersionWeights(existing, '1.0.0');
    assert.deepEqual(result, {
        main: { weight: 1 },
        latest: { weight: 2 },
        '1.0.0': { weight: 3 },
        legacy: { weight: 4 }
    });
});

test('assignVersionWeights: main and latest coexist without legacy', () => {
    const existing = {
        main: { weight: 1 },
        latest: { weight: 2 }
    };
    const result = assignVersionWeights(existing, '2.0.0');
    assert.deepEqual(result, {
        main: { weight: 1 },
        latest: { weight: 2 },
        '2.0.0': { weight: 3 }
    });
});

test('assignVersionWeights: main and latest coexist with existing semvers', () => {
    const existing = {
        main: { weight: 1 },
        latest: { weight: 2 },
        '0.21.0': { weight: 3 },
        legacy: { weight: 4 }
    };
    const result = assignVersionWeights(existing, '0.22.0');
    assert.deepEqual(result, {
        main: { weight: 1 },
        latest: { weight: 2 },
        '0.22.0': { weight: 3 },
        '0.21.0': { weight: 4 },
        legacy: { weight: 5 }
    });
});

test('assignVersionWeights: empty existing versions', () => {
    const result = assignVersionWeights({}, '1.0.0');
    assert.deepEqual(result, {
        '1.0.0': { weight: 1 }
    });
});

test('assignVersionWeights: null existing versions', () => {
    const result = assignVersionWeights(null, '1.0.0');
    assert.deepEqual(result, {
        '1.0.0': { weight: 1 }
    });
});

// Test pinLatestContentMount
test('pinLatestContentMount: rewrites live content mount to snapshot', () => {
    const parsed = {
        mounts: [
            { source: 'content', target: 'content', sites: { matrix: { versions: ['latest'] } } },
        ],
    };
    const changed = pinLatestContentMount(parsed, '1.0.0');
    assert.equal(changed, true);
    assert.equal(parsed.mounts[0].source, 'content_versioned/version-1.0.0');
});

test('pinLatestContentMount: leaves main content mount alone', () => {
    const parsed = {
        mounts: [
            { source: 'content', target: 'content', sites: { matrix: { versions: ['main'] } } },
            { source: 'content', target: 'content', sites: { matrix: { versions: ['latest'] } } },
        ],
    };
    pinLatestContentMount(parsed, '2.0.0');
    assert.equal(parsed.mounts[0].source, 'content');
    assert.equal(parsed.mounts[1].source, 'content_versioned/version-2.0.0');
});

test('pinLatestContentMount: does not touch non-content targets on latest', () => {
    const parsed = {
        mounts: [
            { source: 'something/else', target: 'static/latest/schemas/foo',
              sites: { matrix: { versions: ['latest'] } } },
        ],
    };
    const changed = pinLatestContentMount(parsed, '1.0.0');
    assert.equal(changed, false);
    assert.equal(parsed.mounts[0].source, 'something/else');
});

test('pinLatestContentMount: updates existing snapshot mount to newer version', () => {
    const parsed = {
        mounts: [
            { source: 'content_versioned/version-1.0.0', target: 'content',
              sites: { matrix: { versions: ['latest'] } } },
        ],
    };
    const changed = pinLatestContentMount(parsed, '2.0.0');
    assert.equal(changed, true);
    assert.equal(parsed.mounts[0].source, 'content_versioned/version-2.0.0');
});

test('pinLatestContentMount: no-op when latest already pinned to that version', () => {
    const parsed = {
        mounts: [
            { source: 'content_versioned/version-1.0.0', target: 'content',
              sites: { matrix: { versions: ['latest'] } } },
        ],
    };
    const changed = pinLatestContentMount(parsed, '1.0.0');
    assert.equal(changed, false);
});

test('pinLatestContentMount: no-op when no latest mount present', () => {
    const parsed = {
        mounts: [
            { source: 'content', target: 'content', sites: { matrix: { versions: ['main'] } } },
        ],
    };
    const changed = pinLatestContentMount(parsed, '1.0.0');
    assert.equal(changed, false);
});

test('pinLatestContentMount: skips mounts that target multiple versions', () => {
    const parsed = {
        mounts: [
            { source: 'content', target: 'content',
              sites: { matrix: { versions: ['latest', 'main'] } } },
        ],
    };
    const changed = pinLatestContentMount(parsed, '1.0.0');
    assert.equal(changed, false);
    assert.equal(parsed.mounts[0].source, 'content');
});

test('pinLatestContentMount: handles null/empty mounts', () => {
    assert.equal(pinLatestContentMount(null, '1.0.0'), false);
    assert.equal(pinLatestContentMount({}, '1.0.0'), false);
    assert.equal(pinLatestContentMount({ mounts: [] }, '1.0.0'), false);
});
