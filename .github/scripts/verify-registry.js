// @ts-check

import { execSync } from "node:child_process";

/**
 * Verify that .github/go-modules.yml is in sync with actual go.mod files.
 *
 * @param {string} registryPath - Path to the YAML registry file
 * @returns {{ missing: string[], extra: string[] }} Diff between actual and registered modules
 */
export function verifyRegistry(registryPath) {
  const raw = execSync(`yq -o=json ${registryPath}`, { encoding: "utf-8" });
  const registry = JSON.parse(raw);
  const registered = new Set(registry.modules.map((m) => m.path));

  const actual = new Set(
    execSync("find . -name go.mod -not -path '*/vendor/*' -exec dirname {} \\;", { encoding: "utf-8" })
      .split("\n")
      .filter(Boolean)
      .map((p) => p.replace(/^\.\//, "")),
  );

  const missing = [...actual].filter((p) => !registered.has(p)).sort();
  const extra = [...registered].filter((p) => !actual.has(p)).sort();

  return { missing, extra };
}

/**
 * Entry point for actions/github-script.
 *
 * @param {{ core: import("@actions/core") }} params
 */
export default function run({ core }) {
  const { missing, extra } = verifyRegistry(".github/go-modules.yml");

  if (missing.length > 0 || extra.length > 0) {
    if (missing.length > 0) {
      core.error(`Missing from registry:\n${missing.map((p) => `  ${p}`).join("\n")}`);
    }
    if (extra.length > 0) {
      core.error(`In registry but not found:\n${extra.map((p) => `  ${p}`).join("\n")}`);
    }
    core.setFailed("Module registry is out of sync. Please update .github/go-modules.yml");
  }
}
