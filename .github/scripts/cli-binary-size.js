// @ts-check

// Decision logic of .github/workflows/cli-binary-size.yml. The workflow only
// checks out and builds; this module picks the baseline, validates the
// dispatch inputs, and decides whether the growth is acceptable.

export const DEFAULT_THRESHOLD_PERCENT = 10;
export const WORKFLOW_FILE = "cli-binary-size.yml";
export const MAIN_REF = "refs/heads/main";

const MiB = 1024 * 1024;

/**
 * @param {string | undefined} input - The threshold_percent dispatch input; empty on a push.
 * @returns {number}
 */
export function parseThreshold(input) {
  const value = (input ?? "").trim();
  if (value === "") {
    return DEFAULT_THRESHOLD_PERCENT;
  }
  // Only plain non-negative decimals: "15%" or "1e3" are rejected instead of
  // being coerced into a threshold the maintainer did not mean.
  if (!/^\d+(\.\d+)?$/.test(value)) {
    throw new Error(`threshold_percent must be a non-negative number, got '${input}'`);
  }
  return Number(value);
}

/**
 * The head commit of the most recent successful run of this workflow on main,
 * or the parent of the tip when no run has passed yet (the first run).
 *
 * @param {any} github - Octokit client from actions/github-script.
 * @param {{ owner: string, repo: string }} repo
 * @param {string} tipSha
 * @returns {Promise<{ sha: string, source: "last-green" | "parent" }>}
 */
export async function findBaseline(github, repo, tipSha) {
  const { data } = await github.rest.actions.listWorkflowRuns({
    ...repo,
    workflow_id: WORKFLOW_FILE,
    branch: "main",
    status: "success",
    per_page: 1,
  });
  const run = data.workflow_runs?.[0];
  if (run) {
    return { sha: run.head_sha, source: "last-green" };
  }
  const { data: commit } = await github.rest.repos.getCommit({ ...repo, ref: tipSha });
  const parent = commit.parents?.[0]?.sha;
  if (!parent) {
    throw new Error(`no green run of ${WORKFLOW_FILE} on main and ${tipSha} has no parent`);
  }
  return { sha: parent, source: "parent" };
}

/**
 * @typedef {object} Evaluation
 * @property {"reset" | "same-commit" | "compared"} kind
 * @property {number} newSize
 * @property {number} [oldSize]
 * @property {number} [growthPercent]
 * @property {number} [deltaBytes]
 * @property {boolean} over
 */

/**
 * @param {{ oldSize?: number, newSize: number, threshold: number, reset: boolean, sameCommit: boolean }} input
 * @returns {Evaluation}
 */
export function evaluate({ oldSize, newSize, threshold, reset, sameCommit }) {
  if (!(newSize > 0)) {
    throw new Error(`invalid tip binary size: ${newSize}`);
  }
  if (reset) {
    return { kind: "reset", newSize, over: false };
  }
  if (sameCommit) {
    return { kind: "same-commit", newSize, oldSize: newSize, growthPercent: 0, deltaBytes: 0, over: false };
  }
  if (oldSize === undefined || !(oldSize > 0)) {
    throw new Error(`invalid baseline binary size: ${oldSize}`);
  }
  const deltaBytes = newSize - oldSize;
  const growthPercent = (deltaBytes / oldSize) * 100;
  return { kind: "compared", newSize, oldSize, growthPercent, deltaBytes, over: growthPercent > threshold };
}

/**
 * @param {number} bytes
 * @returns {string}
 */
export function mib(bytes) {
  return (bytes / MiB).toFixed(1);
}

/**
 * @param {number} bytes
 * @returns {string}
 */
function signedMiB(bytes) {
  return `${bytes >= 0 ? "+" : ""}${mib(bytes)}`;
}

/**
 * @param {Evaluation} result
 * @param {{ baseSha: string, tipSha: string, threshold: number, eventName: string, actor: string }} meta
 * @returns {string} Markdown for the job summary.
 */
export function renderSummary(result, { baseSha, tipSha, threshold, eventName, actor }) {
  const lines = ["### CLI binary size guard", ""];
  if (result.kind === "reset") {
    lines.push(
      `Baseline reset by @${actor}. This run becomes the new green baseline.`,
      "",
      "| metric | value |",
      "| --- | --- |",
      `| new baseline | \`${tipSha}\` |`,
      `| size | ${mib(result.newSize)} MiB (${result.newSize} bytes) |`,
    );
    return lines.join("\n") + "\n";
  }
  const oldSize = /** @type {number} */ (result.oldSize);
  lines.push(
    "| metric | value |",
    "| --- | --- |",
    `| baseline (last green) | \`${baseSha}\` |`,
    `| new tip | \`${tipSha}\` |`,
    `| baseline size | ${mib(oldSize)} MiB (${oldSize} bytes) |`,
    `| new tip size | ${mib(result.newSize)} MiB (${result.newSize} bytes) |`,
    `| growth | ${formatGrowth(result)} |`,
    `| threshold | ${threshold}% |`,
  );
  if (eventName === "workflow_dispatch") {
    lines.push(`| manual override | by @${actor} |`);
  }
  return lines.join("\n") + "\n";
}

/**
 * @param {Evaluation} result
 * @returns {string}
 */
function formatGrowth(result) {
  const growth = /** @type {number} */ (result.growthPercent);
  return `${growth.toFixed(2)}% (${signedMiB(/** @type {number} */ (result.deltaBytes))} MiB)`;
}

/**
 * First step: validate the dispatch inputs and resolve the baseline before
 * anything is built, so a bad input fails in seconds.
 *
 * Outputs: base_sha, tip_sha, threshold, reset, skip_baseline_build.
 *
 * @param {{ github: any, context: any, core: any }} args
 */
export async function resolveAction({ github, context, core }) {
  const env = /** @type {Record<string, string | undefined>} */ (process.env);
  try {
    // A dispatch sets the next green baseline, so it must run on main. The
    // branch filter in findBaseline already keeps branch runs out of the
    // baseline; this makes the rule explicit.
    if (context.eventName === "workflow_dispatch" && context.ref !== MAIN_REF) {
      throw new Error(`workflow_dispatch must run on main, got ${context.ref}`);
    }
    const threshold = parseThreshold(env.THRESHOLD_INPUT);
    const reset = env.RESET_INPUT === "true";
    const tipSha = context.sha;
    const baseline = await findBaseline(github, context.repo, tipSha);
    if (baseline.source === "parent") {
      core.notice(`No green run found, using parent ${baseline.sha} as baseline.`);
    }
    core.info(`Baseline: ${baseline.sha} (${baseline.source}), tip: ${tipSha}, threshold: ${threshold}%`);

    core.setOutput("base_sha", baseline.sha);
    core.setOutput("tip_sha", tipSha);
    core.setOutput("threshold", String(threshold));
    core.setOutput("reset", String(reset));
    core.setOutput("skip_baseline_build", String(reset || baseline.sha === tipSha));
  } catch (err) {
    core.setFailed(err instanceof Error ? err.message : String(err));
  }
}

/**
 * Last step: compare the two measured sizes and enforce the threshold.
 *
 * @param {{ context: any, core: any }} args
 */
export async function compareAction({ context, core }) {
  const env = /** @type {Record<string, string | undefined>} */ (process.env);
  try {
    const baseSha = env.BASE_SHA ?? "";
    const tipSha = env.TIP_SHA ?? "";
    const threshold = Number(env.THRESHOLD);
    const result = evaluate({
      oldSize: env.OLD_SIZE ? Number(env.OLD_SIZE) : undefined,
      newSize: Number(env.NEW_SIZE),
      threshold,
      reset: env.RESET === "true",
      sameCommit: baseSha === tipSha,
    });

    await core.summary
      .addRaw(renderSummary(result, { baseSha, tipSha, threshold, eventName: context.eventName, actor: context.actor }))
      .write();

    if (result.kind === "reset") {
      core.info(`Baseline reset: new baseline ${tipSha} at ${mib(result.newSize)} MiB (${result.newSize} bytes).`);
      return;
    }
    core.info(`Growth: ${formatGrowth(result)}, threshold ${threshold}%`);
    if (result.over) {
      core.setFailed(
        `CLI binary grew ${formatGrowth(result)}, above the ${threshold}% threshold. ` +
          "Approve with a workflow_dispatch and a higher threshold, or reduce the size.",
      );
      return;
    }
    core.info("CLI binary growth is within the threshold.");
  } catch (err) {
    core.setFailed(err instanceof Error ? err.message : String(err));
  }
}
