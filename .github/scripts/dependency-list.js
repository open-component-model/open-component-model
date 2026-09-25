// @ts-check

// Generates documentation from the dependency list encoded in the golangci.yml
// depguard rules: the binding dependency layer table (docs/dependency-table.md)
// and the bindings/go README packages table.

import { readFileSync, realpathSync, writeFileSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import * as yaml from "js-yaml";

const BINDING_PREFIX = "ocm.software/open-component-model/bindings/go/";
export const GENERATED_TABLE_START = "<!-- GENERATED PACKAGES TABLE:START (run 'task tools:dependency-list/generate' to update) -->";
export const GENERATED_TABLE_END = "<!-- GENERATED PACKAGES TABLE:END -->";
// Matches the marker lines regardless of their annotation text.
const GENERATED_TABLE_BLOCK = /^<!-- GENERATED PACKAGES TABLE:START.*-->$[\s\S]*^<!-- GENERATED PACKAGES TABLE:END.*-->$/m;

/**
 * Parse depguard rules from golangci.yml and return a dependency map.
 * Each key is a short binding name, value is a sorted array of its allowed sibling bindings.
 *
 * @param {string} configPath
 * @returns {Map<string, string[]>}
 */
export function parseDepguardRules(configPath) {
    const content = readFileSync(configPath, "utf-8");
    const config = yaml.load(content);
    const rules = config?.linters?.settings?.depguard?.rules;
    if (!rules) {
        throw new Error("No depguard rules found in config");
    }

    /** @type {Map<string, string[]>} */
    const deps = new Map();

    for (const [, rule] of Object.entries(rules)) {
        const files = rule.files?.[0] ?? "";
        const match = files.match(/\*\*\/bindings\/go\/(.+?)\/\*\*/);
        if (!match) continue;

        const binding = match[1];
        const allows = (rule.allow ?? [])
            .filter((/** @type {string} */ a) => a.startsWith(BINDING_PREFIX))
            .map((/** @type {string} */ a) => a.slice(BINDING_PREFIX.length))
            .filter((/** @type {string} */ a) => a !== binding);

        deps.set(binding, allows.sort());
    }

    return deps;
}

/**
 * Extract the first sentence of a package doc comment from a doc.go file.
 * Leading "Package <name>" or "Command <name>" boilerplate is removed and the
 * result is capitalized, so it can serve as a short package purpose.
 *
 * @param {string} docGoPath
 * @returns {string}
 */
export function extractPackageSummary(docGoPath) {
    const lines = readFileSync(docGoPath, "utf-8").split("\n");
    const pkgIndex = lines.findIndex((line) => /^package\s+\w+/.test(line));
    if (pkgIndex === -1) {
        throw new Error(`no package clause found in ${docGoPath}`);
    }
    // The doc comment is the contiguous block of "//" lines directly above the package clause.
    const comment = [];
    for (let i = pkgIndex - 1; i >= 0 && lines[i].startsWith("//"); i--) {
        comment.unshift(lines[i]);
    }
    if (comment.length === 0) {
        throw new Error(`no package doc comment found in ${docGoPath}`);
    }
    // Only the first paragraph of the doc comment is considered.
    const paragraph = [];
    for (const line of comment) {
        const text = line.replace(/^\/\/ ?/, "");
        if (text === "") break;
        paragraph.push(text);
    }
    // The first sentence ends at the first period followed by whitespace or the end of the paragraph.
    const sentence = /^.*?\.(?=\s|$)/s.exec(paragraph.join(" "))?.[0] ?? paragraph.join(" ");
    const summary = sentence.replace(/\.$/, "").replace(/^(?:Package|Command)\s+\S+\s+/, "");
    return summary.charAt(0).toUpperCase() + summary.slice(1);
}

/**
 * Generate the packages table for the bindings/go README.
 * The package list is derived from depguard rules, the purpose of each package
 * is the first sentence of its doc.go package comment.
 *
 * @param {Map<string, string[]>} deps
 * @param {string} bindingsDir path to the bindings/go directory
 * @returns {string}
 */
export function generatePackagesTable(deps, bindingsDir) {
    const names = [...deps.keys()].sort();
    /** @type {string[]} */
    const missing = [];
    const rows = [];
    for (const name of names) {
        try {
            rows.push(`| **${name}** | ${extractPackageSummary(join(bindingsDir, name, "doc.go"))} |`);
        } catch (error) {
            if (error instanceof Error && "code" in error && error.code === "ENOENT") {
                missing.push(name);
            } else {
                throw error;
            }
        }
    }
    if (missing.length > 0) {
        throw new Error(
            `missing doc.go for depguard bindings: ${missing.join(", ")}. ` +
            "Each public binding needs a doc.go with a package comment; its first sentence is used in the README packages table."
        );
    }
    const lines = [
        GENERATED_TABLE_START,
        "",
        "| Package | Purpose |",
        "|---------|---------|",
        ...rows,
        "",
        GENERATED_TABLE_END,
    ];
    return lines.join("\n");
}

/**
 * Regenerate the README at readmePath by replacing the generated packages table block.
 * The block between GENERATED_TABLE_START and GENERATED_TABLE_END is replaced,
 * all other content is preserved.
 *
 * @param {string} configPath path to the golangci-lint config with depguard rules
 * @param {string} readmePath path to bindings/go/README.md
 * @returns {string} the updated README content
 */
export function generatePackagesReadme(configPath, readmePath) {
    const deps = parseDepguardRules(configPath);
    const table = generatePackagesTable(deps, dirname(resolve(readmePath)));
    const content = readFileSync(readmePath, "utf-8");
    if (!GENERATED_TABLE_BLOCK.test(content)) {
        throw new Error(
            `${readmePath} does not contain the generated packages table markers. ` +
            `Add "${GENERATED_TABLE_START}" and "${GENERATED_TABLE_END}" around the packages table.`
        );
    }
    return content.replace(GENERATED_TABLE_BLOCK, table);
}

/**
 * Compute the topological layer (depth) of each binding.
 * Layer 0 = no internal dependencies.
 *
 * @param {Map<string, string[]>} deps
 * @returns {Map<string, number>}
 */
export function computeLayers(deps) {
    /** @type {Map<string, number>} */
    const layers = new Map();

    /** @param {string} mod */
    function getLayer(mod, visiting = new Set()) {
        if (layers.has(mod)) return /** @type {number} */ (layers.get(mod));
        if (visiting.has(mod)) {
            throw new Error(`Circular dependency detected: ${[...visiting, mod].join(" → ")}`);
        }
        visiting.add(mod);

        const modDeps = deps.get(mod) ?? [];
        if (modDeps.length === 0) {
            layers.set(mod, 0);
            return 0;
        }
        const maxDep = Math.max(...modDeps.map((d) => getLayer(d, new Set(visiting))));
        const layer = maxDep + 1;
        layers.set(mod, layer);
        return layer;
    }

    for (const mod of deps.keys()) {
        getLayer(mod);
    }
    return layers;
}

/**
 * Generate a markdown layer table from the dependency map.
 * Groups modules with identical dependency sets on the same row.
 *
 * @param {Map<string, string[]>} deps
 * @param {Map<string, number>} layers
 * @returns {string}
 */
export function generateTable(deps, layers) {
    const maxLayer = Math.max(...layers.values());

    const lines = [
        "# Binding Dependency Layers",
        "",
        "| Layer | Module | Direct dependencies |",
        "|-------|--------|---------------------|",
    ];

    for (let layer = 0; layer <= maxLayer; layer++) {
        const modsInLayer = [...layers.entries()]
            .filter(([, l]) => l === layer)
            .map(([mod]) => mod)
            .sort();

        if (modsInLayer.length === 0) continue;

        // Group modules that share the same dependency set
        /** @type {Map<string, string[]>} */
        const groups = new Map();
        for (const mod of modsInLayer) {
            const key = (deps.get(mod) ?? []).join(", ");
            if (!groups.has(key)) groups.set(key, []);
            groups.get(key)?.push(mod);
        }

        for (const [depsStr, mods] of groups) {
            const modCell = mods.map((m) => `\`${m}\``).join(", ");
            const depsCell = depsStr ? depsStr.split(", ").map((d) => `\`${d}\``).join(", ") : "—";
            lines.push(`| ${layer} | ${modCell} | ${depsCell} |`);
        }
    }

    lines.push("");
    return lines.join("\n");
}

/**
 * Render every generated document.
 *
 * @param {string} configPath path to golangci.yml
 * @param {string} tablePath path to docs/dependency-table.md
 * @param {string} readmePath path to bindings/go/README.md
 * @returns {{path: string, content: string}[]}
 */
export function renderDocuments(configPath, tablePath, readmePath) {
    const deps = parseDepguardRules(configPath);
    const layers = computeLayers(deps);
    return [
        { path: tablePath, content: generateTable(deps, layers) },
        { path: readmePath, content: generatePackagesReadme(configPath, readmePath) },
    ];
}

/**
 * Report the first line that differs between the expected and the actual content.
 *
 * @param {string} expected
 * @param {string} actual
 * @returns {string} a human readable hint about the first difference
 */
function firstDifference(expected, actual) {
    const expectedLines = expected.split("\n");
    const actualLines = actual.split("\n");
    for (let i = 0; i < Math.max(expectedLines.length, actualLines.length); i++) {
        if (expectedLines[i] !== actualLines[i]) {
            return `line ${i + 1}:\n  expected: ${expectedLines[i] ?? "<end of file>"}\n  actual:   ${actualLines[i] ?? "<end of file>"}`;
        }
    }
    return "no line difference";
}

function main() {
    const [command, configPath, tablePath, readmePath] = process.argv.slice(2);

    if ((command !== "generate" && command !== "verify") || !configPath || !tablePath || !readmePath) {
        process.stderr.write("Usage: dependency-list.js <generate|verify> <golangci.yml> <dependency-table.md> <README.md>\n");
        process.stderr.write("  generate  Write the generated documents\n");
        process.stderr.write("  verify    Fail if a generated document is out of sync\n");
        process.exit(1);
    }

    const documents = renderDocuments(configPath, tablePath, readmePath);

    if (command === "generate") {
        for (const { path, content } of documents) {
            writeFileSync(path, content);
        }
    } else {
        const stale = documents.filter(({ path, content }) => readFileSync(path, "utf-8") !== content);
        for (const { path, content } of stale) {
            process.stderr.write(`${path} is out of sync: ${firstDifference(content, readFileSync(path, "utf-8"))}\n`);
        }
        if (stale.length > 0) {
            process.exit(1);
        }
    }
}

// Only run the CLI when executed directly, not when imported by tests.
// realpathSync to cover cases with symlinks
if (process.argv[1] && import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href) {
    main();
}
