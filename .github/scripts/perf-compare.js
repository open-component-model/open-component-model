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
 * The phases of a result, in execution order. A base that predates phases has none:
 * its pull request has to merge the base branch first.
 *
 * @param {any} result
 * @returns {any[]}
 */
function phasesOf(result) {
  return result.phases ?? [];
}

/**
 * @param {any} result
 * @param {string} name
 * @returns {any} The phase of that name; undefined when the result has none.
 */
function phaseOf(result, name) {
  return phasesOf(result).find((phase) => phase.name === name);
}

/**
 * The names of the phases of any of the results, in execution order.
 *
 * @param {any[]} results
 * @returns {string[]}
 */
function phaseNames(results) {
  const names = new Set();
  for (const result of results) {
    for (const phase of phasesOf(result)) {
      names.add(phase.name);
    }
  }
  return [...names];
}

/**
 * @param {any} phase
 * @param {string} name - Prometheus counter name.
 * @param {string} controller
 * @returns {number}
 */
function counter(phase, name, controller) {
  return phase.cost.controllerCounters?.[`${name}{controller="${controller}"}`] ?? 0;
}

/**
 * The controllers that reconciled anything in any of the phases.
 *
 * @param {any[]} phases
 * @returns {string[]}
 */
function activeControllers(phases) {
  const names = new Set();
  for (const phase of phases) {
    for (const [key, value] of Object.entries(phase.cost.controllerCounters ?? {})) {
      const match = key.match(/^controller_runtime_reconcile_time_seconds_count\{controller="(.+)"\}$/);
      if (match && value > 0) {
        names.add(match[1]);
      }
    }
  }
  return [...names].sort();
}

/**
 * Registry requests by handler and method, summed over every other label.
 *
 * @param {any} phase
 * @returns {Map<string, number> | undefined} keyed by "handler method"; undefined when the registry scrape failed.
 */
function registryRequests(phase) {
  if (!phase.cost.registryCounters) {
    return undefined;
  }
  const sums = new Map();
  for (const [key, value] of Object.entries(phase.cost.registryCounters)) {
    if (!key.startsWith("registry_http_requests_total{")) {
      continue;
    }
    const handler = key.match(/[{,]handler="([^"]*)"/);
    const method = key.match(/[{,]method="([^"]*)"/);
    if (handler && method) {
      const name = `${handler[1]} ${method[1]}`;
      sums.set(name, (sums.get(name) ?? 0) + value);
    }
  }
  return sums;
}

/**
 * The handler and method pairs that any of the phases sent requests for.
 *
 * @param {any[]} phases
 * @returns {string[]}
 */
function activeRegistryRequests(phases) {
  const names = new Set();
  for (const phase of phases) {
    for (const [name, value] of registryRequests(phase) ?? []) {
      if (value > 0) {
        names.add(name);
      }
    }
  }
  return [...names].sort();
}

/**
 * @typedef {{ label: string, value: (result: any) => number | undefined, runnerDependent?: boolean, rate?: boolean }} Row
 */

/**
 * The compared rows of one phase. runnerDependent marks rows that vary with the runner's hardware.
 * Hand-picked sections of the result only. CPU is ignored as it can vary with the parallel runner.
 * A result without the phase has no value for any of them.
 *
 * @param {string} name - The phase.
 * @param {any[]} results
 * @returns {Row[]}
 */
function phaseRows(name, results) {
  const phases = results.map((r) => phaseOf(r, name)).filter(Boolean);
  const prefix = `${name.charAt(0).toUpperCase()}${name.slice(1)}:`;
  /**
   * @param {string} label
   * @param {(phase: any) => number | undefined | null} value
   * @param {{ runnerDependent?: boolean, rate?: boolean }} [opts]
   * @returns {Row}
   */
  const row = (label, value, opts = {}) => ({
    label: `${prefix} ${label}`,
    value: (r) => {
      const phase = phaseOf(r, name);
      return phase ? (value(phase) ?? undefined) : undefined;
    },
    ...opts,
  });
  return [
    row("pass rate (%)", (p) => (100 * p.outcome.ready) / p.outcome.expected, { rate: true }),
    row("total (s)", (p) => p.durations.total, { runnerDependent: true }),
    row("settled (s)", (p) => p.durations.settled, { runnerDependent: true }),
    row("p50 (s)", (p) => p.durations.p50, { runnerDependent: true }),
    row("p90 (s)", (p) => p.durations.p90, { runnerDependent: true }),
    row("peak working set (MiB)", (p) => p.cost.peakWorkingSetBytes / MiB),
    row("peak RSS (MiB)", (p) => p.cost.peakRSSBytes / MiB),
    row("registry requests", (p) => p.cost.registryRequests),
    ...activeRegistryRequests(phases).map((request) =>
      row(`registry ${request}`, (p) => {
        const requests = registryRequests(p);
        return requests ? (requests.get(request) ?? 0) : undefined;
      }),
    ),
    ...activeControllers(phases).flatMap((c) => [
      row(`${c}: reconciles`, (p) => counter(p, "controller_runtime_reconcile_time_seconds_count", c)),
      row(`${c}: reconcile time (s)`, (p) => counter(p, "controller_runtime_reconcile_time_seconds_sum", c), {
        runnerDependent: true,
      }),
      row(
        `${c}: error rate (%)`,
        (p) => {
          const reconciles = counter(p, "controller_runtime_reconcile_time_seconds_count", c);
          return reconciles ? (100 * counter(p, "controller_runtime_reconcile_errors_total", c)) / reconciles : 0;
        },
        { rate: true },
      ),
    ]),
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
 * The values of a row. A missing phase or a failed registry scrape (null) is no value, not 0.
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
  for (const row of phaseNames(all).flatMap((name) => phaseRows(name, all))) {
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
