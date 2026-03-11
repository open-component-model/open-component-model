// @ts-check

import { execSync } from "node:child_process";

/**
 * @typedef {Object} ModuleEntry
 * @property {string} path
 * @property {"unit" | "integration" | "none"} test
 * @property {string[]} [extra-checkout]
 */

/**
 * @typedef {Object} ResolveResult
 * @property {string[]} allModules
 * @property {string[]} filteredModules
 * @property {string[]} lintModules
 * @property {string[]} unitTestModules
 * @property {string[]} integrationTestModules
 * @property {Record<string, string>} sparseExtras
 * @property {boolean} envChanged
 */

/**
 * Determine which files changed relative to a base ref using git diff.
 * Returns null when all modules should run (no filtering).
 *
 * @param {string | undefined} baseRef - Base branch/ref to diff against, or undefined to skip filtering
 * @returns {string[] | null} List of changed file paths, or null if no filtering
 */
export function getChangedFiles(baseRef) {
  if (!baseRef) {
    return null;
  }

  execSync(`git fetch origin ${baseRef} --depth=1`, { stdio: "pipe" });
  const diff = execSync(`git diff --name-only origin/${baseRef}..HEAD`, {
    encoding: "utf-8",
  });
  return diff.split("\n").filter(Boolean);
}

/**
 * Check whether a module is affected by the given changed files.
 * Integration modules also trigger when their parent module changes.
 *
 * @param {string} modulePath
 * @param {string[]} changedFiles
 * @returns {boolean}
 */
export function isModuleAffected(modulePath, changedFiles) {
  const prefixes = [modulePath + "/"];

  if (modulePath.includes("/integration")) {
    prefixes.push(modulePath.split("/integration")[0] + "/");
  }

  return changedFiles.some((f) => prefixes.some((p) => f.startsWith(p)));
}

/**
 * Resolve module lists from the static YAML registry, optionally filtered
 * by a list of changed files.
 *
 * @param {string} registryPath - Path to .github/go-modules.yml
 * @param {string[] | null} changedFiles - Changed file paths, or null to include all modules
 * @returns {ResolveResult}
 */
export function resolveModules(registryPath, changedFiles) {
  const raw = execSync(`yq -o=json ${registryPath}`, { encoding: "utf-8" });
  /** @type {{ modules: ModuleEntry[] }} */
  const registry = JSON.parse(raw);
  const entries = registry.modules;

  const allModules = entries.map((e) => e.path);
  const unitModules = entries.filter((e) => e.test === "unit").map((e) => e.path);
  const integrationModules = entries.filter((e) => e.test === "integration").map((e) => e.path);

  // Filter by changed files (null = no filtering, include all)
  const filteredModules = changedFiles
    ? allModules.filter((m) => isModuleAffected(m, changedFiles))
    : allModules;

  // Lint all modules when .env changed, otherwise only changed
  const envChanged = changedFiles ? changedFiles.includes(".env") : false;
  const lintModules = envChanged ? allModules : filteredModules;

  // Build sparse-checkout extras map
  const sparseExtras = {};
  for (const entry of entries) {
    if (entry["extra-checkout"]?.length > 0) {
      sparseExtras[entry.path] = entry["extra-checkout"].join("\n");
    }
  }

  return {
    allModules,
    filteredModules,
    lintModules,
    unitTestModules: unitModules.filter((m) => filteredModules.includes(m)),
    integrationTestModules: integrationModules.filter((m) => filteredModules.includes(m)),
    sparseExtras,
    envChanged,
  };
}

/**
 * Entry point for actions/github-script.
 *
 * Environment variables:
 *   BASE_REF - base branch for PR diff (empty on push → no filtering)
 *
 * @param {{ core: import("@actions/core") }} params
 */
export default function run({ core }) {
  const baseRef = process.env.BASE_REF || undefined;
  const changedFiles = getChangedFiles(baseRef);
  const result = resolveModules(".github/go-modules.yml", changedFiles);

  core.setOutput("modules_json", JSON.stringify(result.filteredModules));
  core.setOutput("lint_modules_json", JSON.stringify(result.lintModules));
  core.setOutput("unit_test_modules_json", JSON.stringify(result.unitTestModules));
  core.setOutput("integration_test_modules_json", JSON.stringify(result.integrationTestModules));
  core.setOutput("sparse_extras_json", JSON.stringify(result.sparseExtras));
  core.setOutput("env_changed", String(result.envChanged));

  const { allModules, filteredModules, unitTestModules, integrationTestModules, envChanged } = result;
  console.log(
    `${allModules.length} registered, ${filteredModules.length} after filtering ` +
      `(${unitTestModules.length} unit, ${integrationTestModules.length} integration)`,
  );

  // Job summary
  const list = (items) => (items.length ? items.map((i) => `- \`${i}\``).join("\n") : "_None_");
  let summary = `### Go Module Discovery Summary\n\n`;
  summary += `**Registered Modules (${allModules.length}):**\n${list(allModules)}\n\n`;
  summary += `**Filtered Modules (${filteredModules.length}):**\n${list(filteredModules)}\n\n`;
  summary += `**Lint Scope:** ${envChanged ? "all modules (env changed)" : "changed modules only"}\n\n`;
  summary += `**Unit Test Modules (${unitTestModules.length}):**\n${list(unitTestModules)}\n\n`;
  summary += `**Integration Test Modules (${integrationTestModules.length}):**\n${list(integrationTestModules)}\n`;
  core.summary.addRaw(summary).write();
}
