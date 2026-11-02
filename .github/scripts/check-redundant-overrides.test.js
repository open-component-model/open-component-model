// @ts-check

import assert from "node:assert";
import { test } from "node:test";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  removeOverrideKey,
  extractAdvisoryIds,
  parseAuditOutput,
  newlyIntroduced,
  classify,
  selectCombinedRemovals,
  auditVariant,
  ResolutionError,
  RESOLUTION_FAILURE,
  renderMarkdown,
  applyRemovals,
  IGNORE,
} from "./check-redundant-overrides.js";

test("removeOverrideKey drops one key without mutating input", () => {
  const overrides = { "minimatch@<11.0.1": "11.0.1", glob: "^14.0.2", semver: "^7.5.4" };
  const result = removeOverrideKey(overrides, "glob");
  assert.deepStrictEqual(result, { "minimatch@<11.0.1": "11.0.1", semver: "^7.5.4" });
  assert.deepStrictEqual(overrides, { "minimatch@<11.0.1": "11.0.1", glob: "^14.0.2", semver: "^7.5.4" });
});

test("removeOverrideKey handles a nested override value", () => {
  const overrides = { "eslint-config-developit": { "eslint-plugin-compat": { eslint: "$eslint" } }, glob: "^14.0.2" };
  assert.deepStrictEqual(removeOverrideKey(overrides, "eslint-config-developit"), { glob: "^14.0.2" });
});

test("extractAdvisoryIds pulls GHSA and CVE ids from audit output", () => {
  const audit = {
    vulnerabilities: { minimatch: { via: [{ url: "https://github.com/advisories/GHSA-g3hv-75wg-m249" }] } },
    advisories: { note: "see CVE-2026-118764" },
  };
  const ids = extractAdvisoryIds(audit);
  assert.ok(ids.has("GHSA-G3HV-75WG-M249"));
  assert.ok(ids.has("CVE-2026-118764"));
});

test("extractAdvisoryIds returns an empty set for a clean audit", () => {
  assert.strictEqual(extractAdvisoryIds({ vulnerabilities: {}, metadata: {} }).size, 0);
});

test("parseAuditOutput accepts a clean audit report", () => {
  const doc = parseAuditOutput(JSON.stringify({ auditReportVersion: 2, vulnerabilities: {}, metadata: {} }));
  assert.deepStrictEqual(doc.vulnerabilities, {});
  assert.strictEqual(extractAdvisoryIds(doc).size, 0);
});

test("parseAuditOutput accepts an audit report with vulnerabilities", () => {
  const report = {
    auditReportVersion: 2,
    vulnerabilities: { minimatch: { via: [{ url: "https://github.com/advisories/GHSA-g3hv-75wg-m249" }] } },
  };
  const ids = extractAdvisoryIds(parseAuditOutput(JSON.stringify(report)));
  assert.deepStrictEqual([...ids], ["GHSA-G3HV-75WG-M249"]);
});

test("parseAuditOutput rejects empty output", () => {
  assert.throws(() => parseAuditOutput(""), /non-json output/);
});

test("parseAuditOutput rejects non-JSON output", () => {
  assert.throws(() => parseAuditOutput("npm ERR! network timeout"), /non-json output: npm ERR! network timeout/);
});

test("parseAuditOutput rejects an npm error document and keeps the error as cause", () => {
  const error = { code: "ENOAUDIT", summary: "Your configured registry does not support audit requests.", detail: "" };
  assert.throws(
    () => parseAuditOutput(JSON.stringify({ error })),
    (err) => {
      assert.ok(err instanceof Error);
      assert.match(err.message, /npm audit failed: Your configured registry does not support audit requests\./);
      assert.deepStrictEqual(err.cause, error);
      return true;
    },
  );
});

test("parseAuditOutput rejects an npm error document without a summary", () => {
  assert.throws(() => parseAuditOutput(JSON.stringify({ error: { code: "E500" } })), /npm audit failed: \{"code":"E500"\}/);
});

test("parseAuditOutput rejects JSON that is not an audit report", () => {
  assert.throws(() => parseAuditOutput(JSON.stringify({ metadata: {} })), /did not produce the expected output/);
});

test("newlyIntroduced returns advisories present in candidate but not baseline", () => {
  const base = new Set(["GHSA-AAAA-BBBB-CCCC"]);
  const candidate = new Set(["GHSA-AAAA-BBBB-CCCC", "GHSA-G3HV-75WG-M249"]);
  assert.deepStrictEqual(newlyIntroduced(base, candidate), ["GHSA-G3HV-75WG-M249"]);
});

test("newlyIntroduced is empty when removing the override changes nothing", () => {
  const same = new Set(["GHSA-AAAA-BBBB-CCCC"]);
  assert.deepStrictEqual(newlyIntroduced(same, new Set(["GHSA-AAAA-BBBB-CCCC"])), []);
});

test("classify splits candidates, protecting and ignored", () => {
  const result = classify({
    overrideKeys: ["glob", "minimatch@<11.0.1", "eslint-config-developit"],
    introducedByKey: {
      glob: [], // removal introduces nothing -> candidate
      "minimatch@<11.0.1": ["GHSA-g3hv-75wg-m249"], // removal reintroduces -> protecting
    },
    ignore: { "eslint-config-developit": "compat" },
  });
  assert.deepStrictEqual(result.candidates, ["glob"]);
  assert.deepStrictEqual(result.protecting, [{ key: "minimatch@<11.0.1", advisories: ["GHSA-g3hv-75wg-m249"] }]);
  assert.deepStrictEqual(result.ignored, ["eslint-config-developit"]);
});

test("classify never reports an ignored override as a candidate", () => {
  const result = classify({
    overrideKeys: ["eslint-config-developit"],
    introducedByKey: { "eslint-config-developit": [] },
    ignore: { "eslint-config-developit": "compat" },
  });
  assert.deepStrictEqual(result.candidates, []);
  assert.deepStrictEqual(result.ignored, ["eslint-config-developit"]);
});

test("the shipped IGNORE set excludes the eslint compatibility override", () => {
  assert.ok("eslint-config-developit" in IGNORE);
});

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

test("renderMarkdown lists held-back overrides with the advisory they reintroduce together", () => {
  const md = renderMarkdown({
    candidates: ["a"],
    protecting: [],
    ignored: [],
    heldBack: [{ key: "b", advisories: ["GHSA-XXXX-XXXX-XXXX"] }],
  });
  assert.match(md, /### Held back/);
  assert.match(md, /`b` together with the removal candidates reintroduces GHSA-XXXX-XXXX-XXXX/);
});

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

test("auditVariant reports a resolution failure as RESOLUTION_FAILURE and logs the variant", () => {
  const { value, stderr } = captureStderr(() =>
    auditVariant("Without x", () => {
      throw new ResolutionError("npm install failed");
    }),
  );
  assert.deepStrictEqual(value, new Set([RESOLUTION_FAILURE]));
  assert.match(stderr, /Without x: npm install failed/);
});

test("auditVariant rethrows errors that are not resolution failures", () => {
  assert.throws(
    () =>
      auditVariant("Without x", () => {
        throw new Error("npm audit failed: registry down");
      }),
    /npm audit failed: registry down/,
  );
});

test("an override whose removal breaks resolution is still required, not a candidate", () => {
  const result = classify({
    overrideKeys: ["peer-pin", "glob"],
    introducedByKey: {
      "peer-pin": newlyIntroduced(new Set(["GHSA-BASE-BASE-BASE"]), new Set([RESOLUTION_FAILURE])),
      glob: [],
    },
    ignore: {},
  });
  assert.deepStrictEqual(result.candidates, ["glob"]);
  assert.deepStrictEqual(result.protecting, [{ key: "peer-pin", advisories: [RESOLUTION_FAILURE] }]);
  assert.match(renderMarkdown(result, {}), /`peer-pin` protects against a dependency resolution failure/);
});

test("selectCombinedRemovals holds back a candidate whose combined removal breaks resolution", () => {
  const result = selectCombinedRemovals({
    candidates: ["a", "b"],
    baseline: new Set(),
    auditWithout: (keys) => (keys.length > 1 ? new Set([RESOLUTION_FAILURE]) : new Set()),
  });
  assert.deepStrictEqual(result, { removable: ["a"], heldBack: [{ key: "b", advisories: [RESOLUTION_FAILURE] }] });
});

test("renderMarkdown lists removal candidates", () => {
  const md = renderMarkdown({ candidates: ["glob"], protecting: [], ignored: [] });
  assert.match(md, /Removal candidates/);
  assert.match(md, /glob/);
});

test("renderMarkdown names the advisory a required override protects against", () => {
  const md = renderMarkdown({ candidates: [], protecting: [{ key: "minimatch@<11.0.1", advisories: ["GHSA-g3hv-75wg-m249"] }], ignored: [] });
  assert.match(md, /still required/i);
  assert.match(md, /GHSA-g3hv-75wg-m249/);
});

test("renderMarkdown states nothing to remove when there are no candidates", () => {
  const md = renderMarkdown({ candidates: [], protecting: [], ignored: [] });
  assert.match(md, /Nothing to remove/);
});

test("renderMarkdown lists ignored overrides with their reason", () => {
  const md = renderMarkdown({ candidates: [], protecting: [], ignored: ["eslint-config-developit"] });
  assert.match(md, /Ignored \(non-security\)/);
  assert.ok(md.includes(`\`eslint-config-developit\` — ${IGNORE["eslint-config-developit"]}`));
});

test("renderMarkdown takes ignore reasons from the given set", () => {
  const md = renderMarkdown({ candidates: [], protecting: [], ignored: ["some-pin"] }, { "some-pin": "custom reason" });
  assert.ok(md.includes("`some-pin` — custom reason"));
  assert.ok(!md.includes("undefined"));
});

test("applyRemovals deletes only the given override keys and keeps clean JSON", () => {
  const root = mkdtempSync(join(tmpdir(), "ocm-apply-"));
  try {
    mkdirSync(join(root, "website"));
    const pkg = { name: "x", overrides: { "minimatch@<11.0.1": "11.0.1", semver: "^7.5.4", glob: "^14.0.2" } };
    writeFileSync(join(root, "website", "package.json"), JSON.stringify(pkg, null, 2) + "\n");

    const removed = applyRemovals(root, ["semver", "glob", "absent"]);
    assert.deepStrictEqual(removed, ["semver", "glob"]);

    const after = readFileSync(join(root, "website", "package.json"), "utf8");
    assert.deepStrictEqual(JSON.parse(after).overrides, { "minimatch@<11.0.1": "11.0.1" });
    assert.ok(after.endsWith("\n"));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
