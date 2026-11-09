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
 * @returns {number} NaN for no values.
 */
export function median(values) {
  if (values.length === 0) {
    return NaN;
  }
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

const blobCacheFetches = /^ocm_system_ocm_k8s_toolkit_oci_blob_cache_fetches_total\{kind="(.+)",result="(.+)"\}$/;

/**
 * The blob cache series of the drift blocks: the counter key with its kind and result.
 *
 * @param {any[]} drifts
 * @returns {{ key: string, kind: string, result: string }[]}
 */
function blobCacheSeries(drifts) {
  const series = new Map();
  for (const drift of drifts) {
    for (const key of Object.keys(drift.controllerCounters ?? {})) {
      const match = key.match(blobCacheFetches);
      if (match) {
        series.set(key, { key, kind: match[1], result: match[2] });
      }
    }
  }
  return [...series.keys()].sort().map((key) => series.get(key));
}

/**
 * @typedef {{ label: string, value: (result: any) => number | undefined, runnerDependent?: boolean, rate?: boolean }} Row
 */

/**
 * The compared rows. runnerDependent marks rows that vary with the runner's hardware.
 * Hand-picked sections of the result only. CPU is ignored as it can vary with the parallel runner.
 *
 * @param {string[]} controllers
 * @returns {Row[]}
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

/**
 * The rows of the drift phase of complex. Results without a drift block, such as those
 * of a base branch that predates the phase, have no value for them.
 *
 * @param {any[]} results
 * @returns {Row[]}
 */
function driftRows(results) {
  const drifts = results.map((r) => r.drift).filter(Boolean);
  if (drifts.length === 0) {
    return [];
  }
  /** @param {(drift: any) => number} value */
  const ofDrift = (value) => (/** @type {any} */ r) => (r.drift ? value(r.drift) : undefined);
  return [
    { label: "Drift: total (s)", value: ofDrift((d) => d.durations.total), runnerDependent: true },
    { label: "Drift: registry requests", value: ofDrift((d) => d.usage.registryRequests) },
    ...blobCacheSeries(drifts).map(({ key, kind, result }) => ({
      label: `Drift: blob cache ${kind} ${result}`,
      value: ofDrift((d) => d.controllerCounters[key] ?? 0),
    })),
    ...activeControllers(drifts).map((c) => ({
      label: `Drift: ${c}: reconciles`,
      value: ofDrift((d) => counter(d, "controller_runtime_reconcile_time_seconds_count", c)),
    })),
  ];
}

/** @param {number} value */
function format(value) {
  if (Number.isNaN(value)) {
    return "n/a";
  }
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
  if (Number.isNaN(base) || Number.isNaN(pr)) {
    return "n/a";
  }
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
 * The values of a row. A missing drift block or a failed registry scrape (null) is no value, not 0.
 *
 * @param {any[]} results
 * @param {Row} row
 * @returns {number[]}
 */
function valuesOf(results, row) {
  return /** @type {number[]} */ (results.map(row.value).filter((v) => v !== undefined && v !== null));
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
  const all = [...base, ...pr];
  for (const row of [...rows(activeControllers(all)), ...driftRows(all)]) {
    const a = median(valuesOf(base, row));
    const b = median(valuesOf(pr, row));
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
