#!/usr/bin/env node
/**
 * Register a new versioned docs import (module-import-only model)
 *
 * Usage:
 *   node scripts/register-docs-version.js X.Y.Z --cli-gomod <path>
 *
 * Behavior:
 * - Accepts SemVer version X.Y.Z, derives minor identifier X.Y
 * - Ensures hugo.yaml has the version entry (idempotent, creates if missing)
 * - Ensures module.yaml has import blocks (creates if missing, updates tags if present)
 * - Retires oldest minor version when >10 minor versions exist
 */

const fsp = require('node:fs/promises');
const { execFileSync } = require('node:child_process');
const path = require('node:path');
const yaml = require('js-yaml');

// Paths
const REPO_ROOT = path.resolve(__dirname, '..');
const HUGO_CONFIG = path.join(REPO_ROOT, 'config', '_default', 'hugo.yaml');
const MODULE_CONFIG = path.join(REPO_ROOT, 'config', '_default', 'module.yaml');

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

// Serialize a parsed config back to YAML
function dumpYaml(parsed) {
    return yaml.dump(parsed, { lineWidth: -1, noRefs: true });
}

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
 * @param {Object} existingVersions - current versions from hugo.yaml
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
    const flags = {};
    const positionals = [];

    for (let i = 0; i < args.length; i++) {
        if (args[i] === '--cli-gomod') {
            if (i + 1 >= args.length) throw new Error('--cli-gomod requires a path argument');
            flags.cliGomod = args[++i];
        } else if (args[i].startsWith('--')) {
            throw new Error(`Unknown flag: ${args[i]}`);
        } else {
            positionals.push(args[i].trim());
        }
    }

    if (positionals.length === 0) throw new Error('Missing version. Usage: register-docs-version.js X.Y.Z --cli-gomod <path>');
    if (positionals.length > 1) throw new Error(`Expected exactly one version argument, got ${positionals.length}: ${positionals.join(', ')}`);

    const fullVersion = positionals[0];
    const versionPattern = /^\d+\.\d+\.\d+$/;
    if (!versionPattern.test(fullVersion)) {
        throw new Error(`Invalid version '${fullVersion}'. Expected X.Y.Z, without "v" or suffixes, e.g. 1.2.3`);
    }

    // Derive X.Y from X.Y.Z
    const parts = fullVersion.split('.');
    const version = `${parts[0]}.${parts[1]}`;

    return { version, fullVersion, cliGomod: flags.cliGomod };
}

// True if at least one import references this version in its site matrix.
function hasAnyImportForVersion(parsed, version) {
    return parsed?.imports?.some(i => i?.mounts?.some(m => m?.sites?.matrix?.versions?.includes(version))) ?? false;
}

// True if every module path returned by buildModuleBlocks(version, _, deps)
// has a matching import. `deps` must be the resolved CLI module versions so
// that bindings absent from this release are excluded from the expected set
// (otherwise we would always report "incomplete" for older releases that
// lack newer bindings). A mismatch (hasAny=true, hasAll=false) indicates a
// corrupted partial state.
function hasAllImportsForVersion(parsed, version, deps) {
    const { imports: expected } = buildModuleBlocks(version, `${version}.0`, deps);
    const existingByPath = new Map(
        (parsed?.imports || [])
            .filter(i => i?.mounts?.some(m => m?.sites?.matrix?.versions?.includes(version)))
            .map(i => [i.path, i])
    );
    return expected.every(exp => {
        const existing = existingByPath.get(exp.path);
        return existing && existing.mounts.length === exp.mounts.length;
    });
}

/**
 * Resolve dependency versions from a go.mod file.
 * Runs `go mod edit -json <goModPath>` and extracts the version for each
 * requested module path.
 *
 * Modules in `modulePaths` that don't appear in the file are treated as
 * "binding not part of this release" (e.g. introduced after the snapshot
 * was taken) and are skipped with a warning. Callers (buildModuleBlocks,
 * updateImportTags) treat a missing entry as "do not emit/touch the import
 * for this binding."
 *
 * @param {string} goModPath - absolute path to go.mod
 * @param {string[]} modulePaths - module paths to look up
 * @returns {Object<string, string>} map of modulePath -> version (e.g. "v0.0.7"),
 *          missing entries omitted
 */
function resolveGoModVersions(goModPath, modulePaths) {
    const absPath = path.resolve(goModPath);
    const output = execFileSync('go', ['mod', 'edit', '-json', absPath], { encoding: 'utf-8' });
    const mod = JSON.parse(output);

    const result = {};
    const requires = mod.Require || [];
    for (const req of requires) {
        if (modulePaths.includes(req.Path)) {
            result[req.Path] = req.Version;
        }
    }

    const missing = modulePaths.filter(p => !result[p]);
    if (missing.length) {
        console.warn(
            `[WARN] ${missing.length} binding(s) not in ${path.basename(goModPath)} - ` +
            `skipping their imports for this version (likely introduced after this release):\n  ` +
            missing.join('\n  ')
        );
    }

    return result;
}

// Shared prefix for every Go module path in this repo; kept as a single
// constant so the long FQN doesn't get repeated dozens of times below.
const MODULE_PREFIX = 'ocm.software/open-component-model';

// Modules whose versions are derived from the CLI's go.mod
const CLI_DERIVED_MODULES = [
    `${MODULE_PREFIX}/bindings/go/constructor`,
    `${MODULE_PREFIX}/bindings/go/credentials`,
    `${MODULE_PREFIX}/bindings/go/descriptor/v2`,
    `${MODULE_PREFIX}/bindings/go/gpg`,
    `${MODULE_PREFIX}/bindings/go/helm`,
    `${MODULE_PREFIX}/bindings/go/http`,
    `${MODULE_PREFIX}/bindings/go/oci`,
    `${MODULE_PREFIX}/bindings/go/rsa`,
    `${MODULE_PREFIX}/bindings/go/sigstore`,
];

// Build import blocks for a given version (pure when deps are passed, testable).
// Bindings whose version is not in `deps` (e.g. introduced after this release)
// are filtered out at the end - their `version` is `undefined` and we drop the
// import block entirely. The always-pinned entries (website/cli/controller) use
// `v${fullVersion}` so they're never dropped.
function buildModuleBlocks(version, fullVersion, deps) {
    const constructorVersion = deps?.[`${MODULE_PREFIX}/bindings/go/constructor`];
    const credentialsVersion = deps?.[`${MODULE_PREFIX}/bindings/go/credentials`];
    const descriptorVersion = deps?.[`${MODULE_PREFIX}/bindings/go/descriptor/v2`];
    const gpgVersion = deps?.[`${MODULE_PREFIX}/bindings/go/gpg`];
    const helmVersion = deps?.[`${MODULE_PREFIX}/bindings/go/helm`];
    const httpVersion = deps?.[`${MODULE_PREFIX}/bindings/go/http`];
    const ociVersion = deps?.[`${MODULE_PREFIX}/bindings/go/oci`];
    const rsaVersion = deps?.[`${MODULE_PREFIX}/bindings/go/rsa`];
    const sigstoreVersion = deps?.[`${MODULE_PREFIX}/bindings/go/sigstore`];

    // Hugo passes `version:` verbatim to `go mod download <path>@<version>`,
    // which expects a Go module version (e.g. v0.9.0), not the git tag form
    // (<subdir>/v0.9.0). Go's resolver maps tag->version internally based on
    // the module path's subdir, so callers always use the bare SemVer.
    const imports = [
        {
            path: `${MODULE_PREFIX}/website`,
            version: `v${fullVersion}`,
            // Self-import: treat the snapshot as a content-only tarball.
            // ignoreImports drops its transitive version pins (which override
            // ours via Hugo's first-wins resolution); ignoreConfig also drops
            // its mounts/params/hugo.yaml so the parent project is the sole
            // source of truth for theme, params, and module graph.
            ignoreImports: true,
            ignoreConfig: true,
            mounts: [{
                files: ['**', '!blog/**'],
                source: 'content/',
                target: 'content',
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/cli`,
            version: `v${fullVersion}`,
            mounts: [{
                source: 'docs/reference',
                target: 'content/docs/reference/ocm-cli',
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/bindings/go/constructor`,
            version: constructorVersion,
            mounts: [{
                source: 'spec/v1/resources',
                target: `static/${version}/schemas/bindings/go/constructor`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/bindings/go/descriptor/v2`,
            version: descriptorVersion,
            mounts: [{
                source: 'resources',
                target: `static/${version}/schemas/bindings/go/descriptor/v2`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/bindings/go/http`,
            version: httpVersion,
            mounts: [{
                source: 'spec/config/v1alpha1/schemas',
                target: `static/${version}/schemas/bindings/go/http`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/bindings/go/oci`,
            version: ociVersion,
            mounts: [{
                source: 'spec/credentials/v1/schemas',
                target: `static/${version}/schemas/bindings/go/credentials/oci/v1`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/bindings/go/helm`,
            version: helmVersion,
            mounts: [{
                source: 'spec/credentials/v1/schemas',
                target: `static/${version}/schemas/bindings/go/credentials/helm/v1`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/bindings/go/rsa`,
            version: rsaVersion,
            mounts: [{
                source: 'spec/credentials/v1/schemas',
                target: `static/${version}/schemas/bindings/go/credentials/rsa/v1`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/bindings/go/gpg`,
            version: gpgVersion,
            mounts: [{
                source: 'spec/credentials/v1alpha1/schemas',
                target: `static/${version}/schemas/bindings/go/credentials/gpg/v1alpha1`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/bindings/go/sigstore`,
            version: sigstoreVersion,
            mounts: [
                {
                    source: 'spec/credentials/oidcidentitytoken/v1alpha1/schemas',
                    target: `static/${version}/schemas/bindings/go/credentials/sigstore/oidcidentitytoken/v1alpha1`,
                    sites: { matrix: { versions: [version] } }
                },
                {
                    source: 'spec/credentials/trustedroot/v1alpha1/schemas',
                    target: `static/${version}/schemas/bindings/go/credentials/sigstore/trustedroot/v1alpha1`,
                    sites: { matrix: { versions: [version] } }
                }
            ]
        },
        {
            path: `${MODULE_PREFIX}/bindings/go/credentials`,
            version: credentialsVersion,
            mounts: [{
                source: 'spec/config/v1/schemas',
                target: `static/${version}/schemas/bindings/go/credentials/direct/v1`,
                sites: { matrix: { versions: [version] } }
            }]
        },
        {
            path: `${MODULE_PREFIX}/kubernetes/controller`,
            version: `v${fullVersion}`,
            mounts: [{
                source: 'config/crd/bases',
                target: `static/${version}/schemas/kubernetes/controller`,
                sites: { matrix: { versions: [version] } }
            }]
        },
    ];

    // Drop CLI-derived bindings whose version didn't resolve from cli-go.mod.
    // resolveGoModVersions emits a warning for those; here we just filter out
    // their import blocks so we don't emit `version: undefined`. Always-pinned
    // entries (website/cli/controller, version=`v${fullVersion}`) survive.
    return { imports: imports.filter(i => i.version !== undefined) };
}

/**
 * Retire the oldest minor version when there are more than MAX_MINOR_VERSIONS.
 * Removes it from hugo.yaml versions and returns the removed version key.
 *
 * @param {Object} versions - versions object from hugo.yaml
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
 * Updates binding import tags to the versions resolved from deps when supplied;
 * binding tags are left unchanged when deps is not provided.
 *
 * @param {Object} parsed - parsed module.yaml
 * @param {string} version - minor version (X.Y)
 * @param {string} fullVersion - full version (X.Y.Z)
 * @param {Object} [deps] - resolved binding versions keyed by module path
 * @returns {boolean} true if any tags were updated
 */
function updateImportTags(parsed, version, fullVersion, deps) {
    if (!parsed?.imports) return false;

    let changed = false;

    for (const imp of parsed.imports) {
        const matchesVersion = imp?.mounts?.some(m => m?.sites?.matrix?.versions?.includes(version));
        if (!matchesVersion) continue;

        let newTag = null;
        if (imp.path.endsWith('/website') ||
            imp.path.endsWith('/cli') ||
            imp.path.endsWith('/kubernetes/controller')) {
            newTag = `v${fullVersion}`;
        } else if (deps && imp.path.endsWith('/bindings/go/constructor')) {
            newTag = deps[`${MODULE_PREFIX}/bindings/go/constructor`];
        } else if (deps && imp.path.endsWith('/bindings/go/descriptor/v2')) {
            newTag = deps[`${MODULE_PREFIX}/bindings/go/descriptor/v2`];
        } else if (deps && imp.path.endsWith('/bindings/go/http')) {
            newTag = deps[`${MODULE_PREFIX}/bindings/go/http`];
        } else if (deps && imp.path.endsWith('/bindings/go/oci')) {
            newTag = deps[`${MODULE_PREFIX}/bindings/go/oci`];
        } else if (deps && imp.path.endsWith('/bindings/go/helm')) {
            newTag = deps[`${MODULE_PREFIX}/bindings/go/helm`];
        } else if (deps && imp.path.endsWith('/bindings/go/rsa')) {
            newTag = deps[`${MODULE_PREFIX}/bindings/go/rsa`];
        } else if (deps && imp.path.endsWith('/bindings/go/gpg')) {
            newTag = deps[`${MODULE_PREFIX}/bindings/go/gpg`];
        } else if (deps && imp.path.endsWith('/bindings/go/sigstore')) {
            newTag = deps[`${MODULE_PREFIX}/bindings/go/sigstore`];
        } else if (deps && imp.path.endsWith('/bindings/go/credentials')) {
            newTag = deps[`${MODULE_PREFIX}/bindings/go/credentials`];
        }

        if (newTag && imp.version !== newTag) {
            imp.version = newTag;
            changed = true;
        }
    }

    return changed;
}

// Remove all imports for a given version from module.yaml parsed object
function removeImportsForVersion(parsed, version) {
    if (!parsed?.imports) return;
    parsed.imports = parsed.imports.filter(
        imp => !imp?.mounts?.some(m => m?.sites?.matrix?.versions?.includes(version))
    );
}

// Update hugo.yaml: add version, set default, retire old.
async function updateHugoConfig(version) {
    const content = await fsp.readFile(HUGO_CONFIG, 'utf-8').catch(e => fail(`Read hugo.yaml: ${e.message}`));
    const parsed = yaml.load(content) || {};

    const alreadyExists = !!(parsed.versions && parsed.versions[version]);

    parsed.versions = assignVersionWeights(parsed.versions || {}, version);

    if (alreadyExists) {
        console.log(`hugo.yaml: version ${version} already exists, skipping.`);
    } else {
        const oldDefault = parsed.defaultContentVersion;
        if (!oldDefault || compareSemver(version, oldDefault) > 0) {
            parsed.defaultContentVersion = version;
            console.log(`hugo.yaml: defaultContentVersion changed from '${oldDefault}' to '${version}'.`);
        } else {
            console.log(`hugo.yaml: added version ${version} but keeping defaultContentVersion '${oldDefault}' (newer).`);
        }
    }

    // Retire oldest if over limit
    const retired = retireOldestVersion(parsed.versions);
    if (retired) {
        console.log(`hugo.yaml: retired oldest version '${retired}' (exceeded ${MAX_MINOR_VERSIONS} minor versions).`);
    }

    await fsp.writeFile(HUGO_CONFIG, HUGO_HEADER + dumpYaml(parsed), 'utf-8');
    if (!alreadyExists) {
        console.log(`hugo.yaml: added version ${version} (weights reassigned).`);
    }

    return retired;
}

// Update module.yaml: ensure imports exist for a version, update tags,
// optionally retire old version.
async function updateModuleConfig(version, fullVersion, cliGomod, { retiredVersion } = {}) {
    const content = await fsp.readFile(MODULE_CONFIG, 'utf-8').catch(e => fail(`Read module.yaml: ${e.message}`));
    const parsed = yaml.load(content) || {};

    const deps = resolveGoModVersions(cliGomod, CLI_DERIVED_MODULES);

    const hasAllImports = hasAllImportsForVersion(parsed, version, deps);
    const hasAnyImport = hasAnyImportForVersion(parsed, version);

    if (hasAllImports || hasAnyImport) {
        if (!hasAllImports) {
            // Partial block: a previous release did not cover every module in
            // CLI_DERIVED_MODULES. updateImportTags only bumps tags on imports
            // that already exist, so the gaps stay gaps - warn the reviewer to
            // fix them by hand before merging.
            console.warn(
                `[WARN] module.yaml: incomplete block for ${version}. ` +
                `Updating tags on existing entries only - fix the missing imports manually before merging.`
            );
        }
        const changed = updateImportTags(parsed, version, fullVersion, deps);
        if (changed) {
            console.log(`module.yaml: updated import tags for version ${version} to ${fullVersion}.`);
        } else {
            console.log(`module.yaml: version ${version} already up to date.`);
        }
    } else {
        const { imports } = buildModuleBlocks(version, fullVersion, deps);
        parsed.imports = parsed.imports || [];
        for (const imp of imports) {
            parsed.imports.push(imp);
        }
        console.log(`module.yaml: added imports for version ${version}.`);
    }

    if (retiredVersion) {
        removeImportsForVersion(parsed, retiredVersion);
        console.log(`module.yaml: removed imports for retired version '${retiredVersion}'.`);
    }

    await fsp.writeFile(MODULE_CONFIG, MODULE_HEADER + dumpYaml(parsed), 'utf-8');
}

// Main
async function main() {
    const { version, fullVersion, cliGomod } = parseArguments(process.argv.slice(2));

    if (!cliGomod) {
        fail('--cli-gomod <path> is required. Provide the path to the CLI go.mod for the release being versioned.');
    }

    const retired = await updateHugoConfig(version);
    await updateModuleConfig(version, fullVersion, cliGomod, { retiredVersion: retired });

    console.log('Docs version registered.');
}

if (require.main === module) {
    main().catch(e => {
        console.error(`[ERROR] ${e.message || String(e)}`);
        process.exit(1);
    });
}

module.exports = { parseArguments, hasAnyImportForVersion, hasAllImportsForVersion, buildModuleBlocks, compareSemver, assignVersionWeights, retireOldestVersion, updateImportTags, resolveGoModVersions, CLI_DERIVED_MODULES };
