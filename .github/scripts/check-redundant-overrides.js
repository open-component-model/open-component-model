// @ts-check

/**
 * Redundant-override detector for website/package.json.
 *
 * npm `overrides` become stale over time. Usually they are added to alleviate
 * a specific CVE before the fixed version is adopted upstream. But nothing
 * triggers a removal of that override once it is no longer necessary.
 * This script closes this gap.
 *
 * It works by removing each override individually, updating the package-lock
 * and running `npm audit`.
 * If removing an override introduces nothing new, it is a removal candidate.
 * If it reintroduces advisories, it is actively protecting against them and
 * the report names which (the rationale, discovered at run time).
 *
 * Two overrides can protect against the same advisory. Each then looks
 * redundant alone, but removing both reintroduces it. So the candidates are
 * also audited together, and a candidate that is only needed in combination
 * with others is held back, not reported as removable.
 *
 * If npm cannot resolve the dependency tree without an override (for example
 * ERESOLVE), that override is reported as still required, and the other
 * overrides are still checked.
 *
 * Not every override exists for a security advisory (for example a peer-dep
 * compatibility override). Those would always look removable, so the IGNORE
 * set below lists them, each with a reason. They are reported as ignored, not
 * as candidates.
 *
 * Run locally:
 *   node .github/scripts/check-redundant-overrides.js [repo-root]
 */

import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, copyFileSync, rmSync, existsSync, appendFileSync, realpathSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { pathToFileURL } from "node:url";

/**
 * Overrides that are not security related must not be reported as removal
 * candidates. Keyed by the override key exactly as it appears in package.json.
 *
 * @type {Record<string, string>}
 */
export const IGNORE = {
  // Peer-dependency alignment: forces eslint-plugin-compat (transitive via
  // eslint-config-developit) onto the root project's eslint. Compatibility,
  // not security; removing it never changes npm audit.
  "eslint-config-developit": "peer-dependency alignment for eslint-plugin-compat, not a security pin",
};

/**
 * Produce a copy of an `overrides` object with one key removed. Not mutated.
 *
 * @param {Record<string, unknown>} overrides
 * @param {string} key
 * @returns {Record<string, unknown>}
 */
export function removeOverrideKey(overrides, key) {
  return Object.fromEntries(Object.entries(overrides ?? {}).filter(([k]) => k !== key));
}

/**
 * Extract every advisory identifier (GHSA-... and CVE-...) referenced anywhere
 * in an `npm audit --json` document.
 *
 * @param {unknown} auditJson - Parsed `npm audit --json` output.
 * @returns {Set<string>}
 */
export function extractAdvisoryIds(auditJson) {
  const text = JSON.stringify(auditJson ?? {});
  const ids = new Set();
  const re = /(GHSA-[0-9a-z]{4}-[0-9a-z]{4}-[0-9a-z]{4}|CVE-\d{4}-\d{3,})/gi;
  for (const m of text.matchAll(re)) ids.add(m[1].toUpperCase());
  return ids;
}

/**
 * Advisory ids present in `candidate` but not in `base`.
 *
 * @param {Set<string>} base
 * @param {Set<string>} candidate
 * @returns {string[]} sorted newly-introduced ids.
 */
export function newlyIntroduced(base, candidate) {
  return [...candidate].filter((id) => !base.has(id)).sort();
}

/**
 * Classify every override key against the advisories its removal introduces.
 *
 * @param {object} args
 * @param {string[]} args.overrideKeys - keys of package.json `overrides`.
 * @param {Record<string, string[]>} args.introducedByKey - key -> advisory ids its removal introduces.
 * @param {Record<string, string>} [args.ignore] - keys to treat as non-security.
 * @returns {{ candidates: string[], protecting: Array<{ key: string, advisories: string[] }>, ignored: string[] }}
 */
export function classify({ overrideKeys, introducedByKey, ignore = IGNORE }) {
  const candidates = [];
  const protecting = [];
  const ignored = [];
  for (const key of overrideKeys) {
    if (key in ignore) {
      ignored.push(key);
      continue;
    }
    const advisories = introducedByKey[key] ?? [];
    if (advisories.length === 0) candidates.push(key);
    else protecting.push({ key, advisories });
  }
  return { candidates, protecting, ignored };
}

/**
 * Pick the subset of individually redundant overrides that is also safe to
 * remove together. First audit all candidates removed at once. If that
 * introduces advisories, add candidates one at a time in the given order and
 * keep each one only while the combined removal stays clean.
 *
 * @param {object} args
 * @param {string[]} args.candidates - individually redundant override keys.
 * @param {Set<string>} args.baseline - advisory ids with all overrides in place.
 * @param {(keys: string[]) => Set<string>} args.auditWithout - advisory ids with the given keys removed.
 * @returns {{ removable: string[], heldBack: Array<{ key: string, advisories: string[] }> }}
 */
export function selectCombinedRemovals({ candidates, baseline, auditWithout }) {
  // One candidate was already audited alone, so there is nothing to combine.
  if (candidates.length < 2) return { removable: [...candidates], heldBack: [] };
  if (newlyIntroduced(baseline, auditWithout(candidates)).length === 0) {
    return { removable: [...candidates], heldBack: [] };
  }

  // The first candidate was already audited alone and is clean.
  const removable = [candidates[0]];
  const heldBack = [];
  for (const key of candidates.slice(1)) {
    const advisories = newlyIntroduced(baseline, auditWithout([...removable, key]));
    if (advisories.length === 0) removable.push(key);
    else heldBack.push({ key, advisories });
  }
  return { removable, heldBack };
}

/**
 * @typedef {ReturnType<typeof classify> & { heldBack?: Array<{ key: string, advisories: string[] }> }} Result
 */

/**
 * Render a Markdown report for the classification result.
 *
 * @param {Result} result
 * @param {Record<string, string>} [ignore] - reasons for ignored keys, same set as passed to classify.
 * @returns {string}
 */
export function renderMarkdown(result, ignore = IGNORE) {
  const lines = ["## Redundant-override check", ""];
  if (result.candidates.length === 0) {
    lines.push("Every security override is still required. Nothing to remove.", "");
  } else {
    lines.push("### Removal candidates", "");
    lines.push("Removing these from `website/package.json` introduces no new advisory. If an override here is a non-security pin, add it to the `IGNORE` set in `check-redundant-overrides.js` instead.", "");
    for (const key of result.candidates) lines.push(`- \`${key}\``);
    lines.push("");
  }
  const heldBack = result.heldBack ?? [];
  if (heldBack.length > 0) {
    lines.push("### Held back: needed together with other overrides", "");
    lines.push("Each of these is redundant alone, but removing it together with the removal candidates reintroduces an advisory.", "");
    for (const h of heldBack) lines.push(`- \`${h.key}\` together with the removal candidates reintroduces ${h.advisories.join(", ")}`);
    lines.push("");
  }
  if (result.protecting.length > 0) {
    lines.push("### Still required", "");
    for (const p of result.protecting) lines.push(`- \`${p.key}\` protects against ${p.advisories.join(", ")}`);
    lines.push("");
  }
  if (result.ignored.length > 0) {
    lines.push("### Ignored (non-security)", "");
    for (const key of result.ignored) lines.push(`- \`${key}\` — ${ignore[key]}`);
    lines.push("");
  }
  return lines.join("\n");
}

/**
 * Parse and validate `npm audit --json` stdout. Throws when the output is not
 * JSON, reports an npm error, or is not an audit report.
 *
 * @param {string} stdout
 * @returns {{ vulnerabilities: Record<string, unknown> }}
 */
export function parseAuditOutput(stdout) {
  let jsonStdout;
  try {
    jsonStdout = JSON.parse(stdout);
  } catch {
    throw new Error(`npm audit returned non-json output: ${stdout}`);
  }
  if (jsonStdout.error) {
    throw new Error(`npm audit failed: ${jsonStdout.error.summary ?? JSON.stringify(jsonStdout.error)}`, { cause: jsonStdout.error });
  }
  if (!jsonStdout.vulnerabilities) {
    throw new Error(`npm audit did not produce the expected output ${stdout}`);
  }
  return jsonStdout;
}

/**
 * Reported in place of an advisory id when npm cannot resolve the dependency
 * tree without an override. The override then counts as still required.
 */
export const RESOLUTION_FAILURE = "a dependency resolution failure";

/** `npm install` failed for an overrides variant, for example with ERESOLVE. */
export class ResolutionError extends Error {}

/**
 * Audit a removal variant. A variant that npm cannot resolve introduces
 * RESOLUTION_FAILURE, so the removed overrides are kept and the run goes on.
 * All other errors, such as a failed `npm audit`, still throw.
 *
 * @param {string} label - which variant, for the log.
 * @param {() => Set<string>} audit
 * @returns {Set<string>}
 */
export function auditVariant(label, audit) {
  try {
    return audit();
  } catch (err) {
    if (!(err instanceof ResolutionError)) throw err;
    process.stderr.write(`${label}: ${err.message}. Keeping the override(s).\n`);
    return new Set([RESOLUTION_FAILURE]);
  }
}

/**
 * Run `npm audit --json` for a copy of the website package whose `overrides`
 * object is replaced by the given one, re-resolving a temp lockfile.
 * Throws ResolutionError when `npm install` fails.
 *
 * @param {string} websiteDir - Absolute path to the website/ directory.
 * @param {Record<string, unknown>} overrides
 * @returns {Set<string>} advisory ids present.
 */
function auditWithOverrides(websiteDir, overrides) {
  const pkg = JSON.parse(readFileSync(join(websiteDir, "package.json"), "utf8"));
  pkg.overrides = overrides;
  const lockPath = join(websiteDir, "package-lock.json");

  const tmp = mkdtempSync(join(tmpdir(), "ocm-override-check-"));
  try {
    writeFileSync(join(tmp, "package.json"), JSON.stringify(pkg, null, 2));
    if (existsSync(lockPath)) copyFileSync(lockPath, join(tmp, "package-lock.json"));

    try {
      execFileSync("npm", ["install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"], {
        cwd: tmp,
        stdio: ["ignore", "ignore", "inherit"],
      });
    } catch (err) {
      throw new ResolutionError("npm install failed, see the npm output above", { cause: err });
    }

    // `npm audit --json` exits non-zero when vulnerabilities exist, which is
    // the normal case here, so read stdout regardless of exit code, but ensure
    // the result does not contain an error.
    let stdout = "";
    try {
      stdout = execFileSync("npm", ["audit", "--json"], { cwd: tmp, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] });
    } catch (err) {
      stdout = /** @type {{ stdout?: string }} */ (err).stdout ?? "";
    }
    return extractAdvisoryIds(parseAuditOutput(stdout));
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
}

/**
 * Entry point for CLI and CI use.
 *
 * @param {string} repoRoot
 * @returns {{ result: Result, markdown: string }}
 */
export function run(repoRoot) {
  const websiteDir = join(repoRoot, "website");
  const packageJson = JSON.parse(readFileSync(join(websiteDir, "package.json"), "utf8"));
  const overrides = /** @type {Record<string, unknown>} */ (packageJson.overrides ?? {});
  const overrideKeys = Object.keys(overrides);

  // The baseline must resolve, otherwise no comparison is possible.
  const baseline = auditWithOverrides(websiteDir, overrides);

  /** @type {Record<string, string[]>} */
  const introducedByKey = {};
  for (const key of overrideKeys) {
    if (key in IGNORE) continue;
    const without = auditVariant(`Without ${key}`, () => auditWithOverrides(websiteDir, removeOverrideKey(overrides, key)));
    introducedByKey[key] = newlyIntroduced(baseline, without);
  }

  const classified = classify({ overrideKeys, introducedByKey });
  const { removable, heldBack } = selectCombinedRemovals({
    candidates: classified.candidates,
    baseline,
    auditWithout: (keys) =>
      auditVariant(`Without ${keys.join(", ")}`, () => auditWithOverrides(websiteDir, keys.reduce(removeOverrideKey, overrides))),
  });

  /** @type {Result} */
  const result = { ...classified, candidates: removable, heldBack };
  return { result, markdown: renderMarkdown(result) };
}

/**
 * Remove the given override keys from website/package.json, writing the file
 * back with stable 2-space JSON formatting (the file round-trips cleanly, so
 * only the deleted lines change). Returns the keys actually removed.
 *
 * @param {string} repoRoot
 * @param {string[]} keys
 * @returns {string[]}
 */
export function applyRemovals(repoRoot, keys) {
  const pkgPath = join(repoRoot, "website", "package.json");
  const pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
  const removed = [];
  for (const key of keys) {
    if (pkg.overrides && key in pkg.overrides) {
      delete pkg.overrides[key];
      removed.push(key);
    }
  }
  writeFileSync(pkgPath, JSON.stringify(pkg, null, 2) + "\n");
  return removed;
}

// Executed directly:
//   node .github/scripts/check-redundant-overrides.js [repo-root] [--apply]
// --apply removes the detected redundant overrides from website/package.json
if (process.argv[1] && import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href) {
  const args = process.argv.slice(2);
  const apply = args.includes("--apply");
  const positional = args.find((a) => !a.startsWith("--"));
  const repoRoot = resolve(positional ?? join(import.meta.dirname, "..", ".."));

  const { result, markdown } = run(repoRoot);
  process.stdout.write(markdown + "\n");

  if (apply && result.candidates.length > 0) {
    const removed = applyRemovals(repoRoot, result.candidates);
    process.stdout.write(`\nRemoved ${removed.length} override(s) from website/package.json: ${removed.join(", ")}\n`);
  }

  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, markdown + "\n");
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `candidate-count=${result.candidates.length}\n`);
  process.exit(0);
}
