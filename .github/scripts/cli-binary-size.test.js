// @ts-check
import assert from "assert";
import { test } from "node:test";
import {
  DEFAULT_THRESHOLD_PERCENT,
  compareAction,
  evaluate,
  findBaseline,
  parseThreshold,
  renderSummary,
  resolveAction,
} from "./cli-binary-size.js";

const MiB = 1024 * 1024;
const repo = { owner: "o", repo: "r" };

/**
 * @param {{ runs?: any[], parents?: any[] }} opts
 */
function fakeGithub({ runs = [], parents = [{ sha: "parent" }] } = {}) {
  /** @type {any[]} */
  const calls = [];
  return {
    calls,
    rest: {
      actions: {
        listWorkflowRuns: async (/** @type {any} */ params) => {
          calls.push(["listWorkflowRuns", params]);
          return { data: { workflow_runs: runs } };
        },
      },
      repos: {
        getCommit: async (/** @type {any} */ params) => {
          calls.push(["getCommit", params]);
          return { data: { parents } };
        },
      },
    },
  };
}

function fakeCore() {
  /** @type {Record<string, string>} */
  const outputs = {};
  const state = { failed: /** @type {string | undefined} */ (undefined), summary: "" };
  const summary = {
    addRaw(/** @type {string} */ text) {
      state.summary += text;
      return summary;
    },
    async write() {},
  };
  return {
    outputs,
    state,
    summary,
    setOutput: (/** @type {string} */ k, /** @type {string} */ v) => (outputs[k] = v),
    setFailed: (/** @type {string} */ msg) => (state.failed = msg),
    info: () => {},
    notice: () => {},
  };
}

/**
 * Run fn with the given environment variables set, then restore them.
 *
 * @param {Record<string, string | undefined>} vars
 * @param {() => Promise<void>} fn
 */
async function withEnv(vars, fn) {
  const saved = Object.fromEntries(Object.keys(vars).map((k) => [k, process.env[k]]));
  for (const [k, v] of Object.entries(vars)) {
    if (v === undefined) delete process.env[k];
    else process.env[k] = v;
  }
  try {
    await fn();
  } finally {
    for (const [k, v] of Object.entries(saved)) {
      if (v === undefined) delete process.env[k];
      else process.env[k] = v;
    }
  }
}

test("parseThreshold: empty input uses the default", () => {
  assert.strictEqual(parseThreshold(undefined), DEFAULT_THRESHOLD_PERCENT);
  assert.strictEqual(parseThreshold(""), DEFAULT_THRESHOLD_PERCENT);
  assert.strictEqual(parseThreshold("  "), DEFAULT_THRESHOLD_PERCENT);
});

test("parseThreshold: accepts plain decimals", () => {
  assert.strictEqual(parseThreshold("15"), 15);
  assert.strictEqual(parseThreshold("12.5"), 12.5);
  assert.strictEqual(parseThreshold(" 20 "), 20);
  assert.strictEqual(parseThreshold("0"), 0);
});

test("parseThreshold: rejects anything else", () => {
  for (const input of ["15%", "abc", "-1", "1e3", "1.", ".5", "10 20"]) {
    assert.throws(() => parseThreshold(input), /must be a non-negative number/, input);
  }
});

test("findBaseline: uses the head of the last green run on main", async () => {
  const github = fakeGithub({ runs: [{ head_sha: "green" }] });
  assert.deepStrictEqual(await findBaseline(github, repo, "tip"), { sha: "green", source: "last-green" });
  assert.deepStrictEqual(github.calls, [
    [
      "listWorkflowRuns",
      { ...repo, workflow_id: "cli-binary-size.yml", branch: "main", status: "success", per_page: 1 },
    ],
  ]);
});

test("findBaseline: first run falls back to the parent of the tip", async () => {
  const github = fakeGithub({ runs: [], parents: [{ sha: "parent" }] });
  assert.deepStrictEqual(await findBaseline(github, repo, "tip"), { sha: "parent", source: "parent" });
  assert.deepStrictEqual(github.calls[1], ["getCommit", { ...repo, ref: "tip" }]);
});

test("findBaseline: fails without a green run and without a parent", async () => {
  await assert.rejects(findBaseline(fakeGithub({ runs: [], parents: [] }), repo, "tip"), /has no parent/);
});

test("evaluate: growth below, at and above the threshold", () => {
  const base = { oldSize: 100 * MiB, threshold: 10, reset: false, sameCommit: false };
  assert.strictEqual(evaluate({ ...base, newSize: 105 * MiB }).over, false);
  assert.strictEqual(evaluate({ ...base, newSize: 110 * MiB }).over, false, "exactly at the threshold passes");
  assert.strictEqual(evaluate({ ...base, newSize: 110 * MiB + 1 }).over, true);
});

test("evaluate: a smaller binary passes with negative growth", () => {
  const result = evaluate({ oldSize: 100, newSize: 90, threshold: 10, reset: false, sameCommit: false });
  assert.strictEqual(result.over, false);
  assert.strictEqual(result.growthPercent, -10);
  assert.strictEqual(result.deltaBytes, -10);
});

test("evaluate: reset passes without a baseline size", () => {
  const result = evaluate({ newSize: 500, threshold: 0, reset: true, sameCommit: false });
  assert.deepStrictEqual(result, { kind: "reset", newSize: 500, over: false });
});

test("evaluate: same commit reports zero growth without a baseline size", () => {
  const result = evaluate({ newSize: 500, threshold: 0, reset: false, sameCommit: true });
  assert.strictEqual(result.kind, "same-commit");
  assert.strictEqual(result.growthPercent, 0);
  assert.strictEqual(result.over, false);
});

test("evaluate: rejects missing or invalid sizes", () => {
  const base = { threshold: 10, reset: false, sameCommit: false };
  assert.throws(() => evaluate({ ...base, newSize: 100 }), /invalid baseline/);
  assert.throws(() => evaluate({ ...base, oldSize: 0, newSize: 100 }), /invalid baseline/);
  assert.throws(() => evaluate({ ...base, oldSize: 100, newSize: NaN }), /invalid tip/);
});

test("renderSummary: compared run lists both commits and sizes", () => {
  const result = evaluate({ oldSize: 50 * MiB, newSize: 55 * MiB, threshold: 10, reset: false, sameCommit: false });
  const md = renderSummary(result, { baseSha: "b", tipSha: "t", threshold: 10, eventName: "push", actor: "x" });
  assert.match(md, /\| baseline \(last green\) \| `b` \|/);
  assert.match(md, /\| new tip \| `t` \|/);
  assert.match(md, /\| baseline size \| 50\.0 MiB \(52428800 bytes\) \|/);
  assert.match(md, /\| growth \| 10\.00% \(\+5\.0 MiB\) \|/);
  assert.doesNotMatch(md, /manual override/);
});

test("renderSummary: dispatch records the actor", () => {
  const result = evaluate({ oldSize: 100, newSize: 100, threshold: 15, reset: false, sameCommit: false });
  const md = renderSummary(result, {
    baseSha: "b",
    tipSha: "t",
    threshold: 15,
    eventName: "workflow_dispatch",
    actor: "maint",
  });
  assert.match(md, /\| manual override \| by @maint \|/);
});

test("renderSummary: reset names the new baseline and the actor", () => {
  const result = evaluate({ newSize: MiB, threshold: 10, reset: true, sameCommit: false });
  const md = renderSummary(result, {
    baseSha: "b",
    tipSha: "t",
    threshold: 10,
    eventName: "workflow_dispatch",
    actor: "maint",
  });
  assert.match(md, /Baseline reset by @maint/);
  assert.match(md, /\| new baseline \| `t` \|/);
  assert.doesNotMatch(md, /growth/);
});

test("resolveAction: push sets all outputs", async () => {
  const core = fakeCore();
  const context = { eventName: "push", ref: "refs/heads/main", sha: "tip", repo };
  await withEnv({ THRESHOLD_INPUT: "", RESET_INPUT: "" }, () =>
    resolveAction({ github: fakeGithub({ runs: [{ head_sha: "green" }] }), context, core }),
  );
  assert.strictEqual(core.state.failed, undefined);
  assert.deepStrictEqual(core.outputs, {
    base_sha: "green",
    tip_sha: "tip",
    threshold: "10",
    reset: "false",
    skip_baseline_build: "false",
  });
});

test("resolveAction: re-run of a green commit skips the baseline build", async () => {
  const core = fakeCore();
  const context = { eventName: "push", ref: "refs/heads/main", sha: "tip", repo };
  await withEnv({ THRESHOLD_INPUT: undefined, RESET_INPUT: undefined }, () =>
    resolveAction({ github: fakeGithub({ runs: [{ head_sha: "tip" }] }), context, core }),
  );
  assert.strictEqual(core.outputs.skip_baseline_build, "true");
});

test("resolveAction: reset skips the baseline build", async () => {
  const core = fakeCore();
  const context = { eventName: "workflow_dispatch", ref: "refs/heads/main", sha: "tip", repo };
  await withEnv({ THRESHOLD_INPUT: "", RESET_INPUT: "true" }, () =>
    resolveAction({ github: fakeGithub({ runs: [{ head_sha: "green" }] }), context, core }),
  );
  assert.strictEqual(core.outputs.reset, "true");
  assert.strictEqual(core.outputs.skip_baseline_build, "true");
});

test("resolveAction: dispatch on a branch fails before any API call", async () => {
  const core = fakeCore();
  const github = fakeGithub();
  const context = { eventName: "workflow_dispatch", ref: "refs/heads/feature", sha: "tip", repo };
  await withEnv({ THRESHOLD_INPUT: "", RESET_INPUT: "" }, () => resolveAction({ github, context, core }));
  assert.match(core.state.failed ?? "", /must run on main/);
  assert.deepStrictEqual(github.calls, []);
  assert.deepStrictEqual(core.outputs, {});
});

test("resolveAction: invalid threshold fails", async () => {
  const core = fakeCore();
  const context = { eventName: "workflow_dispatch", ref: "refs/heads/main", sha: "tip", repo };
  await withEnv({ THRESHOLD_INPUT: "15%", RESET_INPUT: "" }, () =>
    resolveAction({ github: fakeGithub(), context, core }),
  );
  assert.match(core.state.failed ?? "", /threshold_percent/);
});

/**
 * @param {Record<string, string | undefined>} env
 * @param {string} [eventName]
 */
async function runCompare(env, eventName = "push") {
  const core = fakeCore();
  await withEnv(
    { BASE_SHA: "b", TIP_SHA: "t", THRESHOLD: "10", RESET: "false", OLD_SIZE: undefined, NEW_SIZE: undefined, ...env },
    () => compareAction({ context: { eventName, actor: "maint" }, core }),
  );
  return core;
}

test("compareAction: growth within the threshold passes", async () => {
  const core = await runCompare({ OLD_SIZE: "100", NEW_SIZE: "105" });
  assert.strictEqual(core.state.failed, undefined);
  assert.match(core.state.summary, /\| growth \| 5\.00%/);
});

test("compareAction: growth above the threshold fails", async () => {
  const core = await runCompare({ OLD_SIZE: "100", NEW_SIZE: "120" });
  assert.match(core.state.failed ?? "", /above the 10% threshold/);
});

test("compareAction: reset passes without OLD_SIZE", async () => {
  const core = await runCompare({ RESET: "true", NEW_SIZE: "120" }, "workflow_dispatch");
  assert.strictEqual(core.state.failed, undefined);
  assert.match(core.state.summary, /Baseline reset by @maint/);
});

test("compareAction: same commit passes without OLD_SIZE", async () => {
  const core = await runCompare({ BASE_SHA: "t", NEW_SIZE: "120" });
  assert.strictEqual(core.state.failed, undefined);
  assert.match(core.state.summary, /\| growth \| 0\.00%/);
});

test("compareAction: missing OLD_SIZE on a normal run fails", async () => {
  const core = await runCompare({ NEW_SIZE: "120" });
  assert.match(core.state.failed ?? "", /invalid baseline/);
});
