// Tests for register-docs-version.js

// Run: `npm test` or `node --test scripts/register-docs-version.test.js`

const test = require('node:test');
const assert = require('node:assert/strict');
const { parseArguments, hasAnyImportForVersion, hasAllImportsForVersion, buildModuleBlocks, compareSemver, assignVersionWeights, retireOldestVersion, updateImportTags } = require('./register-docs-version');

// --- parseArguments ---

test('parseArguments: valid version derives X.Y', () => {
    const result = parseArguments(['1.2.0']);
    assert.equal(result.version, '1.2');
    assert.equal(result.fullVersion, '1.2.0');
    assert.equal(result.cliGomod, undefined);
});

test('parseArguments: Z > 0 still derives X.Y correctly', () => {
    const result = parseArguments(['1.2.3']);
    assert.equal(result.version, '1.2');
    assert.equal(result.fullVersion, '1.2.3');
});

test('parseArguments: --cli-gomod is parsed', () => {
    const result = parseArguments(['1.2.0', '--cli-gomod', '/tmp/go.mod']);
    assert.equal(result.cliGomod, '/tmp/go.mod');
});

test('parseArguments: --cli-gomod without value throws', () => {
    assert.throws(() => parseArguments(['1.2.0', '--cli-gomod']), /requires a path/);
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

test('hasAllImportsForVersion: returns false when sigstore has only one of its two mounts', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    // Truncate sigstore to a single mount (simulates a partially-written module.yaml)
    const truncated = imports.map(i =>
        i.path.endsWith('/bindings/go/sigstore')
            ? { ...i, mounts: [i.mounts[0]] }
            : i
    );
    assert.equal(hasAllImportsForVersion({ imports: truncated }, '0.3'), false);
});

// --- buildModuleBlocks ---

test('buildModuleBlocks: returns 12 imports (website + CLI + 9 bindings + controller)', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    assert.equal(imports.length, 12);
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

test('buildModuleBlocks: bindings use fallback tag when no deps provided', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    const constructor = imports.find(i => i.path.endsWith('/bindings/go/constructor'));
    const credentials = imports.find(i => i.path.endsWith('/bindings/go/credentials'));
    const descriptor = imports.find(i => i.path.endsWith('/bindings/go/descriptor/v2'));
    const gpg = imports.find(i => i.path.endsWith('/bindings/go/gpg'));
    const helm = imports.find(i => i.path.endsWith('/bindings/go/helm'));
    const http = imports.find(i => i.path.endsWith('/bindings/go/http'));
    const oci = imports.find(i => i.path.endsWith('/bindings/go/oci'));
    const rsa = imports.find(i => i.path.endsWith('/bindings/go/rsa'));
    const sigstore = imports.find(i => i.path.endsWith('/bindings/go/sigstore'));
    assert.equal(constructor.version, 'bindings/go/constructor/latest');
    assert.equal(credentials.version, 'bindings/go/credentials/latest');
    assert.equal(descriptor.version, 'bindings/go/descriptor/v2/latest');
    assert.equal(gpg.version, 'bindings/go/gpg/latest');
    assert.equal(helm.version, 'bindings/go/helm/latest');
    assert.equal(http.version, 'bindings/go/http/latest');
    assert.equal(oci.version, 'bindings/go/oci/latest');
    assert.equal(rsa.version, 'bindings/go/rsa/latest');
    assert.equal(sigstore.version, 'bindings/go/sigstore/latest');
});

test('buildModuleBlocks: bindings use resolved versions when deps provided', () => {
    const deps = {
        'ocm.software/open-component-model/bindings/go/constructor': 'v0.0.7',
        'ocm.software/open-component-model/bindings/go/credentials': 'v0.0.13',
        'ocm.software/open-component-model/bindings/go/descriptor/v2': 'v2.0.3-alpha3',
        'ocm.software/open-component-model/bindings/go/gpg': 'v0.0.1',
        'ocm.software/open-component-model/bindings/go/helm': 'v0.0.1',
        'ocm.software/open-component-model/bindings/go/http': 'v0.0.5',
        'ocm.software/open-component-model/bindings/go/oci': 'v0.0.46',
        'ocm.software/open-component-model/bindings/go/rsa': 'v0.0.1',
        'ocm.software/open-component-model/bindings/go/sigstore': 'v0.0.1',
    };
    const { imports } = buildModuleBlocks('0.3', '0.3.0', deps);
    const constructor = imports.find(i => i.path.endsWith('/bindings/go/constructor'));
    const credentials = imports.find(i => i.path.endsWith('/bindings/go/credentials'));
    const descriptor = imports.find(i => i.path.endsWith('/bindings/go/descriptor/v2'));
    const gpg = imports.find(i => i.path.endsWith('/bindings/go/gpg'));
    const helm = imports.find(i => i.path.endsWith('/bindings/go/helm'));
    const http = imports.find(i => i.path.endsWith('/bindings/go/http'));
    const oci = imports.find(i => i.path.endsWith('/bindings/go/oci'));
    const rsa = imports.find(i => i.path.endsWith('/bindings/go/rsa'));
    const sigstore = imports.find(i => i.path.endsWith('/bindings/go/sigstore'));
    assert.equal(constructor.version, 'bindings/go/constructor/v0.0.7');
    assert.equal(credentials.version, 'bindings/go/credentials/v0.0.13');
    assert.equal(descriptor.version, 'bindings/go/descriptor/v2/v2.0.3-alpha3');
    assert.equal(gpg.version, 'bindings/go/gpg/v0.0.1');
    assert.equal(helm.version, 'bindings/go/helm/v0.0.1');
    assert.equal(http.version, 'bindings/go/http/v0.0.5');
    assert.equal(oci.version, 'bindings/go/oci/v0.0.46');
    assert.equal(rsa.version, 'bindings/go/rsa/v0.0.1');
    assert.equal(sigstore.version, 'bindings/go/sigstore/v0.0.1');
});

test('buildModuleBlocks: version matrix uses X.Y not X.Y.Z', () => {
    const { imports } = buildModuleBlocks('1.5', '1.5.3');
    for (const imp of imports) {
        assert.deepEqual(imp.mounts[0].sites.matrix.versions, ['1.5']);
    }
});

test('buildModuleBlocks: schema imports have correct targets with version prefix', () => {
    const { imports } = buildModuleBlocks('2.0', '2.0.0');
    const targets = imports.flatMap(i => i.mounts.map(m => m.target)).sort();
    assert.deepEqual(targets, [
        'content',
        'content/docs/reference/ocm-cli',
        'static/2.0/schemas/bindings/go/constructor',
        'static/2.0/schemas/bindings/go/credentials/direct/v1',
        'static/2.0/schemas/bindings/go/credentials/gpg/v1alpha1',
        'static/2.0/schemas/bindings/go/credentials/helm/v1',
        'static/2.0/schemas/bindings/go/credentials/oci/v1',
        'static/2.0/schemas/bindings/go/credentials/rsa/v1',
        'static/2.0/schemas/bindings/go/credentials/sigstore/oidcidentitytoken/v1alpha1',
        'static/2.0/schemas/bindings/go/credentials/sigstore/trustedroot/v1alpha1',
        'static/2.0/schemas/bindings/go/descriptor/v2',
        'static/2.0/schemas/bindings/go/http',
        'static/2.0/schemas/kubernetes/controller',
    ]);
});

test('buildModuleBlocks: schema imports have correct sources', () => {
    const { imports } = buildModuleBlocks('0.3', '0.3.0');
    const schemaImports = imports.filter(i => !i.path.endsWith('/cli') && !i.path.endsWith('/website'));
    const sources = schemaImports.flatMap(i => i.mounts.map(m => m.source)).sort();
    assert.deepEqual(sources, [
        'config/crd/bases',
        'resources',
        'spec/config/v1/schemas',
        'spec/config/v1alpha1/schemas',
        'spec/credentials/oidcidentitytoken/v1alpha1/schemas',
        'spec/credentials/trustedroot/v1alpha1/schemas',
        'spec/credentials/v1/schemas',
        'spec/credentials/v1/schemas',
        'spec/credentials/v1/schemas',
        'spec/credentials/v1alpha1/schemas',
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

test('assignVersionWeights: first registration (main + legacy -> add version)', () => {
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

test('assignVersionWeights: second registration adds newer version before older', () => {
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
    const deps = {
        'ocm.software/open-component-model/bindings/go/constructor': 'v0.0.8',
        'ocm.software/open-component-model/bindings/go/credentials': 'v0.0.14',
        'ocm.software/open-component-model/bindings/go/descriptor/v2': 'v2.0.4',
        'ocm.software/open-component-model/bindings/go/gpg': 'v0.0.2',
        'ocm.software/open-component-model/bindings/go/helm': 'v0.0.2',
        'ocm.software/open-component-model/bindings/go/http': 'v0.0.5',
        'ocm.software/open-component-model/bindings/go/oci': 'v0.0.47',
        'ocm.software/open-component-model/bindings/go/rsa': 'v0.0.2',
        'ocm.software/open-component-model/bindings/go/sigstore': 'v0.0.2',
    };
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
                version: 'bindings/go/constructor/v0.0.7',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/credentials',
                version: 'bindings/go/credentials/v0.0.13',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/descriptor/v2',
                version: 'bindings/go/descriptor/v2/v2.0.3',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/gpg',
                version: 'bindings/go/gpg/v0.0.1',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/helm',
                version: 'bindings/go/helm/v0.0.1',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/http',
                version: 'bindings/go/http/v0.0.4',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/oci',
                version: 'bindings/go/oci/v0.0.46',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/rsa',
                version: 'bindings/go/rsa/v0.0.1',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/bindings/go/sigstore',
                version: 'bindings/go/sigstore/v0.0.1',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
            {
                path: 'ocm.software/open-component-model/kubernetes/controller',
                version: 'kubernetes/controller/v0.3.0',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
        ]
    };

    const changed = updateImportTags(parsed, '0.3', '0.3.1', deps);
    assert.equal(changed, true);
    const byPath = Object.fromEntries(parsed.imports.map(i => [i.path, i.version]));
    assert.equal(byPath['ocm.software/open-component-model/website'], 'website/v0.3.1');
    assert.equal(byPath['ocm.software/open-component-model/cli'], 'cli/v0.3.1');
    assert.equal(byPath['ocm.software/open-component-model/bindings/go/constructor'], 'bindings/go/constructor/v0.0.8');
    assert.equal(byPath['ocm.software/open-component-model/bindings/go/credentials'], 'bindings/go/credentials/v0.0.14');
    assert.equal(byPath['ocm.software/open-component-model/bindings/go/descriptor/v2'], 'bindings/go/descriptor/v2/v2.0.4');
    assert.equal(byPath['ocm.software/open-component-model/bindings/go/gpg'], 'bindings/go/gpg/v0.0.2');
    assert.equal(byPath['ocm.software/open-component-model/bindings/go/helm'], 'bindings/go/helm/v0.0.2');
    assert.equal(byPath['ocm.software/open-component-model/bindings/go/http'], 'bindings/go/http/v0.0.5');
    assert.equal(byPath['ocm.software/open-component-model/bindings/go/oci'], 'bindings/go/oci/v0.0.47');
    assert.equal(byPath['ocm.software/open-component-model/bindings/go/rsa'], 'bindings/go/rsa/v0.0.2');
    assert.equal(byPath['ocm.software/open-component-model/bindings/go/sigstore'], 'bindings/go/sigstore/v0.0.2');
    assert.equal(byPath['ocm.software/open-component-model/kubernetes/controller'], 'kubernetes/controller/v0.3.1');
});

test('updateImportTags: does not update bindings when no deps provided', () => {
    const parsed = {
        imports: [
            {
                path: 'ocm.software/open-component-model/bindings/go/constructor',
                version: 'bindings/go/constructor/v0.0.7',
                mounts: [{ sites: { matrix: { versions: ['0.3'] } } }]
            },
        ]
    };

    const changed = updateImportTags(parsed, '0.3', '0.3.1');
    assert.equal(changed, false);
    assert.equal(parsed.imports[0].version, 'bindings/go/constructor/v0.0.7');
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

// --- patch recovery: missing imports yields same result as fresh creation ---

test('updateImportTags: patching freshly-built blocks equals building directly with patch version', () => {
    const deps = {
        'ocm.software/open-component-model/bindings/go/constructor': 'v0.0.8',
        'ocm.software/open-component-model/bindings/go/credentials': 'v0.0.14',
        'ocm.software/open-component-model/bindings/go/descriptor/v2': 'v2.0.4',
        'ocm.software/open-component-model/bindings/go/gpg': 'v0.0.2',
        'ocm.software/open-component-model/bindings/go/helm': 'v0.0.2',
        'ocm.software/open-component-model/bindings/go/http': 'v0.0.5',
        'ocm.software/open-component-model/bindings/go/oci': 'v0.0.47',
        'ocm.software/open-component-model/bindings/go/rsa': 'v0.0.2',
        'ocm.software/open-component-model/bindings/go/sigstore': 'v0.0.2',
    };
    const depsInitial = {
        'ocm.software/open-component-model/bindings/go/constructor': 'v0.0.7',
        'ocm.software/open-component-model/bindings/go/credentials': 'v0.0.13',
        'ocm.software/open-component-model/bindings/go/descriptor/v2': 'v2.0.3',
        'ocm.software/open-component-model/bindings/go/gpg': 'v0.0.1',
        'ocm.software/open-component-model/bindings/go/helm': 'v0.0.1',
        'ocm.software/open-component-model/bindings/go/http': 'v0.0.4',
        'ocm.software/open-component-model/bindings/go/oci': 'v0.0.46',
        'ocm.software/open-component-model/bindings/go/rsa': 'v0.0.1',
        'ocm.software/open-component-model/bindings/go/sigstore': 'v0.0.1',
    };

    // Path A: build at 0.3.0 with old deps, then patch to 0.3.1 with new deps
    const { imports: recoveryImports } = buildModuleBlocks('0.3', '0.3.0', depsInitial);
    const parsed = { imports: recoveryImports };
    updateImportTags(parsed, '0.3', '0.3.1', deps);

    // Path B: build directly at 0.3.1 with new deps
    const { imports: directImports } = buildModuleBlocks('0.3', '0.3.1', deps);

    assert.deepEqual(parsed.imports, directImports);
});