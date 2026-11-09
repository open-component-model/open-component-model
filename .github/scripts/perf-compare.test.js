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
        'ocm_system_ocm_k8s_toolkit_oci_blob_cache_fetches_total{kind="metadata",result="hit"}': 6000,
        'ocm_system_ocm_k8s_toolkit_oci_blob_cache_fetches_total{kind="blob",result="bypass"}': 1000,
      },
      registryCounters: {},
    },
  };
}

test("drift rows come from the drift block", () => {
  const table = renderTable([complexResult(100, 8000)], [complexResult(100, 4000)], "main");

  assert.match(table, /^\| Drift: registry requests \| 8000 \| 4000 \| -50\.0% \|$/m);
  assert.match(table, /^\| Drift: total \(s\) \\\* \| 30 \| 30 \| 0% \|$/m);
  assert.match(table, /^\| Drift: blob cache metadata hit \| 6000 \| 6000 \| 0% \|$/m);
  assert.match(table, /^\| Drift: blob cache blob bypass \| 1000 \| 1000 \| 0% \|$/m);
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
