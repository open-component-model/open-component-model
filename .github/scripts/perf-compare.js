// @ts-check
import fs from "fs";
import path from "path";

const MiB = 1024 * 1024;

/**
 * Read every result.json below dir: one per benchmark run.
 *
 * @param {string} dir - A downloaded `results` artifact.
 * @returns {any[]}
 */
export function loadResults(dir) {
  return fs
    .readdirSync(dir, { recursive: true, encoding: "utf8" })
    .filter((file) => path.basename(file) === "result.json")
    .map((file) => JSON.parse(fs.readFileSync(path.join(dir, file), "utf8")));
}

/**
 * @param {number[]} values
 * @returns {number}
 */
export function median(values) {
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

/**
 * @param {any} result
 * @param {string} name - Prometheus counter name.
 * @param {string} controller
 * @returns {number}
 */
function counter(result, name, controller) {
  return result.controllerCounters[`${name}{controller="${controller}"}`] ?? 0;
}

/**
 * The controllers that reconciled anything in any of the results.
 *
 * @param {any[]} results
 * @returns {string[]}
 */
function activeControllers(results) {
  const names = new Set();
  for (const result of results) {
    for (const [key, value] of Object.entries(result.controllerCounters)) {
      const match = key.match(/^controller_runtime_reconcile_time_seconds_count\{controller="(.+)"\}$/);
      if (match && value > 0) {
        names.add(match[1]);
      }
    }
  }
  return [...names].sort();
}

/**
 * The compared rows. runnerDependent marks rows that vary with the runner's hardware.
 * Hand-picked sections of the result only. CPU is ignored as it can vary with the parallel runner.
 *
 * @param {string[]} controllers
 * @returns {{ label: string, value: (result: any) => number, runnerDependent?: boolean, rate?: boolean }[]}
 */
function rows(controllers) {
  return [
    { label: "Pass rate (%)", value: (r) => (100 * r.outcome.ready) / r.outcome.expected, rate: true },
    { label: "Total (s)", value: (r) => r.durations.total, runnerDependent: true },
    { label: "Settled (s)", value: (r) => r.durations.settled, runnerDependent: true },
    { label: "p50 (s)", value: (r) => r.durations.p50, runnerDependent: true },
    { label: "p90 (s)", value: (r) => r.durations.p90, runnerDependent: true },
    { label: "Peak working set (MiB)", value: (r) => r.usage.peakWorkingSetBytes / MiB },
    { label: "Peak RSS (MiB)", value: (r) => r.usage.peakRSSBytes / MiB },
    { label: "Registry requests", value: (r) => r.usage.registryRequests },
    ...controllers.flatMap((c) => [
      {
        label: `${c}: reconciles`,
        value: (/** @type {any} */ r) => counter(r, "controller_runtime_reconcile_time_seconds_count", c),
      },
      {
        label: `${c}: reconcile time (s)`,
        value: (/** @type {any} */ r) => counter(r, "controller_runtime_reconcile_time_seconds_sum", c),
        runnerDependent: true,
      },
      {
        label: `${c}: error rate (%)`,
        value: (/** @type {any} */ r) => {
          const reconciles = counter(r, "controller_runtime_reconcile_time_seconds_count", c);
          return reconciles ? (100 * counter(r, "controller_runtime_reconcile_errors_total", c)) / reconciles : 0;
        },
        rate: true,
      },
    ]),
  ];
}

/** @param {number} value */
function format(value) {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

/**
 * Rates change by percentage points: a relative change makes a small rate look dramatic.
 *
 * @param {number} base
 * @param {number} pr
 * @param {boolean} [rate]
 */
function delta(base, pr, rate) {
  if (!rate && base === 0) {
    return pr === 0 ? "0%" : "n/a";
  }
  const change = rate ? pr - base : ((pr - base) / base) * 100;
  const unit = rate ? " pp" : "%";
  if (Math.abs(change) < 0.05) {
    return `0${unit}`;
  }
  return `${change > 0 ? "+" : ""}${change.toFixed(1)}${unit}`;
}

/**
 * Render the medians of both result sets as a markdown table.
 *
 * @param {any[]} base - Results of the base branch.
 * @param {any[]} pr - Results of the pull request.
 * @param {string} baseLabel - Column header of the base branch.
 * @returns {string}
 */
export function renderTable(base, pr, baseLabel) {
  const lines = [`| Metric | ${baseLabel} | PR | Δ |`, "|---|---:|---:|---:|"];
  for (const row of rows(activeControllers([...base, ...pr]))) {
    const a = median(base.map(row.value));
    const b = median(pr.map(row.value));
    lines.push(`| ${row.label}${row.runnerDependent ? " \\*" : ""} | ${format(a)} | ${format(b)} | ${delta(a, b, row.rate)} |`);
  }
  lines.push("", "\\* Depends on the runner: both sides run on different machines.");
  return lines.join("\n");
}

/**
 * github-script entry point: comment the comparison of a /perf run on the pull request.
 *
 * Reads the downloaded artifacts from results/base and results/pr, and BASE_REF,
 * BASE_SHA, PR_SHA, RESULT and RUN_URL from the environment.
 *
 * @param {object} deps
 * @param {import("@actions/github-script").AsyncFunctionArguments["github"]} deps.github
 * @param {import("@actions/github-script").AsyncFunctionArguments["context"]} deps.context
 */
export default async function perfCompare({ github, context }) {
  const env = /** @type {Record<string, string>} */ (process.env);
  const commits = `\`${env.BASE_REF}\` ${env.BASE_SHA.slice(0, 7)} vs PR ${env.PR_SHA.slice(0, 7)}`;

  const body =
    env.RESULT === "success"
      ? `✅ Performance run finished: ${commits}\n\n` +
        `${renderTable(loadResults("results/base"), loadResults("results/pr"), env.BASE_REF)}\n\n` +
        `[Run](${env.RUN_URL})`
      : `🛑 Performance run failed: ${commits}\n\n[Run](${env.RUN_URL})`;

  await github.rest.issues.createComment({
    ...context.repo,
    issue_number: context.issue.number,
    body,
  });
}
