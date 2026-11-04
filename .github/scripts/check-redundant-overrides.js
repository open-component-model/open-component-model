// @ts-check

/**
 * Redundant-override detector for the repo's npm packages (website/package.json
 * and .github/scripts/package.json, see PACKAGE_DIRS).
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
 * If npm cannot resolve the dependency tree without an override (ERESOLVE),
 * that override is reported as still required, and the other overrides are
 * still checked. Other install failures, such as network errors, stop the run.
 *
 * Not every override exists for a security advisory (for example a peer-dep
 * compatibility override). Those would always look removable, so the IGNORE
 * set below lists them, each with a reason. They are reported as ignored, not
 * as candidates.
 *
 * Run locally:
 *   node .github/scripts/check-redundant-overrides.js [repo-root]
 * Both packages are checked; pass --apply to edit the package.json files.
 */

import { spawnSync } from "node:child_process";
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
/**
 * npm packages checked for redundant overrides, each a path relative to the
 * repo root. Every entry is a standalone npm package with its own lockfile.
 *
 * @type {string[]}
 */
export const PACKAGE_DIRS = ["website", ".github/scripts"];

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
 * Collect the advisories of an `npm audit --json` (report version 2) document.
 * Each advisory is a `via` object of a vulnerable package. Its id is the GHSA
 * id from its `url`, or `npm:<source>` when the url has none, so every
 * advisory is counted.
 *
 * @param {{ vulnerabilities: Record<string, any> }} auditJson - Parsed `npm audit --json` output.
 * @returns {Set<string>}
 */
export function extractAdvisoryIds(auditJson) {
  const ids = new Set();
  for (const vulnerability of Object.values(auditJson.vulnerabilities)) {
    for (const via of vulnerability.via ?? []) {
      // A string names another vulnerable package; its own entry holds the advisory.
      if (typeof via !== "object") continue;
      const ghsa = /GHSA(-[0-9a-z]{4}){3}/i.exec(via.url ?? "");
      ids.add(ghsa ? ghsa[0].toUpperCase() : `npm:${via.source}`);
    }
  }
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
 * Split override keys into those to audit and those in the ignore set.
 *
 * @param {string[]} overrideKeys - keys of package.json `overrides`.
 * @param {Record<string, string>} [ignore] - keys to treat as non-security.
 * @returns {{ audited: string[], ignored: string[] }}
 */
export function partitionIgnored(overrideKeys, ignore = IGNORE) {
  /** @type {string[]} */
  const audited = [];
  /** @type {string[]} */
  const ignored = [];
  for (const key of overrideKeys) (Object.hasOwn(ignore, key) ? ignored : audited).push(key);
  return { audited, ignored };
}

/**
 * Classify audited override keys by the advisories their removal introduces.
 *
 * @param {object} args
 * @param {string[]} args.keys - audited override keys, see partitionIgnored.
 * @param {Record<string, string[]>} args.introducedByKey - key -> advisory ids its removal introduces.
 * @returns {{ candidates: string[], protecting: Array<{ key: string, advisories: string[] }> }}
 */
export function classify({ keys, introducedByKey }) {
  const candidates = [];
  const protecting = [];
  for (const key of keys) {
    const advisories = Object.hasOwn(introducedByKey, key) ? introducedByKey[key] : [];
    if (advisories.length === 0) candidates.push(key);
    else protecting.push({ key, advisories });
  }
  return { candidates, protecting };
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
 * @typedef {object} Result
 * @property {string[]} candidates - safe to remove, also together.
 * @property {Array<{ key: string, advisories: string[] }>} protecting
 * @property {string[]} ignored
 * @property {Array<{ key: string, advisories: string[] }>} [heldBack]
 */

/**
 * Render a Markdown report for the classification result.
 *
 * @param {Result} result
 * @param {Record<string, string>} [ignore] - reasons for ignored keys, same set as passed to partitionIgnored.
 * @param {string} [packageFile] - the package.json the report is about, for the heading and hints.
 * @returns {string}
 */
export function renderMarkdown(result, ignore = IGNORE, packageFile = "website/package.json") {
  const lines = [`## Redundant-override check: \`${packageFile}\``, ""];
  if (result.candidates.length === 0) {
    lines.push("Every security override is still required. Nothing to remove.", "");
  } else {
    lines.push("### Removal candidates", "");
    lines.push(`Removing these from \`${packageFile}\` introduces no new advisory. If an override here is a non-security pin, add it to the \`IGNORE\` set in \`check-redundant-overrides.js\` instead.`, "");
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

/** npm reported an error in its `--json` output. `code` is npm's error code, for example ERESOLVE. */
export class NpmError extends Error {
  /**
   * @param {string} message
   * @param {string | undefined} code
   * @param {ErrorOptions} [options]
   */
  constructor(message, code, options) {
    super(message, options);
    this.name = "NpmError";
    this.code = code;
  }
}

/**
 * Parse the stdout of an npm command run with `--json`. In JSON mode npm
 * writes failures as `{ "error": { "code", "summary", "detail" } }` to stdout,
 * so failures are read from there, not from the exit code.
 *
 * @param {string} command - npm subcommand, for messages.
 * @param {string} stdout
 * @returns {any}
 */
export function parseNpmOutput(command, stdout) {
  let jsonStdout;
  try {
    jsonStdout = JSON.parse(stdout);
  } catch {
    throw new Error(`npm ${command} returned non-json output: ${stdout}`);
  }
  if (jsonStdout.error) {
    // `npm audit` can leave `summary` empty and put the reason in a top-level `message`.
    const reason = jsonStdout.error.summary || jsonStdout.message || JSON.stringify(jsonStdout.error);
    throw new NpmError(`npm ${command} failed: ${reason}`, jsonStdout.error.code, { cause: jsonStdout.error });
  }
  return jsonStdout;
}

/**
 * Parse and validate `npm audit --json` stdout. Throws when the output is not
 * JSON, reports an npm error, or is not an audit report.
 *
 * @param {string} stdout
 * @returns {{ vulnerabilities: Record<string, unknown> }}
 */
export function parseAuditOutput(stdout) {
  const jsonStdout = parseNpmOutput("audit", stdout);
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

/**
 * Audit a removal variant. A variant that npm cannot resolve (ERESOLVE)
 * introduces RESOLUTION_FAILURE, so the removed overrides are kept and the run
 * goes on. All other errors, such as a network failure or a failed
 * `npm audit`, still throw.
 *
 * @param {string} label - which variant, for the log.
 * @param {() => Set<string>} audit
 * @returns {Set<string>}
 */
export function auditVariant(label, audit) {
  try {
    return audit();
  } catch (err) {
    if (!(err instanceof NpmError && err.code === "ERESOLVE")) throw err;
    process.stderr.write(`${label}: ${err.message}. Keeping the override(s).\n`);
    return new Set([RESOLUTION_FAILURE]);
  }
}

/**
 * Upper limit for one npm call. A normal install or audit takes seconds, so
 * this only stops a hung call, for example on a stalled registry.
 */
const NPM_TIMEOUT_MS = 5 * 60 * 1000;

/**
 * Run an npm command with `--json` and return its stdout. The exit code is
 * not used: `npm audit` exits non-zero when it finds vulnerabilities, and
 * real failures are in the JSON (see parseNpmOutput). stderr goes to the log.
 * Throws when npm cannot be run or exceeds NPM_TIMEOUT_MS.
 *
 * @param {string[]} args
 * @param {string} cwd
 * @returns {string}
 */
function npmJson(args, cwd) {
  const result = spawnSync("npm", [...args, "--json"], {
    cwd,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "inherit"],
    // The default of 1 MiB would kill npm on a large audit report.
    maxBuffer: 64 * 1024 * 1024,
    timeout: NPM_TIMEOUT_MS,
  });
  if (result.error) {
    const timedOut = /** @type {NodeJS.ErrnoException} */ (result.error).code === "ETIMEDOUT";
    const reason = timedOut ? `timed out after ${NPM_TIMEOUT_MS / 60_000} minutes` : "could not be run";
    throw new Error(`npm ${args[0]} ${reason}`, { cause: result.error });
  }
  return result.stdout;
}

/**
 * Run `npm audit --json` for a copy of the package whose `overrides` object is
 * replaced by the given one, re-resolving a temp lockfile.
 * Throws NpmError when npm reports an error, for example ERESOLVE.
 *
 * @param {string} packageDir - Absolute path to the package directory.
 * @param {Record<string, unknown>} pkg - parsed package.json, not mutated.
 * @param {Record<string, unknown>} overrides
 * @returns {Set<string>} advisory ids present.
 */
function auditWithOverrides(packageDir, pkg, overrides) {
  const lockPath = join(packageDir, "package-lock.json");

  const tmp = mkdtempSync(join(tmpdir(), "ocm-override-check-"));
  try {
    writeFileSync(join(tmp, "package.json"), JSON.stringify({ ...pkg, overrides }, null, 2));
    if (existsSync(lockPath)) copyFileSync(lockPath, join(tmp, "package-lock.json"));

    parseNpmOutput("install", npmJson(["install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"], tmp));
    return extractAdvisoryIds(parseAuditOutput(npmJson(["audit"], tmp)));
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
}

/**
 * Entry point for CLI and CI use. Checks one package directory.
 *
 * @param {string} repoRoot
 * @param {string} [packageDir] - package path relative to repoRoot.
 * @returns {{ result: Result, markdown: string }}
 */
export function run(repoRoot, packageDir = "website") {
  const absPackageDir = join(repoRoot, packageDir);
  const packageJson = JSON.parse(readFileSync(join(absPackageDir, "package.json"), "utf8"));
  const overrides = /** @type {Record<string, unknown>} */ (packageJson.overrides ?? {});
  const { audited, ignored } = partitionIgnored(Object.keys(overrides));
  /** @param {Record<string, unknown>} variant */
  const audit = (variant) => auditWithOverrides(absPackageDir, packageJson, variant);

  // The baseline must resolve, otherwise no comparison is possible.
  const baseline = audit(overrides);

  /** @type {Record<string, string[]>} */
  const introducedByKey = {};
  for (const key of audited) {
    const without = auditVariant(`${packageDir}: without ${key}`, () => audit(removeOverrideKey(overrides, key)));
    introducedByKey[key] = newlyIntroduced(baseline, without);
  }

  const { candidates, protecting } = classify({ keys: audited, introducedByKey });
  const { removable, heldBack } = selectCombinedRemovals({
    candidates,
    baseline,
    auditWithout: (keys) => auditVariant(`${packageDir}: without ${keys.join(", ")}`, () => audit(keys.reduce(removeOverrideKey, overrides))),
  });

  /** @type {Result} */
  const result = { candidates: removable, heldBack, protecting, ignored };
  return { result, markdown: renderMarkdown(result, IGNORE, `${packageDir}/package.json`) };
}

/**
 * Remove the given override keys from a package.json, writing the file back
 * with stable 2-space JSON formatting (the file round-trips cleanly, so only
 * the deleted lines change). Returns the keys actually removed.
 *
 * @param {string} repoRoot
 * @param {string[]} keys
 * @param {string} [packageDir] - package path relative to repoRoot.
 * @returns {string[]}
 */
export function applyRemovals(repoRoot, keys, packageDir = "website") {
  const pkgPath = join(repoRoot, packageDir, "package.json");
  const pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
  const removed = [];
  for (const key of keys) {
    if (pkg.overrides && Object.hasOwn(pkg.overrides, key)) {
      delete pkg.overrides[key];
      removed.push(key);
    }
  }
  writeFileSync(pkgPath, JSON.stringify(pkg, null, 2) + "\n");
  return removed;
}

// Executed directly:
//   node .github/scripts/check-redundant-overrides.js [repo-root] [--apply]
// --apply removes the detected redundant overrides from each package.json
// listed in PACKAGE_DIRS.
if (process.argv[1] && import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href) {
  const args = process.argv.slice(2);
  const apply = args.includes("--apply");
  const positional = args.find((a) => !a.startsWith("--"));
  const repoRoot = resolve(positional ?? join(import.meta.dirname, "..", ".."));

  const reports = [];
  let candidateCount = 0;
  for (const packageDir of PACKAGE_DIRS) {
    const { result, markdown } = run(repoRoot, packageDir);
    reports.push(markdown);
    candidateCount += result.candidates.length;

    if (apply && result.candidates.length > 0) {
      const removed = applyRemovals(repoRoot, result.candidates, packageDir);
      reports.push(`Removed ${removed.length} override(s) from ${packageDir}/package.json: ${removed.join(", ")}\n`);
    }
  }
  const markdown = reports.join("\n");
  process.stdout.write(markdown + "\n");

  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, markdown + "\n");
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `candidate-count=${candidateCount}\n`);
}
