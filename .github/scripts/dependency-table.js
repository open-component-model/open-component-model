// @ts-check

import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import * as yaml from "js-yaml";

const __dirname = dirname(fileURLToPath(import.meta.url));
const DEFAULT_CONFIG = resolve(__dirname, "../../golangci.yml");
const BINDING_PREFIX = "ocm.software/open-component-model/bindings/go/";

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

function main() {
    const args = process.argv.slice(2);
    const command = args[0];

    if (command === "generate") {
        const configPath = args[1] ?? DEFAULT_CONFIG;
        const deps = parseDepguardRules(configPath);
        const layers = computeLayers(deps);
        const table = generateTable(deps, layers);
        process.stdout.write(table);
    } else {
        process.stderr.write("Usage: dependency-table.js <generate|diff> [...args]\n");
        process.stderr.write("  generate [config-path]            Generate layer table to stdout\n");
        process.exit(1);
    }
}

main();
