#!/usr/bin/env node
/**
 * Cut off a new versioned content release (module-import-only model)
 *
 * Usage:
 *   node scripts/cutoff-version.js X.Y.Z
 *
 * Behavior:
 * - Accepts SemVer version X.Y.Z, derives minor identifier X.Y
 * - If Z == 0 (minor release): adds version to hugo.toml, adds import blocks to module.toml
 * - If Z > 0 (patch release): updates existing import tags for version X.Y (no hugo.toml changes)
 * - Retires oldest minor version when >10 minor versions exist
 */

const fsp = require('node:fs/promises');
const path = require('node:path');

// Paths
const REPO_ROOT = path.resolve(__dirname, '..');
const HUGO_TOML = path.join(REPO_ROOT, 'config', '_default', 'hugo.toml');
const MODULE_TOML = path.join(REPO_ROOT, 'config', '_default', 'module.toml');

// Headers for regenerated files
const HUGO_HEADER = `# Hugo Configuration
# This file is partially auto-generated. Comments may be lost on regeneration.
# Per-version settings are auto-generated at the end.

`;

const MODULE_HEADER = `# Hugo Module Configuration
# This file is partially auto-generated. Comments may be lost on regeneration.
#
# Static mounts (data, layouts, i18n, archetypes, assets, static) are fixed.
# Per-version imports are auto-generated at the end.

`;

// TOML module cache
let TOML;

// Load smol-toml
const loadToml = async () => TOML || (TOML = await import('smol-toml'));

// Maximum number of minor versions (excluding special versions like main/legacy)
const MAX_MINOR_VERSIONS = 10;

// Compare two SemVer strings (X.Y or X.Y.Z). Returns <0 if a<b, >0 if a>b, 0 if equal.
function compareSemver(a, b) {
    const pa = a.split('.').map(Number);
    const pb = b.split('.').map(Number);
    const len = Math.max(pa.length, pb.length);
    for (let i = 0; i < len; i++) {
        const av = pa[i] || 0;
        const bv = pb[i] || 0;
        if (av !== bv) return av - bv;
    }
    return 0;
}

// Special version keys that are not SemVer
const SPECIAL_VERSIONS = new Set(['main', 'legacy']);

/**
 * Rebuild the versions object with correct weights.
 *
 * Rules:
 * - "main" (if present) always gets weight 1
 * - SemVer versions (X.Y) are sorted descending (newest first)
 * - "legacy" (if present) always gets the highest weight (last)
 *
 * @param {Object} existingVersions - current versions from hugo.toml
 * @param {string} newVersion - minor version to add (X.Y)
 * @returns {Object} rebuilt versions object with weights
 */
function assignVersionWeights(existingVersions, newVersion) {
    const versions = existingVersions || {};

    const alreadyExists = !!versions[newVersion];

    let hasMain = false;
    let hasLegacy = false;
    const semverKeys = [];

    for (const key of Object.keys(versions)) {
        if (key === 'main') hasMain = true;
        else if (key === 'legacy') hasLegacy = true;
        else semverKeys.push(key);
    }

    if (!alreadyExists) semverKeys.push(newVersion);
    semverKeys.sort((a, b) => compareSemver(b, a)); // descending

    const result = {};
    let weight = 1;

    if (hasMain) result.main = { weight: weight++ };

    for (const sv of semverKeys) {
        result[sv] = { weight: weight++ };
    }

    if (hasLegacy) result.legacy = { weight: weight };

    return result;
}

// Log error and exit
function fail(msg) {
    console.error(`[ERROR] ${msg}`);
    throw new Error(msg);
}

// Parse CLI arguments
function parseArguments(args) {
    const flags = [];
    const positionals = [];

    for (const arg of args) {
        if (arg.startsWith('--')) flags.push(arg);
        else positionals.push(arg.trim());
    }

    if (flags.length) throw new Error(`Unknown flag(s): ${flags.join(', ')}`);
    if (positionals.length === 0) throw new Error('Missing version. Usage: cutoff-version.js X.Y.Z');
    if (positionals.length > 1) throw new Error(`Expected exactly one version argument, got ${positionals.length}: ${positionals.join(', ')}`);

    const fullVersion = positionals[0];
    const versionPattern = /^\d+\.\d+\.\d+$/;
    if (!versionPattern.test(fullVersion)) {
        throw new Error(`Invalid version '${fullVersion}'. Expected X.Y.Z, without "v" or suffixes, e.g. 1.2.3`);
    }

    // Derive X.Y from X.Y.Z and determine patch from Z > 0
    const parts = fullVersion.split('.');
    const version = `${parts[0]}.${parts[1]}`;
    const patch = Number(parts[2]) > 0;

    return { version, fullVersion, patch };
}

function hasAnyImportForVersion(parsed, version) {
    return parsed?.imports?.some(i => i?.mounts?.some(m => m?.sites?.matrix?.versions?.includes(version))) ?? false;
}

function hasAllImportsForVersion(parsed, version) {
    const { imports: expected } = buildModuleBlocks(version, `${version}.0`);
    const expectedPaths = expected.map(i => i.path);
    const existingPaths = new Set(
        (parsed?.imports || [])
            .filter(i => i?.mounts?.some(m => m?.sites?.matrix?.versions?.includes(version)))
            .map(i => i.path)
    );
    return expectedPaths.every(p => existingPaths.has(p));
}

// Build import blocks for a given version (pure, testable)
function buildModuleBlocks(version, fullVersion) {
    const imports = [
        // Hand-written docs
        {
            path: 'ocm.software/open-component-model/website',
            version: `website/v${fullVersion}`,
            mounts: [{
                files: ['**', '!blog/**'],
                source: 'content/',
                target: 'content',
                sites: { matrix: { versions: [version] } }
            }]
        },
        // CLI reference
        {
            path: 'ocm.software/open-component-model/cli',
            version: `cli/v${fullVersion}`,
            mounts: [{
                source: 'docs/reference',
                target: 'content/docs/reference/ocm-cli',
                sites: { matrix: { versions: [version] } }
            }]
        },
        // Go constructor schemas (independent release cycle, uses latest)
        {
            path: 'ocm.software/open-component-model/bindings/go/constructor',
            version: 'latest',
            mounts: [{
                source: 'spec/v1/resources',
                target: `static/${version}/schemas/bindings/go/constructor`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        // Go descriptor schemas (independent release cycle, uses latest)
        {
            path: 'ocm.software/open-component-model/bindings/go/descriptor/v2',
            version: 'latest',
            mounts: [{
                source: 'resources',
                target: `static/${version}/schemas/bindings/go/descriptor/v2`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        // Kubernetes controller CRDs
        {
            path: 'ocm.software/open-component-model/kubernetes/controller',
            version: `kubernetes/controller/v${fullVersion}`,
            mounts: [{
                source: 'config/crd/bases',
                target: `static/${version}/schemas/kubernetes/controller`,
                sites: { matrix: { versions: [version] } }
            }]
        },
    ];

    return { imports };
}

/**
 * Retire the oldest minor version when there are more than MAX_MINOR_VERSIONS.
 * Removes it from hugo.toml versions and returns the removed version key.
 *
 * @param {Object} versions - versions object from hugo.toml
 * @returns {string|null} removed version key, or null if no retirement needed
 */
function retireOldestVersion(versions) {
    const semverKeys = Object.keys(versions).filter(k => !SPECIAL_VERSIONS.has(k));
    if (semverKeys.length <= MAX_MINOR_VERSIONS) return null;

    semverKeys.sort((a, b) => compareSemver(a, b)); // ascending
    const oldest = semverKeys[0];
    delete versions[oldest];
    return oldest;
}

/**
 * Update import tags for an existing version (patch mode).
 * Updates versioned tags (website, cli, controller) to the new fullVersion.
 * Bindings imports (version: 'latest') are left unchanged.
 *
 * @param {Object} parsed - parsed module.toml
 * @param {string} version - minor version (X.Y)
 * @param {string} fullVersion - full version (X.Y.Z)
 * @returns {boolean} true if any tags were updated
 */
function updateImportTags(parsed, version, fullVersion) {
    if (!parsed?.imports) return false;
    let changed = false;

    for (const imp of parsed.imports) {
        const matchesVersion = imp?.mounts?.some(m => m?.sites?.matrix?.versions?.includes(version));
        if (!matchesVersion) continue;

        // Only update versioned imports (not 'latest')
        if (imp.version === 'latest') continue;

        let newTag = null;
        if (imp.path.endsWith('/website')) {
            newTag = `website/v${fullVersion}`;
        } else if (imp.path.endsWith('/cli')) {
            newTag = `cli/v${fullVersion}`;
        } else if (imp.path.endsWith('/kubernetes/controller')) {
            newTag = `kubernetes/controller/v${fullVersion}`;
        }

        if (newTag && imp.version !== newTag) {
            imp.version = newTag;
            changed = true;
        }
    }

    return changed;
}

// Remove all imports for a given version from module.toml parsed object
function removeImportsForVersion(parsed, version) {
    if (!parsed?.imports) return;
    parsed.imports = parsed.imports.filter(
        imp => !imp?.mounts?.some(m => m?.sites?.matrix?.versions?.includes(version))
    );
}

// Update hugo.toml: add version, set default, retire old
async function updateHugoToml(version) {
    const { parse, stringify } = await loadToml();
    const content = await fsp.readFile(HUGO_TOML, 'utf-8').catch(e => fail(`Read hugo.toml: ${e.message}`));
    const parsed = parse(content);

    const alreadyExists = !!(parsed.versions && parsed.versions[version]);

    parsed.versions = assignVersionWeights(parsed.versions || {}, version);

    if (alreadyExists) {
        console.log(`hugo.toml: version ${version} already exists, skipping.`);
    } else {
        const oldDefault = parsed.defaultContentVersion;
        parsed.defaultContentVersion = version;
        console.log(`hugo.toml: defaultContentVersion changed from '${oldDefault}' to '${version}'.`);
    }

    // Retire oldest if over limit
    const retired = retireOldestVersion(parsed.versions);
    if (retired) {
        console.log(`hugo.toml: retired oldest version '${retired}' (exceeded ${MAX_MINOR_VERSIONS} minor versions).`);
    }

    await fsp.writeFile(HUGO_TOML, HUGO_HEADER + stringify(parsed), 'utf-8');
    if (!alreadyExists) {
        console.log(`hugo.toml: added version ${version} (weights reassigned).`);
    }

    return retired;
}

// Update module.toml: add import blocks for a new version
async function updateModuleToml(version, fullVersion, retiredVersion) {
    const { parse, stringify } = await loadToml();
    const content = await fsp.readFile(MODULE_TOML, 'utf-8').catch(e => fail(`Read module.toml: ${e.message}`));
    const parsed = parse(content);

    const hasAllImports = hasAllImportsForVersion(parsed, version);
    const hasAnyImport = hasAnyImportForVersion(parsed, version);

    if (hasAllImports) {
        console.log(`module.toml: version ${version} exists, skipping.`);
    } else if (hasAnyImport) {
        fail(`module.toml: incomplete block for ${version}. Fix manually.`);
    } else {
        const { imports } = buildModuleBlocks(version, fullVersion);
        parsed.imports = parsed.imports || [];
        for (const imp of imports) {
            parsed.imports.push(imp);
        }
        console.log(`module.toml: added imports for version ${version}.`);
    }

    // Remove imports for retired version
    if (retiredVersion) {
        removeImportsForVersion(parsed, retiredVersion);
        console.log(`module.toml: removed imports for retired version '${retiredVersion}'.`);
    }

    await fsp.writeFile(MODULE_TOML, MODULE_HEADER + stringify(parsed), 'utf-8');
}

// Update module.toml in patch mode: update tags for existing version
async function updateModuleTomlPatch(version, fullVersion) {
    const { parse, stringify } = await loadToml();
    const content = await fsp.readFile(MODULE_TOML, 'utf-8').catch(e => fail(`Read module.toml: ${e.message}`));
    const parsed = parse(content);

    if (!hasAnyImportForVersion(parsed, version)) {
        fail(`module.toml: no imports found for version '${version}'. Cannot patch.`);
    }

    const changed = updateImportTags(parsed, version, fullVersion);
    if (changed) {
        await fsp.writeFile(MODULE_TOML, MODULE_HEADER + stringify(parsed), 'utf-8');
        console.log(`module.toml: updated import tags for version ${version} to ${fullVersion}.`);
    } else {
        console.log(`module.toml: import tags for version ${version} already up to date.`);
    }
}

// Main
async function main() {
    const { version, fullVersion, patch } = parseArguments(process.argv.slice(2));

    if (patch) {
        // Patch release (Z > 0): verify version exists in hugo.toml, update tags only
        const { parse } = await loadToml();
        const content = await fsp.readFile(HUGO_TOML, 'utf-8').catch(e => fail(`Read hugo.toml: ${e.message}`));
        const parsed = parse(content);
        if (!parsed.versions || !parsed.versions[version]) {
            fail(`Version '${version}' does not exist in hugo.toml. Cannot patch a version that hasn't been created yet.`);
        }
        await updateModuleTomlPatch(version, fullVersion);
    } else {
        const retired = await updateHugoToml(version);
        await updateModuleToml(version, fullVersion, retired);
    }

    console.log('Cutoff completed.');
}

if (require.main === module) {
    main().catch(e => {
        console.error(`[ERROR] ${e.message || String(e)}`);
        process.exit(1);
    });
}

module.exports = { parseArguments, hasAnyImportForVersion, hasAllImportsForVersion, buildModuleBlocks, compareSemver, assignVersionWeights, retireOldestVersion, updateImportTags };
