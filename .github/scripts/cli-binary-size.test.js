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
 * @typedef {{ head_sha: string, conclusion: string | null }} Run
 * @typedef {{ sha: string, touchesWorkflow?: boolean }} Commit
 */

/**
 * A GitHub client with a run history of this workflow and a linear git history.
 *
 * @param {{ runs?: Run[], history?: Commit[], afterEachCall?: (runs: Run[]) => void }} opts
 *   runs: newest first, like the API; include the current (in-progress) run.
 *   history: oldest first; each commit's parent is the one before it.
 *   afterEachCall: mutates runs after every API call, to simulate runs that
 *   start while findBaseline is between requests.
 */
function fakeGithub({ runs = [], history = [], afterEachCall = () => {} } = {}) {
  /** @type {any[]} */
  const calls = [];
  const listCommits = async (/** @type {any} */ params) => {
    calls.push(["listCommits", params]);
    const end = history.findIndex((c) => c.sha === params.sha);
    const data = history
      .slice(0, end + 1)
      .map((c, i) => ({ sha: c.sha, touches: c.touchesWorkflow, parents: i > 0 ? [{ sha: history[i - 1].sha }] : [] }))
      .filter((c) => params.path === undefined || c.touches)
      .reverse()
      .map(({ sha, parents }) => ({ sha, parents }));
    afterEachCall(runs);
    return { data };
  };
  return {
    calls,
    paginate: async (/** @type {any} */ fn, /** @type {any} */ params) => (await fn(params)).data,
    rest: {
      actions: {
        listWorkflowRuns: async (/** @type {any} */ params) => {
          calls.push(["listWorkflowRuns", params]);
          const matching = params.status ? runs.filter((r) => r.conclusion === params.status) : runs;
          const perPage = params.per_page ?? 30;
          const page = params.page ?? 1;
          const data = { total_count: matching.length, workflow_runs: matching.slice((page - 1) * perPage, page * perPage) };
          afterEachCall(runs);
          return { data };
        },
      },
      repos: { listCommits },
    },
  };
}

/** The workflow is added in c1 and edited in c3; "before" is main before the guard. */
const guardHistory = [
  { sha: "before" },
  { sha: "c1", touchesWorkflow: true },
  { sha: "c2" },
  { sha: "c3", touchesWorkflow: true },
  { sha: "c4" },
];

/** @param {string} sha */
const green = (sha) => ({ head_sha: sha, conclusion: "success" });
/** @param {string} sha */
const failed = (sha) => ({ head_sha: sha, conclusion: "failure" });
/** @param {string} sha */
const cancelled = (sha) => ({ head_sha: sha, conclusion: "cancelled" });
/** @param {string} sha */
const running = (sha) => ({ head_sha: sha, conclusion: null });

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
  const github = fakeGithub({ runs: [running("tip"), failed("c2"), green("c1")] });
  assert.deepStrictEqual(await findBaseline(github, repo, "tip"), { sha: "c1", source: "last-green" });
  assert.deepStrictEqual(github.calls, [
    [
      "listWorkflowRuns",
      { ...repo, workflow_id: "cli-binary-size.yml", branch: "main", status: "success", per_page: 1 },
    ],
  ]);
});

test("findBaseline: the very first run uses the commit before the workflow was added", async () => {
  const github = fakeGithub({ runs: [running("c1")], history: guardHistory });
  assert.deepStrictEqual(await findBaseline(github, repo, "c1"), { sha: "before", source: "initial" });
  assert.deepStrictEqual(github.calls.at(-1), [
    "listCommits",
    { ...repo, sha: "c1", path: ".github/workflows/cli-binary-size.yml", per_page: 100 },
  ]);
});

test("findBaseline: without a green run the baseline stays before the workflow was added", async () => {
  // Failed and cancelled runs, and a later edit of the workflow (c3), do not move it.
  const github = fakeGithub({
    runs: [running("c4"), cancelled("c3"), failed("c2"), failed("c1")],
    history: guardHistory,
  });
  assert.deepStrictEqual(await findBaseline(github, repo, "c4"), { sha: "before", source: "initial" });
});

test("findBaseline: runs that start between requests do not move the baseline", async () => {
  // Review repro: a dispatch (separate concurrency group) starts while the
  // push run is resolving its baseline. Insert a new run after every call.
  let n = 0;
  const github = fakeGithub({
    runs: [running("c2"), failed("c1")],
    history: guardHistory,
    afterEachCall: (runs) => runs.unshift(running(`dispatch-${++n}`)),
  });
  assert.deepStrictEqual(await findBaseline(github, repo, "c2"), { sha: "before", source: "initial" });
});

test("findBaseline: fails if the workflow is not in the history of the tip", async () => {
  const github = fakeGithub({ runs: [running("x")], history: [{ sha: "before" }, { sha: "x" }] });
  await assert.rejects(findBaseline(github, repo, "x"), /is not in the history of x/);
});

test("findBaseline: fails if the commit that added the workflow has no parent", async () => {
  const github = fakeGithub({ runs: [running("root")], history: [{ sha: "root", touchesWorkflow: true }] });
  await assert.rejects(findBaseline(github, repo, "root"), /root, which added it, has no parent/);
});

test("a rejected increase stays rejected on the next push", async () => {
  // Review repro: the first run fails at +20%; the next push must not pass by
  // comparing against the rejected size.
  const sizes = /** @type {Record<string, number>} */ ({ before: 100, c1: 120, c2: 120 });
  /** @type {Run[]} */
  const runs = [];

  for (const tip of ["c1", "c2"]) {
    runs.unshift(running(tip));
    const { sha } = await findBaseline(fakeGithub({ runs, history: guardHistory }), repo, tip);
    const result = evaluate({
      oldSize: sizes[sha],
      newSize: sizes[tip],
      threshold: 10,
      reset: false,
      sameCommit: sha === tip,
    });
    assert.strictEqual(sha, "before", `${tip} compares against the size before the first run`);
    assert.strictEqual(result.over, true, `${tip} stays rejected`);
    runs[0] = failed(tip);
  }
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
    resolveAction({ github: fakeGithub({ runs: [running("tip"), green("green")] }), context, core }),
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
    resolveAction({ github: fakeGithub({ runs: [green("tip")] }), context, core }),
  );
  assert.strictEqual(core.outputs.skip_baseline_build, "true");
});

test("resolveAction: reset skips the baseline build", async () => {
  const core = fakeCore();
  const context = { eventName: "workflow_dispatch", ref: "refs/heads/main", sha: "tip", repo };
  await withEnv({ THRESHOLD_INPUT: "", RESET_INPUT: "true" }, () =>
    resolveAction({ github: fakeGithub({ runs: [running("tip"), green("green")] }), context, core }),
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
