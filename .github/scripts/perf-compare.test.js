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
