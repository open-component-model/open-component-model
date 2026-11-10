// @ts-check
import assert from "assert";
import { test } from "node:test";
import { median, renderTable } from "./perf-compare.js";

/**
 * @param {number} total - durations.total, in seconds.
 * @param {number} errors - Reconcile errors of the component controller.
 */
function result(total, errors) {
  return {
    outcome: { ready: 1000, expected: 1000 },
    durations: { total, settled: total, p50: total / 2, p90: total },
    usage: { peakWorkingSetBytes: 256 * 1024 * 1024, peakRSSBytes: 300 * 1024 * 1024, registryRequests: 44000 },
    controllerCounters: {
      'controller_runtime_reconcile_time_seconds_count{controller="component"}': 2000,
      'controller_runtime_reconcile_time_seconds_sum{controller="component"}': 200,
      'controller_runtime_reconcile_errors_total{controller="component"}': errors,
      'controller_runtime_reconcile_time_seconds_count{controller="deployer"}': 0,
    },
  };
}

test("median of odd and even counts", () => {
  assert.strictEqual(median([3, 1, 2]), 2);
  assert.strictEqual(median([4, 1, 3, 2]), 2.5);
});

test("table compares medians and skips idle controllers", () => {
  const base = [result(110, 10), result(120, 10), result(100, 10)];
  const pr = [result(99, 0), result(90, 0), result(100, 0)];
  const table = renderTable(base, pr, "main");

  assert.match(table, /^\| Metric \| main \| PR \| Δ \|$/m);
  assert.match(table, /^\| Pass rate \(%\) \| 100 \| 100 \| 0 pp \|$/m);
  assert.match(table, /^\| Total \(s\) \\\* \| 110 \| 99 \| -10\.0% \|$/m);
  assert.match(table, /^\| Peak working set \(MiB\) \| 256 \| 256 \| 0% \|$/m);
  assert.match(table, /^\| component: error rate \(%\) \| 0\.5 \| 0 \| -0\.5 pp \|$/m);
  assert.doesNotMatch(table, /deployer/);
});

test("a change that rounds to zero reads 0%", () => {
  const table = renderTable([result(100, 0)], [result(100.01, 0)], "main");
  assert.match(table, /^\| Total \(s\) \\\* \| 100 \| 100\.0 \| 0% \|$/m);
});

/**
 * @param {number} total - durations.total of the rollout, in seconds.
 * @param {number} requests - Registry requests of the drift phase.
 */
function complexResult(total, requests) {
  return {
    ...result(total, 0),
    drift: {
      outcome: { ready: 1000, expected: 1000 },
      durations: { total: 30, settled: 31, p50: 10, p90: 20 },
      usage: { peakWorkingSetBytes: 0, peakRSSBytes: 0, registryRequests: requests },
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

test("drift rows come from the drift block", () => {
  const table = renderTable([complexResult(100, 8000)], [complexResult(100, 4000)], "main");

  assert.match(table, /^\| Drift: registry requests \| 8000 \| 4000 \| -50\.0% \|$/m);
  assert.match(table, /^\| Drift: total \(s\) \\\* \| 30 \| 30 \| 0% \|$/m);
  assert.match(table, /^\| Drift: registry manifest head \| 8000 \| 4000 \| -50\.0% \|$/m, "summed over status codes");
  assert.doesNotMatch(table, /Drift: registry blob get/, "requests the drift never made get no row");
  assert.match(table, /^\| Drift: namespaceddeployer: reconciles \| 2000 \| 2000 \| 0% \|$/m);
  assert.doesNotMatch(table, /Drift: component/, "controllers idle during the drift get no row");
});

test("rollout medians ignore the drift block", () => {
  const table = renderTable([complexResult(100, 8000)], [result(100, 0)], "main");

  assert.match(table, /^\| Registry requests \| 44000 \| 44000 \| 0% \|$/m);
  assert.doesNotMatch(table, /^\| namespaceddeployer/m);
});

test("a side without drift blocks renders n/a", () => {
  const table = renderTable([result(100, 0)], [complexResult(100, 4000)], "main");

  assert.match(table, /^\| Drift: registry requests \| n\/a \| 4000 \| n\/a \|$/m);
  assert.match(table, /^\| Drift: namespaceddeployer: reconciles \| n\/a \| 2000 \| n\/a \|$/m);
});

test("no drift rows without drift blocks", () => {
  assert.doesNotMatch(renderTable([result(100, 0)], [result(100, 0)], "main"), /Drift/);
});

test("a failed registry scrape is no value, not 0", () => {
  const failed = complexResult(100, 0);
  failed.drift.usage.registryRequests = null;
  failed.usage.registryRequests = null;
  const table = renderTable([failed, complexResult(100, 100)], [failed], "main");

  assert.match(table, /^\| Drift: registry requests \| 100 \| n\/a \| n\/a \|$/m);
  assert.match(table, /^\| Registry requests \| 44000 \| n\/a \| n\/a \|$/m);
});

test("registry requests group by handler and method whatever the other labels", () => {
  const extra = complexResult(100, 100);
  extra.drift.registryCounters = {
    'registry_http_requests_total{code="200",handler="manifest",method="head",zone="a"}': 60,
    'registry_http_requests_total{code="200",handler="manifest",method="head",zone="b"}': 40,
  };
  const table = renderTable([extra], [extra], "main");

  assert.match(table, /^\| Drift: registry manifest head \| 100 \| 100 \| 0% \|$/m);
});

test("a failed registry scrape is no value in the handler rows", () => {
  const failed = complexResult(100, 0);
  delete failed.drift.registryCounters;
  const table = renderTable([failed, complexResult(100, 100)], [failed], "main");

  assert.match(table, /^\| Drift: registry manifest head \| 100 \| n\/a \| n\/a \|$/m);
});
