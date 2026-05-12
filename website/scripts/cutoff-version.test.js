// Tests for cutoff-version.js

// Run: `npm test` or `node --test scripts/cutoff-version.test.js`

const test = require('node:test');
const assert = require('node:assert/strict');
const { parseArguments, hasAnyImportForVersion, hasAllImportsForVersion, buildModuleBlocks, compareSemver, assignVersionWeights, retireOldestVersion, updateImportTags } = require('./cutoff-version');

// --- parseArguments ---

test('parseArguments: valid version derives X.Y', () => {
    const result = parseArguments(['1.2.0']);
    assert.equal(result.version, '1.2');
    assert.equal(result.fullVersion, '1.2.0');
    assert.equal(result.patch, false);
});

test('parseArguments: patch is deduced when Z > 0', () => {
    const result = parseArguments(['1.2.3']);
    assert.equal(result.version, '1.2');
    assert.equal(result.fullVersion, '1.2.3');
    assert.equal(result.patch, true);

    const result2 = parseArguments(['0.3.0']);
    assert.equal(result2.patch, false);
});

test('parseArguments: missing version throws', () => {
    assert.throws(() => parseArguments([]), /Missing version/);
});

test('parseArguments: invalid version throws', () => {
    assert.throws(() => parseArguments(['v1.2.3']), /Invalid version/);
    assert.throws(() => parseArguments(['1.2']), /Invalid version/);
});

test('parseArguments: unknown flag throws', () => {
    assert.throws(() => parseArguments(['1.2.3', '--unknown']), /Unknown flag/);
});

test('parseArguments: --keepDefault is rejected as unknown', () => {
    assert.throws(() => parseArguments(['1.2.3', '--keepDefault']), /Unknown flag/);
});

test('parseArguments: --patch is rejected as unknown', () => {
    assert.throws(() => parseArguments(['1.2.3', '--patch']), /Unknown flag/);
});

// --- hasAnyImportForVersion ---

test('hasAnyImportForVersion: returns true/false correctly', () => {
    const parsed = { imports: [{ mounts: [{ sites: { matrix: { versions: ['0.3'] } } }] }] };
    assert.equal(hasAnyImportForVersion(parsed, '0.3'), true);
    assert.equal(hasAnyImportForVersion(parsed, '0.4'), false);
    assert.equal(hasAnyImportForVersion(null, '0.3'), false);
    assert.equal(hasAnyImportForVersion({}, '0.3'), false);
});

// --- hasAllImportsForVersion ---

test('hasAllImportsForVersion: returns true when all 5 imports exist', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    const parsed = { imports };
    assert.equal(hasAllImportsForVersion(parsed, '0.3'), true);
});

test('hasAllImportsForVersion: returns false when only a subset of imports exist', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    // Keep only the first import (1 of 5)
    const parsed = { imports: [imports[0]] };
    assert.equal(hasAllImportsForVersion(parsed, '0.3'), false);
});

test('hasAllImportsForVersion: returns false when no imports exist', () => {
    assert.equal(hasAllImportsForVersion({}, '0.3'), false);
    assert.equal(hasAllImportsForVersion(null, '0.3'), false);
});

test('hasAllImportsForVersion: returns false for wrong version', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    const parsed = { imports };
    assert.equal(hasAllImportsForVersion(parsed, '0.4'), false);
});

// --- buildModuleBlocks ---

test('buildModuleBlocks: returns 5 imports (website + CLI + 2 bindings + controller)', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    assert.equal(imports.length, 5);
});

test('buildModuleBlocks: does not return a mount field', () => {
    const result = buildModuleBlocks('0.3', '0.3.0');
    assert.equal(result.mount, undefined);
});

test('buildModuleBlocks: website import has correct tag format', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    const website = imports.find(i => i.path.endsWith('/website'));
    assert.ok(website, 'website import should exist');
    assert.equal(website.version, 'website/v0.3.0');
    assert.deepEqual(website.mounts[0].files, ['**', '!blog/**']);
    assert.equal(website.mounts[0].source, 'content/');
    assert.equal(website.mounts[0].target, 'content');
    assert.deepEqual(website.mounts[0].sites.matrix.versions, ['0.3']);
});

test('buildModuleBlocks: CLI import has correct tag format', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.5');
    const cli = imports.find(i => i.path.endsWith('/cli'));
    assert.ok(cli, 'CLI import should exist');
    assert.equal(cli.version, 'cli/v0.3.5');
    assert.equal(cli.mounts[0].target, 'content/docs/reference/ocm-cli');
    assert.deepEqual(cli.mounts[0].sites.matrix.versions, ['0.3']);
});

test('buildModuleBlocks: controller import has correct tag format', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.2');
    const controller = imports.find(i => i.path.endsWith('/kubernetes/controller'));
    assert.ok(controller, 'controller import should exist');
    assert.equal(controller.version, 'kubernetes/controller/v0.3.2');
    assert.deepEqual(controller.mounts[0].sites.matrix.versions, ['0.3']);
});

test('buildModuleBlocks: bindings use latest', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    const constructor = imports.find(i => i.path.endsWith('/bindings/go/constructor'));
    const descriptor = imports.find(i => i.path.endsWith('/bindings/go/descriptor/v2'));
    assert.equal(constructor.version, 'latest');
    assert.equal(descriptor.version, 'latest');
});

test('buildModuleBlocks: version matrix uses X.Y not X.Y.Z', () => {
    const { imports } = buildModuleBlocks('1.5', '1.5.3');
    for (const imp of imports) {
        assert.deepEqual(imp.mounts[0].sites.matrix.versions, ['1.5']);
    }
});

test('buildModuleBlocks: schema imports have correct targets with version prefix', () => {
    const { imports } = buildModuleBlocks('2.0', '2.0.0');
    const targets = imports.map(i => i.mounts[0].target).sort();
    assert.deepEqual(targets, [
        'content',
        'content/docs/reference/ocm-cli',
        'static/2.0/schemas/bindings/go/constructor',
        'static/2.0/schemas/bindings/go/descriptor/v2',
        'static/2.0/schemas/kubernetes/controller',
    ]);
});

test('buildModuleBlocks: schema imports have correct sources', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    const schemaImports = imports.filter(i => !i.path.endsWith('/cli') && !i.path.endsWith('/website'));
    const sources = schemaImports.map(i => i.mounts[0].source).sort();
    assert.deepEqual(sources, [
        'config/crd/bases',
        'resources',
        'spec/v1/resources',
    ]);
});

// --- compareSemver ---

test('compareSemver: equal versions return 0', () => {
    assert.equal(compareSemver('1.2', '1.2'), 0);
    assert.equal(compareSemver('1.2.3', '1.2.3'), 0);
    assert.equal(compareSemver('0.0', '0.0'), 0);
});

test('compareSemver: major version difference', () => {
    assert.ok(compareSemver('2.0', '1.0') > 0);
    assert.ok(compareSemver('1.0', '2.0') < 0);
});

test('compareSemver: minor version difference', () => {
    assert.ok(compareSemver('1.2', '1.1') > 0);
    assert.ok(compareSemver('1.1', '1.2') < 0);
});

test('compareSemver: patch version difference', () => {
    assert.ok(compareSemver('1.0.2', '1.0.1') > 0);
    assert.ok(compareSemver('1.0.1', '1.0.2') < 0);
});

test('compareSemver: complex ordering', () => {
    assert.ok(compareSemver('0.22', '0.21') > 0);
    assert.ok(compareSemver('1.0', '0.99') > 0);
});

test('compareSemver: mixed lengths (X.Y vs X.Y.Z) treated as missing=0', () => {
    assert.equal(compareSemver('1.2', '1.2.0'), 0);
    assert.ok(compareSemver('1.2.1', '1.2') > 0);
});

// --- assignVersionWeights ---

test('assignVersionWeights: first cutoff (main + legacy -> add version)', () => {
    const existing = {
        main: { weight: 1 },
        legacy: { weight: 2 }
    };
    const result = assignVersionWeights(existing, '0.21');
    assert.deepEqual(result, {
        main: { weight: 1 },
        '0.21': { weight: 2 },
        legacy: { weight: 3 }
    });
});

test('assignVersionWeights: second cutoff adds newer version before older', () => {
    const existing = {
        main: { weight: 1 },
        '0.21': { weight: 2 },
        legacy: { weight: 3 }
    };
    const result = assignVersionWeights(existing, '0.22');
    assert.deepEqual(result, {
        main: { weight: 1 },
        '0.22': { weight: 2 },
        '0.21': { weight: 3 },
        legacy: { weight: 4 }
    });
});

test('assignVersionWeights: adding older version sorts correctly', () => {
    const existing = {
        main: { weight: 1 },
        '0.22': { weight: 2 },
        legacy: { weight: 3 }
    };
    const result = assignVersionWeights(existing, '0.20');
    assert.deepEqual(result, {
        main: { weight: 1 },
        '0.22': { weight: 2 },
        '0.20': { weight: 3 },
        legacy: { weight: 4 }
    });
});

test('assignVersionWeights: duplicate version is idempotent', () => {
    const existing = {
        main: { weight: 1 },
        '0.21': { weight: 2 },
        legacy: { weight: 3 }
    };
    const result = assignVersionWeights(existing, '0.21');
    assert.deepEqual(result, {
        main: { weight: 1 },
        '0.21': { weight: 2 },
        legacy: { weight: 3 }
    });
});

test('assignVersionWeights: no legacy present', () => {
    const existing = {
        main: { weight: 1 }
    };
    const result = assignVersionWeights(existing, '1.0');
    assert.deepEqual(result, {
        main: { weight: 1 },
        '1.0': { weight: 2 }
    });
});

test('assignVersionWeights: no main present', () => {
    const existing = {
        '0.21': { weight: 1 },
        legacy: { weight: 2 }
    };
    const result = assignVersionWeights(existing, '0.22');
    assert.deepEqual(result, {
        '0.22': { weight: 1 },
        '0.21': { weight: 2 },
        legacy: { weight: 3 }
    });
});

test('assignVersionWeights: multiple existing versions re-sorted correctly', () => {
    const existing = {
        main: { weight: 1 },
        '0.20': { weight: 4 },
        '0.22': { weight: 2 },
        '0.21': { weight: 3 },
        legacy: { weight: 5 }
    };
    const result = assignVersionWeights(existing, '0.23');
    assert.deepEqual(result, {
        main: { weight: 1 },
        '0.23': { weight: 2 },
        '0.22': { weight: 3 },
        '0.21': { weight: 4 },
        '0.20': { weight: 5 },
        legacy: { weight: 6 }
    });
});

test('assignVersionWeights: empty existing versions', () => {
    const result = assignVersionWeights({}, '1.0');
    assert.deepEqual(result, {
        '1.0': { weight: 1 }
    });
});

test('assignVersionWeights: null existing versions', () => {
    const result = assignVersionWeights(null, '1.0');
    assert.deepEqual(result, {
        '1.0': { weight: 1 }
    });
});

test('assignVersionWeights: no "latest" handling (latest is not special)', () => {
    // 'latest' is no longer a special version, it would be treated as semver-like
    // but since it's not in SPECIAL_VERSIONS, passing it as a key should just be treated as a semver key
    const existing = {
        main: { weight: 1 },
        legacy: { weight: 2 }
    };
    const result = assignVersionWeights(existing, '0.3');
    // No 'latest' anywhere
    assert.equal(result.latest, undefined);
    assert.deepEqual(result, {
        main: { weight: 1 },
        '0.3': { weight: 2 },
        legacy: { weight: 3 }
    });
});

// --- retireOldestVersion ---

test('retireOldestVersion: no retirement when under limit', () => {
    const versions = {
        main: { weight: 1 },
        '0.1': { weight: 2 },
        '0.2': { weight: 3 },
        legacy: { weight: 4 }
    };
    const retired = retireOldestVersion(versions);
    assert.equal(retired, null);
    assert.ok(versions['0.1']); // still there
    assert.ok(versions['0.2']); // still there
});

test('retireOldestVersion: no retirement at exactly 10 versions', () => {
    const versions = { main: { weight: 1 } };
    for (let i = 1; i <= 10; i++) {
        versions[`0.${i}`] = { weight: i + 1 };
    }
    versions.legacy = { weight: 12 };
    const retired = retireOldestVersion(versions);
    assert.equal(retired, null);
});

test('retireOldestVersion: retires oldest when over 10 versions', () => {
    const versions = { main: { weight: 1 } };
    for (let i = 1; i <= 11; i++) {
        versions[`0.${i}`] = { weight: i + 1 };
    }
    versions.legacy = { weight: 13 };
    const retired = retireOldestVersion(versions);
    assert.equal(retired, '0.1');
    assert.equal(versions['0.1'], undefined); // removed
    assert.ok(versions['0.2']); // still there
    assert.ok(versions['0.11']); // still there
});

test('retireOldestVersion: does not retire main or legacy', () => {
    const versions = { main: { weight: 1 }, legacy: { weight: 13 } };
    for (let i = 1; i <= 11; i++) {
        versions[`0.${i}`] = { weight: i + 1 };
    }
    const retired = retireOldestVersion(versions);
    assert.equal(retired, '0.1');
    assert.ok(versions.main);
    assert.ok(versions.legacy);
});

test('retireOldestVersion: correctly identifies oldest by semver', () => {
    const versions = {};
    // Add versions out of order
    for (let i = 11; i >= 1; i--) {
        versions[`1.${i}`] = { weight: 12 - i };
    }
    const retired = retireOldestVersion(versions);
    assert.equal(retired, '1.1');
});

// --- updateImportTags ---

test('updateImportTags: updates versioned tags for matching version', () => {
    const parsed = {
        imports: [
            {
                path: 'ocm.software/open-component-model/website',
                version: 'website/v0.3.0',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/cli',
                version: 'cli/v0.3.0',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/constructor',
                version: 'latest',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/descriptor/v2',
                version: 'latest',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/kubernetes/controller',
                version: 'kubernetes/controller/v0.3.0',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
        ]
    };

    const changed = updateImportTags(parsed, '0.3', '0.3.1');
    assert.equal(changed, true);
    assert.equal(parsed.imports[0].version, 'website/v0.3.1');
    assert.equal(parsed.imports[1].version, 'cli/v0.3.1');
    assert.equal(parsed.imports[2].version, 'latest'); // unchanged
    assert.equal(parsed.imports[3].version, 'latest'); // unchanged
    assert.equal(parsed.imports[4].version, 'kubernetes/controller/v0.3.1');
});

test('updateImportTags: does not change imports for other versions', () => {
    const parsed = {
        imports: [
            {
                path: 'ocm.software/open-component-model/cli',
                version: 'cli/v0.2.0',
                mounts: [{ sites: { matrix: { versions: ['0.2'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/cli',
                version: 'cli/v0.3.0',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
        ]
    };

    updateImportTags(parsed, '0.3', '0.3.1');
    assert.equal(parsed.imports[0].version, 'cli/v0.2.0'); // unchanged
    assert.equal(parsed.imports[1].version, 'cli/v0.3.1'); // updated
});

test('updateImportTags: returns false when already up to date', () => {
    const parsed = {
        imports: [
            {
                path: 'ocm.software/open-component-model/cli',
                version: 'cli/v0.3.1',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
        ]
    };
    const changed = updateImportTags(parsed, '0.3', '0.3.1');
    assert.equal(changed, false);
});

test('updateImportTags: returns false on null/empty parsed', () => {
    assert.equal(updateImportTags(null, '0.3', '0.3.1'), false);
    assert.equal(updateImportTags({}, '0.3', '0.3.1'), false);
});
