// @ts-check
import assert from "assert";
import { test } from "node:test";
import { median, renderTable } from "./perf-compare.js";

/**
 * @param {number} total - durations.total, in seconds.
 * @param {number} errors - Reconcile errors of the component controller.
 */
function rollout(total, errors) {
  return {
    name: "rollout",
    outcome: { ready: 1000, expected: 1000 },
    durations: { total, settled: total, p50: total / 2, p90: total },
    cost: {
      peakWorkingSetBytes: 256 * 1024 * 1024,
      peakRSSBytes: 300 * 1024 * 1024,
      registryRequests: 44000,
      controllerCounters: {
        'controller_runtime_reconcile_time_seconds_count{controller="component"}': 2000,
        'controller_runtime_reconcile_time_seconds_sum{controller="component"}': 200,
        'controller_runtime_reconcile_errors_total{controller="component"}': errors,
        'controller_runtime_reconcile_time_seconds_count{controller="deployer"}': 0,
      },
      registryCounters: {
        'registry_http_requests_total{code="200",handler="blob",method="get"}': 13000,
      },
    },
  };
}

/**
 * @param {string} name
 * @param {number} requests - Registry requests of the phase.
 */
function deployerPhase(name, requests) {
  return {
    name,
    outcome: { ready: 1000, expected: 1000 },
    durations: { total: 30, settled: 31, p50: 10, p90: 20 },
    cost: {
      peakWorkingSetBytes: 0,
      peakRSSBytes: 0,
      registryRequests: requests,
      controllerCounters: {
        'controller_runtime_reconcile_time_seconds_count{controller="namespaceddeployer"}': 2000,
        'controller_runtime_reconcile_time_seconds_count{controller="component"}': 0,
      },
      registryCounters: {
        'registry_http_requests_total{code="200",handler="manifest",method="head"}': requests - 10,
        'registry_http_requests_total{code="404",handler="manifest",method="head"}': 10,
        'registry_http_requests_total{code="200",handler="blob",method="get"}': 0,
        'registry_http_request_duration_seconds_count{handler="manifest",method="head"}': requests,
      },
    },
  };
}

/** @param {...any} phases */
function result(...phases) {
  return { phases };
}

/**
 * A result of a base branch that predates phases: the rollout at the top level.
 *
 * @param {number} total
 */
function oldResult(total) {
  const { outcome, durations, cost } = rollout(total, 0);
  return { outcome, durations, usage: cost };
}

test("median of odd and even counts", () => {
  assert.strictEqual(median([3, 1, 2]), 2);
  assert.strictEqual(median([4, 1, 3, 2]), 2.5);
});

test("table compares medians and skips idle controllers", () => {
  const base = [result(rollout(110, 10)), result(rollout(120, 10)), result(rollout(100, 10))];
  const pr = [result(rollout(99, 0)), result(rollout(90, 0)), result(rollout(100, 0))];
  const table = renderTable(base, pr, "main");

  assert.match(table, /^\| Metric \| main \| PR \| Δ \|$/m);
  assert.match(table, /^\| Rollout: pass rate \(%\) \| 100 \| 100 \| 0 pp \|$/m);
  assert.match(table, /^\| Rollout: total \(s\) \\\* \| 110 \| 99 \| -10\.0% \|$/m);
  assert.match(table, /^\| Rollout: peak working set \(MiB\) \| 256 \| 256 \| 0% \|$/m);
  assert.match(table, /^\| Rollout: component: error rate \(%\) \| 0\.5 \| 0 \| -0\.5 pp \|$/m);
  assert.match(table, /^\| Rollout: registry blob get \| 13000 \| 13000 \| 0% \|$/m);
  assert.doesNotMatch(table, /deployer/);
});

test("a change that rounds to zero reads 0%", () => {
  const table = renderTable([result(rollout(100, 0))], [result(rollout(100.01, 0))], "main");
  assert.match(table, /^\| Rollout: total \(s\) \\\* \| 100 \| 100\.0 \| 0% \|$/m);
});

test("every phase gets the same rows, in execution order", () => {
  const full = () => result(rollout(100, 0), deployerPhase("drift", 12000), deployerPhase("update", 20000));
  const table = renderTable([full()], [full()], "main");

  const prefixes = [...table.matchAll(/^\| (\w+): pass rate/gm)].map((m) => m[1]);
  assert.deepStrictEqual(prefixes, ["Rollout", "Drift", "Update"]);
  for (const phase of ["Rollout", "Drift", "Update"]) {
    for (const row of ["total \\(s\\) \\\\\\*", "settled \\(s\\) \\\\\\*", "p90 \\(s\\) \\\\\\*", "peak RSS \\(MiB\\)", "registry requests"]) {
      assert.match(table, new RegExp(`^\\| ${phase}: ${row} \\|`, "m"));
    }
  }
  assert.match(table, /^\| Drift: registry requests \| 12000 \| 12000 \| 0% \|$/m);
  assert.match(table, /^\| Update: registry manifest head \| 20000 \| 20000 \| 0% \|$/m, "summed over status codes");
  assert.match(table, /^\| Drift: namespaceddeployer: reconciles \| 2000 \| 2000 \| 0% \|$/m);
  assert.doesNotMatch(table, /Drift: component/, "controllers idle in a phase get no row there");
  assert.doesNotMatch(table, /Drift: registry blob get/, "requests a phase never made get no row there");
});

test("a base without phases has no value in any phase", () => {
  const pr = result(rollout(90, 0), deployerPhase("drift", 4000));
  const table = renderTable([oldResult(100)], [pr], "main");

  assert.match(table, /^\| Rollout: total \(s\) \\\* \| n\/a \| 90 \| n\/a \|$/m);
  assert.match(table, /^\| Drift: registry requests \| n\/a \| 4000 \| n\/a \|$/m);
});

test("a side without a phase renders n/a", () => {
  const table = renderTable([result(rollout(100, 0))], [result(rollout(100, 0), deployerPhase("drift", 4000))], "main");

  assert.match(table, /^\| Drift: registry requests \| n\/a \| 4000 \| n\/a \|$/m);
  assert.match(table, /^\| Drift: namespaceddeployer: reconciles \| n\/a \| 2000 \| n\/a \|$/m);
  assert.doesNotMatch(table, /Update/, "no side has an update phase");
});

test("a failed registry scrape is no value, not 0", () => {
  const failed = result(rollout(100, 0), deployerPhase("drift", 0));
  failed.phases[0].cost.registryRequests = null;
  failed.phases[1].cost.registryRequests = null;
  const table = renderTable([failed, result(rollout(100, 0), deployerPhase("drift", 100))], [failed], "main");

  assert.match(table, /^\| Drift: registry requests \| 100 \| n\/a \| n\/a \|$/m);
  assert.match(table, /^\| Rollout: registry requests \| 44000 \| n\/a \| n\/a \|$/m);
});

test("registry requests group by handler and method whatever the other labels", () => {
  const extra = result(rollout(100, 0), deployerPhase("drift", 100));
  extra.phases[1].cost.registryCounters = {
    'registry_http_requests_total{code="200",handler="manifest",method="head",zone="a"}': 60,
    'registry_http_requests_total{code="200",handler="manifest",method="head",zone="b"}': 40,
  };
  const table = renderTable([extra], [extra], "main");

  assert.match(table, /^\| Drift: registry manifest head \| 100 \| 100 \| 0% \|$/m);
});

test("a failed registry scrape is no value in the handler rows", () => {
  const failed = result(rollout(100, 0), deployerPhase("drift", 0));
  // A failed scrape writes no counters: null in result.json.
  failed.phases[0].cost.registryCounters = null;
  failed.phases[1].cost.registryCounters = null;
  const table = renderTable([failed, result(rollout(100, 0), deployerPhase("drift", 100))], [failed], "main");

  assert.match(table, /^\| Drift: registry manifest head \| 100 \| n\/a \| n\/a \|$/m);
  assert.match(table, /^\| Rollout: registry blob get \| 13000 \| n\/a \| n\/a \|$/m);
});
