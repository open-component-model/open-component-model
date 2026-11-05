// @ts-check

import assert from "node:assert";
import { test } from "node:test";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  parseNpmOutput,
  parseAuditOutput,
  NpmError,
  extractAdvisoryIds,
  newlyIntroduced,
  partitionIgnored,
  classify,
  selectCombinedRemovals,
  auditVariant,
  RESOLUTION_FAILURE,
  renderMarkdown,
  removeOverrideKey,
  applyRemovals,
  PACKAGE_DIRS,
} from "./check-redundant-overrides.js";

// --- npm output -------------------------------------------------------------

// Real `--json` stdout of npm 11, shortened.
const INSTALL_OK = JSON.stringify({ add: [], added: 0, audited: 0, change: [], changed: 0, funding: 0, remove: [], removed: 0 });
const INSTALL_ERESOLVE = JSON.stringify({
  error: { code: "ERESOLVE", summary: "unable to resolve dependency tree", detail: "Could not resolve dependency:\npeer react@\"^18.2.0\" from react-dom@18.2.0" },
});
const INSTALL_NETWORK = JSON.stringify({
  error: { code: "ECONNREFUSED", summary: "FetchError: request to http://127.0.0.1:9/left-pad failed, reason: connect ECONNREFUSED 127.0.0.1:9", detail: "" },
});
const AUDIT_UNREACHABLE = JSON.stringify({
  message: "request to http://127.0.0.1:9/-/npm/v1/security/advisories/bulk failed, reason: connect ECONNREFUSED 127.0.0.1:9",
  error: { summary: "", detail: "" },
});

test("parseNpmOutput returns the parsed document of a successful command", () => {
  assert.strictEqual(parseNpmOutput("install", INSTALL_OK).added, 0);
});

test("parseNpmOutput throws an NpmError with npm's code, the error as cause and a readable reason", () => {
  /** @type {Array<[string, string, string | undefined, RegExp]>} */
  const cases = [
    ["install", INSTALL_ERESOLVE, "ERESOLVE", /^npm install failed: unable to resolve dependency tree$/],
    ["install", INSTALL_NETWORK, "ECONNREFUSED", /^npm install failed: FetchError: .* connect ECONNREFUSED/],
    // empty summary: falls back to the top-level message
    ["audit", AUDIT_UNREACHABLE, undefined, /^npm audit failed: request to .* connect ECONNREFUSED/],
    // neither summary nor message: falls back to the error object
    ["audit", JSON.stringify({ error: { code: "E500" } }), "E500", /^npm audit failed: \{"code":"E500"\}$/],
  ];
  for (const [command, stdout, code, message] of cases) {
    assert.throws(
      () => parseNpmOutput(command, stdout),
      (err) => {
        assert.ok(err instanceof NpmError);
        assert.strictEqual(err.code, code);
        assert.deepStrictEqual(err.cause, JSON.parse(stdout).error);
        assert.match(err.message, message);
        return true;
      },
      stdout,
    );
  }
});

test("parseNpmOutput rejects output that is not JSON", () => {
  assert.throws(() => parseNpmOutput("audit", ""), /^Error: npm audit returned non-json output: $/);
  assert.throws(() => parseNpmOutput("audit", "npm ERR! network timeout"), /non-json output: npm ERR! network timeout/);
});

test("parseAuditOutput returns an audit report", () => {
  const report = { auditReportVersion: 2, vulnerabilities: {}, metadata: {} };
  assert.deepStrictEqual(parseAuditOutput(JSON.stringify(report)), report);
});

test("parseAuditOutput rejects JSON that is not an audit report", () => {
  assert.throws(() => parseAuditOutput(JSON.stringify({ metadata: {} })), /did not produce the expected output/);
});

// --- advisory comparison ----------------------------------------------------

test("extractAdvisoryIds takes the GHSA id from each advisory url, upper-cased", () => {
  const audit = {
    vulnerabilities: {
      // Real entry; the title mentions a CVE that is not an advisory of this report.
      "adm-zip": {
        via: [
          {
            source: 1240032,
            url: "https://github.com/advisories/GHSA-rcw4-f5rp-g42v",
            title: "adm-zip: Decompression-bomb protection (fix for CVE-2026-39244) can be bypassed by declaring uncompressed size as 0",
          },
        ],
      },
      // A string names the vulnerable dependency, which has its own entry.
      "some-parent": { via: ["adm-zip"] },
    },
  };
  assert.deepStrictEqual(extractAdvisoryIds(audit), new Set(["GHSA-RCW4-F5RP-G42V"]));
});

test("extractAdvisoryIds counts an advisory without a GHSA url by its npm source", () => {
  const audit = { vulnerabilities: { legacy: { via: [{ source: 1096302, url: "https://npmjs.com/advisories/1096302" }] } } };
  assert.deepStrictEqual(extractAdvisoryIds(audit), new Set(["npm:1096302"]));
});

test("extractAdvisoryIds returns an empty set for a clean audit", () => {
  assert.strictEqual(extractAdvisoryIds({ vulnerabilities: {} }).size, 0);
});

test("newlyIntroduced returns the sorted ids that are not in the baseline", () => {
  const base = new Set(["GHSA-BASE-BASE-BASE"]);
  assert.deepStrictEqual(newlyIntroduced(base, new Set(["GHSA-ZZZZ-ZZZZ-ZZZZ", "GHSA-BASE-BASE-BASE", "GHSA-AAAA-AAAA-AAAA"])), [
    "GHSA-AAAA-AAAA-AAAA",
    "GHSA-ZZZZ-ZZZZ-ZZZZ",
  ]);
  assert.deepStrictEqual(newlyIntroduced(base, new Set(["GHSA-BASE-BASE-BASE"])), []);
});

// --- classification ---------------------------------------------------------

test("partitionIgnored splits keys into audited and ignored, keeping their order", () => {
  const result = partitionIgnored(["glob", "eslint-config-developit", "semver"], { "eslint-config-developit": "compat" });
  assert.deepStrictEqual(result, { audited: ["glob", "semver"], ignored: ["eslint-config-developit"] });
});

test("partitionIgnored does not treat inherited Object properties as ignored", () => {
  assert.deepStrictEqual(partitionIgnored(["constructor", "tostring"], {}), { audited: ["constructor", "tostring"], ignored: [] });
});

test("classify splits audited keys into candidates and protecting", () => {
  const result = classify({
    keys: ["glob", "minimatch@<11.0.1"],
    introducedByKey: { glob: [], "minimatch@<11.0.1": ["GHSA-G3HV-75WG-M249"] },
  });
  assert.deepStrictEqual(result, {
    candidates: ["glob"],
    protecting: [{ key: "minimatch@<11.0.1", advisories: ["GHSA-G3HV-75WG-M249"] }],
  });
});

test("classify does not read inherited Object properties from introducedByKey", () => {
  assert.deepStrictEqual(classify({ keys: ["constructor"], introducedByKey: {} }), { candidates: ["constructor"], protecting: [] });
});

// --- combined removal -------------------------------------------------------

/**
 * Fake audit in which overrides "a" and "b" both protect against GHSA-XXXX-XXXX-XXXX:
 * removing either alone is clean, removing both reintroduces it. Records each call.
 *
 * @param {string[][]} calls
 * @returns {(keys: string[]) => Set<string>}
 */
function overlappingAudit(calls) {
  return (keys) => {
    calls.push([...keys]);
    const ids = new Set(["GHSA-BASE-BASE-BASE"]);
    if (keys.includes("a") && keys.includes("b")) ids.add("GHSA-XXXX-XXXX-XXXX");
    return ids;
  };
}

test("selectCombinedRemovals does not audit again for fewer than two candidates", () => {
  /** @type {string[][]} */
  const calls = [];
  const audit = overlappingAudit(calls);
  assert.deepStrictEqual(selectCombinedRemovals({ candidates: [], baseline: new Set(), auditWithout: audit }), { removable: [], heldBack: [] });
  assert.deepStrictEqual(selectCombinedRemovals({ candidates: ["a"], baseline: new Set(), auditWithout: audit }), { removable: ["a"], heldBack: [] });
  assert.deepStrictEqual(calls, []);
});

test("selectCombinedRemovals keeps all candidates when the combined removal is clean", () => {
  /** @type {string[][]} */
  const calls = [];
  const result = selectCombinedRemovals({
    candidates: ["a", "c", "d"],
    baseline: new Set(["GHSA-BASE-BASE-BASE"]),
    auditWithout: overlappingAudit(calls),
  });
  assert.deepStrictEqual(result, { removable: ["a", "c", "d"], heldBack: [] });
  assert.deepStrictEqual(calls, [["a", "c", "d"]]);
});

test("selectCombinedRemovals holds back an override that is only needed together with another", () => {
  /** @type {string[][]} */
  const calls = [];
  const result = selectCombinedRemovals({
    candidates: ["a", "b", "c"],
    baseline: new Set(["GHSA-BASE-BASE-BASE"]),
    auditWithout: overlappingAudit(calls),
  });
  assert.deepStrictEqual(result, {
    removable: ["a", "c"],
    heldBack: [{ key: "b", advisories: ["GHSA-XXXX-XXXX-XXXX"] }],
  });
  // All at once, then incremental from the first candidate (already clean alone).
  assert.deepStrictEqual(calls, [["a", "b", "c"], ["a", "b"], ["a", "c"]]);
});

test("selectCombinedRemovals ignores advisories already in the baseline", () => {
  const result = selectCombinedRemovals({
    candidates: ["a", "b"],
    baseline: new Set(["GHSA-BASE-BASE-BASE", "GHSA-XXXX-XXXX-XXXX"]),
    auditWithout: overlappingAudit([]),
  });
  assert.deepStrictEqual(result, { removable: ["a", "b"], heldBack: [] });
});

// --- resolution failures ----------------------------------------------------

/**
 * Run fn with process.stderr.write captured.
 *
 * @param {() => unknown} fn
 * @returns {{ value: unknown, stderr: string }}
 */
function captureStderr(fn) {
  const original = process.stderr.write;
  let stderr = "";
  process.stderr.write = /** @type {typeof process.stderr.write} */ ((chunk) => {
    stderr += String(chunk);
    return true;
  });
  try {
    return { value: fn(), stderr };
  } finally {
    process.stderr.write = original;
  }
}

test("auditVariant returns the audit result when the variant resolves", () => {
  const ids = new Set(["GHSA-AAAA-BBBB-CCCC"]);
  assert.strictEqual(auditVariant("Without x", () => ids), ids);
});

test("auditVariant reports ERESOLVE as RESOLUTION_FAILURE and logs the variant", () => {
  const { value, stderr } = captureStderr(() => auditVariant("Without x", () => parseNpmOutput("install", INSTALL_ERESOLVE)));
  assert.deepStrictEqual(value, new Set([RESOLUTION_FAILURE]));
  assert.match(stderr, /Without x: npm install failed: unable to resolve dependency tree/);
});

test("auditVariant rethrows every other error, such as a network failure", () => {
  const network = () => parseNpmOutput("install", INSTALL_NETWORK);
  const other = () => {
    throw new Error("npm audit did not produce the expected output");
  };
  assert.throws(() => auditVariant("Without x", network), (err) => err instanceof NpmError && err.code === "ECONNREFUSED");
  assert.throws(() => auditVariant("Without x", other), /did not produce the expected output/);
});

// --- report -----------------------------------------------------------------

test("renderMarkdown renders every section", () => {
  const md = renderMarkdown(
    {
      candidates: ["glob"],
      heldBack: [{ key: "b", advisories: ["GHSA-XXXX-XXXX-XXXX"] }],
      protecting: [{ key: "katex", advisories: ["GHSA-238P-PMPM-9MQ7", RESOLUTION_FAILURE] }],
      ignored: ["some-pin"],
    },
    { "some-pin": "custom reason" },
  );
  const lines = md.split("\n");
  for (const line of [
    "### Removal candidates",
    "- `glob`",
    "### Held back: needed together with other overrides",
    "- `b` together with the removal candidates reintroduces GHSA-XXXX-XXXX-XXXX",
    "### Still required",
    "- `katex` protects against GHSA-238P-PMPM-9MQ7, a dependency resolution failure",
    "### Ignored (non-security)",
    "- `some-pin` — custom reason",
  ]) {
    assert.ok(lines.includes(line), `missing line: ${line}\n${md}`);
  }
});

test("renderMarkdown states nothing to remove and omits empty sections", () => {
  const md = renderMarkdown({ candidates: [], protecting: [], ignored: [] });
  assert.match(md, /Nothing to remove/);
  assert.doesNotMatch(md, /###/);
});

test("renderMarkdown names the package file in the heading and the removal hint", () => {
  const md = renderMarkdown({ candidates: ["glob"], protecting: [], ignored: [] }, {}, ".github/scripts/package.json");
  assert.match(md, /^## Redundant-override check: `\.github\/scripts\/package\.json`$/m);
  assert.match(md, /Removing these from `\.github\/scripts\/package\.json`/);
});

test("PACKAGE_DIRS covers both checked npm packages", () => {
  assert.deepStrictEqual(PACKAGE_DIRS, ["website", ".github/scripts"]);
});

// --- package.json edits -----------------------------------------------------

test("removeOverrideKey drops one key without mutating the input", () => {
  const overrides = { "minimatch@<11.0.1": "11.0.1", glob: "^14.0.2", semver: "^7.5.4" };
  assert.deepStrictEqual(removeOverrideKey(overrides, "glob"), { "minimatch@<11.0.1": "11.0.1", semver: "^7.5.4" });
  assert.deepStrictEqual(overrides, { "minimatch@<11.0.1": "11.0.1", glob: "^14.0.2", semver: "^7.5.4" });
});

test("applyRemovals deletes only the given override keys and keeps clean JSON", () => {
  const root = mkdtempSync(join(tmpdir(), "ocm-apply-"));
  try {
    mkdirSync(join(root, "website"));
    const pkg = { name: "x", overrides: { "minimatch@<11.0.1": "11.0.1", semver: "^7.5.4", glob: "^14.0.2" } };
    writeFileSync(join(root, "website", "package.json"), JSON.stringify(pkg, null, 2) + "\n");

    const removed = applyRemovals(root, ["semver", "glob", "absent", "constructor"]);
    assert.deepStrictEqual(removed, ["semver", "glob"]);

    const after = readFileSync(join(root, "website", "package.json"), "utf8");
    assert.deepStrictEqual(JSON.parse(after).overrides, { "minimatch@<11.0.1": "11.0.1" });
    assert.ok(after.endsWith("\n"));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("applyRemovals edits the package.json of the given package directory", () => {
  const root = mkdtempSync(join(tmpdir(), "ocm-apply-dir-"));
  try {
    mkdirSync(join(root, ".github", "scripts"), { recursive: true });
    const pkg = { name: "scripts", overrides: { "minimatch@<11.0.1": "11.0.1", "@actions/github": { undici: "^6.29.1" } } };
    writeFileSync(join(root, ".github", "scripts", "package.json"), JSON.stringify(pkg, null, 2) + "\n");

    const removed = applyRemovals(root, ["@actions/github"], ".github/scripts");
    assert.deepStrictEqual(removed, ["@actions/github"]);

    const after = JSON.parse(readFileSync(join(root, ".github", "scripts", "package.json"), "utf8"));
    assert.deepStrictEqual(after.overrides, { "minimatch@<11.0.1": "11.0.1" });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
